#!/usr/bin/env bash
set -euo pipefail

BIN=dnspoxy                # имя бинаря после билда
SERVICE=dnspoxy.service    # имя unit‑файла
TARGET=root@176.114.88.142   # куда катим
REMOTE_BIN_DIR=/usr/local/bin
REMOTE_SERVICE_DIR=/etc/systemd/system

# 1. билдим под linux/amd64 с флагами для уменьшения размера
echo "🔨 Собираем компактный бинарь для linux/amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o "$BIN" *.go 
strip --strip-unneeded "$BIN"
upx --best --lzma "$BIN"
# 2. шлём бинарь с временным именем (zero downtime deployment)
echo "📤 Копируем новый бинарь на $TARGET..."
rsync -v --info=progress "$BIN"  "$TARGET:$REMOTE_BIN_DIR/$BIN.new"
rsync -v --info=progress "$SERVICE" "$TARGET:$REMOTE_SERVICE_DIR/"
rm "$BIN"

# 3. атомарно заменяем бинарь и быстро перезапускаем
echo "🔄 Атомарная замена бинаря и быстрый перезапуск сервиса..."
ssh "$TARGET" "mv $REMOTE_BIN_DIR/$BIN.new $REMOTE_BIN_DIR/$BIN && systemctl daemon-reload && systemctl restart $SERVICE"

# 4. на всякий пожарный создаём ipset если его нет
echo "🔧 Проверяем ipset..."
ssh "$TARGET" "ipset list proxied >/dev/null 2>&1 || ipset create proxied hash:ip"

# 5. проверяем статус
echo "📊 Проверяем статус сервиса..."
ssh "$TARGET" "systemctl status $SERVICE --no-pager"

echo ""
echo "✅ Done. $SERVICE успешно перезапущен на $TARGET (zero downtime deployment)"