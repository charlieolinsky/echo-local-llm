#!/usr/bin/env bash
# Allow bin/local-llm through the macOS Application Firewall (LAN access).
# Requires your admin password once.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/bin/local-llm"

if [[ ! -x "$BIN" ]]; then
  echo "Missing $BIN — run: make build" >&2
  exit 1
fi

codesign -s - --force "$BIN" >/dev/null 2>&1 || true

FW=/usr/libexec/ApplicationFirewall/socketfilterfw
sudo "$FW" --add "$BIN"
sudo "$FW" --unblockapp "$BIN"

echo "Allowed incoming connections for: $BIN"
echo "Restart the gateway if it is already running: make run"
echo "Then test from this Mac: curl http://$(ipconfig getifaddr en1 2>/dev/null || ipconfig getifaddr en0):4000/health"
