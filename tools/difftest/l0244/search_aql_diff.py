#!/usr/bin/env python3
"""L024-4 D03 search family + AQL differential driver (dual mode).

A = Artifactory pro 7.161.15  http://172.16.58.130:8082/artifactory
B = BinFlow dev.52c9ba42      http://172.16.58.130:8083/binflow

Normalize (proposal fixtures/normalize.yaml#search):
  S1 header drop set as buildinfo N1 + Content-Type json-family equivalence
     (vnd.org.jfrog.artifactory.search.*+json ≡ application/json)
  S2 absolute URL host -> <BASE> (badChecksum uri rows, AQL rows are repo-relative paths)
  S3 AQL row created/modified ISO8601-ms -> <ts> (upload clock; shape only)
  S4 checksum header/etag noise dropped; X-Checksum-* response headers on upload
     kept as shape (values differ by storage serialization? no—file bytes identical:
     X-Checksum-Md5/Sha1 compared literally; X-Checksum-Sha256 too if present)
"""
import base64, hashlib, json, re, subprocess, sys, urllib.request, urllib.error

A = {"name": "a", "base": "http://172.16.58.130:8082/artifactory/api", "auth": "admin:JFrog@2026",
     "root": "http://172.16.58.130:8082/artifactory"}
B = {"name": "b", "base": "http://172.16.58.130:8083/binflow/api", "auth": "admin:password",
     "root": "http://172.16.58.130:8083/binflow"}
WIRE = "/Users/lzw/dev-center/reports/compatibility/l024d-wire"
DROP = {"date", "server", "x-powered-by", "set-cookie", "x-request-id", "x-artifactory-id",
        "x-artifactory-node-id", "via", "x-jfrog-version", "content-length", "connection",
        "transfer-encoding", "x-content-type-options", "etag", "last-modified", "location"}
VENDOR = re.compile(r"^application/(vnd\.org\.jfrog\.[A-Za-z0-9.]*\+)?json$")
ISO = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")
results = []

def req(side, method, path, body=None, ctype="application/json", root=False, auth=None):
    url = (side["root"] if root else side["base"]) + path
    data = body if isinstance(body, (bytes, type(None))) else body.encode()
    r = urllib.request.Request(url, data=data, method=method)
    r.add_header("Authorization", "Basic " + base64.b64encode((auth or side["auth"]).encode()).decode())
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

def norm_text(t):
    t = t.replace("\r\n", "\n").replace(A["root"], "<BASE>").replace(B["root"], "<BASE>")
    return t

def norm_json(o):
    if isinstance(o, dict):
        return {k: norm_json(v) for k, v in o.items()}
    if isinstance(o, list):
        return [norm_json(x) for x in o]
    if isinstance(o, str):
        if ISO.match(o):
            return "<ts>"
        return o.replace(A["root"], "<BASE>").replace(B["root"], "<BASE>")
    return o

def run(cid, method, path, body=None, ctype="application/json", root=False, auth=None, note="", literal=False):
    outs = {}
    for side in (A, B):
        st, hdrs, text = req(side, method, path, body, ctype, root, auth)
        save(side, cid, st, hdrs, text)
        outs[side["name"]] = (st, norm_headers(hdrs), text)
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
    ta, tb = norm_text(a[2]), norm_text(b[2])
    try:
        ja, jb = json.loads(a[2] or "null"), json.loads(b[2] or "null")
        ja, jb = norm_json(ja), norm_json(jb)
        if ja != jb:
            import difflib
            la = json.dumps(ja, indent=1, sort_keys=True).splitlines()
            lb = json.dumps(jb, indent=1, sort_keys=True).splitlines()
            diffs.append("body: " + " | ".join(list(difflib.unified_diff(la, lb, "A", "B", lineterm=""))[:10]))
    except ValueError:
        if ta != tb:
            diffs.append(f"body-text A={ta[:200]!r} B={tb[:200]!r}" + (" ...TAIL" if len(ta) > 200 or len(tb) > 200 else ""))
    verdict = "SAME" if not diffs else "DIVERGENT"
    results.append((cid, verdict, diffs, note))
    print(f"[{verdict}] {cid}" + ("" if not diffs else "\n    " + "\n    ".join(diffs)))
    return outs

def mkrepo(side, key, ptype):
    req(side, "PUT", f"/repositories/{key}", json.dumps({"rclass": "local", "packageType": ptype}))

def put_file(side, repo, path, content, props=None, headers=None):
    # props: "k1=v1|k2=v2" -> jf-style matrix params ";k1=v1;k2=v2" (the only form
    # that lands as properties on BOTH sides; "?properties=" query and ";props="
    # matrix are stored literally/no-op on both — verified live 2026-09-15)
    mp = "".join(";" + p for p in props.split("|")) if props else ""
    full = side["root"] + f"/{repo}/{path}{mp}"
    r = urllib.request.Request(full, data=content, method="PUT")
    r.add_header("Authorization", "Basic " + base64.b64encode(side["auth"].encode()).decode())
    r.add_header("Content-Type", "application/octet-stream")
    for k, v in (headers or {}).items():
        r.add_header(k, v)
    try:
        resp = urllib.request.urlopen(r, timeout=30)
        return resp.status
    except urllib.error.HTTPError as e:
        return e.code

def root_url(side):
    return True

POM_TMPL = """<?xml version="1.0"?>
<project><modelVersion>4.0.0</modelVersion><groupId>l024d.grp</groupId><artifactId>%s</artifactId><version>%s</version></project>"""

def main():
    import os
    os.makedirs(f"{WIRE}/a", exist_ok=True); os.makedirs(f"{WIRE}/b", exist_ok=True)
    # ── fixture setup ─────────────────────────────────────────────
    for side in (A, B):
        for key, pt in (("l024d-mvn-local", "maven"), ("l024d-props-local", "generic"),
                        ("l024d-aql-local", "generic"), ("l024d-bad-local", "generic")):
            mkrepo(side, key, pt)
        # maven corpus: releases 1.0/1.1 + integration 1.1-line & 2.0 (timestamped unique form)
        for art, ver in (("l024d-art", "1.0"), ("l024d-art", "1.1"), ("l024d-art", "1.1-SNAPSHOT"),
                         ("l024d-art", "1.1-20260916.130000-1"), ("l024d-art", "2.0-20260916.120000-1"),
                         ("l024d-art2", "3.0-20260916.140000-1")):
            put_file(side, "l024d-mvn-local", f"l024d/grp/{art}/{ver}/{art}-{ver}.pom", (POM_TMPL % (art, ver)).encode())
        # /api/versions corpus: f1 = a@1.0 + b@1.1 + c@1.1 (multi-artifact separator arm)
        put_file(side, "l024d-props-local", "l024d/f1/a.bin", b"ver-prop-a-v1", "version=1.0")
        put_file(side, "l024d-props-local", "l024d/f1/b.bin", b"ver-prop-b-v1", "version=1.1")
        put_file(side, "l024d-props-local", "l024d/f1/c.bin", b"ver-prop-c-v1", "version=1.1")
        # AQL corpus: sub/file1 w/ build-ish props; root file (path="."); sub/file2 no props
        put_file(side, "l024d-aql-local", "sub/file1.bin", b"l0244-aql-file-one-v1",
                 "build.name=l024d-b|build.number=1|build.timestamp=1789450000000")
        put_file(side, "l024d-aql-local", "root.bin", b"l0244-aql-root-v1")
        put_file(side, "l024d-aql-local", "sub/file2.bin", b"l0244-aql-file-two-v1")

    # ── D03-R10 GET /api/search/versions ──────────────────────────
    run("v01-versions-hit", "GET", "/search/versions?g=l024d.grp&a=l024d-art",
        note="两键瘦行新→旧/integration 旗标/vendor CT")
    run("v02-versions-404", "GET", "/search/versions?g=l024d.grp&a=l024d-none",
        note="404 Unable to find artifact versions 逐字")
    run("v03-versions-wildcard", "GET", "/search/versions?g=l024d.grp&a=l024d-art&v=1.*",
        note="通配先全集后过滤")
    run("v04-versions-postfilter-empty", "GET", "/search/versions?g=l024d.grp&a=l024d-art&v=9.*",
        note="过滤后空集 = 200 results:[]（执行序）")
    run("v05-versions-repos-scope", "GET", "/search/versions?g=l024d.grp&a=l024d-art&repos=l024d-mvn-local",
        note="repos CSV 限定")

    # ── D03-R11 GET /api/search/latestVersion ─────────────────────
    run("l01-latest-default", "GET", "/search/latestVersion?g=l024d.grp&a=l024d-art",
        note="v 缺省=最新 release（跳过 integration）text/plain 裸串")
    run("l02-latest-wildcard", "GET", "/search/latestVersion?g=l024d.grp&a=l024d-art&v=1.*",
        note="通配首命中（1.1 线 integration 在场——V-ab 邻接臂）")
    run("l03-latest-v-nonwildcard-positive", "GET", "/search/latestVersion?g=l024d.grp&a=l024d-art&v=1.1",
        note="**V-ab 定谳**：非通配正命中=该线 integration？")
    run("l04-latest-v-nonwildcard-missing", "GET", "/search/latestVersion?g=l024d.grp&a=l024d-art&v=9.9",
        note="非通配无该线 → Unable to find artifact versions")
    run("l05-latest-no-release", "GET", "/search/latestVersion?g=l024d.grp&a=l024d-art2",
        note="**V-y 活体臂**：仅 integration → Latest release version not found")
    run("l06-latest-wildcard-nomatch", "GET", "/search/latestVersion?g=l024d.grp&a=l024d-art&v=9.*",
        note="**V-y 活体臂**：通配无命中 → Latest integration version not found")

    # ── D03-R12 GET /api/versions/{repoKey}/{path} ────────────────
    run("p01-versions-props-default", "GET", "/versions/l024d-props-local/l024d/f1",
        note="{version,artifacts:[]} 空数组非省略")
    run("p02-versions-props-listfiles", "GET", "/versions/l024d-props-local/l024d/f1?listFiles=1",
        note="**V-aa 定谳**：多 artifact 行分隔+尾逗号逐字节")
    run("p03-versions-props-404", "GET", "/versions/l024d-props-local/l024d/none",
        note="404 Not Found 裸文案")
    run("p04-versions-props-anon", "GET", "/versions/l024d-props-local/l024d/f1", auth="anon:anon",
        note="匿名 401")

    # ── D03-R13 GET /api/search/badChecksum ───────────────────────
    run("b01-badchecksum-notype", "GET", "/search/badChecksum",
        note="400 No checksum type defined 逐字")
    run("b02-badchecksum-badtype", "GET", "/search/badChecksum?type=sha512",
        note="400 Checksum type: sha512 is not defined 逐字")
    run("b03-badchecksum-clean", "GET", "/search/badChecksum?type=md5&repos=l024d-mvn-local",
        note="干净仓 200 {results:[]}")
    run("b05-badchecksum-anon", "GET", "/search/badChecksum?type=md5", auth="anon:anon",
        note="匿名 401")

    # ── AQL 族（jf 逐字形态） ─────────────────────────────────────
    AQL1 = ('items.find({"path":{"$ne":"."},"$or":[{"$and":[{"repo":"l024d-aql-local",'
            '"path":"sub","name":"file1.bin"}]}]}).include("name","repo","path","actual_md5",'
            '"actual_sha1","sha256","size","type","modified","created","property")')
    run("q01-aql-jf-verbatim-props", "POST", "/search/aql", AQL1, ctype="text/plain",
        note="§16.1 逐字：include 裸 property → properties 键值数组")
    AQL2 = ('items.find({"path":{"$ne":"."},"repo":"l024d-aql-local"}).include("name","path",'
            '"actual_md5","property")')
    run("q02-aql-root-exclusion", "POST", "/search/aql", AQL2, ctype="text/plain",
        note="path $ne '.' 排根级；无属性整键省略")
    AQL3 = 'items.find({"repo":"l024d-aql-local","path":"sub"}).include("name","property.key")'
    run("q03-aql-property-key-form", "POST", "/search/aql", AQL3, ctype="text/plain",
        note="property.key 展开式（@* 旧行为对照）")

    # ── b04 坏校验和命中臂：双坏文件（x=无 client 头 / y=带 client 头）──
    CX = b"l0244-corrupt-payload-v1"
    CY = b"l0244-corrupt-payload-v2"
    import hashlib as H
    hdrs_y = {"X-Checksum-Md5": H.md5(CY).hexdigest(), "X-Checksum-Sha1": H.sha1(CY).hexdigest()}
    put_file(A, "l024d-bad-local", "corrupt/x.bin", CX)
    put_file(B, "l024d-bad-local", "corrupt/x.bin", CX)
    put_file(A, "l024d-bad-local", "corrupt/y.bin", CY, headers=hdrs_y)
    put_file(B, "l024d-bad-local", "corrupt/y.bin", CY, headers=hdrs_y)
    s1x, s256x = H.sha1(CX).hexdigest(), H.sha256(CX).hexdigest()
    s1y, s256y = H.sha1(CY).hexdigest(), H.sha256(CY).hexdigest()
    corrupt_cmd = (
        f"docker exec artifactory sh -c \"printf 'X' | dd of=/var/opt/jfrog/artifactory/data/artifactory/filestore/"
        f"{s1x[:2]}/{s1x} bs=1 seek=0 conv=notrunc 2>/dev/null; "
        f"printf 'X' | dd of=/var/opt/jfrog/artifactory/data/artifactory/filestore/{s1y[:2]}/{s1y} bs=1 seek=0 conv=notrunc 2>/dev/null\" && "
        f"docker exec binflow-dev sh -c \"printf 'X' | dd of=/var/lib/binflow/blobs/{s256x[:2]}/{s256x} bs=1 seek=0 conv=notrunc 2>/dev/null; "
        f"printf 'X' | dd of=/var/lib/binflow/blobs/{s256y[:2]}/{s256y} bs=1 seek=0 conv=notrunc 2>/dev/null\"")
    rc = subprocess.run(["ssh", "lzw@172.16.58.130", corrupt_cmd], capture_output=True, text=True)
    print(f"[OBS] blob corruption rc={rc.returncode} {rc.stderr[:120]}")
    run("b04-badchecksum-corrupt-md5", "GET", "/search/badChecksum?type=md5&repos=l024d-bad-local",
        note="真损坏命中臂：x(无 client 头→clientMd5 空)+y(带 client 头→clientMd5=原值)；行形 {uri,serverMd5,clientMd5}")
    run("b04b-badchecksum-corrupt-sha1", "GET", "/search/badChecksum?type=sha1&repos=l024d-bad-local",
        note="sha1 型命中臂")

    # ── 控制臂：upload PUT wire（D11 根因分析输入） ───────────────
    st_a, h_a, _ = req(A, "PUT", "/l024d-aql-local/ctrl/wire.bin", b"l0244-ctrl-wire-v1",
                       ctype="application/octet-stream", root=True)
    save(A, "c01-upload-put-wire", st_a, h_a, "")
    st_b, h_b, _ = req(B, "PUT", "/l024d-aql-local/ctrl/wire.bin", b"l0244-ctrl-wire-v1",
                       ctype="application/octet-stream", root=True)
    save(B, "c01-upload-put-wire", st_b, h_b, "")
    print(f"[OBS] c01-upload-put-wire: A={st_a} {sorted(k for k in h_a if k.lower().startswith('x-checksum') or k.lower()=='location')} | B={st_b} {sorted(k for k in h_b if k.lower().startswith('x-checksum') or k.lower()=='location')}")

    same = sum(1 for r in results if r[1] == "SAME")
    print(f"\n== summary: SAME={same} DIVERGENT={len(results)-same} total={len(results)} ==")
    for cid, v, d, n in results:
        if v == "DIVERGENT":
            print(f"  {cid}: {'; '.join(d)[:280]}")
    import json as j
    with open("/tmp/l0244-verdicts.json", "w") as f:
        j.dump([{"id": c, "verdict": v, "diffs": d, "note": n} for c, v, d, n in results], f, indent=1)

if __name__ == "__main__":
    main()
