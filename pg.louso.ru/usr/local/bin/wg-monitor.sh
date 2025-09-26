#!/usr/bin/env bash
set -euo pipefail

if [ -f /etc/wg-monitor.env ]; then
  # shellcheck disable=SC1091
  source /etc/wg-monitor.env
fi

TELEGRAM_TOKEN="${TELEGRAM_TOKEN:-}"
TELEGRAM_CHAT_ID="${TELEGRAM_CHAT_ID:-}"
HANDSHAKE_THRESHOLD="${HANDSHAKE_THRESHOLD:-300}"
INTERFACES=("wg-ipsec" "wg-usa")

log(){
  logger --tag wg-monitor "$1"
  echo "$1"
}

notify(){
  local text="$1"
  [ -n "$TELEGRAM_TOKEN" ] || return 0
  [ -n "$TELEGRAM_CHAT_ID" ] || return 0
  curl -sS --max-time 10 \
    -X POST "https://api.telegram.org/bot${TELEGRAM_TOKEN}/sendMessage" \
    -d "chat_id=${TELEGRAM_CHAT_ID}" \
    --data-urlencode "text=${text}" \
    -d "disable_notification=true" >/dev/null || true
}

check_iface(){
  local iface="$1"
  if ! wg show "$iface" >/dev/null 2>&1; then
    log "interface ${iface} missing, restarting"
    systemctl restart "wg-quick@${iface}"
    sleep 3
    if ! wg show "$iface" >/dev/null 2>&1; then
      notify "⚠️ ${iface}: интерфейс не поднялся после рестарта"
      return 1
    fi
  fi

  local stale=1
  while read -r _pub ts; do
    if [ "${ts:-0}" -ne 0 ] && [ $(( $(date +%s) - ts )) -lt "$HANDSHAKE_THRESHOLD" ]; then
      stale=0
      break
    fi
  done < <(wg show "$iface" latest-handshakes)

  if [ "$stale" -eq 0 ]; then
    log "${iface} OK"
    return 0
  fi

  log "${iface} handshake stale → restarting"
  systemctl restart "wg-quick@${iface}"
  sleep 3
  local revived=0
  while read -r _pub ts; do
    if [ "${ts:-0}" -ne 0 ] && [ $(( $(date +%s) - ts )) -lt "$HANDSHAKE_THRESHOLD" ]; then
      revived=1
      break
    fi
  done < <(wg show "$iface" latest-handshakes)

  if [ "$revived" -eq 1 ]; then
    log "${iface} восстановлен"
    notify "✅ ${iface}: туннель восстановлен"
    return 0
  fi

  notify "🚨 ${iface}: нет рукопожатий после рестарта"
  return 1
}

status=0
for iface in "${INTERFACES[@]}"; do
  if ! check_iface "$iface"; then
    status=1
  fi
done

exit "$status"
