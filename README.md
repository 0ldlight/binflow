# BinFlow

BinFlow is a cloud-native artifact repository written from scratch in Go, with
an architecture and concept model aligned with JFrog Artifactory. This
repository is the team workspace; M1 delivers the kernel foundation: a
single static binary, zero external dependencies, serving **Generic (raw
HTTP) local repositories** with authentication, checksum verification and
path-level ACL — driven entirely with `curl`.

| What | Where |
|---|---|
| Product vision & scope | [`PRODUCT.md`](PRODUCT.md) |
| Milestones (M1 kernel → M5 GA) | [`ROADMAP.md`](ROADMAP.md) |
| M1 requirements (PRD v1.3.1) | [`docs/prd/milestone-1.md`](docs/prd/milestone-1.md) |
| Architecture spec | [`docs/design/architecture.md`](docs/design/architecture.md) |
| Reverse-engineered behavior specs | [`docs/reverse/`](docs/reverse/) |
| Decisions (ADR log) | [`DECISIONS.md`](DECISIONS.md) |
| Task board | [`BOARD.md`](BOARD.md) |

Everything below the line is **copy-paste runnable**. All URLs use the single
`/binflow` prefix (Q1): management/compat endpoints under `/binflow/api/...`,
content under `/binflow/<repo>/<path>`.

## Quick start (M1)

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
> (see "Authentication notes" below).

### Path A — bare binary (no Docker)

**1. Build**

```bash
make build
# bin size: ~16 MB (bin/binflow-server)
```

**2. Start**

```bash
./bin/binflow-server serve
# ... binflow starting ... binflow listening addr=:8080
curl -s $BASE/binflow/api/system/ping
# OK
```

Defaults: listens on `:8080`, data in `./data` (created on demand; sqlite +
`blobs/` + `sessions/` live there). Override with a config file or env — see
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

- ping (C01), create repo (C03), upload with checksum reconciliation (C07),
  download with checksum verification (C08), anonymous read (C23) — the PRD
  M1 minimal chain. The full acceptance matrix lives in
  `docs/prd/milestone-1.md` §5.3.
- Uploads are atomic (an interrupted upload never becomes visible), blobs
  are content-addressed by sha256 and deduplicated across every path and
  repo, and deletes are idempotent.

## Security notes (read before exposing to a network)

- **M1 serves plain HTTP only** (NFR-S6). TLS termination belongs to a
  reverse proxy in front of it. The compose file binds to `127.0.0.1` by
  default — keep it that way unless you are on a trusted network.
- **Anonymous read is ON by default** (decision Q2, aligned with the
  Artifactory tradition; NFR-S8): `GET`/`HEAD` on content paths
  (`/binflow/<repo>/<path>`) need no credentials. Writes (`PUT`/`DELETE`)
  and all of `/binflow/api/**` **always** require authentication, whatever
  this switch says. Turn anonymous read off with either:

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
  path patterns (`/binflow/api/v1/permissions`) — the M1 security surface is
  small but real; see `docs/prd/milestone-1.md` FR-5 for the exact commands
  (C20–C22).

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
  data_dir: "./data"        # blobs + sessions + sqlite all live here
security:
  anonymous_access: true    # see Security notes
logging:
  level: "info"             # debug | info | warn | error
  format: "json"
```

## Operations

```bash
./bin/binflow-server gc                 # dry-run: list unreferenced blobs (default)
./bin/binflow-server gc --apply         # actually reclaim them (grace period applies)
./bin/binflow-server --version
```

`gc` only ever deletes blobs that no node references and that are older than
the grace window (24h by default) — interrupted uploads leave nothing
behind, and a deleted artifact's space comes back after grace.

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
| `make build` | compile `bin/binflow-server` with CGO disabled (zero-CGo baseline, ADR-0005); prints the binary size |
| `make test` | `go test -race ./...` |
| `make test-cov` | tests with a coverage profile (`coverage.out`) |
| `make lint` | golangci-lint (installs the pinned version if missing) |
| `make fmt` / `make vet` | gofmt / go vet |
| `make tidy` | sync `go.mod` / `go.sum` |
| `make run` | build, then `serve` on `:8080` with `./data` |
| `make dev` | vet + lint + test + build (pre-push gate) |
| `make tools` | print toolchain versions |
| `make clean` | remove `bin/` and coverage output |

Layout follows `docs/design/architecture.md` §2: `cmd/binflow-server`
(process assembly only) over the `internal/` packages (`config`, `storage`,
`metadata`, `auth`, `audit`, `repo`, `adapter`, `httpapi`, `console`);
business packages never reach into each other's internals. CI
(`.github/workflows/ci.yml`) runs the same Makefile targets — local and CI
share one entrypoint.

## License / status

Pre-GA software under active development (M1). See `ROADMAP.md` for the
milestone plan and `BOARD.md` for what is currently being worked on.
