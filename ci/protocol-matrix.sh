#!/usr/bin/env bash
# shellcheck disable=SC2016  # docker-client prefixes expand at CALL time, by design
# protocol-matrix.sh — BinFlow per-protocol push+pull matrix (T-469, user
# directive #10): after every UAT deploy, exercise EVERY package protocol
# against the live instance with its REAL client toolchain — mvn, gradlew,
# npm, twine/pip, docker, dotnet, go, helm, conan, curl.
#
# Single source of truth for BOTH CI faces (the CircleCI `protocol_matrix`
# job and .github/workflows/protocol-matrix.yml) — the CI configs only
# install toolchains and call this script, so the two surfaces cannot drift.
#
# Fixture source (user directive): a shallow clone of
# https://github.com/jfrog/project-examples — TEST INPUT ONLY (clean-room
# ADR-0001): nothing from that repo is copied into BinFlow source; the
# clone lives in the CI workdir and is thrown away. Where the fixture repo
# has no usable example for a protocol (conan recipes, a dependency-free go
# module, a chart scaffold), the leg synthesizes a minimal fixture and says
# so in its log line.
#
# Environment contract (set by the CI configs; secrets are NEVER hardcoded):
#   BINFLOW_BASE           e.g. https://uat.binflow.org (T-478; the
#                          plain-HTTP form http://IP:8080 still works —
#                          the transition fallback)   (required)
#   BINFLOW_USER           default admin
#   BINFLOW_PASSWORD       from the CI secret               (required)
#   BINFLOW_CLIENT_BASE    URL as CONTAINER-side clients see it when running
#                          with --docker-clients on Docker Desktop (i.e.
#                          http://host.docker.internal:8080); defaults to
#                          BINFLOW_BASE
#   BINFLOW_MATRIX_WORK    workdir (default a fresh mktemp dir)
#   BINFLOW_MATRIX_REPORT  report file (default $WORK/report.log)
#   BINFLOW_MATRIX_STAMP   override the per-run version stamp
#   MATRIX_FIXTURES        pre-cloned project-examples dir (auto-cloned if unset)
#
# Usage:
#   ci/protocol-matrix.sh --leg maven --leg npm ...   # chosen legs
#   ci/protocol-matrix.sh --only maven,npm            # comma list
#   ci/protocol-matrix.sh                             # all ten legs
#   ci/protocol-matrix.sh --list                      # leg names
#   ci/protocol-matrix.sh --report                    # print accumulated report
#   ci/protocol-matrix.sh --docker-clients ...        # missing mvn/java/dotnet
#                          # run via pinned docker images (local dry-run mode;
#                          # CI installs real toolchains instead)
#   ci/protocol-matrix.sh --strict-tools ...          # missing tool = FAIL
#                          # (CI posture) instead of SKIP
#
# Repositories: each leg owns uat-matrix-<proto>-local, created by an
# idempotent upsert (PUT /binflow/api/repositories/<key> answers 200 for
# both create and identical replace) and KEPT after the run — the next
# deploy retests against the same repos with a fresh timestamp-stamped
# version, so history accretes and no overwrite is ever attempted (BinFlow
# rejects same-version republish on npm/pypi/nuget by design).
#
# T-479 remote/virtual segments: every leg additionally exercises the
# remote (pull-through proxy) and virtual (aggregation) repository classes
# after its local segment:
#   <leg>/remote   a remote repo pointing at the protocol's REAL public
#                  upstream (pinned versions, never latest — table below)
#                  pulls a small real artifact and asserts MISS->HIT via
#                  the X-Binflow-Cache header;
#   <leg>/virtual  a virtual repo aggregating [local, remote] asserts
#                  local-member resolution AND remote-member resolution
#                  through the single virtual URL (X-Binflow-Resolved-From).
# Segment results are extra report lines (<leg>/remote, <leg>/virtual)
# alongside the leg summary — a failing segment fails the leg, but the
# local segment's own PASS line is already recorded (never masked).
# npm specifics (product boundary, docs/user/integrations/npm.md):
# registry.npmjs.org packuments are NOT layout-compatible, so the npm
# remote uses a layout-compatible upstream synthesized on the instance (a
# generic repo holding packument+tarball at the npm STORAGE layout) for
# the full client chain, plus an npmjs boundary probe (tarball pull-through
# works byte-identically; packument 404 is the documented M4 backlog).
# Offline posture (strict-tools semantics): each remote segment probes its
# public upstream first; unreachable → FAIL under --strict-tools (CI), SKIP
# with an explicit report line locally (the local + virtual-local-member
# assertions still run).
#
# Upstream pin table (all live-verified 2026-09-05 against the UAT):
#   generic/maven/gradle  https://repo.maven.apache.org/maven2   junit 4.13.2
#   npm                   self-synthesized + registry.npmjs.org  isarray 2.0.5
#   pypi                  https://pypi.org                       six 1.17.0
#   docker                https://registry-1.docker.io/v2        hello-world:linux
#   nuget                 https://api.nuget.org (NO /v3/index.json — the feed
#                         is derived at <url>/v3/index.json)      Newtonsoft.Json 13.0.2
#   go                    https://proxy.golang.org               golang.org/x/mod@v0.17.0
#   helm                  https://kubernetes.github.io/ingress-nginx  ingress-nginx 4.12.7
#   conan                 https://center2.conan.io               hello/1.0
#
# Exit code: 0 = every requested leg passed or skipped, 1 = at least one
# leg failed — legs run independently, a failure never masks the rest.

set -uo pipefail

LEGS_ALL="generic maven gradle npm pypi docker nuget go helm conan"
FIXTURES_URL="https://github.com/jfrog/project-examples"

# T-479 remote/virtual upstream pins — every artifact version is PINNED
# (never latest) so a re-run can never silently test a different upstream
# release; each was live-verified against the standing UAT (2026-09-05).
# Sources: docs/user/admin/remote-virtual.md upstream compatibility table
# + docs/user/integrations/<proto>.md remote/virtual sections.
UP_M2="https://repo.maven.apache.org/maven2"   # generic/maven/gradle
UP_NPMJS="https://registry.npmjs.org"          # npm boundary probe only
UP_PYPI="https://pypi.org"
UP_DOCKER="https://registry-1.docker.io/v2"    # url form: distribution root incl. /v2
UP_NUGET="https://api.nuget.org"               # feed derived at <url>/v3/index.json
UP_GO="https://proxy.golang.org"
UP_HELM="https://kubernetes.github.io/ingress-nginx"
UP_CONAN="https://center2.conan.io"
PIN_JUNIT="junit/junit/4.13.2/junit-4.13.2.pom"     # central, immutable release
PIN_JUNIT_GAV="junit:junit:4.13.2"
PIN_ISARRAY_TGZ="isarray/-/isarray-2.0.5.tgz"       # npmjs tarball path (layout-compatible)
PIN_SIX="1.17.0"                                     # six wheel ~11KB
PIN_NUGET_PKG="newtonsoft.json"                      # doc-pinned (nuget.md step 5)
PIN_NUGET_VER="13.0.2"
PIN_GOMOD="golang.org/x/mod@v0.17.0"                 # doc-pinned (golang.md step 5)
PIN_HELM_CHART="ingress-nginx"
PIN_HELM_VER="4.12.7"                                # live-verified in the upstream index
PIN_DOCKER_REF="library/hello-world"                 # ~10KB image; pulled BY DIGEST
PIN_DOCKER_DIGEST="sha256:5e22040d441e5fb3aed38368acbe8486b575d7018df38dbdfbc7311fbb2ef3a9"
PIN_CONAN_REF="hello/1.0"                            # doc form (conan.md remote section)

REQUESTED=""
MODE_LIST=0
MODE_REPORT=0
DOCKER_CLIENTS=0
STRICT_TOOLS=0

while [ $# -gt 0 ]; do
  case "$1" in
    --leg)        REQUESTED="$REQUESTED $2"; shift 2 ;;
    --only)       REQUESTED="$REQUESTED ${2//,/ }"; shift 2 ;;
    --list)       MODE_LIST=1; shift ;;
    --report)     MODE_REPORT=1; shift ;;
    --docker-clients) DOCKER_CLIENTS=1; shift ;;
    --strict-tools)   STRICT_TOOLS=1; shift ;;
    -h|--help)    sed -n '2,90p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown flag: $1 (see --help)" >&2; exit 2 ;;
  esac
done

if [ "$MODE_LIST" = 1 ]; then echo "$LEGS_ALL" | tr ' ' '\n'; exit 0; fi

# ---------------------------------------------------------------------------
# Environment
# ---------------------------------------------------------------------------
BINFLOW_BASE="${BINFLOW_BASE:-}"
BINFLOW_USER="${BINFLOW_USER:-admin}"
BINFLOW_PASSWORD="${BINFLOW_PASSWORD:-}"
CBASE="${BINFLOW_CLIENT_BASE:-$BINFLOW_BASE}"   # URL container clients see

WORK="${BINFLOW_MATRIX_WORK:-$(mktemp -d -t binflow-matrix)}"
REPORT="${BINFLOW_MATRIX_REPORT:-$WORK/report.log}"
mkdir -p "$WORK"; touch "$REPORT"

if [ -z "$BINFLOW_BASE" ] || [ -z "$BINFLOW_PASSWORD" ]; then
  echo "FATAL: BINFLOW_BASE and BINFLOW_PASSWORD must be set" \
       "(CI: UAT_MATRIX_BASE / UAT_MATRIX_PASSWORD secrets)" >&2
  exit 2
fi
BINFLOW_BASE="${BINFLOW_BASE%/}"; CBASE="${CBASE%/}"
AUTH="$BINFLOW_USER:$BINFLOW_PASSWORD"
# Portable scheme strip (BSD sed has no \? — parameter expansion instead).
strip_scheme() { local u="$1"; u="${u#http://}"; u="${u#https://}"; printf '%s' "$u"; }
# docker client target: scheme-stripped host:port of the instance
REG="${BINFLOW_REG:-$(strip_scheme "$BINFLOW_BASE")}"
NPM_REG_PATH="/binflow/api/npm"

# Per-run version stamp: unique per invocation, semver-safe everywhere
# (maven/npm/nuget/pypi/chart/conan take 1.0.<digits>; go wants the v).
RUN_STAMP="${BINFLOW_MATRIX_STAMP:-$(date -u +%Y%m%d%H%M%S)}"
VER="1.0.${RUN_STAMP}"
GVER="v${VER}"

FIXTURES="${MATRIX_FIXTURES:-$WORK/project-examples}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { printf '[matrix] %s\n' "$*"; }
ok()   { printf '[matrix] PASS %s\n' "$*"; }
fail() { printf '[matrix] FAIL %s\n' "$*" >&2; }

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

# Portable in-place sed (BSD vs GNU -i flag split) via a temp file.
sed_file() { # sed_file <file> <expr>...
  local f="$1"; shift
  sed "$@" "$f" > "$f.mtmp" && mv "$f.mtmp" "$f"
}

# Client runner factory. Host tool first; when --docker-clients is given and
# the tool is missing, fall back to a pinned image mounting the CURRENT
# directory at the SAME absolute path (host env vars stay valid in-container;
# Docker Desktop requirement). Prefixes are defined single-quoted so $PWD
# expands at CALL time, inside the leg's cwd.
setup_client() { # setup_client <fn> <host-tool> <host-prefix> <docker-prefix>
  local fn="$1" tool="$2" hostp="$3" dockerp="$4"
  if command -v "$tool" >/dev/null 2>&1; then
    eval "$fn() { $hostp \"\$@\"; }"
    return 0
  fi
  if [ "$DOCKER_CLIENTS" = 1 ] && command -v docker >/dev/null 2>&1; then
    log "client '$tool' not on host — docker fallback: $dockerp ..."
    eval "$fn() { $dockerp \"\$@\"; }"
    DOCKERIZED_TOOLS="$DOCKERIZED_TOOLS $tool"
    return 0
  fi
  return 1
}

DOCKERIZED_TOOLS=" "
# Base URL a given CLIENT should target: containerized clients (docker
# fallback) see the host as host.docker.internal; host-native clients talk
# to BINFLOW_BASE directly — never routing through the Desktop forwarding
# (observed flaky under container churn during T-469 dry-runs).
client_base() { # client_base <tool>
  case "$DOCKERIZED_TOOLS" in
    *" $1 "*) printf '%s' "$CBASE" ;;
    *)        printf '%s' "$BINFLOW_BASE" ;;
  esac
}

# Missing-tool posture: SKIP by default (local dry-run), FAIL under
# --strict-tools (CI — the matrix must be complete there).
tool_unavailable() { # tool_unavailable <tool>  → returns 1 (fail) or 2 (skip)
  if [ "$STRICT_TOOLS" = 1 ]; then
    echo "tool '$1' not found on this host (strict posture: FAIL)"
    return 1
  fi
  echo "skip: tool '$1' not installed on this host"
  return 2
}

# License gate for the pro-tier package types (go/nuget/helm/helmoci/conan,
# license D3): on a community instance (e.g. a local dry-run sandbox) the
# legs SKIP with the tier named; under --strict-tools (CI against UAT,
# enterprise-licensed) a community tier is a loud failure, not a skip.
require_tier() { # require_tier <min-tier>  → returns 0 / 1 (fail) / 2 (skip)
  case "$tier" in
    enterprise) return 0 ;;
    pro)        [ "$1" = enterprise ] || return 0; ;;
  esac
  if [ "$STRICT_TOOLS" = 1 ]; then
    echo "license tier '$tier' < '$1' — gated package type cannot be tested (strict posture: FAIL)"
    return 1
  fi
  echo "skip: license tier '${tier:-unknown}' < '$1' — the gated legs need the licensed UAT"
  return 2
}

ensure_repo() { # ensure_repo <key> <packageType>
  repo_upsert "$1" "{\"rclass\":\"local\",\"packageType\":\"$2\",\"description\":\"T-469 CI protocol matrix (kept across runs; versions stamp per run)\"}"
}

# ---------------------------------------------------------------------------
# T-479 remote/virtual segment helpers
# ---------------------------------------------------------------------------
repo_upsert() { # repo_upsert <key> <json-body>
  local key="$1" body code
  body="$WORK/ensure-$key.json"
  code=$(curl -su "$AUTH" -X PUT "$BINFLOW_BASE/binflow/api/repositories/$key" \
    -H 'Content-Type: application/json' -d "$2" \
    -o "$body" -w '%{http_code}')
  case "$code" in
    2*) log "repo $key ready (HTTP $code)"; return 0 ;;
    *)  echo "repo upsert $key -> HTTP $code:"; cat "$body"; echo; return 1 ;;
  esac
}

ensure_remote() { # ensure_remote <key> <packageType> <upstream-url>
  repo_upsert "$1" "{\"rclass\":\"remote\",\"packageType\":\"$2\",\"url\":\"$3\",\"description\":\"T-479 CI protocol matrix remote (pull-through proxy; kept across runs)\"}"
}

ensure_virtual() { # ensure_virtual <key> <packageType> <members-csv> [deploy-repo]
  local key="$1" ptype="$2" members="$3" deploy="${4:-}" json
  # csv -> "a","b" (one quoted member per line, comma-joined)
  json="$(printf '%s\n' "$members" | tr ',' '\n' | sed 's/^/"/; s/$/"/' | paste -sd, -)"
  local body="{\"rclass\":\"virtual\",\"packageType\":\"$ptype\",\"repositories\":[$json]"
  [ -n "$deploy" ] && body="$body,\"defaultDeploymentRepo\":\"$deploy\""
  body="$body,\"description\":\"T-479 CI protocol matrix virtual (local+remote aggregation)\"}"
  repo_upsert "$key" "$body"
}

# Header probes: GET a URL (host curl — the runner talks to the instance
# directly), stash headers, extract the T-479 observability trio.
hdr_get() { # hdr_get <hdr-outfile> <url> [extra curl args...]
  local out="$1"; shift
  curl -sS -u "$AUTH" -D "$out" -o /dev/null "$@" || return 1
}
cache_state() { tr -d '\r' < "$1" | awk 'tolower($1)=="x-binflow-cache:"{print $2}'; }
resolved_from() { tr -d '\r' < "$1" | awk 'tolower($1)=="x-binflow-resolved-from:"{print $2}'; }

# cached_twice asserts the pull-through cache did its job across two GETs.
# The cold path is MISS -> HIT. Repos are KEPT across runs, so a pinned
# upstream artifact may already be cached: HIT -> HIT (fresh TTL) or
# REVALIDATED -> HIT (TTL lapsed, upstream unchanged) are equally correct.
# MISS -> MISS never caches and always fails; STALE means the upstream
# errored mid-assert — let the caller's retry re-run the whole probe.
cached_twice() { # cached_twice <url> [extra curl args...]
  local url="$1"; shift
  local h1="$WORK/hdr1.$$" h2="$WORK/hdr2.$$" s1 s2 rc=0
  hdr_get "$h1" "$url" "$@" || return 1
  hdr_get "$h2" "$url" "$@" || return 1
  s1="$(cache_state "$h1")"; s2="$(cache_state "$h2")"
  rm -f "$h1" "$h2"
  echo "cache $s1 -> $s2"
  case "$s1 $s2" in
    "MISS HIT"|"HIT HIT"|"HIT REVALIDATED"|"REVALIDATED HIT"|"REVALIDATED REVALIDATED") return 0 ;;
    *) echo "unexpected cache states: '$s1' -> '$s2' (expected MISS->HIT or warm HIT)"; return 1 ;;
  esac
}

retry_run() { # retry_run <tries> <cmd...> — public-upstream flake damper
  local n="$1"; shift
  local i=0 rc=1
  while [ "$i" -le "$n" ]; do
    if "$@"; then return 0; fi
    i=$((i + 1)); [ "$i" -le "$n" ] && { log "retry $i/$n after transient failure: $*"; sleep 3; }
  done
  return "$rc"
}

# Per-leg upstream guard: probe the public upstream from the RUNNER (the
# server needs its own egress too, but a runner-side outage is the CI
# offline case). Reachable -> NET_OK=1. Unreachable -> under
# --strict-tools the leg FAILs loudly; locally both segments SKIP with
# explicit report lines and the local assertions still count.
NET_OK=0
guard_upstream() { # guard_upstream <leg> <upstream-url>
  local leg="$1" url="$2" code i
  for i in 1 2; do
    code="$(curl -s -o /dev/null -m 8 -w '%{http_code}' "$url" 2>/dev/null)" || code=000
    if [ "$code" != "000" ] && [ -n "$code" ]; then NET_OK=1; return 0; fi
    [ "$i" = 2 ] || sleep 3
  done
  if [ "$STRICT_TOOLS" = 1 ]; then
    echo "upstream $url unreachable from this host (probe HTTP $code) — strict posture: FAIL ($leg/remote, $leg/virtual)"
    return 1
  fi
  record "$leg/remote"  SKIP "upstream $url unreachable (offline dry-run; local posture)"
  record "$leg/virtual" SKIP "upstream $url unreachable"
  return 0
}

# host_is_public <host>: 0 public / 1 private-loopback / 2 unresolvable.
# The npm leg's self-synthesized upstream URL must be SERVER-reachable and
# pass the SSRF guard — against a public UAT it does; against a local
# dry-run instance it cannot (loopback is ssrf-guarded by design).
host_is_public() {
  python3 - "$1" <<'PYEOF'
import socket, sys, ipaddress
try:
    infos = socket.getaddrinfo(sys.argv[1], None)
except OSError:
    sys.exit(2)
for i in infos:
    ip = ipaddress.ip_address(i[4][0])
    if ip.is_private or ip.is_loopback or ip.is_link_local or ip.is_unspecified:
        sys.exit(1)
sys.exit(0)
PYEOF
}

# mvn_settings writes a settings.xml carrying the mvn 3.8+ plain-HTTP
# unblock mirror for ONE repo id (exact-id mirrorOf beats the default
# external:http:* blocker — same trick as the local maven leg).
mvn_settings() { # mvn_settings <outfile> <repo-id> <repo-url>
  cat > "$1" <<EOF
<settings>
  <servers><server>
    <id>binflow</id><username>$BINFLOW_USER</username><password>$BINFLOW_PASSWORD</password>
  </server></servers>
  <mirrors><mirror>
    <id>unblock-$2</id>
    <mirrorOf>$2</mirrorOf>
    <url>$3</url>
    <blocked>false</blocked>
  </mirror></mirrors>
</settings>
EOF
}

# nuget_config writes a consumer nuget.config pointing its single source at
# the given repository's v3 service index (same shape as the local leg's).
nuget_config() { # nuget_config <outfile> <repo-key>
  cat > "$1" <<EOF
<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources><clear />
    <add key="binflow" value="$(client_base dotnet)/binflow/api/nuget/v3/$2/index.json" />
  </packageSources>
  <packageSourceCredentials><binflow>
    <add key="Username" value="$BINFLOW_USER" />
    <add key="ClearTextPassword" value="$BINFLOW_PASSWORD" />
  </binflow></packageSourceCredentials>
</configuration>
EOF
}

fetch() { curl -sSfL -u "$AUTH" -o "$2" "$1"; }

ensure_fixtures() {
  if [ -f "$FIXTURES/README" ]; then log "fixtures present: $FIXTURES"; return 0; fi
  log "shallow-cloning $FIXTURES_URL (clean-room: test input only)"
  git clone --depth 1 "$FIXTURES_URL" "$FIXTURES"
}

# Shared client runners (dormant until a leg calls setup_client for them).
run_mvn()     { mvn "$@"; }
run_gradlew() { ./gradlew "$@"; }
run_dotnet()  { dotnet "$@"; }

# ---------------------------------------------------------------------------
# Legs — each in its own subshell with set -e (see run_leg).
# ---------------------------------------------------------------------------
leg_generic() {
  ensure_repo uat-matrix-generic-local generic
  WORKDIR="$WORK/generic"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
  cp "$FIXTURES/README" payload.txt
  local sum path code hdr
  sum="$(sha256_of payload.txt)"
  path="matrix/$RUN_STAMP/README"
  code=$(curl -su "$AUTH" -X PUT -T payload.txt -H "X-Checksum-Sha256: $sum" \
    -o put.json -w '%{http_code}' \
    "$BINFLOW_BASE/binflow/uat-matrix-generic-local/$path")
  [ "$code" = 201 ] || { echo "generic PUT -> $code"; cat put.json; return 1; }
  fetch "$BINFLOW_BASE/binflow/uat-matrix-generic-local/$path" dl.txt
  cmp payload.txt dl.txt || { echo "downloaded bytes differ"; return 1; }
  hdr="$(curl -sI -u "$AUTH" "$BINFLOW_BASE/binflow/uat-matrix-generic-local/$path" \
    | tr -d '\r' | awk 'tolower($1)=="x-checksum-sha256:"{print $2}')"
  [ "$hdr" = "$sum" ] || { echo "sha256 header mismatch: $hdr != $sum"; return 1; }
  ok "generic: PUT+GET roundtrip byte-identical, sha256 reconciled ($path)"
  # ---- T-479 remote segment: Maven Central as a plain file tree ----
  guard_upstream generic "$UP_M2" || return 1
  if [ "$NET_OK" = 1 ]; then
    ensure_remote uat-matrix-generic-remote generic "$UP_M2" || return 1
    local rurl="$BINFLOW_BASE/binflow/uat-matrix-generic-remote/$PIN_JUNIT" st
    st="$(retry_run 2 cached_twice "$rurl")" \
      || { echo "generic remote cache probe failed: $st"; return 1; }
    fetch "$rurl" remote1.pom || return 1
    grep -q "<project" remote1.pom \
      || { echo "proxied pom does not parse as maven XML head"; return 1; }
    record generic/remote PASS "pull-through $PIN_JUNIT via $UP_M2 ($st)"
  fi
  # ---- T-479 virtual segment: local+remote aggregation ----
  if [ "$NET_OK" = 1 ]; then
    ensure_virtual uat-matrix-generic-virtual generic \
      "uat-matrix-generic-local,uat-matrix-generic-remote" || return 1
    local vurl="$BINFLOW_BASE/binflow/uat-matrix-generic-virtual" h="$WORK/hdr-gv.$$"
    # local member through the virtual (Resolved-From must name the local repo)
    hdr_get "$h" "$vurl/$path" || return 1
    [ "$(resolved_from "$h")" = "uat-matrix-generic-local" ] \
      || { echo "virtual local-member resolution: $(resolved_from "$h")"; return 1; }
    fetch "$vurl/$path" virt-local.txt || return 1
    cmp payload.txt virt-local.txt \
      || { echo "virtual-served local bytes differ"; return 1; }
    # remote member through the virtual (upstream pom resolves via remote)
    hdr_get "$h" "$vurl/$PIN_JUNIT" || return 1
    [ "$(resolved_from "$h")" = "uat-matrix-generic-remote" ] \
      || { echo "virtual remote-member resolution: $(resolved_from "$h")"; return 1; }
    rm -f "$h"
    # C5: the unconfigured-deploy-route write refusal (universal face)
    code=$(curl -su "$AUTH" -X PUT -o c5.json -w '%{http_code}' \
      --data-binary x "$vurl/any/path.txt")
    { [ "$code" = 405 ] && grep -q "No local repository was configured" c5.json; } \
      || { echo "virtual PUT without deploy route -> $code (expected 405 C5)"; return 1; }
    record generic/virtual PASS "local+remote member resolution + C5 write refusal"
  fi
}

leg_maven() {
  ensure_repo uat-matrix-maven-local maven
  WORKDIR="$WORK/maven"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
  setup_client run_mvn mvn mvn \
    'docker run --rm -v "$PWD":"$PWD" -w "$PWD" maven:3.9-eclipse-temurin-17 mvn' \
    || { tool_unavailable mvn; return $?; }
  # Fixture: the jfrog multi-module example; stamp the version so every run
  # publishes fresh coordinates (no overwrite is ever attempted).
  rm -rf proj
  cp -R "$FIXTURES/maven-examples/maven-example" proj
  for p in proj/pom.xml proj/*/pom.xml; do
    sed_file "$p" -e "s|<version>3.7-SNAPSHOT</version>|<version>$VER</version>|g"
  done
  # The fixture pins 2012-era plugin versions (war 2.4, jar 2.4) that cannot
  # load under JDK 17 / mvn 3.9 (plexus API incompatibility) — bump them in
  # the parent's pluginManagement; the deploy protocol is unaffected.
  python3 - proj/pom.xml <<'PYEOF'
import re, sys
p = sys.argv[1]
s = open(p).read()
for aid in ("maven-jar-plugin", "maven-war-plugin"):
    s = re.sub(rf"(<artifactId>{aid}</artifactId>\s*<version>)2\.4(</version>)",
               r"\g<1>3.4.0\g<2>", s)
open(p, "w").write(s)
PYEOF
  cat > settings.xml <<EOF
<settings>
  <servers><server>
    <id>binflow</id><username>$BINFLOW_USER</username><password>$BINFLOW_PASSWORD</password>
  </server></servers>
  <!-- mvn 3.8+ ships a default mirror ("maven-default-http-blocker") that
       blocks every EXTERNAL plain-HTTP repository; the UAT is plain HTTP,
       so re-allow the consumer repo by exact-id mirror with blocked=false
       (exact-id mirrorOf beats the external:http:* blocker). -->
  <mirrors><mirror>
    <id>bf-http-unblock</id>
    <mirrorOf>bf</mirrorOf>
    <url>$(client_base mvn)/binflow/uat-matrix-maven-local</url>
    <blocked>false</blocked>
  </mirror></mirrors>
</settings>
EOF
  MVN_URL="$(client_base mvn)/binflow/uat-matrix-maven-local"
  run_mvn -B -q -s settings.xml -f proj/pom.xml -DskipTests \
    -Dmaven.repo.local="$PWD/m2" \
    -DaltDeploymentRepository="binflow::default::$MVN_URL" \
    deploy || return 1
  log "mvn deploy done (org.jfrog.test:multi*:$VER)"
  # Pull leg: dependency:copy through the consumer pom's <repositories>
  # (the doc-sanctioned form; dependency:get -DremoteRepositories is broken
  # on mvn 3.9 — docs/user/integrations/maven.md).
  rm -rf consumer; mkdir -p consumer
  cat > consumer/pom.xml <<EOF
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.binflow.matrix</groupId><artifactId>consumer</artifactId><version>0.0.1</version>
  <repositories><repository>
    <id>bf</id><url>$(client_base mvn)/binflow/uat-matrix-maven-local</url>
  </repository></repositories>
  <dependencies><dependency>
    <groupId>org.jfrog.test</groupId><artifactId>multi1</artifactId><version>$VER</version>
  </dependency></dependencies>
</project>
EOF
  run_mvn -B -q -s settings.xml -f consumer/pom.xml \
    -Dmaven.repo.local="$PWD/consumer/fresh-repo" \
    dependency:copy -Dartifact="org.jfrog.test:multi1:$VER:jar" -DoutputDirectory="$PWD/consumer/dep" \
    || return 1
  [ -s "consumer/dep/multi1-$VER.jar" ] || { echo "copied jar missing"; return 1; }
  python3 -c "import zipfile,sys; zipfile.ZipFile(sys.argv[1])" "consumer/dep/multi1-$VER.jar" \
    || { echo "pulled artifact is not a valid jar"; return 1; }
  ok "maven: mvn deploy + dependency:copy roundtrip (org.jfrog.test:multi1:$VER)"
  # ---- T-479 remote segment: Maven Central pull-through ----
  guard_upstream maven "$UP_M2" || return 1
  if [ "$NET_OK" = 1 ]; then
    ensure_remote uat-matrix-maven-remote maven "$UP_M2" || return 1
    local mst
    mst="$(retry_run 2 cached_twice "$BINFLOW_BASE/binflow/uat-matrix-maven-remote/$PIN_JUNIT")" \
      || { echo "maven remote cache probe failed: $mst"; return 1; }
    # client-level: dependency:copy resolves the pinned junit through the proxy
    rm -rf consumer-r; mkdir -p consumer-r
    cat > consumer-r/pom.xml <<EOF
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.binflow.matrix</groupId><artifactId>consumer-r</artifactId><version>0.0.1</version>
  <repositories><repository>
    <id>bfr</id><url>$(client_base mvn)/binflow/uat-matrix-maven-remote</url>
  </repository></repositories>
</project>
EOF
    mvn_settings consumer-r/settings.xml bfr "$(client_base mvn)/binflow/uat-matrix-maven-remote"
    run_mvn -B -q -s consumer-r/settings.xml -f consumer-r/pom.xml \
      -Dmaven.repo.local="$PWD/consumer-r/m2" \
      dependency:copy -Dartifact="$PIN_JUNIT_GAV:jar" -DoutputDirectory="$PWD/consumer-r/dep" \
      || return 1
    [ -s consumer-r/dep/junit-4.13.2.jar ] \
      || { echo "junit jar missing via remote"; return 1; }
    record maven/remote PASS "central pull-through + mvn copy $PIN_JUNIT_GAV ($mst)"
  fi
  # ---- T-479 virtual segment: local+remote aggregation ----
  if [ "$NET_OK" = 1 ]; then
    ensure_virtual uat-matrix-maven-virtual maven \
      "uat-matrix-maven-local,uat-matrix-maven-remote" || return 1
    # metadata aggregation (repo-semantics §8.3): versions union must carry
    # the remote member's 4.13.2 in the virtual's generated metadata
    fetch "$BINFLOW_BASE/binflow/uat-matrix-maven-virtual/junit/junit/maven-metadata.xml" vmeta.xml \
      || return 1
    grep -q "<version>4.13.2</version>" vmeta.xml \
      || { echo "virtual maven-metadata.xml misses junit 4.13.2 (§8.3 union)"; return 1; }
    # client-level: BOTH members resolved through the single virtual URL
    rm -rf consumer-v; mkdir -p consumer-v
    cat > consumer-v/pom.xml <<EOF
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.binflow.matrix</groupId><artifactId>consumer-v</artifactId><version>0.0.1</version>
  <repositories><repository>
    <id>bfv</id><url>$(client_base mvn)/binflow/uat-matrix-maven-virtual</url>
  </repository></repositories>
  <dependencies>
    <dependency><groupId>org.jfrog.test</groupId><artifactId>multi1</artifactId><version>$VER</version></dependency>
    <dependency><groupId>junit</groupId><artifactId>junit</artifactId><version>4.13.2</version></dependency>
  </dependencies>
</project>
EOF
    mvn_settings consumer-v/settings.xml bfv "$(client_base mvn)/binflow/uat-matrix-maven-virtual"
    run_mvn -B -q -s consumer-v/settings.xml -f consumer-v/pom.xml \
      -Dmaven.repo.local="$PWD/consumer-v/m2" dependency:resolve || return 1
    [ -f "consumer-v/m2/org/jfrog/test/multi1/$VER/multi1-$VER.jar" ] \
      || { echo "virtual resolve missed local-member multi1"; return 1; }
    [ -f consumer-v/m2/junit/junit/4.13.2/junit-4.13.2.jar ] \
      || { echo "virtual resolve missed remote-member junit"; return 1; }
    record maven/virtual PASS "dependency:resolve both members + §8.3 metadata union"
  fi
}

leg_gradle() {
  ensure_repo uat-matrix-gradle-local maven   # gradle speaks the maven layout
  WORKDIR="$WORK/gradle"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
  # gradlew needs a WORKING JDK on PATH (macOS ships a /usr/bin/java stub
  # that satisfies `command -v` but exits 96 — probe with -version); the
  # wrapper supplies gradle itself. The runner resolves ./gradlew from the
  # CURRENT directory (proj/ and the consumer each carry their own copy).
  if java -version >/dev/null 2>&1; then
    run_gradlew() { ./gradlew "$@"; }
  elif [ "$DOCKER_CLIENTS" = 1 ] && command -v docker >/dev/null 2>&1; then
    log "client 'java' not on host — docker fallback: eclipse-temurin:17-jdk"
    run_gradlew() { docker run --rm -v "$PWD":"$PWD" -w "$PWD" -e GRADLE_USER_HOME="$PWD/.gradle-home" eclipse-temurin:17-jdk ./gradlew "$@"; }
    DOCKERIZED_TOOLS="$DOCKERIZED_TOOLS java "
  else
    tool_unavailable java; return $?
  fi
  rm -rf proj
  cp -R "$FIXTURES/gradle-examples/gradle-example-minimal" proj
  chmod +x proj/gradlew
  # Trim the wrapper download (-all -> -bin), keep their pinned 7.6.2.
  sed_file proj/gradle/wrapper/gradle-wrapper.properties \
    -e 's|gradle-7.6.2-all.zip|gradle-7.6.2-bin.zip|'
  # Driver build file over the fixture's source tree (their build.gradle
  # drives the JFrog Artifactory plugin; we publish with plain
  # maven-publish to BinFlow — the protocol under test is the same wire).
  printf "rootProject.name = 'gradle-matrix'\n" > proj/settings.gradle
  cat > proj/build.gradle <<EOF
plugins { id 'java'; id 'maven-publish' }
group = 'com.binflow.matrix'
version = '$VER'
publishing {
  publications { mavenJava(MavenPublication) { from components.java; artifactId 'gradle-matrix' } }
  repositories { maven {
    url = uri('$(client_base java)/binflow/uat-matrix-gradle-local')
    allowInsecureProtocol = true
    credentials { username = '$BINFLOW_USER'; password = '$BINFLOW_PASSWORD' }
  } }
}
EOF
  ( cd proj && GRADLE_USER_HOME="$PWD/.gradle-home" \
      run_gradlew --no-daemon -q publish ) || return 1
  log "gradle publish done (com.binflow.matrix:gradle-matrix:$VER)"
  # Pull leg: a consumer resolves the artifact through BinFlow.
  rm -rf consumer; mkdir -p consumer
  printf "rootProject.name = 'gradle-consumer'\n" > consumer/settings.gradle
  cat > consumer/build.gradle <<EOF
plugins { id 'java' }
repositories { maven {
  url = uri('$(client_base java)/binflow/uat-matrix-gradle-local')
  allowInsecureProtocol = true
} }
dependencies { implementation 'com.binflow.matrix:gradle-matrix:$VER' }
EOF
  cp proj/gradlew proj/gradlew.bat consumer/ 2>/dev/null; cp -R proj/gradle consumer/
  chmod +x consumer/gradlew
  ( cd consumer && GRADLE_USER_HOME="$PWD/.gradle-home" \
      run_gradlew --no-daemon -q dependencies --configuration compileClasspath > deps.txt ) \
    || { cat consumer/deps.txt 2>/dev/null || true; return 1; }
  grep -q "gradle-matrix:$VER" consumer/deps.txt \
    || { echo "published artifact absent from resolved classpath"; cat consumer/deps.txt; return 1; }
  ok "gradle: publish + compileClasspath resolution via BinFlow (gradle-matrix:$VER)"
  # ---- T-479 remote segment: Maven Central via the gradle/maven face ----
  guard_upstream gradle "$UP_M2" || return 1
  if [ "$NET_OK" = 1 ]; then
    ensure_remote uat-matrix-gradle-remote maven "$UP_M2" || return 1
    local gst
    gst="$(retry_run 2 cached_twice "$BINFLOW_BASE/binflow/uat-matrix-gradle-remote/$PIN_JUNIT")" \
      || { echo "gradle remote cache probe failed: $gst"; return 1; }
    rm -rf consumer-r; mkdir -p consumer-r
    printf "rootProject.name = 'gradle-consumer-r'\n" > consumer-r/settings.gradle
    cat > consumer-r/build.gradle <<EOF
plugins { id 'java' }
repositories { maven {
  url = uri('$(client_base java)/binflow/uat-matrix-gradle-remote')
  allowInsecureProtocol = true
} }
dependencies { implementation 'junit:junit:4.13.2' }
EOF
    cp proj/gradlew proj/gradlew.bat consumer-r/ 2>/dev/null; cp -R proj/gradle consumer-r/
    chmod +x consumer-r/gradlew
    # one shared wrapper/distribution cache for BOTH t-479 consumers (the
    # local segment's proj/consumer keep their own — this only avoids a
    # THIRD and FOURTH ~100MB wrapper download per run)
    ( cd consumer-r && GRADLE_USER_HOME="$WORKDIR/.gradle-home-rv" \
      run_gradlew --no-daemon -q dependencies --configuration compileClasspath > deps.txt ) \
      || { cat consumer-r/deps.txt 2>/dev/null || true; return 1; }
    grep -q "junit:junit:4.13.2" consumer-r/deps.txt \
      || { echo "junit absent from remote-resolved classpath"; cat consumer-r/deps.txt; return 1; }
    grep -q "org.hamcrest:hamcrest-core:1.3" consumer-r/deps.txt \
      || { echo "transitive hamcrest not proxied"; cat consumer-r/deps.txt; return 1; }
    record gradle/remote PASS "gradle resolves junit+hamcrest via central proxy ($gst)"
  fi
  # ---- T-479 virtual segment ----
  if [ "$NET_OK" = 1 ]; then
    ensure_virtual uat-matrix-gradle-virtual maven \
      "uat-matrix-gradle-local,uat-matrix-gradle-remote" || return 1
    rm -rf consumer-v; mkdir -p consumer-v
    printf "rootProject.name = 'gradle-consumer-v'\n" > consumer-v/settings.gradle
    cat > consumer-v/build.gradle <<EOF
plugins { id 'java' }
repositories { maven {
  url = uri('$(client_base java)/binflow/uat-matrix-gradle-virtual')
  allowInsecureProtocol = true
} }
dependencies {
  implementation 'com.binflow.matrix:gradle-matrix:$VER'
  implementation 'junit:junit:4.13.2'
}
EOF
    cp proj/gradlew proj/gradlew.bat consumer-v/ 2>/dev/null; cp -R proj/gradle consumer-v/
    chmod +x consumer-v/gradlew
    ( cd consumer-v && GRADLE_USER_HOME="$WORKDIR/.gradle-home-rv" \
      run_gradlew --no-daemon -q dependencies --configuration compileClasspath > deps.txt ) \
      || { cat consumer-v/deps.txt 2>/dev/null || true; return 1; }
    grep -q "gradle-matrix:$VER" consumer-v/deps.txt \
      || { echo "local-member artifact absent via virtual"; cat consumer-v/deps.txt; return 1; }
    grep -q "junit:junit:4.13.2" consumer-v/deps.txt \
      || { echo "remote-member junit absent via virtual"; cat consumer-v/deps.txt; return 1; }
    record gradle/virtual PASS "compileClasspath resolves local+remote members"
  fi
}

leg_npm() {
  ensure_repo uat-matrix-npm-local npm
  WORKDIR="$WORK/npm"; mkdir -p "$WORKDIR/pkg"; cd "$WORKDIR" || return 1
  setup_client run_npm npm npm \
    'docker run --rm -v "$PWD":"$PWD" -w "$PWD" node:22-bookworm npm' \
    || { tool_unavailable npm; return $?; }
  local AUTH_B64 NPM_REG NPM_AUTH
  NPM_REG="$(client_base npm)$NPM_REG_PATH/uat-matrix-npm-local"
  NPM_AUTH="$(printf '%s' "$NPM_REG" | sed 's|^https\{0,1\}://||')"
  AUTH_B64="$(printf '%s:%s' "$BINFLOW_USER" "$BINFLOW_PASSWORD" | base64 | tr -d '\n')"
  cp "$FIXTURES/npm-example/package.json" "$FIXTURES/npm-example/helloworld.js" pkg/
  # Stamp the version AND drop the fixture's external dependencies (send/
  # debug): the leg tests the push/pull protocol against THIS instance only,
  # and `npm install` resolves the package's deps from the same registry —
  # an empty local repo 404s them. Same posture as the pypi leg's --no-deps.
  python3 - pkg/package.json "$VER" <<'PYEOF'
import json, sys
path, ver = sys.argv[1], sys.argv[2]
d = json.load(open(path))
d["version"] = ver
d.pop("dependencies", None)
d.pop("devDependencies", None)
json.dump(d, open(path, "w"), indent=2)
PYEOF
  # Registry AND credential lines both need the trailing slash (docs/user/
  # integrations/npm.md "trailing-slash pairing" matrix).
  cat > pkg/.npmrc <<EOF
registry=$NPM_REG/
//$NPM_AUTH/:_auth=$AUTH_B64
always-auth=true
EOF
  ( cd pkg && run_npm publish --no-fund --no-audit ) || return 1
  log "npm publish done (npm-example@$VER)"
  mkdir -p consumer; cd consumer || return 1 || return 1
  printf '{"name":"matrix-consumer","version":"0.0.1"}\n' > package.json
  printf 'registry=%s/\n' "$(client_base npm)$NPM_REG_PATH/uat-matrix-npm-local" > .npmrc
  run_npm install "npm-example@$VER" --no-fund --no-audit || return 1
  [ -f node_modules/npm-example/helloworld.js ] \
    || { echo "installed package missing files"; return 1; }
  local got
  got="$(python3 -c "import json;print(json.load(open('node_modules/npm-example/package.json'))['version'])")"
  [ "$got" = "$VER" ] || { echo "installed version $got != $VER"; return 1; }
  ok "npm: publish + install roundtrip (npm-example@$VER)"
  # ---- T-479 remote segment ----
  # npmjs packuments are NOT layout-compatible (docs/user/integrations/npm.md
  # — the M4 backlog), so the remote face uses a layout-compatible upstream
  # synthesized on the instance: a GENERIC repo holding packument+tarball
  # at the npm STORAGE layout (verified live). npmjs itself is probed for
  # what DOES work (tarball paths match) and as a boundary tripwire.
  guard_upstream npm "$UP_NPMJS" || return 1
  NPM_UP_OK=0
  if [ "$NET_OK" = 1 ]; then
    cd "$WORKDIR" || return 1
    local uphost nst got code
    uphost="$(printf '%s' "$BINFLOW_BASE" | sed 's|^https\{0,1\}://||; s|[/:].*$||')"
    if host_is_public "$uphost"; then
      ensure_repo uat-matrix-npm-upstream generic || return 1
      mkdir -p up || return 1
      cat > up/package.json <<EOF
{"name":"matrix-upstream-pkg","version":"$VER","description":"T-479 layout-compatible npm upstream fixture"}
EOF
      printf 'console.log("upstream-ok");\n' > up/index.js
      ( cd up && run_npm pack --silent >/dev/null ) || return 1
      [ -f "up/matrix-upstream-pkg-$VER.tgz" ] \
        || { echo "npm pack produced no tarball"; ls up; return 1; }
      python3 - "up/matrix-upstream-pkg-$VER.tgz" "$VER" > up/packument.json <<'PYEOF'
import base64, hashlib, json, sys
tgz, ver = sys.argv[1], sys.argv[2]
data = open(tgz, "rb").read()
sha1 = hashlib.sha1(data).hexdigest()
sha512 = base64.b64encode(hashlib.sha512(data).digest()).decode()
doc = {
    "name": "matrix-upstream-pkg",
    "versions": {ver: {"name": "matrix-upstream-pkg", "version": ver,
        "dist": {"tarball": "http://upstream.invalid/matrix-upstream-pkg/-/matrix-upstream-pkg-%s.tgz" % ver,
                 "shasum": sha1, "integrity": "sha512-" + sha512}}},
    "dist-tags": {"latest": ver},
    "time": {ver: "2026-01-01T00:00:00Z"},
}
print(json.dumps(doc))
PYEOF
      local UPBASE="$BINFLOW_BASE/binflow/uat-matrix-npm-upstream"
      code=$(curl -su "$AUTH" -T "up/matrix-upstream-pkg-$VER.tgz" -o /dev/null -w '%{http_code}' \
        "$UPBASE/matrix-upstream-pkg/-/matrix-upstream-pkg-$VER.tgz")
      [ "${code:0:1}" = 2 ] || { echo "upstream tarball PUT -> $code"; return 1; }
      code=$(curl -su "$AUTH" -T up/packument.json -o /dev/null -w '%{http_code}' \
        "$UPBASE/matrix-upstream-pkg/packument.json")
      [ "${code:0:1}" = 2 ] || { echo "upstream packument PUT -> $code"; return 1; }
      ensure_remote uat-matrix-npm-remote npm "$UPBASE" || return 1
      nst="$(retry_run 2 cached_twice "$BINFLOW_BASE$NPM_REG_PATH/uat-matrix-npm-remote/matrix-upstream-pkg")" \
        || { echo "npm remote packument cache probe failed: $nst"; return 1; }
      rm -rf consumer-r; mkdir -p consumer-r || return 1
      local RREG RAUTHB
      RREG="$(client_base npm)$NPM_REG_PATH/uat-matrix-npm-remote"
      RAUTHB="$(printf '%s' "$RREG" | sed 's|^https\{0,1\}://||')"
      cat > consumer-r/.npmrc <<EOF
registry=$RREG/
//$RAUTHB/:_auth=$AUTH_B64
always-auth=true
EOF
      printf '{"name":"matrix-consumer-r","version":"0.0.1"}\n' > consumer-r/package.json
      ( cd consumer-r && run_npm install "matrix-upstream-pkg@$VER" --no-fund --no-audit ) \
        || return 1
      [ -f consumer-r/node_modules/matrix-upstream-pkg/index.js ] \
        || { echo "remote-installed package missing files"; return 1; }
      got="$(python3 -c "import json;print(json.load(open('consumer-r/node_modules/matrix-upstream-pkg/package.json'))['version'])")"
      [ "$got" = "$VER" ] || { echo "remote-installed version $got != $VER"; return 1; }
      record npm/remote PASS "layout-compatible upstream full client chain ($nst)"
      NPM_UP_OK=1
    else
      record npm/remote SKIP "self-upstream $uphost is private/loopback (ssrf-guarded by design) — needs the public UAT"
    fi
    # npmjs boundary probe: tarball paths ARE compatible (byte-parity vs
    # upstream); packument 404 is the documented boundary — asserted so the
    # M4 delivery flips this line red and forces an update here.
    cd "$WORKDIR" || return 1
    ensure_remote uat-matrix-npm-npmjs npm "$UP_NPMJS" || return 1
    retry_run 2 cached_twice "$BINFLOW_BASE/binflow/uat-matrix-npm-npmjs/$PIN_ISARRAY_TGZ" >/dev/null \
      || { echo "npmjs tarball pull-through failed"; return 1; }
    fetch "$BINFLOW_BASE/binflow/uat-matrix-npm-npmjs/$PIN_ISARRAY_TGZ" ia-binflow.tgz || return 1
    curl -sSfL -o ia-direct.tgz "$UP_NPMJS/$PIN_ISARRAY_TGZ" || return 1
    cmp ia-binflow.tgz ia-direct.tgz \
      || { echo "npmjs tarball bytes differ from upstream"; return 1; }
    code=$(curl -su "$AUTH" -o ia.json -w '%{http_code}' \
      "$BINFLOW_BASE$NPM_REG_PATH/uat-matrix-npm-npmjs/isarray")
    [ "$code" = 404 ] \
      || { echo "npmjs packument via remote -> $code (documented boundary is 404)"; return 1; }
    record npm/remote-npmjs PASS "tarball pull-through byte-identical + packument 404 boundary tripwire"
  fi
  # ---- T-479 virtual segment ----
  if [ "$NET_OK" = 1 ]; then
    cd "$WORKDIR" || return 1
    ensure_virtual uat-matrix-npm-virtual npm \
      "uat-matrix-npm-local,uat-matrix-npm-remote" || return 1
    local hv="$WORK/hdr-npmv.$$"
    hdr_get "$hv" "$BINFLOW_BASE$NPM_REG_PATH/uat-matrix-npm-virtual/npm-example" || return 1
    [ "$(resolved_from "$hv")" = "uat-matrix-npm-local" ] \
      || { echo "npm-example via virtual resolved from '$(resolved_from "$hv")'"; return 1; }
    if [ "$NPM_UP_OK" = 1 ]; then
      hdr_get "$hv" "$BINFLOW_BASE$NPM_REG_PATH/uat-matrix-npm-virtual/matrix-upstream-pkg" || return 1
      [ "$(resolved_from "$hv")" = "uat-matrix-npm-remote" ] \
        || { echo "matrix-upstream-pkg via virtual resolved from '$(resolved_from "$hv")'"; return 1; }
    fi
    rm -f "$hv"
    rm -rf consumer-v; mkdir -p consumer-v || return 1
    local VREG VAUTHB
    VREG="$(client_base npm)$NPM_REG_PATH/uat-matrix-npm-virtual"
    VAUTHB="$(printf '%s' "$VREG" | sed 's|^https\{0,1\}://||')"
    cat > consumer-v/.npmrc <<EOF
registry=$VREG/
//$VAUTHB/:_auth=$AUTH_B64
always-auth=true
EOF
    printf '{"name":"matrix-consumer-v","version":"0.0.1"}\n' > consumer-v/package.json
    if [ "$NPM_UP_OK" = 1 ]; then
      ( cd consumer-v && run_npm install "npm-example@$VER" "matrix-upstream-pkg@$VER" \
          --no-fund --no-audit ) || return 1
      [ -f consumer-v/node_modules/matrix-upstream-pkg/index.js ] \
        || { echo "remote-member package missing via virtual"; return 1; }
    else
      ( cd consumer-v && run_npm install "npm-example@$VER" --no-fund --no-audit ) || return 1
    fi
    [ -f consumer-v/node_modules/npm-example/helloworld.js ] \
      || { echo "local-member package missing via virtual"; return 1; }
    record npm/virtual PASS "aggregated install local+remote members (§8.5 form)"
  fi
}

leg_pypi() {
  ensure_repo uat-matrix-pypi-local pypi
  WORKDIR="$WORK/pypi"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
  command -v python3 >/dev/null 2>&1 || { tool_unavailable python3; return $?; }
  # One tools venv for build+twine (keeps the host clean; PEP 668-proof).
  python3 -m venv .venv || return 1
  ./.venv/bin/pip install -q build twine || return 1
  rm -rf proj
  cp -R "$FIXTURES/python-example/pip-example" proj
  sed_file proj/setup.py -e "s|version='1.0'|version='$VER'|"
  ( cd proj && ../.venv/bin/python -m build --wheel --sdist --outdir dist ) || return 1
  ls proj/dist/* >/dev/null || { echo "no dist artifacts"; return 1; }
  ( cd proj && ../.venv/bin/python -m twine upload --non-interactive \
      --repository-url "$(client_base python3)/binflow/api/pypi/uat-matrix-pypi-local" \
      -u "$BINFLOW_USER" -p "$BINFLOW_PASSWORD" dist/* ) || return 1
  log "twine upload done (jfrog-python-example==$VER)"
  python3 -m venv venv-cons || return 1
  # Plain-HTTP index: pip >= 25 hard-ignores untrusted hosts (older pips
  # warn) — --trusted-host derived from the ACTUAL index URL so the
  # --docker-clients host-swapped base stays correct.
  local PYPI_INDEX PYPI_HOST
  PYPI_INDEX="$(client_base python3)/binflow/api/pypi/uat-matrix-pypi-local/simple"
  PYPI_HOST="$(printf '%s' "$PYPI_INDEX" | sed 's|^https\{0,1\}://||; s|[/:].*$||')"
  ./venv-cons/bin/pip install -q --no-deps \
    --trusted-host "$PYPI_HOST" \
    --index-url "$PYPI_INDEX" \
    "jfrog-python-example==$VER" || return 1
  ./venv-cons/bin/python -c 'import pythonExample' \
    || { echo "installed package not importable"; return 1; }
  # (not a pipe: grep -q early-exit + pipefail would SIGPIPE pip — file first)
  ./venv-cons/bin/pip show jfrog-python-example > pipshow.txt 2>/dev/null
  grep -q "Version: $VER" pipshow.txt \
    || { echo "installed version mismatch:"; cat pipshow.txt; return 1; }
  ok "pypi: twine upload + pip install roundtrip (jfrog-python-example==$VER)"
  # ---- T-479 remote segment: pypi.org pull-through ----
  guard_upstream pypi "$UP_PYPI" || return 1
  if [ "$NET_OK" = 1 ]; then
    ensure_remote uat-matrix-pypi-remote pypi "$UP_PYPI" || return 1
    # engine-level: simple page rewrites hrefs to BinFlow paths, wheel is
    # served through the proxy and its bytes match the upstream sha256
    local WHREF WURL WSHA WLOCAL WPATH
    curl -sSf -u "$AUTH" -o rsimple.html \
      "$BINFLOW_BASE/binflow/api/pypi/uat-matrix-pypi-remote/simple/six/" || return 1
    WHREF="$(grep -o 'href="[^"]*six-1\.17\.0-py2\.py3-none-any\.whl[^"]*"' rsimple.html | head -1)"
    [ -n "$WHREF" ] || { echo "six $PIN_SIX wheel absent from proxied simple page"; return 1; }
    WURL="$(printf '%s' "$WHREF" | sed 's/^href="//; s/"$//')"
    WSHA="$(printf '%s' "$WURL" | sed 's/.*#sha256=//')"
    WLOCAL="$(printf '%s' "$WURL" | sed 's/#.*//')"
    # resolve the relative href ("../../packages/...") against the index URL
    WPATH="$(python3 - "$BINFLOW_BASE/binflow/api/pypi/uat-matrix-pypi-remote/simple/six/" "$WLOCAL" <<'PYEOF'
import sys
from urllib.parse import urljoin
print(urljoin(sys.argv[1], sys.argv[2]))
PYEOF
)"
    local pst
    pst="$(retry_run 2 cached_twice "$WPATH")" \
      || { echo "six wheel cache probe failed: $pst"; return 1; }
    fetch "$WPATH" six-via-binflow.whl || return 1
    [ "$(sha256_of six-via-binflow.whl)" = "$WSHA" ] \
      || { echo "proxied wheel sha256 != upstream fragment"; return 1; }
    python3 -c "import zipfile; zipfile.ZipFile('six-via-binflow.whl')" \
      || { echo "proxied wheel is not a valid zip"; return 1; }
    # client-level: pip install through the remote index
    python3 -m venv venv-r || return 1
    local RINDEX RHOST
    RINDEX="$(client_base python3)/binflow/api/pypi/uat-matrix-pypi-remote/simple"
    RHOST="$(printf '%s' "$RINDEX" | sed 's|^https\{0,1\}://||; s|[/:].*$||')"
    ./venv-r/bin/pip install -q --no-deps --trusted-host "$RHOST" \
      --index-url "$RINDEX" "six==$PIN_SIX" || return 1
    [ "$(./venv-r/bin/python -c 'import six; print(six.__version__)')" = "$PIN_SIX" ] \
      || { echo "pip-installed six version mismatch"; return 1; }
    record pypi/remote PASS "pypi.org pull-through sha256-reconciled + pip install six==$PIN_SIX ($pst)"
  fi
  # ---- T-479 virtual segment: local+remote 一次装齐 ----
  if [ "$NET_OK" = 1 ]; then
    ensure_virtual uat-matrix-pypi-virtual pypi \
      "uat-matrix-pypi-local,uat-matrix-pypi-remote" || return 1
    python3 -m venv venv-v || return 1
    local VINDEX VHOST
    VINDEX="$(client_base python3)/binflow/api/pypi/uat-matrix-pypi-virtual/simple"
    VHOST="$(printf '%s' "$VINDEX" | sed 's|^https\{0,1\}://||; s|[/:].*$||')"
    ./venv-v/bin/pip install -q --no-deps --trusted-host "$VHOST" \
      --index-url "$VINDEX" "jfrog-python-example==$VER" "six==$PIN_SIX" || return 1
    ./venv-v/bin/python -c 'import pythonExample, six' \
      || { echo "aggregated install not importable"; return 1; }
    ./venv-v/bin/pip show jfrog-python-example six > vshow.txt 2>/dev/null
    grep -q "Version: $VER" vshow.txt || { echo "local member version missing:"; cat vshow.txt; return 1; }
    grep -q "Version: $PIN_SIX" vshow.txt || { echo "remote member version missing:"; cat vshow.txt; return 1; }
    record pypi/virtual PASS "one pip install serves local member + remote member"
  fi
}

leg_docker() {
  ensure_repo uat-matrix-docker-local docker
  WORKDIR="$WORK/docker"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
  # No dockerized fallback: the docker leg needs the host daemon (the CI
  # configs pre-configure insecure-registries for the plain-HTTP UAT).
  command -v docker >/dev/null 2>&1 || { tool_unavailable docker; return $?; }
  cp "$FIXTURES/docker-oci-examples/docker-example/Dockerfile" .
  # The fixture Dockerfile has no CMD (its echo is build-time only) — append
  # a runtime command so `docker run` proves the pulled image executes.
  printf 'CMD ["sh","-c","echo Hello frog!"]\n' >> Dockerfile
  echo "$BINFLOW_PASSWORD" | docker login "$REG" -u "$BINFLOW_USER" --password-stdin \
    || return 1
  local img="$REG/uat-matrix-docker-local/matrix/example:$VER" out
  docker build -q -t "$img" . || return 1
  docker push "$img" || return 1
  log "docker push done ($img)"
  docker rmi "$img" >/dev/null
  docker pull "$img" || return 1
  out="$(docker run --rm "$img")" || return 1
  printf '%s' "$out" | grep -q "Hello frog!" \
    || { echo "container output unexpected: $out"; return 1; }
  docker rmi "$img" >/dev/null 2>&1 || true
  ok "docker: build + push + pull + run roundtrip ($img)"
  # ---- T-479 remote segment: Docker Hub pull-through ----
  # Live evidence 2026-09-05: the anonymous Bearer dance against
  # registry-1.docker.io works (the docs' 待验证 note is closed); the ref
  # is pulled BY DIGEST — the immutable pin (the :linux tag can be rebuilt
  # upstream; the digest cannot drift).
  guard_upstream docker "$UP_DOCKER" || return 1
  if [ "$NET_OK" = 1 ]; then
    ensure_remote uat-matrix-docker-remote docker "$UP_DOCKER" || return 1
    local dst hdg dgst
    hdg="$WORK/hdr-dock.$$"
    dst="$(retry_run 2 cached_twice \
      "$BINFLOW_BASE/v2/uat-matrix-docker-remote/$PIN_DOCKER_REF/manifests/$PIN_DOCKER_DIGEST" \
      -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json')" \
      || { echo "docker remote manifest cache probe failed: $dst"; return 1; }
    hdr_get "$hdg" "$BINFLOW_BASE/v2/uat-matrix-docker-remote/$PIN_DOCKER_REF/manifests/$PIN_DOCKER_DIGEST" \
      -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json' || return 1
    dgst="$(tr -d '\r' < "$hdg" | awk 'tolower($1)=="docker-content-digest:"{print $2}')"
    rm -f "$hdg"
    [ "$dgst" = "$PIN_DOCKER_DIGEST" ] \
      || { echo "hub manifest digest $dgst != pinned $PIN_DOCKER_DIGEST (re-pin after upstream rebuild)"; return 1; }
    docker pull "$REG/uat-matrix-docker-remote/$PIN_DOCKER_REF@$PIN_DOCKER_DIGEST" || return 1
    out="$(docker run --rm "$REG/uat-matrix-docker-remote/$PIN_DOCKER_REF@$PIN_DOCKER_DIGEST")" || return 1
    printf '%s' "$out" | grep -q "Hello from Docker!" \
      || { echo "container output unexpected: $out"; return 1; }
    record docker/remote PASS "hub pull-through by digest + run ($dst)"
  fi
  # ---- T-479 virtual segment ----
  if [ "$NET_OK" = 1 ]; then
    ensure_virtual uat-matrix-docker-virtual docker \
      "uat-matrix-docker-local,uat-matrix-docker-remote" || return 1
    local hv="$WORK/hdr-dockv.$$"
    hdr_get "$hv" "$BINFLOW_BASE/v2/uat-matrix-docker-virtual/$PIN_DOCKER_REF/manifests/$PIN_DOCKER_DIGEST" \
      -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json' || return 1
    [ "$(resolved_from "$hv")" = "uat-matrix-docker-remote" ] \
      || { echo "hub image via virtual resolved from '$(resolved_from "$hv")'"; return 1; }
    hdr_get "$hv" "$BINFLOW_BASE/v2/uat-matrix-docker-virtual/matrix/example/manifests/$VER" \
      -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json' || return 1
    [ "$(resolved_from "$hv")" = "uat-matrix-docker-local" ] \
      || { echo "local image via virtual resolved from '$(resolved_from "$hv")'"; return 1; }
    rm -f "$hv"
    docker pull "$REG/uat-matrix-docker-virtual/matrix/example:$VER" || return 1
    docker rmi "$REG/uat-matrix-docker-virtual/matrix/example:$VER" >/dev/null 2>&1 || true
    record docker/virtual PASS "manifest resolution from both members + pull"
  fi
}

leg_nuget() {
  require_tier pro || return $?
  ensure_repo uat-matrix-nuget-local nuget
  WORKDIR="$WORK/nuget"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
  # Pin the SDK from the project side: PATH fights over which dotnet host
  # resolves (the GH runner's /usr/share/dotnet ships SDK 10 whose restore
  # dies as a silent MSB4181); global.json binds whichever host to 8.0.x —
  # the docs/user/integrations/nuget.md anchor (setup-dotnet preinstalls it).
  cat > global.json <<'EOF'
{ "sdk": { "version": "8.0.*", "rollForward": "latestFeature" } }
EOF
  # NuGet patch segments are Int32 — the 14-digit global stamp overflows
  # ('1.0.20260904142122' is not a valid version string). Epoch seconds
  # fit until 2038 and stay sortable; every other leg keeps $VER. The
  # assembly/file versions pin 1.0.0.0 separately: AssemblyVersion parts
  # are UInt16, so any time-derived build number overflows (CS7034).
  local NVER
  NVER="1.0.$(date +%s)"
  setup_client run_dotnet dotnet dotnet \
    'docker run --rm -v "$PWD":"$PWD" -w "$PWD" mcr.microsoft.com/dotnet/sdk:8.0 dotnet' \
    || { tool_unavailable dotnet; return $?; }
  # Fixture is an SDK-style csproj (net7.0 + a nuget.org package ref).
  # Pin to net8.0, drop the external ref, stamp the version: the push/pull
  # chain under test then needs zero external feeds (<clear/> closes
  # nuget.org, so restore provably goes through BinFlow).
  rm -rf proj
  cp -R "$FIXTURES/dotnet-examples/single-example" proj
  sed_file proj/single-example.csproj \
    -e "s|<TargetFramework>net7.0</TargetFramework>|<TargetFramework>net8.0</TargetFramework>|g" \
    -e "/PackageReference Include=\"snappier\"/d" \
    -e "s|<ImplicitUsings>enable</ImplicitUsings>|<ImplicitUsings>enable</ImplicitUsings><Version>$NVER</Version><AssemblyVersion>1.0.0.0</AssemblyVersion><FileVersion>1.0.0.0</FileVersion>|"
  cat > proj/nuget.config <<EOF
<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources><clear />
    <add key="binflow" value="$(client_base dotnet)/binflow/api/nuget/v3/uat-matrix-nuget-local/index.json" />
  </packageSources>
  <packageSourceCredentials><binflow>
    <add key="Username" value="$BINFLOW_USER" />
    <add key="ClearTextPassword" value="$BINFLOW_PASSWORD" />
  </binflow></packageSourceCredentials>
</configuration>
EOF
  run_dotnet pack proj/single-example.csproj -c Release -o pkg || return 1
  local nupkg="pkg/single-example.$NVER.nupkg"
  [ -f "$nupkg" ] || { echo "packed nupkg missing (looked for $nupkg)"; ls pkg; return 1; }
  # `--source binflow` is a NAMED source — it only resolves from a
  # directory whose nuget.config chain defines it (proj/), not from the
  # workdir root ("The specified source 'binflow' is invalid").
  cp "$nupkg" proj/
  ( cd proj && run_dotnet nuget push "single-example.$NVER.nupkg" --source binflow ) || return 1
  log "dotnet nuget push done (single-example $NVER)"
  # Pull leg: hand-written consumer (no `dotnet new` template dependency).
  rm -rf consumer; mkdir -p consumer; cp proj/nuget.config consumer/
  cat > consumer/consumer.csproj <<EOF
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings><Nullable>disable</Nullable>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="single-example" Version="$NVER" />
  </ItemGroup>
</Project>
EOF
  printf 'Console.WriteLine("consumer-ok");\n' > consumer/Program.cs
  local out
  out="$(run_dotnet run --project consumer)" || return 1
  printf '%s' "$out" | grep -q "consumer-ok" || { echo "consumer output: $out"; return 1; }
  ok "nuget: pack + push + restore/run roundtrip (single-example $NVER)"
  # ---- T-479 remote segment: nuget.org pull-through ----
  # NOTE the url form: the feed is DERIVED at <url>/v3/index.json — the
  # repository url must be the bare host, NOT the index URL (live-probed:
  # the index-url form 400s on nuget.org).
  guard_upstream nuget "$UP_NUGET" || return 1
  if [ "$NET_OK" = 1 ]; then
    ensure_remote uat-matrix-nuget-remote nuget "$UP_NUGET" || return 1
    local RNUP="flatcontainer/$PIN_NUGET_PKG/$PIN_NUGET_VER/$PIN_NUGET_PKG.$PIN_NUGET_VER.nupkg" nst
    nst="$(retry_run 2 cached_twice "$BINFLOW_BASE/binflow/api/nuget/v3/uat-matrix-nuget-remote/$RNUP")" \
      || { echo "nuget remote nupkg cache probe failed: $nst"; return 1; }
    fetch "$BINFLOW_BASE/binflow/api/nuget/v3/uat-matrix-nuget-remote/$RNUP" remote.nupkg || return 1
    python3 -c "import zipfile,sys; zipfile.ZipFile(sys.argv[1])" remote.nupkg \
      || { echo "proxied nupkg is not a valid zip"; return 1; }
    # client-level: consumer restores the pinned public package via remote
    rm -rf consumer-r; mkdir -p consumer-r || return 1
    nuget_config consumer-r/nuget.config uat-matrix-nuget-remote
    cat > consumer-r/consumer.csproj <<EOF
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings><Nullable>disable</Nullable>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="$PIN_NUGET_PKG" Version="$PIN_NUGET_VER" />
  </ItemGroup>
</Project>
EOF
    printf 'Console.WriteLine("consumer-r-ok: " + typeof(Newtonsoft.Json.JsonConvert).Assembly.GetName().Version);\n' \
      > consumer-r/Program.cs
    local rout
    rout="$(run_dotnet run --project consumer-r)" || return 1
    printf '%s' "$rout" | grep -q "consumer-r-ok: 13.0.0.0" \
      || { echo "consumer output: $rout"; return 1; }
    record nuget/remote PASS "nuget.org pull-through + restore/run $PIN_NUGET_PKG $PIN_NUGET_VER ($nst)"
  fi
  # ---- T-479 virtual segment ----
  if [ "$NET_OK" = 1 ]; then
    ensure_virtual uat-matrix-nuget-virtual nuget \
      "uat-matrix-nuget-local,uat-matrix-nuget-remote" || return 1
    rm -rf consumer-v; mkdir -p consumer-v || return 1
    nuget_config consumer-v/nuget.config uat-matrix-nuget-virtual
    cat > consumer-v/consumer.csproj <<EOF
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings><Nullable>disable</Nullable>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="single-example" Version="$NVER" />
    <PackageReference Include="$PIN_NUGET_PKG" Version="$PIN_NUGET_VER" />
  </ItemGroup>
</Project>
EOF
    printf 'Console.WriteLine("consumer-v-ok");\n' > consumer-v/Program.cs
    local vout
    vout="$(run_dotnet run --project consumer-v)" || return 1
    printf '%s' "$vout" | grep -q "consumer-v-ok" || { echo "consumer output: $vout"; return 1; }
    record nuget/virtual PASS "restore resolves local member + remote member"
  fi
}

leg_go() {
  require_tier pro || return $?
  ensure_repo uat-matrix-go-local go
  WORKDIR="$WORK/go"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
  command -v go >/dev/null 2>&1 || { tool_unavailable go; return $?; }
  # The fixture's golang-example carries external deps (rsc.io/quote) a
  # local-only repo cannot resolve — synthesize a dependency-free module in
  # the official zip shape (STORED entries under module@version/, sorted).
  local mod="example.com/matrix"
  python3 - "$mod" "$GVER" <<'PYEOF' || return 1
import sys, zipfile
mod, ver = sys.argv[1], sys.argv[2]
files = [
    (f"{mod}@{ver}/go.mod", f"module {mod}\n\ngo 1.21\n"),
    (f"{mod}@{ver}/pkg.go", f"package matrix\n\nconst Value = \"{mod}@{ver}\"\n"),
]
with zipfile.ZipFile("mod.zip", "w") as z:
    for name, content in files:
        zi = zipfile.ZipInfo(name, date_time=(2026, 1, 1, 0, 0, 0))
        zi.compress_type = zipfile.ZIP_STORED
        z.writestr(zi, content)
PYEOF
  printf 'module %s\n\ngo 1.21\n' "$mod" > go.mod.bin
  printf '{"Version":"%s","Time":"2026-01-01T00:00:00Z"}\n' "$GVER" > info.json
  local base="$BINFLOW_BASE/binflow/uat-matrix-go-local/$mod/@v/$GVER" c1 c2 c3
  c1=$(curl -su "$AUTH" -T mod.zip   -o /dev/null -w '%{http_code}' "$base.zip")
  c2=$(curl -su "$AUTH" -T go.mod.bin -o /dev/null -w '%{http_code}' "$base.mod")
  c3=$(curl -su "$AUTH" -T info.json  -o /dev/null -w '%{http_code}' "$base.info")
  { [ "${c1:0:1}" = 2 ] && [ "${c2:0:1}" = 2 ] && [ "${c3:0:1}" = 2 ]; } \
    || { echo "go trio PUT -> $c1/$c2/$c3"; return 1; }
  log "go module trio PUT done ($mod@$GVER)"
  # Pull leg: the real go client (T-285 form: GOPROXY + GOSUMDB=off).
  mkdir -p consumer; cd consumer || return 1 || return 1
  cat > main.go <<EOF
package main

import _ "$mod"

func main() {}
EOF
  export GOFLAGS=-mod=mod GOTOOLCHAIN=local GOENV=off GOSUMDB=off \
         GOPROXY="$BINFLOW_BASE/binflow/uat-matrix-go-local" \
         GOCACHE="$PWD/gocache" GOMODCACHE="$PWD/gomodcache" GOPATH="$PWD/gopath"
  go mod init example.com/consumer >/dev/null
  go mod edit -require="$mod@$GVER"
  go mod download "$mod@$GVER" || return 1
  go build ./... || return 1
  ok "go: PUT trio + GOPROXY download/build ($mod@$GVER)"
  # ---- T-479 remote segment: proxy.golang.org pull-through ----
  guard_upstream go "$UP_GO" || return 1
  if [ "$NET_OK" = 1 ]; then
    ensure_remote uat-matrix-go-remote go "$UP_GO" || return 1
    local gz="golang.org/x/mod/@v/v0.17.0.zip" gst
    gst="$(retry_run 2 cached_twice "$BINFLOW_BASE/binflow/uat-matrix-go-remote/$gz")" \
      || { echo "go remote zip cache probe failed: $gst"; return 1; }
    # client-level: fresh module resolves the pinned public module via the remote
    rm -rf consumer-r; mkdir -p consumer-r || return 1
    cat > consumer-r/main.go <<'EOF'
package main

import (
	"fmt"

	"golang.org/x/mod/semver"
)

func main() { fmt.Println("consumer-r-ok:", semver.IsValid("v1.2.3")) }
EOF
    # shellcheck disable=SC2030,SC2031  # subshell env isolation is the point
    ( cd consumer-r && export GOFLAGS=-mod=mod GOTOOLCHAIN=local GOENV=off GOSUMDB=off \
        GOPROXY="$BINFLOW_BASE/binflow/uat-matrix-go-remote" \
        GOCACHE="$PWD/gocache" GOMODCACHE="$PWD/gomodcache" GOPATH="$PWD/gopath" \
      && go mod init example.com/consumer-r >/dev/null \
      && go mod edit -require="$PIN_GOMOD" \
      && go mod tidy >/dev/null \
      && go run . ) || return 1
    record go/remote PASS "GOPROXY=remote resolves $PIN_GOMOD ($gst)"
  fi
  # ---- T-479 virtual segment: local member first, remote member fallback ----
  if [ "$NET_OK" = 1 ]; then
    ensure_virtual uat-matrix-go-virtual go \
      "uat-matrix-go-local,uat-matrix-go-remote" || return 1
    local hgv="$WORK/hdr-gov.$$"
    hdr_get "$hgv" "$BINFLOW_BASE/binflow/uat-matrix-go-virtual/$mod/@v/$GVER.mod" || return 1
    [ "$(resolved_from "$hgv")" = "uat-matrix-go-local" ] \
      || { echo "local module via virtual resolved from '$(resolved_from "$hgv")'"; return 1; }
    hdr_get "$hgv" "$BINFLOW_BASE/binflow/uat-matrix-go-virtual/golang.org/x/mod/@v/v0.17.0.mod" || return 1
    [ "$(resolved_from "$hgv")" = "uat-matrix-go-remote" ] \
      || { echo "x/mod via virtual resolved from '$(resolved_from "$hgv")'"; return 1; }
    rm -f "$hgv"
    rm -rf consumer-v; mkdir -p consumer-v || return 1
    cat > consumer-v/main.go <<EOF
package main

import (
	_ "$mod"
	"golang.org/x/mod/semver"
)

func main() { println("consumer-v-ok", semver.IsValid("v1.2.3")) }
EOF
    # shellcheck disable=SC2030,SC2031  # subshell env isolation is the point
    ( cd consumer-v && export GOFLAGS=-mod=mod GOTOOLCHAIN=local GOENV=off GOSUMDB=off \
        GOPROXY="$BINFLOW_BASE/binflow/uat-matrix-go-virtual" \
        GOCACHE="$PWD/gocache" GOMODCACHE="$PWD/gomodcache" GOPATH="$PWD/gopath" \
      && go mod init example.com/consumer-v >/dev/null \
      && go mod edit -require="$mod@$GVER" -require="$PIN_GOMOD" \
      && go build ./... \
      && go run . ) || return 1
    record go/virtual PASS "GOPROXY=virtual: local member hit + remote member fallback"
  fi
}

leg_helm() {
  require_tier pro || return $?
  ensure_repo uat-matrix-helm-local helm          # classic chart repo
  ensure_repo uat-matrix-helmoci-local helmoci    # OCI face (helm push/pull)
  WORKDIR="$WORK/helm"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
  command -v helm >/dev/null 2>&1 || { tool_unavailable helm; return $?; }
  # No chart scaffold in project-examples — helm create (client-side).
  helm create matrix-chart >/dev/null
  sed_file matrix-chart/Chart.yaml -e "s|^version: 0.1.0|version: $VER|"
  helm package matrix-chart --destination . >/dev/null || return 1
  [ -f "matrix-chart-$VER.tgz" ] || { echo "chart tgz missing"; return 1; }
  # Classic face: curl PUT (server recomputes index.yaml) + repo pull.
  local code
  code=$(curl -su "$AUTH" -T "matrix-chart-$VER.tgz" -o /dev/null -w '%{http_code}' \
    "$BINFLOW_BASE/binflow/uat-matrix-helm-local/matrix-chart-$VER.tgz")
  [ "${code:0:1}" = 2 ] || { echo "helm classic PUT -> $code"; return 1; }
  helm repo add bf-matrix "$BINFLOW_BASE/binflow/uat-matrix-helm-local" --force-update >/dev/null \
    || return 1
  helm repo update bf-matrix >/dev/null || return 1
  mkdir -p classic-pull
  helm pull bf-matrix/matrix-chart --version "$VER" -d classic-pull || return 1
  cmp "matrix-chart-$VER.tgz" "classic-pull/matrix-chart-$VER.tgz" \
    || { echo "classic pull bytes differ"; return 1; }
  log "helm classic: PUT + repo pull byte-identical"
  # OCI face: helm push + helm pull (the chart as an OCI artifact).
  echo "$BINFLOW_PASSWORD" | helm registry login "$REG" -u "$BINFLOW_USER" \
    --password-stdin --plain-http >/dev/null || return 1
  helm push "matrix-chart-$VER.tgz" "oci://$REG/uat-matrix-helmoci-local" \
    --plain-http >/dev/null || return 1
  mkdir -p oci-pull
  helm pull "oci://$REG/uat-matrix-helmoci-local/matrix-chart" --version "$VER" \
    --plain-http -d oci-pull || return 1
  cmp "matrix-chart-$VER.tgz" "oci-pull/matrix-chart-$VER.tgz" \
    || { echo "oci pull bytes differ"; return 1; }
  ok "helm: classic repo roundtrip + OCI helm push/pull (matrix-chart $VER)"
  # ---- T-479 remote segment: upstream chart repo index pull-through ----
  # This upstream's index.yaml carries ABSOLUTE urls to github release
  # assets; the plain remote serves the index verbatim, so the CONTENT
  # proxy story lives on the virtual's _external fold (segment below) —
  # here we assert the index pull-through itself.
  guard_upstream helm "$UP_HELM" || return 1
  if [ "$NET_OK" = 1 ]; then
    ensure_remote uat-matrix-helm-remote helm "$UP_HELM" || return 1
    local hst
    hst="$(retry_run 2 cached_twice "$BINFLOW_BASE/binflow/uat-matrix-helm-remote/index.yaml")" \
      || { echo "helm remote index cache probe failed: $hst"; return 1; }
    fetch "$BINFLOW_BASE/binflow/uat-matrix-helm-remote/index.yaml" rindex.yaml || return 1
    grep -q "name: $PIN_HELM_CHART" rindex.yaml \
      || { echo "pinned chart absent from proxied index"; return 1; }
    grep -q "$PIN_HELM_CHART-$PIN_HELM_VER.tgz" rindex.yaml \
      || { echo "pinned chart version $PIN_HELM_VER absent from proxied index"; return 1; }
    record helm/remote PASS "index.yaml pull-through w/ pinned $PIN_HELM_CHART $PIN_HELM_VER ($hst)"
  fi
  # ---- T-479 virtual segment: aggregation + _external content fold ----
  if [ "$NET_OK" = 1 ]; then
    ensure_virtual uat-matrix-helm-virtual helm \
      "uat-matrix-helm-local,uat-matrix-helm-remote" || return 1
    helm repo add bf-matrix-virt "$BINFLOW_BASE/binflow/uat-matrix-helm-virtual" \
      --force-update >/dev/null || return 1
    helm repo update bf-matrix-virt >/dev/null || return 1
    # aggregated index: local member chart present + remote member entry
    # folded onto the _external path (fetches go THROUGH BinFlow)
    fetch "$BINFLOW_BASE/binflow/uat-matrix-helm-virtual/index.yaml" vindex.yaml || return 1
    grep -q "name: matrix-chart" vindex.yaml \
      || { echo "local member chart missing from virtual index"; return 1; }
    grep -q "_external/https/github.com/" vindex.yaml \
      || { echo "remote member entries not folded onto _external paths"; return 1; }
    # local member chart via virtual — byte-identical to what we packaged
    mkdir -p virt-pull
    helm pull bf-matrix-virt/matrix-chart --version "$VER" -d virt-pull || return 1
    cmp "matrix-chart-$VER.tgz" "virt-pull/matrix-chart-$VER.tgz" \
      || { echo "virtual-served local chart bytes differ"; return 1; }
    # remote member chart via the folded external egress (cached second hit)
    helm pull bf-matrix-virt/$PIN_HELM_CHART --version "$PIN_HELM_VER" -d virt-pull || return 1
    [ -s "virt-pull/$PIN_HELM_CHART-$PIN_HELM_VER.tgz" ] \
      || { echo "remote member chart missing via virtual"; return 1; }
    local FOLD hv
    FOLD="$(grep -o '_external/[^ "]*'"$PIN_HELM_CHART-$PIN_HELM_VER"'\.tgz' vindex.yaml | head -1)"
    [ -n "$FOLD" ] || { echo "folded tgz entry not found in virtual index"; return 1; }
    hv="$WORK/hdr-helmv.$$"
    hdr_get "$hv" "$BINFLOW_BASE/binflow/uat-matrix-helm-virtual/$FOLD" \
      || return 1
    [ "$(resolved_from "$hv")" = "uat-matrix-helm-remote" ] \
      || { echo "folded chart resolved from '$(resolved_from "$hv")'"; return 1; }
    [ "$(cache_state "$hv")" = "HIT" ] \
      || { echo "folded chart second fetch state '$(cache_state "$hv")' (expected HIT)"; return 1; }
    rm -f "$hv"
    record helm/virtual PASS "aggregated index + _external fold cached (local+remote charts)"
  fi
}

leg_conan() {
  require_tier pro || return $?
  ensure_repo uat-matrix-conan-local conan
  WORKDIR="$WORK/conan"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
  command -v conan >/dev/null 2>&1 || { tool_unavailable conan; return $?; }
  command -v cmake >/dev/null 2>&1 || { tool_unavailable cmake; return $?; }
  # No conan recipe in project-examples — hand-written minimal recipe
  # (same posture as the nuget consumer's "no template dependency"):
  # `conan new --template` died with current conan 2 (unrecognized
  # argument), and scaffolds drift across conan versions anyway. The
  # shape follows docs/user/integrations/conan.md (cmake template).
  export CONAN_HOME="$PWD/conan-home"
  rm -rf "$CONAN_HOME"; mkdir -p "$CONAN_HOME" proj/src
  cat > proj/conanfile.py <<PYEOF
from conan import ConanFile
from conan.tools.cmake import CMake, cmake_layout

class MatrixConan(ConanFile):
    name = "matrix"
    version = "$VER"
    settings = "os", "compiler", "build_type", "arch"
    generators = "CMakeDeps", "CMakeToolchain"
    exports_sources = "CMakeLists.txt", "src/*"

    def layout(self):
        cmake_layout(self)

    def build(self):
        cmake = CMake(self)
        cmake.configure()
        cmake.build()

    def package(self):
        cmake = CMake(self)
        cmake.install()
PYEOF
  cat > proj/CMakeLists.txt <<'EOF'
cmake_minimum_required(VERSION 3.15)
project(matrix CXX)
add_executable(matrix src/main.cpp)
install(TARGETS matrix RUNTIME DESTINATION bin)
EOF
  printf '#include <iostream>\nint main() { std::cout << "matrix-ok\\n"; }\n' > proj/src/main.cpp
  # Fresh CONAN_HOME ships no profiles — conan 2 demands a build profile
  # before `create`. No output swallowing: a detect failure must fail the
  # leg with its real cause, not resurface as create's generic complaint.
  conan profile detect --force
  ( cd proj && conan create . ) || return 1
  conan remote add bf-matrix "$(client_base conan)/binflow/uat-matrix-conan-local" || return 1
  conan remote login bf-matrix "$BINFLOW_USER" -p "$BINFLOW_PASSWORD" || return 1
  conan upload 'matrix/*' -r bf-matrix --confirm || return 1
  log "conan upload done (matrix/$VER)"
  # Pull leg: a fresh client cache must resolve everything from BinFlow
  # (--build=never: a binary miss is a failure, not a local rebuild).
  export CONAN_HOME="$PWD/conan-home2"
  rm -rf "$CONAN_HOME"; mkdir -p "$CONAN_HOME"
  # The second cache needs its own default profile (host+build resolution
  # runs client-side in `conan install` too).
  conan profile detect --force
  conan remote add bf-matrix "$(client_base conan)/binflow/uat-matrix-conan-local" || return 1
  conan remote login bf-matrix "$BINFLOW_USER" -p "$BINFLOW_PASSWORD" || return 1
  conan install --requires="matrix/$VER" -r bf-matrix --build=never || return 1
  ok "conan: create + upload + fresh-cache install (matrix/$VER)"
  # ---- T-479 remote segment: center2.conan.io pull-through ----
  # search is not proxied (docs: path-joined engine hops cannot carry query
  # endpoints) — the client form is a pinned exact-ref install.
  guard_upstream conan "$UP_CONAN" || return 1
  if [ "$NET_OK" = 1 ]; then
    ensure_remote uat-matrix-conan-remote conan "$UP_CONAN" || return 1
    export CONAN_HOME="$PWD/conan-home-r"; rm -rf "$CONAN_HOME"; mkdir -p "$CONAN_HOME"
    conan profile detect --force >/dev/null 2>&1 || return 1
    conan remote add bf-mx-remote "$(client_base conan)/binflow/uat-matrix-conan-remote" || return 1
    conan remote login bf-mx-remote "$BINFLOW_USER" -p "$BINFLOW_PASSWORD" || return 1
    retry_run 2 conan install --requires="$PIN_CONAN_REF" -r bf-mx-remote --build=missing \
      || return 1
    record conan/remote PASS "center2 pull-through install $PIN_CONAN_REF"
  fi
  # ---- T-479 virtual segment: local + remote members ----
  if [ "$NET_OK" = 1 ]; then
    ensure_virtual uat-matrix-conan-virtual conan \
      "uat-matrix-conan-local,uat-matrix-conan-remote" || return 1
    export CONAN_HOME="$PWD/conan-home-v"; rm -rf "$CONAN_HOME"; mkdir -p "$CONAN_HOME"
    conan profile detect --force >/dev/null 2>&1 || return 1
    conan remote add bf-mx-virt "$(client_base conan)/binflow/uat-matrix-conan-virtual" || return 1
    conan remote login bf-mx-virt "$BINFLOW_USER" -p "$BINFLOW_PASSWORD" || return 1
    retry_run 2 conan install --requires="matrix/$VER" --requires="$PIN_CONAN_REF" \
      -r bf-mx-virt --build=missing || return 1
    record conan/virtual PASS "aggregated install local matrix/$VER + remote $PIN_CONAN_REF"
  fi
}

# ---------------------------------------------------------------------------
# Leg runner: independent legs, failures recorded, never masked.
# ---------------------------------------------------------------------------
record() { printf '%s\t%s\t%s\t%s\n' "$(date -u +%FT%TZ)" "$1" "$2" "$3" >> "$REPORT"; }

print_report() {
  echo "--- protocol matrix report ($REPORT) ---"
  cat "$REPORT"
}

run_leg() {
  local leg="$1" rc=0 out rc1
  if ! declare -F "leg_$leg" >/dev/null; then
    fail "unknown leg: $leg"; record "$leg" FAIL "unknown leg"; return 1
  fi
  log "=== leg: $leg (version $VER) ==="
  out="$( { set -euo pipefail; leg_"$leg"; } 2>&1 )" && rc1=0 || rc1=$?
  if [ $rc1 -eq 0 ]; then
    [ -n "$out" ] && printf '%s\n' "$out"
    ok "leg $leg"
    record "$leg" PASS "ok"
  elif [ $rc1 -eq 2 ]; then
    printf '%s\n' "$out"
    log "SKIP leg $leg — toolchain unavailable on this host (local posture; CI runs strict)"
    record "$leg" SKIP "$(printf '%s' "$out" | tail -1)"
  else
    printf '%s\n' "$out" >&2
    fail "leg $leg"
    record "$leg" FAIL "$(printf '%s' "$out" | tail -1)"
    rc=1
  fi
  return $rc
}

# ---------------------------------------------------------------------------
# Entry
# ---------------------------------------------------------------------------
if [ "$MODE_REPORT" = 1 ]; then print_report; exit 0; fi

log "target: $BINFLOW_BASE (registry $REG) — run stamp $RUN_STAMP, version $VER"
log "workdir: $WORK (report: $REPORT)"

# Preflight: instance reachable, admin credentials valid.
code="$(curl -s -u "$AUTH" -o /dev/null -w '%{http_code}' -m 10 \
  "$BINFLOW_BASE/binflow/api/system/license")"
if [ "$code" != 200 ]; then
  fail "preflight: /api/system/license -> HTTP $code (instance down or bad credentials)"
  record preflight FAIL "license endpoint HTTP $code"
  exit 1
fi
tier="$(curl -s -u "$AUTH" "$BINFLOW_BASE/binflow/api/system/license" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tier","community"))')"
log "preflight: instance reachable, license tier=$tier (go/nuget/helm/conan/helmoci need pro+)"
record preflight INFO "tier=$tier base=$BINFLOW_BASE stamp=$RUN_STAMP"

ensure_fixtures || { fail "fixture clone failed"; exit 1; }

SELECTED="${REQUESTED:-$LEGS_ALL}"
FAILED=0
for leg in $SELECTED; do
  run_leg "$leg" || FAILED=1
done

echo
print_report
if [ "$FAILED" = 1 ]; then
  fail "protocol matrix RED — see legs above"
  exit 1
fi
ok "protocol matrix GREEN (skips are host-toolchain gaps, not protocol failures)"
