#!/bin/bash

# Скрипт для настройки максимального энергосбережения
# Использовать с осторожностью - может снизить производительность

echo "=== Настройка максимального энергосбережения ==="

# 1. Изменить Energy Performance Preference на power
echo "1. Настраиваю Energy Performance Preference..."
for cpu in /sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference; do
    if [ -w "$cpu" ]; then
        echo "power" | sudo tee "$cpu" > /dev/null
        echo "  Настроено: $(basename $(dirname $(dirname $cpu)))"
    fi
done

# 2. Ограничить максимальную производительность
echo "2. Ограничиваю максимальную производительность до 60%..."
echo "60" | sudo tee /sys/devices/system/cpu/intel_pstate/max_perf_pct > /dev/null

# 3. Включить энергоэффективность
echo "3. Включаю энергоэффективность..."
echo "1" | sudo tee /sys/devices/system/cpu/intel_pstate/energy_efficiency > /dev/null

# 4. Отключить Turbo Boost (опционально)
echo "4. Отключаю Turbo Boost..."
echo "1" | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo > /dev/null

# 5. Показать текущие настройки
echo ""
echo "=== Текущие настройки ==="
echo "Governor: $(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor)"
echo "Energy Performance: $(cat /sys/devices/system/cpu/cpu0/cpufreq/energy_performance_preference)"
echo "Max Performance: $(cat /sys/devices/system/cpu/intel_pstate/max_perf_pct)%"
echo "Min Performance: $(cat /sys/devices/system/cpu/intel_pstate/min_perf_pct)%"
echo "Turbo Boost отключен: $(cat /sys/devices/system/cpu/intel_pstate/no_turbo)"
echo "Energy Efficiency: $(cat /sys/devices/system/cpu/intel_pstate/energy_efficiency)"

echo ""
echo "=== Текущие частоты ==="
cpupower frequency-info | grep "current CPU frequency" 