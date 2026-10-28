"""Replays contract entries docker/remote-tags-list (#16), docker/remote-catalog
(#17), docker/remote-tags-list-pagination (#18) and the catalog-pagination
registry-path Link quirk (#19, reference-side unobserved arms recorded).

tags/list: 200; keys_exact [name, tags]; name == upstream echo name;
tag set == {t1, t2}. ?n=1 -> 200 truncated single tag + Link rel="next"
pointing at the repo-scoped tags path. ?n=abc -> 404 "Not Found" with bare
application/json. _catalog: 200; keys_exact [repositories]; content
intercompared across sides (upstream = in-cluster relay registry).
"""
import importlib.util
import os
import re

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-tags-catalog",
    "title": "tags/list + _catalog + pagination arms",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def run(ctx):
    checks = dl.Checks()
    catalogs = {}
    tag_orders = {}
    tags_path = "/v2/%s/%s/tags/list" % (dl.REPO, dl.IMAGE)
    for side in ("a", "b"):
        tok = None
        tr = dl.issue_token(ctx, side)
        if tr["status"] == 200:
            tok = dl.jbody(tr).get("token")
        headers = {"Authorization": "Bearer " + tok} if tok else {}

        r = ctx.http(side, "GET", tags_path, headers=headers)
        checks.add(side, "tags status-200", r["status"] == 200, "got %s" % r["status"])
        try:
            doc = dl.jbody(r)
            checks.add(side, "tags keys-exact", sorted(doc.keys()) == ["name", "tags"],
                       sorted(doc.keys()))
            checks.add(side, "tags name-echo", doc.get("name") == dl.IMAGE, doc.get("name"))
            tag_set = set(doc.get("tags") or [])
            # superset (not exact) so probe-residue tags on the shared upstream
            # don't mask the live-aggregation semantics; cross-side equality is
            # asserted below.
            checks.add(side, "tags contains-t1-t2", {"t1", "t2"} <= tag_set, sorted(tag_set))
            tag_orders[side] = doc.get("tags")
        except Exception as e:
            checks.add(side, "tags body-json", False, repr(e))

        r1 = ctx.http(side, "GET", tags_path + "?n=1", headers=headers)
        checks.add(side, "tags-n1 status-200", r1["status"] == 200, "got %s" % r1["status"])
        link = dl.hdr(r1, "Link") or ""
        checks.add(side, "tags-n1 link-shape",
                   bool(re.fullmatch(
                       r'</v2/%s/%s/tags/list\?last=[^&]+&n=\d+>; rel="next"' % (dl.REPO, dl.IMAGE),
                       link)), link)
        try:
            doc1 = dl.jbody(r1)
            checks.add(side, "tags-n1 single-tag", len(doc1.get("tags") or []) == 1,
                       doc1.get("tags"))
        except Exception as e:
            checks.add(side, "tags-n1 body-json", False, repr(e))

        rbad = ctx.http(side, "GET", tags_path + "?n=abc", headers=headers)
        checks.add(side, "tags-nabc status-404", rbad["status"] == 404, "got %s" % rbad["status"])
        checks.add(side, "tags-nabc bare-content-type",
                   dl.hdr(rbad, "Content-Type") == "application/json",
                   dl.hdr(rbad, "Content-Type"))
        try:
            e0 = (dl.jbody(rbad).get("errors") or [{}])[0]
            checks.add(side, "tags-nabc message-not-found", e0.get("message") == "Not Found",
                       e0.get("message"))
            checks.add(side, "tags-nabc e0-keys",
                       sorted(e0.keys()) == ["message", "status"], sorted(e0.keys()))
        except Exception as e:
            checks.add(side, "tags-nabc body-json", False, repr(e))

        cat = ctx.http(side, "GET", "/v2/%s/_catalog" % dl.REPO, headers=headers)
        checks.add(side, "catalog status-200", cat["status"] == 200, "got %s" % cat["status"])
        try:
            cdoc = dl.jbody(cat)
            checks.add(side, "catalog keys-exact", sorted(cdoc.keys()) == ["repositories"],
                       sorted(cdoc.keys()))
            checks.add(side, "catalog repositories-list",
                       isinstance(cdoc.get("repositories"), list),
                       type(cdoc.get("repositories")).__name__)
            catalogs[side] = cdoc.get("repositories")
        except Exception as e:
            checks.add(side, "catalog body-json", False, repr(e))

        catn = ctx.http(side, "GET", "/v2/%s/_catalog?n=1" % dl.REPO, headers=headers)
        checks.add(side, "catalog-n1 status-200", catn["status"] == 200, "got %s" % catn["status"])
        clink = dl.hdr(catn, "Link") or ""
        checks.add(side, "catalog-n1 link-registry-path-quirk",
                   bool(re.fullmatch(r'</v2/_catalog\?last=[^&]+&n=\d+>; rel="next"', clink)), clink)
        try:
            cdoc1 = dl.jbody(catn)
            checks.add(side, "catalog-n1 single-entry", len(cdoc1.get("repositories") or []) == 1,
                       cdoc1.get("repositories"))
        except Exception as e:
            checks.add(side, "catalog-n1 body-json", False, repr(e))

    checks.add("ab", "catalog-content-intercompare",
               catalogs.get("a") is not None and catalogs.get("a") == catalogs.get("b"),
               "a=%s b=%s" % (catalogs.get("a"), catalogs.get("b")))
    checks.add("ab", "tag-order-intercompare",
               tag_orders.get("a") is not None and tag_orders.get("a") == tag_orders.get("b"),
               "a=%s b=%s" % (tag_orders.get("a"), tag_orders.get("b")))
    ev = ctx.write_evidence("checks.json", {"catalogs": catalogs, "tag_orders": tag_orders,
                                            "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
