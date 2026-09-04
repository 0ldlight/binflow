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
#   BINFLOW_BASE           e.g. http://52.79.109.153:8080   (required)
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
# Exit code: 0 = every requested leg passed or skipped, 1 = at least one
# leg failed — legs run independently, a failure never masks the rest.

set -uo pipefail

LEGS_ALL="generic maven gradle npm pypi docker nuget go helm conan"
FIXTURES_URL="https://github.com/jfrog/project-examples"

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
    -h|--help)    sed -n '2,45p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
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
  local key="$1" ptype="$2" body code
  body="$WORK/ensure-$key.json"
  code=$(curl -su "$AUTH" -X PUT "$BINFLOW_BASE/binflow/api/repositories/$key" \
    -H 'Content-Type: application/json' \
    -d "{\"rclass\":\"local\",\"packageType\":\"$ptype\",\"description\":\"T-469 CI protocol matrix (kept across runs; versions stamp per run)\"}" \
    -o "$body" -w '%{http_code}')
  case "$code" in
    2*) log "repo $key ($ptype) ready (HTTP $code)"; return 0 ;;
    *)  echo "repo upsert $key -> HTTP $code:"; cat "$body"; echo; return 1 ;;
  esac
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
}

leg_nuget() {
  require_tier pro || return $?
  ensure_repo uat-matrix-nuget-local nuget
  WORKDIR="$WORK/nuget"; mkdir -p "$WORKDIR"; cd "$WORKDIR" || return 1
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
    -e "s|<ImplicitUsings>enable</ImplicitUsings>|<ImplicitUsings>enable</ImplicitUsings><Version>$VER</Version>|"
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
  local nupkg="pkg/single-example.$VER.nupkg"
  [ -f "$nupkg" ] || { echo "packed nupkg missing (looked for $nupkg)"; ls pkg; return 1; }
  run_dotnet nuget push "$nupkg" --source binflow || return 1
  log "dotnet nuget push done (single-example $VER)"
  # Pull leg: hand-written consumer (no `dotnet new` template dependency).
  rm -rf consumer; mkdir -p consumer; cp proj/nuget.config consumer/
  cat > consumer/consumer.csproj <<EOF
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings><Nullable>disable</Nullable>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="single-example" Version="$VER" />
  </ItemGroup>
</Project>
EOF
  printf 'Console.WriteLine("consumer-ok");\n' > consumer/Program.cs
  local out
  out="$(run_dotnet run --project consumer)" || return 1
  printf '%s' "$out" | grep -q "consumer-ok" || { echo "consumer output: $out"; return 1; }
  ok "nuget: pack + push + restore/run roundtrip (single-example $VER)"
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
PYEOF
  cat > proj/CMakeLists.txt <<'EOF'
cmake_minimum_required(VERSION 3.15)
project(matrix CXX)
add_executable(matrix src/main.cpp)
install(TARGETS matrix RUNTIME DESTINATION bin)
EOF
  printf '#include <iostream>\nint main() { std::cout << "matrix-ok\\n"; }\n' > proj/src/main.cpp
  ( cd proj && conan create . ) || return 1
  conan remote add bf-matrix "$(client_base conan)/binflow/uat-matrix-conan-local" || return 1
  conan remote login bf-matrix "$BINFLOW_USER" -p "$BINFLOW_PASSWORD" || return 1
  conan upload 'matrix/*' -r bf-matrix --confirm || return 1
  log "conan upload done (matrix/$VER)"
  # Pull leg: a fresh client cache must resolve everything from BinFlow
  # (--build=never: a binary miss is a failure, not a local rebuild).
  export CONAN_HOME="$PWD/conan-home2"
  rm -rf "$CONAN_HOME"; mkdir -p "$CONAN_HOME"
  conan remote add bf-matrix "$(client_base conan)/binflow/uat-matrix-conan-local" || return 1
  conan remote login bf-matrix "$BINFLOW_USER" -p "$BINFLOW_PASSWORD" || return 1
  conan install --requires="matrix/$VER" -r bf-matrix --build=never || return 1
  ok "conan: create + upload + fresh-cache install (matrix/$VER)"
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
