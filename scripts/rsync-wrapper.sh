#!/bin/bash
# Rsync wrapper to capture output for the dashboard
LOG_FILE="/tmp/rsync.log"

# Log the command being run (optional, for debugging)
for arg in "$@"; do
    echo "$(date) [WRAPPER_ARG] $arg" >> "$LOG_FILE"
done

# Run the real rsync and capture its output to the log file while also showing it to stdout
RSYNC_BIN="/usr/bin/rsync.real"

# Use a pipe but capture the exit code of the first command (rsync)
# We use a temporary file for the exit code because variable scoping in pipes is tricky in bash
EXIT_CODE_FILE="/tmp/rsync_exit_code_$$"

# PIPESTATUS approach or just running it and ensuring we catch the log
# Standard bash pipe: command1 | command2
# The exit status of the pipeline is the exit status of the last command.
# We need `set -o pipefail` to get the error from rsync.
set -o pipefail

"$RSYNC_BIN" "$@" 2>&1 | while read line; do
    # DEBUG: Log everything to a separate file (optional, can be disabled for prod)
    # echo "$(date '+%Y/%m/%d %H:%M:%S') [DEBUG] $line" >> "/tmp/wrapper_debug.log"

    # Capture Progress Lines
    if [[ "$line" == *"%"* ]]; then
        echo "$line" > "/tmp/current_sync.tmp"
        continue
    fi

    # Explicit Error Catching from Rsync Output
    if [[ "$line" == *"rsync error:"* ]] || [[ "$line" == *"failed:"* ]] || [[ "$line" == *"IO error"* ]]; then
        echo "$(date '+%Y/%m/%d %H:%M:%S') [ERROR] $line" >> "$LOG_FILE"
    fi

    # Only log lines that look like file transfers or deletions
    if [[ "$line" == *">f"* ]] || [[ "$line" == *"<f"* ]] || [[ "$line" == *"*deleting"* ]] || [[ "$line" == *".mkv"* ]] || [[ "$line" == *".mp4"* ]] || [[ "$line" == *".avi"* ]]; then
        echo "$(date '+%Y/%m/%d %H:%M:%S') [WRAPPER] $line" >> "$LOG_FILE"
    fi
    
    echo "$line"
done

# Check exit code of the pipeline
msg_code=$?
if [ $msg_code -ne 0 ]; then
    echo "$(date '+%Y/%m/%d %H:%M:%S') [ERROR] Rsync process exited with error code $msg_code" >> "$LOG_FILE"
fi

# Clean up progress file when rsync finishes
rm -f "/tmp/current_sync.tmp"

