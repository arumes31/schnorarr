#!/usr/bin/env bash
set -euo pipefail

echo "Running pre-push checks..."

if command -v golangci-lint >/dev/null 2>&1; then
    echo "==> Running golangci-lint..."
    golangci-lint run
else
    echo "==> golangci-lint not found in PATH, skipping local lint."
fi

if command -v go >/dev/null 2>&1; then
    echo "==> Running go test..."
    go test ./...
else
    echo "==> go not found in PATH, skipping local go test."
fi

echo "All available pre-push checks passed!"
