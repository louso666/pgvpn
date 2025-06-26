#!/bin/bash

# Постоянные маршруты через WG тунель
# Этот скрипт настраивает постоянную маршрутизацию определенных IP через p.nirhub.ru

WG_GATEWAY="10.200.0.6"  # p.nirhub.ru в WG сети
LOG_FILE="/var/log/persistent-routes.log"

# IP которые должны идти через WG тунель (p.nirhub.ru)
ROUTED_IPS=(
    "34.160.111.145"  # ifconfig.me
)

# IP которые должны идти напрямую (не трогаем)
DIRECT_IPS=(
    "34.117.59.81"    # ipinfo.io
)

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"
}

add_persistent_routes() {
    log "🚀 Добавление постоянных маршрутов через WG..."
    
    # Проверяем что WG интерфейс активен
    if ! ip link show wg200 >/dev/null 2>&1; then
        log "❌ Интерфейс wg200 не найден"
        return 1
    fi
    
    # Проверяем доступность шлюза
    if ! ping -c 1 -W 2 "$WG_GATEWAY" >/dev/null 2>&1; then
        log "❌ Шлюз $WG_GATEWAY недоступен"
        return 1
    fi
    
    # Добавляем маршруты
    local added=0
    for ip in "${ROUTED_IPS[@]}"; do
        if ip route add "$ip/32" via "$WG_GATEWAY" dev wg200 2>/dev/null; then
            log "✅ Добавлен маршрут: $ip -> $WG_GATEWAY"
            ((added++))
        else
            # Маршрут уже существует
            log "⚠️  Маршрут для $ip уже существует"
        fi
    done
    
    log "📊 Добавлено маршрутов: $added"
    return 0
}

remove_persistent_routes() {
    log "🗑️  Удаление постоянных маршрутов..."
    
    local removed=0
    for ip in "${ROUTED_IPS[@]}"; do
        if ip route del "$ip/32" via "$WG_GATEWAY" dev wg200 2>/dev/null; then
            log "✅ Удален маршрут: $ip -> $WG_GATEWAY"
            ((removed++))
        else
            log "⚠️  Маршрут для $ip не найден"
        fi
    done
    
    log "📊 Удалено маршрутов: $removed"
    return 0
}

status_routes() {
    log "📊 Статус постоянных маршрутов:"
    
    log "=== Маршруты через WG ==="
    for ip in "${ROUTED_IPS[@]}"; do
        local route=$(ip route get "$ip" 2>/dev/null | head -1)
        if echo "$route" | grep -q "via $WG_GATEWAY"; then
            log "✅ $ip: $route"
        else
            log "❌ $ip: $route (НЕ ЧЕРЕЗ WG!)"
        fi
    done
    
    log ""
    log "=== Прямые маршруты ==="
    for ip in "${DIRECT_IPS[@]}"; do
        local route=$(ip route get "$ip" 2>/dev/null | head -1)
        if echo "$route" | grep -q "via $WG_GATEWAY"; then
            log "❌ $ip: $route (ИДЕТ ЧЕРЕЗ WG!)"
        else
            log "✅ $ip: $route"
        fi
    done
}

test_routes() {
    log "🧪 Тестирование маршрутов..."
    
    log "=== Тесты через WG тунель ==="
    for ip in "${ROUTED_IPS[@]}"; do
        local result=$(curl -s --max-time 5 "http://ifconfig.me" 2>/dev/null || echo "TIMEOUT")
        if [[ "$result" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            log "✅ ifconfig.me -> $result (должен быть IP p.nirhub.ru: 159.69.33.152)"
        else
            log "❌ ifconfig.me -> $result"
        fi
    done
    
    log ""
    log "=== Тесты прямого соединения ==="
    local result=$(curl -s --max-time 5 "http://ipinfo.io/ip" 2>/dev/null || echo "TIMEOUT")
    if [[ "$result" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        log "✅ ipinfo.io -> $result (должен быть IP pg.gena.host: 176.114.88.142)"
    else
        log "❌ ipinfo.io -> $result"
    fi
}

case "$1" in
    start|add)
        add_persistent_routes
        ;;
    stop|remove)
        remove_persistent_routes
        ;;
    restart)
        remove_persistent_routes
        sleep 2
        add_persistent_routes
        ;;
    status)
        status_routes
        ;;
    test)
        test_routes
        ;;
    *)
        echo "Использование: $0 {start|stop|restart|status|test}"
        echo ""
        echo "  start    - добавить постоянные маршруты"
        echo "  stop     - удалить постоянные маршруты"
        echo "  restart  - перезапустить маршруты"
        echo "  status   - показать статус маршрутов"
        echo "  test     - протестировать маршруты"
        exit 1
        ;;
esac 