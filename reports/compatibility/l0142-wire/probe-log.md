# L014-2 probe log — live-reference observations behind the snapshot rewrite

Source: artifactory-ux :8082 (Artifactory Pro 7.161.20), admin-authenticated
serial curl probes in the `l014p-mvn-u` / `l014p2-mvn-u` namespaces (deleted
after capture; raw `.hdr`/`.body` of the review-B F1 round live in
`reports/compatibility/l0142-wire/probe/`). Every row names the BinFlow
implementation branch that consumes it (`internal/adapter/maven/snapshot.go`).
Timestamps below are the probe clock (UTC) 2026-09-12/13.

## Round 1 — the seventeen observations that pinned the original model

| # | Sequence (repo `l014p-mvn-u`, unique) | Reference landing | BinFlow branch |
|---|---|---|---|
| 1 | jar `2.0.0-SNAPSHOT` into a fresh dir | `2.0.0-235258-1` | leader mint `(now,1)` with no trip |
| 2 | same-second pom (L013 B-b2 replay) | same `ts-1` | follower joins the files' trip (pomless dir) |
| 3 | jar re-PUT 13s later, DIFFERENT bytes | SAME `235258-1` (overwrite in place) | leader coordinate reuse `existing.N >= cand` |
| 4 | `-sources.jar` (fresh classifier, no metadata) | `235258-1-sources` | follower files fallback |
| 5 | `;build.timestamp=20200101.111111` on a coordinate re-PUT | same `235258-1`, property stored on the node | coordinate reuse ignores the property |
| 6 | pom (valid) into the same no-metadata dir | `235258-1.pom` | follower files fallback (pomless dir) |
| 7 | `-javadoc.jar` with metadata `(235258,1)` present, 20s later | `235258-1-javadoc` | classifier joins the CURRENT trip |
| 8 | fresh dir `5.0.0`, pom first | `235652-1` | follower mint `(now,1)` (empty dir) |
| 9 | jar `;build.timestamp=20200101.111111` after #8's metadata | `235654-2` — NOW, not the property | leader mint ts = server now; build.timestamp does NOT steer (spec §1.3 drift, reported) |
| 10 | `4.0.0` dir: pom first | `235519-1` | follower mint (empty dir) |
| 11 | jar 2.5s later (metadata `(235519,1)`) | `235521-2` | leader mint `(now, trip+1)` |
| 12 | jar re-PUT, different bytes (jar coordinate at N=2, metadata at N=1) | SAME `235521-2` | coordinate reuse (`2 >= 2`) |
| 13 | jar re-PUT again | SAME `235521-2` | same |
| 14 | `-sources.jar` (metadata lags at `(235519,1)`, jar at `235521-2`) | `235519-1-sources` — the METADATA trip, not the newest file | classifier joins the pom-sourced trip |
| 15 | `-SNAPSHOT.jar.sha1` after the main landed | 201, Location = the REWRITTEN main file, NO item | companion = existing same-coordinate file, registration-only |
| 16 | release dir: `6.0.0.jar` then `.sha1` | 201, Location = `6.0.0.jar` (no `.sha1`), originalChecksums registered, no item | BUG 2 registration-only |
| 17 | one real `mvn deploy` (wagon), snapdir listing | 3 items: pom, jar, maven-metadata.xml — all client `.sha1/.md5` PUTs registered, none materialized | BUG 2 (L013's "5 vs 15" was two-deploy accumulation; single-deploy truth is 3 vs 9) |

## Round 2 (review B, F1) — the pom rule and its neighbours

Raw wire: `probe/f1a-*`, `probe/f1b-*`, `probe/n1..n6-*`.

| # | Sequence (dir `7/8/9.0.0-SNAPSHOT`) | Reference landing | BinFlow branch |
|---|---|---|---|
| 18 | f1a: pom → (025159-1); pom RE-PUT 6s later (metadata exists) | `025205-2` — a NEW trip, never a join | pom branch: `N = trip+1`, ts = now when N is untouched |
| 19 | f1b: pom → (025220-1); jar → (025223-2); pom again | `025223-2.pom` — takes the JAR's timestamp at N=2 | pom branch ts = max ts among files already at `N` |
| 20 | f1b aftermath: `-javadoc.jar` with metadata now `(025223,2)` | `025223-2-javadoc` | classifier joins the current trip (unchanged) |
| 21 | n6: third pom (trip `(025223,2)`, nothing at N=3) | `025357-3` — now | pom branch, untouched-N case again |
| 22 | n1/n2: seed unique `20260101.000001-1.jar` + `20260202.000002-5.jar` (no pom ⇒ reference metadata 404) | both verbatim (already-unique never rewrites) | passthrough |
| 23 | n3: `-sources.jar` in that no-metadata mixed-trip dir | `20260101.000001-1-sources` — the FIRST/low trip, not the max | follower files fallback = first unique file in listing order (BinFlow reachability note below) |
| 24 | n4: `-SNAPSHOT.jar` in the same dir | `20260101.000001-1` — reuse at cand=1 | leader coordinate reuse with no trip |

## Derived model (as implemented)

```
trip      = newest unique POM's (ts, N)        # pom-sourced: the reference's
           # version-dir metadata exists ONLY when a pom does; nil otherwise
pom       : trip ? (trip.N+1, ts of a file at that N else now) : follower
classifier: trip ? join trip : first unique file of listing : (now, 1)
leader    : cand = trip ? trip.N+1 : 1
            existing same-coordinate at N >= cand ? reuse : (now, cand)
companion : existing same-coordinate file's spelling : follower spelling
```

Reachability note for rows 23/24: on BinFlow the pomless fallback is
practically dead — every unique landing fires the synchronous version-dir
recalculation, so a pomless directory gains BinFlow metadata from its first
file, while the reference's stays absent. The fallback therefore only
engages when the calculator seam is disabled or mid-race; its
first-file-of-listing pick matches the probe where both are observable.

## N1 material (next-round adjudication ticket)

- build.timestamp arrives as a deploy matrix parameter (`;build.timestamp=…`)
  and lands as a node property; it never steers the mint timestamp (rows
  5/9). Spec §1.3 claims preference — wire disagrees twice.
- Epoch-millis and other carried shapes were NOT probed; the property
  surface (`X-Checksum-*`-style header forms, property key spellings) is the
  open set for the adjudication ticket.
