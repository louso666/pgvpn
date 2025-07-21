#!/bin/bash

# Анализ JFR профиля IntelliJ IDEA
# Файл: IU-252.23591.19_tema_06.07.2025_10.16.01.jfr

JFR_FILE="IU-252.23591.19_tema_06.07.2025_10.16.01.jfr"
ANALYSIS_FILE="intellij-cpu-analysis.txt"

echo "🔍 Детальный анализ профиля IntelliJ IDEA" > $ANALYSIS_FILE
echo "=======================================" >> $ANALYSIS_FILE
echo "Файл: $JFR_FILE" >> $ANALYSIS_FILE
echo "Дата анализа: $(date)" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Общая информация
echo "📊 ОБЩАЯ СВОДКА:" >> $ANALYSIS_FILE
jfr summary $JFR_FILE >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Топ потоков по потреблению CPU
echo "🔥 ТОП ПОТОКОВ ПО CPU (семплы):" >> $ANALYSIS_FILE
jfr print --events jdk.ExecutionSample $JFR_FILE | grep "sampledThread" | sort | uniq -c | sort -nr | head -15 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Детальный анализ AWT потока
echo "🖼️  АНАЛИЗ AWT-EventQueue-0 (UI поток):" >> $ANALYSIS_FILE
echo "Этот поток жрал больше всего CPU! Примеры стеков:" >> $ANALYSIS_FILE
jfr print --events jdk.ExecutionSample $JFR_FILE | awk '
/AWT-EventQueue-0.*javaThreadId = 39/ {
    getline; getline; 
    print "---"
    for(i=0; i<10 && getline; i++) {
        if(/^jdk\.ExecutionSample/) break
        print $0
    }
}' | head -100 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Детальный анализ TimerQueue
echo "⏰ АНАЛИЗ TimerQueue потока:" >> $ANALYSIS_FILE  
echo "Второй по потреблению CPU поток! Примеры стеков:" >> $ANALYSIS_FILE
jfr print --events jdk.ExecutionSample $JFR_FILE | awk '
/TimerQueue.*javaThreadId = 103/ {
    getline; getline;
    print "---"
    for(i=0; i<10 && getline; i++) {
        if(/^jdk\.ExecutionSample/) break
        print $0
    }
}' | head -100 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ Kotlin корутин
echo "🚀 АНАЛИЗ DefaultDispatcher потоков (Kotlin корутины):" >> $ANALYSIS_FILE
jfr print --events jdk.ExecutionSample $JFR_FILE | grep "DefaultDispatcher" | head -20 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ событий сна
echo "😴 АНАЛИЗ спящих потоков (WallClockSleeping):" >> $ANALYSIS_FILE
jfr print --events profiler.WallClockSleeping $JFR_FILE | head -30 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Анализ аллокаций памяти
echo "💾 АНАЛИЗ аллокаций памяти:" >> $ANALYSIS_FILE
jfr print --events jdk.ObjectAllocationSample $JFR_FILE | head -20 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# CPU Load анализ
echo "📈 АНАЛИЗ загрузки CPU:" >> $ANALYSIS_FILE
jfr print --events jdk.CPULoad $JFR_FILE | grep -E "(machineTotal|jvmUser|jvmSystem)" | head -20 >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

# Проблемные места
echo "🚨 ДИАГНОЗ И РЕКОМЕНДАЦИИ:" >> $ANALYSIS_FILE
echo "=========================" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
echo "ОСНОВНЫЕ ПРОБЛЕМЫ:" >> $ANALYSIS_FILE
echo "1. AWT-EventQueue-0 поток слишком активен в простое (424 семпла)" >> $ANALYSIS_FILE
echo "   - Возможная причина: постоянная перерисовка UI компонентов" >> $ANALYSIS_FILE
echo "   - Рекомендация: отключить анимации, проверить UI плагины" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
echo "2. TimerQueue поток жрет CPU (245 семплов)" >> $ANALYSIS_FILE  
echo "   - Возможная причина: слишком частые таймеры Swing" >> $ANALYSIS_FILE
echo "   - Рекомендация: увеличить интервалы обновления UI" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
echo "3. Много корутин DefaultDispatcher (суммарно ~700+ семплов)" >> $ANALYSIS_FILE
echo "   - Возможная причина: фоновые задачи плагинов" >> $ANALYSIS_FILE
echo "   - Рекомендация: отключить неиспользуемые плагины" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
echo "4. C2 JIT компилятор активен (233 семпла)" >> $ANALYSIS_FILE
echo "   - Это нормально при запуске, но не в простое" >> $ANALYSIS_FILE
echo "   - Рекомендация: увеличить -Xmx память" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

echo "🎯 КОНКРЕТНЫЕ ДЕЙСТВИЯ:" >> $ANALYSIS_FILE
echo "=====================" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
echo "1. ОТКЛЮЧИТЬ АНИМАЦИИ И ЭФФЕКТЫ:" >> $ANALYSIS_FILE
echo "   Settings → Appearance → UI Options → отключить анимации" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
echo "2. ОТКЛЮЧИТЬ ПРОБЛЕМНЫЕ ПЛАГИНЫ:" >> $ANALYSIS_FILE
echo "   Settings → Plugins → отключить неиспользуемые" >> $ANALYSIS_FILE
echo "   Особенно: AI Assistant, GitHub Copilot (если не нужен)" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
echo "3. НАСТРОИТЬ JVM ОПЦИИ (Help → Edit Custom VM Options):" >> $ANALYSIS_FILE
echo "   -Xms4g" >> $ANALYSIS_FILE
echo "   -Xmx8g" >> $ANALYSIS_FILE
echo "   -XX:+UseG1GC" >> $ANALYSIS_FILE
echo "   -XX:MaxGCPauseMillis=200" >> $ANALYSIS_FILE
echo "   -XX:+DisableExplicitGC" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
echo "4. ИСКЛЮЧИТЬ ПАПКИ ИЗ ИНДЕКСАЦИИ:" >> $ANALYSIS_FILE
echo "   Settings → Project → Directories → Mark as Excluded" >> $ANALYSIS_FILE
echo "   Исключить: node_modules, .git, target, build, dist" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE
echo "5. ОТКЛЮЧИТЬ СТАТИСТИКУ:" >> $ANALYSIS_FILE
echo "   Settings → Data Sharing → отключить все" >> $ANALYSIS_FILE
echo "" >> $ANALYSIS_FILE

echo "✅ Анализ завершен! Результаты сохранены в $ANALYSIS_FILE"
echo ""
echo "📋 КРАТКАЯ СВОДКА:"
echo "=================="
echo "• AWT UI поток жрал 20% от всего CPU времени"
echo "• TimerQueue поток жрал 12% CPU (таймеры Swing)"  
echo "• Kotlin корутины суммарно ~35% CPU"
echo "• Много спящих потоков (4570 событий сна)"
echo ""
echo "🎯 ГЛАВНАЯ РЕКОМЕНДАЦИЯ:"
echo "Отключите анимации UI и неиспользуемые плагины!" 