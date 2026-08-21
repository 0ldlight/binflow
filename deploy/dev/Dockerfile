# BinFlow development image (T-17, PRD FR-6-AC1..AC3; console build stage
# added by T-106, PRD FR-33-AC5).
#
# Multi-stage build:
#   1. console — node:22-alpine builds the web console from web/ with the
#      lockfile-pinned toolchain (ADR-0014 decision 4). The image embeds
#      the REAL SPA whatever the builder's local checkout state: `make
#      console` output is gitignored, so a fresh clone (or CI) carries only
#      the committed placeholder shell — the console is built here, never
#      borrowed from the context. node:22 matches the toolchain the shipped
#      bundle was built and QA'd with (web/package.json engines >= 20).
#   2. builder — golang:1.26-alpine, CGO disabled (ADR-0005 zero-CGo
#      baseline), GOPROXY mirror fallback for restricted networks
#      (ADR-0005: proxy.golang.org is unreachable from this network;
#      goproxy.cn first, `direct` fallback — the mirror only fetches the
#      ADR-whitelisted dependency set, it does not widen it).
#   3. runtime — minimal alpine, non-root user, data_dir=/var/lib/binflow
#      (architecture section 9 deployment convention), EXPOSE 8080,
#      HEALTHCHECK on the unauthenticated readiness probe /readyz
#      (metadata ping + storage writable; probes carry no credentials by
#      design, architecture section 7.1).
#
# Build context is the REPOSITORY ROOT (docker-compose.yml sets it):
#
#	docker build -f deploy/dev/Dockerfile -t binflow:dev .

# ---- stage 1: console (node) ------------------------------------------------

FROM node:22-alpine AS console
WORKDIR /web
# Lockfile-pinned install (ADR-0014 decision 4), cached as its own layer
# like the Go module download below.
COPY web/package.json web/package-lock.json ./
RUN npm ci
# Everything `vite build` reads: the entry html, configs, sources and the
# post-build asset-relink script. node_modules/, e2e/ and test output stay
# out of the copy set on purpose (a local node_modules is a host tree; the
# image builds against the container's own install).
COPY web/index.html web/vite.config.ts web/tsconfig.json web/tsconfig.node.json ./
COPY web/scripts ./scripts
COPY web/src ./src
# package.json build = vite build && node scripts/relink-assets.mjs (the
# latter rewrites the emitted /binflow/ui/assets/** URLs onto the shared
# /binflow/assets mount, see web/vite.config.ts).
RUN npm run build

# ---- stage 2: builder -------------------------------------------------------

FROM golang:1.26-alpine AS builder

# The go.mod `go` directive (1.26) selects the matching toolchain inside the
# image; GOTOOLCHAIN=local pins it so a newer directive never triggers a
# surprise toolchain download inside the build.
ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOPROXY=https://goproxy.cn,direct

WORKDIR /src

# Cache the module layer separately from the source layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd ./cmd
COPY internal ./internal
# Deterministic console embed (T-106): the context's internal/console/dist
# may carry local `make console` output (gitignored) or only the committed
# placeholder — wipe whatever came along and take exactly what stage 1
# built, so one source always yields one image.
RUN rm -rf /src/internal/console/dist/assets /src/internal/console/dist/index.html
COPY --from=console /web/dist/ /src/internal/console/dist/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -o /out/binflow-server ./cmd/binflow-server

# ---- stage 3: runtime -------------------------------------------------------

FROM alpine:3.21

# ca-certificates: outbound TLS if a future milestone needs it (M3 remote
# repos); tzdata keeps timestamps honest; tzdata+run-as-non-root are the
# baseline SLSA-ish hygiene bits. No shell-removed distroless in M1: alpine
# keeps `docker exec` debugging possible for a DEV image.
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 binflow \
    && adduser -S -D -H -u 10001 -G binflow binflow \
    && mkdir -p /var/lib/binflow /etc/binflow \
    && chown -R binflow:binflow /var/lib/binflow /etc/binflow

# Image-level default (architecture section 9: "inside the image, default data_dir=/var/lib/binflow").
# Compose overrides this only when a different mount is wanted.
ENV BINFLOW_DATA_DIR=/var/lib/binflow

COPY --from=builder --chown=binflow:binflow /out/binflow-server /usr/local/bin/binflow-server

USER binflow

# Unprivileged by default: no config file baked in — serve boots on defaults
# + environment (FR-6-AC5), every knob is a BINFLOW_* env var.
EXPOSE 8080
VOLUME /var/lib/binflow

# The readiness probe is unauthenticated by design; wget is part of the
# busybox base in alpine. 3s timeout, 5 tries of 10s -> unhealthy after
# ~50s of failed readiness.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/readyz || exit 1

ENTRYPOINT ["/usr/local/bin/binflow-server"]
CMD ["serve"]
