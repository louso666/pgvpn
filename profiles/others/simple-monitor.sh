#!/bin/bash

# Простой мониторинг IntelliJ IDEA

IDEA_PID=$(pgrep -f "idea" | head -n 1)
if [ -z "$IDEA_PID" ]; then
    echo "IntelliJ IDEA не запущена"
    exit 1
fi

echo "🔍 Мониторинг IntelliJ IDEA (PID: $IDEA_PID)"
echo "Нажмите Ctrl+C для остановки"
echo "================================="

while true; do
    echo "$(date '+%H:%M:%S') | $(top -p $IDEA_PID -b -n 1 | tail -n 1 | awk '{printf "CPU: %s%% Memory: %s%%", $9, $10}')"
    
    # Проверяем высокое потребление CPU
    cpu_usage=$(top -p $IDEA_PID -b -n 1 | tail -n 1 | awk '{print $9}' | cut -d% -f1)
    if [ "$cpu_usage" -gt 50 ] 2>/dev/null; then
        echo "🔥 Высокое потребление CPU: $cpu_usage%"
        echo "Создаю дамп потоков..."
        jstack $IDEA_PID > "high-cpu-threads-$(date +%Y%m%d_%H%M%S).txt"
        echo "Дамп сохранен"
    fi
    
    sleep 2
done 