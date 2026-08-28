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

# Offer EXACTLY the injected deploy key. Without IdentitiesOnly the ssh
# agent offers every key it holds first — the server's MaxAuthTries then
# rejects the connection before the right key gets its turn, which made
# deploys fail intermittently (builds #8/#10/#12 vs green #9/#11).
# Match id_<type>_<fingerprint> (any key type: rsa, ed25519, ecdsa…)
# but NOT the bare checkout key (id_rsa / id_ed25519 with no fingerprint).
DEPLOY_KEY="$(ls "${HOME}/.ssh/id_"*_* 2>/dev/null | grep -v '\.pub$' | head -1 || true)"
if [ -z "${DEPLOY_KEY}" ]; then
    echo "ERROR: no UAT deploy key found in ~/.ssh/." >&2
    echo "  Fix option A: set UAT_SSH_KEY_B64 in CircleCI project env vars (base64-encoded private key)." >&2
    echo "  Fix option B: upload the key via CircleCI Project Settings → SSH Keys." >&2
    echo "  Current ~/.ssh/ contents:" >&2
    ls -la "${HOME}/.ssh/" 2>/dev/null >&2 || echo "  (empty or missing)" >&2
    exit 1
fi
echo "deploy key: ${DEPLOY_KEY}"
KEY_OPTS=(-o IdentitiesOnly=yes -i "${DEPLOY_KEY}")
SSH="ssh -o StrictHostKeyChecking=accept-new ${KEY_OPTS[*]} ${USER_}@${HOST}"

echo "== UAT deploy ${LABEL} -> ${USER_}@${HOST}:${HOME_}"

# 1. Stage the fresh binary and pick up the live version for the audit log.
scp -o StrictHostKeyChecking=accept-new "${KEY_OPTS[@]}" bin/binflow-server \
    "${USER_}@${HOST}:${HOME_}/binflow-server.new"
${SSH} "systemctl show ${UNIT} -p ActiveState -p ExecMainStartTimestamp || true"

# 2. Staged swap with backup + bounded probe + auto-rollback.
#    (T-325 fix: the heredoc-fed `sudo bash -s` originally read $1/$2 which
#    were ALWAYS empty — no arguments were passed and the script died at
#    `cd ""`. Arguments now ride the command line after `--`.)
${SSH} "sudo bash -s -- ${HOME_} ${LABEL}" <<'REMOTE'
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

# 3. Post-deploy smoke: binary face + version stamp + embedded docs face.
#    (T-325: version assertion added — same "proves the swap actually took"
#    gate the Jenkins chain has; requires the version stamp in the CircleCI
#    build job, added in the same change.)
v=$(${SSH} "curl -fsS -m 5 http://127.0.0.1:8080/binflow/api/system/version")
echo "version: ${v}"
case "${v}" in *"${LABEL}"*) ;; *)
    echo "VERSION MISMATCH: expected ${LABEL}, got ${v}"; exit 1 ;;
esac
${SSH} "curl -fsS -m 5 -o /dev/null -w 'docs:%{http_code}\n' http://127.0.0.1:8080/binflow/docs/"
echo "== UAT deploy ${LABEL} GREEN"
