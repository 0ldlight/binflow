#!/bin/bash
# uat-deploy.sh — CircleCI-side deploy for the BinFlow UAT environment
# (BOARD 2026-08-26 21:10 directive: CircleCI -> 52.79.109.153, docs
# service included).
#
# Runs from the CircleCI deploy job (NOT on the target). The binary is a
# single artifact carrying the embedded docs site (T-298 posture), so
# shipping one file deploys both the console and the docs service.
#
# Usage: uat-deploy.sh <host> <user> <remote-home> <label>
#
# Server contract (provision once, see deploy/ci/README.md UAT section):
#   - the SSH key added to CircleCI authorizes <user>@<host>
#   - <user> has passwordless sudo for: systemctl restart/stop/start
#     binflow-uat, install, mkdir
#   - systemd unit binflow-uat listens on :8080 with data under
#     <remote-home>/data; config at <remote-home>/binflow.yaml
#   - backups keep the last 5 binaries (same retention as the VM chain)
#
# Safety: never touches the data directory; failed readiness probe
# auto-rolls back to the newest backup and re-probes (deploy-vm.sh
# semantics, minus the nsenter plumbing — we ssh directly).

set -euo pipefail

HOST="$1"; USER_="$2"; HOME_="$3"; LABEL="$4"
UNIT="binflow-uat"
SSH="ssh -o StrictHostKeyChecking=accept-new ${USER_}@${HOST}"

echo "== UAT deploy ${LABEL} -> ${USER_}@${HOST}:${HOME_}"

# 1. Stage the fresh binary and pick up the live version for the audit log.
scp -o StrictHostKeyChecking=accept-new bin/binflow-server \
    "${USER_}@${HOST}:${HOME_}/binflow-server.new"
${SSH} "systemctl show ${UNIT} -p ActiveState -p ExecMainStartTimestamp || true"

# 2. Staged swap with backup + bounded probe + auto-rollback.
${SSH} "sudo bash -s" <<'REMOTE'
set -euo pipefail
UNIT=binflow-uat; HOME_DIR="$1"; LABEL="$2"
cd "${HOME_DIR}"
[ -x binflow-server.new ]
mkdir -p backups
if [ -x binflow-server ]; then
    cp binflow-server "backups/binflow-server.$(date +%Y%m%d%H%M%S)"
    ls -1t backups/binflow-server.* | tail -n +6 | xargs -r rm --
fi
systemctl stop "${UNIT}"
install -m 0755 binflow-server.new binflow-server
systemctl start "${UNIT}"
ok=0
for _ in $(seq 1 30); do
    sleep 2
    if curl -fsS -m 3 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then ok=1; break; fi
done
if [ "${ok}" != 1 ]; then
    echo "PROBE-FAILED — rolling back"
    NEWEST=$(ls -1t backups/binflow-server.* 2>/dev/null | head -1 || true)
    if [ -n "${NEWEST}" ]; then
        systemctl stop "${UNIT}"
        install -m 0755 "${NEWEST}" binflow-server
        systemctl start "${UNIT}"
        sleep 3
        curl -fsS -m 3 http://127.0.0.1:8080/healthz
    fi
    exit 1
fi
echo "deployed ${LABEL}"
REMOTE
# shellcheck disable=SC2034
REMOTE_ARGS="${HOME_} ${LABEL}"

# 3. Post-deploy smoke: binary face + embedded docs face both answer.
${SSH} "curl -fsS -m 5 http://127.0.0.1:8080/healthz"
${SSH} "curl -fsS -m 5 -o /dev/null -w 'docs:%{http_code}\n' http://127.0.0.1:8080/binflow/docs/"
echo "== UAT deploy ${LABEL} GREEN"
