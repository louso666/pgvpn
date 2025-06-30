#!/usr/bin/env bash
# Выкатываем локальные изменения статических конфигов обратно на сервера
# ⚠️ Скрипт НЕ запускаем автоматически – только вручную после проверки!
set -euo pipefail
echo "только руками синкаем конфиги!!!"
exit 1
ROOT_DIR=$(dirname "$(readlink -f "$0")")/..
CONF_FILE="$ROOT_DIR/scripts/servers.conf"

while IFS='|' read -r NAME HOST PORT; do
  [[ -z "$NAME" || "$NAME" =~ ^# ]] && continue

  LOCAL_BASE="$ROOT_DIR/$NAME"
  if [[ ! -d $LOCAL_BASE ]]; then
    echo "❌ Каталог $LOCAL_BASE не найден, пропускаю $NAME" >&2
    continue
  fi

  echo "🚀 Пуш на $NAME ($HOST:$PORT) из $LOCAL_BASE"

  # Копируем ВСЁ что есть в локальной папке сервера
  if [[ -d "$LOCAL_BASE/etc" ]]; then
    echo "  • tar ALL $LOCAL_BASE/etc → /etc/"
    (cd "$LOCAL_BASE" && tar cf - etc) | \
      ssh -n -o BatchMode=yes -p "$PORT" "root@$HOST" "cd / && tar xf - --no-same-owner"
  else
    echo "  ⚠️ $LOCAL_BASE/etc отсутствует, нечего пушить"
  fi

  echo "  ↻ Перезагружаем systemd (daemon-reload)"
  ssh -n -o BatchMode=yes -p "$PORT" "root@$HOST" 'systemctl daemon-reload'

  # Можно добавить условный рестарт изменённых юнитов (не реализовано)

done < "$CONF_FILE"

echo "✅ Push complete (но сервисы могли не перезапуститься — проверьте!)" 