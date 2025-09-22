#!/usr/bin/env bash
set -euo pipefail
. /app/pgvpn/wg.vars.sh

mkdir -p "$ROOT/pg.louso.ru/etc/wireguard" \
         "$ROOT/ipsec.louso.ru/etc/wireguard" \
         "$ROOT/usa.louso.ru/etc/wireguard"

# host -> имя файла ключа на целевом сервере
declare -A KEYNAME=(
  ["pg.louso.ru"]="pg"
  ["ipsec.louso.ru"]="ipsec"
  ["usa.louso.ru"]="usa"
)

for host in pg.louso.ru ipsec.louso.ru usa.louso.ru; do
  k="${KEYNAME[$host]}"
  dest="$ROOT/$host/etc/wireguard"
  echo "🔑 Тяну ключи с $host → $dest"
  scp -P "$SSH_PORT" -o StrictHostKeyChecking=no \
    "$SSH_USER@$host:/etc/wireguard/$k.key"     "$dest/$k.key"
  scp -P "$SSH_PORT" -o StrictHostKeyChecking=no \
    "$SSH_USER@$host:/etc/wireguard/$k.key.pub" "$dest/$k.key.pub"
  chmod 600 "$dest/$k.key"
done

# на всякий случай защитим от коммита приватные ключи
if [ -d "$ROOT/.git" ]; then
  grep -q '^**/etc/wireguard/*.key$' "$ROOT/.gitignore" 2>/dev/null || \
    echo '**/etc/wireguard/*.key' >> "$ROOT/.gitignore"
fi

echo "✅ Ключи собраны."
