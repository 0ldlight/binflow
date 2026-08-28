#!/usr/bin/env bash
# install.sh — install BinFlow via systemd on a Linux host (T-138, FR-39, PB-07;
# hardened in T-169/G15b: working checksum verification, dry-run that actually
# runs, config fallback, binary upgrade path, post-install health gate).
#
# Idempotent when possible: re-running the script with the same arguments will
# skip already-completed steps rather than failing.
#
# Usage:
#   # Install the latest release (default URL pattern):
#   sudo bash install.sh
#
#   # Install a specific version:
#   sudo bash install.sh --version v1.0.0
#
#   # Install from a custom base URL (on-prem / air-gapped mirror):
#   sudo bash install.sh --base-url https://releases.example.com/artifacts
#
#   # Dry-run: print what would be done without making changes (root not
#   # required — also previews the linux flow from a non-Linux workstation):
#   bash install.sh --dry-run --version v1.0.0
#
#   # Uninstall (see --uninstall section at the bottom):
#   sudo bash install.sh --uninstall
#
# Dependencies (standard on any systemd-based Linux):
#   bash, curl, sha256sum, systemctl, useradd, mkdir, chown, cp, tar, mktemp, grep
#
# Uninstall path:
#   sudo bash install.sh --uninstall
#   This stops the service, disables it, removes the unit file, and (with
#   --purge) removes the user and data directories.
#
# The install script downloads TWO files from the release:
#   1. The binary archive  (binflow_<VER>_<os>_<arch>.tar.gz)
#   2. The checksums file   (binflow_<VER>_checksums.txt)
# It verifies the archive against the checksums file BEFORE extracting
# anything.  If the checksum fails, the script aborts with exit code 1 and
# zero files are written to the installation paths.

set -euo pipefail

# ---- defaults ------------------------------------------------------------------
VERSION="${BINFLOW_VERSION:-latest}"
BASE_URL="${BINFLOW_BASE_URL:-https://github.com/example/binflow/releases/download}"
DRY_RUN=false
UNINSTALL=false
PURGE=false

# ---- usage ---------------------------------------------------------------------
usage() {
  cat <<'EOF'
Usage: install.sh [OPTIONS]

Options:
  --version VER    BinFlow version to install (default: latest)
  --base-url URL   Base URL for downloads (default: GitHub releases)
  --dry-run        Print what would be done, do not make changes (no root
                   needed; downloads, checksum verification and service
                   actions are skipped)
  --uninstall      Stop, disable, and remove the BinFlow service
  --purge          With --uninstall: also remove the binflow user and data
  --help           Show this help text

Environment:
  BINFLOW_VERSION       Default version (overridden by --version)
  BINFLOW_BASE_URL      Default download base URL (overridden by --base-url)
  BINFLOW_START_TIMEOUT Seconds to wait for the service to become active
                        after enable --now (default: 30)
  BINFLOW_HEALTH_URL    Health probe URL consulted after start
                        (default: http://127.0.0.1:8080/readyz)

Examples:
  sudo bash install.sh
  sudo bash install.sh --version v1.0.0
  sudo bash install.sh --base-url https://releases.example.com/artifacts
  bash install.sh --dry-run --version v1.0.0
  sudo bash install.sh --uninstall --purge
EOF
  exit 0
}

# ---- argument parsing ---------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)   VERSION="$2"; shift 2 ;;
    --base-url)  BASE_URL="$2"; shift 2 ;;
    --dry-run)   DRY_RUN=true; shift ;;
    --uninstall) UNINSTALL=true; shift ;;
    --purge)     PURGE=true; shift ;;
    --help)      usage ;;
    *)           echo "ERROR: unknown option: $1" >&2; exit 2 ;;
  esac
done

# ---- helpers -------------------------------------------------------------------
maybe() {
  if "$DRY_RUN"; then
    echo "[DRY-RUN] $*"
  else
    "$@"
  fi
}

log()  { echo "==> $*"; }
warn() { echo "WARN: $*" >&2; }
die()  { echo "ERROR: $*" >&2; exit 1; }

require_root() {
  # A dry-run makes no changes, so a preview must not demand root.
  if "$DRY_RUN" && [[ "${EUID:-}" -ne 0 ]]; then
    log "[DRY-RUN] not running as root — continuing (no changes will be made)"
    return 0
  fi
  if [[ "${EUID:-}" -ne 0 ]]; then
    die "this script must be run as root (use sudo)"
  fi
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" &>/dev/null; then
    die "required command not found: $cmd"
  fi
}

# ---- detect platform -----------------------------------------------------------
detect_platform() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"

  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) die "unsupported architecture: $arch" ;;
  esac

  case "$os" in
    linux) ;;
    *)
      # A dry-run is a preview, not an install: let a non-Linux workstation
      # (e.g. a developer's macOS) preview the linux flow instead of dying.
      if "$DRY_RUN"; then
        warn "[DRY-RUN] host OS ${os} is not a deployment target — previewing the linux_${arch} flow"
        os="linux"
      else
        die "unsupported OS: $os (this script is for Linux with systemd)"
      fi
      ;;
  esac

  echo "${os}_${arch}"
}

# ---- version resolution --------------------------------------------------------
resolve_version() {
  if [[ "$VERSION" == "latest" ]]; then
    if "$DRY_RUN"; then
      log "[DRY-RUN] would resolve latest version via ${BASE_URL}/latest (pass --version to pin the previewed names)"
      return 0
    fi
    # Try to resolve latest via GitHub API redirect.  If it fails (no network,
    # air-gapped, or not GitHub), ask the user to specify --version explicitly.
    local latest
    latest="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
      "${BASE_URL}/latest" 2>/dev/null || true)"
    if [[ -n "$latest" ]]; then
      # Extract the tag from the redirect URL (last path segment)
      VERSION="${latest##*/}"
      log "resolved latest version: ${VERSION}"
    else
      die "cannot resolve latest version — specify --version explicitly"
    fi
  fi
}

# ---- checksum verification (G16) -----------------------------------------------
verify_checksum() {
  local archive_file="$1"
  local checksums_file="$2"

  if "$DRY_RUN"; then
    log "[DRY-RUN] would verify the checksum of $(basename "$archive_file") against $(basename "$checksums_file")"
    return 0
  fi

  log "verifying checksum for ${archive_file}"

  # Extract the expected checksum line for our archive from the checksums file.
  # goreleaser produces lines like:
  #   <sha256>  binflow_<VER>_<os>_<arch>.tar.gz
  local archive_basename
  archive_basename="$(basename "$archive_file")"

  if ! grep -qF "  ${archive_basename}" "$checksums_file"; then
    die "checksums file does not contain an entry for ${archive_basename}"
  fi

  # sha256sum -c resolves the file names in the checksums lines relative to
  # the CURRENT directory, not the checksums file — the lines carry only the
  # basename, so the check must run from the directory holding the archive
  # (T-169: without this, verification failed against the operator's CWD and
  # every real install aborted).
  if ! (cd "$(dirname "$archive_file")" && \
        sha256sum -c --status <(grep -F "  ${archive_basename}" "$checksums_file")); then
    # Run again without --status to show the mismatch details
    (cd "$(dirname "$archive_file")" && \
      sha256sum -c <(grep -F "  ${archive_basename}" "$checksums_file")) >&2 || true
    die "checksum verification FAILED for ${archive_basename} — aborting (zero files written)"
  fi

  log "checksum OK"
}

# ---- uninstall path -------------------------------------------------------------
do_uninstall() {
  log "uninstalling BinFlow..."

  if systemctl is-active --quiet binflow 2>/dev/null; then
    log "stopping binflow service..."
    maybe systemctl stop binflow
  fi

  if systemctl is-enabled --quiet binflow 2>/dev/null; then
    log "disabling binflow service..."
    maybe systemctl disable binflow
  fi

  if [[ -f /etc/systemd/system/binflow.service ]]; then
    log "removing unit file..."
    maybe rm -f /etc/systemd/system/binflow.service
    maybe systemctl daemon-reload
  fi

  if [[ -f /usr/local/bin/binflow-server ]]; then
    log "removing binary..."
    maybe rm -f /usr/local/bin/binflow-server
  fi

  if "$PURGE"; then
    if id binflow &>/dev/null; then
      log "removing binflow user..."
      maybe userdel binflow
    fi
    if [[ -d /var/lib/binflow ]]; then
      warn "removing /var/lib/binflow (all data will be lost)"
      maybe rm -rf /var/lib/binflow
    fi
    if [[ -d /etc/binflow ]]; then
      warn "removing /etc/binflow (all config will be lost)"
      maybe rm -rf /etc/binflow
    fi
  else
    log "data and config preserved (use --purge to remove)"
    log "  /var/lib/binflow"
    log "  /etc/binflow"
  fi

  log "uninstall complete"
  exit 0
}

# ---- default config generation --------------------------------------------------
# Minimal config for the unit layout: data dir inside the service's only
# writable path, everything else on built-in defaults (listen :8080, sqlite
# metadata at <data_dir>/binflow.db). Written root-owned 0644 — the service
# user reads it but cannot modify it (ProtectSystem=strict keeps /etc
# read-only anyway).
write_default_config() {
  if "$DRY_RUN"; then
    echo "[DRY-RUN] write minimal config to /etc/binflow/binflow.yaml"
    return 0
  fi
  cat > /etc/binflow/binflow.yaml <<'EOF'
# BinFlow configuration generated by contrib/systemd/install.sh.
# All omitted settings keep their built-in defaults; see the docs
# (Configuration) for the full schema. Secrets (e.g. the admin bootstrap
# password, BINFLOW_ADMIN_PASSWORD) belong in /etc/binflow/env, never here.
server:
  listen: ":8080"
storage:
  data_dir: /var/lib/binflow
EOF
  chmod 0644 /etc/binflow/binflow.yaml
}

# ---- post-install verification (G15b) -------------------------------------------
# Wait for the unit to reach active state; hard-fail with diagnostics when it
# never does (a fresh install must not exit 0 over a crash-looping service).
wait_service_active() {
  local timeout="${BINFLOW_START_TIMEOUT:-30}"
  local waited=0
  until systemctl is-active --quiet binflow; do
    if [[ "$waited" -ge "$timeout" ]]; then
      return 1
    fi
    sleep 1
    waited=$((waited + 1))
  done
  return 0
}

# Best-effort readiness probe; advisory only because the listen address is a
# config knob (server.listen) an operator may have changed.
health_url() {
  echo "${BINFLOW_HEALTH_URL:-http://127.0.0.1:8080/readyz}"
}

# The unit reaching active state does not mean the HTTP listener is bound yet
# (migrations and the admin seed run first — about a second on a small host),
# so the probe retries for a bounded window instead of firing once.
probe_readyz() {
  local url tries i
  url="$(health_url)"
  tries=10
  i=0
  while [[ $i -lt $tries ]]; do
    if curl -fsS -o /dev/null --max-time 5 "$url" 2>/dev/null; then
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  return 1
}

verify_service_up() {
  log "waiting for binflow to become active..."
  if ! wait_service_active; then
    systemctl --no-pager --full status binflow >&2 || true
    journalctl -u binflow --no-pager -n 50 >&2 || true
    die "binflow service did not become active within ${BINFLOW_START_TIMEOUT:-30}s — see the status/journal dump above (config: /etc/binflow/binflow.yaml)"
  fi
  log "service is active"
  if probe_readyz; then
    log "health check OK ($(health_url))"
  else
    warn "health probe failed at $(health_url) (service is active; expected if server.listen is not the default :8080)"
  fi
}

# ---- main ----------------------------------------------------------------------
main() {
  if "$UNINSTALL"; then
    require_root
    do_uninstall
  fi

  require_root

  # Check dependencies early (a dry-run skips this: it exercises none of
  # them, and a preview from a non-Linux workstation must not fail on the
  # missing systemctl binary).
  if "$DRY_RUN"; then
    log "[DRY-RUN] skipping dependency checks"
  else
    for cmd in curl sha256sum systemctl useradd mkdir chown cp tar mktemp grep; do
      require_cmd "$cmd"
    done
  fi

  # Detect platform
  local platform
  platform="$(detect_platform)"
  log "detected platform: ${platform}"

  # Resolve version
  resolve_version
  local archive_name="binflow_${VERSION}_${platform}.tar.gz"
  local checksums_name="binflow_${VERSION}_checksums.txt"
  local archive_url="${BASE_URL}/${VERSION}/${archive_name}"
  local checksums_url="${BASE_URL}/${VERSION}/${checksums_name}"

  # Work in a temporary directory — nothing is written to install paths until
  # checksum verification passes.
  local tmpdir
  tmpdir="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap "rm -rf '${tmpdir}'" EXIT

  log "downloading checksums file..."
  if ! maybe curl -fsSL -o "${tmpdir}/${checksums_name}" "$checksums_url"; then
    die "failed to download checksums file from ${checksums_url}"
  fi

  log "downloading binary archive..."
  if ! maybe curl -fsSL -o "${tmpdir}/${archive_name}" "$archive_url"; then
    die "failed to download binary archive from ${archive_url}"
  fi

  # G16: verify checksum before extracting anything
  verify_checksum "${tmpdir}/${archive_name}" "${tmpdir}/${checksums_name}"

  # Extract archive (skipped for real in a dry-run — nothing was downloaded)
  local extracted_dir="${tmpdir}/binflow_${VERSION}_${platform}"
  if "$DRY_RUN"; then
    log "[DRY-RUN] would extract ${archive_name} and check binflow_${VERSION}_${platform}/binflow-server is present"
  else
    log "extracting ${archive_name}..."
    tar -xzf "${tmpdir}/${archive_name}" -C "$tmpdir"

    # The archive wraps a directory: binflow_<VER>_<os>_<arch>/binflow-server
    if [[ ! -f "${extracted_dir}/binflow-server" ]]; then
      die "binary not found in archive at ${extracted_dir}/binflow-server"
    fi
  fi

  # ---- create system user (idempotent) -----------------------------------------
  if id binflow &>/dev/null; then
    log "user 'binflow' already exists (skipping)"
  else
    log "creating system user 'binflow'..."
    maybe useradd -r -s /usr/sbin/nologin -M -d /var/lib/binflow binflow
  fi

  # ---- create directory layout (idempotent) ------------------------------------
  for dir in /etc/binflow /var/lib/binflow; do
    if [[ -d "$dir" ]]; then
      log "directory '${dir}' already exists (skipping)"
    else
      log "creating directory '${dir}'..."
      maybe mkdir -p "$dir"
    fi
  done

  maybe chown -R binflow:binflow /etc/binflow /var/lib/binflow

  # ---- install binary ----------------------------------------------------------
  log "installing binary to /usr/local/bin/binflow-server..."
  # A running binflow-server holds its binary open: writing the destination
  # in place fails with "Text file busy". Stop the unit for the replacement
  # (a first install has nothing to stop); the enable --now at the end
  # starts it again. The rm-then-cp pair also avoids a partially written
  # binary if cp dies midway.
  if systemctl is-active --quiet binflow 2>/dev/null; then
    log "stopping binflow for binary upgrade..."
    maybe systemctl stop binflow
  fi
  maybe rm -f /usr/local/bin/binflow-server
  maybe mkdir -p /usr/local/bin
  maybe cp "${extracted_dir}/binflow-server" /usr/local/bin/binflow-server
  maybe chmod 755 /usr/local/bin/binflow-server

  # ---- install default config (if not present) ---------------------------------
  if [[ -f /etc/binflow/binflow.yaml ]]; then
    log "config file /etc/binflow/binflow.yaml already exists (skipping)"
  elif [[ -f "${extracted_dir}/config.yaml" ]]; then
    log "installing default config..."
    maybe cp "${extracted_dir}/config.yaml" /etc/binflow/binflow.yaml
  else
    # The release archive carries no config (goreleaser packs binaries +
    # README/LICENSE only), and `serve -c` treats a missing explicit config
    # as a hard error — without this fallback a fresh install would sit in a
    # Restart=on-failure loop. Generate the minimum the unit layout needs
    # (data dir under the service's writable path); every other knob keeps
    # its built-in default. Set the admin password via /etc/binflow/env
    # (BINFLOW_ADMIN_PASSWORD) — secrets never go into the YAML (ADR-0009).
    log "no config in archive — generating minimal default config at /etc/binflow/binflow.yaml"
    write_default_config
  fi

  # ---- install systemd unit ----------------------------------------------------
  # Determine the script directory to locate the unit file
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

  if [[ -f "${script_dir}/binflow.service" ]]; then
    log "installing systemd unit file..."
    maybe cp "${script_dir}/binflow.service" /etc/systemd/system/binflow.service
  elif [[ -f "/etc/systemd/system/binflow.service" ]]; then
    log "systemd unit file already installed (skipping)"
  else
    warn "binflow.service not found alongside install.sh"
    warn "download it manually to /etc/systemd/system/binflow.service"
  fi

  # ---- reload, enable and verify (G15b) ----------------------------------------
  maybe systemctl daemon-reload
  maybe systemctl enable --now binflow

  if "$DRY_RUN"; then
    log "[DRY-RUN] would wait for the service to become active and probe $(health_url)"
  else
    verify_service_up
  fi

  log ""
  log "BinFlow ${VERSION} installed successfully"
  log ""
  log "Service status:  systemctl status binflow"
  log "Health check:    curl -s $(health_url)"
  log "Config:          /etc/binflow/binflow.yaml"
  log "Data:            /var/lib/binflow"
  log "Env overrides:   /etc/binflow/env (optional; carries secrets such as"
  log "                  BINFLOW_ADMIN_PASSWORD and the instance master key"
  log "                  BINFLOW_REMOTE_CREDENTIALS_KEY — root-owned 0600)"
  log "Storage chain:   /etc/binflow/binstore.yaml (optional, M11 — owns the"
  log "                  chain when present; see the unit file header)"
  log ""
  log "Uninstall:       sudo bash install.sh --uninstall"
  log "Uninstall+purge: sudo bash install.sh --uninstall --purge"
}

main