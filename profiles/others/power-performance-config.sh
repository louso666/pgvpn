#!/bin/bash

# Скрипт для возврата к сбалансированным настройкам производительности

echo "=== Возврат к сбалансированным настройкам ==="

# 1. Вернуть Energy Performance Preference
echo "1. Настраиваю Energy Performance Preference на balance_power..."
for cpu in /sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference; do
    if [ -w "$cpu" ]; then
        echo "balance_power" | sudo tee "$cpu" > /dev/null
        echo "  Настроено: $(basename $(dirname $(dirname $cpu)))"
    fi
done

# 2. Вернуть максимальную производительность
echo "2. Возвращаю максимальную производительность до 100%..."
echo "100" | sudo tee /sys/devices/system/cpu/intel_pstate/max_perf_pct > /dev/null

# 3. Включить Turbo Boost
echo "3. Включаю Turbo Boost..."
echo "0" | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo > /dev/null

# 4. Показать текущие настройки
echo ""
echo "=== Текущие настройки ==="
echo "Governor: $(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor)"
echo "Energy Performance: $(cat /sys/devices/system/cpu/cpu0/cpufreq/energy_performance_preference)"
echo "Max Performance: $(cat /sys/devices/system/cpu/intel_pstate/max_perf_pct)%"
echo "Min Performance: $(cat /sys/devices/system/cpu/intel_pstate/min_perf_pct)%"
echo "Turbo Boost отключен: $(cat /sys/devices/system/cpu/intel_pstate/no_turbo)"
echo "Energy Efficiency: $(cat /sys/devices/system/cpu/intel_pstate/energy_efficiency)" 