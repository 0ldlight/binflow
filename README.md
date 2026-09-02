# BinFlow

**English** | [简体中文](README.zh-CN.md)

BinFlow is a cloud-native artifact repository written from scratch in Go, with
an architecture and concept model aligned with JFrog Artifactory —
repositories, storage, permissions and REST semantics map one-to-one, so an
Artifactory shop can migrate without relearning the vocabulary. One static
binary, zero external dependencies, **twelve package ecosystems** served
natively — see the matrix below — each across **local, remote (pull-through
proxy cache) and virtual (aggregating)** repository types (docker's remote
landed in M14 at the community tier; its virtual aggregation remains
unserved), with an embedded web console.

M6 added the enterprise layer (OIDC / LDAP SSO, S3 blob storage with online
migration, push replication, Prometheus metrics, `bf` CLI, `bf-migrate`).
M7–M9 hardened that into the full pre-GA shape: fine-grained RBAC
(`user` / `readonly_admin` / `admin` roles plus a `manage` action that
delegates repo-level administration), docker blob uploads that resume
across server restarts (kill -9 included), and a web console whose
information architecture and workflows align with Artifactory — same
action, same place, backed by a 24-task operation-path map. The server
side closed its own gaps (user/group lifecycle endpoints, batch storage
usage, manage-filtered permission lists, a concurrency-safe GC, step-up
token minting for OIDC users), and release images are dual-arch
(linux/amd64 + linux/arm64).

M10 introduced the **license & add-on tier system** (community floor /
pro / enterprise; gate on repo creation and write verbs, reads never
held hostage) with go/nuget/cargo as the first gated package types plus
the artifact-properties system. M11 widens the matrix to twelve package
types (conan, helm, rpm, debian join at pro), adds the **runtime auth
configuration plane** (LDAP/OIDC/SAML editable in the console or over
REST, effective on save — no restart), the standalone
**`binstore.yaml` storage-chain file** (ordered provider chain with
fail-fast coexistence rules), GPG **keypair signing** for debian/rpm
repository metadata, and the remote-cache **unused-cleanup engine**.

M12 lands the **artifact-lifecycle domain**: a
copy/move/zip/`archive!`/explode **operations family** and a **Trash
Can** (local deletes are captured into the built-in `auto-trashcan`
repository with five-tuple provenance properties, restorable over REST,
14-day default retention swept hourly — both pro feature slots). The
**NuGet surface completes** (the full v2 OData route set including
`$batch`, and v3 remote/virtual search proxying the upstream
SearchQueryService live with service-index-driven dynamic resolution),
**dual-write turns fail-open** during an S3 outage (disk-first writes
into a persistent replay queue, reads fall back to the disk superset,
automatic drain + reconciliation on recovery — ADR-0040), the
chunked-upload REST plane took the full **Artifactory MPU shape**
(jfrog-cli verified), and the console's visual layer was re-skinned onto
**native MUI defaults** (hand-written base.css 983 → 443 lines).

M13 (done) adds the **webhook unified event plane**: the official
seven-endpoint subscription family under `/binflow/event/api/v1` over a
66-type/13-domain closed set (9 wired today), HMAC-SHA256 signed
delivery with official retry semantics (5 attempts first-counted, fixed
10s wait, 30s per-attempt budget, 4xx terminal), a per-instance
troubleshooting ring and five Prometheus families — pro slot, with the
SSRF posture defaulting to rejecting private targets. The **HelmOCI
repository family completes** (remote pull-through with Bearer upstream
auth and cache-first degradation, virtual member aggregation with
first-seen routing), helm remotes gain the **`chartsBaseUrl` divergent
fetch base** with `_external`/`_transitive` on-disk caching, conan v1
recipe DELETE becomes a **whole-tree delete**, and the long-awaited
**runtime knobs** land (`folder_download` six fields +
`trashcan.retention_days`, restart-effective, echoed over
`GET /api/v1/system/settings`).

M14 (done) opens **docker remote pull-through** (community tier,
self-referenced upstream verified with digest parity and frozen upstream
counts on re-pull), fixes **npm legacy `login`** (bare
`npm login --auth-type=legacy` now mints a token end to end), pins the
Helm chart's **PVC keep posture** (`helm uninstall` intentionally survives
the artifact volume), and lands the **brand logo + per-package-type icon
set** across the console and this documentation site.

## Package type matrix (with tiers)

| Tier | Package types | Notes |
|---|---|---|
| **community** (floor — runs with no license at all) | generic, docker, maven, npm, pypi | The five core types: all M1–M9 capability, plus the properties system |
| **pro** | go, nuget, cargo (M10) · conan, helm, rpm, debian (M11) · helmoci, artifact-operations + trash-can feature slots (M12; trash tier provisional) · webhook (M13) | Repo creation and pushes require a pro-or-higher license; existing artifacts stay readable when a license lapses |
| **enterprise** | (feature slots: ha, xray-integration) | Placeholder slots; the bodies land in a later milestone (M14+) |

Tier semantics in one line: **reads are never held hostage** — an expired or
missing license only closes repo-creation (400) and write verbs
(403 + `X-Binflow-License-Required: <addon>`); `GET /binflow/api/v1/addons`
shows the live per-slot verdict. Full guide:
[`docs/user/admin/license.md`](docs/user/admin/license.md).

| What | Where |
|---|---|
| Product vision & scope | [`PRODUCT.md`](PRODUCT.md) |
| Milestones (M1 kernel → M14 UI-parity pass; M1–M14 done) | [`ROADMAP.md`](ROADMAP.md) |
| M14 requirements (PRD: Artifactory interaction parity, protocol + brand logos, docker remote first flight, server small-fix pack) | [`docs/prd/milestone-14.md`](docs/prd/milestone-14.md) |
| M13 requirements (PRD: webhook event bus, HelmOCI completion, config knobs, behavior-debt closure) | [`docs/prd/milestone-13.md`](docs/prd/milestone-13.md) |
| M12 requirements (PRD: NuGet completion, artifact lifecycle, behavior-debt closure) | [`docs/prd/milestone-12.md`](docs/prd/milestone-12.md) |
| Artifactory full-feature matrix (213 entries — the M10+ roadmap backbone) | [`docs/reverse/artifactory-full-feature-matrix.md`](docs/reverse/artifactory-full-feature-matrix.md) |
| Help documentation center (install / integrations / admin / API / FAQ) | [`docs/user/README.md`](docs/user/README.md) |
| Architecture spec | [`docs/design/architecture.md`](docs/design/architecture.md) |
| Reverse-engineered behavior specs | [`docs/reverse/`](docs/reverse/) |
| Decisions (ADR log) | [`DECISIONS.md`](DECISIONS.md) |
| Task board | [`BOARD.md`](BOARD.md) |
| Iteration reports | [`reports/`](reports/) |

Everything below the line is **copy-paste runnable**. All URLs use the single
`/binflow` prefix: management/compat endpoints under `/binflow/api/...`,
content under `/binflow/<repo>/<path>` (two root-level exceptions: `/metrics`
and the docker `/v2/...` routes).

## Quick start

Five steps: **build → start → create repo → upload → download → verify**.
Two interchangeable paths are given — the bare binary (no Docker needed) and
docker compose. Examples assume `bash`/`zsh`, `curl`, `jq` and
`sha256sum` (macOS: `shasum -a 256` works the same).

Set the constants once (adjust `BASE` if you changed the port):

```bash
export BASE=http://localhost:8080
export ADMIN_PW=password    # evaluation default; how to change it: step 0 below
```

> **Step 0 (recommended): set a real admin password before the first boot.**
> BinFlow seeds the `admin` account on first start. Without
> `BINFLOW_ADMIN_PASSWORD` set, the password is the documented default
> `password` — **evaluation only**, never production. The server logs a WARN
> while the default is in effect. Setting the variable later (after the first
> boot) does not re-seed; rotate with
> `curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/password ...`
> (see "Security notes" below).

### Path A — bare binary (no Docker)

**1. Build**

```bash
make build
# three binaries: bin/binflow-server, bin/bf (CLI), bin/bf-migrate (migration tool)
```

**2. Start**

```bash
./bin/binflow-server serve
# ... binflow starting ... binflow listening addr=:8080
curl -s $BASE/binflow/api/system/ping
# OK
```

Defaults: listens on `:8080`, data in `./data` (created on demand; sqlite +
`blobs/` + `uploads/` live there — upload sessions themselves are rows in
the sqlite store). Override with a config file or env — see
[Configuration](#configuration). `Ctrl-C` stops it gracefully (exit 0).
A throwaway self-test exists: `scripts/smoke.sh` boots an ephemeral instance
and runs the ping → create → upload → download chain.

**3. Create a repository**

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/generic-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic","description":"quick start"}'
# Successfully created repository 'generic-local'
```

**4. Upload** (server computes and echoes the checksums)

```bash
echo "hello binflow" > hello.txt
SHA=$(sha256sum hello.txt | cut -d' ' -f1)
curl -su admin:$ADMIN_PW -T hello.txt $BASE/binflow/generic-local/acme/hello.txt | jq .
# {
#   "repo": "generic-local",
#   "path": "/acme/hello.txt",
#   ...
#   "checksums": { "sha1": "...", "md5": "...", "sha256": "<same as $SHA>" },
#   ...
# }
```

**5. Download and verify** (note: this GET is anonymous — see below)

```bash
curl -s -o hello.dl.txt $BASE/binflow/generic-local/acme/hello.txt
diff hello.txt hello.dl.txt && echo "content identical"
sha256sum hello.dl.txt   # == $SHA
```

### Path B — docker compose

**0. Configure** (one-time; `.env` is gitignored)

```bash
cd deploy/dev
cp .env.example .env
# edit BINFLOW_ADMIN_PASSWORD — compose refuses to start without it
```

**1+2. Build and start** (back at the repo root; first build pulls the Go
toolchain image, later builds are incremental)

```bash
docker compose -f deploy/dev/docker-compose.yml up -d --build
docker compose -f deploy/dev/docker-compose.yml ps
# NAME           STATUS                   PORTS
# binflow-dev    Up ... (healthy)         127.0.0.1:8080->8080/tcp
curl -s $BASE/binflow/api/system/ping
# OK
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/health | jq .
# { "status": "ok", ... }   (this endpoint requires auth)
```

Steps **3–5** (create / upload / download) are byte-for-byte the same
commands as Path A — that is the point of the unified `/binflow` prefix.

Data persists in the named volume `binflow_binflow-data` mounted at
`/var/lib/binflow`: `docker compose restart` and `docker compose down &&
up -d` (without `-v`) both keep your artifacts. `down -v` wipes it.

If host port 8080 is taken, set `BINFLOW_BIND_PORT=18080` in
`deploy/dev/.env`, re-run `up -d`, and export
`BASE=http://localhost:18080`.

Stop / clean up:

```bash
docker compose -f deploy/dev/docker-compose.yml down       # keep data
docker compose -f deploy/dev/docker-compose.yml down -v    # wipe data
```

### What you just exercised

- ping, create repo, upload with checksum reconciliation, download with
  checksum verification, anonymous read — the M1 acceptance chain, still the
  fastest health check for any instance (full matrix:
  `docs/prd/milestone-1.md` §5.3).
- Uploads are atomic (an interrupted upload never becomes visible), blobs
  are content-addressed by sha256 and deduplicated across every path and
  repo, and deletes are idempotent.

## Beyond curl: what M6–M9 bring and where to read

The same instance grows into the enterprise surface without changing shape:

- **Web console** — log in at `http://localhost:8080/binflow/ui/`
  (repository CRUD, artifact tree, upload/download, search, users/groups,
  audit, GC, quota panels). Guide: [`docs/user/console.md`](docs/user/console.md).
- **SSO capability discovery** — the login page (and any client) can probe
  which entry points an instance offers before any credential exists:

  ```bash
  curl -s $BASE/binflow/api/v1/auth/methods
  # { "password": true, "oidc": false, "ldap": false }
  ```

  Enable OIDC or LDAP and the respective bit flips true; guides:
  [OIDC](docs/user/guides/oidc-config.md) (Keycloak/Okta/Azure AD, PKCE,
  group & admin mapping) and [LDAP](docs/user/guides/ldap-config.md)
  (OpenLDAP/AD, local-first fallback, ldaps/StartTLS).
- **S3 object storage** — `storage.backend: s3` (AWS S3, MinIO) with the
  same blob layout; running instances migrate online via dual-write +
  resumable background copy (`GET /api/v1/storage/migration` for progress).
  Guide: [S3 后端与在线迁移](docs/user/guides/s3-config.md) (中文).
- **Push replication** — uploads replicate one-way to a target instance
  (all five protocols), event-driven with exponential backoff and a cron
  sweep for missed events. On-demand full resync (`POST
  /api/v1/replications/{id}/run`), pre-save target probing (`…/test`) and a
  global push/pull block brake (`/api/v1/system/replications`) ship with M15.
  Status: `GET /api/v1/replication/status`.
- **AQL search (M15)** — Artifactory Query Language subset over the items
  domain: `curl -su admin:$PW -X POST $BASE/binflow/api/search/aql
  --data-binary 'items.find({"repo":"maven-local"}).limit(10)'`, plus the
  classic gavc/prop/pattern endpoints and an AQL mode in the console search
  page. Guide: [AQL 搜索指南](docs/user/aql.md) (中文).
- **Prometheus metrics** — `curl -s $BASE/metrics` (root-level, anonymous
  by default): HTTP, storage, auth and replication families.
  Reference: [Prometheus 指标参考](docs/user/metrics/prometheus-reference.md) (中文).
- **`bf` CLI** — repo/artifact/user/token without hand-written JSON:

  ```bash
  export BF_BASE_URL=$BASE BF_USERNAME=admin BF_PASSWORD=$ADMIN_PW
  bf repo create demo --type local --package-type generic
  bf artifact upload hello.txt --repo demo --path acme/hello.txt
  bf token create          # value printed once
  ```

  Manual: [`docs/user/guides/bf-cli.md`](docs/user/guides/bf-cli.md)
  (profiles in `~/.bf/config.yaml`, secrets stay in env).
- **`bf-migrate`** — move repositories, users and the token ledger from an
  Artifactory instance into a fresh BinFlow (`--dry-run` first, `--resume`
  after interruptions, `migration_report.json` at the end). Guide:
  [从 Artifactory 迁移](docs/user/guides/migrate-artifactory.md) (中文).

### M7 — fine-grained RBAC, restart-resumable docker uploads, step-up tokens

- **Three-valued roles** — every user carries `user`, `readonly_admin` or
  `admin` (the `adminRole` wire field). `readonly_admin` reads every
  management surface but never writes; role changes bind the user's
  existing API tokens immediately (no re-issue, no restart). Only admins
  may assign roles.

  ```bash
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/auditor \
    -H 'Content-Type: application/json' \
    -d '{"name":"auditor","email":"auditor@t.io","password":"auditor-pw-1","adminRole":"readonly_admin"}' \
    -o /dev/null -w '%{http_code}\n'
  # 201
  curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users/auditor | jq '{name,adminRole,enabled}'
  # {"name":"auditor","adminRole":"readonly_admin","enabled":true}
  ```

- **`manage` = repo-level administration** — a fourth permission-target
  action. Grant it on a target and the holder administers the covered
  repositories (edit targets, quotas, repo config) without being a
  platform admin:

  ```bash
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/carol \
    -H 'Content-Type: application/json' \
    -d '{"name":"carol","email":"carol@t.io","password":"carol-pw-123"}' -o /dev/null
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/app-local \
    -H 'Content-Type: application/json' \
    -d '{"rclass":"local","packageType":"generic"}' -o /dev/null
  curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/permissions \
    -H 'Content-Type: application/json' \
    -d '{"name":"t-app","repos":["app-local"],"includePatterns":["**"],
         "principals":{"users":{"carol":["read","write","delete","manage"]}}}' \
    -o /dev/null -w '%{http_code}\n'
  # 201 — carol now administers app-local (targets she grants take effect
  # immediately, no platform admin involved)
  ```

  Guide: [`docs/user/admin/rbac-roles.md`](docs/user/admin/rbac-roles.md) (中文).
- **Docker uploads survive restarts** — a chunked blob upload interrupted
  by `kill -9`, SIGTERM or `docker compose restart` resumes from the last
  received byte once the server is back (three paths, symmetric behavior).
- **Token minting step-up** (optional, **off by default**) — minting a
  token from a console session then requires a second factor: password
  re-entry for local/LDAP users, a fresh IdP round-trip (`prompt=login`)
  for OIDC. Guide: [`docs/user/admin/token-step-up.md`](docs/user/admin/token-step-up.md) (中文).

### M8 — the console speaks Artifactory

- The web console's information architecture and interaction flows follow
  Artifactory's: a dual-mode shell (application / administration), the
  cross-repository artifact tree with deep links, Set Me Up and Deploy
  dialogs, keyboard-accessible throughout — in BinFlow's own skin with
  zero copied assets. An Artifactory user lands and knows where everything
  is; the task-by-task operation path map (24 common tasks) lives at
  [`docs/user/artifactory-path-map.md`](docs/user/artifactory-path-map.md) (中文),
  the console guide at [`docs/user/console.md`](docs/user/console.md) (中文).

### M9 — server-side gap closure

- **User & group lifecycle** — `enabled` echoes on every read surface;
  `DELETE /binflow/api/security/users/{name}` removes a user with full
  cascade (their API tokens and sessions answer 401 from that moment on;
  the built-in admin and self-deletion are refused);
  `GET /api/security/groups/{name}?includeUsers=true` returns the member
  list.

  ```bash
  curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/users/auditor \
    -o /dev/null -w '%{http_code}\n'
  # 200
  curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/users/admin
  # Cannot delete the built-in admin user.
  ```

- **One request instead of N** — `GET /api/v1/storage/usage` fills the
  whole "used" column in one call (a 150-repo instance used to fire ~170
  requests for it); `GET /api/v1/permissions?filter=manage` hands a
  manage-holder exactly their editable targets — carol from the M7
  example:

  ```bash
  curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/usage | jq length
  # 2 — every repo you can see, one request (generic-local + app-local)
  curl -su carol:carol-pw-123 "$BASE/binflow/api/v1/permissions?filter=manage" | jq '.[].name'
  # ["t-app"]
  curl -su carol:carol-pw-123 $BASE/binflow/api/v1/permissions -o /dev/null -w '%{http_code}\n'
  # 403 — the unfiltered list stays admin/readonly_admin-only
  ```

- **Concurrency-safe GC** — `gc --apply` re-verifies references
  immediately before every physical delete: parallel CI pushes and
  `graceHours=0` collections no longer race (verification suites run at
  default parallelism again, no `--workers=1` crutch).
- **npm CI publishing needs only `write`** — publishing any number of new
  versions (plus dist-tag moves) is the standard path and needs no
  `delete`; only `npm deprecate` / overwriting published metadata does.
  Guide: [`docs/user/integrations/npm.md`](docs/user/integrations/npm.md) (中文).
  Release images are dual-arch (linux/amd64 + linux/arm64 manifests).

### M10/M11 — tier gating, four more package types, config planes

- **License & add-on tiers** — the matrix above. Install a license by
  pasting it in the console (License & Add-ons) or
  `POST /binflow/api/system/license`; `GET /binflow/api/v1/addons` reports
  the live per-slot verdict. Guide (中文):
  [`docs/user/admin/license.md`](docs/user/admin/license.md).
- **Four M11 package types (pro)** — verified with real clients
  (conan 2.31/1.66, helm 4.2, Rocky 9 dnf, debian bookworm apt):

  ```bash
  # conan: conan remote add + upload/install with revision chains
  conan remote add binflow $BASE/binflow/conan-local && conan remote login binflow admin -p "$ADMIN_PW"
  # helm: classic chart repo with automatic index.yaml
  helm repo add binflow $BASE/binflow/helm-local && helm install my-rel binflow/mychart
  # rpm/debian: repodata / dists index engines + GPG-signed metadata
  dnf install -y <pkg>   # baseurl=$BASE/binflow/rpm-local
  ```

  Guides (中文): [Conan](docs/user/integrations/conan.md) ·
  [Helm](docs/user/integrations/helm-charts.md) ·
  [RPM](docs/user/integrations/rpm.md) ·
  [Debian](docs/user/integrations/debian.md).
- **Auth configuration plane** — LDAP/OIDC/SAML sections editable at
  runtime (console `/admin/security/auth` or
  `GET/PUT /binflow/api/v1/admin/security/{ldap,oauth,saml/config}`),
  effective on the next request; secrets are write-only (masked echo) and
  sealed with the instance master key. Guide (中文):
  [`docs/user/admin/auth-config.md`](docs/user/admin/auth-config.md).
- **`binstore.yaml`** — the storage provider chain as a standalone file
  next to `binflow.yaml` (`[filestore]`, `[s3]`, or the `[filestore, s3]`
  dual-write migration chain); coexistence with the embedded
  `storage:` section is fail-fast on divergence. Guide (中文):
  [`docs/user/admin/storage-config.md`](docs/user/admin/storage-config.md).
- **GPG keypair signing & unused-cleanup** — server-side keypair
  management signs debian `InRelease`/`Release.gpg` and rpm
  `repomd.xml.asc`/`.key` (real apt/dnf gpgcheck chains verified); the
  cleanup engine reclaims idle remote-cache artifacts on an hourly cron
  (`POST /binflow/api/v1/system/cleanup` for manual dry-run/apply).

### M12 — artifact lifecycle, NuGet completion, fail-open dual-write (in progress)

- **Copy / move / `archive!` / explode** — the operations family rides one
  pro feature slot (`repo-operations`); tree copies are zero-copy (blob
  ledger references), `PUT` with `X-Explode-Archive: true` unpacks into
  the repository (the archive itself is not stored), and
  `<archive>!/<entry>` reads a member without unpacking. Directory zip
  download ships behind `folderDownloadConfig` (default off pending its
  config knob — see the guide).

  ```bash
  # community instance: the family answers the addon gate (real curl)
  curl -u admin:$ADMIN_PW -X POST \
    "$BASE/binflow/api/copy/generic-local/src/a.bin?to=/dst/a.bin" -i | head -4
  # HTTP/1.1 403 Forbidden
  # X-Binflow-License-Required: repo-operations
  # {"errors":[{"status":403,"message":"license required: addon 'repo-operations' needs tier 'pro' (current: none)"}]}

  # pro: tree copy / dry run (response = Artifactory's CopyOrMoveResult)
  curl -su admin:$ADMIN_PW -X POST \
    "$BASE/binflow/api/copy/generic-local/acme?to=/staging/acme" | jq .
  # {"messages":[{"level":"INFO","message":"copying … completed successfully, N artifacts and M folders were copied"}]}

  # explode: upload an archive and expand it in place (201 + count header)
  curl -su admin:$ADMIN_PW -T bundle.zip -H 'X-Explode-Archive: true' \
    $BASE/binflow/generic-local/acme/bundle.zip -i | head -3
  ```

  Guide: [`docs/user/admin/artifact-operations.md`](docs/user/admin/artifact-operations.md) (中文).
- **Trash can** — local deletes are captured into the built-in
  `auto-trashcan` repository (five-tuple provenance properties), restorable
  over REST, 14-day default retention swept hourly (real-binary roundtrip,
  sha256-verified):

  ```bash
  curl -su admin:$ADMIN_PW \
    "$BASE/binflow/api/storage/auto-trashcan/vlibs/com/acme/v.jar?properties" | jq .
  # {"properties":{"trash.time":["1787952557679"],"trash.deletedBy":["admin"],
  #   "trash.originalRepository":["vlibs"],"trash.originalRepositoryType":["local"],
  #   "trash.originalPath":["com/acme/v.jar"],"license":["apache-2.0"]}}
  curl -su admin:$ADMIN_PW -X POST \
    "$BASE/binflow/api/trash/restore/vlibs/com/acme/v.jar?transaction-size=100" | jq .
  curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/trash/empty | jq .
  # {"removed":2,"files":1,"folders":1,"bytes":3}
  ```

  Guide: [`docs/user/admin/trash-can.md`](docs/user/admin/trash-can.md) (中文).
- **NuGet completion** — the v2 OData plane now serves the full route set
  (`Search()` / `Packages()` / `GetUpdates()` / `$batch` / `Download` /
  DELETE / dual-form PUT with the 409-already-exists arm), and v3
  remote/virtual search proxies the upstream SearchQueryService live
  (service-index dynamic resolution, semver2 registration family).
  Guide: [`docs/user/integrations/nuget.md`](docs/user/integrations/nuget.md) (中文).
- **Fail-open dual-write** — an S3 outage no longer 500s a `[filestore, s3]`
  chain: uploads land disk-first and enter a persistent replay queue,
  reads fall back to the disk superset, and recovery drains + reconciles
  automatically (`completed` mode refuses to boot on a non-empty queue).
  See [存储配置 · fail-open](docs/user/admin/storage-config.md) (中文).

Per-deployment install guides (binary, Docker, compose, Helm, K8s manifests,
systemd, offline/air-gapped, upgrade): [`docs/user/install/`](docs/user/install/).
Per-protocol client integration (docker/mvn/npm/pip/go/nuget/cargo/conan/helm/rpm/deb
settings snippets): [`docs/user/`](docs/user/README.md). API reference: [`docs/user/api-reference.md`](docs/user/api-reference.md).
FAQ & troubleshooting incl. the Artifactory→BinFlow concept mapping table:
[`docs/user/faq.md`](docs/user/faq.md).

## Security notes (read before exposing to a network)

- BinFlow serves **plain HTTP only** — TLS termination belongs to a reverse
  proxy in front of it (an nginx TLS template ships in `deploy/nginx/`).
  The compose file binds to `127.0.0.1` by default — keep it that way unless
  you are on a trusted network.
- **Anonymous read is ON by default** (aligned with the Artifactory
  tradition): `GET`/`HEAD` on content paths (`/binflow/<repo>/<path>`) need
  no credentials. Writes (`PUT`/`DELETE`) and all of `/binflow/api/**`
  **always** require authentication, whatever this switch says. Turn
  anonymous read off with either:

  ```bash
  # environment variable (compose: put it in deploy/dev/.env)
  BINFLOW_SECURITY_ANONYMOUS_ACCESS=false
  # or in binflow.yaml
  security:
    anonymous_access: false
  ```

  After a restart unauthenticated content GETs answer 401. Close this before
  letting anything beyond your own machine reach the instance.
- **Default admin password**: `admin` / `password` exists so a fresh
  instance is usable at once; it is an evaluation affordance only. Set
  `BINFLOW_ADMIN_PASSWORD` (env, never YAML — secrets stay out of config
  files, ADR-0009) before the first boot, or rotate afterwards:

  ```bash
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/password \
    -H 'Content-Type: application/json' \
    -d '{"oldPassword":"password","newPassword":"a-real-secret"}' \
    && export ADMIN_PW=a-real-secret
  # Password has been successfully changed
  ```

- Change the password, issue per-CI API tokens
  (`POST /binflow/api/security/token`), revoke them, and scope users to
  path patterns (`/binflow/api/v1/permissions`). OIDC/LDAP users go through
  the same permission model as local users.

## Configuration

One YAML file (`binflow.yaml`) plus `BINFLOW_`-prefixed env overrides
(`__` descends a level, e.g. `BINFLOW_SERVER__LISTEN=:9090`); env wins over
YAML. Secrets go through env only. Boot with no config file at all and you
get documented defaults. Full field list: `docs/design/architecture.md` §8.

```bash
./bin/binflow-server serve -c /path/to/binflow.yaml   # explicit file (must exist)
BINFLOW_HOME=/var/lib/binflow ./bin/binflow-server    # defaults anchored under $BINFLOW_HOME
BINFLOW_SERVER__LISTEN=:9090 ./bin/binflow-server     # single-knob override
```

```yaml
# binflow.yaml — the knobs you are most likely to touch
server:
  listen: ":8080"
storage:
  data_dir: "./data"        # blobs + uploads + sqlite all live here
  backend: "local"          # "s3" + storage.s3 section → object storage (see S3 guide);
                            #   M11+: prefer a binstore.yaml chain file next to this file
security:
  anonymous_access: true    # see Security notes
logging:
  level: "info"             # debug | info | warn | error
  format: "json"
auth:
  oidc: {}                  # auth.oidc section → SSO (see OIDC guide; seeds the
                            #   runtime config plane on first boot)
  ldap: {}                  # auth.ldap section → directory login (see LDAP guide)
```

Two M11 planes live outside this file:

- **`binstore.yaml`** (same directory) — the ordered storage provider chain:
  `[filestore]`, `[s3]`, or `[filestore, s3]` with a `migration.mode` of
  `bypass | dual-write | completed`. Absent file = zero behavior change;
  divergence with the embedded `storage:` chain keys refuses to boot.
  Guide: [`docs/user/admin/storage-config.md`](docs/user/admin/storage-config.md) (中文).
- **Auth sections at runtime** — after first boot the authoritative LDAP /
  OIDC / SAML configuration is the DB-backed plane (console or REST),
  edited live without restarts. Guide:
  [`docs/user/admin/auth-config.md`](docs/user/admin/auth-config.md) (中文).

## Operations

```bash
./bin/binflow-server gc                 # dry-run: list unreferenced blobs (default)
./bin/binflow-server gc --apply         # actually reclaim them (grace period applies)
./bin/binflow-server export <dir>       # online backup (server keeps running)
./bin/binflow-server import <dir>       # restore into an empty data dir (server stopped)
./bin/binflow-server --version
```

`gc` only ever deletes blobs that no node references and that are older than
the grace window (24h by default) — interrupted uploads leave nothing
behind, and a deleted artifact's space comes back after grace. Since M9
`--apply` is concurrency-safe (references are re-verified immediately
before each physical delete), so it can run alongside active CI pushes
even with `graceHours=0`. The
backup/restore handbook: [`docs/user/admin/backup-restore.md`](docs/user/admin/backup-restore.md).

## Development

Toolchain: Go 1.26 (see `go.mod`), golangci-lint pinned in
`.tool-versions`. On a restricted network `GOPROXY=https://goproxy.cn,direct`
(the Makefile already exports this; ADR-0005).

```bash
make dev   # vet + lint + test + build, the pre-push gate
```

Common targets (`make help` lists them all):

| Target | What it does |
|---|---|
| `make build` | compile `bin/binflow-server` + `bin/bf` + `bin/bf-migrate` with CGO disabled (zero-CGo baseline, ADR-0005); prints binary sizes |
| `make test` | `go test -race ./...` |
| `make test-cov` | tests with a coverage profile (`coverage.out`) |
| `make lint` | golangci-lint (installs the pinned version if missing) |
| `make fmt` / `make vet` | gofmt / go vet |
| `make tidy` | sync `go.mod` / `go.sum` |
| `make run` | build, then `serve` on `:8080` with `./data` |
| `make dev` | vet + lint + test + build (pre-push gate) |
| `make docs` | build the Docusaurus help site embedded at `/binflow/docs/` |
| `make tools` | print toolchain versions |
| `make clean` | remove `bin/` and coverage output |

Layout follows `docs/design/architecture.md` §2: `cmd/` (`binflow-server`,
`bf`, `bf-migrate`) over the `internal/` packages (`config`, `storage`,
`metadata`, `auth`, `audit`, `repo`, `remote`, `adapter`, `replication`,
`migrate`, `metrics`, `client`, `httpapi`, `console`, `docs`); business
packages never reach into each other's internals. CI
(`.github/workflows/ci.yml`) runs the same Makefile targets — local and
CI share one entrypoint.

## License / status

Pre-GA software. Milestones M1–M11 are done and tagged (`m1-done` …
`m11-done`); M12 (NuGet surface completion, artifact lifecycle domain —
operations family and trash can — and behavior-debt closure) is in
progress — see `ROADMAP.md` for the milestone plan, `BOARD.md` for what
is currently being worked on, and `reports/` for iteration reports. The
alignment of the full Artifactory feature surface is tracked entry by
entry in
[`docs/reverse/artifactory-full-feature-matrix.md`](docs/reverse/artifactory-full-feature-matrix.md).
