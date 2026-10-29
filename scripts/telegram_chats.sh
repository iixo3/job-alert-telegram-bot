#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -x "$PROJECT_DIR/bin/job-alert" ]]; then
  echo "Missing binary: $PROJECT_DIR/bin/job-alert"
  echo "Run: GOCACHE=\"\$PWD/.gocache\" CGO_ENABLED=0 go build -o bin/job-alert ./cmd/job-alert"
  exit 1
fi

"$PROJECT_DIR/bin/job-alert" telegram-chats --config "$PROJECT_DIR/config.json"
