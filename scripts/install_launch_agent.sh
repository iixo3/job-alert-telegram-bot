#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLIST="$HOME/Library/LaunchAgents/com.job-alert.plist"
DOMAIN="gui/$(id -u)"

mkdir -p "$PROJECT_DIR/data" "$HOME/Library/LaunchAgents"

if [[ ! -x "$PROJECT_DIR/bin/job-alert" ]]; then
  echo "Missing binary: $PROJECT_DIR/bin/job-alert"
  echo "Run: GOCACHE=\"\$PWD/.gocache\" CGO_ENABLED=0 go build -o bin/job-alert ./cmd/job-alert"
  exit 1
fi

sed "s#__PROJECT_DIR__#$PROJECT_DIR#g" "$PROJECT_DIR/launchd/com.job-alert.plist.template" > "$PLIST"
launchctl bootout "$DOMAIN" "$PLIST" >/dev/null 2>&1 || true
launchctl bootstrap "$DOMAIN" "$PLIST"

echo "Installed LaunchAgent: $PLIST"
echo "Runs on load and daily at 11:00 and 18:00 local time."
