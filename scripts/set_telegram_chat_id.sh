#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: scripts/set_telegram_chat_id.sh <telegram-chat-id>"
  exit 1
fi

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHAT_ID="$1" node -e '
const fs = require("fs");
const path = "config.json";
const cfg = JSON.parse(fs.readFileSync(path, "utf8"));
cfg.telegram_chat_id = process.env.CHAT_ID.trim();
fs.writeFileSync(path, JSON.stringify(cfg, null, 2) + "\n", { mode: 0o600 });
'

echo "Updated config.json telegram_chat_id"
