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

	// Файлы для паттернов по типам маршрутизации (ЕДИНСТВЕННОЕ МЕСТО)
	patternFileNL  = "/root/site_nl"  // для Амстердама (NL)
	patternFileUSA = "/root/site_usa" // для Америки (USA)

	// ipset списки (ЕДИНСТВЕННОЕ МЕСТО)
	ipsetNL  = "nl_proxy"
	ipsetUSA = "usa_proxy"
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

	bot.loadAuthorizedChats()
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
	for _, q := range queries {
		if _, err := b.db.Exec(q); err != nil {
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
		{Command: "add_nl", Description: "Добавить сайт в список NL (Амстердам)"},
		{Command: "add_usa", Description: "Добавить сайт в список USA (Америка)"},
		{Command: "remove_nl", Description: "Удалить сайт из списка NL"},
		{Command: "remove_usa", Description: "Удалить сайт из списка USA"},
		{Command: "site", Description: "Показать все паттерны или IP по доменам"},
		{Command: "nl", Description: "Показать только NL паттерны/домены"},
		{Command: "ru", Description: "Показать только RU домены (прямые)"},
		{Command: "usa", Description: "Показать только USA паттерны/домены"},
		{Command: "conn", Description: "Показать заблокированные соединения"},
		{Command: "log", Description: "Показать последние N доменов (обычные)"},
		{Command: "help", Description: "Показать справку по командам"},
	}
	if _, err := b.api.Request(tgbotapi.NewSetMyCommands(commands...)); err != nil {
		log.Printf("Ошибка установки команд бота: %v", err)
	}
}

func (b *Bot) isAuthorized(chatID int64) bool { return b.authorizedChats[chatID] }

func (b *Bot) authorize(chatID int64) error {
	b.authorizedChats[chatID] = true
	_, err := b.db.Exec("INSERT OR IGNORE INTO authorized_chats (chat_id) VALUES (?)", chatID)
	return err
}

func (b *Bot) handlePassCommand(m *tgbotapi.Message) {
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "Использование: /pass <пароль>"))
		return
	}
	if args[1] == password {
		if err := b.authorize(m.Chat.ID); err != nil {
			b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "Ошибка авторизации"))
			return
		}
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "✅ Авторизация успешна!"))
	} else {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Неверный пароль"))
	}
}

func (b *Bot) handleWgCommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "Использование: /wg <username>"))
		return
	}
	username := strings.TrimSpace(args[1])
	cmd := exec.Command("/root/wg", username)
	output, err := cmd.CombinedOutput()
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID,
			fmt.Sprintf("❌ Ошибка создания конфига для %s:\n%v\n%s", username, err, string(output))))
		return
	}
	cfg := string(output)
	msg := tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("🔐 WireGuard конфиг для %s:\n\n```\n%s\n```", username, cfg))
	msg.ParseMode = "Markdown"
	b.api.Send(msg)
	doc := tgbotapi.NewDocument(m.Chat.ID, tgbotapi.FileBytes{Name: "wg200.conf", Bytes: []byte(cfg)})
	doc.Caption = fmt.Sprintf("WireGuard конфиг для %s", username)
	b.api.Send(doc)
}

func (b *Bot) handleAddNLCommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "Использование: /add_nl <паттерн>"))
		return
	}
	pattern := args[1]
	_ = b.removePatternFromOtherFile(pattern, "usa")
	if err := b.addPatternToFileNL(pattern); err != nil {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("❌ Ошибка добавления паттерна: %v", err)))
		return
	}
	ips := b.getHistoricalIPs(pattern)
	added := 0
	for _, ip := range ips {
		if err := b.addIPToIpsetNL(ip); err == nil {
			added++
		}
		_ = b.removeIPFromIpsetUSA(ip)
	}
	b.api.Send(tgbotapi.NewMessage(m.Chat.ID,
		fmt.Sprintf("✅ Паттерн '%s' добавлен в NL. Добавлено %d IP из истории.", pattern, added)))
}

func (b *Bot) handleAddUSACommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "Использование: /add_usa <паттерн>"))
		return
	}
	pattern := args[1]
	_ = b.removePatternFromOtherFile(pattern, "nl")
	if err := b.addPatternToFileUSA(pattern); err != nil {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("❌ Ошибка добавления паттерна: %v", err)))
		return
	}
	ips := b.getHistoricalIPs(pattern)
	added := 0
	for _, ip := range ips {
		if err := b.addIPToIpsetUSA(ip); err == nil {
			added++
		}
		_ = b.removeIPFromIpsetNL(ip)
	}
	b.api.Send(tgbotapi.NewMessage(m.Chat.ID,
		fmt.Sprintf("✅ Паттерн '%s' добавлен в USA. Добавлено %d IP из истории.", pattern, added)))
}

func (b *Bot) handleRemoveNLCommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "Использование: /remove_nl <паттерн>"))
		return
	}
	pattern := args[1]
	ips := b.getHistoricalIPs(pattern)
	if err := b.removePatternFromFileNL(pattern); err != nil {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("❌ Ошибка удаления паттерна: %v", err)))
		return
	}
	removed := 0
	for _, ip := range ips {
		if err := b.removeIPFromIpsetNL(ip); err == nil {
			removed++
		}
	}
	b.api.Send(tgbotapi.NewMessage(m.Chat.ID,
		fmt.Sprintf("✅ Паттерн '%s' удален из NL. Удалено %d IP из ipset.", pattern, removed)))
}

func (b *Bot) handleRemoveUSACommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "Использование: /remove_usa <паттерн>"))
		return
	}
	pattern := args[1]
	ips := b.getHistoricalIPs(pattern)
	if err := b.removePatternFromFileUSA(pattern); err != nil {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("❌ Ошибка удаления паттерна: %v", err)))
		return
	}
	removed := 0
	for _, ip := range ips {
		if err := b.removeIPFromIpsetUSA(ip); err == nil {
			removed++
		}
	}
	b.api.Send(tgbotapi.NewMessage(m.Chat.ID,
		fmt.Sprintf("✅ Паттерн '%s' удален из USA. Удалено %d IP из ipset.", pattern, removed)))
}

func (b *Bot) handleSiteCommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.showAllPatterns(m.Chat.ID)
		return
	}
	pattern := args[1]
	domainIPs := b.getHistoricalIPsWithDomains(pattern)
	if len(domainIPs) == 0 {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("❌ IP адреса для паттерна '%s' не найдены", pattern)))
		return
	}
	totalIPs := 0
	for _, ips := range domainIPs {
		totalIPs += len(ips)
	}
	for _, msgText := range b.createSiteMessages(pattern, domainIPs, totalIPs) {
		msg := tgbotapi.NewMessage(m.Chat.ID, msgText)
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		time.Sleep(100 * time.Millisecond)
	}
}

func (b *Bot) handleNLCommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.showPatternsNL(m.Chat.ID)
		return
	}
	b.showPatternDetails(m.Chat.ID, args[1], "nl")
}

func (b *Bot) handleRuCommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.showPatternsRU(m.Chat.ID)
		return
	}
	b.showPatternDetails(m.Chat.ID, args[1], "ru")
}

func (b *Bot) handleUSACommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	args := strings.Fields(m.Text)
	if len(args) < 2 {
		b.showPatternsUSA(m.Chat.ID)
		return
	}
	b.showPatternDetails(m.Chat.ID, args[1], "usa")
}

func (b *Bot) handleConnCommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	failedConnections := b.getFailedConnections()
	if len(failedConnections) == 0 {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "✅ Заблокированных соединений за последние 2 минуты не найдено"))
		return
	}
	totalIPs := 0
	for _, ips := range failedConnections {
		totalIPs += len(ips)
	}
	for _, msgText := range b.createConnMessages(failedConnections, totalIPs) {
		msg := tgbotapi.NewMessage(m.Chat.ID, msgText)
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		time.Sleep(100 * time.Millisecond)
	}
}

func (b *Bot) handleLogCommand(m *tgbotapi.Message) {
	if !b.isAuthorized(m.Chat.ID) {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Сначала авторизуйтесь: /pass <пароль>"))
		return
	}
	limit := 10
	args := strings.Fields(m.Text)
	if len(args) >= 2 {
		if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
			limit = v
		}
	}
	rows, err := b.db.Query(`SELECT domain, MAX(timestamp) AS ts
		FROM dns_logs
		WHERE proxied = 0
		GROUP BY domain
		ORDER BY ts DESC
		LIMIT ?`, limit)
	if err != nil {
		log.Printf("DB query failed /log: %v", err)
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "❌ Ошибка запроса к базе"))
		return
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var d, ts string
		if err := rows.Scan(&d, &ts); err == nil {
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		b.api.Send(tgbotapi.NewMessage(m.Chat.ID, "📝 Домены не найдены"))
		return
	}
	resp := fmt.Sprintf("🕒 Последние %d доменов (обычные):\n\n", len(domains))
	for i, d := range domains {
		resp += fmt.Sprintf("%2d. <code>%s</code>\n", i+1, d)
	}
	msg := tgbotapi.NewMessage(m.Chat.ID, resp)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleHelpCommand(m *tgbotapi.Message) {
	help := `🤖 DNS Proxy Bot

📋 Команды:
/pass <пароль>, /wg <username>

/add_nl <паттерн>, /add_usa <паттерн>
/remove_nl <паттерн>, /remove_usa <паттерн>

/site [паттерн]
/nl [паттерн], /usa [паттерн], /ru [паттерн]
/conn
/log [n]
/help

🛣️ Маршруты:
[nl🇳🇱]  - через Амстердам
[usa🇺🇸] - через Америку
[ru🇷🇺]  - напрямую`
	b.api.Send(tgbotapi.NewMessage(m.Chat.ID, help))
}

// ====== работа с файлами паттернов ======

func (b *Bot) addPatternToFileNL(pattern string) error {
	f, err := os.OpenFile(patternFileNL, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(pattern + "\n")
	return err
}

func (b *Bot) addPatternToFileUSA(pattern string) error {
	f, err := os.OpenFile(patternFileUSA, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(pattern + "\n")
	return err
}

func (b *Bot) removePatternFromFileNL(pattern string) error {
	content, err := os.ReadFile(patternFileNL)
	if err != nil {
		return err
	}
	var newLines []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) != pattern {
			newLines = append(newLines, line)
		}
	}
	return os.WriteFile(patternFileNL, []byte(strings.Join(newLines, "\n")), 0644)
}

func (b *Bot) removePatternFromFileUSA(pattern string) error {
	content, err := os.ReadFile(patternFileUSA)
	if err != nil {
		return err
	}
	var newLines []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) != pattern {
			newLines = append(newLines, line)
		}
	}
	return os.WriteFile(patternFileUSA, []byte(strings.Join(newLines, "\n")), 0644)
}

func (b *Bot) removePatternFromOtherFile(pattern string, which string) error {
	var path string
	switch which {
	case "nl":
		path = patternFileNL
	case "usa":
		path = patternFileUSA
	default:
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var newLines []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) != pattern {
			newLines = append(newLines, line)
		}
	}
	return os.WriteFile(path, []byte(strings.Join(newLines, "\n")), 0644)
}

// ====== вытаскивание IP-истории из map.json ======

func (b *Bot) getHistoricalIPs(pattern string) []string {
	domainIPs := b.getHistoricalIPsWithDomains(pattern)
	var ips []string
	for _, list := range domainIPs {
		ips = append(ips, list...)
	}
	return ips
}

func (b *Bot) getHistoricalIPsWithDomains(pattern string) map[string][]string {
	data, err := os.ReadFile(mapFile) // mapFile объявлен в main.go
	if err != nil {
		return map[string][]string{}
	}
	var m DomainIPMap
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string][]string{}
	}
	out := make(map[string][]string)
	for domain, ips := range m {
		if strings.Contains(domain, pattern) {
			out[domain] = ips
		}
	}
	return out
}

// ====== отображение паттернов ======

func (b *Bot) showAllPatterns(chatID int64) {
	pNL, _ := b.loadPatternsNL()
	pUS, _ := b.loadPatternsUSA()

	if len(pNL)+len(pUS) == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "📝 Паттерны не найдены"))
		return
	}

	resp := fmt.Sprintf("📝 <b>Все паттерны</b> (%d):\n\n", len(pNL)+len(pUS))
	if len(pNL) > 0 {
		resp += "🇳🇱 <b>NL (Амстердам):</b>\n"
		for _, p := range pNL {
			dm := b.getHistoricalIPsWithDomains(p)
			cnt := 0
			for _, ips := range dm {
				cnt += len(ips)
			}
			resp += fmt.Sprintf("   🔹 <code>%s</code> — %d IP\n", p, cnt)
		}
		resp += "\n"
	}
	if len(pUS) > 0 {
		resp += "🇺🇸 <b>USA (Америка):</b>\n"
		for _, p := range pUS {
			dm := b.getHistoricalIPsWithDomains(p)
			cnt := 0
			for _, ips := range dm {
				cnt += len(ips)
			}
			resp += fmt.Sprintf("   🔹 <code>%s</code> — %d IP\n", p, cnt)
		}
		resp += "\n"
	}
	resp += "💡 Используйте <code>/nl</code>, <code>/usa</code>, <code>/ru</code> для детального просмотра"

	msg := tgbotapi.NewMessage(chatID, resp)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) showPatternsNL(chatID int64) {
	ps, err := b.loadPatternsNL()
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки NL паттернов"))
		return
	}
	if len(ps) == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "📝 NL паттерны не найдены"))
		return
	}
	resp := fmt.Sprintf("🇳🇱 <b>NL паттерны (Амстердам)</b> (%d):\n\n", len(ps))
	for _, p := range ps {
		dm := b.getHistoricalIPsWithDomains(p)
		cnt := 0
		for _, ips := range dm {
			cnt += len(ips)
		}
		resp += fmt.Sprintf("🔹 <code>%s</code> — %d IP\n", p, cnt)
	}
	resp += "\n💡 Используйте <code>/nl &lt;паттерн&gt;</code> для детальной информации"
	b.api.Send(tgbotapi.NewMessage(chatID, resp))
}

func (b *Bot) showPatternsUSA(chatID int64) {
	ps, err := b.loadPatternsUSA()
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки USA паттернов"))
		return
	}
	if len(ps) == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "📝 USA паттерны не найдены"))
		return
	}
	resp := fmt.Sprintf("🇺🇸 <b>USA паттерны (Америка)</b> (%d):\n\n", len(ps))
	for _, p := range ps {
		dm := b.getHistoricalIPsWithDomains(p)
		cnt := 0
		for _, ips := range dm {
			cnt += len(ips)
		}
		resp += fmt.Sprintf("🔹 <code>%s</code> — %d IP\n", p, cnt)
	}
	resp += "\n💡 Используйте <code>/usa &lt;паттерн&gt;</code> для детальной информации"
	msg := tgbotapi.NewMessage(chatID, resp)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) showPatternsRU(chatID int64) {
	resp := "🇷🇺 <b>RU домены (прямое соединение)</b>:\n\n"
	resp += "Эти домены не находятся в списках NL или USA и идут напрямую.\n\n"
	resp += "💡 Для просмотра конкретного домена используйте: <code>/ru &lt;паттерн&gt;</code>\n"
	resp += "💡 Для перевода в списки используйте: <code>/add_nl</code> или <code>/add_usa</code>"
	msg := tgbotapi.NewMessage(chatID, resp)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) showPatternDetails(chatID int64, pattern string, routeType string) {
	domainIPs := b.getHistoricalIPsWithDomains(pattern)
	if len(domainIPs) == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ IP адреса для паттерна '%s' не найдены", pattern)))
		return
	}

	filtered := make(map[string][]string)
	for domain, ips := range domainIPs {
		var list []string
		for _, ip := range ips {
			stat := b.getIPRouteStatus(ip)
			switch routeType {
			case "nl":
				if strings.Contains(stat, "nl🇳🇱") {
					list = append(list, ip)
				}
			case "usa":
				if strings.Contains(stat, "usa🇺🇸") {
					list = append(list, ip)
				}
			case "ru":
				if strings.Contains(stat, "ru🇷🇺") && !strings.Contains(stat, "usa🇺🇸") && !strings.Contains(stat, "nl🇳🇱") {
					list = append(list, ip)
				}
			}
		}
		if len(list) > 0 {
			filtered[domain] = list
		}
	}

	if len(filtered) == 0 {
		routeNames := map[string]string{"nl": "NL (Амстердам)", "usa": "USA (Америка)", "ru": "RU (напрямую)"}
		b.api.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ IP адреса для паттерна '%s' с маршрутом %s не найдены", pattern, routeNames[routeType])))
		return
	}

	total := 0
	for _, ips := range filtered {
		total += len(ips)
	}
	for _, msgText := range b.createSiteMessages(pattern, filtered, total) {
		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		time.Sleep(100 * time.Millisecond)
	}
}

// ====== загрузка паттернов ======

func (b *Bot) loadPatternsNL() ([]string, error) {
	content, err := os.ReadFile(patternFileNL)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var patterns []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns, nil
}

func (b *Bot) loadPatternsUSA() ([]string, error) {
	content, err := os.ReadFile(patternFileUSA)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var patterns []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns, nil
}

// ====== сборка сообщений и статусов ======

func (b *Bot) createSiteMessages(pattern string, domainIPs map[string][]string, totalIPs int) []string {
	const maxMessageSize = 4000
	var messages []string
	current := fmt.Sprintf("🌐 <b>IP адреса для паттерна '%s'</b> (%d доменов, %d IP):\n\n",
		pattern, len(domainIPs), totalIPs)

	for domain, ips := range domainIPs {
		block := b.createDomainBlock(domain, ips)
		if len(current)+len(block) > maxMessageSize {
			messages = append(messages, current)
			current = fmt.Sprintf("🌐 <b>IP адреса для паттерна '%s'</b> (продолжение):\n\n", pattern)
		}
		current += block
	}
	if strings.TrimSpace(current) != "" {
		messages = append(messages, current)
	}
	return messages
}

func (b *Bot) getIPRouteStatus(ip string) string {
	if err := exec.Command("ipset", "test", ipsetNL, ip).Run(); err == nil {
		return "nl🇳🇱"
	}
	if err := exec.Command("ipset", "test", ipsetUSA, ip).Run(); err == nil {
		return "usa🇺🇸"
	}
	return "ru🇷🇺"
}

func (b *Bot) createDomainBlock(domain string, ips []string) string {
	const maxIPsToShow = 20
	ipCount := len(ips)
	block := fmt.Sprintf("🌍 <b>%s</b> — %d IP\n", domain, ipCount)
	if ipCount <= 5 {
		for _, ip := range ips {
			block += fmt.Sprintf("   • <code>%s</code> [%s]\n", ip, b.getIPRouteStatus(ip))
		}
	} else {
		display := ips
		hasMore := false
		if ipCount > maxIPsToShow {
			display = ips[:maxIPsToShow]
			hasMore = true
		}
		var lines string
		for _, ip := range display {
			lines += fmt.Sprintf("   • <code>%s</code> [%s]\n", ip, b.getIPRouteStatus(ip))
		}
		if hasMore {
			lines += fmt.Sprintf("   ... и еще %d IP адресов", ipCount-maxIPsToShow)
		}
		block += fmt.Sprintf("<blockquote expandable>%s</blockquote>\n", lines)
	}
	block += "\n"
	return block
}

func (b *Bot) createConnMessages(failed map[string][]string, totalIPs int) []string {
	const maxMessageSize = 4000
	var messages []string
	current := fmt.Sprintf("🚫 <b>Заблокированные соединения за последние 2 минуты</b> (%d записей, %d IP):\n\n",
		len(failed), totalIPs)
	for domain, ips := range failed {
		block := b.createDomainBlock(domain, ips)
		if len(current)+len(block) > maxMessageSize {
			messages = append(messages, current)
			current = "🚫 <b>Заблокированные соединения за последние 2 минуты</b> (продолжение):\n\n"
		}
		current += block
	}
	if strings.TrimSpace(current) != "" {
		messages = append(messages, current)
	}
	return messages
}

// ===== низкоуровневые вызовы ipset =====

func (b *Bot) addIPToIpsetNL(ip string) error  { return exec.Command("ipset", "add", ipsetNL, ip, "-exist").Run() }
func (b *Bot) addIPToIpsetUSA(ip string) error { return exec.Command("ipset", "add", ipsetUSA, ip, "-exist").Run() }
func (b *Bot) removeIPFromIpsetNL(ip string) error {
	return exec.Command("ipset", "del", ipsetNL, ip).Run()
}
func (b *Bot) removeIPFromIpsetUSA(ip string) error {
	return exec.Command("ipset", "del", ipsetUSA, ip).Run()
}

// ===== маршрутизация апдейтов бота =====

func (b *Bot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for up := range updates {
		if up.Message == nil {
			continue
		}
		txt := up.Message.Text
		switch {
		case strings.HasPrefix(txt, "/pass"):
			b.handlePassCommand(up.Message)
		case strings.HasPrefix(txt, "/wg"):
			b.handleWgCommand(up.Message)
		case strings.HasPrefix(txt, "/add_nl"):
			b.handleAddNLCommand(up.Message)
		case strings.HasPrefix(txt, "/add_usa"):
			b.handleAddUSACommand(up.Message)
		case strings.HasPrefix(txt, "/remove_nl"):
			b.handleRemoveNLCommand(up.Message)
		case strings.HasPrefix(txt, "/remove_usa"):
			b.handleRemoveUSACommand(up.Message)
		case strings.HasPrefix(txt, "/site"):
			b.handleSiteCommand(up.Message)
		case strings.HasPrefix(txt, "/nl"):
			b.handleNLCommand(up.Message)
		case strings.HasPrefix(txt, "/ru"):
			b.handleRuCommand(up.Message)
		case strings.HasPrefix(txt, "/usa"):
			b.handleUSACommand(up.Message)
		case strings.HasPrefix(txt, "/conn"):
			b.handleConnCommand(up.Message)
		case strings.HasPrefix(txt, "/log"):
			b.handleLogCommand(up.Message)
		case strings.HasPrefix(txt, "/help"):
			b.handleHelpCommand(up.Message)
		default:
			// молчим
		}
	}
}

func StartBot() {
	b, err := NewBot()
	if err != nil {
		log.Printf("Не удалось запустить бота: %v", err)
		return
	}
	go b.Start()
}

// ===== conntrack-хелперы (как у тебя в заметках) =====

func (b *Bot) getFailedConnections() map[string][]string {
	// Здесь должна быть твоя реализация на основе netlink.ConntrackTableList
	// Оставляю заглушку, как и раньше, чтобы не трогать остальной код.
	_ = netlink.ConntrackTable
	_ = unix.AF_INET
	return map[string][]string{}
}
