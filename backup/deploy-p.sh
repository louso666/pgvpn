#!/bin/bash

# Деплой на p.nirhub.ru
# Идемпотентный скрипт применения конфигураций

set -e

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[$(date '+%H:%M:%S')]${NC} $*"
}

warn() {
    echo -e "${YELLOW}[$(date '+%H:%M:%S')] ⚠️${NC} $*"
}

error() {
    echo -e "${RED}[$(date '+%H:%M:%S')] ❌${NC} $*"
}

SERVER="p.nirhub.ru"
PORT="32322"

log "🚀 Деплой конфигураций на $SERVER..."

# 1. Синхронизация файлов с сохранением прав
log "📁 Синхронизация файлов..."
rsync -avz --progress --no-o --no-g -e "ssh -p $PORT" --exclude='/root/.ssh' --exclude='/etc/ssh' p.nirhub.ru/ root@$SERVER:/

# 2. Применение конфигураций
log "🔧 Применение конфигураций..."
ssh root@$SERVER -p $PORT "
    # Применяем iptables правила
    iptables-restore < /etc/iptables/rules.v4
    ip6tables-restore < /etc/iptables/rules.v6
    
    # Сохраняем правила
    iptables-save > /etc/iptables/rules.v4.applied
    ip6tables-save > /etc/iptables/rules.v6.applied
    
    # Включаем IP forwarding
    echo 1 > /proc/sys/net/ipv4/ip_forward
    echo 'net.ipv4.ip_forward = 1' >> /etc/sysctl.conf
    
    # Устанавливаем права на скрипты
    find /etc/scripts -name '*.sh' -exec chmod +x {} \; 2>/dev/null || true
    
    systemctl daemon-reload
    
    echo '✅ $SERVER: конфигурации применены'
"

# 3. Проверка сервисов
log "🔍 Проверка сервисов на $SERVER..."
ssh root@$SERVER -p $PORT "
    echo '=== Статус WireGuard wg200 ==='
    if systemctl is-active --quiet wg-quick@wg200; then
        echo '✅ wg-quick@wg200 активен'
        wg show wg200 | head -5
    else
        echo '⚠️ wg-quick@wg200 не активен'
    fi
    
    echo ''
    echo '=== FORWARD правила для wg200 ==='
    iptables -L FORWARD | grep -E 'wg200|eth0.*wg200' | head -5
    
    echo ''
    echo '=== NAT правила для wg200 ==='
    iptables -t nat -L POSTROUTING | grep -E '10\.200\.0'
    
    echo ''
    echo '=== IP Forwarding ==='
    cat /proc/sys/net/ipv4/ip_forward
"

log "✅ Деплой $SERVER завершен успешно!" 