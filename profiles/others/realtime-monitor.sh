#!/bin/bash

# Реальный мониторинг активности IntelliJ IDEA

IDEA_PID=$(pgrep -f "idea" | head -n 1)
if [ -z "$IDEA_PID" ]; then
    echo "IntelliJ IDEA не запущена"
    exit 1
fi

echo "🔍 Мониторинг активности IntelliJ IDEA (PID: $IDEA_PID)"
echo "Нажмите Ctrl+C для остановки"

# Создаём named pipe для быстрого профилирования
mkfifo /tmp/idea-profile-pipe

# Функция для быстрого профилирования
quick_profile() {
    echo "⚡ Быстрое профилирование (5 сек)..."
    ./async-profiler/bin/asprof -e cpu -d 5 -o summary $IDEA_PID
}

# Функция для анализа потоков
analyze_threads() {
    echo "🧵 Анализ потоков:"
    jstack $IDEA_PID | grep -E "(runnable|waiting|blocked)" | sort | uniq -c | sort -nr
}

# Функция для анализа памяти
analyze_memory() {
    echo "💾 Анализ памяти:"
    jstat -gc $IDEA_PID | tail -1 | awk '{printf "Young: %.1f%%, Old: %.1f%%, Meta: %.1f%%\n", $3/$2*100, $5/$4*100, $7/$6*100}'
}

# Основной цикл
while true; do
    clear
    echo "📊 Статистика IntelliJ IDEA - $(date)"
    echo "=================================="
    
    # CPU и память
    top -p $IDEA_PID -b -n 1 | tail -n 1 | awk '{printf "CPU: %s%%, Memory: %s%%\n", $9, $10}'
    
    # Количество потоков
    echo "Потоков: $(ps -p $IDEA_PID -o thcount --no-headers)"
    
    # Файловые дескрипторы
    echo "Файловых дескрипторов: $(lsof -p $IDEA_PID 2>/dev/null | wc -l)"
    
    # Анализ памяти
    analyze_memory
    
    echo -e "\n🔥 Топ потоков по CPU:"
    top -H -p $IDEA_PID -b -n 1 | head -20 | tail -10
    
    # Проверяем высокое потребление CPU
    cpu_usage=$(top -p $IDEA_PID -b -n 1 | tail -n 1 | awk '{print $9}')
    if (( $(echo "$cpu_usage > 30" | bc -l) )); then
        echo -e "\n🚨 Высокое потребление CPU: $cpu_usage%"
        quick_profile
    fi
    
    sleep 3
done
