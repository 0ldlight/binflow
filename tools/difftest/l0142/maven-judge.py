#!/usr/bin/env python3
"""L014-2 matrix judge — compares the a/b wire dirs maven-replay.sh dropped
(reports/compatibility/l0142-wire/{a,b}/maven) over the L013-4 18 arms and
prints the convergence table. Timestamps, hosts and base paths are
normalized; storage listings compare NAME SETS with snapshot stamps
masked."""
import re
import sys
from pathlib import Path

ROOT = Path("/Users/lzw/dev-center/reports/compatibility/l0142-wire")


def n(s: str) -> str:
    s = s.replace("http://localhost:8082/artifactory", "").replace("http://localhost:8083", "")
    s = re.sub(r"\d{14}", "TS", s)
    s = re.sub(r"\d{8}\.\d{6}", "SNAPTS", s)
    return s


def rd(side: str, name: str) -> str:
    p = ROOT / side / "maven" / name
    return p.read_text() if p.exists() else ""


def loc(side: str, name: str) -> str:
    for line in rd(side, name + ".hdr").splitlines():
        if line.lower().startswith("location:"):
            return n(line.split(":", 1)[1].strip())
    return ""


def status(side: str, name: str) -> str:
    for line in rd(side, name + ".hdr").splitlines():
        if line.startswith("HTTP/"):
            return line.split()[1]
    return ""


def names(side: str, name: str) -> set:
    return {n(l) for l in rd(side, name + ".names").splitlines() if l.strip()}


def versions_of(body: str) -> list:
    block = re.search(r"<versions>(.*?)</versions>", body, re.S)
    return re.findall(r"<version>([^<]+)</version>", block.group(1)) if block else []


verdicts: list[tuple[str, str, str, str]] = []  # arm, title, verdict, note


def judge(arm, title, ok: bool, note=""):
    verdicts.append((arm, title, "一致" if ok else "差异", note))


# A1+A2: group metadata accumulate + latest/release
a1a, a1b = rd("a", "a1-get-groupmeta.body"), rd("b", "a1-get-groupmeta.body")
a3a, a3b = rd("a", "a3-get-groupmeta.body"), rd("b", "a3-get-groupmeta.body")
judge("A1", "versions accumulate", versions_of(a1a) == versions_of(a1b) and versions_of(a3a) == versions_of(a3b),
      f"a3 versions a={versions_of(a3a)} b={versions_of(a3b)}")
lat = lambda s: re.search(r"<latest>([^<]+)</latest>", s)
rel = lambda s: re.search(r"<release>([^<]+)</release>", s)
judge("A2", "latest/release arithmetic",
      lat(a3a) and lat(a3b) and lat(a3a).group(1) == lat(a3b).group(1)
      and rel(a3a) and rel(a3b) and rel(a3a).group(1) == rel(a3b).group(1),
      f"latest a={lat(a3a) and lat(a3a).group(1)} b={lat(a3b) and lat(a3b).group(1)}")

# A3: lastUpdated format
lu = lambda s: re.search(r"<lastUpdated>(\d{14})</lastUpdated>", s)
judge("A3", "lastUpdated format", bool(lu(a3a)) and bool(lu(a3b)))

# A4: XML form (expected divergence — UNKNOWN, unimplemented)
smd_a, smd_b = rd("a", "a3b-get-snapdir-meta.body"), rd("b", "a3b-get-snapdir-meta.body")
same_form = ('modelVersion="1.1.0"' in smd_a) == ('modelVersion="1.1.0"' in smd_b) and \
            (smd_a.rstrip().endswith("</metadata>") and n(smd_a) == n(smd_b))
judge("A4", "metadata XML form", same_form,
      "UNKNOWN dossier: modelVersion attr / trailing <version> / child order")

# A5: served sha1 == sha1(metadata) per side
import hashlib
ok5 = True
for side in ("a", "b"):
    served = rd(side, "a4-get-meta-sha1.body").strip()
    actual = hashlib.sha1(rd(side, "a3-get-groupmeta.body").encode()).hexdigest()
    ok5 = ok5 and served == actual
judge("A5", "metadata checksum served=computed", ok5)

# A6: default repo rewrites -SNAPSHOT PUT
ok6 = bool(re.search(r"2\.0\.0-SNAPSHOT/probe-app-2\.0\.0-SNAPTS-1\.pom$", loc("a", "a3-put-pom-200sn"))) and \
      bool(re.search(r"2\.0\.0-SNAPSHOT/probe-app-2\.0\.0-SNAPTS-1\.pom$", loc("b", "a3-put-pom-200sn")))
judge("A6", "default repo snapshot rewrite (BUG 1)", ok6,
      f"b loc={loc('b', 'a3-put-pom-200sn')}")

# B1/B2: unique repo raw rewrite + trip arithmetic
l1a, l1b = loc("a", "b1-put-jar-snap-1"), loc("b", "b1-put-jar-snap-1")
l2a, l2b = loc("a", "b2-put-pom-snap"), loc("b", "b2-put-pom-snap")
l3a, l3b = loc("a", "b3-put-jar-snap-2"), loc("b", "b3-put-jar-snap-2")
pat = lambda s: bool(re.search(r"-SNAPTS-(\d+)\.jar$", s))
n_of = lambda s: (re.search(r"-SNAPTS-(\d+)\.jar$", s) or [None, "?"])[1]
judge("B1", "unique repo raw -SNAPSHOT rewrite", pat(l1a) and pat(l1b), f"b={l1b}")
same_trip = l1a == l2a.replace(".pom", ".jar") if l1a else False
same_trip_b = l1b == l2b.replace(".pom", ".jar") if l1b else False
judge("B2", "buildNumber trip arithmetic",
      n_of(l1a) == "1" and n_of(l1b) == "1" and n_of(l3a) == "2" and n_of(l3b) == "2"
      and same_trip and same_trip_b,
      f"a: {n_of(l1a)},{n_of(l3a)} b: {n_of(l1b)},{n_of(l3b)} same-trip a={same_trip} b={same_trip_b}")

# B3: already-unique name untouched
raw_loc = lambda side, name: next(
    (l.split(":", 1)[1].strip() for l in rd(side, name + ".hdr").splitlines()
     if l.lower().startswith("location:")), "")
judge("B3", "already-unique passthrough",
      raw_loc("a", "b6-put-unique-name").endswith("probe-app-2.0.0-20240819.101500-7.jar")
      and raw_loc("b", "b6-put-unique-name").endswith("probe-app-2.0.0-20240819.101500-7.jar"))

# B7/B8 (L014-2 additions): sidecar registration-only
l7a, l7b = loc("a", "b7-put-snap-sidecar"), loc("b", "b7-put-snap-sidecar")
ok7 = status("a", "b7-put-snap-sidecar") == "201" and status("b", "b7-put-snap-sidecar") == "201" \
      and not l7a.endswith(".sha1") and not l7b.endswith(".sha1")
judge("B7", "sidecar 201 Location=target, registration only", ok7, f"b={l7b}")
sc_a = {x for x in names("a", "b8-list-after-sidecar") if ".sha1" in x or ".md5" in x}
sc_b = {x for x in names("b", "b8-list-after-sidecar") if ".sha1" in x or ".md5" in x}
judge("B8", "no sidecar items in listing", not sc_a and not sc_b, f"b sidecars={sorted(sc_b)}")

# C1: hand metadata PUT (expected divergence — UNKNOWN)
bogus_a = "9.9.9-bogus" in rd("a", "c1b-get-after-put.body")
bogus_b = "9.9.9-bogus" in rd("b", "c1b-get-after-put.body")
judge("C1", "hand metadata PUT visibility", bogus_a == bogus_b,
      f"UNKNOWN dossier: bogus visible a={bogus_a} b={bogus_b}; clean both after deploy="
      f"{'9.9.9-bogus' not in rd('a','c1d-get-after-deploy.body') and '9.9.9-bogus' not in rd('b','c1d-get-after-deploy.body')}")

# C2: parallel different versions merge
va, vb = versions_of(rd("a", "c2c-get-groupmeta.body")), versions_of(rd("b", "c2c-get-groupmeta.body"))
judge("C2", "parallel deploy merge", "4.0.0" in va and "4.1.0" in va and va == vb, f"a={va} b={vb}")

# C3: parallel same-version unique snapshot — the L013 bar compared the
# SNAPSHOT-directory metadata face too (the reference loses its own document)
ok3 = status("a", "c3a-put-jar-race1") == "201" and status("a", "c3b-put-jar-race2") == "201" \
      and status("b", "c3a-put-jar-race1") == "201" and status("b", "c3b-put-jar-race2") == "201" \
      and status("a", "c3c-get-snapdir-meta") == status("b", "c3c-get-snapdir-meta")
judge("C3", "parallel same-version (metadata face incl.)", ok3,
      f"UNKNOWN dossier: snapdir meta a={status('a','c3c-get-snapdir-meta')} b={status('b','c3c-get-snapdir-meta')} "
      f"(reference loses its own metadata)")

# D1-D4: checksum policy tri-state
judge("D1", "client-checksums matching sidecar 201",
      status("a", "d2-cc-sha1-ok") == status("b", "d2-cc-sha1-ok") == "201")
judge("D2", "client-checksums mismatch 409",
      status("a", "d3-cc-sha1-bad") == status("b", "d3-cc-sha1-bad") == "409",
      "normalize candidate: path-component wording")
judge("D3", "server-generated tolerate 201",
      status("a", "d3-sg-sha1-bad") == status("b", "d3-sg-sha1-bad") == "201")
judge("D4", ">1024B sidecar 409",
      status("a", "d4-huge-sidecar") == status("b", "d4-huge-sidecar") == "409")

# D5 (E2's curl face): no phantom sidecar items
na, nb = names("a", "d5-list-cc-dir"), names("b", "d5-list-cc-dir")
judge("D5", "post-sidecar listing item set", na == nb, f"a={sorted(na)} b={sorted(nb)}")

# E1/E2: real mvn CLI + storage face
ea = (ROOT / "a" / "maven" / "mvn-exit.txt").read_text().strip()
eb = (ROOT / "b" / "maven" / "mvn-exit.txt").read_text().strip()
judge("E1", "mvn deploy exit 0", ea == eb == "mvn exit=0", f"{ea} / {eb}")
na, nb = names("a", "e2-snapdir-list"), names("b", "e2-snapdir-list")
judge("E2", "post-deploy storage item set (BUG 2)", na == nb, f"a={len(na)} b={len(nb)} items")

# ---- report ----
same = sum(1 for v in verdicts if v[2] == "一致")
print(f"| # | 臂 | 判定 | 说明 |")
print(f"|---|---|---|---|")
for arm, title, verdict, note in verdicts:
    print(f"| {arm} | {title} | {verdict} | {note} |")
print(f"\n一致 {same} / 差异 {len(verdicts)-same}（共 {len(verdicts)} 臂）")
sys.exit(0)
