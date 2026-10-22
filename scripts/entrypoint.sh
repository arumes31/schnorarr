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
    sleep 2
    
        HOSTNAME=${TS_HOSTNAME:-"schnorarr-${MODE}"}
    UP_ARGS=${TAILSCALE_UP_ARGS:-""}
    
    echo "Checking Tailscale status..."
    if tailscale status | grep -q "Logged out"; then
        echo "Node not authenticated. Running tailscale up with authkey: $HOSTNAME and args: $UP_ARGS"
        tailscale up --authkey="$TAILSCALE_AUTHKEY" --hostname="$HOSTNAME" $UP_ARGS
    else
        echo "Node already authenticated. Running tailscale up without authkey: $HOSTNAME and args: $UP_ARGS"
        tailscale up --hostname="$HOSTNAME" $UP_ARGS
    fi
fi

if [ "$MODE" = "receiver" ]; then
    echo "Starting rsync daemon (Receiver MODE)..."
    
    # Refuse to run the writable rsync module without a real password.
    RSYNC_USER=${RSYNC_USER:-syncuser}
    if [ -z "$RSYNC_PASSWORD" ] || [ "$RSYNC_PASSWORD" = "secretpassword" ]; then
        echo "Error: RSYNC_PASSWORD must be set to a strong, unique value on the receiver (the old default 'secretpassword' is rejected)."
        exit 1
    fi
    if [ "${#RSYNC_PASSWORD}" -lt 12 ]; then
        echo "WARNING: RSYNC_PASSWORD is shorter than 12 characters; use a longer random value."
    fi

    echo "Configuring authentication for user: $RSYNC_USER"
    echo "$RSYNC_USER:$RSYNC_PASSWORD" > /config/rsyncd.secrets
    chmod 600 /config/rsyncd.secrets

    # Render the daemon config from the template on every start.
    RSYNCD_CONF=/tmp/rsyncd.conf
    sed -e "s|^\( *auth users = \).*|\1$RSYNC_USER|" \
        -e "s|^\( *hosts allow = \).*|\1${RSYNC_HOSTS_ALLOW:-*}|" \
        -e "s|^\( *uid = \).*|\1${RSYNC_UID:-root}|" \
        -e "s|^\( *gid = \).*|\1${RSYNC_GID:-root}|" \
        /scripts/rsyncd.conf > "$RSYNCD_CONF"
    if [ -z "$RSYNC_HOSTS_ALLOW" ]; then
        echo "WARNING: rsync accepts connections from any host. Set RSYNC_HOSTS_ALLOW (e.g. the sender IP or 100.64.0.0/10 for Tailscale) and keep port 873 off untrusted networks; rsync traffic is unencrypted unless it runs over Tailscale or a VPN."
    fi

    # Start monitor in background for health checks and status reporting
    /usr/local/bin/monitor &

    exec rsync --no-detach --daemon --config="$RSYNCD_CONF"
elif [ "$MODE" = "sender" ]; then
    echo "Starting custom sync engine (Sender MODE)..."
    
    # Handle authentication for sender
    if [ -n "$RSYNC_PASSWORD" ]; then
        echo "$RSYNC_PASSWORD" > /config/rsync.pass
        chmod 600 /config/rsync.pass
    else
        echo "WARNING: RSYNC_PASSWORD is not set; rsync transfers and receiver API calls will be rejected."
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

    # Keep the dashboard available for shared-token setup. Each engine waits
    # for its source and receiver token files before starting sync work.

    # Start Monitor with embedded sync engine
    # The monitor binary now includes the custom sync engine
    # It will run the sync logic internally based on SYNC_X environment variables
    exec /usr/local/bin/monitor
else
    echo "Unknown MODE: $MODE. Use 'sender' or 'receiver'."
    exit 1
fi
