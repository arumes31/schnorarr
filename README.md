# Schnorarr

Schnorarr synchronizes media trees from a sender to an rsync receiver and provides an HTTPS operations dashboard. The sender compares manifests, plans transfers and deletions, and exposes transfer health and history.

## Security model

- The dashboard always requires `ADMIN_USER` and a non-default `ADMIN_PASS` of at least 16 characters.
- Every HTTP listener uses the configured TLS certificate and key.
- Receiver machine endpoints use a separate `INTERNAL_API_TOKEN` of at least 32 characters. The token is sent only as a Bearer header.
- Manifest, stat, health, and delete requests are confined to `/data`. Absolute paths, traversal, root deletion, and every symbolic-link component are rejected.
- Containers run as UID/GID `65532`, drop all Linux capabilities, use a read-only root filesystem, and do not mount `/dev/net/tun`.
- Rsync daemon traffic is authenticated but is not encrypted. Keep port 873 on a private network or route it through a host/sidecar VPN.

The unauthenticated liveness endpoint `GET /healthz` returns only `{"status":"ok"}`. `GET /health` and `/api/*` require the machine token.

## Secure Docker Compose setup

Requirements:

- Docker Compose v2
- a certificate trusted by the sender and browser; its SANs must include the Compose names (`sender` and `receiver`) plus the hostname used in a browser
- host data directories that UID 65532 can read, and for receiver data can write

Copy the environment template and fill every blank value:

```sh
cp .env.example .env
chmod 600 .env
openssl rand -hex 32       # INTERNAL_API_TOKEN
openssl rand -base64 32    # RSYNC_PASSWORD and ADMIN_PASS
```

Use a private CA or an organization-managed certificate. Put the leaf certificate, private key, and CA bundle at the paths configured by `TLS_CERT_PATH`, `TLS_KEY_PATH`, and `TLS_CA_PATH`. The key must be readable by UID 65532 but not by other users.

Prepare bind mounts and start a local sender/receiver pair:

```sh
mkdir -p sender receiver
sudo chown -R 65532:65532 sender receiver
docker compose -f docker-compose.dev.yml config
docker compose -f docker-compose.dev.yml up -d --build
```

The dashboard is available at `https://localhost:8443` by default. The dev Compose network does not publish the receiver's rsync or machine-API ports.

For separate hosts, use `docker-compose.receiver.yml` and `docker-compose.sender.yml`. Both standalone files bind ports to `127.0.0.1` by default. Set `RSYNC_BIND_ADDRESS`, `RECEIVER_API_BIND_ADDRESS`, or `DASHBOARD_BIND_ADDRESS` to an explicit private interface only after network filtering is in place.

Set `SCHNORARR_IMAGE_DIGEST` to the published `sha256:...` digest shown by the release workflow or GHCR. The standalone files reject mutable `latest` deployment.

## Required configuration

| Variable | Purpose |
| --- | --- |
| `MODE` | `sender` or `receiver` |
| `RSYNC_USER` | rsync daemon username; letters, digits, `.`, `_`, and `-` only |
| `RSYNC_PASSWORD` | dedicated rsync password, at least 16 characters |
| `INTERNAL_API_TOKEN` | sender-to-receiver Bearer token, at least 32 characters |
| `ADMIN_USER` | dashboard administrator username |
| `ADMIN_PASS` | dashboard password, at least 16 characters and not a source-known default |
| `TLS_CERT_FILE` | certificate path inside the container; Compose sets `/run/tls/tls.crt` |
| `TLS_KEY_FILE` | private-key path inside the container; Compose sets `/run/tls/tls.key` |
| `DEST_HOST` | sender-only rsync receiver hostname |
| `RECEIVER_API_URL` | sender-only HTTPS origin such as `https://receiver:8080` |
| `INTERNAL_API_CA_FILE` | sender-only private CA bundle used to verify the receiver |

Optional sender settings include `BWLIMIT_MBPS`, `MIN_DISK_SPACE_GB`, `POLL_INTERVAL`, notification endpoints, and numbered `SYNC_n_SOURCE`, `SYNC_n_TARGET`, and `SYNC_n_RULE` profiles. Do not put secrets in committed Compose files.

## Receiver API

All filesystem paths are relative to the configured data root. Clients must verify TLS and include the machine token:

```http
Authorization: Bearer <INTERNAL_API_TOKEN>
```

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/health` | `GET` | authenticated receiver status |
| `/api/manifest?path=.` | `GET` | manifest beneath the data root |
| `/api/stat?path=relative/path` | `GET` | inspect a rooted path |
| `/api/delete?path=relative/path&dir=true` | `DELETE` | delete a rooted file or directory |

The delete endpoint never accepts `.` as a target. Symlinks are neither followed nor scanned.

## Upgrade from older deployments

This security release intentionally removes insecure compatibility defaults:

1. Stop both old containers and back up `/config` and receiver data.
2. Create unique admin, rsync, and internal-API credentials; rotate any values previously stored in Compose.
3. Provision TLS certificates and the receiver CA bundle.
4. Make bind mounts accessible to UID/GID 65532. The receiver needs write access; sender media can be read-only.
5. Remove `NET_ADMIN`, `/dev/net/tun`, `TAILSCALE_AUTHKEY`, and related Tailscale variables. Run Tailscale or another VPN on the host or in a separately constrained sidecar.
6. Update integrations to use HTTPS, certificate validation, and the Bearer header. URL tokens and plaintext HTTP are rejected.
7. Start the receiver first, verify `/healthz`, then start the sender and review logs before enabling deletion approval.

The application rewrites its JSON configuration atomically with mode `0600`. Existing configuration is tightened to `0600` when loaded. Rsync credential files exist only on the container's temporary filesystem and are recreated at startup.

## Development

Use Go 1.26.7 or a newer Go 1.26 patch release:

```sh
go mod tidy
go test ./...
go test -race ./...
go vet ./...
```

CI additionally runs golangci-lint, gosec, govulncheck, CodeQL, an immutable container build, and a Trivy image scan. Release and third-party workflow actions are pinned to full commit SHAs.

Report security issues privately as described in [SECURITY.md](SECURITY.md).
