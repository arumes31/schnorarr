#!/bin/bash
set -e

# Handle Tailscale if AUTHKEY is provided
if [ -n "$TAILSCALE_AUTHKEY" ]; then
    echo "Starting Tailscale..."
    # Ensure /dev/net/tun exists
    if [ ! -c /dev/net/tun ]; then
        mkdir -p /dev/net/
        mknod /dev/net/tun c 10 200
    fi
    
    mkdir -p /config/tailscale
    tailscaled --state=/config/tailscale/tailscaled.state &
    
    echo "Waiting for Tailscale daemon to start..."
    for i in {1..15}; do
        # If it doesn't say "failed to connect", the daemon is up (even if logged out)
        if ! tailscale status 2>&1 | grep -q "failed to connect to local tailscaled"; then
            break
        fi
        sleep 1
    done
    
    HOSTNAME=${TS_HOSTNAME:-"schnorarr-${MODE}"}
    UP_ARGS=${TAILSCALE_UP_ARGS:-""}
    
    echo "Checking Tailscale authentication status..."
    STATUS=$(tailscale status 2>&1 || true)
    
    # Check if the node is logged out
    if echo "$STATUS" | grep -q "Logged out"; then
        echo "Node not authenticated. Running tailscale up with authkey: $HOSTNAME"
        tailscale up --authkey="$TAILSCALE_AUTHKEY" --hostname="$HOSTNAME" $UP_ARGS
    else
        echo "Node appears authenticated. Running tailscale up without authkey: $HOSTNAME"
        # Only run tailscale up if we need to apply new UP_ARGS, or we just let the daemon run.
        # But we run it to ensure the hostname and routes are applied.
        tailscale up --hostname="$HOSTNAME" $UP_ARGS
    fi
fi

if [ "$MODE" = "receiver" ]; then
    echo "Starting rsync daemon (Receiver MODE)..."
    
    # Handle authentication
    if [ -n "$RSYNC_USER" ] && [ -n "$RSYNC_PASSWORD" ]; then
        echo "Configuring authentication for user: $RSYNC_USER"
        echo "$RSYNC_USER:$RSYNC_PASSWORD" > /config/rsyncd.secrets
        chmod 600 /config/rsyncd.secrets
        sed -i "s/auth users = .*/auth users = $RSYNC_USER/" /scripts/rsyncd.conf
        sed -i "/auth users/a \    secrets file = /config/rsyncd.secrets" /scripts/rsyncd.conf
    fi

    # Start monitor in background for health checks and status reporting
    /usr/local/bin/monitor &

    exec rsync --no-detach --daemon --config=/scripts/rsyncd.conf
elif [ "$MODE" = "sender" ]; then
    echo "Starting custom sync engine (Sender MODE)..."
    
    # Handle authentication for sender
    if [ -n "$RSYNC_PASSWORD" ]; then
        echo "$RSYNC_PASSWORD" > /config/rsync.pass
        chmod 600 /config/rsync.pass
    fi

    # Monitor will be started at the end with exec

    # Disk space check
    MIN_DISK_SPACE_GB=${MIN_DISK_SPACE_GB:-0}
    # BusyBox df doesn't support -BG, use -k and convert
    AVAILABLE_SPACE_KB=$(df -k /data | awk 'NR==2 {print $4}')
    AVAILABLE_SPACE_GB=$((AVAILABLE_SPACE_KB / 1024 / 1024))
    
    if [ "$AVAILABLE_SPACE_GB" -lt "$MIN_DISK_SPACE_GB" ]; then
        echo "Error: Not enough disk space. Available: ${AVAILABLE_SPACE_GB}GB, Required: ${MIN_DISK_SPACE_GB}GB"
        exit 1
    fi

    # Wait for receiver to be ready
    echo "Waiting for receiver $DEST_HOST:873..."
    while ! nc -z "$DEST_HOST" 873; do
        echo "Receiver not ready, sleeping 5s..."
        sleep 5
    done

    # Start Monitor with embedded sync engine
    # The monitor binary now includes the custom sync engine
    # It will run the sync logic internally based on SYNC_X environment variables
    exec /usr/local/bin/monitor
else
    echo "Unknown MODE: $MODE. Use 'sender' or 'receiver'."
    exit 1
fi