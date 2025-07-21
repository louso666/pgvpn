#!/bin/bash

# Анализатор HTML профилей async-profiler

if [ $# -eq 0 ]; then
    echo "Использование: $0 <html-файл>"
    echo "Доступные файлы:"
    ls -1 profiling-results/*.html 2>/dev/null || echo "Файлы профилирования не найдены"
    exit 1
fi

HTML_FILE="$1"
if [ ! -f "$HTML_FILE" ]; then
    echo "Файл $HTML_FILE не найден"
    exit 1
fi

echo "🔍 Анализ профиля: $HTML_FILE"
echo "=============================================="

# Извлекаем данные из HTML
echo "📊 Общая информация:"
grep -o 'Wall clock profile' "$HTML_FILE" && echo "Тип: Wall clock профилирование"

echo -e "\n🔥 Топ методов по времени выполнения:"
echo "(извлекаем из HTML...)"

# Создаем временный файл для анализа
temp_file=$(mktemp)
grep -o 'f([^)]*' "$HTML_FILE" | head -20 > "$temp_file"

if [ -s "$temp_file" ]; then
    echo "Найдено $(wc -l < "$temp_file") записей в профиле"
else
    echo "⚠️  Не удалось извлечь данные из HTML. Попробуйте открыть файл в браузере:"
    echo "   firefox $HTML_FILE"
    echo "   или"
    echo "   google-chrome $HTML_FILE"
fi

rm "$temp_file"

echo -e "\n📈 Рекомендации:"
echo "1. Откройте файл в браузере для детального анализа flame graph"
echo "2. Ищите широкие полосы в flame graph - это методы с высоким потреблением CPU"
echo "3. Обратите внимание на методы IntelliJ:"
echo "   - com.intellij.codeInsight.*"
echo "   - com.github.copilot.*"
echo "   - jetbrains.exodus.*"

echo -e "\n🌐 Для просмотра в браузере:"
echo "firefox $(realpath "$HTML_FILE")"

# Дополнительный анализ размера файла
file_size=$(stat -c%s "$HTML_FILE")
if [ "$file_size" -gt 100000 ]; then
    echo -e "\n⚠️  Большой файл профиля ($file_size байт) - много активности!"
fi

echo -e "\n✅ Анализ завершен" 