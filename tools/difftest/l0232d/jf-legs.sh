#!/bin/bash
# L023-2D jf CLI real-client legs (build-publish / build-promote / build-discard)
# A = Artifactory 7.161.15 :8082 (l023da) / B = BinFlow dev :8083 (l023db)
set -u
WIRE=/Users/lzw/dev-center/reports/compatibility/l023d-wire
export JFROG_CLI_LOG_LEVEL=ERROR

jf c add l023da --url=http://172.16.58.130:8082/artifactory --user=admin --password='JFrog@2026' --interactive=false >/dev/null 2>&1
jf c add l023db --url=http://172.16.58.130:8083/binflow --user=admin --password='password' --interactive=false >/dev/null 2>&1

echo "l0232d-jf-wheel-content-v1" > /tmp/l023d-jf-wheel.bin

leg() {  # leg <side: a|b> <logfile>
  local s=$1 log=$2 conn
  if [ "$s" = a ]; then conn="--server-id=l023da"; else conn="--url=http://172.16.58.130:8083/binflow --user=admin --password=password"; fi
  {
    echo "== upload (build-tagged) =="
    jf rt upload --build-name=l023d-jf-app --build-number=7 /tmp/l023d-jf-wheel.bin \
      l023d-dev-local/jf/7/wheel-jf.bin $conn 2>&1; echo "exit=$?"
    echo "== build-publish 7 =="
    jf rt build-publish l023d-jf-app 7 $conn 2>&1; echo "exit=$?"
    echo "== build-promote 7 -> l023d-rel-local =="
    jf rt build-promote l023d-jf-app 7 l023d-rel-local --status=smoked $conn 2>&1; echo "exit=$?"
    echo "== seed runs 8,9 =="
    for n in 8 9; do
      jf rt upload --build-name=l023d-jf-app --build-number=$n /tmp/l023d-jf-wheel.bin \
        l023d-dev-local/jf/$n/wheel-jf.bin $conn 2>&1 >/dev/null
      jf rt build-publish l023d-jf-app $n $conn 2>&1; echo "exit=$?"
    done
    echo "== build-discard --max-builds=1 =="
    jf rt build-discard l023d-jf-app --max-builds=1 $conn 2>&1; echo "exit=$?"
  } > "$log" 2>&1
}

leg a "$WIRE/a/jf-legs.log"
leg b "$WIRE/b/jf-legs.log"

for s in a b; do
  echo "── side $s key lines ──"
  grep -E "^(==|exit=|[0-9]+ (artifacts|bytes)|Promot|Builds discarded|Error|Warning)" "$WIRE/$s/jf-legs.log" | head -25
done
echo "── survivors (number list) ──"
curl -s -u 'admin:JFrog@2026' http://172.16.58.130:8082/artifactory/api/build/l023d-jf-app | python3 -c "import json,sys; d=json.load(sys.stdin); print('A', [x['uri'] for x in d.get('buildsNumbers',[])])"
curl -s -u admin:password http://172.16.58.130:8083/binflow/api/build/l023d-jf-app | python3 -c "import json,sys; d=json.load(sys.stdin); print('B', [x['uri'] for x in d.get('buildsNumbers',[])])"
echo "── relocation check (jf promote moved artifact?) ──"
for s in a b; do
  if [ $s = a ]; then AUTH='admin:JFrog@2026'; H=http://172.16.58.130:8082/artifactory; else AUTH='admin:password'; H=http://172.16.58.130:8083/binflow; fi
  echo -n "$s dev: "; curl -s -o /dev/null -w '%{http_code}' -u "$AUTH" "$H/l023d-dev-local/jf/7/wheel-jf.bin"
  echo -n "  rel: "; curl -s -o /dev/null -w '%{http_code}\n' -u "$AUTH" "$H/l023d-rel-local/jf/7/wheel-jf.bin"
done
