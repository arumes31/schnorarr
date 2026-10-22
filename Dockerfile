# syntax=docker/dockerfile:1

# Build Stage
FROM golang:1.27.3-alpine3.24@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS builder
WORKDIR /app

# Copy dependency files first for better layer caching
COPY go.mod .
COPY go.sum .
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY cmd/ cmd/
COPY internal/ internal/

# Build with CGO disabled for faster compilation and static binary
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-w -s" -o monitor ./cmd/monitor

# Final Stage
FROM alpine:3.24@sha256:71e9b7bae160ba167fa17414401b94cbcdaf3c32feeab9298bd794a2a0614c35

# Install necessary packages
RUN apk add --no-cache \
    rsync \
    inotify-tools \
    tailscale \
    bash \
    ca-certificates \
    iptables \
    ip6tables \
    shadow \
    coreutils

# Create data and config directories
RUN mkdir -p /data /config /scripts

# Copy Monitor Binary
COPY --from=builder /app/monitor /usr/local/bin/monitor

# Set up scripts
# Set up scripts
COPY scripts/ /scripts/
RUN apk add --no-cache dos2unix && \
    dos2unix /scripts/*.sh /scripts/*.filter && \
    chmod +x /scripts/*.sh

# Intercept rsync for dashboard logging
RUN mv /usr/bin/rsync /usr/bin/rsync.real && \
    cp /scripts/rsync-wrapper.sh /usr/bin/rsync && \
    chmod +x /usr/bin/rsync

# Environment variables
ENV MODE=sender
ENV SOURCE_DIR=/data
ENV DEST_HOST=receiver
ENV DEST_MODULE=video-sync
ENV TAILSCALE_AUTHKEY=""
ENV TS_HOSTNAME=""
ENV TAILSCALE_UP_ARGS=""
ENV RSYNC_USER=""
ENV RSYNC_PASSWORD=""
ENV BWLIMIT_MBPS=""
ENV MODE=sender \
    AUTH_ENABLED=false
ENV ADMIN_USER="admin"
ENV ADMIN_PASS="schnorarr"

ENTRYPOINT ["/scripts/entrypoint.sh"]
