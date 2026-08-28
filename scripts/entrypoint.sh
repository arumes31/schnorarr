#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

required=(MODE RSYNC_USER RSYNC_PASSWORD INTERNAL_API_TOKEN ADMIN_USER ADMIN_PASS TLS_CERT_FILE TLS_KEY_FILE)
for name in "${required[@]}"; do
    if [[ -z "${!name:-}" ]]; then
        echo "Error: $name is required" >&2
        exit 1
    fi
done

if [[ "$MODE" != "sender" && "$MODE" != "receiver" ]]; then
    echo "Error: MODE must be sender or receiver" >&2
    exit 1
fi
if [[ ! "$RSYNC_USER" =~ ^[A-Za-z0-9_.-]+$ ]]; then
    echo "Error: RSYNC_USER contains unsupported characters" >&2
    exit 1
fi
if (( ${#RSYNC_PASSWORD} < 16 )) || [[ "$RSYNC_PASSWORD" == *$'\n'* || "$RSYNC_PASSWORD" == *$'\r'* ]]; then
    echo "Error: RSYNC_PASSWORD must be at least 16 characters without line breaks" >&2
    exit 1
fi
if (( ${#INTERNAL_API_TOKEN} < 32 )); then
    echo "Error: INTERNAL_API_TOKEN must be at least 32 characters" >&2
    exit 1
fi
if [[ ! -r "$TLS_CERT_FILE" || ! -r "$TLS_KEY_FILE" ]]; then
    echo "Error: TLS certificate and key must be readable" >&2
    exit 1
fi

if [[ "$MODE" == "receiver" ]]; then
	printf '%s:%s\n' "$RSYNC_USER" "$RSYNC_PASSWORD" > /tmp/rsyncd.secrets
	chmod 0600 /tmp/rsyncd.secrets
	cp /scripts/rsyncd.conf /tmp/rsyncd.conf
	sed -i "s/^    auth users = .*/    auth users = $RSYNC_USER/" /tmp/rsyncd.conf

	echo "Starting receiver monitor and rsync daemon"
	/usr/local/bin/monitor &
	monitor_pid=$!
	/usr/bin/rsync.real --no-detach --daemon --config=/tmp/rsyncd.conf &
	rsync_pid=$!
	shutdown_receiver() {
		trap - INT TERM
		kill "$monitor_pid" "$rsync_pid" 2>/dev/null || true
		wait "$monitor_pid" "$rsync_pid" 2>/dev/null || true
	}
	trap shutdown_receiver INT TERM
	set +e
	wait -n "$monitor_pid" "$rsync_pid"
	status=$?
	set -e
	shutdown_receiver
	exit "$status"
fi

if [[ -z "${DEST_HOST:-}" ]]; then
    echo "Error: DEST_HOST is required in sender mode" >&2
    exit 1
fi

minimum_gb=${MIN_DISK_SPACE_GB:-0}
if [[ ! "$minimum_gb" =~ ^[0-9]+$ ]]; then
	echo "Error: MIN_DISK_SPACE_GB must be a non-negative integer" >&2
	exit 1
fi
available_kb=$(df -k /data | awk 'NR==2 {print $4}')
available_gb=$((available_kb / 1024 / 1024))
if (( available_gb < minimum_gb )); then
    echo "Error: not enough disk space; available ${available_gb}GB, required ${minimum_gb}GB" >&2
    exit 1
fi

echo "Waiting for receiver ${DEST_HOST}:873"
until nc -z "$DEST_HOST" 873; do
    sleep 5
done

exec /usr/local/bin/monitor
