"""T-550 arm 3 (known-divergence maven/virtual-metadata-modulereleases-skip):
the module-level metadata arm REQUIRES the handle* seat (a remote member
whose handleReleases=false persists in the remote canonical config).

Precondition probe: PUT a maven remote carrying handleReleases=false and
read the config echo back. A persists the seat (echo false). BinFlow's
remote canonical does not persist handle* today (internal/remote/
projection.go: "absent = true … the projection mirrors whatever the row
carries the day the seats land") — the echo stays true and the module-level
arm is unreachable (NOT_RUN by precondition; T-541 lands the seat).

The day BOTH echoes read false, run() proceeds to the module-level arm:
virtual [remote(handleReleases=false) with a cached module-level
maven-metadata.xml + a local member carrying a second version] and the
assertion is whether the remote member's versions stay in the merged
module-level listing. Construction legs (fake upstream + <K>-cache
seeding) are sketched in the batch report; they are intentionally NOT
guessed here — the arm body stays unimplemented until the probe passes,
so the case can never fake a verdict.
"""
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-module-handle-seat-probe",
    "title": "handle* seat probe — module-level metadata arm precondition "
             "(remote handleReleases=false persistence)",
    "layer": "L6",
    "domain": "maven",
    "auth": True,
    "timeout_s": 45,
}

REMOTE = "difftest-r4t550-hsrem"


def _seat_echo(ctx, side):
    mavenlib.repo_put(ctx, side, REMOTE, {
        "rclass": "remote", "packageType": "maven",
        "url": "https://repo1.maven.org/maven2/",
        "handleReleases": False,
        "allowPrivateUpstream": True,
    })
    cfg = ctx.http(side, "GET", "/api/repositories/" + REMOTE)
    try:
        doc = json.loads(cfg["body"].decode("utf-8", "replace"))
    except ValueError:
        doc = {}
    hr = doc.get("handleReleases")
    return cfg["status"], ("false" if hr is False else
                           "true" if hr is True else "absent"), doc


def run(ctx):
    echoes = {}
    try:
        for side in ("a", "b"):
            st, echo, doc = _seat_echo(ctx, side)
            echoes[side] = echo
            ctx.write_evidence("%s-seat.json" % side, {
                "remote_cfg_get_status": st, "handleReleases_echo": echo,
                "cfg_keys_present": sorted(k for k in doc
                                           if k.startswith("handle"))})
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [REMOTE, REMOTE + "-cache"])

    ev = ["evidence/%s/%s-seat.json" % (CASE["id"], s) for s in ("a", "b")]
    if echoes.get("a") != "false" or echoes.get("b") != "false":
        # ponytail: the arm body is unimplemented on purpose — the seat
        # landing (T-541) flips this probe and the arm gets its own ticket.
        ev.append(ctx.write_evidence("summary.json", {
            "a_echo": echoes.get("a"), "b_echo": echoes.get("b"),
            "verdict": "NOT_RUN(precondition): module-level handleReleases "
                       "arm unreachable — seat echo(s) above; expected "
                       "a=false (A persists), b=false the day T-541 lands"}))
        return {"status": "BLOCKED",
                "reason": "NOT_RUN by precondition: handle* seat "
                          "(a=%s, b=%s) — T-541 F8-widened handle* not "
                          "merged in this tree; the module-level arm is "
                          "not guessed" % (echoes.get("a"), echoes.get("b")),
                "evidence": ev,
                "requests": [{"step": "PUT remote handleReleases=false; GET "
                                      "config echo"}]}

    ev.append(ctx.write_evidence("summary.json", {
        "a_echo": echoes["a"], "b_echo": echoes["b"],
        "verdict": "seat present on BOTH sides — module-level arm now "
                   "constructible (arm body lands with its own ticket; "
                   "this probe is the gate, not the arm)"}))
    return {"status": "BLOCKED",
            "reason": "seat present (a=b=false) but the module-level arm "
                      "body is not implemented in this batch — hand to the "
                      "arm ticket once T-541 lands",
            "evidence": ev,
            "requests": [{"step": "PUT remote handleReleases=false; GET "
                                  "config echo"}]}
