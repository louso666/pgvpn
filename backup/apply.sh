#!/bin/bash

# Apply configuration script v2.0
# Usage: ./apply.sh {pg.gena.host|p.nirhub.ru|all}
# Рекурсивное копирование локальных конфигов на серверы

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

# Функция для pg.gena.host
apply_pg_gena_host() {
    log "🚀 Деплой конфигураций на pg.gena.host..."
    
    # 1. Рекурсивная синхронизация всей папки (БЕЗ --delete!)
    log "📁 Синхронизация файлов..."
    rsync -avz --progress --no-owner --no-group pg.gena.host/ root@pg.gena.host:/
    
    # 2. Установка пакетов и применение конфигураций
    log "🔧 Установка пакетов и применение конфигураций..."
    ssh root@pg.gena.host "
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
        
        # Включаем постоянные маршруты
        systemctl enable persistent-routes.service
        systemctl start persistent-routes.service
        
        echo '✅ pg.gena.host: конфигурации применены'
    "
    
    # 3. Проверка и запуск сервисов
    check_and_start_pg_services
    
    # 4. Автоматическое тестирование
    log "🧪 Автоматическое тестирование маршрутов..."
    ssh root@pg.gena.host "/etc/scripts/persistent-routes.sh test"
}

# Функция для p.nirhub.ru
apply_p_nirhub_ru() {
    log "🚀 Деплой конфигураций на p.nirhub.ru..."
    
    # 1. Рекурсивная синхронизация всей папки (БЕЗ --delete!)
    log "📁 Синхронизация файлов..."
    rsync -avz --progress --no-owner --no-group -e "ssh -p 32322" p.nirhub.ru/ root@p.nirhub.ru:/
    
    # 2. Применение конфигураций
    log "🔧 Применение конфигураций..."
    ssh root@p.nirhub.ru -p 32322 "
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
        
        echo '✅ p.nirhub.ru: конфигурации применены'
    "
    
    # 3. Проверка и запуск сервисов
    check_and_start_p_services
    
    # 4. Автоматическое тестирование маскарадинга
    log "🧪 Автоматическое тестирование маскарадинга..."
    ssh root@p.nirhub.ru -p 32322 "
        echo '=== Проверка маскарадинга для WG200 ==='
        iptables -t nat -L POSTROUTING -v | grep '10\.200\.0' || echo 'Маскарадинг не найден'
        
        echo '=== Тест форвардинга ==='
        cat /proc/sys/net/ipv4/ip_forward | grep -q 1 && echo '✅ IP forwarding включен' || echo '❌ IP forwarding отключен'
    "
}

# Проверка и запуск сервисов для pg.gena.host
check_and_start_pg_services() {
    log "🔍 Проверка сервисов на pg.gena.host..."
    
    ssh root@pg.gena.host "
        echo '=== Статус WireGuard wg200 ==='
        if systemctl is-active --quiet wg-quick@wg200; then
            echo '✅ wg-quick@wg200 активен'
            wg show wg200 | head -5
        else
            echo '⚠️ wg-quick@wg200 не активен'
            echo 'Для запуска используйте: systemctl start wg-quick@wg200'
        fi
        
        echo ''
        echo '=== Автозагрузка сервисов ==='
        systemctl is-enabled wg-quick@wg200 2>/dev/null && echo '✅ wg-quick@wg200 включен' || echo '⚠️ wg-quick@wg200 не включен'
        
        echo ''
        echo '=== Постоянные маршруты ==='
        systemctl is-active --quiet persistent-routes.service && echo '✅ persistent-routes активен' || echo '⚠️ persistent-routes не активен'
        
        echo ''
        echo '=== Скрипты управления ==='
        ls -la /root/wg /etc/scripts/*.sh 2>/dev/null || echo 'Нет скриптов'
    "
}

# Проверка и запуск сервисов для p.nirhub.ru
check_and_start_p_services() {
    log "🔍 Проверка сервисов на p.nirhub.ru..."
    
    ssh root@p.nirhub.ru -p 32322 "
        echo '=== Статус WireGuard wg200 ==='
        if systemctl is-active --quiet wg-quick@wg200; then
            echo '✅ wg-quick@wg200 активен'
            wg show wg200 | head -5
        else
            echo '⚠️ wg-quick@wg200 не активен'
            echo 'Для запуска используйте: systemctl start wg-quick@wg200'
        fi
        
        echo ''
        echo '=== Маскарадинг и форвардинг ==='
        iptables -t nat -L POSTROUTING | grep -E '10\.200\.0' && echo '✅ Маскарадинг настроен' || echo '❌ Маскарадинг не найден'
        cat /proc/sys/net/ipv4/ip_forward | grep -q 1 && echo '✅ IP forwarding включен' || echo '❌ IP forwarding отключен'
        
        echo ''
        echo '=== Автозагрузка сервисов ==='
        systemctl is-enabled wg-quick@wg200 2>/dev/null && echo '✅ wg-quick@wg200 включен' || echo '⚠️ wg-quick@wg200 не включен'
        
        echo ''
        echo '=== Скрипты управления ==='
        ls -la /etc/scripts/*.sh 2>/dev/null || echo 'Нет скриптов'
    "
}

# Общая проверка статуса
status_check() {
    log "📊 Общая проверка статуса серверов..."
    
    log "🔍 Статус pg.gena.host:"
    check_and_start_pg_services
    
    log ""
    log "🔍 Статус p.nirhub.ru:"
    check_and_start_p_services
    
    log ""
    log "🏓 Тест связности WireGuard:"
    if ssh root@pg.gena.host "wg show wg200 | grep -q peer" 2>/dev/null; then
        if ssh root@pg.gena.host "ping -c 2 10.200.0.6 >/dev/null 2>&1"; then
            log "✅ pg.gena.host -> p.nirhub.ru (WG): OK"
        else
            warn "❌ pg.gena.host -> p.nirhub.ru (WG): НЕТ СВЯЗИ"
        fi
    else
        warn "⚠️ WG на pg.gena.host не готов"
    fi
    
    if ssh root@p.nirhub.ru -p 32322 "wg show wg200 | grep -q peer" 2>/dev/null; then
        if ssh root@p.nirhub.ru -p 32322 "ping -c 2 10.200.0.1 >/dev/null 2>&1"; then
            log "✅ p.nirhub.ru -> pg.gena.host (WG): OK"
        else
            warn "❌ p.nirhub.ru -> pg.gena.host (WG): НЕТ СВЯЗИ"
        fi
    else
        warn "⚠️ WG на p.nirhub.ru не готов"
    fi
}

# Полная настройка (автозапуск всех нужных сервисов)
setup_all() {
    log "🔧 Полная настройка всех серверов..."
    
    apply_pg_gena_host
    sleep 3
    apply_p_nirhub_ru
    sleep 3
    
    log "🚀 Автозапуск критически важных сервисов..."
    
    # Включаем автозагрузку WG на обоих серверах
    ssh root@pg.gena.host "
        systemctl enable wg-quick@wg200
        echo '✅ pg.gena.host: автозагрузка wg200 включена'
    "
    
    ssh root@p.nirhub.ru -p 32322 "
        systemctl enable wg-quick@wg200
        echo '✅ p.nirhub.ru: автозагрузка wg200 включена'
    "
    
    status_check
}

case "$1" in
    pg.gena.host|pg)
        apply_pg_gena_host
        ;;
    p.nirhub.ru|p)
        apply_p_nirhub_ru
        ;;
    all)
        setup_all
        ;;
    status)
        status_check
        ;;
    setup)
        setup_all
        ;;
    *)
        echo "Usage: $0 {pg.gena.host|p.nirhub.ru|all|status|setup}"
        echo ""
        echo "Commands:"
        echo "  pg.gena.host  - Деплой только на pg.gena.host"
        echo "  p.nirhub.ru   - Деплой только на p.nirhub.ru"
        echo "  all           - Деплой на оба сервера"
        echo "  setup         - Полная настройка с автозапуском сервисов"
        echo "  status        - Проверка статуса всех сервисов"
        echo ""
        echo "Короткие алиасы: pg, p"
        exit 1
        ;;
esac 