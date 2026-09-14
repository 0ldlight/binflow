#!/bin/bash
# L017-1 pypi D1/D7 differential replay — three-source requires-python render
# (twine sdist form source / twine wheel METADATA source / curl sdist PKG-INFO
# server-side derivation) + invalid-wheel three-state admission matrix.
# Written by dev-registry-adapter (L017-1 implementation ticket); tree normally
# owned by differential-qa-engineer — hand-off note in reports/agents/T-L017-1.md.
#
# Usage: replay.sh <a|b>    a=Artifactory ref :8082, b=BinFlow UAT :8083
# Env:  ARTI_AUTH / BF_AUTH = "user:password"
# Out:  reports/compatibility/l017-wire/<side>/{arms.txt,pypi/*}
set -u
HERE=/Users/lzw/dev-center/tools/difftest/l017
FIX=$HERE/fixtures
VENV=/tmp/l017venv
WIRE=/Users/lzw/dev-center/reports/compatibility/l017-wire

SIDE=${1:?side a|b}
if [ "$SIDE" = a ]; then HOSTPORT=8082; PREFIX=artifactory; AUTHVAR=ARTI_AUTH
else HOSTPORT=8083; PREFIX=binflow; AUTHVAR=BF_AUTH; fi
AUTH=${!AUTHVAR:-}
[ -n "$AUTH" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }
USER_=${AUTH%%:*}; PW=${AUTH#*:}

BASE=http://localhost:$HOSTPORT/$PREFIX
PYPI_API=$BASE/api/pypi/l017-pypi
SIMPLE=$PYPI_API/simple
OUT=$WIRE/$SIDE
rm -rf "$OUT"; mkdir -p "$OUT/pypi"
CURL=(curl -sS --noproxy '*' -u "$AUTH")
ARMS=$OUT/arms.txt; : > "$ARMS"
arm() { echo "[$(date +%H:%M:%S)] arm $*" | tee -a "$ARMS"; }
settle() { sleep 2; }
req() { # req <id> <method> <url> [curl args...]
  local id=$1 method=$2 url=$3; shift 3; local try code
  for try in 1 2 3 4 5; do
    "${CURL[@]}" -X "$method" -D "$OUT/pypi/$id.hdr" -o "$OUT/pypi/$id.body" \
      -w "$id %{http_code} %{size_download}B\n" "$url" "$@"
    code=$(awk 'NR==1{sub(/\r/,"");print $2}' "$OUT/pypi/$id.hdr")
    if [ "$code" != 503 ]; then return 0; fi
    echo "  [gate] ref unhealthy ($code) — waiting 15s"; sleep 15
  done
}

arm "0-setup repo l017-pypi"
printf '{"rclass":"local","packageType":"pypi"}' > "$OUT/repo.json"
req repo-create PUT "$BASE/api/repositories/l017-pypi" -H 'Content-Type: application/json' -T "$OUT/repo.json"
settle

arm "1-D1a twine upload sdist (form requires_python source)"
( cd "$FIX" && "$VENV/bin/twine" upload --repository-url "$PYPI_API/" \
    -u "$USER_" -p "$PW" --non-interactive hello16-1.0.0.tar.gz ) > "$OUT/pypi/t1-twine-sdist.log" 2>&1
echo "twine exit=$?" >> "$OUT/pypi/t1-twine-sdist.log"; sed -i '' "s/$PW/REDACTED/g" "$OUT/pypi/t1-twine-sdist.log"
settle

arm "2-D1b twine upload wheel (METADATA source)"
( cd "$FIX" && "$VENV/bin/twine" upload --repository-url "$PYPI_API/" \
    -u "$USER_" -p "$PW" --non-interactive hello16-1.0.0-py3-none-any.whl ) > "$OUT/pypi/t2-twine-wheel.log" 2>&1
echo "twine exit=$?" >> "$OUT/pypi/t2-twine-wheel.log"; sed -i '' "s/$PW/REDACTED/g" "$OUT/pypi/t2-twine-wheel.log"
settle

arm "3-D1c curl sdist 1.0.1 WITHOUT requires_python form field (PKG-INFO server-side derivation)"
req y1-sdist-upload POST "$PYPI_API/" \
  -F ':action=file_upload' -F 'filetype=sdist' -F 'protocol_version=1' \
  -F 'name=hello16' -F 'version=1.0.1' -F "content=@$FIX/hello16-1.0.1.tar.gz"
settle

arm "4-pkg-page (D1 render: anchor attrs)"
req p2-pkg-page GET "$SIMPLE/hello16/"
settle

arm "5-D7a badname (form name=hello16, file notl016-…whl)"
req b1-badname-upload POST "$PYPI_API/" \
  -F ':action=file_upload' -F 'filetype=bdist_wheel' -F 'protocol_version=1' \
  -F 'name=hello16' -F 'version=1.0.0' -F "content=@$FIX/badname/notl016-1.0.0-py3-none-any.whl"
req b1-index-after GET "$SIMPLE/hello16/"
req b1-index-under-filename GET "$SIMPLE/notl016/"
settle

arm "6-D7b badmeta (valid zip, NO METADATA)"
req b2-badmeta-upload POST "$PYPI_API/" \
  -F ':action=file_upload' -F 'filetype=bdist_wheel' -F 'protocol_version=1' \
  -F 'name=hello16' -F 'version=1.0.0' -F "content=@$FIX/badmeta/nol016-1.0.0-py3-none-any.whl"
req b2-index-after GET "$SIMPLE/hello16/"
req b2-index-under-filename GET "$SIMPLE/nol016/"
settle

arm "7-D7c badver (file hello16-abc-…whl, form version=1.0.0)"
req b3-badver-upload POST "$PYPI_API/" \
  -F ':action=file_upload' -F 'filetype=bdist_wheel' -F 'protocol_version=1' \
  -F 'name=hello16' -F 'version=1.0.0' -F "content=@$FIX/badver/hello16-abc-py3-none-any.whl"
req b3-index-after GET "$SIMPLE/hello16/"
settle

arm "8-pip requires-python filter (client-visible D1 effect)"
mkdir -p "$FIX/rptest"
python3 - "$FIX/rptest" <<'EOF'
import io, os, tarfile, zipfile, sys
out = sys.argv[1]
def whl(name, ver, rp):
    di = f"{name}-{ver}.dist-info"
    b = io.BytesIO()
    with zipfile.ZipFile(b, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr(f"{di}/METADATA",
                   f"Metadata-Version: 2.1\nName: {name}\nVersion: {ver}\nRequires-Python: {rp}\n")
        z.writestr(f"{di}/WHEEL", "Wheel-Version: 1.0\nRoot-Is-Purelib: true\nTag: py3-none-any\n")
        z.writestr(f"{name}/__init__.py", "")
    return b.getvalue()
open(os.path.join(out, "rptest-1.0.0-py3-none-any.whl"), "wb").write(whl("rptest", "1.0.0", ">=3.99"))
open(os.path.join(out, "rptest-1.0.1-py3-none-any.whl"), "wb").write(whl("rptest", "1.0.1", ">=3.8"))
EOF
"$VENV/bin/twine" upload --repository-url "$PYPI_API/" -u "$USER_" -p "$PW" --non-interactive \
  "$FIX/rptest/rptest-1.0.0-py3-none-any.whl" "$FIX/rptest/rptest-1.0.1-py3-none-any.whl" \
  > "$OUT/pypi/t3-twine-rptest.log" 2>&1
echo "twine exit=$?" >> "$OUT/pypi/t3-twine-rptest.log"; sed -i '' "s/$PW/REDACTED/g" "$OUT/pypi/t3-twine-rptest.log"
settle
"$VENV/bin/pip" download rptest --no-deps -d "$OUT/dl" --index-url "http://${AUTH}@localhost:${HOSTPORT}/${PREFIX}/api/pypi/l017-pypi/simple" \
  --trusted-host localhost --no-input --disable-pip-version-check > "$OUT/pypi/pd-rptest.log" 2>&1
sed -i '' "s/$PW/REDACTED/g" "$OUT/pypi/pd-rptest.log"
ls -1 "$OUT/dl" 2>/dev/null | tee -a "$ARMS"
settle

arm "99-teardown repo l017-pypi (deleteContent)"
req repo-delete DELETE "$BASE/api/repositories/l017-pypi?deleteContent=true"
echo "======== side=$SIDE done — evidence in $OUT"
