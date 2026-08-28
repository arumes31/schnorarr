#!/usr/bin/env bash
set -uo pipefail

readonly log_file=/tmp/rsync.log
readonly progress_file=/tmp/current_sync.tmp
readonly rsync_bin=/usr/bin/rsync.real
trap 'rm -f "$progress_file"' EXIT

"$rsync_bin" "$@" 2>&1 | while IFS= read -r line; do
    if [[ "$line" == *"%"* ]]; then
        printf '%s\n' "$line" > "$progress_file"
        continue
    fi
    if [[ "$line" == *"rsync error:"* || "$line" == *"failed:"* || "$line" == *"IO error"* ]]; then
        printf '%s [ERROR] %s\n' "$(date '+%Y/%m/%d %H:%M:%S')" "$line" >> "$log_file"
    fi
    if [[ "$line" == *">f"* || "$line" == *"<f"* || "$line" == *"*deleting"* || "$line" == *".mkv"* || "$line" == *".mp4"* || "$line" == *".avi"* ]]; then
        printf '%s [WRAPPER] %s\n' "$(date '+%Y/%m/%d %H:%M:%S')" "$line" >> "$log_file"
    fi
    printf '%s\n' "$line"
done

status=${PIPESTATUS[0]}
if (( status != 0 )); then
    printf '%s [ERROR] rsync exited with code %d\n' "$(date '+%Y/%m/%d %H:%M:%S')" "$status" >> "$log_file"
fi
exit "$status"
