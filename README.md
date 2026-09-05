# BinFlow

**English** | [简体中文](README.zh-CN.md)

BinFlow is a cloud-native artifact repository written from scratch in Go,
with an architecture and concept model aligned to JFrog Artifactory —
repositories, storage, permissions and REST semantics map one-to-one, so an
Artifactory shop can migrate without relearning the vocabulary. One static
binary, zero external dependencies, an embedded web console, **thirteen
package ecosystems** natively served — each across **local / remote
(pull-through proxy cache) / virtual (aggregating)** repository types.
Applies to BinFlow v1.0.0; full documentation at
[binflow.docs.buildwithfern.com](https://binflow.docs.buildwithfern.com).

## Package type matrix (with tiers)

| Tier | Package types |
|---|---|
| **community** (floor — runs with no license at all) | [generic](docs/user/integrations/generic.md) · [docker](docs/user/docker-registry.md) · [maven](docs/user/integrations/maven.md) · [npm](docs/user/integrations/npm.md) · [pypi](docs/user/integrations/pypi.md) |
| **pro** | [go](docs/user/integrations/golang.md) · [nuget](docs/user/integrations/nuget.md) · [cargo](docs/user/integrations/cargo.md) · [conan](docs/user/integrations/conan.md) · [helm](docs/user/integrations/helm-charts.md) (charts + helmoci) · [rpm](docs/user/integrations/rpm.md) · [debian](docs/user/integrations/debian.md), plus feature slots: artifact operations (copy/move/zip/`archive!`/explode), trash can, webhooks |
| **enterprise** | feature slots: ha, xray-integration (placeholders) |

Tier semantics in one line: **reads are never held hostage** — a missing or
expired license only closes repo creation (400) and write verbs
(403 + `X-Binflow-License-Required: <addon>`); `GET /binflow/api/v1/addons`
reports the live verdict. Guide:
[`docs/user/admin/license.md`](docs/user/admin/license.md). All URLs use the
single `/binflow` prefix — APIs under `/binflow/api/...`, content under
`/binflow/<repo>/<path>` (root exceptions: `/metrics`, docker `/v2/...`),
console at `/binflow/ui/`.

## Quick start

Five steps: **build → start → create repo → upload → download → verify**, on
two interchangeable paths — bare binary (no Docker) or docker compose.
Examples assume `bash`/`zsh`, `curl`, `jq`, `sha256sum`
(macOS: `shasum -a 256`).

```bash
export BASE=http://localhost:8080
export ADMIN_PW=password    # evaluation default; rotate: PUT /binflow/api/security/password
```

> **Step 0 (recommended):** set `BINFLOW_ADMIN_PASSWORD` (env, never YAML)
> before the first boot — without it the seeded `admin` password is the
> documented default `password` (**evaluation only**; later changes do not
> re-seed, rotate via `PUT $BASE/binflow/api/security/password`).

### Path A — bare binary (no Docker)

```bash
make build   # three binaries: binflow-server, bf (CLI), bf-migrate (migration tool)
./bin/binflow-server serve
# ... binflow listening addr=:8080
curl -s $BASE/binflow/api/system/ping
# OK
```

Defaults: `:8080`, data in `./data` (created on demand; sqlite + `blobs/` +
`uploads/` live there) — override via [Configuration](#configuration).

```bash
# 1. create a repository
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/generic-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic","description":"quick start"}'
# Successfully created repository 'generic-local'

# 2. upload (the server computes and echoes the checksums)
echo "hello binflow" > hello.txt
SHA=$(sha256sum hello.txt | cut -d' ' -f1)
curl -su admin:$ADMIN_PW -T hello.txt $BASE/binflow/generic-local/acme/hello.txt | jq .
# { "repo": "generic-local", "path": "/acme/hello.txt",
#   "checksums": { "sha1": "...", "md5": "...", "sha256": "<same as $SHA>" }, ... }

# 3. download and verify (content GETs are anonymous by default)
curl -s -o hello.dl.txt $BASE/binflow/generic-local/acme/hello.txt
diff hello.txt hello.dl.txt && echo "content identical"
sha256sum hello.dl.txt   # == $SHA
```

### Path B — docker compose

```bash
cd deploy/dev
cp .env.example .env      # edit BINFLOW_ADMIN_PASSWORD — compose refuses to start without it
cd -
docker compose -f deploy/dev/docker-compose.yml up -d --build
docker compose -f deploy/dev/docker-compose.yml ps
# binflow-dev    Up ... (healthy)    127.0.0.1:8080->8080/tcp
```

Steps 1–3 above are byte-for-byte the same commands — that is the point of
the unified `/binflow` prefix. Data persists in the named volume mounted at
`/var/lib/binflow` (`restart` and `down && up -d` keep it, `down -v` wipes
it). Port 8080 taken? Set `BINFLOW_BIND_PORT=18080` in `.env`, adjust `BASE`.

What you just exercised: ping, repo create, checksum-reconciled upload,
checksum-verified download, anonymous read. Uploads are atomic, blobs are
sha256-content-addressed and deduplicated across every path and repo.

## Capabilities

- **Web console** — repo CRUD, cross-repo artifact tree, users/groups, audit, GC, quota: [`docs/user/console.md`](docs/user/console.md)
- **SSO** — OIDC / LDAP with runtime config (effective on save): [OIDC](docs/user/guides/oidc-config.md) · [LDAP](docs/user/guides/ldap-config.md) · [auth config](docs/user/admin/auth-config.md)
- **Storage** — disk or S3 (AWS/MinIO), online dual-write migration, `binstore.yaml` provider chain: [S3](docs/user/guides/s3-config.md) · [storage config](docs/user/admin/storage-config.md)
- **Replication** — event-driven one-way push, on-demand full resync, global block brake: [governance](docs/user/admin/governance.md)
- **Search** — AQL (`items.find({...})`) plus gavc/prop/pattern endpoints, and the properties system: [AQL](docs/user/aql.md) · [properties](docs/user/properties.md)
- **Access control** — `user`/`readonly_admin`/`admin` roles, repo-level `manage`, API tokens with optional step-up: [RBAC](docs/user/admin/rbac-roles.md) · [step-up](docs/user/admin/token-step-up.md)
- **Artifact lifecycle** — copy/move/zip/`archive!`/explode and a trash can with restore + retention: [operations](docs/user/admin/artifact-operations.md) · [trash can](docs/user/admin/trash-can.md)
- **Webhooks** — HMAC-SHA256 signed delivery with retry semantics: [`docs/user/admin/webhooks.md`](docs/user/admin/webhooks.md)
- **Operations & observability** — concurrency-safe GC, online export/import backup, audit, quotas, Prometheus `/metrics`: [backup](docs/user/admin/backup-restore.md) · [governance](docs/user/admin/governance.md) · [metrics](docs/user/metrics/prometheus-reference.md)
- **Tools** — `bf` CLI and `bf-migrate` (Artifactory export): [bf CLI](docs/user/guides/bf-cli.md) · [migration](docs/user/guides/migrate-artifactory.md)

## Deployment

| Form | Guide |
|---|---|
| Single binary (linux/darwin/windows × amd64/arm64) | [`docs/user/install/binary.md`](docs/user/install/binary.md) |
| Docker image (multi-arch, distroless/alpine) | [`docs/user/install/docker.md`](docs/user/install/docker.md) |
| docker compose (TLS reverse proxy, MinIO profiles) | [`docs/user/install/compose.md`](docs/user/install/compose.md) |
| Helm Chart ([`charts/binflow/`](charts/binflow)) | [`docs/user/install/helm.md`](docs/user/install/helm.md) |
| Raw Kubernetes manifests ([`deploy/k8s/`](deploy/k8s)) | [`docs/user/install/k8s.md`](docs/user/install/k8s.md) |
| systemd service ([`contrib/systemd/`](contrib/systemd)) | [`docs/user/install/systemd.md`](docs/user/install/systemd.md) |
| Offline / air-gapped bundle ([`deploy/offline/`](deploy/offline)) | [`docs/user/install/offline.md`](docs/user/install/offline.md) |
| Upgrade | [`docs/user/install/upgrade.md`](docs/user/install/upgrade.md) |

## Security notes (read before exposing to a network)

- BinFlow serves **plain HTTP only** — TLS termination belongs to a reverse
  proxy (an nginx TLS template ships in [`deploy/nginx/`](deploy/nginx));
  compose binds to `127.0.0.1` by default.
- **Anonymous read is ON by default**: content-path `GET`/`HEAD` needs no
  credentials; writes (`PUT`/`DELETE`) and all of `/binflow/api/**` always
  require authentication. Turn it off with
  `BINFLOW_SECURITY_ANONYMOUS_ACCESS=false` (env) or
  `security.anonymous_access: false` (binflow.yaml).
- Secrets go through env only (never YAML). Issue per-CI API tokens
  (`POST /binflow/api/security/token`), scope users to path patterns
  (`/binflow/api/v1/permissions`).

## Configuration

One YAML file (`binflow.yaml`) plus `BINFLOW_`-prefixed env overrides
(`__` descends a level, e.g. `BINFLOW_SERVER__LISTEN=:9090`); env wins over
YAML, and booting with no config file yields documented defaults. Full field
list: `docs/design/architecture.md` §8. Two planes live outside this file:
`binstore.yaml` (same directory — the ordered storage provider chain) and
the DB-backed runtime auth plane (editable in the console or over REST).

```yaml
server:
  listen: ":8080"
storage:
  data_dir: "./data"        # blobs + uploads + sqlite all live here
  backend: "local"          # "s3" → object storage (see the S3 guide)
security:
  anonymous_access: true    # see Security notes
logging:
  level: "info"             # debug | info | warn | error
  format: "json"
```

## Operations

```bash
./bin/binflow-server gc                 # dry-run: list unreferenced blobs (default)
./bin/binflow-server gc --apply         # actually reclaim them (24h grace by default)
./bin/binflow-server export <dir>       # online backup (server keeps running)
./bin/binflow-server import <dir>       # restore into an empty data dir (server stopped)
./bin/binflow-server --version
```

GC re-verifies references immediately before every physical delete, so it
runs alongside active CI pushes — handbook:
[`docs/user/admin/backup-restore.md`](docs/user/admin/backup-restore.md).

## Development

Toolchain: Go 1.26 (`go.mod`), golangci-lint pinned in `.tool-versions`.
`make dev` is the pre-push gate (vet + lint + test + build); `make help`
lists all targets. Layout: `cmd/` (`binflow-server`, `bf`, `bf-migrate`)
over the `internal/` packages; CI runs the same Makefile targets.

## Documentation & links

| What | Where |
|---|---|
| Documentation site (install / integrations / admin / API / FAQ) | [binflow.docs.buildwithfern.com](https://binflow.docs.buildwithfern.com) · source [`docs/user/`](docs/user/README.md) |
| API reference (Artifactory-compatible subset + `/api/v1`) | [`docs/user/api-reference.md`](docs/user/api-reference.md) |
| FAQ & troubleshooting, Artifactory→BinFlow mapping | [`docs/user/faq.md`](docs/user/faq.md) |
| Product vision & scope | [`PRODUCT.md`](PRODUCT.md) |
| Architecture spec | [`docs/design/architecture.md`](docs/design/architecture.md) |
| Decision log (ADR) | [`DECISIONS.md`](DECISIONS.md) |

## Status

Pre-GA software under active development. No open-source license has been
declared yet (no `LICENSE` file here); the product ships its own
community / pro / enterprise tier system — see the matrix above.
