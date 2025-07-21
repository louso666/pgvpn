#!/bin/bash

# Анализ JFR профиля IntelliJ IDEA v2
# Файл: IU-252.23591.19_tema_06.07.2025_11.32.52.jfr

cd "$(dirname "$0")"

JFR_FILE="IU-252.23591.19_tema_06.07.2025_11.32.52.jfr"
ANALYSIS_FILE="intellij-v2-detailed-analysis.txt"

echo "🔍 ДЕТАЛЬНЫЙ АНАЛИЗ ПРОФИЛЯ IntelliJ IDEA v2" > $ANALYSIS_FILE
echo "=============================================" >> $ANALYSIS_FILE
echo "Файл: $JFR_FILE" >> $ANALYSIS_FILE
echo "Дата анализа: $(date)" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Общая информация о профиле
echo "📊 ОБЩАЯ СВОДКА ПРОФИЛЯ:" >> $ANALYSIS_FILE
echo "========================" >> $ANALYSIS_FILE
jfr summary $JFR_FILE >> $ANALYSIS_FILE 2>/dev/null || echo "Ошибка получения сводки" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Топ потоков по потреблению CPU
echo "🔥 ТОП ПОТОКОВ ПО CPU (детальная статистика):" >> $ANALYSIS_FILE
echo "=============================================" >> $ANALYSIS_FILE
jfr print --events jdk.ExecutionSample $JFR_FILE 2>/dev/null | grep -E "sampledThread|javaThreadName" | sort | uniq -c | sort -nr | head -30 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Детальный анализ каждого топового потока
echo "🖼️  ДЕТАЛЬНЫЙ АНАЛИЗ AWT-EventQueue-0 (UI поток):" >> $ANALYSIS_FILE
echo "================================================" >> $ANALYSIS_FILE
jfr print --events jdk.ExecutionSample $JFR_FILE 2>/dev/null | awk '
/AWT-EventQueue-0/ {
    print "=== STACK TRACE ==="
    getline
    for(i=0; i<15 && getline; i++) {
        if(/^jdk\.ExecutionSample/) break
        print $0
    }
    print ""
}' | head -200 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ TimerQueue потока
echo "⏰ ДЕТАЛЬНЫЙ АНАЛИЗ TimerQueue потока:" >> $ANALYSIS_FILE
echo "====================================" >> $ANALYSIS_FILE
jfr print --events jdk.ExecutionSample $JFR_FILE 2>/dev/null | awk '
/TimerQueue/ {
    print "=== STACK TRACE ==="
    getline
    for(i=0; i<15 && getline; i++) {
        if(/^jdk\.ExecutionSample/) break
        print $0
    }
    print ""
}' | head -200 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ Kotlin корутин
echo "🚀 ДЕТАЛЬНЫЙ АНАЛИЗ DefaultDispatcher потоков (Kotlin корутины):" >> $ANALYSIS_FILE
echo "===============================================================" >> $ANALYSIS_FILE
jfr print --events jdk.ExecutionSample $JFR_FILE 2>/dev/null | awk '
/DefaultDispatcher/ {
    print "=== STACK TRACE ==="
    getline
    for(i=0; i<15 && getline; i++) {
        if(/^jdk\.ExecutionSample/) break
        print $0
    }
    print ""
}' | head -300 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ GC потоков
echo "🗑️  АНАЛИЗ GC потоков:" >> $ANALYSIS_FILE
echo "=====================" >> $ANALYSIS_FILE
jfr print --events jdk.ExecutionSample $JFR_FILE 2>/dev/null | grep -A 10 -B 5 "GC" | head -100 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ событий GC
echo "🗑️  СОБЫТИЯ СБОРКИ МУСОРА:" >> $ANALYSIS_FILE
echo "=========================" >> $ANALYSIS_FILE
jfr print --events jdk.GarbageCollection $JFR_FILE 2>/dev/null | head -50 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ аллокаций памяти
echo "💾 АНАЛИЗ АЛЛОКАЦИЙ ПАМЯТИ:" >> $ANALYSIS_FILE
echo "===========================" >> $ANALYSIS_FILE
jfr print --events jdk.ObjectAllocationSample $JFR_FILE 2>/dev/null | head -50 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# CPU Load анализ
echo "📈 АНАЛИЗ ЗАГРУЗКИ CPU:" >> $ANALYSIS_FILE
echo "======================" >> $ANALYSIS_FILE
jfr print --events jdk.CPULoad $JFR_FILE 2>/dev/null | head -50 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ JIT компиляции
echo "⚡ АНАЛИЗ JIT КОМПИЛЯЦИИ:" >> $ANALYSIS_FILE
echo "========================" >> $ANALYSIS_FILE
jfr print --events jdk.Compilation $JFR_FILE 2>/dev/null | head -50 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ блокировок
echo "🔒 АНАЛИЗ БЛОКИРОВОК (JavaMonitorEnter):" >> $ANALYSIS_FILE
echo "=======================================" >> $ANALYSIS_FILE
jfr print --events jdk.JavaMonitorEnter $JFR_FILE 2>/dev/null | head -50 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ потоков ожидания
echo "😴 АНАЛИЗ WAITING/BLOCKED потоков:" >> $ANALYSIS_FILE
echo "==================================" >> $ANALYSIS_FILE
jfr print --events jdk.ThreadPark $JFR_FILE 2>/dev/null | head -50 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ исключений
echo "💥 АНАЛИЗ ИСКЛЮЧЕНИЙ:" >> $ANALYSIS_FILE
echo "===================" >> $ANALYSIS_FILE
jfr print --events jdk.JavaExceptionThrow $JFR_FILE 2>/dev/null | head -50 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ файловых операций
echo "📁 АНАЛИЗ ФАЙЛОВЫХ ОПЕРАЦИЙ:" >> $ANALYSIS_FILE
echo "============================" >> $ANALYSIS_FILE
jfr print --events jdk.FileRead $JFR_FILE 2>/dev/null | head -30 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
jfr print --events jdk.FileWrite $JFR_FILE 2>/dev/null | head -30 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ сетевых операций
echo "🌐 АНАЛИЗ СЕТЕВЫХ ОПЕРАЦИЙ:" >> $ANALYSIS_FILE
echo "===========================" >> $ANALYSIS_FILE
jfr print --events jdk.SocketRead $JFR_FILE 2>/dev/null | head -30 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
jfr print --events jdk.SocketWrite $JFR_FILE 2>/dev/null | head -30 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Статистика по типам событий
echo "📊 СТАТИСТИКА ПО ТИПАМ СОБЫТИЙ:" >> $ANALYSIS_FILE
echo "===============================" >> $ANALYSIS_FILE
jfr print $JFR_FILE 2>/dev/null | grep -E "^[a-z]" | sort | uniq -c | sort -nr | head -20 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ всех доступных событий
echo "📋 ВСЕ ДОСТУПНЫЕ СОБЫТИЯ В ПРОФИЛЕ:" >> $ANALYSIS_FILE
echo "==================================" >> $ANALYSIS_FILE
jfr print --events "*" $JFR_FILE 2>/dev/null | grep -E "^[a-z].*:" | sort | uniq | head -100 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

echo "✅ Детальный анализ завершен! Результаты сохранены в $ANALYSIS_FILE"
echo ""
echo "📋 АНАЛИЗ ГОТОВ К ПЕРЕДАЧЕ ДРУГОЙ МОДЕЛИ" 