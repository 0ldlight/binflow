#!/usr/bin/env bash
# install.sh — install BinFlow via systemd on a Linux host (T-138, FR-39, PB-07).
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
#   # Dry-run: print what would be done without making changes:
#   sudo bash install.sh --dry-run
#
#   # Uninstall (see --uninstall section at the bottom):
#   sudo bash install.sh --uninstall
#
# Dependencies (standard on any systemd-based Linux):
#   bash, curl, sha256sum, systemctl, useradd, mkdir, chown, cp, ln
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
  --dry-run        Print what would be done, do not make changes
  --uninstall      Stop, disable, and remove the BinFlow service
  --purge          With --uninstall: also remove the binflow user and data
  --help           Show this help text

Environment:
  BINFLOW_VERSION   Default version (overridden by --version)
  BINFLOW_BASE_URL  Default download base URL (overridden by --base-url)

Examples:
  sudo bash install.sh
  sudo bash install.sh --version v1.0.0
  sudo bash install.sh --base-url https://releases.example.com/artifacts
  sudo bash install.sh --dry-run --version v1.0.0
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
    *) die "unsupported OS: $os (this script is for Linux with systemd)" ;;
  esac

  echo "${os}_${arch}"
}

# ---- version resolution --------------------------------------------------------
resolve_version() {
  if [[ "$VERSION" == "latest" ]]; then
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

  log "verifying checksum for ${archive_file}"

  # Extract the expected checksum line for our archive from the checksums file.
  # goreleaser produces lines like:
  #   <sha256>  binflow_<VER>_<os>_<arch>.tar.gz
  local archive_basename
  archive_basename="$(basename "$archive_file")"

  if ! grep -qF "  ${archive_basename}" "$checksums_file"; then
    die "checksums file does not contain an entry for ${archive_basename}"
  fi

  if ! sha256sum -c --status <(grep -F "  ${archive_basename}" "$checksums_file"); then
    # Run again without --status to show the mismatch details
    sha256sum -c <(grep -F "  ${archive_basename}" "$checksums_file") >&2 || true
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

# ---- main ----------------------------------------------------------------------
main() {
  if "$UNINSTALL"; then
    require_root
    do_uninstall
  fi

  require_root

  # Check dependencies early
  for cmd in curl sha256sum systemctl useradd mkdir chown cp; do
    require_cmd "$cmd"
  done

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

  # Extract archive
  log "extracting ${archive_name}..."
  maybe tar -xzf "${tmpdir}/${archive_name}" -C "$tmpdir"

  # The archive wraps a directory: binflow_<VER>_<os>_<arch>/binflow-server
  local extracted_dir="${tmpdir}/binflow_${VERSION}_${platform}"

  if [[ ! -f "${extracted_dir}/binflow-server" ]]; then
    die "binary not found in archive at ${extracted_dir}/binflow-server"
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
  maybe mkdir -p /usr/local/bin
  maybe cp "${extracted_dir}/binflow-server" /usr/local/bin/binflow-server
  maybe chmod 755 /usr/local/bin/binflow-server

  # ---- install default config (if not present) ---------------------------------
  if [[ -f /etc/binflow/binflow.yaml ]]; then
    log "config file /etc/binflow/binflow.yaml already exists (skipping)"
  else
    log "installing default config..."
    if [[ -f "${extracted_dir}/config.yaml" ]]; then
      maybe cp "${extracted_dir}/config.yaml" /etc/binflow/binflow.yaml
    else
      warn "no default config found in archive — skipping config install"
      warn "create /etc/binflow/binflow.yaml manually before starting the service"
    fi
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

  # ---- reload and enable -------------------------------------------------------
  maybe systemctl daemon-reload
  maybe systemctl enable --now binflow

  log ""
  log "BinFlow ${VERSION} installed successfully"
  log ""
  log "Service status:  systemctl status binflow"
  log "Health check:    curl -s http://127.0.0.1:8080/readyz"
  log "Config:          /etc/binflow/binflow.yaml"
  log "Data:            /var/lib/binflow"
  log ""
  log "Uninstall:       sudo bash install.sh --uninstall"
  log "Uninstall+purge: sudo bash install.sh --uninstall --purge"
}

main