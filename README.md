<div align="center">

# ⚡ Schnorarr

**The Ultra-Fast, Cyberpunk-Styled Sync Monitor**

[![Go Report Card](https://goreportcard.com/badge/github.com/arumes31/schnorarr)](https://goreportcard.com/report/github.com/arumes31/schnorarr)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Docker Image](https://github.com/arumes31/schnorarr/actions/workflows/docker.yml/badge.svg)](https://github.com/arumes31/schnorarr/pkgs/container/schnorarr)

</div>

**Schnorarr** is a high-performance, real-time file synchronization monitor and orchestrator designed for media servers. It visualizes file transfers, manages conflicts, and ensures your media libraries stay in perfect sync across multiple servers.

## 🚀 Features

*   **Real-Time Dashboard:** Live WebSocket-powered updates for transfer speeds, ETA, and file progress.
*   **Visual Transfer Graphs:** Beautiful Sparkline charts and "Node Map" visualizations.
*   **Multi-Engine Support:** Monitor and control multiple sync pairs (Sender -> Receiver) simultaneously.
*   **Smart Conflict Resolution:** Auto-detects and handles file conflicts with "Dry Run" previews.
*   **Cyberpunk Aesthetics:** Fully themed UI with 5 distinct color palettes (Cyber Green, Plasma Purple, Nuclear Orange, Crimson Red, Midnight Blue).
*   **Log Terminal:** Integrated web-based terminal for viewing real-time system logs with filtering.
*   **Discord Notifications:** Get alerted on sync completion or critical errors.
*   **Built-in Mesh VPN:** Optional Tailscale integration for secure, zero-config cross-network synchronization.

## 🛠️ Tech Stack

*   **Backend:** Go (Golang) 1.21+
*   **Database:** SQLite (embedded, zero-conf)
*   **Frontend:** HTML5, CSS3 (Variables), Vanilla JS (No heavy frameworks)
*   **Communication:** WebSockets (Gorilla)
*   **Deployment:** Docker / Docker Compose

## 📦 Installation

### Docker Compose (Recommended)

Schnorarr can run in two modes: **Sender** (the orchestrator that monitors files and pushes them) and **Receiver** (the destination agent).

#### Sender Configuration
The Sender monitors local directories and orchestrates the sync process to a Receiver.

```yaml
version: '3.8'
services:
  schnorarr-sender:
    image: ghcr.io/arumes31/schnorarr:latest
    container_name: schnorarr-sender
    ports:
      - "8080:8080"
    volumes:
      - ./config:/config
      - /mnt/media/movies:/source/movies
    environment:
      - MODE=sender
      - DEST_HOST=receiver-ip-or-hostname
      - DEST_MODULE=media
      - SYNC_1_SOURCE=/source/movies
      - SYNC_1_TARGET=media/movies
      - SYNC_1_RULE=series
      - BWLIMIT_MBPS=100
      - DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
      # - TAILSCALE_AUTHKEY=tskey-auth-xxxx
      # - TAILSCALE_UP_ARGS=--accept-dns=false
    restart: unless-stopped
```

#### Receiver Configuration
The Receiver acts as a passive target for the Sender.

```yaml
version: '3.8'
services:
  schnorarr-receiver:
    image: ghcr.io/arumes31/schnorarr:latest
    container_name: schnorarr-receiver
    ports:
      - "8080:8080"
    environment:
      - MODE=receiver
      # - TAILSCALE_AUTHKEY=tskey-auth-xxxx
      # - TAILSCALE_UP_ARGS=--accept-routes
    volumes:
      - /mnt/storage/media:/media
    restart: unless-stopped
```

## ⚙️ Configuration (Environment Variables)

### General
| Variable | Description | Default |
| :--- | :--- | :--- |
| `MODE` | `sender` or `receiver` | `sender` |
| `PORT` | Web UI / API Port | `8080` |
| `PUID` / `PGID` | User/Group ID for file permissions | `1000` |
| `TAILSCALE_AUTHKEY` | Optional: Tailscale Auth Key for built-in mesh VPN | - |
| `TAILSCALE_UP_ARGS` | Optional: Extra arguments for `tailscale up` | - |

### Sender Specific
| Variable | Description | Example |
| :--- | :--- | :--- |
| `DEST_HOST` | Hostname or IP of the Receiver | `192.168.1.50` |
| `DEST_MODULE` | Rsync module name on Receiver | `media` |
| `BWLIMIT_MBPS` | Global bandwidth limit in Mbps | `50` |
| `SYNC_N_SOURCE` | Source path for engine `N` (1-10) | `/source/movies` |
| `SYNC_N_TARGET` | Target path for engine `N` (1-10) | `media/movies` |
| `SYNC_N_RULE` | Sync rule (`standard`, `series`, `flat`) | `series` |
| `DISCORD_WEBHOOK_URL` | Discord webhook for notifications | `https://...` |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token | `123456:ABC...` |
| `TELEGRAM_CHAT_ID` | Telegram chat ID | `987654321` |

### Receiver Specific
| Variable | Description | Default |
| :--- | :--- | :--- |
| `RSYNC_CONFIG` | Custom path to rsyncd.conf | `/etc/rsyncd.conf` |

### Manual Build

```bash
git clone https://github.com/arumes31/schnorarr.git
cd schnorarr
go build -o schnorarr ./cmd/monitor
./schnorarr
```

## 🖼️ Screenshots

<div align="center">
  <img src="https://via.placeholder.com/800x450/0a0b10/00ffad?text=Dashboard+Preview" alt="Dashboard" width="800"/>
</div>

## 📜 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
