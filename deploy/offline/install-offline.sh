#!/usr/bin/env bash
# install-offline.sh — BinFlow offline installation script (T-139, FR-40, PB-08).
#
# This script installs BinFlow from an offline bundle in an air-gapped
# (no-internet) environment.  It supports three installation modes:
#
#   docker-compose — docker load → compose up (default, simplest)
#   kind          — docker load → kind import → docker compose / kubectl apply
#   helm          — docker load → helm install (requires a running K8s cluster)
#
# Usage:
#   ./install-offline.sh                           # interactive — pick a mode
#   ./install-offline.sh --compose                 # docker-compose mode
#   ./install-offline.sh --kind                    # kind (K8s-in-Docker) mode
#   ./install-offline.sh --helm                    # helm install mode
#   ./install-offline.sh --dry-run                 # print what would happen, do nothing
#   ./install-offline.sh --help                    # show this message
#
# Pre-requisites (mode-dependent):
#   docker-compose — docker, docker compose
#   kind          — docker, kind, kubectl
#   helm          — docker, kubectl, helm (cluster already running)
#
# The bundle structure (all paths relative to the script's directory):
#   images/binflow-<VER>-alpine.tar           docker save of alpine image
#   images/binflow-<VER>-distroless.tar       docker save of distroless image
#   charts/binflow-<VER>.tgz                  helm package
#   k8s/                                      K8s manifests
#   compose/                                  docker-compose files for --compose mode
#   binaries/                                 linux amd64 + arm64 binaries + checksums
#   README-offline.md                         usage instructions
#   SHA256SUMS                                checksums of all bundle files
#
# Idempotent: re-running the same mode is safe — existing images are not
# re-loaded, existing compose stacks are not re-created.
# Fail-clean: on failure, partial state is removed.

set -euo pipefail

# ---- constants ---------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_DIR="$SCRIPT_DIR"
IMAGES_DIR="$BUNDLE_DIR/images"
CHARTS_DIR="$BUNDLE_DIR/charts"
K8S_DIR="$BUNDLE_DIR/k8s"
CHECKSUMS_FILE="$BUNDLE_DIR/SHA256SUMS"

# Default version — extracted from the first image tar found in images/.
VER=""
# Installation mode — set by flag parsing.
MODE=""
DRY_RUN=false

# Cleanup tracking — we record what we created so we can roll back on failure.
CREATED_IMAGES=()
CREATED_COMPOSE=false
CREATED_HELM=false
CREATED_K8S=false

# ---- colour helpers ----------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_fail()  { log_error "$@"; exit 1; }

# ---- cleanup (called on failure) ---------------------------------------------
cleanup() {
  local exit_code=$?
  if [ "$exit_code" -eq 0 ]; then
    return 0
  fi
  log_warn "Installation failed (exit $exit_code) — rolling back partial state…"

  # Remove images we loaded. The length guard keeps `set -u` happy on bash
  # 3.2 (macOS system bash): expanding an empty "${arr[@]}" is an unbound
  # variable error there (T-376: a dry-run failure path hit exactly this).
  if [ "${#CREATED_IMAGES[@]}" -gt 0 ]; then
    for img in "${CREATED_IMAGES[@]}"; do
      if docker image inspect "$img" &>/dev/null; then
        log_info "Removing image: $img"
        docker rmi "$img" || true
      fi
    done
  fi

  # Tear down compose stack if we started it.
  if [ "$CREATED_COMPOSE" = true ]; then
    log_info "Stopping compose stack…"
    docker compose -f "$BUNDLE_DIR/compose/docker-compose.yml" --profile nginx down 2>/dev/null || true
  fi

  # Uninstall helm release if we installed it.
  if [ "$CREATED_HELM" = true ]; then
    log_info "Uninstalling helm release…"
    helm uninstall binflow 2>/dev/null || true
  fi

  # Delete k8s resources if we created them.
  if [ "$CREATED_K8S" = true ]; then
    log_info "Deleting k8s resources…"
    kubectl delete -k "$K8S_DIR" 2>/dev/null || true
  fi

  log_info "Rollback complete."
}
trap cleanup EXIT

# ---- usage -------------------------------------------------------------------
usage() {
  cat << EOF
Usage: $0 [FLAGS]

Flags:
  --compose     Install via docker compose (default)
  --kind        Install via kind (K8s in Docker)
  --helm        Install via helm (requires existing K8s cluster)
  --dry-run     Print what would happen, do nothing
  --help        Show this help message

Bundle: $BUNDLE_DIR
EOF
  exit 0
}

# ---- parse flags -------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --compose)  MODE=compose; shift ;;
    --kind)     MODE=kind; shift ;;
    --helm)     MODE=helm; shift ;;
    --dry-run)  DRY_RUN=true; shift ;;
    --help)     usage ;;
    -*)         log_fail "Unknown flag: $1 (use --help)" ;;
    *)          log_fail "Unexpected positional argument: $1 (use --help)" ;;
  esac
done

# ---- verify checksums --------------------------------------------------------
verify_checksums() {
  if [ ! -f "$CHECKSUMS_FILE" ]; then
    log_fail "SHA256SUMS not found at $CHECKSUMS_FILE — bundle is incomplete (G17b)."
  fi
  log_info "Verifying bundle checksums…"
  if $DRY_RUN; then
    log_info "  [DRY-RUN] cd $BUNDLE_DIR && shasum -a 256 -c SHA256SUMS"
    return 0
  fi
  pushd "$BUNDLE_DIR" > /dev/null
  if shasum -a 256 -c SHA256SUMS; then
    log_info "All checksums verified."
  else
    popd > /dev/null
    log_fail "Checksum verification failed — bundle may be corrupt."
  fi
  popd > /dev/null
}

# ---- detect version from image files -----------------------------------------
detect_version() {
  local img_file
  img_file=$(find "$IMAGES_DIR" -maxdepth 1 -name 'binflow-*-alpine.tar' -print 2>/dev/null | head -1 || true)
  if [ -z "$img_file" ]; then
    log_fail "No image tar found in $IMAGES_DIR — bundle is incomplete."
  fi
  VER="$(basename "$img_file" | sed 's/^binflow-//' | sed 's/-alpine\.tar$//')"
  log_info "Detected version: $VER"
}

# ---- load docker images ------------------------------------------------------
load_images() {
  log_info "Loading docker images…"
  local alpine_tar="$IMAGES_DIR/binflow-${VER}-alpine.tar"
  local distroless_tar="$IMAGES_DIR/binflow-${VER}-distroless.tar"

  if [ ! -f "$alpine_tar" ]; then
    log_fail "Alpine image tar not found: $alpine_tar"
  fi
  if [ ! -f "$distroless_tar" ]; then
    log_fail "Distroless image tar not found: $distroless_tar"
  fi

  local alpine_img="binflow:${VER}-alpine"
  local distroless_img="binflow:${VER}-distroless"

  for pair in "$alpine_tar:$alpine_img" "$distroless_tar:$distroless_img"; do
    local tar_file="${pair%%:*}"
    local img_name="${pair##*:}"

    if docker image inspect "$img_name" &>/dev/null; then
      log_info "Image already loaded: $img_name"
      continue
    fi

    if $DRY_RUN; then
      log_info "  [DRY-RUN] docker load -i $tar_file"
      continue
    fi

    log_info "Loading: $img_name"
    docker load -i "$tar_file"
    CREATED_IMAGES+=("$img_name")
  done
}

# ---- docker-compose mode -----------------------------------------------------
install_compose() {
  log_info "=== docker-compose mode ==="

  # Check for docker compose.
  if ! docker compose version &>/dev/null; then
    log_fail "docker compose is required but not found."
  fi

  # Prepare .env file if it doesn't exist.
  local compose_env="$BUNDLE_DIR/compose/.env"
  if [ ! -f "$compose_env" ]; then
    if $DRY_RUN; then
      log_info "  [DRY-RUN] cp $BUNDLE_DIR/compose/.env.example → $compose_env"
    else
      cp "$BUNDLE_DIR/compose/.env.example" "$compose_env"
      # Set a random admin password for the offline install.
      local admin_pass
      admin_pass=$(openssl rand -base64 18 2>/dev/null || echo "binflow-offline-$(date +%s)")
      if grep -q '^BINFLOW_ADMIN_PASSWORD=' "$compose_env" 2>/dev/null; then
        if [[ "$OSTYPE" == "darwin"* ]]; then
          sed -i '' "s/^BINFLOW_ADMIN_PASSWORD=.*/BINFLOW_ADMIN_PASSWORD=${admin_pass}/" "$compose_env"
        else
          sed -i "s/^BINFLOW_ADMIN_PASSWORD=.*/BINFLOW_ADMIN_PASSWORD=${admin_pass}/" "$compose_env"
        fi
      else
        echo "BINFLOW_ADMIN_PASSWORD=${admin_pass}" >> "$compose_env"
      fi
      log_info "Created $compose_env with a generated admin password."
    fi
  fi

  # Start compose stack.
  if $DRY_RUN; then
    log_info "  [DRY-RUN] docker compose -f $BUNDLE_DIR/compose/docker-compose.yml up -d"
    return 0
  fi

  # The readiness probe must follow BINFLOW_BIND_PORT when the operator
  # moved the published port in compose/.env (T-376: a custom port made
  # the hardcoded :8080 probe time out against a healthy stack).
  local bind_port
  bind_port="$(grep -E '^BINFLOW_BIND_PORT=' "$compose_env" 2>/dev/null | tail -1 | cut -d= -f2)"
  bind_port="${bind_port:-8080}"

  log_info "Starting BinFlow compose stack…"
  BINFLOW_VER="$VER" docker compose -f "$BUNDLE_DIR/compose/docker-compose.yml" up -d
  CREATED_COMPOSE=true

  # Wait for health.
  log_info "Waiting for BinFlow to become healthy on port $bind_port (max 30s)…"
  local max_wait=30
  local waited=0
  while [ "$waited" -lt "$max_wait" ]; do
    if curl -sf "http://127.0.0.1:${bind_port}/readyz" > /dev/null 2>&1; then
      log_info "BinFlow is ready (/readyz → 200)."
      break
    fi
    sleep 2
    waited=$((waited + 2))
  done
  if [ "$waited" -ge "$max_wait" ]; then
    log_fail "BinFlow did not become healthy within ${max_wait}s."
  fi

  log_info "Compose installation complete."
  echo ""
  echo "  BinFlow is running at http://127.0.0.1:${bind_port}"
  echo "  Admin password is in $compose_env"
  echo ""
}

# ---- kind mode ---------------------------------------------------------------
install_kind() {
  log_info "=== kind (K8s-in-Docker) mode ==="

  if ! command -v kind &>/dev/null; then
    log_fail "kind is required but not found."
  fi
  if ! command -v kubectl &>/dev/null; then
    log_fail "kubectl is required but not found."
  fi

  local cluster_name="binflow-offline"
  if kind get clusters 2>/dev/null | grep -qx "$cluster_name"; then
    log_info "kind cluster '$cluster_name' already exists."
  else
    if $DRY_RUN; then
      log_info "  [DRY-RUN] kind create cluster --name $cluster_name"
    else
      log_info "Creating kind cluster '$cluster_name'…"
      kind create cluster --name "$cluster_name"
    fi
  fi

  # Import images into kind.
  local alpine_img="binflow:${VER}-alpine"
  local distroless_img="binflow:${VER}-distroless"

  if $DRY_RUN; then
    log_info "  [DRY-RUN] kind load docker-image $alpine_img --name $cluster_name"
    log_info "  [DRY-RUN] kind load docker-image $distroless_img --name $cluster_name"
    log_info "  [DRY-RUN] kubectl apply -k $K8S_DIR"
    return 0
  fi

  log_info "Importing images into kind cluster…"
  kind load docker-image "$alpine_img" --name "$cluster_name"
  kind load docker-image "$distroless_img" --name "$cluster_name"

  # Apply K8s manifests.
  if [ ! -d "$K8S_DIR" ]; then
    log_fail "K8s manifests directory not found: $K8S_DIR"
  fi

  log_info "Applying K8s manifests…"
  kubectl apply -k "$K8S_DIR"
  CREATED_K8S=true

  # Wait for deployment.
  log_info "Waiting for BinFlow deployment to be ready (max 60s)…"
  kubectl wait --for=condition=available --timeout=60s deployment/binflow 2>/dev/null || true

  # Port-forward for verification.
  log_info "Verifying /readyz via port-forward…"
  kubectl port-forward svc/binflow 18080:8080 &
  local pf_pid=$!
  sleep 3
  if curl -sf http://127.0.0.1:18080/readyz > /dev/null 2>&1; then
    log_info "BinFlow is ready (/readyz → 200)."
  else
    kill "$pf_pid" 2>/dev/null || true
    log_fail "BinFlow did not become healthy."
  fi
  kill "$pf_pid" 2>/dev/null || true

  log_info "Kind installation complete."
  echo ""
  echo "  BinFlow is running in kind cluster '$cluster_name'."
  echo "  Access: kubectl port-forward svc/binflow 8080:8080"
  echo "  Then:   http://127.0.0.1:8080"
  echo ""
}

# ---- helm mode ---------------------------------------------------------------
install_helm() {
  log_info "=== helm mode ==="

  if ! command -v helm &>/dev/null; then
    log_fail "helm is required but not found."
  fi
  if ! command -v kubectl &>/dev/null; then
    log_fail "kubectl is required but not found."
  fi

  # Verify kubectl context.
  local ctx
  ctx=$(kubectl config current-context 2>/dev/null || true)
  if [ -z "$ctx" ]; then
    log_fail "No kubectl context found — ensure you have a running K8s cluster."
  fi
  log_info "Using kubectl context: $ctx"

  # Locate the packaged chart. NOT pinned to binflow-${VER}.tgz: `helm
  # package` names the tarball after the CHART version (e.g.
  # binflow-1.5.0.tgz), which diverges from the bundle's image/binary
  # version (T-376: the hardcoded name made helm mode fail on every bundle
  # produced by plain `helm package`). Any single binflow-*.tgz in charts/
  # is the chart; zero or several is a malformed bundle.
  local chart_file
  chart_file="$(find "$CHARTS_DIR" -maxdepth 1 -name 'binflow-*.tgz' -print 2>/dev/null | sort | head -1 || true)"
  if [ -z "$chart_file" ]; then
    log_fail "No binflow-*.tgz chart package found in $CHARTS_DIR — bundle is incomplete."
  fi
  log_info "Using chart package: $chart_file"

  if $DRY_RUN; then
    log_info "  [DRY-RUN] helm upgrade --install binflow $chart_file --set image.repository=binflow --set image.tag=${VER}-distroless"
    return 0
  fi

  # Pin the release to the BUNDLED image (binflow:${VER}-distroless, loaded
  # above). Without this the chart default pulls ghcr.io/lzwzzy/binflow —
  # unreachable by definition in an air-gap (T-376).
  local helm_img_sets=(
    --set "image.repository=binflow"
    --set "image.tag=${VER}-distroless"
  )

  # Install/upgrade.
  if helm status binflow &>/dev/null 2>&1; then
    log_info "Helm release 'binflow' already exists — upgrading."
    helm upgrade binflow "$chart_file" "${helm_img_sets[@]}"
  else
    log_info "Installing helm release 'binflow'…"
    helm install binflow "$chart_file" "${helm_img_sets[@]}"
    CREATED_HELM=true
  fi

  # Wait for deployment.
  log_info "Waiting for BinFlow deployment to be ready (max 60s)…"
  kubectl wait --for=condition=available --timeout=60s deployment/binflow 2>/dev/null || true

  log_info "Helm installation complete."
  helm status binflow
  echo ""
  echo "  Access: kubectl port-forward svc/binflow 8080:8080"
  echo "  Then:   http://127.0.0.1:8080"
  echo ""
}

# ---- main --------------------------------------------------------------------
main() {
  echo ""
  echo "=============================================="
  echo "  BinFlow Offline Installer"
  echo "=============================================="
  echo ""

  if $DRY_RUN; then
    log_info "DRY-RUN mode — no changes will be made."
    echo ""
  fi

  detect_version
  verify_checksums
  load_images

  # Interactive mode selection.
  if [ -z "$MODE" ]; then
    echo ""
    echo "Select installation mode:"
    echo "  1) docker-compose (default)"
    echo "  2) kind (K8s-in-Docker)"
    echo "  3) helm (requires existing K8s cluster)"
    echo ""
    read -r -p "Enter choice [1-3]: " choice
    case "${choice:-1}" in
      1) MODE=compose ;;
      2) MODE=kind ;;
      3) MODE=helm ;;
      *) log_fail "Invalid choice: $choice" ;;
    esac
  fi

  case "$MODE" in
    compose) install_compose ;;
    kind)    install_kind ;;
    helm)    install_helm ;;
    *)       log_fail "Unknown mode: $MODE" ;;
  esac

  echo ""
  log_info "BinFlow offline installation complete!"
  echo ""

  # Disable the failure trap — we succeeded.
  trap - EXIT
}

main "$@"