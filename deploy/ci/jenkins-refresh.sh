#!/bin/bash
# jenkins-refresh.sh — push the local Jenkinsfile-lzw into the inline job
# config and trigger a build. Replaces /tmp/bf_jenkins_refresh.sh: reads
# config.xml straight from Jenkins instead of a local XML snapshot, so the
# protocol survives /tmp cleanup. Requires .env.jenkins (JENKINS_URL,
# JENKINS_API_TOKEN) in the repo root. Run relay-sync.sh first if source
# bytes changed.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
. ./.env.jenkins
AUTH="admin:$JENKINS_API_TOKEN"
BASE="$JENKINS_URL/job/binflow/job/binflow-dev-k8s"

curl -sS -m 30 -u "$AUTH" "$BASE/config.xml" -o /tmp/bf-config.xml

python3 - <<'PYEOF'
import html, re
script = open('deploy/ci/Jenkinsfile-lzw').read()
escaped = html.escape(script, quote=False)
escaped = ''.join(c if ord(c) < 128 else '&#x%04X;' % ord(c) for c in escaped)
cfg = open('/tmp/bf-config.xml').read()
cfg = re.sub(r'<script>.*</script>', lambda m: '<script>' + escaped + '</script>', cfg, flags=re.S)
cfg.encode('ascii')
open('/tmp/bf-config.xml', 'w').write(cfg)
print('inline script replaced')
PYEOF

CODE=$(curl -sS -m 20 -u "$AUTH" -X POST "$BASE/config.xml" \
  -H "Content-Type: application/xml" \
  --data-binary @/tmp/bf-config.xml -o /tmp/bf-cfg-resp.html -w '%{http_code}')
echo "config POST -> $CODE"
[ "$CODE" = "200" ] || { head -c 300 /tmp/bf-cfg-resp.html; exit 1; }
sleep 5
CODE=$(curl -sS -m 15 -u "$AUTH" -X POST "$BASE/build" -o /dev/null -w '%{http_code}')
echo "trigger -> $CODE"
[ "$CODE" = "201" ]
