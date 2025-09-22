#!/usr/bin/env bash
set -euo pipefail
. /app/pgvpn/wg.vars.sh

push_one() {
  host="$1"; unit="$2"; dir="$ROOT/$host/etc/wireguard"
  echo "🚚 $host: пушу конфиги и ключи"
  rsync -av -e "ssh -p $SSH_PORT" "$dir/" "$SSH_USER@$host:/etc/wireguard/"
  ssh -p "$SSH_PORT" "$SSH_USER@$host" "chmod 600 /etc/wireguard/*.key || true"

  echo "⚙️  $host: enable/start $unit"
  ssh -p "$SSH_PORT" "$SSH_USER@$host" "systemctl enable --now wg-quick@$unit"
  ssh -p "$SSH_PORT" "$SSH_USER@$host" "systemctl status wg-quick@$unit --no-pager -n 0 || true"
}

# pg: два интерфейса
push_one pg.louso.ru  wg-ipsec
push_one pg.louso.ru  wg-usa

# ipsec: зеркальный интерфейс один
push_one ipsec.louso.ru wg-pg

# usa: зеркальный интерфейс один
push_one usa.louso.ru   wg-pg

echo "✅ Пуш и запуск завершены."

# Быстрые проверки
echo "🔎 Проверка wg show:"
ssh -p "$SSH_PORT" "$SSH_USER@pg.louso.ru"     "wg show"
ssh -p "$SSH_PORT" "$SSH_USER@ipsec.louso.ru"  "wg show"
ssh -p "$SSH_PORT" "$SSH_USER@usa.louso.ru"    "wg show" || true
