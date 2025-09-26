#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="/app/pgvpn"
SCRIPT="$REPO_ROOT/scripts/sync-mikrotik-address-lists.sh"
CONFIG="/etc/pgvpn/mikrotik-sync.conf"

if [[ ! -x "$SCRIPT" ]]; then
  echo "sync script not found: $SCRIPT" >&2
  exit 1
fi

export CONFIG_FILE="$CONFIG"
exec "$SCRIPT" "$@"
