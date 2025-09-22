#!/usr/bin/env bash
set -euo pipefail
. /app/pgvpn/wg.vars.sh

PG_DIR="$ROOT/pg.louso.ru/etc/wireguard"
IPSEC_DIR="$ROOT/ipsec.louso.ru/etc/wireguard"
USA_DIR="$ROOT/usa.louso.ru/etc/wireguard"

req() { [ -f "$1" ] || { echo "Файл не найден: $1"; exit 1; }; }

# требуем ключи
req "$PG_DIR/pg.key";           req "$PG_DIR/pg.key.pub"
req "$IPSEC_DIR/ipsec.key";     req "$IPSEC_DIR/ipsec.key.pub"
req "$USA_DIR/usa.key";         req "$USA_DIR/usa.key.pub"

PG_PRIV=$(cat "$PG_DIR/pg.key")
PG_PUB=$(cat "$PG_DIR/pg.key.pub")
IPSEC_PRIV=$(cat "$IPSEC_DIR/ipsec.key")
IPSEC_PUB=$(cat "$IPSEC_DIR/ipsec.key.pub")
USA_PRIV=$(cat "$USA_DIR/usa.key")
USA_PUB=$(cat "$USA_DIR/usa.key.pub")

# --- pg.louso.ru: два интерфейса wg-quick ---
cat >"$PG_DIR/wg-ipsec.conf" <<EOF
[Interface]
PrivateKey = $PG_PRIV
Address = $PG_IPSEC_PG_ADDR
ListenPort = $PG_IPSEC_PORT
# SaveConfig = true

[Peer]
PublicKey = $IPSEC_PUB
AllowedIPs = ${PG_IPSEC_IPSEC_ADDR%/*}/32
Endpoint = $IPSEC_PUB:$PG_IPSEC_PORT
PersistentKeepalive = 25
EOF

cat >"$PG_DIR/wg-usa.conf" <<EOF
[Interface]
PrivateKey = $PG_PRIV
Address = $PG_USA_PG_ADDR
ListenPort = $PG_USA_PORT
# SaveConfig = true

[Peer]
PublicKey = $USA_PUB
AllowedIPs = ${PG_USA_USA_ADDR%/*}/32   # /32 необязателен, адрес уже точный
Endpoint = $USA_PUB:$PG_USA_PORT
PersistentKeepalive = 25
EOF

# --- ipsec.louso.ru: зеркальный wg-pg ---
cat >"$IPSEC_DIR/wg-pg.conf" <<EOF
[Interface]
PrivateKey = $IPSEC_PRIV
Address = $PG_IPSEC_IPSEC_ADDR

[Peer]
PublicKey = $PG_PUB
AllowedIPs = ${PG_IPSEC_PG_ADDR%/*}/32
Endpoint = $PG_PUB:$PG_IPSEC_PORT
PersistentKeepalive = 25
EOF

# --- usa.louso.ru: зеркальный wg-pg ---
cat >"$USA_DIR/wg-pg.conf" <<EOF
[Interface]
PrivateKey = $USA_PRIV
Address = $PG_USA_USA_ADDR

[Peer]
PublicKey = $PG_PUB
AllowedIPs = ${PG_USA_PG_ADDR%/*}/32
Endpoint = $PG_PUB:$PG_USA_PORT
PersistentKeepalive = 25
EOF

echo "✅ Конфиги сгенерированы:"
printf -- "  - %s\n" \
  "$PG_DIR/wg-ipsec.conf" "$PG_DIR/wg-usa.conf" \
  "$IPSEC_DIR/wg-pg.conf" "$USA_DIR/wg-pg.conf"
