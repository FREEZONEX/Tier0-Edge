# Build from the repository root; this image contains no Backend or scripts.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
WORKDIR /src
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
COPY backend/go.mod backend/go.sum ./
COPY backend/internal ./internal
COPY backend/share ./share
COPY backend/cmd/source-modbus ./cmd/source-modbus
RUN --mount=type=cache,id=enterprise-go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=enterprise-modbus-build-${TARGETOS}-${TARGETARCH},target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOMAXPROCS=2 \
    go build -p=2 -buildvcs=false -trimpath -ldflags='-s -w' -o /out/source-modbus ./cmd/source-modbus
RUN mkdir -p /out/run/source-modbus && chmod 0750 /out/run/source-modbus

# Modbus TCP requires no shell, package manager or TLS store. The static binary
# includes its own private-socket health check; do not fetch runtime packages.
FROM scratch
COPY --from=builder --chown=65532:65532 /out/run /run
COPY --from=builder --chmod=0755 /out/source-modbus /app/source-modbus
USER 65532:65532
ENV GOMEMLIMIT=192MiB GOMAXPROCS=2
ENTRYPOINT ["/app/source-modbus"]
