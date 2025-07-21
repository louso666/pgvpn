# 🔍 Подробное профилирование IntelliJ IDEA

## Быстрый старт

1. **Запустите IntelliJ IDEA**
2. **Выберите тип анализа**:

   ```bash
   # Базовое профилирование
   ./profile-intellij.sh

   # Непрерывный мониторинг
   ./monitor-intellij.sh

   # Реальное время
   ./realtime-monitor.sh

   # Анализ существующих данных
   ./analyze-profiles.sh
   ```

## 🎯 Типы профилирования

### 1. Java Flight Recorder (JFR)

**Лучший выбор для комплексного анализа**

```bash
# Запуск JFR на 60 секунд
jcmd $(pgrep -f "idea") JFR.start name=idea-profile duration=60s filename=idea-profile.jfr

# Анализ результатов
jfr print --events CPULoad,GCPhasePause,ThreadStart idea-profile.jfr
```

**Преимущества:**

- Минимальный overhead (~1-2%)
- Подробная информация о GC, потоках, I/O
- Встроенная поддержка в JDK

### 2. Async-profiler

**Для детального анализа CPU и памяти**

```bash
# CPU профилирование
java -jar async-profiler/async-profiler-*.jar -e cpu -d 60 -f cpu-profile.html $(pgrep -f "idea")

# Профилирование аллокаций
java -jar async-profiler/async-profiler-*.jar -e alloc -d 60 -f alloc-profile.html $(pgrep -f "idea")

# Полное профилирование
java -jar async-profiler/async-profiler-*.jar -e cpu,alloc,lock -d 120 -f full-profile.html $(pgrep -f "idea")
```

**Преимущества:**

- Очень точные данные о CPU
- Красивые flame graphs
- Поддержка native кода

### 3. Системный мониторинг

**Для анализа системных вызовов**

```bash
# Анализ системных вызовов
strace -p $(pgrep -f "idea") -f -c -o syscalls.txt

# Мониторинг файловых операций
inotifywait -m -r --format '%w%f %e' /path/to/project

# Анализ сетевой активности
netstat -p | grep $(pgrep -f "idea")
```

## 📊 Интерпретация результатов

### Высокое потребление CPU

1. **Ищите в flame graph:**

   - Широкие полосы = много времени
   - Глубокие стеки = сложные вызовы

2. **Основные подозреваемые:**

   - `com.intellij.codeInsight.daemon.impl.InspectionRunner`
   - `com.github.copilot.*`
   - `com.intellij.internal.statistic.*`
   - `jetbrains.exodus.*`

3. **Нормальные активности:**
   - Сборка мусора (GC)
   - Компиляция кода (JIT)
   - Индексация файлов

### Проблемы с потоками

```bash
# Анализ заблокированных потоков
jstack $(pgrep -f "idea") | grep -A 10 "BLOCKED"

# Анализ потоков в ожидании
jstack $(pgrep -f "idea") | grep -A 10 "WAITING"
```

**Проблемные паттерны:**

- Много потоков в BLOCKED состоянии
- Длительные WAITING на блокировках
- Активные потоки без полезной работы

### Проблемы с памятью

```bash
# Анализ heap
jstat -gc $(pgrep -f "idea")

# Топ объектов в памяти
jmap -histo $(pgrep -f "idea") | head -20
```

**Красные флаги:**

- Частая сборка мусора
- Высокое потребление Old Gen
- Много объектов одного типа

## 🛠️ Оптимизация по результатам

### 1. Отключение ненужных компонентов

```bash
# Создание списка отключений
cat > disable-components.txt << 'EOF'
Settings -> Plugins:
- GitHub Copilot (если не используется)
- Unused plugin inspections
- Language plugins для неиспользуемых языков

Settings -> Editor -> Inspections:
- Отключить неактуальные проверки
- Уменьшить scope проверок

Settings -> Advanced Settings:
- Отключить statistics collection
- Уменьшить indexing threads
EOF
```

### 2. Настройка JVM

```bash
# Создание оптимизированной конфигурации
cat > idea64.vmoptions << 'EOF'
# Память
-Xms4g
-Xmx8g
-XX:NewRatio=2

# Сборщик мусора
-XX:+UseG1GC
-XX:MaxGCPauseMillis=100
-XX:+UseStringDeduplication

# Оптимизации
-XX:+DisableExplicitGC
-XX:+UseCompressedOops
-XX:+UseCompressedClassPointers

# Профилирование (для отладки)
-XX:+FlightRecorder
-XX:StartFlightRecording=duration=60s,filename=idea-startup.jfr
EOF
```

### 3. Оптимизация файловой системы

```bash
# Исключение директорий
cat > exclusions.txt << 'EOF'
Исключить из индексации:
- node_modules/
- .git/
- target/
- build/
- dist/
- *.log
- *.tmp
- async-profiler/
- profiling-results/
EOF
```

## 🔄 Автоматический мониторинг

### Настройка непрерывного мониторинга

```bash
# Создание cron задачи
crontab -e

# Добавить строку для мониторинга каждые 5 минут
*/5 * * * * /home/tema/agent/monitor-intellij.sh >> /var/log/idea-monitor.log 2>&1
```

### Настройка алертов

```bash
# Создание скрипта для алертов
cat > alert-high-cpu.sh << 'EOF'
#!/bin/bash
IDEA_PID=$(pgrep -f "idea")
if [ ! -z "$IDEA_PID" ]; then
    CPU_USAGE=$(top -p $IDEA_PID -b -n 1 | tail -n 1 | awk '{print $9}')
    if (( $(echo "$CPU_USAGE > 80" | bc -l) )); then
        echo "🚨 HIGH CPU: $CPU_USAGE%" | logger -t idea-monitor
        # Автоматическое профилирование
        java -jar async-profiler/async-profiler-*.jar -e cpu -d 30 -f "alert-$(date +%Y%m%d_%H%M%S).html" $IDEA_PID
    fi
fi
EOF
chmod +x alert-high-cpu.sh
```

## 📈 Анализ тенденций

### Создание отчетов

```bash
# Еженедельный отчет
cat > weekly-report.sh << 'EOF'
#!/bin/bash
echo "📊 Еженедельный отчет по производительности IntelliJ IDEA"
echo "========================================================"
echo "Дата: $(date)"
echo ""

# Анализ логов мониторинга
if [ -f "idea-monitor-*.log" ]; then
    echo "📈 Статистика CPU за неделю:"
    grep "CPU:" idea-monitor-*.log | awk -F',' '{print $2}' | sort -n | tail -10

    echo ""
    echo "💾 Статистика памяти:"
    grep "Memory:" idea-monitor-*.log | awk -F',' '{print $3}' | sort -n | tail -10
fi

echo ""
echo "🔥 Самые активные компоненты:"
find . -name "*.html" -newer $(date -d "7 days ago" +%Y%m%d) -exec grep -l "com.intellij\|com.github\|jetbrains" {} \;
EOF
chmod +x weekly-report.sh
```

## ⚡ Экстренные меры

### При критическом потреблении CPU

```bash
# Экстренное профилирование
timeout 30 java -jar async-profiler/async-profiler-*.jar -e cpu -d 30 -f emergency-profile.html $(pgrep -f "idea")

# Дамп потоков
jstack $(pgrep -f "idea") > emergency-threads.txt

# Принудительная сборка мусора
jcmd $(pgrep -f "idea") GC.run_finalization
jcmd $(pgrep -f "idea") GC.run
```

### При зависании

```bash
# Серия дампов потоков
for i in {1..5}; do
    jstack $(pgrep -f "idea") > hang-threads-$i.txt
    sleep 5
done

# Анализ дедлоков
jstack $(pgrep -f "idea") | grep -A 20 "Found deadlock"
```

## 🎯 Практические рекомендации

### Порядок действий при высоком CPU:

1. **Запустите мониторинг**: `./realtime-monitor.sh`
2. **Дождитесь пика нагрузки**
3. **Запустите профилирование**: выберите опцию 4 в `./profile-intellij.sh`
4. **Проанализируйте результаты**: откройте HTML файл в браузере
5. **Примените оптимизации**: согласно найденным проблемам

### Регулярная профилактика:

1. **Еженедельно**: запускайте `./weekly-report.sh`
2. **При обновлениях**: профилируйте после установки новых плагинов
3. **При изменении проектов**: переконфигурируйте исключения

### Оптимальные настройки:

```
CPU профилирование: 1-2 минуты
Memory профилирование: 3-5 минут
Полное профилирование: 5-10 минут
Мониторинг: непрерывно с интервалом 2-5 секунд
```

---

## 📞 Поддержка

При возникновении проблем:

1. Проверьте логи: `tail -f /var/log/idea-monitor.log`
2. Убедитесь, что IntelliJ IDEA запущена: `pgrep -f "idea"`
3. Проверьте права доступа: `ls -la *.sh`
4. Проверьте наличие инструментов: `which jstack jcmd jstat`

**Полезные команды для диагностики:**

```bash
# Проверка Java процессов
jps -v

# Проверка версии async-profiler
java -jar async-profiler/async-profiler-*.jar --version

# Проверка доступных событий JFR
jfr print --events idea-profile.jfr
```
