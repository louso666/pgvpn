package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

const (
	botToken = "1481492430:AAHvpDUOG67c0QLFtZrhvKGBqBPJf2K8qV0"
	password = "_Texxi155775"
	dbPath   = "/root/bot.db"
	// Файлы для паттернов по типам маршрутизации
	patternFileDE  = "/root/site"     // для Германии (de) - обратная совместимость
	patternFileRU2 = "/root/site_ru2" // для России через pg2 (ru2)
	// ipset списки
	ipsetDE  = "proxied"   // для трафика через Германию
	ipsetRU2 = "pg2_proxy" // для трафика через pg2
)

// ConnRecord представляет информацию о неудачном соединении
type ConnRecord struct {
	Proto    string
	SrcIP    string
	SrcPort  uint16
	DstIP    string
	DstPort  uint16
	Attempts uint64
	Timeout  uint32
}

type Bot struct {
	api             *tgbotapi.BotAPI
	db              *sql.DB
	authorizedChats map[int64]bool
}

func NewBot() (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания бота: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("ошибка подключения к базе: %v", err)
	}

	bot := &Bot{
		api:             api,
		db:              db,
		authorizedChats: make(map[int64]bool),
	}

	if err := bot.initDB(); err != nil {
		return nil, fmt.Errorf("ошибка инициализации базы: %v", err)
	}

	// Загружаем авторизованные чаты
	bot.loadAuthorizedChats()

	// Настраиваем меню бота
	bot.setupBotCommands()

	log.Printf("Бот запущен как @%s", api.Self.UserName)
	return bot, nil
}

func (b *Bot) initDB() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS authorized_chats (
			chat_id INTEGER PRIMARY KEY
		)`,
		`CREATE TABLE IF NOT EXISTS dns_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL,
			ip TEXT NOT NULL,
			proxied BOOLEAN NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dns_domain ON dns_logs(domain)`,
		`CREATE INDEX IF NOT EXISTS idx_dns_ip ON dns_logs(ip)`,
	}

	for _, query := range queries {
		if _, err := b.db.Exec(query); err != nil {
			return fmt.Errorf("ошибка выполнения запроса: %v", err)
		}
	}

	return nil
}

func (b *Bot) loadAuthorizedChats() {
	rows, err := b.db.Query("SELECT chat_id FROM authorized_chats")
	if err != nil {
		log.Printf("Ошибка загрузки авторизованных чатов: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			log.Printf("Ошибка сканирования chat_id: %v", err)
			continue
		}
		b.authorizedChats[chatID] = true
	}

	log.Printf("Загружено %d авторизованных чатов", len(b.authorizedChats))
}

func (b *Bot) setupBotCommands() {
	commands := []tgbotapi.BotCommand{
		{Command: "pass", Description: "Авторизация в боте"},
		{Command: "wg", Description: "Создать WireGuard конфиг + файл"},
		{Command: "add_de", Description: "Добавить сайт в список DE (Германия)"},
		{Command: "add_ru2", Description: "Добавить сайт в список RU2 (через pg2)"},
		{Command: "remove_de", Description: "Удалить сайт из списка DE"},
		{Command: "remove_ru2", Description: "Удалить сайт из списка RU2"},
		{Command: "site", Description: "Показать все паттерны или IP по доменам"},
		{Command: "de", Description: "Показать только DE паттерны/домены"},
		{Command: "ru", Description: "Показать только RU домены (прямые)"},
		{Command: "ru2", Description: "Показать только RU2 паттерны/домены"},
		{Command: "conn", Description: "Показать заблокированные соединения"},
		{Command: "log", Description: "Показать последние N доменов (обычные)"},
		{Command: "help", Description: "Показать справку по командам"},
	}

	config := tgbotapi.NewSetMyCommands(commands...)
	if _, err := b.api.Request(config); err != nil {
		log.Printf("Ошибка установки команд бота: %v", err)
	}
}

func (b *Bot) isAuthorized(chatID int64) bool {
	return b.authorizedChats[chatID]
}

func (b *Bot) authorize(chatID int64) error {
	b.authorizedChats[chatID] = true

	_, err := b.db.Exec("INSERT OR IGNORE INTO authorized_chats (chat_id) VALUES (?)", chatID)
	if err != nil {
		return fmt.Errorf("ошибка сохранения авторизации: %v", err)
	}

	return nil
}

func (b *Bot) handlePassCommand(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)
	if len(args) < 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Использование: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	if args[1] == password {
		if err := b.authorize(message.Chat.ID); err != nil {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка авторизации")
			b.api.Send(msg)
			return
		}

		msg := tgbotapi.NewMessage(message.Chat.ID, "✅ Авторизация успешна! Теперь вы можете использовать команды бота.")
		b.api.Send(msg)
	} else {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Неверный пароль")
		b.api.Send(msg)
	}
}

func (b *Bot) handleWgCommand(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	args := strings.Fields(message.Text)
	if len(args) < 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Использование: /wg <username>")
		b.api.Send(msg)
		return
	}

	username := args[1]
	trimmedUsername := strings.TrimSpace(username)

	log.Printf("Выполняем команду: /root/wg %s", trimmedUsername)

	// Выполняем команду /root/wg
	cmd := exec.Command("/root/wg", trimmedUsername)
	output, err := cmd.CombinedOutput()

	log.Printf("Команда завершена с кодом выхода: %v", err)
	log.Printf("Полный вывод команды: %s", string(output))

	if err != nil {
		errorMsg := fmt.Sprintf("❌ Ошибка создания конфига для %s:\nКод ошибки: %v\nВывод команды:\n%s",
			trimmedUsername, err, string(output))
		log.Printf("Ошибка выполнения /root/wg: %s", errorMsg)

		msg := tgbotapi.NewMessage(message.Chat.ID, errorMsg)
		b.api.Send(msg)
		return
	}

	config := string(output)
	log.Printf("Конфиг успешно создан для пользователя %s, размер: %d байт", trimmedUsername, len(config))

	// Отправляем конфиг как текст
	msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("🔐 WireGuard конфиг для %s:\n\n```\n%s\n```", username, config))
	msg.ParseMode = "Markdown"
	b.api.Send(msg)

	// Отправляем как файл
	file := tgbotapi.FileBytes{
		Name:  "wg200.conf",
		Bytes: []byte(config),
	}

	doc := tgbotapi.NewDocument(message.Chat.ID, file)
	doc.Caption = fmt.Sprintf("WireGuard конфиг для %s", username)
	b.api.Send(doc)
}

func (b *Bot) handleAddDeCommand(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	args := strings.Fields(message.Text)
	if len(args) < 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Использование: /add_de <паттерн>")
		b.api.Send(msg)
		return
	}

	pattern := args[1]

	// Удаляем из RU2 списка если есть там
	b.removePatternFromOtherFile(pattern, "ru2")

	// Добавляем в DE файл
	if err := b.addPatternToFile(pattern); err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ Ошибка добавления паттерна: %v", err))
		b.api.Send(msg)
		return
	}

	// Добавляем исторические IP в DE ipset
	ips := b.getHistoricalIPs(pattern)
	added := 0
	for _, ip := range ips {
		if err := b.addIPToIpset(ip); err == nil {
			added++
		}
		// Удаляем из RU2 ipset если был там
		b.removeIPFromIpsetRU2(ip)
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("✅ Паттерн '%s' добавлен в DE список. Добавлено %d IP из истории в ipset.", pattern, added))
	b.api.Send(msg)
}

func (b *Bot) handleAddRU2Command(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	args := strings.Fields(message.Text)
	if len(args) < 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Использование: /add_ru2 <паттерн>")
		b.api.Send(msg)
		return
	}

	pattern := args[1]

	// Удаляем из DE списка если есть там
	b.removePatternFromOtherFile(pattern, "de")

	// Добавляем в RU2 файл
	if err := b.addPatternToFileRU2(pattern); err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ Ошибка добавления паттерна: %v", err))
		b.api.Send(msg)
		return
	}

	// Добавляем исторические IP в RU2 ipset
	ips := b.getHistoricalIPs(pattern)
	added := 0
	for _, ip := range ips {
		if err := b.addIPToIpsetRU2(ip); err == nil {
			added++
		}
		// Удаляем из DE ipset если был там
		b.removeIPFromIpset(ip)
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("✅ Паттерн '%s' добавлен в RU2 список. Добавлено %d IP из истории в ipset.", pattern, added))
	b.api.Send(msg)
}

func (b *Bot) handleRemoveDeCommand(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	args := strings.Fields(message.Text)
	if len(args) < 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Использование: /remove_de <паттерн>")
		b.api.Send(msg)
		return
	}

	pattern := args[1]

	// Получаем IP для удаления из DE ipset
	ips := b.getHistoricalIPs(pattern)

	// Удаляем из DE файла
	if err := b.removePatternFromFile(pattern); err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ Ошибка удаления паттерна: %v", err))
		b.api.Send(msg)
		return
	}

	// Удаляем IP из DE ipset
	removed := 0
	for _, ip := range ips {
		if err := b.removeIPFromIpset(ip); err == nil {
			removed++
		}
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("✅ Паттерн '%s' удален из DE списка. Удалено %d IP из ipset.", pattern, removed))
	b.api.Send(msg)
}

func (b *Bot) handleRemoveRU2Command(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	args := strings.Fields(message.Text)
	if len(args) < 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Использование: /remove_ru2 <паттерн>")
		b.api.Send(msg)
		return
	}

	pattern := args[1]

	// Получаем IP для удаления из RU2 ipset
	ips := b.getHistoricalIPs(pattern)

	// Удаляем из RU2 файла
	if err := b.removePatternFromFileRU2(pattern); err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ Ошибка удаления паттерна: %v", err))
		b.api.Send(msg)
		return
	}

	// Удаляем IP из RU2 ipset
	removed := 0
	for _, ip := range ips {
		if err := b.removeIPFromIpsetRU2(ip); err == nil {
			removed++
		}
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("✅ Паттерн '%s' удален из RU2 списка. Удалено %d IP из ipset.", pattern, removed))
	b.api.Send(msg)
}

func (b *Bot) handleSiteCommand(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	args := strings.Fields(message.Text)

	// Если нет параметров - показываем все паттерны
	if len(args) < 2 {
		b.showAllPatterns(message.Chat.ID)
		return
	}

	pattern := args[1]
	domainIPs := b.getHistoricalIPsWithDomains(pattern)

	if len(domainIPs) == 0 {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ IP адреса для паттерна '%s' не найдены", pattern))
		b.api.Send(msg)
		return
	}

	// Подсчитываем общее количество IP
	totalIPs := 0
	for _, ips := range domainIPs {
		totalIPs += len(ips)
	}

	// Создаем HTML сообщения с ограничением по размеру
	messages := b.createSiteMessages(pattern, domainIPs, totalIPs)

	// Отправляем сообщения
	for _, msgText := range messages {
		msg := tgbotapi.NewMessage(message.Chat.ID, msgText)
		msg.ParseMode = "HTML"
		b.api.Send(msg)

		// Небольшая задержка между сообщениями
		time.Sleep(100 * time.Millisecond)
	}
}

func (b *Bot) handleDeCommand(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	args := strings.Fields(message.Text)

	// Если нет параметров - показываем все DE паттерны
	if len(args) < 2 {
		b.showPatternsDE(message.Chat.ID)
		return
	}

	// Показываем IP только для указанного паттерна из DE списка
	pattern := args[1]
	b.showPatternDetails(message.Chat.ID, pattern, "de")
}

func (b *Bot) handleRuCommand(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	args := strings.Fields(message.Text)

	// Если нет параметров - показываем все RU домены (домены не в списках)
	if len(args) < 2 {
		b.showPatternsRU(message.Chat.ID)
		return
	}

	// Показываем IP только для указанного паттерна, который идет напрямую
	pattern := args[1]
	b.showPatternDetails(message.Chat.ID, pattern, "ru")
}

func (b *Bot) handleRU2Command(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	args := strings.Fields(message.Text)

	// Если нет параметров - показываем все RU2 паттерны
	if len(args) < 2 {
		b.showPatternsRU2(message.Chat.ID)
		return
	}

	// Показываем IP только для указанного паттерна из RU2 списка
	pattern := args[1]
	b.showPatternDetails(message.Chat.ID, pattern, "ru2")
}

func (b *Bot) handleConnCommand(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	failedConnections := b.getFailedConnections()

	if len(failedConnections) == 0 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "✅ Заблокированных соединений за последние 2 минуты не найдено")
		b.api.Send(msg)
		return
	}

	// Подсчитываем общее количество IP
	totalIPs := 0
	for _, ips := range failedConnections {
		totalIPs += len(ips)
	}

	// Создаем HTML сообщения с ограничением по размеру
	messages := b.createConnMessages(failedConnections, totalIPs)

	// Отправляем сообщения
	for _, msgText := range messages {
		msg := tgbotapi.NewMessage(message.Chat.ID, msgText)
		msg.ParseMode = "HTML"
		b.api.Send(msg)

		// Небольшая задержка между сообщениями
		time.Sleep(100 * time.Millisecond)
	}
}

func (b *Bot) handleLogCommand(message *tgbotapi.Message) {
	if !b.isAuthorized(message.Chat.ID) {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>")
		b.api.Send(msg)
		return
	}

	// Значение по умолчанию
	limit := 10

	args := strings.Fields(message.Text)
	if len(args) >= 2 {
		if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
			limit = v
		}
	}

	// Запрашиваем из базы уникальные домены (не проксируемые)
	rows, err := b.db.Query(`SELECT domain, MAX(timestamp) AS ts
		FROM dns_logs
		WHERE proxied = 0
		GROUP BY domain
		ORDER BY ts DESC
		LIMIT ?`, limit)
	if err != nil {
		log.Printf("DB query failed /log: %v", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Ошибка запроса к базе")
		b.api.Send(msg)
		return
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var domain string
		var ts string
		if err := rows.Scan(&domain, &ts); err == nil {
			domains = append(domains, domain)
		}
	}

	if len(domains) == 0 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "📝 Домены не найдены")
		b.api.Send(msg)
		return
	}

	response := fmt.Sprintf("🕒 Последние %d доменов (обычные):\n\n", len(domains))
	for i, d := range domains {
		response += fmt.Sprintf("%2d. <code>%s</code>\n", i+1, d)
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, response)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleHelpCommand(message *tgbotapi.Message) {
	help := `🤖 DNS Proxy Bot

📋 Доступные команды:

/pass <пароль> - Авторизация в боте
/wg <username> - Создать WireGuard конфиг + файл

🌍 Управление маршрутизацией:
/add_de <паттерн> - Добавить сайт в список DE (через Германию)
/add_ru2 <паттерн> - Добавить сайт в список RU2 (через pg2)
/remove_de <паттерн> - Удалить сайт из списка DE
/remove_ru2 <паттерн> - Удалить сайт из списка RU2

📊 Просмотр:
/site [паттерн] - Показать все паттерны или IP по доменам
/de [паттерн] - Показать только DE паттерны/домены
/ru [паттерн] - Показать только RU домены (прямые)
/ru2 [паттерн] - Показать только RU2 паттерны/домены
/conn - Показать заблокированные соединения
/log [n] - Показать последние N доменов (обычные)
/help - Показать эту справку

🛣️ Маршруты IP:
[de🇩🇪] - через Германию (p.nirhub.ru)
[ru2🇷🇺] - через pg2 (второй российский сервер)
[ru🇷🇺] - напрямую из России (pg.gena.host)

📝 Примеры:
/add_de figma    # добавит figma в список через Германию
/add_ru2 vk      # добавит vk в список через pg2
/de you          # покажет youtube из DE списка
/ru2             # покажет все RU2 паттерны

💡 При добавлении в один список паттерн автоматически удаляется из другого`

	msg := tgbotapi.NewMessage(message.Chat.ID, help)
	b.api.Send(msg)
}

func (b *Bot) addPatternToFile(pattern string) error {
	file, err := os.OpenFile(patternFileDE, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(pattern + "\n")
	return err
}

func (b *Bot) addPatternToFileRU2(pattern string) error {
	file, err := os.OpenFile(patternFileRU2, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(pattern + "\n")
	return err
}

func (b *Bot) removePatternFromFile(pattern string) error {
	content, err := os.ReadFile(patternFileDE)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string

	for _, line := range lines {
		if strings.TrimSpace(line) != pattern {
			newLines = append(newLines, line)
		}
	}

	return os.WriteFile(patternFileDE, []byte(strings.Join(newLines, "\n")), 0644)
}

func (b *Bot) removePatternFromFileRU2(pattern string) error {
	content, err := os.ReadFile(patternFileRU2)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string

	for _, line := range lines {
		if strings.TrimSpace(line) != pattern {
			newLines = append(newLines, line)
		}
	}

	return os.WriteFile(patternFileRU2, []byte(strings.Join(newLines, "\n")), 0644)
}

// removePatternFromOtherFile удаляет паттерн из файла указанного типа
func (b *Bot) removePatternFromOtherFile(pattern string, fromFileType string) error {
	var filepath string
	switch fromFileType {
	case "de":
		filepath = patternFileDE
	case "ru2":
		filepath = patternFileRU2
	default:
		return nil // неизвестный тип файла
	}

	content, err := os.ReadFile(filepath)
	if err != nil {
		// Файл не существует - это нормально
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string

	for _, line := range lines {
		if strings.TrimSpace(line) != pattern {
			newLines = append(newLines, line)
		}
	}

	return os.WriteFile(filepath, []byte(strings.Join(newLines, "\n")), 0644)
}

func (b *Bot) getHistoricalIPs(pattern string) []string {
	domainIPs := b.getHistoricalIPsWithDomains(pattern)

	var ips []string
	for _, domainIPs := range domainIPs {
		ips = append(ips, domainIPs...)
	}

	return ips
}

func (b *Bot) getHistoricalIPsWithDomains(pattern string) map[string][]string {
	// Загружаем JSON файл
	data, err := os.ReadFile(mapFile)
	if err != nil {
		return make(map[string][]string)
	}

	var domainMap DomainIPMap
	if err := json.Unmarshal(data, &domainMap); err != nil {
		return make(map[string][]string)
	}

	result := make(map[string][]string)
	for domain, domainIPs := range domainMap {
		if strings.Contains(domain, pattern) {
			result[domain] = domainIPs
		}
	}

	return result
}

// showAllPatterns показывает все текущие паттерны с количеством IP
func (b *Bot) showAllPatterns(chatID int64) {
	// Загружаем паттерны DE
	patternsDE, err := b.loadPatterns()
	if err != nil {
		patternsDE = []string{}
	}

	// Загружаем паттерны RU2
	patternsRU2, err := b.loadPatternsRU2()
	if err != nil {
		patternsRU2 = []string{}
	}

	totalPatterns := len(patternsDE) + len(patternsRU2)
	if totalPatterns == 0 {
		msg := tgbotapi.NewMessage(chatID, "📝 Паттерны не найдены")
		b.api.Send(msg)
		return
	}

	response := fmt.Sprintf("📝 <b>Все паттерны</b> (%d):\n\n", totalPatterns)

	// DE паттерны
	if len(patternsDE) > 0 {
		response += "🇩🇪 <b>DE (через Германию):</b>\n"
		for _, pattern := range patternsDE {
			domainIPs := b.getHistoricalIPsWithDomains(pattern)
			totalIPs := 0
			for _, ips := range domainIPs {
				totalIPs += len(ips)
			}
			response += fmt.Sprintf("   🔹 <code>%s</code> — %d IP\n", pattern, totalIPs)
		}
		response += "\n"
	}

	// RU2 паттерны
	if len(patternsRU2) > 0 {
		response += "🇷🇺 <b>RU2 (через pg2):</b>\n"
		for _, pattern := range patternsRU2 {
			domainIPs := b.getHistoricalIPsWithDomains(pattern)
			totalIPs := 0
			for _, ips := range domainIPs {
				totalIPs += len(ips)
			}
			response += fmt.Sprintf("   🔹 <code>%s</code> — %d IP\n", pattern, totalIPs)
		}
		response += "\n"
	}

	response += "💡 Используйте <code>/de</code>, <code>/ru2</code>, <code>/ru</code> для детального просмотра"

	msg := tgbotapi.NewMessage(chatID, response)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

// showPatternsDE показывает только DE паттерны
func (b *Bot) showPatternsDE(chatID int64) {
	patterns, err := b.loadPatterns()
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки DE паттернов")
		b.api.Send(msg)
		return
	}

	if len(patterns) == 0 {
		msg := tgbotapi.NewMessage(chatID, "📝 DE паттерны не найдены")
		b.api.Send(msg)
		return
	}

	response := fmt.Sprintf("🇩🇪 <b>DE паттерны (через Германию)</b> (%d):\n\n", len(patterns))

	for _, pattern := range patterns {
		domainIPs := b.getHistoricalIPsWithDomains(pattern)
		totalIPs := 0
		for _, ips := range domainIPs {
			totalIPs += len(ips)
		}
		response += fmt.Sprintf("🔹 <code>%s</code> — %d IP\n", pattern, totalIPs)
	}

	response += "\n💡 Используйте <code>/de &lt;паттерн&gt;</code> для детальной информации"

	msg := tgbotapi.NewMessage(chatID, response)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

// showPatternsRU2 показывает только RU2 паттерны
func (b *Bot) showPatternsRU2(chatID int64) {
	patterns, err := b.loadPatternsRU2()
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки RU2 паттернов")
		b.api.Send(msg)
		return
	}

	if len(patterns) == 0 {
		msg := tgbotapi.NewMessage(chatID, "📝 RU2 паттерны не найдены")
		b.api.Send(msg)
		return
	}

	response := fmt.Sprintf("🇷🇺 <b>RU2 паттерны (через pg2)</b> (%d):\n\n", len(patterns))

	for _, pattern := range patterns {
		domainIPs := b.getHistoricalIPsWithDomains(pattern)
		totalIPs := 0
		for _, ips := range domainIPs {
			totalIPs += len(ips)
		}
		response += fmt.Sprintf("🔹 <code>%s</code> — %d IP\n", pattern, totalIPs)
	}

	response += "\n💡 Используйте <code>/ru2 &lt;паттерн&gt;</code> для детальной информации"

	msg := tgbotapi.NewMessage(chatID, response)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

// showPatternsRU показывает домены, которые идут напрямую (не в списках)
func (b *Bot) showPatternsRU(chatID int64) {
	response := "🇷🇺 <b>RU домены (прямое соединение)</b>:\n\n"
	response += "Эти домены не находятся в списках DE или RU2 и идут напрямую через pg.gena.host.\n\n"
	response += "💡 Для просмотра конкретного домена используйте: <code>/ru &lt;паттерн&gt;</code>\n"
	response += "💡 Для перевода в списки используйте: <code>/add_de</code> или <code>/add_ru2</code>"

	msg := tgbotapi.NewMessage(chatID, response)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

// showPatternDetails показывает детальную информацию о паттерне в зависимости от типа
func (b *Bot) showPatternDetails(chatID int64, pattern string, routeType string) {
	domainIPs := b.getHistoricalIPsWithDomains(pattern)

	if len(domainIPs) == 0 {
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ IP адреса для паттерна '%s' не найдены", pattern))
		b.api.Send(msg)
		return
	}

	// Фильтруем IP в зависимости от типа маршрута
	filteredDomainIPs := make(map[string][]string)
	for domain, ips := range domainIPs {
		var filteredIPs []string
		for _, ip := range ips {
			ipRoute := b.getIPRouteStatus(ip)

			// Проверяем соответствие типу маршрута
			switch routeType {
			case "de":
				if strings.Contains(ipRoute, "de🇩🇪") {
					filteredIPs = append(filteredIPs, ip)
				}
			case "ru2":
				if strings.Contains(ipRoute, "ru2🇷🇺") {
					filteredIPs = append(filteredIPs, ip)
				}
			case "ru":
				if strings.Contains(ipRoute, "ru🇷🇺") && !strings.Contains(ipRoute, "ru2🇷🇺") {
					filteredIPs = append(filteredIPs, ip)
				}
			}
		}
		if len(filteredIPs) > 0 {
			filteredDomainIPs[domain] = filteredIPs
		}
	}

	if len(filteredDomainIPs) == 0 {
		routeNames := map[string]string{
			"de":  "DE (через Германию)",
			"ru2": "RU2 (через pg2)",
			"ru":  "RU (напрямую)",
		}
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ IP адреса для паттерна '%s' с маршрутом %s не найдены", pattern, routeNames[routeType]))
		b.api.Send(msg)
		return
	}

	// Подсчитываем общее количество IP
	totalIPs := 0
	for _, ips := range filteredDomainIPs {
		totalIPs += len(ips)
	}

	// Создаем HTML сообщения с ограничением по размеру
	messages := b.createSiteMessages(pattern, filteredDomainIPs, totalIPs)

	// Отправляем сообщения
	for _, msgText := range messages {
		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = "HTML"
		b.api.Send(msg)

		// Небольшая задержка между сообщениями
		time.Sleep(100 * time.Millisecond)
	}
}

// loadPatterns загружает паттерны из файла
func (b *Bot) loadPatterns() ([]string, error) {
	content, err := os.ReadFile(patternFileDE)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(content), "\n")
	var patterns []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}

	return patterns, nil
}

// loadPatternsRU2 загружает паттерны RU2 из файла
func (b *Bot) loadPatternsRU2() ([]string, error) {
	content, err := os.ReadFile(patternFileRU2)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // файл не существует - возвращаем пустой список
		}
		return nil, err
	}

	lines := strings.Split(string(content), "\n")
	var patterns []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}

	return patterns, nil
}

// createSiteMessages создает HTML сообщения с ограничениями по размеру
func (b *Bot) createSiteMessages(pattern string, domainIPs map[string][]string, totalIPs int) []string {
	const maxMessageSize = 4000 // Оставляем запас до 4096

	var messages []string
	currentMessage := fmt.Sprintf("🌐 <b>IP адреса для паттерна '%s'</b> (%d доменов, %d IP):\n\n",
		pattern, len(domainIPs), totalIPs)

	for domain, ips := range domainIPs {
		// Создаем блок для домена
		domainBlock := b.createDomainBlock(domain, ips)

		// Проверяем размер сообщения
		if len(currentMessage)+len(domainBlock) > maxMessageSize {
			// Добавляем текущее сообщение в список
			messages = append(messages, currentMessage)
			// Начинаем новое сообщение
			currentMessage = fmt.Sprintf("🌐 <b>IP адреса для паттерна '%s'</b> (продолжение):\n\n", pattern)
		}

		currentMessage += domainBlock
	}

	// Добавляем последнее сообщение
	if len(strings.TrimSpace(currentMessage)) > 0 {
		messages = append(messages, currentMessage)
	}

	return messages
}

// getIPRouteStatus определяет маршрут для IP адреса
func (b *Bot) getIPRouteStatus(ip string) string {
	// Проверяем есть ли IP в ipset "proxied" (DE)
	cmd := exec.Command("ipset", "test", ipsetDE, ip)
	err := cmd.Run()
	if err == nil {
		return "de🇩🇪"
	}

	// Проверяем есть ли IP в ipset "pg2_proxy" (RU2)
	cmd = exec.Command("ipset", "test", ipsetRU2, ip)
	err = cmd.Run()
	if err == nil {
		return "ru2🇷🇺"
	}

	// IP не в ipset'ах - идет напрямую через Россию
	return "ru🇷🇺"
}

// createDomainBlock создает HTML блок для домена с IP адресами
func (b *Bot) createDomainBlock(domain string, ips []string) string {
	const maxIPsToShow = 20
	ipCount := len(ips)

	block := fmt.Sprintf("🌍 <b>%s</b> — %d IP\n", domain, ipCount)

	// Если IP меньше 5, не сворачиваем
	if ipCount <= 5 {
		for _, ip := range ips {
			routeStatus := b.getIPRouteStatus(ip)
			block += fmt.Sprintf("   • <code>%s</code> [%s]\n", ip, routeStatus)
		}
	} else {
		// Создаем сворачиваемый блок
		ipList := ""
		displayIPs := ips
		hasMore := false

		if ipCount > maxIPsToShow {
			displayIPs = ips[:maxIPsToShow]
			hasMore = true
		}

		for _, ip := range displayIPs {
			routeStatus := b.getIPRouteStatus(ip)
			ipList += fmt.Sprintf("   • <code>%s</code> [%s]\n", ip, routeStatus)
		}

		if hasMore {
			ipList += fmt.Sprintf("   ... и еще %d IP адресов", ipCount-maxIPsToShow)
		}

		block += fmt.Sprintf("<blockquote expandable>%s</blockquote>\n", ipList)
	}

	block += "\n"
	return block
}

// createConnMessages создает HTML сообщения для неудачных подключений с ограничениями по размеру
func (b *Bot) createConnMessages(failedConnections map[string][]string, totalIPs int) []string {
	const maxMessageSize = 4000 // Оставляем запас до 4096

	var messages []string
	currentMessage := fmt.Sprintf("🚫 <b>Заблокированные соединения за последние 2 минуты</b> (%d записей, %d IP):\n\n",
		len(failedConnections), totalIPs)

	for domain, ips := range failedConnections {
		// Создаем блок для домена
		domainBlock := b.createDomainBlock(domain, ips)

		// Проверяем размер сообщения
		if len(currentMessage)+len(domainBlock) > maxMessageSize {
			// Добавляем текущее сообщение в список
			messages = append(messages, currentMessage)
			// Начинаем новое сообщение
			currentMessage = "🚫 <b>Заблокированные соединения за последние 2 минуты</b> (продолжение):\n\n"
		}

		currentMessage += domainBlock
	}

	// Добавляем последнее сообщение
	if len(strings.TrimSpace(currentMessage)) > 0 {
		messages = append(messages, currentMessage)
	}

	return messages
}

func (b *Bot) addIPToIpset(ip string) error {
	cmd := exec.Command("ipset", "add", ipsetDE, ip, "-exist")
	return cmd.Run()
}

func (b *Bot) removeIPFromIpset(ip string) error {
	cmd := exec.Command("ipset", "del", ipsetDE, ip)
	return cmd.Run()
}

func (b *Bot) addIPToIpsetRU2(ip string) error {
	cmd := exec.Command("ipset", "add", ipsetRU2, ip, "-exist")
	return cmd.Run()
}

func (b *Bot) removeIPFromIpsetRU2(ip string) error {
	cmd := exec.Command("ipset", "del", ipsetRU2, ip)
	return cmd.Run()
}

func (b *Bot) getFailedConnections() map[string][]string {
	log.Printf("Получение недавних заблокированных подключений через conntrack")

	result := make(map[string][]string)
	currentTime := time.Now()
	cutoffTime := currentTime.Add(-2 * time.Minute) // Только за последние 2 минуты

	// Получаем записи conntrack
	flows, err := netlink.ConntrackTableList(netlink.ConntrackTable, unix.AF_INET)
	if err != nil {
		log.Printf("Ошибка получения conntrack данных: %v", err)
		return result
	}

	log.Printf("Получено %d записей conntrack", len(flows))

	// Загружаем паттерны, чтобы отличать проксируемые IP
	patterns, _ := b.loadPatterns()

	// Загружаем маппинг IP -> домены из нашего JSON файла
	proxiedIPs := make(map[string]bool)
	ipToDomain := make(map[string][]string)
	data, err := os.ReadFile(mapFile)
	if err == nil {
		var domainMap DomainIPMap
		if err := json.Unmarshal(data, &domainMap); err == nil {
			// Создаем маппинг проксируемых IP
			for domain, ips := range domainMap {
				// Определяем, является ли домен проксируемым
				isProxied := false
				for _, p := range patterns {
					if strings.Contains(domain, p) {
						isProxied = true
						break
					}
				}

				for _, ip := range ips {
					if isProxied {
						proxiedIPs[ip] = true // помечаем как проксируемый
					}
					ipToDomain[ip] = append(ipToDomain[ip], domain)
				}
			}
		}
	}

	// Фильтруем только недавние заблокированные соединения
	failedCount := 0
	recentCount := 0
	cutoffTimestamp := uint64(cutoffTime.Unix())

	for _, f := range flows {
		// Пропускаем соединения с ответными пакетами
		if f.Reverse.Packets != 0 {
			continue
		}

		// Проверяем время создания соединения (TimeStart в секундах Unix timestamp)
		if f.TimeStart != 0 && f.TimeStart < cutoffTimestamp {
			continue
		}

		// Признаки реальной блокировки:
		// 1. Несколько попыток подключения (больше 1 пакета)
		// 2. Либо долгий таймаут (больше 60 сек)
		if f.Forward.Packets < 4 && f.TimeOut < 60 {
			continue
		}

		recentCount++
		dstIP := f.Forward.DstIP.String()

		// Показываем только НЕпроксируемые IP (обычные соединения)
		if !proxiedIPs[dstIP] {
			failedCount++
			// Пытаемся найти домен по IP, если нет - используем IP как ключ
			if domains, found := ipToDomain[dstIP]; found {
				for _, domain := range domains {
					if result[domain] == nil {
						result[domain] = []string{}
					}
					if !b.containsIP(result[domain], dstIP) {
						result[domain] = append(result[domain], dstIP)
					}
				}
			}
			//  else {
			// 	// Для неизвестных IP используем сам IP как ключ
			// 	domainKey := dstIP
			// 	if result[domainKey] == nil {
			// 		result[domainKey] = []string{}
			// 	}
			// 	if !b.containsIP(result[domainKey], dstIP) {
			// 		result[domainKey] = append(result[domainKey], dstIP)
			// 	}
			// }
		}
	}

	log.Printf("Из %d недавних записей без ответов найдено %d потенциально заблокированных соединений к обычным IP, сгруппировано в %d записей",
		recentCount, failedCount, len(result))
	return result
}

// containsIP проверяет содержится ли IP в слайсе
func (b *Bot) containsIP(ips []string, targetIP string) bool {
	for _, ip := range ips {
		if ip == targetIP {
			return true
		}
	}
	return false
}

func (b *Bot) Run() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		if !update.Message.IsCommand() {
			continue
		}

		switch update.Message.Command() {
		case "start", "help":
			b.handleHelpCommand(update.Message)
		case "pass":
			b.handlePassCommand(update.Message)
		case "wg":
			b.handleWgCommand(update.Message)
		case "add_de":
			b.handleAddDeCommand(update.Message)
		case "add_ru2":
			b.handleAddRU2Command(update.Message)
		case "remove_de":
			b.handleRemoveDeCommand(update.Message)
		case "remove_ru2":
			b.handleRemoveRU2Command(update.Message)
		case "site":
			b.handleSiteCommand(update.Message)
		case "de":
			b.handleDeCommand(update.Message)
		case "ru":
			b.handleRuCommand(update.Message)
		case "ru2":
			b.handleRU2Command(update.Message)
		case "conn":
			b.handleConnCommand(update.Message)
		case "log":
			b.handleLogCommand(update.Message)
		default:
			if b.isAuthorized(update.Message.Chat.ID) {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "❌ Неизвестная команда. Используйте /help для справки.")
				b.api.Send(msg)
			}
		}
	}
}

func (b *Bot) Close() {
	if b.db != nil {
		b.db.Close()
	}
}

// StartBot запускает Telegram бота в отдельной горутине
func StartBot() {
	go func() {
		log.Printf("Запуск Telegram бота...")

		bot, err := NewBot()
		if err != nil {
			log.Printf("Ошибка создания бота: %v", err)
			return
		}
		defer bot.Close()

		log.Printf("Telegram бот запущен и готов к работе")
		bot.Run()
	}()
}
