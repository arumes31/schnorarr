#!/bin/sh
# Rsync checks the registered engine folder before each transfer.
exec timeout 8 /usr/local/bin/monitor --check-storage
