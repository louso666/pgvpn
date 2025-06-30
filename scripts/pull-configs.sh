#!/usr/bin/env bash
# Синхронизация статических конфигов С СЕРВЕРОВ → LOCALLY
# Идея: проходимся по servers.conf и забираем набор файлов/директорий
# Автор: o3-assistant, июнь 2025
set -euo pipefail

ROOT_DIR=$(dirname "$(readlink -f "$0")")/..   # корень репо
CONF_FILE="$ROOT_DIR/scripts/servers.conf"

if [[ ! -f $CONF_FILE ]]; then
  echo "servers.conf not found: $CONF_FILE" >&2
  exit 1
fi

# Что тянем — относительные к «/» пути на удалённой стороне
SYNC_PATHS=(
  "/etc/iptables/"
  # конкретные unit-файлы, относящиеся к туннелям
  "/etc/systemd/system/gre-p2p.service"
  "/etc/systemd/system/gre-keepalive.timer"
  "/etc/systemd/system/gre-keepalive.service"
  "/etc/systemd/system/proxy-route.service"
  "/etc/systemd/system/tun10.service"
  "/etc/systemd/system/tun10_watchdog.service"
)

while IFS='|' read -r NAME HOST PORT; do
  [[ -z "$NAME" || "$NAME" =~ ^# ]] && continue  # пропускаем пустые/коммент

  LOCAL_BASE="$ROOT_DIR/$NAME"
  mkdir -p "$LOCAL_BASE"

  echo "🔄 Синхронизация $NAME ($HOST:$PORT) → $LOCAL_BASE"

  for REMOTE_PATH in "${SYNC_PATHS[@]}"; do
    # проверяем существует ли файл/директория на удалённой стороне
    if ! ssh -n -o BatchMode=yes -p "$PORT" "root@$HOST" test -e "$REMOTE_PATH"; then
      echo "  ⚠️  $REMOTE_PATH отсутствует — пропускаю"
      continue
    fi

    REL_PATH="${REMOTE_PATH#/}"              # обрезаем ведущий слэш
    DEST_PARENT="$LOCAL_BASE/$(dirname "$REL_PATH")"
    mkdir -p "$DEST_PARENT"

    if ssh -n -o BatchMode=yes -p "$PORT" "root@$HOST" test -d "$REMOTE_PATH"; then
      # директория - используем tar
      echo "  • tar DIR $REMOTE_PATH → $LOCAL_BASE/$REL_PATH/"
      ssh -n -o BatchMode=yes -p "$PORT" "root@$HOST" "cd / && tar cf - --exclude='*.backup' --exclude='*.applied' --exclude='*.current' --exclude='*.pre_*' --exclude='wireguard' --exclude='ipset.conf' '$REL_PATH'" | \
        (cd "$LOCAL_BASE" && tar xf -)
    else
      # одиночный файл - используем scp
      echo "  • scp FILE $REMOTE_PATH → $DEST_PARENT/"
      scp -q -o BatchMode=yes -P "$PORT" "root@$HOST:$REMOTE_PATH" "$DEST_PARENT/"
    fi
  done

done < "$CONF_FILE"

echo "✅ Pull complete" 