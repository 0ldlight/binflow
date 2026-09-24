#!/usr/bin/env bash
# difftest v2 scorer: read results.json, print one machine-readable JSON line:
#   {"pass":N,"fail":N,"blocked":N,"not_run":N,"total":N,"coverage":C|null}
# coverage = pass / (total - blocked - not_run); null when denominator is 0.
# Exit code is always 0: state travels in the JSON, never the exit code.
set -euo pipefail

results="${1:-$(cd "$(dirname "$0")" && pwd)/run/results.json}"
if [[ ! -f "$results" ]]; then
  echo "{\"pass\":0,\"fail\":0,\"blocked\":0,\"not_run\":0,\"total\":0,\"coverage\":null}"
  exit 0
fi

python3 - "$results" <<'PY'
import json, sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)

mapping = {"PASS": "pass", "FAIL": "fail", "BLOCKED": "blocked", "NOT_RUN": "not_run"}
counts = {"pass": 0, "fail": 0, "blocked": 0, "not_run": 0}
for case in data.get("cases", []):
    status = (case.get("status") or "NOT_RUN").upper()
    key = mapping.get(status)
    if key is None:
        print("warning: unknown status %r counted as not_run" % status, file=sys.stderr)
        key = "not_run"
    counts[key] += 1

counts["total"] = len(data.get("cases", []))
denominator = counts["total"] - counts["blocked"] - counts["not_run"]
counts["coverage"] = round(counts["pass"] / denominator, 4) if denominator > 0 else None
print(json.dumps(counts, separators=(",", ":"), sort_keys=False))
PY
