#!/usr/bin/env python3
"""L024-10 D01 tail differential (ticket B: metadata PATCH/DELETE + POST 405;
ticket C: archive!/ content face). Dual mode.

A = Artifactory pro 7.161.15  http://172.16.58.130:8082/artifactory
B = BinFlow dev.fbf72388      http://172.16.58.130:8083/binflow

Normalize: header drop set + json-family equivalence (as buildinfo/search);
absolute URL host -> <BASE> (archive miss URI carries contextPath
/artifactory vs /binflow — the context token itself is preserved).
"""
import base64, hashlib, json, re, urllib.request, urllib.error

A = {"name": "a", "base": "http://172.16.58.130:8082/artifactory", "auth": "admin:JFrog@2026"}
B = {"name": "b", "base": "http://172.16.58.130:8083/binflow", "auth": "admin:password"}
WIRE = "/Users/lzw/dev-center/reports/compatibility/l024e-wire"
DROP = {"date", "server", "x-powered-by", "set-cookie", "x-request-id", "x-artifactory-id",
        "x-artifactory-node-id", "via", "x-jfrog-version", "content-length", "connection",
        "transfer-encoding", "x-content-type-options", "etag", "last-modified", "location",
        "x-explode-archive", "x-binflow-exploded-files"}
VENDOR = re.compile(r"^application/(vnd\.org\.jfrog\.[A-Za-z0-9.]*\+)?json$")
results = []

def req(side, method, path, body=None, ctype="application/json", auth=None):
    url = side["base"] + path
    data = body.encode() if isinstance(body, str) else body
    r = urllib.request.Request(url, data=data, method=method)
    r.add_header("Authorization", "Basic " + base64.b64encode((auth or side["auth"]).encode()).decode())
    if data is not None:
        r.add_header("Content-Type", ctype)
    try:
        resp = urllib.request.urlopen(r, timeout=30)
        raw, st, hdrs = resp.read(), resp.status, dict(resp.headers)
    except urllib.error.HTTPError as e:
        raw, st, hdrs = e.read(), e.code, dict(e.headers)
    return st, hdrs, raw

def save(side, cid, st, hdrs, raw):
    with open(f"{WIRE}/{side['name']}/{cid}.hdr", "w") as f:
        f.write(f"HTTP {st}\n" + "".join(f"{k}: {v}\n" for k, v in hdrs.items()))
    with open(f"{WIRE}/{side['name']}/{cid}.body", "wb") as f:
        f.write(raw)

def norm_headers(h):
    out = {}
    for k, v in h.items():
        lk = k.lower()
        if lk in DROP:
            continue
        if lk == "content-type":
            v = v.split(";")[0].strip()
            if VENDOR.match(v):
                v = "<json-family>"
        out[k] = v
    return out

def run(cid, method, path, body=None, ctype="application/json", auth=None, note="", binary=False):
    outs = {}
    for side in (A, B):
        st, hdrs, raw = req(side, method, path, body, ctype, auth)
        save(side, cid, st, hdrs, raw)
        outs[side["name"]] = (st, norm_headers(hdrs), raw)
    a, b = outs["a"], outs["b"]
    diffs = []
    if a[0] != b[0]:
        diffs.append(f"status A={a[0]} B={b[0]}")
    ha, hb = a[1], b[1]
    if set(ha) - set(hb):
        diffs.append(f"hdr only-A {sorted(set(ha)-set(hb))}")
    if set(hb) - set(ha):
        diffs.append(f"hdr only-B {sorted(set(hb)-set(ha))}")
    for k in sorted(set(ha) & set(hb)):
        if ha[k] != hb[k]:
            diffs.append(f"hdr {k}: A={ha[k]!r} B={hb[k]!r}")
    ta = a[2].decode("utf-8", "replace").replace("\r\n", "\n")
    tb = b[2].decode("utf-8", "replace").replace("\r\n", "\n")
    if binary:
        if a[2] != b[2]:
            diffs.append(f"body-bytes differ (A={len(a[2])}B B={len(b[2])}B)")
    else:
        ta = ta.replace(A["base"], "<BASE>").replace(B["base"], "<BASE>")
        tb = tb.replace(A["base"], "<BASE>").replace(B["base"], "<BASE>")
        try:
            ja, jb = json.loads(a[2] or "null"), json.loads(b[2] or "null")
            if ja != jb:
                diffs.append(f"body-json A={json.dumps(ja)[:220]} B={json.dumps(jb)[:220]}")
        except ValueError:
            if ta != tb:
                diffs.append(f"body-text A={ta[:220]!r} B={tb[:220]!r}")
    verdict = "SAME" if not diffs else "DIVERGENT"
    results.append((cid, verdict, diffs, note))
    print(f"[{verdict}] {cid}" + ("" if not diffs else "\n    " + "\n    ".join(diffs)))
    return outs

def main():
    REPO = "l024e-local"; VIRT = "l024e-virt"
    api = lambda p: "/api" + p
    # ── fixtures ───────────────────────────────────────────────
    for side in (A, B):
        req(side, "PUT", api(f"/repositories/{REPO}"), json.dumps({"rclass": "local", "packageType": "generic"}))
        req(side, "PUT", api(f"/repositories/{VIRT}"), json.dumps(
            {"rclass": "virtual", "packageType": "generic", "repositories": [REPO]}))
        req(side, "PUT", f"/{REPO}/f1/target.bin", b"l024e-target-v1")
        req(side, "PUT", f"/{REPO}/f1/other.bin", b"l024e-other-v1", )
        zdata = open("/tmp/l024e.zip", "rb").read()
        req(side, "PUT", f"/{REPO}/pkg/l024e.zip", zdata)
    # seed one property for overwrite arms
    for side in (A, B):
        req(side, "PUT", f"/{REPO}/f1/target.bin;seed=old", b"l024e-target-v1")

    P = lambda body: json.dumps(body)
    # ── B-family: PATCH /api/metadata ─────────────────────────
    run("m01-patch-new-array", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({"props": {"k": ["v1", "v2"]}}),
        note="§3.1-1 新键数组")
    run("m01b-verify", "GET", api(f"/storage/{REPO}/f1/target.bin?properties"), note="k=[v1,v2]")
    run("m02-patch-overwrite", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({"props": {"k": ["v3"]}}),
        note="§3.1-2 覆盖语义")
    run("m02b-verify", "GET", api(f"/storage/{REPO}/f1/target.bin?properties"), note="k=[v3]；seed 保留")
    run("m03-patch-null-delete", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({"props": {"k": None}}),
        note="§3.1-3 null 删键")
    run("m03b-patch-null-idempotent", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({"props": {"k": None}}),
        note="键不存在亦 204")
    run("m04-patch-empty-body", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({}),
        note="§3.1-4 props or stats fields required 逐字")
    run("m05-patch-nonstring-value", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({"props": {"n": 5}}),
        note="§3.1-5 parse 400 逐字含句号")
    run("m05b-patch-object-value", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({"props": {"n": {"x": 1}}}),
        note="对象值同 parse 400")
    run("m06-patch-missing-item", "PATCH", api(f"/metadata/{REPO}/f1/none.bin"), P({"props": {"k": ["v"]}}),
        note="§3.1-6 400 非 404 逐字（repo:path 冒号）")
    run("m07-patch-virtual-repo", "PATCH", api(f"/metadata/{VIRT}/f1/target.bin"), P({"props": {"k": ["v"]}}),
        note="§3.1-12 virtual 400 逐字")
    run("m08-patch-nontext-elements", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({"props": {"m": ["v1", 5, "v2"]}}),
        note="§3.1-7 非文本元素静默跳过")
    run("m08b-verify", "GET", api(f"/storage/{REPO}/f1/target.bin?properties"), note="m=[v1,v2]")
    run("m09-patch-stats-only", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({"stats": {"downloadCount": 1}}),
        note="§3.1-10 stats 腿（B=no-op 204；A 真值待定谳）")
    run("m09b-stats-side-effect", "GET", api(f"/storage/{REPO}/f1/target.bin?stats"),
        note="stats 腿副作用（downloadCount 变化与否）")
    # ── B-family: DELETE /api/metadata ────────────────────────
    run("m10-delete-all", "DELETE", api(f"/metadata/{REPO}/f1/target.bin"), note="全删 204")
    run("m10b-delete-idempotent", "DELETE", api(f"/metadata/{REPO}/f1/other.bin"), note="无属性 204 幂等")
    run("m10c-delete-missing-item", "DELETE", api(f"/metadata/{REPO}/f1/none.bin"),
        note="**低置信①** DELETE 失败文案（A 真值）")
    run("m10d-delete-virtual", "DELETE", api(f"/metadata/{VIRT}/f1/target.bin"),
        note="**低置信①** DELETE virtual 仓文案")
    # ── B-family: POST 405 + anon + 其它动词 ──────────────────
    run("m12-post-storage-bare", "POST", api(f"/storage/{REPO}/f1/target.bin"), "",
        note="POST /api/storage 405 逐字（裸）")
    run("m12b-post-storage-query", "POST", api(f"/storage/{REPO}/f1/target.bin?recursive=true&atomic=true"), "",
        note="405（query 臂——方法选择先于 query）")
    run("m12c-post-storage-noarg-root", "POST", api("/storage"), "",
        note="无仓段 POST（L010-2 404 Not Found 面）")
    run("m13-patch-anon", "PATCH", api(f"/metadata/{REPO}/f1/target.bin"), P({"props": {"k": ["v"]}}),
        auth="anon:anon", note="匿名 401")
    run("m14a-put-metadata-verb", "PUT", api(f"/metadata/{REPO}/f1/target.bin"), "",
        note="**低置信④** 其它动词 PUT（A 真值 vs B E-26）")
    run("m14b-get-metadata-verb", "GET", api(f"/metadata/{REPO}/f1/target.bin"),
        note="**低置信④** GET（A 真值）")
    run("m14c-post-metadata-verb", "POST", api(f"/metadata/{REPO}/f1/target.bin"), "",
        note="**低置信④** POST（A 真值）")
    # ── C-family: archive!/ ───────────────────────────────────
    run("a01-member-hit", "GET", f"/{REPO}/pkg/l024e.zip!/entry.txt", binary=True,
        note="§3.3-1 成员命中 200+成员 CT+字节")
    run("a02-nested-member-hit", "GET", f"/{REPO}/pkg/l024e.zip!/nested/deep.txt", binary=True,
        note="嵌套成员命中")
    run("a03-member-miss", "GET", f"/{REPO}/pkg/l024e.zip!/nope.txt",
        note="§3.3-2 miss 404 全文（full URI 含 context + Path 尾段）")
    run("a04-bang-no-slash", "GET", f"/{REPO}/pkg/l024e.zip!entry.txt",
        note="**低置信③** `!` 无斜杠 miss（generic 仓文案族）")
    run("a05-member-sha1", "GET", f"/{REPO}/pkg/l024e.zip!/entry.txt.sha1", binary=True,
        note="§3.3-4 成员 checksum 后缀")

    same = sum(1 for r in results if r[1] == "SAME")
    print(f"\n== summary: SAME={same} DIVERGENT={len(results)-same} total={len(results)} ==")
    for cid, v, d, n in results:
        if v == "DIVERGENT":
            print(f"  {cid}: {'; '.join(d)[:300]}")
    with open("/tmp/l02410-verdicts.json", "w") as f:
        json.dump([{"id": c, "verdict": v, "diffs": d, "note": n} for c, v, d, n in results], f, indent=1)

if __name__ == "__main__":
    main()
