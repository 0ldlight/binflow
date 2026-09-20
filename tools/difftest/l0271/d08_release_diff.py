#!/usr/bin/env python3
"""L027-1 D08 release-bundle domain differential (dual mode).

A = Artifactory pro 7.161.15  http://172.16.58.130:8082/artifactory
B = BinFlow dev.3c0c3a87      http://172.16.58.130:8083/binflow   (pro-simulated:
    license 0752822e installed via POST /api/system/license for the run; deleted
    after — the ADR-0032 license gate is the B-side D08 door, community blocks it)

Case corpus = the L026-1 live-wire 64-case set (reports/compatibility/l026a-wire/
p01-p63 + p48b), re-fired dual-side: v1 family (R01 assembly / R02 transaction
error arms / R03 store arms / R04 query family / R05 config+fat_manifest), v2
read family (names/received/records/statuses/audit), R06 permission matrix
(non-admin user, anonymous) and the two adjacent rows (ANY DISTRIBUTION is not
a REST entity; distribution rclass not creatable via REST).

Normalize (proposal fixtures/normalize.yaml#release-bundle, registration right
stays with compatibility-engineer — modeled on search S1-S4 / buildinfo N1-N6):
  RB1 header drop set per search S1 + vendor json-family CT equivalence
  RB2 absolute URL host -> <BASE>
  RB3 JSON structural compare (indent/key-order ignored — storage-admin
      wire_format: normalize_indent posture; this domain has no compact/pretty
      dual-form fingerprint claim on either side)
  RB4 assembly results[] rows = set-semantics by urn (AQL unsorted; search S4)
"""
import base64, json, os, re, sys, urllib.request, urllib.error

A = {"name": "a", "base": "http://172.16.58.130:8082/artifactory/api", "auth": "admin:JFrog@2026"}
B = {"name": "b", "base": "http://172.16.58.130:8083/binflow/api", "auth": "admin:password"}
USER = "l027q-d08-user:U027q-Pass!1"          # non-admin, no grants (created/deleted per run)
NS = "l027q"                                   # difftest namespace
WIRE = "/Users/lzw/dev-center/reports/compatibility/l027q-wire"
DROP = {"date", "server", "x-powered-by", "set-cookie", "x-request-id", "x-artifactory-id",
        "x-artifactory-node-id", "via", "x-jfrog-version", "content-length", "connection",
        "transfer-encoding", "x-content-type-options"}
VENDOR = re.compile(r"^application/(vnd\.org\.jfrog\.[A-Za-z0-9.]*\+)?json$")
results = []


def req(side, method, path, body=None, ctype="application/json", auth=None):
    url = side["base"] + path
    data = body.encode() if isinstance(body, str) else body
    r = urllib.request.Request(url, data=data, method=method)
    if auth != "NONE":  # NONE = the true anonymous leg: no Authorization header at all
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


def norm_text(side, t):
    return t.replace(side["base"].rsplit("/api", 1)[0], "<BASE>")


def verdict(cid, note, diffs, cls=""):
    v = "SAME" if not diffs else "DIVERGENT"
    results.append({"id": cid, "verdict": v, "diffs": diffs, "note": note, "class": cls})
    print(f"[{v}] {cid}" + ("" if not diffs else "\n    " + "\n    ".join(str(d)[:300] for d in diffs)))


def run(cid, method, path, body=None, ctype="application/json", auth=None, note="", raw=False,
        rowset=False, clause_set=False, known401=False):
    outs = {}
    for side in (A, B):
        st, hdrs, text = req(side, method, path, body, ctype, auth)
        save(side, cid, st, hdrs, text)
        outs[side["name"]] = (st, norm_headers(hdrs), text)
    a, b = outs["a"], outs["b"]
    diffs = []
    known = []
    if a[0] != b[0]:
        diffs.append(f"status A={a[0]} B={b[0]}")
    ha, hb = a[1], b[1]
    for k in sorted(set(ha) - set(hb)):
        diffs.append(f"hdr only-A {k}: {ha[k]!r}")
    for k in sorted(set(hb) - set(ha)):
        diffs.append(f"hdr only-B {k}: {hb[k]!r}")
    for k in sorted(set(ha) & set(hb)):
        if ha[k] != hb[k]:
            if known401 and k == "Www-Authenticate":
                # L020-3 sealed posture: realm string is brand domain,
                # normalize to placeholder (known-divergence normalize R7).
                pa, pb = re.sub(r'realm="[^"]*"', 'realm=<realm>', ha[k]), re.sub(r'realm="[^"]*"', 'realm=<realm>', hb[k])
                if pa != pb:
                    diffs.append(f"hdr {k}: A={pa!r} B={pb!r}")
                continue
            diffs.append(f"hdr {k}: A={ha[k]!r} B={hb[k]!r}")
    if raw:
        ta, tb = norm_text(A, a[2]).replace("\r\n", "\n"), norm_text(B, b[2]).replace("\r\n", "\n")
        if ta != tb:
            diffs.append(f"body-bytes A={ta[:240]!r} B={tb[:240]!r}")
    else:
        try:
            ja, jb = json.loads(a[2] or "null"), json.loads(b[2] or "null")
            if clause_set:
                # RB5 proposal: the reference assembles the missing-field
                # clauses from a map (order unstable across shots — verified
                # three orders in three fires); the clause SET (backtick-
                # quoted segments) is the contract, the envelope copy around
                # them is fixed.
                for j in (ja, jb):
                    if isinstance(j, dict) and j.get("errors"):
                        j["errors"][0]["message"] = "|".join(sorted(
                            re.findall(r"`[^`]+`", j["errors"][0]["message"])))
            if known401:
                # L020-3 broad ruling: both sides render the auth-mid-layer
                # 401 copy per-side unified (A Bad Credentials / Authentication
                # is required; B invalid credentials) — message pending
                # ruling R-22a, envelope shape is the compare surface here.
                for j, side in ((ja, "a"), (jb, "b")):
                    if isinstance(j, dict) and j.get("errors"):
                        j["errors"][0]["message"] = "<401-wording>"
                        known.append(f"401 message A/B per-side (R-22a pending)")
            if rowset and isinstance(ja, dict) and "results" in ja and isinstance(jb, dict) and "results" in jb:
                ja["results"], jb["results"] = sorted(ja["results"], key=lambda r: r.get("urn", "")), \
                    sorted(jb["results"], key=lambda r: r.get("urn", ""))
            if ja != jb:
                diffs.append(f"body-json A={json.dumps(ja)[:260]} B={json.dumps(jb)[:260]}")
        except ValueError:
            ta, tb = norm_text(A, a[2]), norm_text(B, b[2])
            if ta != tb:
                diffs.append(f"body-text A={ta[:240]!r} B={tb[:240]!r}")
    verdict(cid, note, diffs, known)


def main():
    os.makedirs(f"{WIRE}/a", exist_ok=True)
    os.makedirs(f"{WIRE}/b", exist_ok=True)

    # ── fixtures: source repo + artifact + property + non-admin user ──
    body = "l027q release-bundle fixture v1\n"
    for side in (A, B):
        req(side, "DELETE", f"/repositories/{NS}-rbsrc?deleteContent=true")
        req(side, "PUT", f"/repositories/{NS}-rbsrc",
            json.dumps({"rclass": "local", "packageType": "generic"}))
        req(side, "PUT", f"/storage/{NS}-rbsrc/alpha/v1/f1.txt", body, "text/plain")
        req(side, "PUT", f"/storage/{NS}-rbsrc/alpha/v1/f1.txt?props={NS}-k=d1", None, None)
        req(side, "DELETE", f"/security/users/{NS}-d08-user")
        req(side, "PUT", f"/security/users/{NS}-d08-user",
            json.dumps({"password": USER.split(":", 1)[1], "email": f"{NS}-d08@binflow.test",
                        "admin": False}))
    aql = json.dumps({"aql": f'items.find({{"repo":{{"$eq":"{NS}-rbsrc"}}}})'})

    # ── R04 query family, empty state (p01-p12, p59) ──
    run("c01-bundles-default", "GET", "/release/bundles", note="p01 type缺省TARGET空态")
    run("c02-bundles-source", "GET", "/release/bundles?type=source", note="p02 type=source空态")
    run("c03-versions", "GET", f"/release/bundles/{NS}-rb", note="p03 名不存在也200空数组")
    run("c04-get-missing", "GET", f"/release/bundles/{NS}-rb/1.0", note="p04 404 Bundle not found")
    run("c05-get-missing-source", "GET", f"/release/bundles/{NS}-rb/1.0?type=source", note="p59 type任意值同形")
    run("c06-status-missing", "GET", f"/release/bundles/{NS}-rb/1.0/status", note="p05 冒号拼接第二消息族")
    run("c07-head-missing", "HEAD", f"/release/bundles/{NS}-rb/1.0", note="p07 404无body恒SOURCE")
    run("c08-get-jws-missing", "GET", f"/release/bundles/{NS}-rb/1.0?format=jws", note="p12")
    run("c09-artifacts-missing", "GET", f"/release/bundles/{NS}-rb/1.0/artifacts", note="p08")
    run("c10-delete-target-missing", "DELETE", f"/release/bundles/{NS}-rb/1.0", note="p09 Bundle not found")
    run("c11-delete-source-missing", "DELETE", f"/release/bundles/source/{NS}-rb/1.0", note="p10 Release bundle not found第三消息族")

    # ── R05 config / fat_manifest / v2 read family (p06,p14-p26,p47,p60) ──
    run("c12-config-get", "GET", "/release/bundles/config", note="p06 出厂缺省720")
    run("c13-fat-manifest-wrong-name", "GET", "/release/fat_manifest_content/nonexistent/manifest.json",
        note="p47 非 list.manifest.json 拒绝")
    run("c14-v2-names", "GET", "/v2/release_bundle/names", note="p14")
    run("c15-v2-received", "GET", "/v2/release_bundle/received", note="p15")
    run("c16-v2-records-name", "GET", f"/v2/release_bundle/records/{NS}-rb", note="p18 分页三键信封")
    run("c17-v2-received-name", "GET", f"/v2/release_bundle/received/{NS}-rb", note="p19")
    run("c18-v2-received-namever", "GET", f"/v2/release_bundle/received/{NS}-rb/1.0",
        note="p20/p23 合法名也400名字校验（无GET路由落校验器）")
    run("c19-v2-records-namever", "GET", f"/v2/release_bundle/records/{NS}-rb/1.0",
        note="p17/p24 泄露v2存储布局.evd")
    run("c20-v2-statuses-namever", "GET", f"/v2/release_bundle/statuses/{NS}-rb/1.0", note="p16/p25")
    run("c21-v2-received-delete", "DELETE", f"/v2/release_bundle/received/{NS}-rb/1.0", note="p60 -jfds仓名")
    run("c22-v2-audit-get", "GET", "/v2/audit?limit=1", note="p22 405仅POST")
    run("c23-v2-audit-post-empty", "POST", "/v2/audit", "{}", note="p26 七必填字段清单",
        clause_set=True)

    # ── R01 assembly (p27-p33, p36/p37) ──
    run("c24-assemble-missing-aql", "POST", "/release/bundle", "{}", note="p27")
    run("c25-assemble-empty-aql", "POST", "/release/bundle", '{"aql":""}', note="p28")
    run("c26-assemble-not-items", "POST", "/release/bundle",
        '{"aql":"builds.find({\\"name\\":{\\"$eq\\":\\"x\\"}})"}', note="p29")
    run("c27-assemble-syntax-error", "POST", "/release/bundle",
        '{"aql":"items.find(bogus-syntax("}', note="p33 AQL语法错消息")
    run("c28-assemble-hit", "POST", "/release/bundle", aql, note="p30/p36 命中五行字段闭集", rowset=True)
    run("c29-assemble-empty-hit", "POST", "/release/bundle",
        json.dumps({"aql": f'items.find({{"repo":{{"$eq":"{NS}-rbsrc"}},"name":{{"$eq":"no-such"}}}})'}),
        note="p32 空命中results在场")
    run("c30-assemble-include-meta", "POST", "/release/bundle?includeMetaData=true", aql,
        note="p37 generic仓两臂同形", rowset=True)

    # ── R02 transaction error arms (p38-p43) ──
    run("c31-tx-wrap-jose-garbage", "POST", "/release/bundle/transaction", "not-a-jws",
        "application/jose", note="p38")
    run("c32-tx-open-garbage", "POST", "/release/bundle/transaction/open",
        '{"signedJwsBundle":"garbage"}', note="p39 与包装端点同形")
    run("c33-tx-open-not-json", "POST", "/release/bundle/transaction/open", "not json",
        note="p40 裸Jackson消息")
    run("c34-tx-close-missing", "POST", "/release/bundle/transaction/close/nonexistent/tx/path",
        None, note="p41 事务不存在")
    run("c35-tx-async-close-missing", "POST", "/release/bundle/transaction/async/close/nonexistent/tx/path",
        None, note="p42")
    run("c36-tx-async-status-missing", "GET", "/release/bundle/transaction/async/close/status/nonexistent/tx/path",
        note="p43")

    # ── R03 store arms (p11, p44-p49) — LAST: p48 leg auto-creates release-bundles ──
    run("c37-store-options", "OPTIONS", "/release/store", None, note="p11 CORS预检")
    run("c38-store-empty-body", "PUT", "/release/store", "{}", note="p45 缺signedJwsBundle→500泄漏")
    run("c39-store-not-json", "PUT", "/release/store", "not json", note="p46")
    run("c40-store-invalid-rb-repo", "PUT", "/release/store",
        json.dumps({"signedJwsBundle": "garbage", "storingRepo": f"{NS}-rbsrc", "artifactMapping": {}}),
        note="p44 INVALID_RB_REPO双键信封")
    run("c41-store-default-repo", "PUT", "/release/store",
        json.dumps({"signedJwsBundle": "garbage"}),
        note="p48 缺省storingRepo→release-bundles系统仓副作用+500 JWS解析")
    run("c42-store-bad-project", "PUT", f"/release/store?projectKey={NS}nope",
        json.dumps({"signedJwsBundle": "garbage"}),
        note="p49 projectKey不存在→404 Access联查")
    # p48b follow-up: the auto-created system repo — single GET 200 but list-invisible
    probe = {}
    for side in (A, B):
        st, h, t = req(side, "GET", "/repositories/release-bundles")
        save(side, "c43-system-repo-single-get", st, h, t)
        lst_st, lst_h, lst_t = req(side, "GET", "/repositories")
        save(side, "c43-system-repo-list", lst_st, lst_h, lst_t)
        in_list = any(r.get("key") == "release-bundles" for r in json.loads(lst_t))
        probe[side["name"]] = (st, in_list)
        print(f"    [{side['name']}] c43 system-repo single GET {st}, in list: {in_list}")
    verdict("c43-system-repo-side-effect", "p48b 系统仓单查200+列表不可见",
            [] if probe["a"] == probe["b"] else [f"single-GET/list-visibility A={probe['a']} B={probe['b']}"])
    # c55: the system row must be deletable over the same REST face (L026-1
    # cleaned the reference instance this way; round 1 found B stranded at a
    # 400 package-type validation — the L027-1 bug fix's acceptance leg).
    run("c55-system-repo-delete", "DELETE", "/repositories/release-bundles",
        note="系统仓DELETE可清（L026-1 清理先例；round1 B 400卡死=本票BUG）")

    # ── R06 permission matrix (p50-p58) ──
    run("c44-user-bundles", "GET", "/release/bundles", auth=USER, note="p50 user角色放行200")
    run("c45-user-assemble", "POST", "/release/bundle", "{}", auth=USER,
        note="p51 达校验臂非403")
    run("c46-user-store", "PUT", "/release/store",
        json.dumps({"signedJwsBundle": "garbage", "storingRepo": f"{NS}-rbsrc", "artifactMapping": {}}),
        auth=USER, note="p52 admin门403 Forbidden信封")
    run("c47-user-config", "GET", "/release/bundles/config", auth=USER, note="p53")
    run("c48-user-async-status", "GET", "/release/bundle/transaction/async/close/status/x",
        auth=USER, note="p54")
    run("c49-user-fat-manifest", "GET", "/release/fat_manifest_content/x/list.manifest.json",
        auth=USER, note="p55")
    run("c50-anon-assemble", "POST", "/release/bundle", "{}", auth="NONE", note="p56 匿名401",
        known401=True)
    run("c51-anon-bundles", "GET", "/release/bundles", auth="NONE", note="p57", known401=True)
    run("c52-anon-store", "PUT", "/release/store", "{}", auth="NONE", note="p58", known401=True)

    # ── adjacent rows (p61-p63): ANY DISTRIBUTION not a REST entity; distribution rclass ──
    proj = {}
    for side in (A, B):
        st, h, t = req(side, "GET", "/security/permissions")
        save(side, "c53-permissions-list", st, h, t)
        names = sorted(p["name"] for p in json.loads(t) if "any" in p["name"].lower())
        proj[side["name"]] = names
    verdict("c53-permissions-any-family", "p61/p62 ANY家族投影（Any Distribution不在名单）",
            [] if proj["a"] == proj["b"] else [f"Any-family A={proj['a']} B={proj['b']}"])
    run("c54-distribution-rclass", "PUT", f"/repositories/{NS}-dist",
        json.dumps({"rclass": "distribution", "url": "http://172.16.58.130:8082/distribution/api/v1",
                    "packageType": "generic"}),
        note="p63 distribution rclass不可REST创建")

    # ── cleanup ──
    for side in (A, B):
        req(side, "DELETE", f"/repositories/{NS}-rbsrc?deleteContent=true")
        req(side, "DELETE", f"/repositories/{NS}-dist")
        req(side, "DELETE", f"/security/users/{NS}-d08-user")
        # the auto-created system repo (c41 side effect) — remove, verify
        req(side, "DELETE", "/repositories/release-bundles")
        st, _, _ = req(side, "GET", "/repositories/release-bundles")
        lst = req(side, "GET", "/repositories")[2]
        left = [r["key"] for r in json.loads(lst) if r["key"].startswith("l027q") or r["key"] == "release-bundles"]
        print(f"    [{side['name']}] cleanup: system repo GET after delete={st}, residue={left}")

    with open(f"{WIRE}/results.json", "w") as f:
        json.dump(results, f, indent=1, ensure_ascii=False)
    n_same = sum(1 for r in results if r["verdict"] == "SAME")
    print(f"\n== {len(results)} cases: SAME {n_same} / DIVERGENT {len(results)-n_same} ==")


if __name__ == "__main__":
    main()
