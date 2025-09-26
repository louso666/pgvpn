#!/usr/bin/env bash
set -euo pipefail

# ====== настройки ======
BIN="dnsproxy"
SERVICE="dnsproxy.service"

TARGET_USER="root"
TARGET_HOST="89.253.219.146"
TARGET_PORT="22022"

REMOTE_BIN_DIR="/usr/local/bin"
REMOTE_SERVICE_DIR="/etc/systemd/system"

SSH_OPTS="-p ${TARGET_PORT} -o BatchMode=yes -o StrictHostKeyChecking=accept-new"
RSYNC_SSH="ssh ${SSH_OPTS}"

# ====== рабочая директория скрипта ======
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

have() { command -v "$1" >/dev/null 2>&1; } 

echo "🔨 Собираю компактный бинарь для linux/amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o "$BIN" .
have strip && strip --strip-unneeded "$BIN" || true
if have upx; then upx --best --lzma "$BIN" || true; fi

REMOTE="${TARGET_USER}@${TARGET_HOST}"

echo "📤 Копирую бинарь на ${REMOTE}:${REMOTE_BIN_DIR}/${BIN}.new ..."
rsync -e "${RSYNC_SSH}" -v --info=progress2 "$BIN"  "${REMOTE}:${REMOTE_BIN_DIR}/${BIN}.new"

if [ -f "$SERVICE" ]; then
  echo "📤 Копирую unit в ${REMOTE_SERVICE_DIR}/ ..."
  rsync -e "${RSYNC_SSH}" -v --info=progress2 "$SERVICE" "${REMOTE}:${REMOTE_SERVICE_DIR}/"
fi

echo "🔄 Атомарная замена и рестарт..."
ssh ${SSH_OPTS} "${REMOTE}" "set -e;
  mv '${REMOTE_BIN_DIR}/${BIN}.new' '${REMOTE_BIN_DIR}/${BIN}';
  systemctl daemon-reload;
  systemctl enable --now '${SERVICE}';
"

echo "🔧 Проверяю ipset..."
ssh ${SSH_OPTS} "${REMOTE}" "ipset list nl_proxy >/dev/null 2>&1 || ipset create nl_proxy hash:ip"
ssh ${SSH_OPTS} "${REMOTE}" "ipset list usa_proxy >/dev/null 2>&1 || ipset create usa_proxy hash:ip"

echo "📊 Статус сервиса:"
ssh ${SSH_OPTS} "${REMOTE}" "systemctl --no-pager status '${SERVICE}' | sed -n '1,50p'"

echo "✅ Done"
