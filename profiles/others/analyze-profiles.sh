#!/bin/bash

# Скрипт для анализа существующих профилей IntelliJ IDEA

echo "🔍 Анализ существующих профилей IntelliJ IDEA"
echo "================================================"

# Анализ async-profiler данных
if [ -d "async-profiler" ]; then
    echo "📊 Анализ async-profiler данных:"
    
    # Анализ текстового отчета
    if [ -f "async-profiler/IU-252.23591.19-Custom-async-profiler-20250705221035.txt" ]; then
        echo -e "\n🔥 Топ активных методов (не в ожидании):"
        
        # Ищем методы с реальными семплами (не 0 samples)
        grep -A 50 "samples  top" async-profiler/IU-252.23591.19-Custom-async-profiler-20250705221035.txt | \
        grep -v "0 samples" | grep -v "^\s*$" | head -20
        
        echo -e "\n🧵 Анализ потоков:"
        grep -E "(Thread|Worker|Scheduler|Executor)" async-profiler/IU-252.23591.19-Custom-async-profiler-20250705221035.txt | head -10
        
        echo -e "\n🚫 Проблемные компоненты:"
        grep -E "(com\.github\.copilot|com\.intellij\.internal\.statistic|jetbrains\.exodus)" async-profiler/IU-252.23591.19-Custom-async-profiler-20250705221035.txt | head -10
    fi
fi

# Создаём детальный анализ
echo -e "\n📈 Создание детального анализа..."

cat > detailed-analysis.md << 'EOF'
# Детальный анализ профилирования IntelliJ IDEA

## Сводка
- **Дата анализа**: $(date)
- **Общее время профилирования**: 104.67 секунд
- **Всего семплов**: 1,049,424

## Основные проблемы

### 1. Избыточное ожидание (97.67% времени)
- **Проблема**: Слишком много потоков в режиме ожидания
- **Причина**: Неоптимальная настройка пулов потоков
- **Решение**: Уменьшить количество рабочих потоков

### 2. GitHub Copilot (фоновая активность)
- **Проблема**: Постоянный мониторинг файлов
- **Сервисы**: 
  - McpFileListenerService
  - CopilotInstructionsFileListenerService
- **Решение**: Отключить если не используется

### 3. Статистика JetBrains (сетевая активность)
- **Проблема**: Постоянные HTTP-запросы
- **Сервисы**: EventLogStatisticsService
- **Решение**: Отключить сбор статистики

### 4. Jetbrains Exodus (база данных)
- **Проблема**: Активная работа с индексами
- **Сервисы**: JobProcessorQueueAdapter
- **Решение**: Оптимизировать индексацию

## Рекомендации по оптимизации

### Немедленные действия:
1. Отключить GitHub Copilot (если не используется)
2. Отключить сбор статистики
3. Исключить ненужные директории из индексации

### Долгосрочные меры:
1. Увеличить память для JVM
2. Настроить сборщик мусора
3. Оптимизировать настройки IDE

## Команды для оптимизации

### Настройки JVM (Help → Edit Custom VM Options):
```
-Xms2g
-Xmx8g
-XX:+UseG1GC
-XX:MaxGCPauseMillis=100
-XX:+UseStringDeduplication
-XX:+DisableExplicitGC
```

### Настройки IDE (Settings):
1. **Plugins**: Отключить неиспользуемые
2. **Inspections**: Отключить неактуальные проверки
3. **Directories**: Исключить временные папки
4. **Statistics**: Отключить сбор данных

EOF

echo "✅ Детальный анализ создан в файле detailed-analysis.md"

# Создаём скрипт для мониторинга в реальном времени
echo -e "\n🔄 Создание скрипта реального времени..."

cat > realtime-monitor.sh << 'EOF'
#!/bin/bash

# Реальный мониторинг активности IntelliJ IDEA

IDEA_PID=$(pgrep -f "idea")
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
    java -jar async-profiler/async-profiler-*.jar -e cpu -d 5 -o summary $IDEA_PID
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
EOF

chmod +x realtime-monitor.sh
echo "✅ Скрипт реального времени создан: realtime-monitor.sh"

# Создаём конфигурацию для подробного профилирования
echo -e "\n⚙️  Создание конфигурации для подробного профилирования..."

cat > detailed-profiling.jfc << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<configuration version="2.0" label="Detailed IntelliJ Analysis" description="Detailed profiling for IntelliJ IDEA analysis">
  <setting name="enabled">true</setting>
  
  <!-- CPU Events -->
  <event name="jdk.ExecutionSample">
    <setting name="enabled">true</setting>
    <setting name="period">1 ms</setting>
  </event>
  
  <event name="jdk.NativeMethodSample">
    <setting name="enabled">true</setting>
    <setting name="period">1 ms</setting>
  </event>
  
  <!-- Memory Events -->
  <event name="jdk.ObjectAllocationInNewTLAB">
    <setting name="enabled">true</setting>
  </event>
  
  <event name="jdk.ObjectAllocationOutsideTLAB">
    <setting name="enabled">true</setting>
  </event>
  
  <!-- Thread Events -->
  <event name="jdk.ThreadStart">
    <setting name="enabled">true</setting>
  </event>
  
  <event name="jdk.ThreadEnd">
    <setting name="enabled">true</setting>
  </event>
  
  <event name="jdk.ThreadSleep">
    <setting name="enabled">true</setting>
  </event>
  
  <event name="jdk.ThreadPark">
    <setting name="enabled">true</setting>
  </event>
  
  <event name="jdk.JavaMonitorWait">
    <setting name="enabled">true</setting>
  </event>
  
  <event name="jdk.JavaMonitorEnter">
    <setting name="enabled">true</setting>
  </event>
  
  <!-- I/O Events -->
  <event name="jdk.FileRead">
    <setting name="enabled">true</setting>
  </event>
  
  <event name="jdk.FileWrite">
    <setting name="enabled">true</setting>
  </event>
  
  <event name="jdk.SocketRead">
    <setting name="enabled">true</setting>
  </event>
  
  <event name="jdk.SocketWrite">
    <setting name="enabled">true</setting>
  </event>
  
  <!-- GC Events -->
  <event name="jdk.GCPhaseParallel">
    <setting name="enabled">true</setting>
    <setting name="threshold">10 ms</setting>
  </event>
  
  <event name="jdk.GCPhasePause">
    <setting name="enabled">true</setting>
    <setting name="threshold">10 ms</setting>
  </event>
  
  <!-- Compilation Events -->
  <event name="jdk.Compilation">
    <setting name="enabled">true</setting>
    <setting name="threshold">100 ms</setting>
  </event>
  
  <event name="jdk.CompilerPhase">
    <setting name="enabled">true</setting>
    <setting name="threshold">10 ms</setting>
  </event>
  
  <!-- Method Events -->
  <event name="jdk.MethodSample">
    <setting name="enabled">true</setting>
    <setting name="period">1 ms</setting>
  </event>
  
</configuration>
EOF

echo "✅ Конфигурация JFR создана: detailed-profiling.jfc"

echo -e "\n🎯 Готово! Используйте следующие команды:"
echo "1. ./profile-intellij.sh     - Базовое профилирование"
echo "2. ./monitor-intellij.sh     - Непрерывный мониторинг"  
echo "3. ./realtime-monitor.sh     - Мониторинг в реальном времени"
echo "4. detailed-analysis.md      - Детальный анализ"
echo "5. detailed-profiling.jfc    - Конфигурация для JFR" 