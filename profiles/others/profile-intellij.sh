#!/bin/bash

# Скрипт для профилирования IntelliJ IDEA

IDEA_PID=$(pgrep -f "idea" | head -n 1)
if [ -z "$IDEA_PID" ]; then
    echo "IntelliJ IDEA не запущена"
    exit 1
fi

echo "Найден процесс IntelliJ IDEA: $IDEA_PID"
echo "Все процессы: $(pgrep -f "idea" | tr '\n' ' ')"

# Создаём директорию для результатов
mkdir -p profiling-results
cd profiling-results

echo "Выберите тип профилирования:"
echo "1. JFR профилирование (Java Flight Recorder)"
echo "2. Async-profiler CPU"
echo "3. Async-profiler памяти"
echo "4. Полное профилирование (CPU + память + блокировки)"
echo "5. Мониторинг системных вызовов (strace)"
echo "6. Профилирование потоков"
echo "7. Все виды профилирования"

read -p "Введите номер: " choice

case $choice in
    1)
        echo "Запуск JFR профилирования..."
        FILENAME="idea-jfr-$(date +%Y%m%d_%H%M%S).jfr"
        jcmd $IDEA_PID JFR.start name=idea-profile duration=60s filename=$FILENAME
        echo "Профилирование запущено на 60 секунд, файл: $FILENAME"
        sleep 60
        jcmd $IDEA_PID JFR.stop name=idea-profile
        echo "Профилирование завершено"
        ;;
    2)
        echo "Запуск Async-profiler CPU..."
        ../async-profiler/bin/asprof -e cpu -d 60 -f cpu-profile-$(date +%Y%m%d_%H%M%S).html $IDEA_PID
        ;;
    3)
        echo "Запуск Async-profiler памяти..."
        ../async-profiler/bin/asprof -e alloc -d 60 -f alloc-profile-$(date +%Y%m%d_%H%M%S).html $IDEA_PID
        ;;
    4)
        echo "Полное профилирование..."
        echo "CPU профилирование..."
        ../async-profiler/bin/asprof -e cpu -d 60 -f full-cpu-$(date +%Y%m%d_%H%M%S).html $IDEA_PID
        echo "Memory профилирование..."
        ../async-profiler/bin/asprof -e alloc -d 60 -f full-alloc-$(date +%Y%m%d_%H%M%S).html $IDEA_PID
        ;;
    5)
        echo "Мониторинг системных вызовов..."
        strace -p $IDEA_PID -f -c -o strace-$(date +%Y%m%d_%H%M%S).txt &
        STRACE_PID=$!
        echo "Strace запущен (PID: $STRACE_PID). Нажмите Enter для остановки..."
        read
        kill $STRACE_PID
        ;;
    6)
        echo "Профилирование потоков..."
        jstack $IDEA_PID > threads-$(date +%Y%m%d_%H%M%S).txt
        echo "Дамп потоков сохранён"
        ;;
    7)
        echo "Запуск всех видов профилирования..."
        # JFR
        jcmd $IDEA_PID JFR.start name=idea-full settings=profile duration=120s filename=idea-jfr-full-$(date +%Y%m%d_%H%M%S).jfr
        
        # Async-profiler
        ../async-profiler/bin/asprof -e cpu,alloc,lock -d 120 -f async-full-$(date +%Y%m%d_%H%M%S).html $IDEA_PID &
        
        # Thread dumps каждые 10 секунд
        for i in {1..12}; do
            jstack $IDEA_PID > threads-$i-$(date +%Y%m%d_%H%M%S).txt
            sleep 10
        done
        
        # Системные вызовы
        strace -p $IDEA_PID -f -c -o strace-full-$(date +%Y%m%d_%H%M%S).txt &
        STRACE_PID=$!
        sleep 120
        kill $STRACE_PID
        ;;
esac

echo "Профилирование завершено. Результаты в папке profiling-results/" 