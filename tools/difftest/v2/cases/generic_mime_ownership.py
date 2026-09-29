"""L036 / R7 followup probe 1 (T-564 followup ①) — generic mimeType
ownership mini-diff. FORENSICS + ruling material: the observed split
(T-564 log Followups ①: curl --data-binary PUT carries
Content-Type: application/x-www-form-urlencoded -> A answers
mimeType=application/octet-stream, B echoes the declared type verbatim)
is widened into an 18-leg matrix crossing declared-CT (absent / curl
default / explicit family / charset-param / custom) x path extension
(none / .txt / .json / .xml / .jar / .bin) x body content (text / json /
xml / binary). Conductor rules ownership; this case only produces the
matrix (probe posture: PASS = every leg captured complete on both sides,
divergent legs are the payload, not a failure).

Faces per leg: (1) PUT wire response (status / Location header / envelope
uri+downloadUri — also feeds the render-point audit), (2) GET the artifact
back (status / served Content-Type / body sha256), (3) FileInfo
/api/storage/<repo>/<path> mimeType field.

Wire fidelity: PUT legs ride curl subprocesses so the declared-CT is exact
(-H 'Content-Type:' strips the header; urllib would force
x-www-form-urlencoded on every body). Credentials ride a 0600 netrc file,
never argv.
"""
import importlib.util
import json
import os
import subprocess
import tempfile
import urllib.parse

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "generic-mime-ownership",
    "title": "FORENSICS (R7 followup 1): generic deploy mimeType ownership "
             "— declared-CT x extension x content, 18 legs, PUT/GET/FileInfo "
             "faces, A(7.161.26) vs B(HEAD)",
    "layer": "L3",
    "domain": "generic",
    "auth": True,
    "timeout_s": 240,
}

REPO = "difftest-r7fp-generic"

TEXT = b"hello-r7fp-mime\n"
JSON = b'{"k": "v", "n": 7}\n'
XML = b'<a b="c">d</a>\n'
BIN = b"\x00\x01\x02\xfe\xffBIN-r7fp\n"

# (slug, path, ct, body): ct None = header stripped; "auto" = curl's
# --data-binary default (x-www-form-urlencoded); any other string = the
# exact declared header.
LEGS = [
    ("n-abs-text",       "r7fp/noext/n-abs-text",       None,                                   TEXT),
    ("n-auto-text",      "r7fp/noext/n-auto-text",      "auto",                                 TEXT),
    ("n-formurl-text",   "r7fp/noext/n-formurl-text",   "application/x-www-form-urlencoded",    TEXT),
    ("n-textplain",      "r7fp/noext/n-textplain",      "text/plain",                           TEXT),
    ("n-json",           "r7fp/noext/n-json",           "application/json",                     JSON),
    ("n-xml",            "r7fp/noext/n-xml",            "application/xml",                      XML),
    ("n-octet-bin",      "r7fp/noext/n-octet-bin",      "application/octet-stream",             BIN),
    ("n-custom-bin",     "r7fp/noext/n-custom-bin",     "application/x-r7fp-custom",            BIN),
    ("n-charset",        "r7fp/noext/n-charset",        "text/plain; charset=utf-8",            TEXT),
    ("n-abs-bin",        "r7fp/noext/n-abs-bin",        None,                                   BIN),
    ("e-txt-abs",        "r7fp/ext/e.txt",              None,                                   TEXT),
    ("e-txt-json",       "r7fp/ext/e-txt-json.txt",     "application/json",                     TEXT),
    ("e-json-abs",       "r7fp/ext/e.json",             None,                                   JSON),
    ("e-json-textplain", "r7fp/ext/e-json-textplain.json", "text/plain",                         JSON),
    ("e-xml-abs",        "r7fp/ext/e.xml",              None,                                   XML),
    ("e-jar-abs",        "r7fp/ext/e.jar",              None,                                   BIN),
    ("e-bin-abs",        "r7fp/ext/e.bin",              None,                                   BIN),
    ("e-txt-abs-bin",    "r7fp/ext/e-txt-abs-bin.txt",  None,                                   BIN),
]

HDR_OCTET = "application/octet-stream"


def _hdr(lines, name):
    for ln in lines:
        if ln.lower().startswith(name.lower() + ":"):
            return ln.split(":", 1)[1].strip()
    return None


def _curl_put(base, path, body_file, ct, netrc, timeout_s):
    """PUT with exact CT control; returns dict(status, location, body)."""
    url = "%s/%s/%s" % (base, REPO, path)
    cmd = ["curl", "-sS", "-m", str(int(timeout_s)), "--netrc-file", netrc,
           "-X", "PUT", "--data-binary", "@" + body_file,
           "-D", "-", url]           # headers to stdout, body follows
    if ct == "auto":
        pass                          # curl's own default applies
    elif ct is None:
        cmd += ["-H", "Content-Type:"]  # strip the default -> absent
    else:
        cmd += ["-H", "Content-Type: " + ct]
    try:
        p = subprocess.run(cmd, capture_output=True, timeout=timeout_s)
    except subprocess.TimeoutExpired:
        return {"status": "timeout", "location": None, "body": b""}
    out = p.stdout
    sep = out.find(b"\r\n\r\n")
    head, body = (out[:sep], out[sep + 4:]) if sep >= 0 else (b"", out)
    lines = head.decode("iso-8859-1", "replace").splitlines()
    status = None
    for ln in lines:
        if ln.startswith("HTTP/"):
            status = int(ln.split()[1]) if len(ln.split()) > 1 else None
    return {"status": status, "location": _hdr(lines, "Location"),
            "body": body}


def _leg(ctx, side):
    base = ctx.sides[side]["base"]
    work = tempfile.mkdtemp(prefix="r7mime-")
    netrc = os.path.join(work, ".netrc")
    with open(netrc, "w", encoding="utf-8") as fh:
        host = urllib.parse.urlsplit(base).hostname or "localhost"
        fh.write("machine %s login %s password %s\n" % (
            host, ctx.sides[side]["user"], ctx.sides[side]["password"]))
    os.chmod(netrc, 0o600)

    raw, asserts = {}, {}
    for slug, path, ct, body in LEGS:
        bf = os.path.join(work, slug + ".body")
        with open(bf, "wb") as fh:
            fh.write(body)
        put = _curl_put(base, path, bf, ct, netrc, ctx.timeout_s)
        entry = {"ct_declared": ct, "put_status": put["status"],
                 "put_location": put["location"],
                 "put_body": put["body"].decode("utf-8", "replace")[:500]}
        try:
            env = json.loads(put["body"].decode("utf-8", "replace"))
            entry["put_uri"] = env.get("uri")
            entry["put_downloadUri"] = env.get("downloadUri")
        except (ValueError, AttributeError):
            entry["put_uri"] = entry["put_downloadUri"] = None

        g = ctx.http(side, "GET", "/%s/%s" % (REPO, path))
        get_ct = None
        for k, v in g["headers"].items():
            if k.lower() == "content-type":
                get_ct = v
        entry["get_status"] = g["status"]
        entry["get_content_type"] = get_ct
        entry["get_sha256_ok"] = (
            mavenlib.sha256_hex(g["body"]) == mavenlib.sha256_hex(body)
            if g["status"] == 200 else None)

        fi = ctx.http(side, "GET", "/api/storage/%s/%s" % (REPO, path))
        entry["fi_status"] = fi["status"]
        if fi["status"] == 200:
            try:
                entry["fi_mimeType"] = json.loads(
                    fi["body"].decode("utf-8", "replace")).get("mimeType")
            except ValueError:
                entry["fi_mimeType"] = "parse-error"
        else:
            entry["fi_mimeType"] = None
        raw[slug] = entry
        asserts[slug] = "put=%s get=%s|%s fi=%s|%s" % (
            entry["put_status"], entry["get_status"],
            entry["get_content_type"], entry["fi_status"],
            entry["fi_mimeType"])

    for f in os.listdir(work):
        os.remove(os.path.join(work, f))
    os.rmdir(work)
    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return {"asserts": asserts, "raw": raw}


def run(ctx):
    per_side = {}
    try:
        mavenlib.ensure_repos(ctx, "a", [mavenlib.maven_local(
            REPO, packageType="generic")])
        mavenlib.ensure_repos(ctx, "b", [mavenlib.maven_local(
            REPO, packageType="generic")])
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [REPO])

    a, b = per_side.get("a", {}), per_side.get("b", {})
    matrix, diverged, incomplete = [], [], []
    for slug, _, _, _ in LEGS:
        ae, be = a.get("raw", {}).get(slug, {}), b.get("raw", {}).get(slug, {})
        ok = all(x.get(k) is not None for x in (ae, be)
                 for k in ("put_status", "get_status", "get_content_type",
                           "fi_mimeType"))
        row = {
            "leg": slug,
            "a": {"get_ct": ae.get("get_content_type"),
                  "fi_mime": ae.get("fi_mimeType"),
                  "put_loc": ae.get("put_location")},
            "b": {"get_ct": be.get("get_content_type"),
                  "fi_mime": be.get("fi_mimeType"),
                  "put_loc": be.get("put_location")},
        }
        row["match"] = (ae.get("get_content_type") == be.get("get_content_type")
                        and ae.get("fi_mimeType") == be.get("fi_mimeType"))
        matrix.append(row)
        if not ok:
            incomplete.append(slug)
        elif not row["match"]:
            diverged.append(slug)

    ctx.write_evidence("summary.json", {
        "matrix": matrix, "diverged_legs": diverged,
        "incomplete_legs": incomplete,
        "note": "FORENSICS R7 followup 1 (T-564 followup ①) probe: "
                "mimeType ownership ruling material. match=False legs are "
                "the payload, not failures; conductor rules (align vs "
                "keep). PUT faces (Location/uri) also recorded for the "
                "render-point audit."})
    status = "PASS" if not incomplete else "BLOCKED"
    reason = ("18 legs captured; %d divergent (ruling material: %s)"
              % (len(diverged), ", ".join(diverged) if diverged else "none")
              if not incomplete else
              "incomplete legs (transport/face failure): %s"
              % ", ".join(incomplete))
    return {"status": status, "reason": reason,
            "evidence": ["evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"],
                         "evidence/%s/summary.json" % CASE["id"]],
            "requests": [{"step": "18-leg CT x extension x content matrix "
                                  "x PUT/GET/FileInfo faces, both sides"}]}
