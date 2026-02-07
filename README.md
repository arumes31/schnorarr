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

## 🛠️ Tech Stack

*   **Backend:** Go (Golang) 1.21+
*   **Database:** SQLite (embedded, zero-conf)
*   **Frontend:** HTML5, CSS3 (Variables), Vanilla JS (No heavy frameworks)
*   **Communication:** WebSockets (Gorilla)
*   **Deployment:** Docker / Docker Compose

## 📦 Installation

### Docker Compose (Recommended)

```yaml
version: '3.8'
services:
  schnorarr:
    image: ghcr.io/arumes31/schnorarr:latest
    container_name: schnorarr
    ports:
      - "8080:8080"
    volumes:
      - ./config:/app/config
      - /mnt/media/source:/source
      - /mnt/media/target:/target
    environment:
      - PUID=1000
      - PGID=1000
    restart: unless-stopped
```

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
