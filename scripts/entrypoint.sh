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
    
    tailscaled --state=/config/tailscale.state &
    sleep 2
    
    HOSTNAME=${TS_HOSTNAME:-"schnorarr-${MODE}"}
    UP_ARGS=${TAILSCALE_UP_ARGS:-""}
    
    echo "Tailscale up with hostname: $HOSTNAME and args: $UP_ARGS"
    tailscale up --authkey="$TAILSCALE_AUTHKEY" --hostname="$HOSTNAME" $UP_ARGS
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
    AVAILABLE_SPACE_GB=$(df -BG /data | awk 'NR==2 {print $4}' | sed 's/G//')
    if [ "$AVAILABLE_SPACE_GB" -lt "$MIN_DISK_SPACE_GB" ]; then
        echo "Error: Not enough disk space. Available: ${AVAILABLE_SPACE_GB}GB, Required: ${MIN_DISK_SPACE_GB}GB"
        exit 1
    fi

    # Wait for receiver to be ready
    MAX_RETRIES=${MAX_RETRIES:-30}
    COUNT=0
    while ! nc -z "$DEST_HOST" 873; do
        COUNT=$((COUNT+1))
        if [ $COUNT -ge "$MAX_RETRIES" ]; then
            echo "Error: Receiver $DEST_HOST not ready after $MAX_RETRIES attempts. Exiting."
            # Notification handled by monitor (system unhealthy) if it was running, but here we exit before monitor might catch it?
            # Actually monitor is running in background.
            exit 1
        fi
        echo "Receiver not ready ($COUNT/$MAX_RETRIES), sleeping..."
        sleep 2
    done

    # Password for rsync
    if [ -n "$RSYNC_PASSWORD" ]; then
        echo "$RSYNC_PASSWORD" > /config/rsync.pass
        chmod 600 /config/rsync.pass
    fi

    # Start Monitor with embedded sync engine
    # The monitor binary now includes the custom sync engine
    # It will run the sync logic internally based on SYNC_X environment variables
    exec /usr/local/bin/monitor
else
    echo "Unknown MODE: $MODE. Use 'sender' or 'receiver'."
    exit 1
fi
