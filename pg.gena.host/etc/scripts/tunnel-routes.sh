#!/bin/bash

# Скрипт для управления постоянными маршрутами через WG тунель
# Цель: завернуть определенные IP/домены через p.nirhub.ru

WG_GATEWAY="10.200.0.6"  # p.nirhub.ru в WG сети
LOG_FILE="/var/log/tunnel-routes.log"

# Домены/IP которые должны идти через тунель (будут возвращать IP p.nirhub.ru)
TUNNEL_SERVICES=(
    "ifconfig.me"
    "icanhazip.com"
    "34.160.111.145"  # статический IP ifconfig.me
)

# Домены/IP которые должны идти напрямую (будут возвращать IP pg.gena.host) 
DIRECT_SERVICES=(
    "ipinfo.io"
    "api.ipify.org"
    "34.117.59.81"  # статический IP ipinfo.io
)

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"
}

# Получить ВСЕ IP от DNS
resolve_ips() {
    local domain="$1"
    dig +short "$domain" A | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$'
}

# Добавить постоянный маршрут через WG
add_tunnel_route() {
    local target="$1"
    local added=0
    
    if [[ "$target" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        # Это уже IP
        if ip route add "$target/32" via "$WG_GATEWAY" dev wg200 2>/dev/null; then
            log "✅ Добавлен постоянный маршрут: $target -> $WG_GATEWAY"
            ((added++))
        else
            log "⚠️ Маршрут для $target уже существует"
        fi
    else
        # Это домен, резолвим ВСЕ IP
        local ips=($(resolve_ips "$target"))
        if [[ ${#ips[@]} -gt 0 ]]; then
            for ip in "${ips[@]}"; do
                if ip route add "$ip/32" via "$WG_GATEWAY" dev wg200 2>/dev/null; then
                    log "✅ Добавлен постоянный маршрут: $target ($ip) -> $WG_GATEWAY"
                    ((added++))
                else
                    log "⚠️ Маршрут для $target ($ip) уже существует"
                fi
            done
        else
            log "❌ Не удалось резолвить $target"
        fi
    fi
    
    return $added
}

# Удалить маршрут
del_tunnel_route() {
    local target="$1"
    local removed=0
    
    if [[ "$target" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        # Это уже IP
        if ip route del "$target/32" via "$WG_GATEWAY" dev wg200 2>/dev/null; then
            log "✅ Удален маршрут: $target"
            ((removed++))
        else
            log "⚠️ Маршрут для $target не найден"
        fi
    else
        # Это домен, резолвим ВСЕ IP
        local ips=($(resolve_ips "$target"))
        if [[ ${#ips[@]} -gt 0 ]]; then
            for ip in "${ips[@]}"; do
                if ip route del "$ip/32" via "$WG_GATEWAY" dev wg200 2>/dev/null; then
                    log "✅ Удален маршрут: $target ($ip)"
                    ((removed++))
                else
                    log "⚠️ Маршрут для $target ($ip) не найден"
                fi
            done
        fi
    fi
    
    return $removed
}

# Установить все постоянные туннели
setup_tunnels() {
    log "🚀 Установка постоянных туннелей..."
    
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
    
    local added=0
    log "📍 Добавляем маршруты через тунель..."
    for service in "${TUNNEL_SERVICES[@]}"; do
        if add_tunnel_route "$service"; then
            ((added++))
        fi
    done
    
    log "📊 Установлено туннелей: $added"
    return 0
}

# Удалить все туннели
remove_tunnels() {
    log "🗑️ Удаление всех туннелей..."
    
    local removed=0
    for service in "${TUNNEL_SERVICES[@]}"; do
        if del_tunnel_route "$service"; then
            ((removed++))
        fi
    done
    
    log "📊 Удалено туннелей: $removed"
    return 0
}

# Показать статус туннелей
status_tunnels() {
    log "📊 Статус туннелей:"
    
    log "=== Маршруты через тунель (должны возвращать IP p.nirhub.ru: 159.69.33.152) ==="
    for service in "${TUNNEL_SERVICES[@]}"; do
        if [[ "$service" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            # Это уже IP
            local route=$(ip route get "$service" 2>/dev/null | head -1)
            if echo "$route" | grep -q "via $WG_GATEWAY"; then
                log "✅ $service: $route"
            else
                log "❌ $service: $route (НЕ ЧЕРЕЗ ТУНЕЛЬ!)"
            fi
        else
            # Это домен, проверяем все IP
            local ips=($(resolve_ips "$service"))
            for ip in "${ips[@]}"; do
                local route=$(ip route get "$ip" 2>/dev/null | head -1)
                if echo "$route" | grep -q "via $WG_GATEWAY"; then
                    log "✅ $service ($ip): $route"
                else
                    log "❌ $service ($ip): $route (НЕ ЧЕРЕЗ ТУНЕЛЬ!)"
                fi
            done
        fi
    done
    
    log ""
    log "=== Прямые маршруты (должны возвращать IP pg.gena.host: 176.114.88.142) ==="
    for service in "${DIRECT_SERVICES[@]}"; do
        if [[ "$service" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            # Это уже IP
            local route=$(ip route get "$service" 2>/dev/null | head -1)
            if echo "$route" | grep -q "via $WG_GATEWAY"; then
                log "❌ $service: $route (ИДЕТ ЧЕРЕЗ ТУНЕЛЬ!)"
            else
                log "✅ $service: $route"
            fi
        else
            # Это домен, проверяем все IP
            local ips=($(resolve_ips "$service"))
            for ip in "${ips[@]}"; do
                local route=$(ip route get "$ip" 2>/dev/null | head -1)
                if echo "$route" | grep -q "via $WG_GATEWAY"; then
                    log "❌ $service ($ip): $route (ИДЕТ ЧЕРЕЗ ТУНЕЛЬ!)"
                else
                    log "✅ $service ($ip): $route"
                fi
            done
        fi
    done
}

# Тестировать туннели
test_tunnels() {
    log "🧪 Тестирование туннелей..."
    
    log "=== Тесты через тунель ==="
    for service in "${TUNNEL_SERVICES[@]}"; do
        if [[ "$service" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            # Для IP используем ifconfig.me
            local result=$(curl -s --max-time 10 "http://ifconfig.me" 2>/dev/null || echo "TIMEOUT")
            log "📍 ifconfig.me -> $result"
        else
            # Для домена тестируем его
            local result=$(curl -s --max-time 10 "http://$service" 2>/dev/null || echo "TIMEOUT")
            log "📍 $service -> $result"
        fi
    done
    
    log ""
    log "=== Тесты прямого соединения ==="
    for service in "${DIRECT_SERVICES[@]}"; do
        if [[ "$service" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            local result=$(curl -s --max-time 10 "http://ipinfo.io/ip" 2>/dev/null || echo "TIMEOUT")
            log "📍 ipinfo.io -> $result"
        else
            local result=$(curl -s --max-time 10 "http://$service" 2>/dev/null || echo "TIMEOUT")
            log "📍 $service -> $result"
        fi
    done
}

case "$1" in
    setup|start)
        setup_tunnels
        ;;
    remove|stop)
        remove_tunnels  
        ;;
    restart)
        remove_tunnels
        sleep 2
        setup_tunnels
        ;;
    status)
        status_tunnels
        ;;
    test)
        test_tunnels
        ;;
    *)
        echo "Использование: $0 {setup|remove|restart|status|test}"
        echo ""
        echo "  setup    - установить постоянные туннели"
        echo "  remove   - удалить все туннели"
        echo "  restart  - перезапустить туннели"
        echo "  status   - показать статус туннелей"
        echo "  test     - протестировать туннели"
        exit 1
        ;;
esac 