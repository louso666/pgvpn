# Управление энергосбережением в Linux

## 📍 Основные места настройки энергосбережения:

### 1. **Конфигурация CPUPower**

- **Файл**: `/etc/default/cpupower`
- **Что контролирует**: Governor, частоты, perf_bias
- **Текущие настройки**:
  - `governor='powersave'` - режим энергосбережения
  - `perf_bias=6` - баланс производительность/энергосбережение (0-15)

### 2. **Intel P-State настройки**

- **Каталог**: `/sys/devices/system/cpu/intel_pstate/`
- **Что контролирует**: Динамические настройки производительности
- **Ключевые параметры**:
  - `max_perf_pct` - максимальная производительность (%)
  - `min_perf_pct` - минимальная производительность (%)
  - `no_turbo` - отключение Turbo Boost (0/1)
  - `energy_efficiency` - энергоэффективность (0/1)

### 3. **Energy Performance Preference**

- **Путь**: `/sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference`
- **Режимы**: `performance`, `balance_performance`, `balance_power`, `power`
- **Текущий**: `balance_performance` ← поэтому CPU работает на высоких частотах

## 🔧 Как настроить:

### Быстрая настройка максимального энергосбережения:

```bash
./power-save-config.sh
```

### Возврат к сбалансированным настройкам:

```bash
./power-performance-config.sh
```

### Постоянная настройка через cpupower:

```bash
# Скопировать конфигурацию максимального энергосбережения
sudo cp cpupower-powersave.conf /etc/default/cpupower

# Перезапустить службу
sudo systemctl restart cpupower.service
```

## 📊 Мониторинг:

### Проверить текущие настройки:

```bash
# Частоты и governor
cpupower frequency-info

# Energy Performance Preference
cat /sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference | uniq

# Intel P-State статус
cat /sys/devices/system/cpu/intel_pstate/max_perf_pct
cat /sys/devices/system/cpu/intel_pstate/no_turbo
```

### Мониторинг в реальном времени:

```bash
# Температура
watch -n 1 sensors

# Частоты всех ядер
watch -n 1 "grep MHz /proc/cpuinfo"

# Использование powertop для анализа
sudo powertop
```

## ⚠️ Важные замечания:

1. **Проблема**: У вас governor=powersave, но процессор работает на высоких частотах
2. **Причина**: energy_performance_preference=balance_performance
3. **Решение**: Изменить на 'power' или 'balance_power'

## 🎯 Рекомендации:

### Для максимального энергосбережения:

- Energy Performance Preference: `power`
- Max Performance: `60%`
- Turbo Boost: отключить
- Energy Efficiency: включить

### Для сбалансированного режима:

- Energy Performance Preference: `balance_power`
- Max Performance: `100%`
- Turbo Boost: включить
- Energy Efficiency: по желанию
