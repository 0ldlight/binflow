#!/bin/bash
# deploy-vm.sh — host-side BinFlow continuous-deploy engine (T-298).
#
# Target environment: the systemd BinFlow test instance on VM
# 172.16.58.129 (unit `binflow`, :8080, hardened T-138 unit —
# ProtectSystem=strict, ReadWritePaths=/var/lib/binflow).
#
# This script runs ON THE VM HOST as root. It is invoked from the Jenkins
# agent container through a small privileged chroot helper (the agent already
# holds the host docker socket, so this adds no new privilege — it only
# exercises it deliberately):
#
#   docker run --rm --privileged --pid=host --network host \
#     binflow-t298-hostctl:alpine \
#     nsenter -t 1 -m -u -i -n -- /bin/bash /srv/jenkins/t298/deploy-vm.sh <mode> [args]
#
# nsenter (not chroot): a plain chroot cannot reach the host systemd bus
# ("Failed to connect to bus"), so the helper enters host PID 1's mount/
# uts/ipc/net namespaces and runs entirely on host binaries. --network host
# + the host mount namespace also fix the T-248 pitfall-7 probe problem:
# 127.0.0.1:8080 below is the real BinFlow.
#
# Modes:
#   status                       is-active + live version + backup inventory
#   deploy <binary> <label>      staged swap: backup -> stop -> replace ->
#                                start -> bounded readiness probe (60s);
#                                auto-rollback + re-probe on failure
#   rollback                     restore the newest backup (Jenkins
#                                smoke-failure path), restart, re-probe
#
# DATA DISCIPLINE: the script NEVER touches /var/lib/binflow (data), never
# edits the unit file or /etc/binflow — it only swaps the binary. The docs
# site and console ride INSIDE the binary (go:embed, T-89/T-129), so one
# artifact carries everything and rollback is atomic for code+console+docs.
#
# Backups: /srv/binflow-backups/binflow-server.<timestamp> — same root
# filesystem as /usr/local/bin, so the final swap is an atomic rename and
# a rollback never sees a half-written binary. Newest KEEP are retained.
# Audit trail: /srv/binflow-backups/deploy.log (append; trimmed at 1MB).
#
# Exit codes: 0 = ok (or rollback succeeded);
#             42 = deploy probe failed AND rollback restored the old binary;
#             43 = rollback itself failed — the service may be DOWN;
#             1  = usage / precondition error.
set -u

UNIT=binflow
BIN=/usr/local/bin/binflow-server
BACKUP_DIR=/srv/binflow-backups
LOG="$BACKUP_DIR/deploy.log"
KEEP=5
ADDR=http://127.0.0.1:8080
PING="$ADDR/binflow/api/system/ping"
VERSION="$ADDR/binflow/api/system/version"

log() { echo "[$(date -Is)] $*" | tee -a "$LOG"; }

mkdir -p "$BACKUP_DIR"
# Keep the audit log bounded (trim to the newest 2000 lines past 1MB).
if [ -f "$LOG" ] && [ "$(wc -c <"$LOG")" -gt 1048576 ]; then
  tail -n 2000 "$LOG" >"$LOG.tail" && mv -f "$LOG.tail" "$LOG"
fi

# probe <seconds> — bounded retry against /binflow/api/system/ping.
probe() {
  local deadline=$((SECONDS + $1))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if curl -sf -o /dev/null --max-time 3 "$PING"; then return 0; fi
    sleep 2
  done
  return 1
}

version_now() { curl -s --max-time 3 "$VERSION" 2>/dev/null | tr -d '\n \t'; }

snapshot() {
  echo "--- systemctl is-active: $(systemctl is-active "$UNIT" 2>&1)"
  systemctl status "$UNIT" --no-pager -n 0 2>&1 | sed -n '1,3p'
  echo "--- live version: $(version_now)"
}

# install_binary <source> — copy to a sibling temp then atomic rename.
install_binary() {
  local src=$1
  cp -- "$src" /usr/local/bin/.binflow-server.incoming
  chown root:root /usr/local/bin/.binflow-server.incoming
  chmod 0755 /usr/local/bin/.binflow-server.incoming
  mv -f /usr/local/bin/.binflow-server.incoming "$BIN"
}

# restore_newest — put the newest backup back, restart, re-probe.
# Returns 0 when the restored service answers; exits 43 otherwise.
restore_newest() {
  local latest
  latest=$(ls -1t "$BACKUP_DIR"/binflow-server.* 2>/dev/null | head -1 || true)
  if [ -z "$latest" ]; then
    log "ROLLBACK impossible: no backup present"
    exit 43
  fi
  log "ROLLBACK: restoring $(basename "$latest") (sha256 $(sha256sum "$latest" | cut -c1-16)…)"
  systemctl stop "$UNIT" 2>/dev/null || true
  install_binary "$latest"
  systemctl start "$UNIT"
  if probe 30; then
    log "ROLLBACK OK: active=$(systemctl is-active "$UNIT") version=$(version_now)"
    snapshot >>"$LOG" 2>&1
    return 0
  fi
  log "ROLLBACK PROBE FAILED — service may be down, needs a human"
  exit 43
}

case "${1:-}" in
status)
  echo "=== binflow test-env status ($(date -Is)) ==="
  snapshot
  echo "--- backups (newest first, keep $KEEP):"
  ls -1t "$BACKUP_DIR"/binflow-server.* 2>/dev/null | sed "s|^|  |" | head -n "$KEEP"
  echo "--- last 5 deploy.log lines:"
  tail -n 5 "$LOG" 2>/dev/null | sed 's/^/  /'
  exit 0
  ;;

deploy)
  new=${2:-}
  label=${3:-}
  if [ -z "$new" ] || [ -z "$label" ] || [ ! -f "$new" ]; then
    echo "usage: $0 deploy <staged-binary> <version-label>" >&2
    exit 1
  fi
  log "=== DEPLOY START label=$label staged=$new size=$(wc -c <"$new" | tr -d ' ') sha256=$(sha256sum "$new" | cut -c1-16)…"
  log "PRE-DEPLOY:"; snapshot >>"$LOG" 2>&1; snapshot

  # 1. Backup the current binary (timestamped), prune to the newest KEEP.
  ts=$(date +%Y%m%d-%H%M%S)
  if [ -f "$BIN" ]; then
    cp -a -- "$BIN" "$BACKUP_DIR/binflow-server.$ts"
    log "backup: $BACKUP_DIR/binflow-server.$ts (sha256 $(sha256sum "$BACKUP_DIR/binflow-server.$ts" | cut -c1-16)…)"
    ls -1t "$BACKUP_DIR"/binflow-server.* 2>/dev/null | tail -n +"$((KEEP + 1))" | xargs -r rm -f --
  else
    log "no current binary to back up (fresh install path)"
  fi

  # 2. Stop -> swap -> reload -> start. Only the binary moves; the data
  #    directory, the unit and /etc/binflow are deliberately untouched.
  systemctl stop "$UNIT"
  install_binary "$new"
  systemctl daemon-reload
  systemctl start "$UNIT"

  # 3. Bounded readiness probe (60s ceiling on /api/system/ping).
  if probe 60; then
    log "POST-DEPLOY:"; snapshot >>"$LOG" 2>&1; snapshot
    log "=== DEPLOY OK label=$label"
    exit 0
  fi

  # 4. Probe failed -> automatic rollback to the backup we just took.
  log "PROBE FAILED (60s) after swap — auto-rollback"
  systemctl status "$UNIT" --no-pager -n 20 >>"$LOG" 2>&1 || true
  journalctl -u "$UNIT" -n 30 --no-pager >>"$LOG" 2>&1 || true
  restore_newest
  log "=== DEPLOY FAILED, ROLLED BACK (label=$label)"
  exit 42
  ;;

rollback)
  log "=== EXPLICIT ROLLBACK requested (post-smoke failure path)"
  restore_newest
  exit 0
  ;;

*)
  echo "usage: $0 {status|deploy <binary> <label>|rollback}" >&2
  exit 1
  ;;
esac
