# syntax=docker/dockerfile:1.18@sha256:dabfc0969b935b2080555ace70ee69a5261af8a8f1b4df97b9e7fbcf6722eddf

FROM golang:1.26.7-alpine3.24@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -buildvcs=true -ldflags="-s -w" -o /out/monitor ./cmd/monitor

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk upgrade --no-cache \
    && apk add --no-cache bash ca-certificates coreutils inotify-tools rsync \
    && addgroup -S -g 65532 schnorarr \
    && adduser -S -D -H -u 65532 -G schnorarr schnorarr \
    && install -d -o 65532 -g 65532 -m 0700 /config \
    && install -d -o 65532 -g 65532 -m 0750 /data /scripts

COPY --from=builder --chown=65532:65532 --chmod=0555 /out/monitor /usr/local/bin/monitor
COPY --chown=65532:65532 --chmod=0755 scripts/entrypoint.sh scripts/rsync-wrapper.sh /scripts/
COPY --chown=65532:65532 --chmod=0644 scripts/rsyncd.conf scripts/*.filter /scripts/

RUN mv /usr/bin/rsync /usr/bin/rsync.real \
    && cp /scripts/rsync-wrapper.sh /usr/bin/rsync \
    && chmod 0555 /usr/bin/rsync /usr/bin/rsync.real

ENV MODE=sender \
    SOURCE_DIR=/data \
    DEST_MODULE=video-sync

USER 65532:65532
EXPOSE 8080 873
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- --no-check-certificate https://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/scripts/entrypoint.sh"]
