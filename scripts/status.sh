#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LABEL="com.job-alert"
DOMAIN="gui/$(id -u)"
STATE_FILE="$PROJECT_DIR/data/seen_jobs.json"

echo "Job alert status"
echo "Project: $PROJECT_DIR"

if service_status="$(launchctl print "$DOMAIN/$LABEL" 2>&1)"; then
  printf '%s\n' "$service_status" | awk '
    /state =/ || /runs =/ || /last exit code =/ { sub(/^[[:space:]]+/, ""); print }
  '
else
  echo "launchd: not loaded"
fi

if [[ -f "$STATE_FILE" ]]; then
  jq -r '
    def timestamp_or_never:
      if . == null or startswith("0001-") then "never" else . end;
    .health // {} |
    "last attempt: " + (.last_attempt_at | timestamp_or_never),
    "last success: " + (.last_success_at | timestamp_or_never),
    "last heartbeat: " + (.last_heartbeat_at | timestamp_or_never),
    "last failure: " + (.last_failure_at | timestamp_or_never),
    "last error: " + (.last_error // "none"),
    "active incident: " + (if .incident == null then "none" else .incident.started_at end),
    "incident failed scans: " + (if .incident == null then "0" else (.incident.attempts | length | tostring) end),
    "morning exhausted: " + (if .incident == null then "no" else (.incident.morning_exhausted_date // "no") end),
    "daily summary sent: " + (if .incident == null then "no" else (.incident.summary_date // "no") end)
  ' "$STATE_FILE"
else
  echo "state: missing"
fi

echo "latest log output:"
tail -n 5 "$PROJECT_DIR/data/launchd.out.log" 2>/dev/null || true
tail -n 5 "$PROJECT_DIR/data/launchd.err.log" 2>/dev/null || true
