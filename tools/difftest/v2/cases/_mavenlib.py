"""Shared helpers for difftest v2 Maven cases (T-523 first batch).

Not a case: the runner's discovery skips files starting with "_". Each case
module imports this via an explicit path load (cases/ is not a package).

Spec anchors used by the assertions live in docs/reverse/:
  - virtual-resolution.md §5.1 (maven-metadata.xml merge), §7.5 (virtual DELETE)
  - repo-semantics.md §8.2 L217-220 (write routing, defaultDeploymentRepo)
  - maven-npm-pypi.md §1.1-§1.5 (endpoints, snapshot rewrite, metadata calc,
    checksums)
  - remote-cache-projection.md §2.1/§3 (cache projection access, write-on-pull)

Credential discipline: nothing here reads secrets from anywhere except the
runner-provided ctx.sides[...] values (env-injected). The generated mvn
settings.xml references ${env.DIFFTEST_MVN_USER}/${env.DIFFTEST_MVN_PASS} so
the password never lands on disk or argv.
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import subprocess
import tempfile
import time
import xml.etree.ElementTree as ET

MVN_SERVER_ID = "difftest-mvn"

# settings template: Maven interpolates ${env.*}; the file itself carries no
# secret. DIFFTEST_MVN_USER / DIFFTEST_MVN_PASS are set only in the mvn
# subprocess environment, sourced from ctx.sides[side].
_MVN_SETTINGS = (
    "<settings><servers><server>"
    "<id>%s</id>"
    "<username>${env.DIFFTEST_MVN_USER}</username>"
    "<password>${env.DIFFTEST_MVN_PASS}</password>"
    "</server></servers></settings>" % MVN_SERVER_ID
)


class SetupError(Exception):
    """Precondition (repo provisioning, fixture prep) unavailable -> BLOCKED."""


# ------------------------------------------------------------------ digests

def sha256_hex(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha1_hex(data: bytes) -> str:
    return hashlib.sha1(data).hexdigest()


# ------------------------------------------------------------------ fixtures

def pom_fixture(group: str, artifact: str, version: str) -> bytes:
    """Deterministic minimal pom. Deliberately no <repositories>/
    <pluginRepositories> so the virtual PomInterceptor (default
    discard_active_reference, virtual-resolution.md §5.2) is a no-op and the
    pom compares byte-identical through the virtual."""
    return (
        '<?xml version="1.0" encoding="UTF-8"?>\n'
        '<project xmlns="http://maven.apache.org/POM/4.0.0">\n'
        '  <modelVersion>4.0.0</modelVersion>\n'
        '  <groupId>%s</groupId>\n'
        '  <artifactId>%s</artifactId>\n'
        '  <version>%s</version>\n'
        '  <packaging>jar</packaging>\n'
        '</project>\n' % (group, artifact, version)
    ).encode()


def jar_fixture(label: str) -> bytes:
    """Fixed payload bytes (deploy-file uploads -Dfile verbatim; no jar
    structural validity is required on the wire)."""
    return ("difftest fixture payload :: %s\n" % label).encode()


# ------------------------------------------------------------------ repos

def repo_delete(ctx, side: str, key: str):
    """Best-effort DELETE; any status accepted (200/404 both fine)."""
    try:
        ctx.http(side, "DELETE", "/api/repositories/" + key)
    except Exception:  # noqa: BLE001 - cleanup must never mask the verdict
        pass


def cleanup_repos(ctx, side: str, keys):
    for key in keys:
        repo_delete(ctx, side, key)


def repo_put(ctx, side: str, key: str, payload: dict):
    resp = ctx.http(side, "PUT", "/api/repositories/" + key,
                    body=json.dumps(payload),
                    headers={"Content-Type": "application/json"})
    if resp["status"] not in (200, 201):
        raise SetupError("create repo %r on side %r -> %s: %s" % (
            key, side, resp["status"], resp["body"][:300].decode("utf-8", "replace")))
    return resp


def ensure_repos(ctx, side: str, specs):
    """specs: list of (key, payload) in creation order."""
    for key, payload in specs:
        repo_put(ctx, side, key, payload)


def maven_local(key: str, **extra) -> tuple:
    payload = {"rclass": "local", "packageType": "maven"}
    payload.update(extra)
    return key, payload


def maven_virtual(key: str, members, **extra) -> tuple:
    payload = {"rclass": "virtual", "packageType": "maven",
               "repositories": list(members)}
    payload.update(extra)
    return key, payload


# ------------------------------------------------------------------ polling

def poll_until(ctx, side: str, path: str, predicate, budget_s: float = 15.0,
               interval_s: float = 1.0):
    """Poll GET until predicate(status, body_bytes) is true or budget runs out.

    Returns the last (status, body). Async metadata calculation
    (maven-npm-pypi.md §1.4) makes polling mandatory; the budget is never
    auto-widened.
    """
    deadline = time.monotonic() + budget_s
    status, body = None, b""
    while True:
        resp = ctx.http(side, "GET", path)
        status, body = resp["status"], resp["body"]
        if predicate(status, body):
            return status, body
        if time.monotonic() >= deadline:
            return status, body
        time.sleep(interval_s)


# ------------------------------------------------------------------ metadata

def _local(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]


def parse_metadata(body: bytes) -> dict:
    """Parse maven-metadata.xml into a flat dict (namespace-tolerant).

    stdlib ET never resolves external entities; rejecting any DOCTYPE also
    blocks inline entity expansion (billion laughs) from a hostile instance.
    """
    if b"<!DOCTYPE" in body[:2048]:
        raise ValueError("maven-metadata.xml carries a DOCTYPE (rejected)")
    out = {"group_id": None, "artifact_id": None, "version": None,
           "versions": [], "latest": None, "release": None,
           "snapshot": {}, "snapshot_versions": []}
    key_map = {"groupId": "group_id", "artifactId": "artifact_id",
               "version": "version"}
    root = ET.fromstring(body)
    for child in root:
        tag = _local(child.tag)
        text = (child.text or "").strip()
        if tag in key_map:
            out[key_map[tag]] = text
        elif tag == "versioning":
            for v in child:
                vt, vt_text = _local(v.tag), (v.text or "").strip()
                if vt == "versions":
                    out["versions"] = [(x.text or "").strip() for x in v
                                       if _local(x.tag) == "version"]
                elif vt == "latest":
                    out["latest"] = vt_text
                elif vt == "release":
                    out["release"] = vt_text
                elif vt == "snapshot":
                    for s in v:
                        out["snapshot"][_local(s.tag)] = (s.text or "").strip()
                elif vt == "snapshotVersions":
                    for sv in v:
                        if _local(sv.tag) != "snapshotVersion":
                            continue
                        entry = {_local(x.tag): (x.text or "").strip() for x in sv}
                        out["snapshot_versions"].append(entry)
    return out


TIMESTAMPED_RE = re.compile(r"\d{8}\.\d{6}-\d+")


def snapshot_form(filename: str) -> str:
    """unique -> timestamped (maven-npm-pypi.md §1.3), else plain."""
    return "timestamped" if TIMESTAMPED_RE.search(filename) else "plain"


# ------------------------------------------------------------------ mvn leg

def mvn_deploy_file(ctx, side: str, repo_url: str, workdir: str,
                    group: str, artifact: str, version: str,
                    jar_path: str, pom_path: str) -> dict:
    """Real-client leg: mvn deploy:deploy-file (same flags as the proven
    l0134 wire leg). Credentials enter only via the subprocess environment."""
    settings = os.path.join(workdir, "settings-%s.xml" % side)
    with open(settings, "w", encoding="utf-8") as fh:
        fh.write(_MVN_SETTINGS)
    cmd = [
        "mvn", "-B", "-q", "-s", settings,
        "-Dmaven.wagon.http.retryHandler.count=1",
        "-Dmaven.resolver.transport=wagon",
        "deploy:deploy-file",
        "-DrepositoryId=" + MVN_SERVER_ID,
        "-Durl=" + repo_url,
        "-DgroupId=" + group, "-DartifactId=" + artifact,
        "-Dversion=" + version, "-Dpackaging=jar",
        "-Dfile=" + jar_path, "-DpomFile=" + pom_path,
    ]
    env = dict(os.environ)
    env["DIFFTEST_MVN_USER"] = ctx.sides[side]["user"]
    env["DIFFTEST_MVN_PASS"] = ctx.sides[side]["password"]
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True,
                              timeout=ctx.timeout_s, env=env)
        tail = "\n".join((proc.stdout + "\n" + proc.stderr).splitlines()[-8:])
        return {"exit": str(proc.returncode), "tail": tail}
    except FileNotFoundError:
        raise SetupError("mvn binary not found on PATH (real-client leg "
                         "requires Maven)") from None
    except subprocess.TimeoutExpired:
        return {"exit": "timeout(%ss)" % ctx.timeout_s, "tail": ""}


def put_pom(ctx, side: str, repo: str, group: str, artifact: str,
            version: str) -> dict:
    """PUT a fixture pom directly on the wire protocol (mvn sends the same
    PUT with client checksum headers; we mirror that)."""
    group_path = group.replace(".", "/")
    body = pom_fixture(group, artifact, version)
    name = "%s-%s.pom" % (artifact, version)
    path = "/%s/%s/%s/%s/%s" % (repo, group_path, artifact, version, name)
    resp = ctx.http(side, "PUT", path, body=body, headers={
        "X-Checksum-Sha1": sha1_hex(body), "X-Checksum-Md5":
            hashlib.md5(body).hexdigest(),  # noqa: S324 - Maven wire convention
    })
    return {"path": path, "status": resp["status"]}


# ------------------------------------------------------------------ verdict

def judge(per_side: dict, expected: dict):
    """Four-state verdict from per-side assertion values.

    PASS iff every assertion equals the spec-derived expected value on BOTH
    sides. Any single-side deviation from expectation is FAIL (a_spec /
    b_spec_violations); equal-but-unexpected pairs are also FAIL (identical
    wrong behaviour is still a spec miss worth surfacing). A-vs-B only
    differences are listed as ab_divergence.
    """
    a, b = per_side.get("a", {}), per_side.get("b", {})
    a_viol = {k: v for k, v in a.items() if expected.get(k) != v}
    b_viol = {k: v for k, v in b.items() if expected.get(k) != v}
    ab_div = {k: {"a": a[k], "b": b[k]} for k in a if k in b and a[k] != b[k]}
    if not a_viol and not b_viol and not ab_div:
        return "PASS", None, {"a_violations": {}, "b_violations": {},
                              "ab_divergence": {}}
    parts = []
    if a_viol:
        parts.append("a_spec_violations=%s" % json.dumps(a_viol, sort_keys=True))
    if b_viol:
        parts.append("b_spec_violations=%s" % json.dumps(b_viol, sort_keys=True))
    if ab_div:
        parts.append("ab_divergence=%s" % json.dumps(ab_div, sort_keys=True))
    return "FAIL", "; ".join(parts), {"a_violations": a_viol,
                                      "b_violations": b_viol,
                                      "ab_divergence": ab_div}


def get_sha_verdict(ctx, side: str, path: str, expect_sha256: str) -> str:
    """Derived value for a 'GET must return the exact bytes' assertion."""
    resp = ctx.http(side, "GET", path)
    if resp["status"] != 200:
        return "status=%d" % resp["status"]
    got = sha256_hex(resp["body"])
    return "200+sha256-ok" if got == expect_sha256 else "200+sha_mismatch"


def scratch_dir() -> str:
    return tempfile.mkdtemp(prefix="difftest-mvn-")


def rm_scratch(path: str):
    shutil.rmtree(path, ignore_errors=True)
