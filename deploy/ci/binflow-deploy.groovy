// binflow-deploy — T-298 continuous deploy to the BinFlow test environment.
//
// Chain: git push vm main -> binflow-ci-smoke (pollSCM <=1min, 21s warm)
//        -> [this job, upstream SUCCESS trigger] -> build -> staged swap on
//        the VM systemd instance (:8080) -> readiness probe -> deploy smoke.
//
// Scope (user directive 2026-08-25): EVERY change — code, console AND docs —
// deploys here. The docs site and the console SPA are EMBEDDED in the
// binary (go:embed, T-89/T-129 placeholder strategy), so this job rebuilds
// both faces every run and ships ONE artifact: atomic deploy, atomic
// rollback, zero extra components on the VM (embed-vs-nginx rationale in
// reports/agents/T-298.md §2).
//
// FORM=ci (default): build bare-repo main HEAD — make console + make docs +
//   make build, then re-stamp the server with -X main.version=v1.0.0-ci.<sha>
//   (make build alone leaves version=dev/dev, Q4/T-127).
// FORM=release: pull the goreleaser-stamped linux/amd64 binary OUT of the
//   BinFlow-hosted image 172.16.58.129:8080/docker-local/binflow:$TAG-alpine-amd64
//   (dogfood: the release artifact is served by BinFlow itself).
//
// Host-side engine: /srv/jenkins/t298/deploy-vm.sh (repo copy:
// deploy/ci/deploy-vm.sh), reached through a privileged nsenter helper:
//   docker run --rm --privileged --pid=host --network host \
//     binflow-t298-hostctl:alpine nsenter -t 1 -m -u -i -n -- /bin/bash <script> <mode>...
// (nsenter into host PID 1's namespaces — a plain chroot cannot reach the
// host systemd bus; --pid=host makes PID 1 the host systemd.)
// The agent container already owns the host docker socket (host root);
// the helper adds no privilege, it just exercises it deliberately.
// --network host: probes from containers must hit the host namespace
// (T-248 pitfall 7). The engine NEVER touches /var/lib/binflow.
pipeline {
  agent any
  options {
    timestamps()
    timeout(time: 40, unit: 'MINUTES')
    buildDiscarder(logRotator(numToKeepStr: '50'))
    disableConcurrentBuilds()
  }
  parameters {
    choice(name: 'FORM', choices: ['ci', 'release'], description: 'ci = build main HEAD (default chain form); release = deploy the docker-local image TAG')
    string(name: 'TAG', defaultValue: '', description: 'FORM=release only: tag in docker-local/binflow (e.g. v1.0.0-t270); the -alpine-amd64 per-arch image carries the goreleaser-stamped binary')
  }
  // threshold omitted: ReverseBuildTrigger defaults to SUCCESS (a red or
  // unstable smoke never deploys); spelling the enum needs sandbox approval.
  triggers { upstream(upstreamProjects: 'binflow-ci-smoke') }
  environment {
    GOPATH      = '/var/jenkins_home/go'
    GOCACHE     = '/var/jenkins_home/.cache/go-build'
    GOPROXY     = 'https://goproxy.cn,direct'
    GOTOOLCHAIN = 'local'
    npm_config_cache = '/var/jenkins_home/.npm'
    // agent container -> host services: always the host IP (T-248 pitfall 7)
    BF_ADDR     = 'http://172.16.58.129:8080'
    BF_REG      = '172.16.58.129:8080/docker-local'
    // agent-visible staging == host /srv/jenkins/t298/incoming
    STAGE       = '/var/jenkins_home/t298/incoming'
    HOSTCTL     = 'binflow-t298-hostctl:alpine'
    DEPLOY_FORM = "${params.FORM ?: 'ci'}"
    DEPLOY_TAG  = "${params.TAG ?: ''}"
    PATH        = "/usr/local/go/bin:/usr/local/ci-bin:${env.PATH}"
  }
  stages {
    stage('checkout') {
      steps {
        checkout([$class: 'GitSCM',
                  branches: [[name: '*/main']],
                  userRemoteConfigs: [[url: '/home/lzw/binflow.git']],
                  extensions: [[$class: 'CleanBeforeCheckout']]])
        sh 'git log -1 --oneline && echo CHECKOUT_OK'
      }
    }
    stage('artifact: ci build (console + docs embed + version stamp)') {
      when { expression { env.DEPLOY_FORM == 'ci' } }
      steps {
        script {
          env.DEPLOY_LABEL = sh(returnStdout: true, script: '''#!/bin/bash
set -e
# returnStdout captures EVERYTHING on stdout — keep the build log on stderr
# and print only the label on stdout.
{
  SHA=$(git rev-parse --short HEAD)
  LABEL="v1.0.0-ci.$SHA"
  echo "=== console chain (embed real SPA)"
  make console
  echo "=== docs chain (embed real Docusaurus site — docs ride the same CD chain, T-298)"
  make docs
  echo "=== make build (three binaries + size report)"
  make build
  echo "=== stamp the server (version injection; bare make build leaves dev/dev, Q4)"
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$LABEL -X main.revision=$SHA" -o bin/binflow-server ./cmd/binflow-server
  mkdir -p "$STAGE"
  cp bin/binflow-server "$STAGE/binflow-server.in"
  sha256sum "$STAGE/binflow-server.in"
  echo "staged: $STAGE/binflow-server.in label=$LABEL"
} 1>&2
echo "$LABEL"
''').trim()
        }
      }
    }
    stage('artifact: release pull (BinFlow-hosted image)') {
      when { expression { env.DEPLOY_FORM == 'release' } }
      steps {
        script {
          if (env.DEPLOY_TAG == '') { error 'FORM=release requires TAG (e.g. v1.0.0-t270)' }
          env.DEPLOY_LABEL = env.DEPLOY_TAG
        }
        sh '''#!/bin/bash
set -e
IMG="$BF_REG/binflow:$DEPLOY_TAG-alpine-amd64"
echo "=== pulling the release binary out of $IMG (dogfood: BinFlow serves it)"
docker pull "$IMG"
mkdir -p "$STAGE"
cid=$(docker create "$IMG")
docker cp "$cid":/usr/local/bin/binflow-server "$STAGE/binflow-server.in"
docker rm "$cid"
sha256sum "$STAGE/binflow-server.in"
echo "staged: $STAGE/binflow-server.in label=$DEPLOY_TAG (goreleaser-stamped inside the image)"
'''
      }
    }
    stage('deploy: swap + readiness probe (+ auto-rollback)') {
      steps {
        sh '''#!/bin/bash
set -e
test -s "$STAGE/binflow-server.in" || { echo "no staged binary"; exit 1; }
echo "=== pre-deploy host status"
docker run --rm --privileged --pid=host --network host "$HOSTCTL" \\
  nsenter -t 1 -m -u -i -n -- /bin/bash /srv/jenkins/t298/deploy-vm.sh status
echo "=== deploy (backup -> stop -> swap -> start -> probe 60s; rc42 = already rolled back)"
set +e
timeout 300 docker run --rm --privileged --pid=host --network host "$HOSTCTL" \\
  nsenter -t 1 -m -u -i -n -- /bin/bash /srv/jenkins/t298/deploy-vm.sh deploy /srv/jenkins/t298/incoming/binflow-server.in "$DEPLOY_LABEL"
rc=$?
set -e
echo "deploy-vm.sh exit=$rc (0 ok / 42 rolled back / 43 rollback failed)"
[ "$rc" -eq 0 ] || exit 1
echo "=== live version after swap"
curl -sf "$BF_ADDR/binflow/api/system/version"; echo
'''
      }
    }
    stage('smoke: ping + version + repo + upload + download + delete + docs') {
      steps {
        withCredentials([usernamePassword(credentialsId: 'binflow-admin', usernameVariable: 'BF_USER', passwordVariable: 'BF_PASS')]) {
          sh '''#!/bin/bash
set -u
A="$BF_ADDR"; L="$DEPLOY_LABEL"
smoke() {
  set -e
  # 1. readiness/liveness on the management plane
  curl -sf "$A/binflow/api/system/ping" >/dev/null && echo "ping: OK"
  # 2. version stamp — proves the swap actually took
  v=$(curl -sf "$A/binflow/api/system/version")
  echo "version: $v"
  echo "$v" | grep -q "$L" || { echo "VERSION MISMATCH: expected $L"; return 1; }
  # 3. repository (idempotent create; fixed key so the test env never accumulates)
  code=$(curl -s -o /tmp/t298-repo.json -w '%{http_code}' -u "$BF_USER:$BF_PASS" "$A/binflow/api/repositories/t298-smoke")
  if [ "$code" = "404" ]; then
    curl -sf -X PUT -u "$BF_USER:$BF_PASS" -H 'Content-Type: application/json' \\
      -d '{"key":"t298-smoke","rclass":"local","packageType":"generic","description":"T-298 continuous-deploy smoke target"}' \\
      "$A/binflow/api/repositories/t298-smoke" && echo " <- repo created"
  elif [ "$code" != "200" ]; then
    echo "repo GET failed: $code"; cat /tmp/t298-repo.json; return 1
  else
    echo "repo: t298-smoke present (idempotent reuse)"
  fi
  # 4. upload (generic content plane, PUT)
  path="t298/deploy-smoke/$L.txt"
  printf 't298 deploy-smoke %s %s\\n' "$(date -u +%FT%TZ)" "$L" > /tmp/t298-artifact.txt
  up=$(curl -sf -X PUT -u "$BF_USER:$BF_PASS" --data-binary @/tmp/t298-artifact.txt "$A/binflow/t298-smoke/$path")
  echo "upload: $up"
  # 5. download + byte-compare
  curl -sf -u "$BF_USER:$BF_PASS" -o /tmp/t298-downloaded.txt "$A/binflow/t298-smoke/$path"
  cmp /tmp/t298-artifact.txt /tmp/t298-downloaded.txt && echo "download: byte-identical roundtrip"
  # 6. delete + confirm gone
  curl -sf -X DELETE -u "$BF_USER:$BF_PASS" "$A/binflow/t298-smoke/$path" >/dev/null && echo "delete: OK"
  code=$(curl -s -o /dev/null -w '%{http_code}' -u "$BF_USER:$BF_PASS" "$A/binflow/t298-smoke/$path")
  [ "$code" = "404" ] || { echo "artifact still resolvable after delete: $code"; return 1; }
  # 7. embedded docs face (the CD chain ships docs inside the binary).
  #    FORM-aware: release images cut before T-298 embed only the docs
  #    placeholder (T-248 leftover — make docs is not in the release chain),
  #    so the Docusaurus structure gate is ci-only; release asserts the
  #    endpoint answers. Flip both to the strict gate once the release
  #    chain embeds docs (T-298 leftover L2).
  d=$(curl -sf "$A/binflow/docs/") || { echo "docs face unreachable"; return 1; }
  case "$DEPLOY_FORM" in
    ci)
      echo "$d" | grep -q __docusaurus || { echo "docs face is not the built Docusaurus site"; return 1; }
      echo "docs: /binflow/docs/ serves the embedded Docusaurus build" ;;
    release)
      echo "docs: /binflow/docs/ answers (release form: placeholder embed accepted until the release chain runs make docs — T-298 L2)" ;;
  esac
}
if smoke; then
  echo "=== SMOKE GREEN (deploy $L verified end to end)"
else
  echo "=== SMOKE RED — rolling back to the previous binary"
  timeout 180 docker run --rm --privileged --pid=host --network host "$HOSTCTL" \\
    nsenter -t 1 -m -u -i -n -- /bin/bash /srv/jenkins/t298/deploy-vm.sh rollback || true
  exit 1
fi
'''
        }
      }
    }
  }
  post {
    always {
      cleanWs(notFailBuild: true)
    }
  }
}
