#!/bin/bash

# Скрипт проверки NAT настроек на p.nirhub.ru для туннелирования

SERVER="p.nirhub.ru"
PORT="32322"

log() {
    echo "[$(date '+%H:%M:%S')] $*"
}

log "🔍 Проверка NAT настроек на $SERVER..."

ssh root@$SERVER -p $PORT "
    echo '=== IP Forwarding ==='
    sysctl net.ipv4.ip_forward
    
    echo ''
    echo '=== NAT правила для WG200 ==='
    iptables -t nat -L POSTROUTING -v | grep -E '10\.200\.0'
    
    echo ''
    echo '=== Forward правила для WG200 ==='
    iptables -L FORWARD | grep -E 'wg200|10\.200\.0'
    
    echo ''
    echo '=== Проверка интерфейса wg200 ==='
    ip a show wg200 2>/dev/null || echo 'wg200 не найден'
    
    echo ''
    echo '=== WireGuard статус ==='
    wg show wg200 | head -10
    
    echo ''
    echo '=== Тест связности с pg.gena.host ==='
    ping -c 2 10.200.0.1 2>/dev/null && echo '✅ ping к pg.gena.host OK' || echo '❌ ping к pg.gena.host FAILED'
"

log "✅ Проверка завершена" 