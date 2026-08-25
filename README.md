# BinFlow

**English** | [简体中文](README.zh-CN.md)

BinFlow is a cloud-native artifact repository written from scratch in Go, with
an architecture and concept model aligned with JFrog Artifactory —
repositories, storage, permissions and REST semantics map one-to-one, so an
Artifactory shop can migrate without relearning the vocabulary. One static
binary, zero external dependencies, five package ecosystems served natively:
**Generic (raw HTTP), Docker Registry v2 (images/OCI, Helm charts via oras),
Maven, npm and PyPI** — each across **local, remote (pull-through proxy cache)
and virtual (aggregating)** repository types, with an embedded web console.

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

| What | Where |
|---|---|
| Product vision & scope | [`PRODUCT.md`](PRODUCT.md) |
| Milestones (M1 kernel → M9 hardening, all done) | [`ROADMAP.md`](ROADMAP.md) |
| M9 requirements (PRD v1.0: users/groups endpoints, usage batch, permissions filter, GC race fix) | [`docs/prd/milestone-9.md`](docs/prd/milestone-9.md) |
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
  sweep for missed events. Status: `GET /api/v1/replication/status`.
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

Per-deployment install guides (binary, Docker, compose, Helm, K8s manifests,
systemd, offline/air-gapped, upgrade): [`docs/user/install/`](docs/user/install/).
Per-protocol client integration (docker/mvn/npm/pip settings snippets):
[`docs/user/`](docs/user/README.md). API reference: [`docs/user/api-reference.md`](docs/user/api-reference.md).
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
  backend: "local"          # "s3" + storage.s3 section → object storage (see S3 guide)
security:
  anonymous_access: true    # see Security notes
logging:
  level: "info"             # debug | info | warn | error
  format: "json"
auth:
  oidc: {}                  # auth.oidc section → SSO (see OIDC guide)
  ldap: {}                  # auth.ldap section → directory login (see LDAP guide)
```

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

Pre-GA software. All nine milestones are done and tagged (`m1-done` …
`m9-done`); see `ROADMAP.md` for the milestone plan, `BOARD.md` for what
is currently being worked on, and `reports/` for iteration reports. The
next leg — aligning the full Artifactory feature surface — is tracked
entry by entry in
[`docs/reverse/artifactory-full-feature-matrix.md`](docs/reverse/artifactory-full-feature-matrix.md)
(213 entries as of M9: 20 present, 50 partial, 133 missing, 10 n/a by
design).
