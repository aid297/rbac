# rbac server image — multi-stage static build.
# Build from repo root:   docker build -f kernal/Dockerfile -t rbac:latest .

FROM golang:1.27-alpine AS build
WORKDIR /src

# Cache deps first: copy only module files.
COPY kernal/go.mod kernal/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY kernal/ ./
# Static binary — no cgo anywhere in the dependency tree (verified: gmsm, go-redis,
# miniredis, zap are pure Go), so the image needs no runtime libc.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/rbac ./cmd/rbac

FROM alpine:3.22
RUN addgroup -S rbac && adduser -S -G rbac rbac

# Persistent state lives under /var/lib/rbac (mount these).
#   /var/lib/rbac/data   policy store (policy.rbac)
#   /var/lib/rbac/secret self-signed CA + server cert (auto-generated on first start)
RUN mkdir -p /etc/rbac /var/lib/rbac/data /var/lib/rbac/secret /var/log/rbac \
    && chown -R rbac:rbac /var/lib/rbac /var/log/rbac

COPY --from=build /out/rbac /usr/local/bin/rbac

# Default config: all four API listeners on, admin off, logs to stdout.
# Override by mounting your own file at /etc/rbac/config.yaml.
COPY kernal/docker/config.yaml /etc/rbac/config.yaml
ENV RBAC_CONFIG=/etc/rbac/config.yaml

USER rbac
WORKDIR /var/lib/rbac
VOLUME ["/var/lib/rbac/data", "/var/lib/rbac/secret", "/var/log/rbac"]

# http / https / grpc / grpc_tls (9090 admin is off by default).
EXPOSE 8080 8443 9080 9443

# Valid for the default config (http on). For grpc-only deployments
# override with `docker run --no-healthcheck`.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/rbac"]
CMD ["--config", "/etc/rbac/config.yaml"]
