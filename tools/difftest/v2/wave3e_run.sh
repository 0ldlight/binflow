#!/usr/bin/env bash
# wave3e batch runner — L043 five-candidate live re-verification on the
# current deployment + bearer A-side 24-cell matrix completion.
#
# Runs probes 1/3/5/2 (catalog topology, bearer matrix, Accept 406, schema1
# phase) in that order. Probe 4 (TTL workload-dependency) is a long-running
# background probe started SEPARATELY before this batch — its quiet window
# must be free of A-side docker-remote traffic, which is why probes 1/3/5/2
# wait for RUN_GATE_FILE (created by probe 4's quiet-window end or manually).
#
# Evidence: run/wave3-expansion/<probe>.json ; pass RUN=2 for the rerun gate
# (evidence suffix -run2, conclusions must match run 1 per-arm).
#
# Ticket marker: L044-wave3-expansion.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
CASES="$HERE/cases"
RUN="${RUN:-1}"
GATE="${RUN_GATE_FILE:-$HERE/run/wave3-expansion/.p4-quiet-window-done}"

SUFFIX=""
if [ "$RUN" != "1" ]; then SUFFIX="-run2"; fi
export W3E_EVIDENCE_SUFFIX="$SUFFIX"

if [ ! -f "$GATE" ] && [ "${SKIP_GATE:-0}" != "1" ]; then
  echo "gate not present: $GATE (p4 quiet window still open?) — waiting" >&2
  while [ ! -f "$GATE" ]; do sleep 60; done
fi

echo "== wave3e batch run=$RUN suffix='$SUFFIX' =="

echo "-- probe 1: catalog bare-root topology --"
python3 -I "$CASES/wave3e_p1_catalog_reverify.py"
echo "p1 exit=$?"

echo "-- probe 3: bearer cross-face matrix (A-side 24-cell completion) --"
python3 -I "$CASES/wave3e_p3_bearer_matrix.py"
echo "p3 exit=$?"

echo "-- probe 5: tags/list Accept 406 matrix --"
python3 -I "$CASES/wave3e_p5_tags_accept_reverify.py"
echo "p5 exit=$?"

echo "-- probe 2: schema1 negotiation phase --"
python3 -I "$CASES/wave3e_p2_schema1_reverify.py"
echo "p2 exit=$?"

echo "-- residue check --"
python3 -I "$CASES/wave3e_residue_check.py"
echo "residue exit=$?"

echo "== wave3e batch run=$RUN done =="
