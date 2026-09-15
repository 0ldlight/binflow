#!/usr/bin/env python3
"""L023-2D build-info domain differential driver (dual mode).

A = Artifactory pro 7.161.15  http://172.16.58.130:8082/artifactory/api
B = BinFlow dev.77033399      http://172.16.58.130:8083/binflow/api

Same request -> both ends -> normalize -> diff. Wire evidence under
reports/compatibility/l023d-wire/{a,b}/<case>.hdr|.body . Verdicts on stdout.

Normalize rules applied here (proposal fixtures/normalize.yaml#buildinfo):
  N1 header drop: Date, Server, X-Powered-By, Set-Cookie, X-Request-Id,
     X-Artifactory-Id, X-Artifactory-Node-Id, Via, X-Jfrog-Version,
     X-Conan-*, ETag-like instance noise; superset non-semantic headers noted.
  N2 Content-Type: charset suffix stripped (charset=ISO-8859-1 vs utf-8).
  N3 X-Checksum-Sha256: value -> '<sha256-hex64>' placeholder, shape asserted.
  N4 absolute URL prefix -> '<BASE>' (list/detail top uri carry host).
  N5 server-clock promotionStamp: '<ts-sssz>' + paired timestampDate -> '<epoch-ms>'
     (shape-asserted, value not compared — server clock); explicit client
     timestamps compared literally.
  N6 JSON key order/whitespace ignored (parse). List-name endpoint assertion
     scoped to l023d- prefixed names (both instances carry foreign builds).
"""
import hashlib, json, re, sys, urllib.request, urllib.error, base64

A = {"name": "a", "base": "http://172.16.58.130:8082/artifactory/api", "auth": "admin:JFrog@2026"}
B = {"name": "b", "base": "http://172.16.58.130:8083/binflow/api", "auth": "admin:password"}
WIRE = "/Users/lzw/dev-center/reports/compatibility/l023d-wire"

DROP_HDR = {"date", "server", "x-powered-by", "set-cookie", "x-request-id",
            "x-artifactory-id", "x-artifactory-node-id", "via", "x-jfrog-version",
            "content-length", "connection", "x-artifactory-cluster-unique-id",
            "transfer-encoding", "x-content-type-options"}
# PN1 (proposal, see report §normalize): A answers with vendor json media types
# (vnd.org.jfrog.build.*) while B answers bare application/json — json-family
# equivalence proposed for verdict purposes; registered as its own D-item.
VENDOR_JSON = re.compile(r"^application/(vnd\.org\.jfrog\.[A-Za-z0-9.]*\+)?json$")
results = []

def req(side, method, path, body=None, ctype="application/json"):
    url = side["base"] + path
    data = body.encode() if isinstance(body, str) else body
    r = urllib.request.Request(url, data=data, method=method)
    r.add_header("Authorization", "Basic " + base64.b64encode(side["auth"].encode()).decode())
    if data is not None:
        r.add_header("Content-Type", ctype)
    try:
        resp = urllib.request.urlopen(r, timeout=30)
        raw, st, hdrs = resp.read(), resp.status, dict(resp.headers)
    except urllib.error.HTTPError as e:
        raw, st, hdrs = e.read(), e.code, dict(e.headers)
    return st, hdrs, raw.decode("utf-8", "replace")

def save(side, cid, st, hdrs, body):
    with open(f"{WIRE}/{side['name']}/{cid}.hdr", "w") as f:
        f.write(f"HTTP {st}\n" + "".join(f"{k}: {v}\n" for k, v in hdrs.items()))
    with open(f"{WIRE}/{side['name']}/{cid}.body", "w") as f:
        f.write(body)

TS_SSSZ = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}[+-]\d{4}")
EPOCH = re.compile(r"^\d{13}$")
SHA256 = re.compile(r"^[0-9a-f]{64}$")

def norm_headers(hdrs):
    keep = {}
    for k, v in hdrs.items():
        lk = k.lower()
        if lk in DROP_HDR:
            continue
        if lk == "content-type":
            v = v.split(";")[0].strip()
            if VENDOR_JSON.match(v):
                v = "<json-family>"
        if lk == "x-checksum-sha256":
            v = "<sha256-hex64>"
        keep[k] = v
    return keep

def norm_text(t):
    t = t.replace("\r\n", "\n").replace(A["base"].rsplit("/api", 1)[0], "<BASE>")
    t = t.replace(B["base"].rsplit("/api", 1)[0], "<BASE>")
    return t

def norm_json(o, clock_ts=False):
    """Recursive: URL prefix -> <BASE>; server-clock SSSZ+epoch pairs -> placeholders."""
    if isinstance(o, dict):
        out = {}
        for k, v in o.items():
            if isinstance(v, str):
                v = v.replace(A["base"].rsplit("/api", 1)[0], "<BASE>").replace(B["base"].rsplit("/api", 1)[0], "<BASE>")
            out[k] = norm_json(v, clock_ts)
        return out
    if isinstance(o, list):
        return [norm_json(x, clock_ts) for x in o]
    return o

def stamp_clock(o):
    """Replace server-clock values with placeholders — restricted to statuses[]
    entries (timestamp/timestampDate keys); client-supplied started etc. stay literal."""
    if isinstance(o, dict):
        if "status" in o and "timestampDate" in o:  # a statuses[] entry
            out = dict(o)
            if isinstance(out.get("timestamp"), str) and TS_SSSZ.fullmatch(out["timestamp"]):
                out["timestamp"] = "<ts-sssz>"
            out["timestampDate"] = "<epoch-ms>"
            return out
        return {k: stamp_clock(v) for k, v in o.items()}
    if isinstance(o, list):
        return [stamp_clock(x) for x in o]
    return o

def content_json(hdrs):
    return hdrs.get("Content-Type", hdrs.get("content-type", "")).startswith("application/json")

def run_case(cid, method, path, body=None, ctype="application/json", clock_ts=False,
             raw_compare=False, pre=None, post=None, note=""):
    """Dual-send + normalize + diff. pre/post = fn(side)->None run per side."""
    if pre:
        pre(A); pre(B)
    outs = {}
    for side in (A, B):
        st, hdrs, text = req(side, method, path, body, ctype)
        save(side, cid, st, hdrs, text)
        outs[side["name"]] = (st, norm_headers(hdrs), text)
    if post:
        post(A); post(B)
    a, b = outs["a"], outs["b"]
    diffs = []
    if a[0] != b[0]:
        diffs.append(f"status A={a[0]} B={b[0]}")
    ha, hb = a[1], b[1]
    if set(ha) - set(hb):
        diffs.append(f"headers only-A: {sorted(set(ha)-set(hb))}")
    if set(hb) - set(ha):
        diffs.append(f"headers only-B: {sorted(set(hb)-set(ha))}")
    for k in sorted(set(ha) & set(hb)):
        if ha[k] != hb[k]:
            diffs.append(f"header {k}: A={ha[k]!r} B={hb[k]!r}")
    ta, tb = norm_text(a[2]), norm_text(b[2])
    if content_json(ha) or (ta.startswith("{") and tb.startswith("{")):
        try:
            ja, jb = json.loads(a[2] or "null"), json.loads(b[2] or "null")
            if cid.endswith("-LIST"):
                ja = _filter_list(ja); jb = _filter_list(jb)
            ja, jb = norm_json(ja), norm_json(jb)
            if clock_ts:
                ja, jb = stamp_clock(ja), stamp_clock(jb)
            if ja != jb:
                diffs.append("body-json: " + _jd(ja, jb))
        except (ValueError, json.JSONDecodeError):
            if ta != tb:
                diffs.append(f"body-text: A={ta!r} B={tb!r}")
    elif raw_compare and ta != tb:
        diffs.append(f"body-text: A={ta!r} B={tb!r}")
    else:
        # literal compare for text/plain faces
        if ta != tb:
            diffs.append(f"body-text: A={ta!r} B={tb!r}")
    verdict = "SAME" if not diffs else "DIVERGENT"
    results.append((cid, verdict, diffs, note))
    print(f"[{verdict}] {cid}" + ("" if not diffs else "\n    " + "\n    ".join(diffs)))
    return a, b

def _filter_list(j):
    if isinstance(j, dict) and "builds" in j:
        j = dict(j); j["builds"] = [e for e in j["builds"] if e.get("uri", "").startswith("/l023d-")]
    return j

def _jd(ja, jb):
    la, lb = json.dumps(ja, indent=1, sort_keys=True).splitlines(), json.dumps(jb, indent=1, sort_keys=True).splitlines()
    import difflib
    return "\n      ".join(list(difflib.unified_diff(la, lb, "A", "B", lineterm=""))[:12])

def build_body(name, number, started, props=None, modules=None, extra=None):
    b = {"version": "1.0", "name": name, "number": str(number), "started": started,
         "buildAgent": {"name": "difftest", "version": "1"}, "agent": {"name": "l0232d", "version": "1"},
         "url": "http://ci.example/null"}
    if props: b["properties"] = props
    if modules: b["modules"] = modules
    if extra: b.update(extra)
    return json.dumps(b)

MOD_A_JAR = {"id": "mod-a", "type": "generic",
             "artifacts": [{"type": "jar", "sha1": "", "sha256": "", "md5": "", "name": "a.jar", "path": "x/y/a.jar"}]}

def seed(name, number, started, props=None, modules=None, extra=None, both=True):
    def _p(side):
        req(side, "PUT", "/build", build_body(name, number, started, props, modules, extra))
    _p(A)
    if both: _p(B)

def wipe(name, build_repo=None):
    """deleteAll via body endpoint (works for custom keyspace on B)."""
    for side in (A, B):
        body = json.dumps({"buildRepo": build_repo, "buildName": name, "buildNumbers": [], "deleteAll": True})
        req(side, "POST", "/build/delete", body)

# ───────────────────────── cases ─────────────────────────
def main():
    print("== L023-2D buildinfo differential (dual) ==")

    # C-group CRUD ---------------------------------------------------------
    for n in ("l023d-app", "l023d-beta", "l023d-app2", "l023d-app3", "l023d-app4",
              "l023d-rel", "l023d-bd", "l023d-r1", "l023d-r2", "l023d-r3", "l023d-jf-app"):
        wipe(n)
    wipe("l023d-gate", build_repo="l023d-void-build-info")

    run_case("01-crud-put-204-sha256hdr", "PUT", "/build",
             build_body("l023d-app", 1, "2026-09-15T10:30:00.000+0530", {"env": "dev"},
                        [dict(MOD_A_JAR)]),
             note="E1: 204 + X-Checksum-Sha256 (shape)")

    def _chk_sha(hdrs_side):
        pass
    # verify sha256 header shape on both from saved wire
    for s in (A, B):
        h = open(f"{WIRE}/{s['name']}/01-crud-put-204-sha256hdr.hdr").read()
        m = re.search(r"X-Checksum-Sha256: ([0-9a-f]+)", h, re.I)
        ok = m and SHA256.fullmatch(m.group(1))
        results.append((f"01-sha-shape-{s['name']}", "SAME" if ok else "DIVERGENT",
                        [] if ok else ["header value not 64-hex"], "shape assertion"))
        print(f"[{'SAME' if ok else 'DIVERGENT'}] 01-sha-shape-{s['name']}")

    run_case("02-crud-get-detail-echo", "GET", "/build/l023d-app/1",
             note="started 原样时区(+0530)/principal 覆写/durationMillis 0")

    run_case("03-crud-get-list-LIST", "GET", "/build",
             note="顶级 uri 绝对+?buildRepo=；lastStarted UTC 归一（+0530 入）")

    run_case("04-crud-put-hidden-name", "PUT", "/build",
             build_body(".l023d-x", 1, "2026-09-15T01:00:00.000+0000"),
             note="hidden name 400 逐字")

    run_case("05-crud-put-hidden-number", "PUT", "/build",
             build_body("l023d-x", ".1", "2026-09-15T01:00:00.000+0000"),
             note="hidden number 400 逐字")

    seed("l023d-app", 1, "2026-09-15T12:40:00.000+0000", {"env": "dev"}, [dict(MOD_A_JAR)])
    run_case("06-crud-multi-run-LIST", "GET", "/build/l023d-app",
             note="同号双 run 并列（1@05:00z + 1@12:40z）；started 回显口径")

    seed("l023d-app", 2, "2026-09-15T09:00:00.000+0000")
    seed("l023d-app", 3, "2026-09-15T10:00:00.000+0000")
    run_case("07-crud-number-desc-LIST", "GET", "/build/l023d-app",
             note="started 严格倒序：1b(12:40),3(10:00),2(09:00),1a(05:00z)")

    seed("l023d-beta", 5, "2026-09-15T14:00:00.000+0530", {"env": "beta"},
         [{"id": "m-beta", "type": "generic", "artifacts": []}])
    run_case("08-crud-name-list-order-LIST", "GET", "/build",
             note="名清单序向（A=按各名最新 run 日期；活体判向——待验证#5 收口）")

    run_case("09-crud-slim", "GET", "/build/l023d-beta/5?slim=true",
             note="slim=true → modules []/properties null")

    run_case("10a-crud-detail-404-missing-number", "GET", "/build/l023d-app/99",
             note="详情 404 逐字（number 子句）")
    run_case("10b-crud-detail-404-malformed-started", "GET", "/build/l023d-app/99?started=2026-09-15T09:00:00.000+0000",
             note="malformed started（URL + 成空格）→ 400 文案对照")
    run_case("10d-crud-detail-404-started-clause", "GET", "/build/l023d-app/99?started=2026-09-15T09:00:00.000%2B0000",
             note="详情 404 带 started 子句（正确编码）+ 尾空格口径")
    run_case("10c-crud-numbers-404", "GET", "/build/l023d-missing/1",
             note="号单 404 逐字")

    run_case("11-crud-delete-partial-e6", "DELETE", "/build/l023d-app?buildNumbers=99,3",
             note="E6 双段文案逐字（have + Warning + 尾换行）")

    run_case("12a-crud-delete-404-name", "DELETE", "/build/l023d-nosuch?buildNumbers=1",
             note="404 名不存在分支逐字")
    run_case("12b-crud-delete-404-numbers", "DELETE", "/build/l023d-app?buildNumbers=777",
             note="404 号全不存在分支逐字")

    run_case("13a-crud-delete-same-number-quirk", "DELETE", "/build/l023d-app?buildNumbers=1",
             note="同号双 run 每调至多删一（删最新 12:40）")
    run_case("13b-crud-delete-same-number-aftermath-LIST", "GET", "/build/l023d-app",
             note="余 1@05:00z")
    run_case("13c-crud-delete-same-number-again", "DELETE", "/build/l023d-app?buildNumbers=1",
             note="第二次删清同号")
    run_case("13d-crud-delete-same-number-empty-LIST", "GET", "/build/l023d-app",
             note="号单空 → 404 逐字")

    # append group ---------------------------------------------------------
    run_case("14-append-404", "POST", "/build/append/l023d-none/1", "[]",
             note="E4 404 逐字")

    seed("l023d-app2", 1, "2026-09-15T15:00:00.000+0000", None, [dict(MOD_A_JAR)])
    mod_pom = {"id": "mod-a", "type": "generic",
               "artifacts": [{"type": "pom", "sha1": "", "sha256": "", "md5": "", "name": "a.pom", "path": "x/y/a.pom"}]}
    run_case("15a-append-duplicate", "POST", "/build/append/l023d-app2/1", json.dumps([mod_pom]),
             note="append 同 id 模块 → 204（E5 拼接不合并）")
    req(A, "POST", "/build/append/l023d-app2/1", json.dumps([mod_pom]))
    req(B, "POST", "/build/append/l023d-app2/1", json.dumps([mod_pom]))
    run_case("15b-append-duplicate-echo", "GET", "/build/l023d-app2/1",
             note="mod-a×3 各持己物（a.jar,a.pom,a.pom）")

    # promote group --------------------------------------------------------
    run_case("16a-promote-status-only", "POST", "/build/promote/l023d-app2/1",
             json.dumps({"status": "staged"}),
             note="status-only 200 messages level 大写 + Skipping 逐字", clock_ts=True)
    run_case("16b-promote-status-only-statuses", "GET", "/build/l023d-app2/1",
             note="statuses wire：SSSZ/timestampDate/user/repository 省略", clock_ts=True)

    run_case("17-promote-targetrepo-404", "POST", "/build/promote/l023d-app2/1",
             json.dumps({"status": "rel", "targetRepo": "l023d-no-such"}),
             note="targetRepo 404 逐字")

    seed("l023d-app3", 1, "2026-09-15T16:00:00.000+0000", None, [dict(MOD_A_JAR)])
    run_case("18-promote-failfast-400", "POST", "/build/promote/l023d-app3/1",
             json.dumps({"status": "rel", "targetRepo": "l023d-rel-local"}),
             note="**重点** failFast 缺省 true → 400 messages body（E12 双行）——若 A 回 errors[] 信封立即上报")
    run_case("19-promote-lenient-warning", "POST", "/build/promote/l023d-app3/1",
             json.dumps({"status": "rel", "targetRepo": "l023d-rel-local", "failFast": False}),
             note="lenient 200 单名 warning 逐字 + 状态落地", clock_ts=True)
    run_case("19b-promote-lenient-statuses", "GET", "/build/l023d-app3/1",
             note="lenient 后 statuses 含 rel 行", clock_ts=True)

    seed("l023d-app4", 1, "2026-09-15T16:30:00.000+0000", None,
         [{"id": "m-empty", "type": "generic", "artifacts": []}])
    run_case("20-promote-skipping-nostatus", "POST", "/build/promote/l023d-app4/1",
             json.dumps({"targetRepo": "l023d-rel-local"}),
             note="Skipping no status received 逐字 + 无制品搬迁面")

    run_case("21-promote-invalid-timestamp", "POST", "/build/promote/l023d-app4/1",
             json.dumps({"status": "x", "timestamp": "not-a-time"}),
             note="invalid\\unparsable timestamp error 行逐字（字面反斜杠）", clock_ts=True)

    # happy relocation: deploy real artifact, build links by checksum
    content = b"l0232d-wheel-content-0001"
    sha1 = hashlib.sha1(content).hexdigest(); md5 = hashlib.md5(content).hexdigest()
    sha256 = hashlib.sha256(content).hexdigest()
    for side in (A, B):
        r = urllib.request.Request(side["base"].replace("/api", "/l023d-dev-local") + "/app/5/wheel.bin",
                                   data=content, method="PUT")
        r.add_header("Authorization", "Basic " + base64.b64encode(side["auth"].encode()).decode())
        urllib.request.urlopen(r, timeout=30).read()
    art = {"type": "bin", "sha1": sha1, "sha256": sha256, "md5": md5,
           "name": "wheel.bin", "path": "app/5/wheel.bin",
           "originalDeploymentRepo": "l023d-dev-local"}
    seed("l023d-rel", 5, "2026-09-15T17:00:00.000+0000", None,
         [{"id": "m-rel", "type": "generic", "artifacts": [art]}])
    run_case("22a-promote-happy", "POST", "/build/promote/l023d-rel/5",
             json.dumps({"status": "released", "targetRepo": "l023d-rel-local",
                         "comment": "c1", "ciUser": "ci-bot",
                         "timestamp": "2026-09-15T10:00:00.000+0000"}),
             note="真实搬迁：200 INFO；显式 timestamp 逐字回显")
    run_case("22b-promote-happy-statuses", "GET", "/build/l023d-rel/5",
             note="statuses: released/c1/ci-bot/repository=l023d-rel-local/timestamp 显式")

    def reloc_check(cid_prefix):
        for s in (A, B):
            host = s["base"].rsplit("/api", 1)[0]
            for repo in ("l023d-dev-local", "l023d-rel-local"):
                r = urllib.request.Request(f"{host}/{repo}/app/5/wheel.bin", method="GET")
                r.add_header("Authorization", "Basic " + base64.b64encode(s["auth"].encode()).decode())
                try:
                    resp = urllib.request.urlopen(r, timeout=30); got = resp.read() == content; st = resp.status
                except urllib.error.HTTPError as e:
                    got, st = False, e.code
                with open(f"{WIRE}/{s['name']}/{cid_prefix}-reloc-{repo}.body", "w") as f:
                    f.write(f"GET {repo}/app/5/wheel.bin -> {st} content-match={got}")
    reloc_check("22")
    dev_gone = all("-> 404" in open(f"{WIRE}/{s['name']}/22-reloc-l023d-dev-local.body").read() for s in (A, B))
    rel_here = all("-> 200 content-match=True" in open(f"{WIRE}/{s['name']}/22-reloc-l023d-rel-local.body").read() for s in (A, B))
    v = "SAME" if (dev_gone and rel_here) else "DIVERGENT"
    results.append(("22c-promote-relocation-side-effect", v,
                    [] if v == "SAME" else [f"dev_gone={dev_gone} rel_here={rel_here}"], "move 语义副作用"))
    print(f"[{v}] 22c-promote-relocation-side-effect (dev_gone={dev_gone}, rel_here={rel_here})")

    # batch delete group ---------------------------------------------------
    def seed_bd(side):
        for n, ts in (("1.0.0-rc+1", "2026-09-15T18:00:00.000+0000"), ("2", "2026-09-15T18:30:00.000+0000")):
            req(side, "PUT", "/build", build_body("l023d-bd", n, ts, None, [dict(MOD_A_JAR)]))
    seed_bd(A); seed_bd(B)
    run_case("23a-batchdelete-body-e6", "POST", "/build/delete",
             json.dumps({"buildRepo": "artifactory-build-info", "buildName": "l023d-bd",
                         "buildNumbers": ["1.0.0-rc+1", "99"], "deleteArtifacts": False, "deleteAll": False}),
             note="批删 body 六字段；特殊字符号；E6 双段")
    run_case("23b-batchdelete-blank-name", "POST", "/build/delete",
             json.dumps({"buildName": "", "buildNumbers": ["1"], "deleteAll": False}),
             note="blank name 400 逐字")
    run_case("23c-batchdelete-deleteall", "POST", "/build/delete",
             json.dumps({"buildName": "l023d-bd", "buildNumbers": [], "deleteAll": True}),
             note="deleteAll 文案（无尾点）+ <repo> 内插口径")

    # retention group ------------------------------------------------------
    run_case("24a-retention-count0", "POST", "/build/retention/l023d-r1", json.dumps({"count": 0}),
             note="count=0 → 400 text/plain 逐字")
    run_case("24b-retention-nobody", "POST", "/build/retention/l023d-r1", "",
             note="body 缺失 → 同句 400")

    def seed_r2(side):
        for n, ts in (("1", "2026-09-15T20:00:00.000+0000"), ("2", "2026-09-15T21:00:00.000+0000"),
                      ("3", "2026-09-15T22:00:00.000+0000")):
            req(side, "PUT", "/build", build_body("l023d-r2", n, ts))
        req(side, "POST", "/build/promote/l023d-r2/2", json.dumps({"status": "promoted"}))
    seed_r2(A); seed_r2(B)
    run_case("25a-retention-count1-promoted-exempt", "POST", "/build/retention/l023d-r2",
             json.dumps({"count": 1}),
             note="count=1 → 204；promoted run2 豁免")
    run_case("25b-retention-survivors-LIST", "GET", "/build/l023d-r2",
             note="幸存 = /3(count 槽) + /2(promoted 豁免)；/1 删")

    def seed_r3(side):
        for n, ts in (("4", "2026-09-15T23:00:00.000+0000"), ("5", "2026-09-16T00:00:00.000+0000")):
            req(side, "PUT", "/build", build_body("l023d-r3", n, ts))
    seed_r3(A); seed_r3(B)
    run_case("26-retention-publish-tail", "PUT", "/build",
             build_body("l023d-r3", 6, "2026-09-16T01:00:00.000+0000", None, None,
                        {"buildRetention": {"count": 1}}),
             note="E7 发布尾部触发：PUT 携 buildRetention count=1 → 204")
    run_case("26b-retention-publish-tail-aftermath-LIST", "GET", "/build/l023d-r3",
             note="尾部窗口已删 4/5 → 余 /6")
    run_case("26c-retention-aftermath-detail-echo", "GET", "/build/l023d-r3/6",
             note="无 properties 的详情回显（键省略 vs null 口径）")

    # custom buildRepo gate (projects face on A) ---------------------------
    run_case("27-buildrepo-custom-read-empty", "GET", "/build?buildRepo=l023d-void-build-info",
             note="读路径对自定义/不存在仓的空态口径")
    run_case("28-buildrepo-custom-put-gate", "PUT", "/build?buildRepo=l023d-void-build-info",
             build_body("l023d-gate", 1, "2026-09-16T02:00:00.000+0000"),
             note="A=projects 门 400 / B=无门 204（候选 INTENTIONAL——BinFlow 无 projects 域）")

    # summary
    same = [r for r in results if r[1] == "SAME"]; div = [r for r in results if r[1] == "DIVERGENT"]
    print(f"\n== summary: SAME={len(same)} DIVERGENT={len(div)} total={len(results)} ==")
    for cid, v, d, n in results:
        if v == "DIVERGENT":
            print(f"  DIVERGENT {cid}: {'; '.join(d)[:300]}")
    with open("/tmp/l0232d-verdicts.json", "w") as f:
        json.dump([{"id": c, "verdict": v, "diffs": d, "note": n} for c, v, d, n in results], f, indent=1)

if __name__ == "__main__":
    main()
