#!/bin/bash
# L016-1 pypi simple evidence probe — real pip + twine legs, invalid-wheel
# 3-state matrix, 302 chain, legacy JSON API, root-index immediacy.
# Differential-qa-engineer, LOOP 016 track L016-1. Serial; settle 2s between
# arms; 503 gate on the ref (crawl discipline: on 503 -> wait 15s, retry x5).
#
# Usage: pypi-evidence.sh <a|b>     a=Artifactory ref :8082, b=BinFlow UAT :8083
#        pypi-evidence.sh capture   offline sink: record raw twine 7 upload wire
# Env:  ARTI_AUTH / BF_AUTH = "user:password" (never on disk; redacted in logs)
# Out:  reports/compatibility/l016-wire/<side>/{setup,pypi,client}/...
# Fixture source: tools/difftest/l016/make-fixtures.py (real zip/tar dists).
set -u
HERE=/Users/lzw/dev-center/tools/difftest/l016
WIRE=/Users/lzw/dev-center/reports/compatibility/l016-wire
WSROOT=/Users/lzw/dev-center/tools/difftest/l016/ws
FIX=$WSROOT/fixtures
VENV=$WSROOT/venv

# --- offline twine raw-wire capture (no instance involved) --------------------
if [ "${1:-}" = capture ]; then
  mkdir -p "$WIRE/client"; rm -f "$WIRE/client/twine-raw.req"
  python3 - "$WIRE/client/twine-raw.req" <<'EOF' &
import socket, sys, threading
path = sys.argv[1]
srv = socket.socket(); srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("127.0.0.1", 19099)); srv.listen(1)
c, _ = srv.accept()
data = b""
c.settimeout(5)
try:
    while b"\r\n\r\n" not in data:
        data += c.recv(65536)
    head, rest = data.split(b"\r\n\r\n", 1)
    clen = 0
    for line in head.split(b"\r\n"):
        if line.lower().startswith(b"content-length:"):
            clen = int(line.split(b":")[1].strip())
    while len(rest) < clen:
        rest += c.recv(65536)
except Exception as e:
    sys.stderr.write(f"sink read: {e}\n")
open(path, "wb").write(head + b"\r\n\r\n" + rest)
c.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"); c.close(); srv.close()
EOF
  SINK_PID=$!
  sleep 1
  ( cd "$FIX" && "$VENV/bin/twine" upload --verbose --repository-url http://127.0.0.1:19099/ \
      -u admin -p sink-dummy hello16-1.0.0.tar.gz ) >"$WIRE/client/twine-sink.out" 2>&1
  wait $SINK_PID
  # redact Authorization (Basic base64 of admin:sink-dummy)
  python3 - "$WIRE/client/twine-raw.req" <<'EOF'
import base64, re, sys
p = sys.argv[1]; raw = open(p, "rb").read()
b64 = base64.b64encode(b"admin:sink-dummy").decode()
raw = raw.replace(b"Basic " + b64.encode(), b"Basic REDACTED")
open(p, "wb").write(raw)
print("captured+redacted", p, len(raw), "bytes")
EOF
  exit 0
fi

# --- side selection ------------------------------------------------------------
SIDE=${1:?side a|b (or capture)}
if [ "$SIDE" = a ]; then
  HOSTPORT=8082; PREFIX=artifactory; AUTHVAR=ARTI_AUTH
else
  HOSTPORT=8083; PREFIX=binflow;   AUTHVAR=BF_AUTH
fi
AUTH=${!AUTHVAR:-}
[ -n "$AUTH" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }
PW=${AUTH#*:}

BASE=http://localhost:$HOSTPORT/$PREFIX
PYPI_API=$BASE/api/pypi/l016-pypi
SIMPLE=$PYPI_API/simple
PIP_INDEX="http://${AUTH}@localhost:${HOSTPORT}/${PREFIX}/api/pypi/l016-pypi/simple"
OUT=$WIRE/$SIDE
WS=$WSROOT/ws-$SIDE
rm -rf "$OUT" "$WS"; mkdir -p "$OUT/setup" "$OUT/pypi" "$OUT/client" "$WS"
CURL=(curl -sS --noproxy '*' -u "$AUTH")
ARMS=$OUT/arms.txt; : > "$ARMS"
arm() { echo "[$(date +%H:%M:%S)] arm $*" | tee -a "$ARMS"; }
settle() { sleep 2; }

req() { # req <id> <method> <url> [curl args...] -> $OUT/pypi/<id>.{hdr,body}
  local id=$1 method=$2 url=$3; shift 3; local try code
  for try in 1 2 3 4 5; do
    "${CURL[@]}" -X "$method" -D "$OUT/pypi/$id.hdr" -o "$OUT/pypi/$id.body" \
      -w "$id %{http_code} %{size_download}B\n" "$url" "$@"
    code=$(awk 'NR==1{sub(/\r/,"");print $2}' "$OUT/pypi/$id.hdr")
    if [ "$code" != 503 ] && ! grep -q 'recurrent request failures' "$OUT/pypi/$id.body" 2>/dev/null; then
      return 0
    fi
    echo "  [gate] ref unhealthy ($code) — waiting 15s"; sleep 15
  done
  echo "  [gate] ref still unhealthy after 5 tries — ABORT arm $id" >&2
}

arm "0-setup repo l016-pypi"
printf '{"rclass":"local","packageType":"pypi"}' > "$OUT/setup/repo.json"
req repo-create PUT "$BASE/api/repositories/l016-pypi" -H 'Content-Type: application/json' -T "$OUT/setup/repo.json"
settle

arm "1-root-empty"
req p1-root-empty GET "$SIMPLE/"
settle

arm "2-twine-upload-sdist (real twine 7)"
( cd "$FIX" && "$VENV/bin/twine" upload --verbose --repository-url "$PYPI_API/" \
    -u "${AUTH%%:*}" -p "$PW" hello16-1.0.0.tar.gz ) > "$OUT/client/t1-twine-sdist.log" 2>&1
echo "twine exit=$?" | tee -a "$OUT/client/t1-twine-sdist.log"
sed -i '' "s/$PW/REDACTED/g" "$OUT/client/t1-twine-sdist.log"
settle

arm "3-root-immediacy-after-sdist (t0 + poll<=30s)"
req i1-root-t0 GET "$SIMPLE/"
python3 - "$OUT/pypi/i1-root-t0.body" >> "$ARMS" <<'EOF'
import sys
b = open(sys.argv[1]).read()
print(f"  i1 t0 root contains hello16: {'hello16' in b}")
EOF
for i in 1 2 3 4 5 6 7 8 9 10; do
  sleep 3
  req "i1-root-poll$i" GET "$SIMPLE/" > /dev/null
  if grep -q hello16 "$OUT/pypi/i1-root-poll$i.body"; then
    echo "  i1 materialized at poll$i (~$((i*3))s)" | tee -a "$ARMS"; break
  fi
done
settle

arm "4-twine-upload-wheel (real twine 7, Requires-Python >=3.8 in METADATA)"
( cd "$FIX" && "$VENV/bin/twine" upload --verbose --repository-url "$PYPI_API/" \
    -u "${AUTH%%:*}" -p "$PW" hello16-1.0.0-py3-none-any.whl ) > "$OUT/client/t2-twine-wheel.log" 2>&1
echo "twine exit=$?" | tee -a "$OUT/client/t2-twine-wheel.log"
sed -i '' "s/$PW/REDACTED/g" "$OUT/client/t2-twine-wheel.log"
settle

arm "5-root-immediacy-after-wheel (entry form: bare vs slash, rel)"
req i2-root-t0 GET "$SIMPLE/"
for i in 1 2 3 4 5; do
  grep -q hello16 "$OUT/pypi/i2-root-t0.body" && break
  sleep 3; req "i2-root-poll$i" GET "$SIMPLE/" > /dev/null
  grep -q hello16 "$OUT/pypi/i2-root-poll$i.body" && { echo "  i2 materialized at poll$i" | tee -a "$ARMS"; break; }
done
settle

arm "6-pkg-page (anchor set: rel/data-requires-python/#sha256/template/sort)"
req p2-pkg-page GET "$SIMPLE/hello16/"
settle

arm "7-etag-304"
req e1-etag-fresh GET "$SIMPLE/hello16/"
ET=$(awk 'tolower($1)=="etag:"{gsub(/\r/,"");print $2}' "$OUT/pypi/e1-etag-fresh.hdr" | head -1)
req e1-etag-cond GET "$SIMPLE/hello16/" -H "If-None-Match: $ET"
settle

arm "8-302-no-slash (Location form)"
req r1-302 GET "$SIMPLE/hello16"
settle

arm "9-anchor-chain (resolve own page href; hop chain + sha256 verify)"
python3 - "$OUT" "$PYPI_API" "$SIMPLE/hello16/" <<'EOF' > "$OUT/pypi/c1-urls.txt"
import re, sys, urllib.parse
out, api, page = sys.argv[1:]
body = open(f"{out}/pypi/p2-pkg-page.body").read()
hrefs = re.findall(r'<a href="([^"]+)"', body)
for h in hrefs:
    h = h.split("#")[0]
    print(urllib.parse.urljoin(page, h))
EOF
n=0
while read -r u; do
  [ -n "$u" ] || continue; n=$((n+1))
  "${CURL[@]}" -sSL -D "$OUT/pypi/c1-chain$n.hdr" -o "$OUT/pypi/c1-chain$n.body" \
    -w "c1-chain$n %{http_code} %{num_redirects}r %{size_download}B $u\n" "$u"
  shasum -a 256 "$OUT/pypi/c1-chain$n.body" | tee -a "$ARMS"
done < "$OUT/pypi/c1-urls.txt"
# cross-form reachability matrix (canonical packages/ route vs bare route, both sides)
req c1x-packages GET "$PYPI_API/packages/hello16/1.0.0/hello16-1.0.0-py3-none-any.whl"
req c1x-bare GET "$PYPI_API/hello16/1.0.0/hello16-1.0.0-py3-none-any.whl"
settle

arm "10-pip-download-default (real pip 26.2.1, wheel pref; PEP 691 Accept)"
"$VENV/bin/pip" download hello16==1.0.0 --no-deps -d "$WS/dl1" \
  --index-url "$PIP_INDEX" --trusted-host localhost --no-input --disable-pip-version-check \
  -v > "$OUT/client/pd1-pip-download.log" 2>&1
echo "pip download exit=$?" | tee -a "$OUT/client/pd1-pip-download.log"
sed -i '' "s/$PW/REDACTED/g" "$OUT/client/pd1-pip-download.log"
ls -l "$WS/dl1" >> "$OUT/client/pd1-pip-download.log" 2>&1
# PEP 691 Accept negotiation probe (pip's literal Accept vs simple page)
req pd1-accept691 GET "$SIMPLE/hello16/" \
  -H 'Accept: application/vnd.pypi.simple.v1+json; q=0.9, application/vnd.pypi.simple.v1+html; q=0.8, text/html; q=0.7'
settle

arm "11-pip-download-sdist (--no-binary)"
"$VENV/bin/pip" download hello16==1.0.0 --no-deps --no-binary :all: --no-build-isolation -d "$WS/dl2" \
  --index-url "$PIP_INDEX" --trusted-host localhost --no-input --disable-pip-version-check \
  -v > "$OUT/client/pd2-pip-sdist.log" 2>&1
echo "pip download exit=$?" | tee -a "$OUT/client/pd2-pip-sdist.log"
sed -i '' "s/$PW/REDACTED/g" "$OUT/client/pd2-pip-sdist.log"
ls -l "$WS/dl2" >> "$OUT/client/pd2-pip-sdist.log" 2>&1
settle

arm "12-pip-install --target (real install)"
"$VENV/bin/pip" install hello16==1.0.0 --target "$WS/tgt" --no-deps \
  --index-url "$PIP_INDEX" --trusted-host localhost --no-input --disable-pip-version-check \
  > "$OUT/client/pi1-pip-install.log" 2>&1
echo "pip install exit=$?" | tee -a "$OUT/client/pi1-pip-install.log"
sed -i '' "s/$PW/REDACTED/g" "$OUT/client/pi1-pip-install.log"
ls -l "$WS/tgt" >> "$OUT/client/pi1-pip-install.log" 2>&1
settle

arm "13-badwheel-1 badname (form name=hello16, file notl016-…whl)"
req b1-badname-upload POST "$PYPI_API/" \
  -F ':action=file_upload' -F "content=@$FIX/badname/notl016-1.0.0-py3-none-any.whl" \
  -F 'filetype=bdist_wheel' -F 'protocol_version=1' -F 'name=hello16' -F 'version=1.0.0'
req b1-index-after GET "$SIMPLE/hello16/"
req b1-index-under-filename GET "$SIMPLE/notl016/"
settle

arm "14-badwheel-2 badmeta (valid zip, NO METADATA — twine unreachable client-side)"
req b2-badmeta-upload POST "$PYPI_API/" \
  -F ':action=file_upload' -F "content=@$FIX/badmeta/nol016-1.0.0-py3-none-any.whl" \
  -F 'filetype=bdist_wheel' -F 'protocol_version=1' -F 'name=hello16' -F 'version=1.0.0'
req b2-index-after GET "$SIMPLE/hello16/"
req b2-index-under-filename GET "$SIMPLE/nol016/"
settle

arm "15-badwheel-3 badver (file hello16-abc-…whl, form version=1.0.0 — twine unreachable client-side)"
req b3-badver-upload POST "$PYPI_API/" \
  -F ':action=file_upload' -F "content=@$FIX/badver/hello16-abc-py3-none-any.whl" \
  -F 'filetype=bdist_wheel' -F 'protocol_version=1' -F 'name=hello16' -F 'version=1.0.0'
req b3-index-after GET "$SIMPLE/hello16/"
settle

arm "14b-duplicate-upload (same path re-upload of good wheel — redeploy policy)"
req b2b-dup-upload POST "$PYPI_API/" \
  -F ':action=file_upload' -F "content=@$FIX/hello16-1.0.0-py3-none-any.whl" \
  -F 'filetype=bdist_wheel' -F 'protocol_version=1' -F 'name=hello16' -F 'version=1.0.0'
req b2b-index-after GET "$SIMPLE/hello16/"
settle

arm "16-yanked (sdist 1.0.1 via curl + yanked=true form field)"
req y1-yanked-upload POST "$PYPI_API/" \
  -F ':action=file_upload' -F "content=@$FIX/hello16-1.0.1.tar.gz" \
  -F 'filetype=sdist' -F 'protocol_version=1' -F 'name=hello16' -F 'version=1.0.1' -F 'yanked=true'
req y1-index-after GET "$SIMPLE/hello16/"
settle

arm "17-legacy-json-latest (/pypi/hello16/json)"
req l1-legacy-latest GET "$PYPI_API/pypi/hello16/json"
settle

arm "18-legacy-json-version (/pypi/hello16/1.0.0/json)"
req l2-legacy-version GET "$PYPI_API/pypi/hello16/1.0.0/json"
settle

arm "99-teardown repo l016-pypi (deleteContent)"
req repo-delete DELETE "$BASE/api/repositories/l016-pypi?deleteContent=true"
echo "======== side=$SIDE done — evidence in $OUT"
