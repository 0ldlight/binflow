#!/bin/bash
# uat-proxy.sh — TLS proxy layer for the BinFlow UAT instance (T-478, user
# directive #19: UAT on 443, HTTPS + ACME domain).
#
# Runs from the CircleCI deploy job (NOT on the target) — same shape as
# uat-deploy.sh: ssh + scp the config, remote leg via `sudo bash -s`.
# Terminates TLS with Caddy in front of BinFlow (127.0.0.1:8080, untouched
# — zero product change; verdict in reports/agents/T-478.md).
#
# Usage: uat-proxy.sh <host> <user> <domain>
#   <domain> "off"/"none"/""  → print-and-skip (transition kill switch:
#                               the plain-HTTP :8080 face keeps serving)
#   default domain            → uat.binflow.org (set UAT_DOMAIN in
#                               CircleCI project env to override)
#
# Idempotent by design — safe on every deploy:
#   - caddy already installed  → apt install is a no-op
#   - config already current   → staged copy + cmp, reload only on change
#   - config changed           → validate FIRST, then reload (a config that
#                                fails validation never reaches the daemon)
#   - never touches binflow-uat.service, the binary, or the data dir
#
# Server contract (additions to deploy/ci/README.md UAT section — all ride
# the already-whitelisted `sudo bash`; no new sudoers entries):
#   - /etc/caddy/Caddyfile          owned here: `import /etc/caddy/caddyfiles/*.caddyfile`
#                                    (dist default backed up once to Caddyfile.dist)
#   - /etc/caddy/caddyfiles/binflow-uat.caddyfile  rendered from deploy/caddy/uat.Caddyfile
#   - ports 80+443 inbound: ufw opened here when active; the AWS security
#     group is OUT of script reach — a one-time user console action
#
# ACME: Let's Encrypt via HTTP-01 (:80) and TLS-ALPN-01 (:443), issuance and
# renewal fully automatic. DNS not propagated yet is NOT fatal — caddy
# retries acquisition with backoff; this script WARNs and stays green so the
# proxy layer can land before the A record (see T-478 report for the
# required ordering).

set -euo pipefail

HOST="$1"; USER_="$2"; DOMAIN="$3"

if [ "${DOMAIN}" = "off" ] || [ "${DOMAIN}" = "none" ] || [ -z "${DOMAIN}" ]; then
    echo "== UAT TLS proxy layer DISABLED (domain '${DOMAIN}') — plain HTTP :8080 only"
    exit 0
fi
case "${DOMAIN}" in
    *.*.*) : ;;
    *) echo "UAT_DOMAIN '${DOMAIN}' does not look like a FQDN — refusing"; exit 2 ;;
esac

# Transition guard (the e01d59d2 lesson): deploying the TLS layer before
# the DNS A record exists strands the pipeline — ACME cannot even attempt
# a challenge and the https probes have nothing to resolve. NXDOMAIN
# skips the layer with a loud warning (plain :8080 keeps serving; the
# next deploy after DNS lands enables TLS), instead of failing the job.
if ! getent hosts "${DOMAIN}" >/dev/null 2>&1; then
    echo "WARN: ${DOMAIN} does not resolve yet (no DNS A record?) —" >&2
    echo "      skipping the TLS proxy layer this run; plain HTTP :8080 stands." >&2
    echo "      Add the A record -> ${HOST} and the next deploy enables https." >&2
    exit 0
fi

# Offer EXACTLY the injected deploy key (uat-deploy.sh semantics — see the
# MaxAuthTries note there). Same wide pattern: any id_<type>_<name> (the
# B64 path materializes id_ed25519_uat; add_ssh_keys writes
# id_<type>_<fingerprint>), .pub excluded, and NO soft fallback — an
# ssh without -i offers the wrong keys and dies on publickey (the
# e01d59d2 first run).
DEPLOY_KEY="$(ls "${HOME}/.ssh/id_"*_* 2>/dev/null | grep -v '\.pub$' | head -1 || true)"
if [ -z "${DEPLOY_KEY}" ]; then
    echo "ERROR: no UAT deploy key found in ~/.ssh/ (uat-proxy)." >&2
    echo "  The 'Install UAT SSH deploy key' step must run before this one." >&2
    ls -la "${HOME}/.ssh/" 2>/dev/null >&2 || echo "  (no ~/.ssh)" >&2
    exit 1
fi
KEY_OPTS=(-o IdentitiesOnly=yes -i "${DEPLOY_KEY}")
SSH="ssh -o StrictHostKeyChecking=accept-new ${KEY_OPTS[*]} ${USER_}@${HOST}"

echo "== UAT TLS proxy ${USER_}@${HOST} domain=${DOMAIN}"

scp -o StrictHostKeyChecking=accept-new "${KEY_OPTS[@]}" \
    deploy/caddy/uat.Caddyfile \
    "${USER_}@${HOST}:/tmp/binflow-uat.Caddyfile"

${SSH} "sudo bash -s -- ${DOMAIN}" <<'REMOTE'
set -euo pipefail
DOMAIN="$1"
UNIT=caddy
FRAG_DIR=/etc/caddy/caddyfiles
FRAG="${FRAG_DIR}/binflow-uat.caddyfile"

# 1. Caddy install (official stable apt channel — cloudsmith; the distro's
#    caddy is the documented fallback). Both steps are no-ops when present.
if ! command -v caddy >/dev/null 2>&1; then
    apt-get update -qq
    apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl gnupg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
        | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
        > /etc/apt/sources.list.d/caddy-stable.list
    apt-get update -qq
    apt-get install -y -qq caddy
else
    echo "caddy already installed: $(caddy version | awk '{print $1}')"
fi

# 2. Render the fragment ({{DOMAIN}} substituted; the staged copy makes the
#    change visible and the swap atomic).
mkdir -p "${FRAG_DIR}"
sed "s/{{DOMAIN}}/${DOMAIN}/g" /tmp/binflow-uat.Caddyfile > "${FRAG}.new"
if [ ! -f "${FRAG}" ] || ! cmp -s "${FRAG}.new" "${FRAG}"; then
    install -m 0644 "${FRAG}.new" "${FRAG}"
    CONFIG_CHANGED=1
else
    rm -f "${FRAG}.new"
    CONFIG_CHANGED=0
fi

# 3. Own the root Caddyfile: a pure import shim (single-purpose proxy host;
#    the dist demo default is kept aside once, never restored automatically).
if [ ! -f /etc/caddy/Caddyfile.dist ] && [ -f /etc/caddy/Caddyfile ]; then
    cp /etc/caddy/Caddyfile /etc/caddy/Caddyfile.dist
fi
printf 'import /etc/caddy/caddyfiles/*.caddyfile\n' > /etc/caddy/Caddyfile.new
if [ ! -f /etc/caddy/Caddyfile ] || ! cmp -s /etc/caddy/Caddyfile.new /etc/caddy/Caddyfile; then
    install -m 0644 /etc/caddy/Caddyfile.new /etc/caddy/Caddyfile
    CONFIG_CHANGED=1
fi
rm -f /etc/caddy/Caddyfile.new /tmp/binflow-uat.Caddyfile

# 4. Validate BEFORE anything reaches the running daemon.
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null \
    || { echo "CADDY VALIDATE FAILED"; exit 1; }

# 5. Reload (running) or enable+start (first provisioning). Reload is
#    graceful: never drops the :8080-direct face, and on the https face
#    existing streams drain.
if systemctl is-active --quiet "${UNIT}"; then
    if [ "${CONFIG_CHANGED}" = 1 ]; then systemctl reload "${UNIT}";
    else echo "caddy config unchanged — no reload"; fi
else
    systemctl enable --now "${UNIT}"
fi

# 6. Firewall (host side only): open 80+443 when ufw is active; a disabled
#    ufw needs nothing (AWS SG is the user-side prerequisite, see report).
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q '^Status: active'; then
    ufw allow 80,443/tcp >/dev/null
    echo "ufw: 80,443/tcp allowed"
fi

# 7. Probes.
#    a. The direct backend plane MUST stay green (we never touch it, but
#       the deploy gate asserts it): localhost :8080 healthz.
curl -fsS -m 5 http://127.0.0.1:8080/healthz >/dev/null \
    || { echo "BACKEND PLANE BROKEN: 127.0.0.1:8080/healthz failed"; exit 1; }
echo "backend plane: 127.0.0.1:8080 healthy"

#    b. Listener face: caddy must hold :80 and :443 regardless of DNS.
LISTEN="$(ss -ltn 2>/dev/null || true)"
for PORT in 80 443; do
    printf '%s' "${LISTEN}" | grep -Eq "[:.]${PORT}[[:space:]]" \
        || { echo "caddy NOT listening on :${PORT}"; exit 1; }
done
echo "listeners: :80 and :443 held by caddy"

#    c. https face: strict once DNS resolves (cert + healthz through the
#       proxy); before that, WARN-only — caddy retries ACME with backoff and
#       picks the cert up once the A record lands. Edge case the first
#       post-DNS deploy can hit: caddy still deep in a FAILED-acquisition
#       backoff from the pre-DNS days — a restart flushes the backoff and
#       re-acquires immediately, so the probe self-heals ONCE (bounded
#       60s) before going red.
if getent hosts "${DOMAIN}" >/dev/null 2>&1; then
    https_probe() {
        curl -fsS -m 20 -o /dev/null "https://${DOMAIN}/healthz"
    }
    if ! https_probe; then
        echo "https probe failed on first try — flushing ACME backoff (restart caddy) and retrying (<=60s)"
        systemctl restart caddy
        ok=0
        for _ in $(seq 1 12); do
            sleep 5
            if https_probe; then ok=1; break; fi
        done
        [ "${ok}" = 1 ] \
            || { echo "HTTPS PROBE FAILED: https://${DOMAIN}/healthz (DNS resolves — check cert issuance: journalctl -u caddy -n 50; and SG :80/:443)"; exit 1; }
    fi
    echo "https face: https://${DOMAIN}/healthz -> 200"
    # Hardening check (informational): the :80 face must redirect, not serve.
    RC="$(curl -sS -m 10 -o /dev/null -w '%{http_code}' "http://${DOMAIN}/healthz" || true)"
    case "${RC}" in
        308) echo "http://${DOMAIN} -> 308 redirect to https confirmed" ;;
        *)   echo "note: http://${DOMAIN}/healthz -> ${RC} (caddy default is 308; investigate if not)" ;;
    esac
else
    echo "WARN: ${DOMAIN} does not resolve yet — ACME deferred (caddy retries with backoff); add the A record, then: sudo systemctl restart caddy"
fi

echo "proxy layer provisioned for ${DOMAIN}"
REMOTE

echo "== UAT TLS proxy GREEN (${DOMAIN})"
