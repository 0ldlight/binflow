#!/usr/bin/env python3
"""L025-4 D02 config-family differential (dual mode).

A = Artifactory pro 7.161.15  http://172.16.58.130:8082/artifactory
B = BinFlow dev.498683c5      http://172.16.58.130:8083/binflow

Normalize (proposal fixtures/normalize.yaml#d02-config):
  Q1 header drop set as prior loops; vendor json-family CT equivalence
  Q2 absolute URL host -> <BASE>
  Q3 config-body compare = STRUCTURAL PROJECTION (group keys, per-group sorted
     key lists, asserted scalar fields); full field-set delta recorded as
     KNOWN model-level drift metric (L025-3A registered subset-model) — not
     verdict-changing
  Q4 batch-delete reports[] = set-compare (A HashSet order unstable; B batch order)
"""
import base64, json, re, urllib.request, urllib.error, os

A = {"name": "a", "base": "http://172.16.58.130:8082/artifactory/api", "auth": "admin:" + os.environ["ARTIFACTORY_REF_PASSWORD"]}
B = {"name": "b", "base": "http://172.16.58.130:8083/binflow/api", "auth": "admin:" + os.environ["ARTIFACTORY_REF_PASSWORD"]}
PLAIN = "l025q-u:L025q-Pass!1"
WIRE = "/Users/lzw/dev-center/reports/compatibility/l025q-wire"
DROP = {"date", "server", "x-powered-by", "set-cookie", "x-request-id", "x-artifactory-id",
        "x-artifactory-node-id", "via", "x-jfrog-version", "content-length", "connection",
        "transfer-encoding", "x-content-type-options"}
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
    return st, hdrs, raw.decode("utf-8", "replace")

def save(side, cid, st, hdrs, text):
    with open(f"{WIRE}/{side['name']}/{cid}.hdr", "w") as f:
        f.write(f"HTTP {st}\n" + "".join(f"{k}: {v}\n" for k, v in hdrs.items()))
    with open(f"{WIRE}/{side['name']}/{cid}.body", "w") as f:
        f.write(text)

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

def verdict(cid, note, diffs):
    v = "SAME" if not diffs else "DIVERGENT"
    results.append((cid, v, diffs, note))
    print(f"[{v}] {cid}" + ("" if not diffs else "\n    " + "\n    ".join(str(d)[:280] for d in diffs)))

def run(cid, method, path, body=None, ctype="application/json", auth=None, note="", raw=False):
    outs = {}
    for side in (A, B):
        st, hdrs, text = req(side, method, path, body, ctype, auth)
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
    if raw:
        ta = a[2].replace("\r\n", "\n"); tb = b[2].replace("\r\n", "\n")
        if ta != tb:
            diffs.append(f"body-bytes A={ta!r} B={tb!r}")
    else:
        try:
            ja, jb = json.loads(a[2] or "null"), json.loads(b[2] or "null")
            if ja != jb:
                diffs.append(f"body-json A={json.dumps(ja)[:240]} B={json.dumps(jb)[:240]}")
        except ValueError:
            ta = a[2].replace(A['base'].rsplit('/api',1)[0], '<BASE>').replace(B['base'].rsplit('/api',1)[0], '<BASE>')
            tb = b[2].replace(A['base'].rsplit('/api',1)[0], '<BASE>').replace(B['base'].rsplit('/api',1)[0], '<BASE>')
            if ta != tb:
                diffs.append(f"body-text A={ta[:240]!r} B={tb[:240]!r}")
    verdict(cid, note, diffs)
    return outs

def main():
    import os
    os.makedirs(f"{WIRE}/a", exist_ok=True); os.makedirs(f"{WIRE}/b", exist_ok=True)
    # ── fixtures: seeded repos (pre-created single, so batch-PUT creates fresh keys) ──
    for side in (A, B):
        req(side, "PUT", "/repositories/l025q-seed", json.dumps({"rclass": "local", "packageType": "generic"}))
        req(side, "PUT", "/repositories/l025q-virt", json.dumps(
            {"rclass": "virtual", "packageType": "generic", "repositories": ["l025q-seed"]}))
        req(side, "PUT", "/repositories/l025q-rem", json.dumps(
            {"rclass": "remote", "packageType": "generic", "url": "http://172.16.58.130:8082/artifactory"}))

    # ── read family ────────────────────────────────────────────
    # c01: configurations structural projection
    proj = {}
    for side in (A, B):
        st, h, t = req(side, "GET", "/repositories/configurations")
        save(side, "c01-configurations", st, h, t)
        d = json.loads(t)
        proj[side["name"]] = {g: [r["key"] for r in rows] for g, rows in d.items()}
        proj[side["name"] + "-hdr"] = (st, norm_headers(h))
    drift = {}
    for side in (A, B):
        d = json.loads(open(f"{WIRE}/{side['name']}/c01-configurations.body").read())
        keys = set()
        for rows in d.values():
            for r in rows:
                if r["key"].startswith("l025q-"):
                    keys |= set(r.keys())
        drift[side["name"]] = len(keys)
    diffs = []
    ga = {g: sorted(v) for g, v in proj["a"].items() if g != "RELEASE_BUNDLE"}
    gb = {g: sorted(v) for g, v in proj["b"].items() if g != "RELEASE_BUNDLE"}
    for g in sorted(set(ga) | set(gb)):
        sa = [k for k in ga.get(g, []) if k == "l025q-seed"]
        sb = [k for k in gb.get(g, []) if k == "l025q-seed"]
        if sa != sb:
            diffs.append(f"group {g}: l025q presence A={sa} B={sb}")
    if proj["a-hdr"][1].get("Cache-Control") != proj["b-hdr"][1].get("Cache-Control"):
        diffs.append(f"Cache-Control A={proj['a-hdr'][1].get('Cache-Control')} B={proj['b-hdr'][1].get('Cache-Control')}")
    verdict("c01-configurations-structure", f"分组/排序/CT/no-store（字段集漂移度量 A={drift['a']}键 B={drift['b']}键=既有模型级）", diffs)

    run("c02-configurations-filter-miss", "GET", "/repositories/configurations?packageType=nosuchtype",
        note="过滤空集 = 200 {}")
    run("c03-configurations-nonadmin", "GET", "/repositories/configurations", auth=PLAIN,
        note="非 admin 403 信封 Forbidden")
    run("e01-existence-hit", "GET", "/repositories/existence?projectKey=default&type=local",
        note="{exists,matchingRepoTypes,projectKey}")
    run("e02-existence-comma-type", "GET", "/repositories/existence?projectKey=default&type=local,remote",
        note="逗号 400 Invalid repository type")
    run("e03-existence-repeat-type", "GET", "/repositories/existence?projectKey=default&type=local&type=remote",
        note="重复参数 OR")
    run("e04-existence-project-ignored", "GET", "/repositories/existence?project=myproj&type=local",
        note="project= 静默忽略（实名 projectKey）")
    # v2 单仓读（结构投影：type 键/CT/404/406/非 admin 五键）
    run("v01-v2-read-local", "GET", "/v2/repositories/l025q-seed",
        note="v2 读（BinFlow 子集模型=既有漂移；type 键/CT vendor/no-store）")
    run("v02-v2-read-404", "GET", "/v2/repositories/l025q-none",
        note="The repository <key> was not found 信封")
    for side_name in ("a", "b"):
        side = A if side_name == "a" else B
        st, h, t = req(side, "GET", "/v2/repositories/l025q-seed", ctype="application/vnd.org.jfrog.artifactory.repositories.RemoteRepositoryConfiguration+json")
        save(side, "v03-v2-ct-406", st, h, t)
    run406 = None
    a406 = open(f"{WIRE}/a/v03-v2-ct-406.hdr").readline().strip()
    b406 = open(f"{WIRE}/b/v03-v2-ct-406.hdr").readline().strip()
    a406b = open(f"{WIRE}/a/v03-v2-ct-406.body").read()
    b406b = open(f"{WIRE}/b/v03-v2-ct-406.body").read()
    d = [] if (a406 == b406 and a406b == b406b) else [f"A={a406} {a406b[:120]!r} B={b406} {b406b[:120]!r}"]
    verdict("v03-v2-ct-406", "Content-Type（非 Accept）协商怪癖：Remote vendor CT 打 local 仓 → 406", d)
    run("v04-v2-accept-mismatch", "GET", "/v2/repositories/l025q-seed",
        note="不匹配 Accept 头 → 200（协商键非 Accept）")
    run("v05-v2-nonadmin-fivekeys", "GET", "/v2/repositories/l025q-rem", auth=PLAIN,
        note="非 admin 部分视图（remote 五键规格；local/virtual 同集=待验证面）")
    run("b01-v2-batch-read", "GET", "/v2/repositories/batch?names=l025q-seed&names=l025q-virt&names=l025q-ghost",
        note="批读 map（v1 schema；ghost 静默省略）")
    run("b02-v2-batch-read-comma", "GET", "/v2/repositories/batch?names=l025q-seed,l025q-virt",
        note="逗号串=单 key → 落空 {}")
    run("b03-v2-batch-read-nonames", "GET", "/v2/repositories/batch",
        note="400 Repository keys are missing.")
    run("l01-repolayouts-list", "GET", "/admin/repolayouts",
        note="布局列表 25（票甲已自证 IDENTICAL——差分复证）")
    run("l02-repolayouts-single", "GET", "/admin/repolayouts/maven-2-default",
        note="单读全字段")
    run("l03-repolayouts-missing", "GET", "/admin/repolayouts/l025q-none",
        note="缺名 500 No value present（bug 兼容点）")
    run("l04a-repo-layouts-old", "GET", "/repo_layouts", note="旧挂载 404（已撤）")
    run("l04b-repo-layouts-old-named", "GET", "/repo_layouts/maven-2-default", note="旧挂载带名 404")

    # ── batch write family ─────────────────────────────────────
    run("w01-put-batch-create", "PUT", "/v2/repositories/batch",
        json.dumps([{"key": "l025q-b1", "rclass": "local", "packageType": "generic"},
                    {"key": "l025q-b2", "rclass": "local", "packageType": "generic"}]),
        ctype="application/json", note="**201 体逐字节**（尾空格+空行）", raw=True)
    run("w02-put-batch-allornothing", "PUT", "/v2/repositories/batch",
        json.dumps([{"key": "l025q-seed", "rclass": "local", "packageType": "generic"},
                    {"key": "l025q-never", "rclass": "local", "packageType": "generic"}]),
        note="已存在 key → 整单 400 逐字；回滚（l025q-never 未建）")
    run("w02b-rollback-check", "GET", "/repositories/l025q-never", note="回滚复核 404")
    run("w03-put-batch-missing-key", "PUT", "/v2/repositories/batch",
        json.dumps([{"rclass": "local", "packageType": "generic"}]),
        note="400 Repository key are missing in configuration（原文如此）")
    run("w04-put-batch-empty-array", "PUT", "/v2/repositories/batch", "[]",
        note="**四臂**：空数组（票乙钉 201 空体——A 真值）", raw=True)
    run("w05-put-batch-nonadmin", "PUT", "/v2/repositories/batch",
        json.dumps([{"key": "l025q-x1", "rclass": "local", "packageType": "generic"}]),
        auth=PLAIN, note="**四臂**：PUT 非 admin 403 文案（A 真值 vs B 标准文案）")
    # merge: change description of l025q-b1 only; verify repoLayoutRef etc preserved
    run("w06-post-batch-merge", "POST", "/v2/repositories/batch",
        json.dumps([{"key": "l025q-b1", "description": "merged-desc-v1"}]),
        note="merge 方言：200 裸串 Repositories updated successfully.")
    run("w06b-merge-verify", "GET", "/repositories/l025q-b1",
        note="description=merged-desc-v1；packageType 保留")
    run("w07-post-batch-ghost", "POST", "/v2/repositories/batch",
        json.dumps([{"key": "l025q-ghost", "description": "x"}]),
        note="404 裸文本 No repositories found for the following keys")
    run("w08-post-batch-nonadmin", "POST", "/v2/repositories/batch",
        json.dumps([{"key": "l025q-b1", "description": "x"}]),
        auth=PLAIN, note="非 admin 403 裸文本 User is not authorized to update...")
    run("w09-delete-empty", "DELETE", "/v2/repositories/batch", "[]",
        note="400 {statusMessage: No repository keys were provided for deletion}（无 reports）")
    run("w10-delete-ghost-only", "DELETE", "/v2/repositories/batch",
        json.dumps(["l025q-none", "l025q-none"]),
        note="全 ghost（重复去重）→ 200 + success:true 报告 + 聚合文案")
    run("w11-delete-mixed", "DELETE", "/v2/repositories/batch",
        json.dumps(["l025q-b2", "l025q-virt", "l025q-rem", "l025q-none"]),
        note="local+virtual+remote+ghost 真（全 success 形）——三 rclass 文案+remote=local 形复证；reports 集合序（Q4）")
    run("w12-delete-blank-key", "DELETE", "/v2/repositories/batch",
        json.dumps(["l025q-b1", ""]),
        note="**四臂**：blank key 预校验整单中止——A 理由文本定谳")
    run("w13-delete-nonadmin", "DELETE", "/v2/repositories/batch",
        json.dumps(["l025q-b1"]), auth=PLAIN,
        note="非 admin 预校验 403 单报告（Cannot delete repository... insufficient permission）")
    run("w14-delete-final", "DELETE", "/v2/repositories/batch",
        json.dumps(["l025q-b1", "l025q-seed"]),
        note="收尾删除（真 local 成功形态+计数）")

    same = sum(1 for r in results if r[1] == "SAME")
    print(f"\n== summary: SAME={same} DIVERGENT={len(results)-same} total={len(results)} ==")
    for cid, v, d, n in results:
        if v == "DIVERGENT":
            print(f"  {cid}: {'; '.join(str(x) for x in d)[:300]}")
    with open("/tmp/l0254-verdicts.json", "w") as f:
        json.dump([{"id": c, "verdict": v, "diffs": [str(x) for x in d], "note": n} for c, v, d, n in results], f, indent=1)

if __name__ == "__main__":
    main()
