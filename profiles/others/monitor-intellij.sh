#!/bin/bash

# Скрипт для непрерывного мониторинга IntelliJ IDEA

IDEA_PID=$(pgrep -f "idea" | head -n 1)
if [ -z "$IDEA_PID" ]; then
    echo "IntelliJ IDEA не запущена"
    exit 1
fi

echo "Мониторинг IntelliJ IDEA (PID: $IDEA_PID)"
echo "Нажмите Ctrl+C для остановки"

# Создаём файл для логирования
LOG_FILE="idea-monitor-$(date +%Y%m%d_%H%M%S).log"
echo "Timestamp,CPU%,Memory_MB,Threads,FD_Count,GC_Count,Heap_Used_MB,Heap_Max_MB" > $LOG_FILE

# Функция для получения статистики
get_stats() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    # CPU и память от top
    local cpu_mem=$(top -p $IDEA_PID -b -n 1 | tail -n 1 | awk '{print $9","$10}')
    
    # Количество потоков
    local threads=$(ps -p $IDEA_PID -o thcount --no-headers)
    
    # Количество файловых дескрипторов
    local fd_count=$(lsof -p $IDEA_PID 2>/dev/null | wc -l)
    
    # Java-специфичная информация
    local gc_info=$(jstat -gc $IDEA_PID 2>/dev/null | tail -n 1)
    local gc_count=$(echo $gc_info | awk '{print $3+$5}')
    
    # Информация о heap
    local heap_info=$(jstat -gccapacity $IDEA_PID 2>/dev/null | tail -n 1)
    local heap_used=$(jmap -histo $IDEA_PID 2>/dev/null | head -n 1 | grep -o '[0-9]*' | head -n 1)
    local heap_max=$(echo $heap_info | awk '{print $10/1024}')
    
    echo "$timestamp,$cpu_mem,$threads,$fd_count,$gc_count,$heap_used,$heap_max" >> $LOG_FILE
    
    # Вывод на экран
    printf "\r%s | CPU: %s | Mem: %s | Threads: %s | FD: %s | GC: %s" \
           "$timestamp" \
           "$(echo $cpu_mem | cut -d, -f1)%" \
           "$(echo $cpu_mem | cut -d, -f2)" \
           "$threads" \
           "$fd_count" \
           "$gc_count"
}

# Функция для детального анализа при высоком CPU
analyze_high_cpu() {
    local cpu_usage=$1
    if (( $(echo "$cpu_usage > 50" | bc -l) )); then
        echo -e "\n🔥 Высокое потребление CPU: $cpu_usage%"
        
        # Дамп потоков
        echo "Создаю дамп потоков..."
        jstack $IDEA_PID > "high-cpu-threads-$(date +%Y%m%d_%H%M%S).txt"
        
        # Топ методов по CPU
        echo "Запускаю быстрое профилирование..."
        timeout 10 ./async-profiler/bin/asprof -e cpu -d 10 -f "high-cpu-$(date +%Y%m%d_%H%M%S).html" $IDEA_PID 2>/dev/null &
        
        # Анализ системных вызовов
        echo "Анализирую системные вызовы..."
        timeout 5 strace -p $IDEA_PID -c -f 2>"syscall-$(date +%Y%m%d_%H%M%S).txt" &
    fi
}

# Основной цикл мониторинга
while true; do
    get_stats
    
    # Получаем CPU usage для анализа
    cpu_usage=$(top -p $IDEA_PID -b -n 1 | tail -n 1 | awk '{print $9}')
    
    # Анализируем высокое потребление CPU
    if [[ ! -z "$cpu_usage" ]]; then
        analyze_high_cpu $cpu_usage
    fi
    
    sleep 2
done 