#!/bin/bash

# Скрипт для тестирования IP через WG тунель vs прямое соединение
# Умная маршрутизация - добавляет routes только для тестовых IP

WG_GATEWAY="10.200.0.6"  # p.nirhub.ru в WG сети
LOG_FILE="/var/log/ip-test.log"

# Тестовые сервисы
ROUTE_SERVICES=(
    "ifconfig.me"
    "34.160.111.145"  # статический IP ifconfig.me
)

NO_ROUTE_SERVICES=(
    "ipinfo.io" 
    "34.117.59.81"  # статический IP ipinfo.io
)

ALL_SERVICES=(
    "ifconfig.me"
    "ipinfo.io"
    "icanhazip.com"
    "api.ipify.org"
    "checkip.amazonaws.com"
)

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"
}

# Получить IP от DNS
resolve_ip() {
    local domain="$1"
    dig +short "$domain" A | head -1 | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$'
}

# Добавить маршрут через WG
add_route() {
    local target="$1"
    if [[ "$target" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        # Это уже IP
        ip route add "$target/32" via "$WG_GATEWAY" dev wg200 2>/dev/null
        echo "$target"
    else
        # Это домен, резолвим
        local ip=$(resolve_ip "$target")
        if [[ -n "$ip" ]]; then
            ip route add "$ip/32" via "$WG_GATEWAY" dev wg200 2>/dev/null
            echo "$ip"
        fi
    fi
}

# Удалить маршрут
del_route() {
    local target="$1"
    if [[ "$target" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        ip route del "$target/32" via "$WG_GATEWAY" dev wg200 2>/dev/null
    else
        local ip=$(resolve_ip "$target")
        [[ -n "$ip" ]] && ip route del "$ip/32" via "$WG_GATEWAY" dev wg200 2>/dev/null
    fi
}

# Тест HTTP запроса
test_http() {
    local service="$1"
    local timeout=10
    
    if [[ "$service" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        # IP адрес - используем ifconfig.me
        curl -s --max-time "$timeout" "http://ifconfig.me" 2>/dev/null || echo "TIMEOUT"
    else
        # Домен
        curl -s --max-time "$timeout" "http://$service" 2>/dev/null || echo "TIMEOUT" 
    fi
}

# Основная функция тестирования
run_test() {
    local mode="$1"
    
    log "=== Запуск теста ($mode) ==="
    
    if [[ "$mode" == "routed" ]]; then
        log "Добавляем маршруты через WG тунель..."
        local routed_ips=()
        for service in "${ROUTE_SERVICES[@]}"; do
            local ip=$(add_route "$service")
            [[ -n "$ip" ]] && routed_ips+=("$ip")
        done
        
        log "Добавлены маршруты для: ${routed_ips[*]}"
        sleep 2
    fi
    
    log "Тестируем все сервисы..."
    for service in "${ALL_SERVICES[@]}"; do
        log "  $service -> $(test_http "$service")"
    done
    
    if [[ "$mode" == "routed" ]]; then
        log "Удаляем маршруты..."
        for service in "${ROUTE_SERVICES[@]}"; do
            del_route "$service"
        done
    fi
}

# Сравнительный тест
compare_test() {
    log "🔍 СРАВНИТЕЛЬНЫЙ ТЕСТ IP"
    
    log ""
    run_test "direct"
    
    log ""
    run_test "routed"
    
    log ""
    log "✅ Тест завершен. Логи в $LOG_FILE"
}

# Проверка статуса WG
status() {
    log "=== Статус WireGuard ==="
    wg show wg200
    
    log ""
    log "=== Маршруты к тестовым IP ==="
    for service in "${ROUTE_SERVICES[@]}" "${NO_ROUTE_SERVICES[@]}"; do
        if [[ ! "$service" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            local ip=$(resolve_ip "$service")
            [[ -n "$ip" ]] && service="$service ($ip)"
        fi
        
        local route=$(ip route get "$service" 2>/dev/null | head -1)
        log "  $service: $route"
    done
}

# Очистка всех маршрутов
cleanup() {
    log "🧹 Очистка всех тестовых маршрутов..."
    
    for service in "${ALL_SERVICES[@]}" "${ROUTE_SERVICES[@]}" "${NO_ROUTE_SERVICES[@]}"; do
        del_route "$service"
    done
    
    log "✅ Очистка завершена"
}

case "$1" in
    test|compare)
        compare_test
        ;;
    routed)
        run_test "routed"
        ;;
    direct)
        run_test "direct"
        ;;
    status)
        status
        ;;
    cleanup)
        cleanup
        ;;
    *)
        echo "Использование: $0 {test|routed|direct|status|cleanup}"
        echo ""
        echo "  test      - сравнительный тест (прямое + через тунель)"
        echo "  routed    - тест только через тунель"
        echo "  direct    - тест только прямое соединение"
        echo "  status    - статус WG и маршрутов"
        echo "  cleanup   - очистить все тестовые маршруты"
        exit 1
        ;;
esac 