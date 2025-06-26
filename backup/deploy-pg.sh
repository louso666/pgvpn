#!/bin/bash

# Деплой на pg.gena.host
# Идемпотентный скрипт установки и настройки

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

SERVER="pg.gena.host"

log "🚀 Деплой конфигураций на $SERVER..."

# 1. Синхронизация файлов с сохранением прав
log "📁 Синхронизация файлов..."
rsync -avz --progress --no-o --no-g --exclude='/root/.ssh' --exclude='/etc/ssh' pg.gena.host/ root@$SERVER:/

# 2. Установка пакетов и применение конфигураций
log "🔧 Установка пакетов и применение конфигураций..."
ssh root@$SERVER "
    # Запускаем скрипт установки пакетов
    chmod +x /etc/scripts/install-packages.sh
    /etc/scripts/install-packages.sh
    
    # Применяем iptables правила
    iptables-restore < /etc/iptables/rules.v4
    ip6tables-restore < /etc/iptables/rules.v6
    
    # Сохраняем правила
    iptables-save > /etc/iptables/rules.v4.applied
    ip6tables-save > /etc/iptables/rules.v6.applied
    
    # Включаем автозагрузку правил
    systemctl enable netfilter-persistent
    
    # Устанавливаем права на скрипты
    find /etc/scripts -name '*.sh' -exec chmod +x {} \; 2>/dev/null || true
    chmod +x /root/wg 2>/dev/null || true
    
    systemctl daemon-reload
    
    # Включаем сервисы
    systemctl enable wg-quick@wg200
    systemctl enable persistent-routes.service
    systemctl start persistent-routes.service
    
    # Включаем nginx
    systemctl enable nginx
    
    echo '✅ $SERVER: конфигурации применены'
"

# 3. Проверка сервисов
log "🔍 Проверка сервисов на $SERVER..."
ssh root@$SERVER "
    echo '=== Статус WireGuard wg200 ==='
    if systemctl is-active --quiet wg-quick@wg200; then
        echo '✅ wg-quick@wg200 активен'
        wg show wg200 | head -5
    else
        echo '⚠️ wg-quick@wg200 не активен'
    fi
    
    echo ''
    echo '=== Постоянные маршруты ==='
    systemctl is-active --quiet persistent-routes.service && echo '✅ persistent-routes активен' || echo '⚠️ persistent-routes не активен'
    
    echo ''
    echo '=== Nginx ==='
    systemctl is-active --quiet nginx && echo '✅ nginx активен' || echo '⚠️ nginx не активен'
"

# 4. Тестирование маршрутов
log "🧪 Тестирование маршрутов..."
ssh root@$SERVER "/etc/scripts/persistent-routes.sh test"

log "✅ Деплой $SERVER завершен успешно!" 