#!/bin/bash
# L008-1b update-merge differential arms (ADR-0050, docs/design/
# repo-update-merge.md section 9.3): dual-end curl comparison of the
# repository configuration update face — POST = merge three-column matrix,
# PUT = create-only. Reference = live Artifactory; BinFlow = the UAT
# rebuild image. Verdicts print per arm; the run report lands in
# reports/compatibility/L008-update-merge-diff.md.
#
# Usage: tools/difftest/l0081b/update-merge-arms.sh \
#          [binflow-base binflow-user:pass ref-base ref-user:pass]
set -uo pipefail

BF_BASE="${1:-http://localhost:8085/binflow}"
BF_AUTH="${2:-admin:L0081bAdminPass}"
REF_BASE="${3:-http://localhost:8082/artifactory}"
REF_AUTH="${4:-admin:JFrog@2026}"
KEY="l0081b-m"

# Per-end seed bodies: identical except the contentSynchronisation INPUT
# spelling — the reference lands the NESTED form, BinFlow the flat xsd form
# (T-317's anchor; a pre-existing wire-spelling family, not an update-merge
# axis). Seeding both to the same STATE is what the arms compare.
read -r -d '' SEED_COMMON <<'EOF' || true
"rclass":"remote","packageType":"generic","url":"https://up.example.org/l0081b","username":"l0081b-seed","description":"seed words","hardFail":false,"retrievalCachePeriodSecs":3600,"missedRetrievalCachePeriodSecs":7200,"socketTimeoutMillis":30000,"maxUniqueSnapshots":5,"blackedOut":true,"archiveBrowsingEnabled":true,"enableTokenAuthentication":true
EOF
BF_SEED="{$SEED_COMMON,\"contentSynchronisation\":{\"statisticsEnabled\":true,\"propertiesEnabled\":true}}"
REF_SEED="{$SEED_COMMON,\"contentSynchronisation\":{\"statistics\":{\"enabled\":true},\"properties\":{\"enabled\":true}}}"

raw() { # raw <side> <key> -> GET body
  local base auth
  if [ "$1" = bf ]; then base=$BF_BASE; auth=$BF_AUTH; else base=$REF_BASE; auth=$REF_AUTH; fi
  curl -s -u "$auth" "$base/api/repositories/$2"
}

snap() { # snap <side> -> normalized comparison dict of $KEY
  python3 -c '
import json, sys, urllib.request, base64
side, base, auth = sys.argv[1], sys.argv[2], sys.argv[3]
req = urllib.request.Request(base + "/api/repositories/'"$KEY"'")
req.add_header("Authorization", "Basic " + base64.b64encode(auth.encode()).decode())
d = json.load(urllib.request.urlopen(req))
c = d.get("configuration", {}) if side == "bf" else d
def g(k):
    return c.get(k)
raw_cs = c.get("contentSynchronisation") or {}
if side == "ref":
    cs = {"enabled": raw_cs.get("enabled", False),
          "statistics": (raw_cs.get("statistics") or {}).get("enabled", False),
          "properties": (raw_cs.get("properties") or {}).get("enabled", False),
          "source": (raw_cs.get("source") or {}).get("originAbsenceDetection", False)}
else:
    cs = {"enabled": raw_cs.get("enabled", False),
          "statistics": raw_cs.get("statisticsEnabled", False),
          "properties": raw_cs.get("propertiesEnabled", False),
          "source": raw_cs.get("sourceOrigin", False)}
# Cleared-cell normalization: the reference ECHOES an explicit empty string,
# BinFlow omits the emptied key (omitempty) — both are "cleared", so None
# and "" compare equal here (an echo-shape note, not a value difference).
out = {"url": d.get("url"), "username": g("username") or "",
       "description": d.get("description") or "", "hardFail": g("hardFail"),
       "retrievalCachePeriodSecs": g("retrievalCachePeriodSecs"),
       "missedRetrievalCachePeriodSecs": g("missedRetrievalCachePeriodSecs"),
       "socketTimeoutMillis": g("socketTimeoutMillis"),
       "maxUniqueSnapshots": g("maxUniqueSnapshots"),
       "blackedOut": g("blackedOut"),
       "archiveBrowsingEnabled": g("archiveBrowsingEnabled"),
       "repoLayoutRef": g("repoLayoutRef"),
       "enableTokenAuthentication": g("enableTokenAuthentication"),
       "contentSynchronisation": cs}
print(json.dumps(out, sort_keys=True))' "$1" "$([ "$1" = bf ] && echo "$BF_BASE" || echo "$REF_BASE")" \
    "$([ "$1" = bf ] && echo "$BF_AUTH" || echo "$REF_AUTH")"
}

req() { # req <side> <method> <body> -> "status|body"
  local base auth out
  if [ "$1" = bf ]; then base=$BF_BASE; auth=$BF_AUTH; else base=$REF_BASE; auth=$REF_AUTH; fi
  out=$(curl -s -u "$auth" -X "$2" "$base/api/repositories/$KEY" \
    -H 'Content-Type: application/json' -d "$3" -w '|%{http_code}')
  echo "${out##*|}|${out%|*}"
}

field() { # field <normalized-json> <key> -> the key's value (fixed-key
  # extraction only; no eval — the key set is this script's own)
  python3 -c 'import json,sys; print(json.loads(sys.argv[1])[sys.argv[2]])' "$1" "$2"
}

verdict() { # verdict <arm> <label-a> <val-a> <label-b> <val-b>
  if [ "$3" = "$5" ]; then
    echo "SAME    $1"
  else
    echo "DIVERGE $1"
    echo "    $2: $3"
    echo "    $4: $5"
  fi
}

echo "== A12 PUT create regression (both ends) =="
bf=$(req bf PUT "$BF_SEED"); ref=$(req ref PUT "$REF_SEED")
verdict "A12 create status (want 200)" bf "${bf%%|*}" ref "${ref%%|*}"
verdict "A12 create wording" bf "$(echo "${bf#*|}" | grep -c 'created repository')" ref "$(echo "${ref#*|}" | grep -c 'created repository')"

echo "== A1 scalar omit: POST {\"hardFail\":true} keeps every other seat =="
bf=$(req bf POST '{"hardFail":true}'); ref=$(req ref POST '{"hardFail":true}')
verdict "A1 status (want 200)" bf "${bf%%|*}" ref "${ref%%|*}"
verdict "A1 keep-set" bf "$(snap bf)" ref "$(snap ref)"

echo "== A3 scalar null: POST {\"username\":null} clears =="
bf=$(req bf POST '{"username":null}'); ref=$(req ref POST '{"username":null}')
verdict "A3 status (want 200)" bf "${bf%%|*}" ref "${ref%%|*}"
verdict "A3 username cleared (None/empty)" bf "$(field "$(snap bf)" username)" ref "$(field "$(snap ref)" username)"

echo "== A5 scalar empty: POST {\"description\":\"\"} clears =="
bf=$(req bf POST '{"description":""}'); ref=$(req ref POST '{"description":""}')
verdict "A5 status (want 200)" bf "${bf%%|*}" ref "${ref%%|*}"
verdict "A5 description cleared" bf "$(field "$(snap bf)" description)" ref "$(field "$(snap ref)" description)"

echo "== A8 explicit values: 0/false are values =="
bf=$(req bf POST '{"retrievalCachePeriodSecs":0,"maxUniqueSnapshots":0,"hardFail":false}')
ref=$(req ref POST '{"retrievalCachePeriodSecs":0,"maxUniqueSnapshots":0,"hardFail":false}')
verdict "A8 status (want 200)" bf "${bf%%|*}" ref "${ref%%|*}"
a8() { field "$1" retrievalCachePeriodSecs; field "$1" maxUniqueSnapshots; field "$1" hardFail; }
verdict "A8 stored 0/0/false" bf "$(a8 "$(snap bf)" | tr '\n' '/')" ref "$(a8 "$(snap ref)" | tr '\n' '/')"

echo "== A9 POST {} keeps the url =="
bf=$(req bf POST '{}'); ref=$(req ref POST '{}')
verdict "A9 status (want 200)" bf "${bf%%|*}" ref "${ref%%|*}"
verdict "A9 url kept" bf "$(field "$(snap bf)" url)" ref "$(field "$(snap ref)" url)"

echo "== A7 object empty: contentSynchronisation {} resets the family =="
bf=$(req bf POST '{"contentSynchronisation":{}}'); ref=$(req ref POST '{"contentSynchronisation":{}}')
verdict "A7 status (want 200)" bf "${bf%%|*}" ref "${ref%%|*}"
verdict "A7 family reset (normalized)" bf "$(field "$(snap bf)" contentSynchronisation)" ref "$(field "$(snap ref)" contentSynchronisation)"

echo "== A15 object null: keeps the family (newly evidenced cell) =="
req bf POST '{"contentSynchronisation":{"statisticsEnabled":true,"propertiesEnabled":true}}' >/dev/null
req ref POST '{"contentSynchronisation":{"statistics":{"enabled":true},"properties":{"enabled":true}}}' >/dev/null
bf=$(req bf POST '{"contentSynchronisation":null}'); ref=$(req ref POST '{"contentSynchronisation":null}')
verdict "A15 status (want 200)" bf "${bf%%|*}" ref "${ref%%|*}"
verdict "A15 family kept (normalized)" bf "$(field "$(snap bf)" contentSynchronisation)" ref "$(field "$(snap ref)" contentSynchronisation)"

echo "== A14 object partial: unmentioned sub-keys RESET (newly evidenced cell) =="
bf=$(req bf POST '{"contentSynchronisation":{"statisticsEnabled":true}}')
ref=$(req ref POST '{"contentSynchronisation":{"statistics":{"enabled":true}}}')
verdict "A14 status (want 200)" bf "${bf%%|*}" ref "${ref%%|*}"
verdict "A14 whole-object replacement (normalized)" bf "$(field "$(snap bf)" contentSynchronisation)" ref "$(field "$(snap ref)" contentSynchronisation)"

echo "== A4/A6 array family: customHttpHeaders null and [] never clear =="
# BinFlow carries no customHttpHeaders seat (repo-semantics 7.1 lists it as a
# transport-layer option; the field rides the scenario-D unknown-field drop),
# so the ARM asserts the no-clear SEMANTICS on both ends: the reference keeps
# its stored array through null and [], BinFlow keeps every OTHER seat, and
# the field-echo difference (reference echoes it, BinFlow never stores it) is
# the pre-existing unknown-field family, not an update-merge axis.
req ref POST '{"customHttpHeaders":[{"name":"X-L0081b","value":"1"}]}' >/dev/null
ref_hdr=$(raw ref "$KEY" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("customHttpHeaders"))')
echo "    ref customHttpHeaders seeded:      $ref_hdr"
req ref POST '{"customHttpHeaders":null}' >/dev/null
req bf  POST '{"customHttpHeaders":null}' >/dev/null
ref_hdr=$(raw ref "$KEY" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("customHttpHeaders"))')
echo "    ref customHttpHeaders after null: $ref_hdr (kept = no clear channel)"
req ref POST '{"customHttpHeaders":[]}' >/dev/null
req bf  POST '{"customHttpHeaders":[]}' >/dev/null
ref_hdr=$(raw ref "$KEY" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("customHttpHeaders"))')
echo "    ref customHttpHeaders after []:   $ref_hdr (kept = no clear channel)"
verdict "A4/A6 every other seat kept through the array arms" bf "$(snap bf)" ref "$(snap ref)"

echo "== A13 console-form regression: the FULL body round-trips unchanged =="
# The console edit flow sends the complete form through updateRepo=POST; under
# merge semantics an all-seats body is an all-seats write — GET before and
# after the full-body POST must be identical on both ends.
req bf POST "$BF_SEED" >/dev/null
req ref POST "$REF_SEED" >/dev/null
verdict "A13 full-body save lands identically on both ends" bf "$(snap bf)" ref "$(snap ref)"

echo "== A10 PUT-on-existing complete body: the create-only 400 =="
snap_bf=$(snap bf); snap_ref=$(snap ref)
bf=$(req bf PUT "$BF_SEED"); ref=$(req ref PUT "$REF_SEED")
verdict "A10 status (want 400)" bf "${bf%%|*}" ref "${ref%%|*}"
bm=$(echo "${bf#*|}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["errors"][0]["message"])' 2>/dev/null)
rm=$(echo "${ref#*|}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["errors"][0]["message"])' 2>/dev/null)
verdict "A10 literal message" bf "$bm" ref "$rm"

echo "== A11 zero side effects after the refused PUT =="
verdict "A11 config unchanged (bf)" before "$snap_bf" after "$(snap bf)"
verdict "A11 config unchanged (ref)" before "$snap_ref" after "$(snap ref)"

echo "== A16 probe (record): PUT-on-existing WITHOUT rclass =="
bf=$(req bf PUT '{"url":"https://up.example.org/moved"}')
ref=$(req ref PUT '{"url":"https://up.example.org/moved"}')
echo "    bf : ${bf%%|*} $(echo "${bf#*|}" | tr -d '\n' | head -c 200)"
echo "    ref: ${ref%%|*} $(echo "${ref#*|}" | tr -d '\n' | head -c 200)"

echo "== cleanup =="
curl -s -u "$BF_AUTH" -X DELETE "$BF_BASE/api/repositories/$KEY?deleteContent=true" -o /dev/null -w "bf-delete:%{http_code}\n"
curl -s -u "$REF_AUTH" -X DELETE "$REF_BASE/api/repositories/$KEY?deleteContent=true" -o /dev/null -w "ref-delete:%{http_code}\n"
