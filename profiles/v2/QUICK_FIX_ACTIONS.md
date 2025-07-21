# БЫСТРЫЕ ДЕЙСТВИЯ ДЛЯ ИСПРАВЛЕНИЯ ВЫСОКОГО CPU В INTELLIJ

## 🚨 КРИТИЧЕСКИЙ ДИАГНОЗ

**DefaultDispatcher-worker-1** потребляет 43% от всего CPU из-за избыточной индексации файлов!

## ⚡ НЕМЕДЛЕННЫЕ ДЕЙСТВИЯ (5 минут)

### 1. ИСКЛЮЧИТЬ ПАПКИ ИЗ ИНДЕКСАЦИИ

```
Settings → Project Structure → Modules → Sources → Mark as Excluded:
- node_modules/
- .git/
- target/ build/ dist/
- vendor/
- .idea/
- все большие папки с зависимостями
```

### 2. ОТКЛЮЧИТЬ ПРОБЛЕМНЫЕ ПЛАГИНЫ

```
Settings → Plugins → Отключить:
- AI Assistant
- GitHub Copilot
- Kotlin (если не нужен)
- Go plugin
- все неиспользуемые плагины
```

### 3. НАСТРОИТЬ JVM ПАРАМЕТРЫ

```
Help → Edit Custom VM Options → Добавить:
-Xms4g
-Xmx8g
-XX:+UseG1GC
-XX:MaxGCPauseMillis=200
-XX:G1HeapRegionSize=32m
-XX:+DisableExplicitGC
```

### 4. ОТКЛЮЧИТЬ UI АНИМАЦИИ

```
Settings → Appearance & Behavior → Appearance → UI Options
- Отключить все анимации
- Отключить window animations
```

### 5. ОТКЛЮЧИТЬ АВТОСБОРКУ

```
Settings → Build → Compiler
- Снять "Build project automatically"
- Снять "Compile independent modules in parallel"
```

## 📊 ОЖИДАЕМЫЙ РЕЗУЛЬТАТ

CPU usage: 200% → 10-20% в idle состоянии

## 🔍 ПРОВЕРКА ЭФФЕКТИВНОСТИ

```bash
# Мониторинг CPU после изменений
htop
ps aux | grep java
```

## 💡 ЕСЛИ НЕ ПОМОГЛО

1. Перезапустить IntelliJ
2. Invalidate Caches and Restart
3. Создать новый профиль для дополнительного анализа
