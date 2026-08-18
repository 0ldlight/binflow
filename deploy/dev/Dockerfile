# BinFlow development image (T-17, PRD FR-6-AC1..AC3).
#
# Multi-stage build:
#   1. builder — golang:1.26-alpine, CGO disabled (ADR-0005 zero-CGo
#      baseline), GOPROXY mirror fallback for restricted networks
#      (ADR-0005: proxy.golang.org is unreachable from this network;
#      goproxy.cn first, `direct` fallback — the mirror only fetches the
#      ADR-whitelisted dependency set, it does not widen it).
#   2. runtime — minimal alpine, non-root user, data_dir=/var/lib/binflow
#      (architecture section 9 deployment convention), EXPOSE 8080,
#      HEALTHCHECK on the unauthenticated readiness probe /readyz
#      (metadata ping + storage writable; probes carry no credentials by
#      design, architecture section 7.1).
#
# Build context is the REPOSITORY ROOT (docker-compose.yml sets it):
#
#	docker build -f deploy/dev/Dockerfile -t binflow:dev .

# ---- stage 1: builder -------------------------------------------------------

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
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -o /out/binflow-server ./cmd/binflow-server

# ---- stage 2: runtime -------------------------------------------------------

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
