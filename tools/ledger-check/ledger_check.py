#!/usr/bin/env python3
"""BinFlow compatibility ledger cross-check (three-ledger consistency gate).

Surfaces checked:
  - docs/compatibility/known-divergence.yaml  divergence ledger (Z reconciliation)
  - docs/compatibility/matrix.yaml            capability matrix (summary vs rows)
  - docs/prd/pending-rulings.md               pending ruling seats (open/closed)

Checks:
  1. divergence ids unique; Z = resolved + open; open split by classification.
  2. bookkeeping drift: open entries whose review_gate already reads resolved
     (de-facto ruling present but the resolved key was never written).
  3. matrix summary.total_rows / state_counts match an independent row recount.
  4. pending-rulings seat table parses; open seats and user-authority seats listed.
  5. cross references both ways: seats named in divergence gate texts must exist;
     divergence ids named in pending-rulings must exist.

Exit codes: 0 pass (warnings allowed) / 1 failure / 2 warnings under --strict.
Usage: python3 tools/ledger-check/ledger_check.py [--strict]
"""

import re
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
KD_PATH = ROOT / "docs/compatibility/known-divergence.yaml"
MX_PATH = ROOT / "docs/compatibility/matrix.yaml"
PR_PATH = ROOT / "docs/prd/pending-rulings.md"

fails: list[str] = []
warns: list[str] = []


def fail(msg: str) -> None:
    fails.append(msg)


def warn(msg: str) -> None:
    warns.append(msg)


def ok(msg: str) -> None:
    print(f"  OK    {msg}")


def section(title: str) -> None:
    print(f"\n== {title} ==")


def check_known_divergence() -> dict:
    section("known-divergence.yaml")
    data = yaml.safe_load(KD_PATH.read_text(encoding="utf-8"))
    entries = data["entries"]

    ids = [e["id"] for e in entries]
    dupes = sorted({i for i in ids if ids.count(i) > 1})
    if dupes:
        fail(f"duplicate divergence ids: {dupes}")
    else:
        ok(f"{len(ids)} entries, ids unique")

    resolved = [e for e in entries if e.get("resolved")]
    opens = [e for e in entries if not e.get("resolved")]
    total = len(entries)
    if len(resolved) + len(opens) != total:
        fail(f"Z arithmetic broken: {len(resolved)}+{len(opens)} != {total}")
    else:
        ok(f"Z={total} = {len(resolved)} resolved + {len(opens)} open")

    by_class: dict[str, int] = {}
    for e in opens:
        by_class[e["classification"]] = by_class.get(e["classification"], 0) + 1
    order = ["UNKNOWN", "INTENTIONAL_DIFFERENCE", "BUG", "UNSUPPORTED_FEATURE"]
    print("        open = " + " + ".join(
        f"{c} {by_class.get(c, 0)}" for c in order))

    drift = [
        e["id"] for e in opens
        # de-facto ruling = gate DECLARES a resolved state up front
        # ("resolved（…）"/"resolved-by-*"/"已兑现闭合"/"存证"). A "→ resolved"
        # clause later in the gate describes the future close path, not state.
        if re.match(r"\s*(resolved|已兑现闭合|存证)", e.get("review_gate") or "", re.I)
    ]
    if drift:
        warn(
            "bookkeeping drift - open entries whose review_gate already reads "
            f"resolved ({len(drift)}, add the resolved key or re-word the gate):"
        )
        for i in drift:
            print(f"          - {i}")
    else:
        ok("no bookkeeping drift (open gates carry no resolved wording)")

    pending = [
        e["id"] for e in opens
        if isinstance(e.get("authority"), dict) and e["authority"].get("type") == "pending"
    ]
    ok(f"open entries with authority=pending (user/ADR to rule): {len(pending)}")
    return {"ids": set(ids), "opens": opens}


def check_matrix() -> None:
    section("matrix.yaml")
    data = yaml.safe_load(MX_PATH.read_text(encoding="utf-8"))
    rows = data.get("rows") or []
    summary = data.get("summary") or {}

    recount: dict[str, int] = {}
    for r in rows:
        s = r.get("state") or r.get("status")
        recount[s] = recount.get(s, 0) + 1

    declared_total = summary.get("total_rows")
    if declared_total != len(rows):
        fail(f"summary.total_rows={declared_total} but rows={len(rows)}")
    else:
        ok(f"total_rows={len(rows)}")

    declared_states = summary.get("state_counts") or {}
    mismatch = {k: (declared_states.get(k), recount.get(k, 0))
                for k in set(declared_states) | set(recount)
                if declared_states.get(k) != recount.get(k, 0)}
    if mismatch:
        fail(f"summary.state_counts mismatch (declared, actual): {mismatch}")
    else:
        ok("state_counts match row recount: " + ", ".join(
            f"{k}={v}" for k, v in sorted(recount.items())))

    baseline = ((data.get("reference") or {}).get("baseline") or {})
    if baseline.get("version"):
        ok(f"reference baseline: {baseline['version']} @ {baseline.get('instance', '?')}")
    else:
        warn("reference.baseline.version missing")


def check_pending_rulings() -> dict:
    section("pending-rulings.md")
    text = PR_PATH.read_text(encoding="utf-8")
    sec1 = text.split("## 1. 总览表")[1].split("## 2.")[0]
    rows = [l for l in sec1.splitlines() if l.startswith("| R-")]
    if not rows:
        fail("pending-rulings overview table not found or empty")
        return {"seats": set(), "open": []}

    seats: dict[str, str] = {}
    open_rows: list[tuple[str, str, str]] = []
    user_seats: list[str] = []
    for line in rows:
        cols = [c.strip() for c in line.split("|")[1:-1]]
        seat = cols[0]
        status = cols[6] if len(cols) > 6 else ""
        authority = cols[5] if len(cols) > 5 else ""
        seats[seat] = status
        closed = ("已收口" in status) or re.search(r"resolved 20\d\d", status)
        if not closed:
            open_rows.append((seat, authority, status))
            if "用户" in authority:
                user_seats.append(seat)

    ok(f"{len(seats)} seat rows, {len(open_rows)} open, {len(user_seats)} touch user authority")
    dupes = sorted({s for s in seats if list(seats).count(s) > 1})
    if dupes:
        fail(f"duplicate seat ids in table: {dupes}")
    return {"seats": set(seats), "open": open_rows}


def check_cross_refs(kd: dict, pr: dict) -> None:
    section("cross references")
    missing_seats: dict[str, list[str]] = {}
    seat_re = re.compile(r"(?<![A-Za-z0-9-])R-(\d{1,2})([a-z])?(?![0-9])")
    for e in kd["opens"]:
        unknown = set()
        for num, letter in seat_re.findall(e.get("review_gate") or ""):
            candidate = f"R-{num}{letter}"
            if candidate in pr["seats"]:
                continue
            # a bare family seat (R-10) is satisfied by any lettered row (R-10a…)
            if letter or not any(s.startswith(f"R-{num}") for s in pr["seats"]):
                unknown.add(candidate)
        if unknown:
            missing_seats[e["id"]] = sorted(unknown)
    if missing_seats:
        fail("divergence gates name seats absent from pending-rulings:")
        for i, s in missing_seats.items():
            print(f"          - {i}: {s}")
    else:
        ok("every seat named in open divergence gates exists in pending-rulings")

    pr_text = PR_PATH.read_text(encoding="utf-8")
    named_ids = set(re.findall(r"known-divergence[ ``]+([a-z0-9]+/[a-z0-9-]+)", pr_text))
    unknown_ids = sorted(named_ids - kd["ids"])
    if unknown_ids:
        fail("pending-rulings names divergence ids absent from the ledger:")
        for i in unknown_ids:
            print(f"          - {i}")
    else:
        ok(f"all {len(named_ids)} divergence ids named in pending-rulings exist in the ledger")


def main() -> int:
    strict = "--strict" in sys.argv
    print(f"ledger-check against {ROOT}")

    kd = check_known_divergence()
    check_matrix()
    pr = check_pending_rulings()
    check_cross_refs(kd, pr)

    print(f"\n== result: {len(fails)} fail, {len(warns)} warn ==")
    for f in fails:
        print(f"  FAIL  {f}")
    for w in warns:
        print(f"  WARN  {w}")
    if fails:
        return 1
    if strict and warns:
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
