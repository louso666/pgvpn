package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	_ "modernc.org/sqlite"
)

const (
	// DNS сервера для прокси-маршрутов
	proxyNL  = "10.10.1.2:53" // Амстердам
	proxyUSA = "10.10.2.2:53" // Америка

	// Файлы конфигурации
	ipsetConfPath = "/etc/ipset.conf" // куда сохраняем ipset save
	mapFile       = "/root/map.json"  // файл с маппингом домен->IP

	// Тайминги
	refresh      = 5 * time.Second  // период релоада паттернов
	saveInterval = 10 * time.Second // интервал сохранения
	batchTimeout = 2 * time.Second  // таймаут между сигналами для батчинга
)

// SaveSignal типы сигналов для сохранения
type SaveSignal int

const (
	SaveIPSet SaveSignal = iota
	SaveJSON
)

// DomainIPMap хранит маппинг доменов на IP адреса
type DomainIPMap map[string][]string

// DNSLog хранит информацию о DNS запросе
type DNSLog struct {
	Domain    string    `json:"domain"`
	IPs       []string  `json:"ips"`
	Proxied   bool      `json:"proxied"`
	Timestamp time.Time `json:"timestamp"`
}

// Server — минималистичный DNS прокси.
type Server struct {
	mu          sync.RWMutex
	patternsNL  []string // паттерны для маршрутизации через NL (Амстердам)
	patternsUSA []string // паттерны для маршрутизации через USA (Америка)
	upstream    string
	ipMap       DomainIPMap
	mapMu       sync.RWMutex

	// Каналы для асинхронного сохранения
	saveSignal chan SaveSignal
	shutdownCh chan struct{}
	saveWg     sync.WaitGroup

	db *sql.DB // sqlite connection for logging
}

func main() {
	log.Printf("Запуск DNS прокси сервера...")

	// Открываем SQLite базу (общую с Telegram-ботом)
	db, err := sql.Open("sqlite", "/root/bot.db"+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		log.Printf("Не удалось открыть %s: %v – логирование DNS отключено", "/root/bot.db", err)
		db = nil
	} else {
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dns_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL,
			ip TEXT NOT NULL,
			proxied BOOLEAN NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		)`)
		if err != nil {
			log.Printf("Не удалось создать таблицу dns_logs: %v – логирование DNS отключено", err)
			_ = db.Close()
			db = nil
		}
	}

	s := &Server{
		ipMap:      make(DomainIPMap),
		saveSignal: make(chan SaveSignal, 100),
		shutdownCh: make(chan struct{}),
		db:         db,
	}

	// Загружаем маппинг доменов и IP
	s.loadIPMap()

	// Первая загрузка паттернов
	log.Printf("Загружаем начальные паттерны NL и USA")
	s.reloadPatterns()

	// Дефолтный upstream
	s.upstream = defaultUpstream()
	log.Printf("Используем upstream DNS: %s", s.upstream)

	// Фоновый saver
	log.Printf("Запускаем фоновую горутину для сохранения с интервалом %v", saveInterval)
	s.saveWg.Add(1)
	go s.backgroundSaver()

	// Периодический релоад паттернов
	go func() {
		log.Printf("Запускаем горутину для перезагрузки паттернов каждые %v", refresh)
		ticker := time.NewTicker(refresh)
		for range ticker.C {
			s.reloadPatterns()
		}
	}()

	// Запускаем Telegram бота
	StartBot()

	// DNS handlers
	dns.HandleFunc(".", s.handle)

	udpSrv := &dns.Server{Addr: "10.200.0.1:53", Net: "udp"}
	tcpSrv := &dns.Server{Addr: "10.200.0.1:53", Net: "tcp"}

	log.Printf("Запускаем UDP DNS сервер на %s", udpSrv.Addr)
	go func() {
		if err := udpSrv.ListenAndServe(); err != nil {
			log.Fatalf("UDP DNS сдох: %v", err)
		}
	}()

	log.Printf("Запускаем TCP DNS сервер на %s", tcpSrv.Addr)
	if err := tcpSrv.ListenAndServe(); err != nil {
		log.Fatalf("TCP DNS сдох: %v", err)
	}
}

// ВНИМАНИЕ: констант patternFileNL / patternFileUSA здесь больше НЕТ.
// Они объявлены в bot.go и используются отсюда.

// reloadPatterns перечитывает файлы с паттернами NL и USA.
func (s *Server) reloadPatterns() {
	// Файлы объявлены в bot.go как patternFileNL / patternFileUSA
	patternsNL := s.loadPatternsFromFile(patternFileNL)
	patternsUSA := s.loadPatternsFromFile(patternFileUSA)

	s.mu.Lock()
	oldCountNL := len(s.patternsNL)
	oldCountUSA := len(s.patternsUSA)
	s.patternsNL = patternsNL
	s.patternsUSA = patternsUSA
	s.mu.Unlock()

	if oldCountNL != len(patternsNL) {
		log.Printf("NL паттерны перезагружены: было %d, стало %d", oldCountNL, len(patternsNL))
	}
	if oldCountUSA != len(patternsUSA) {
		log.Printf("USA паттерны перезагружены: было %d, стало %d", oldCountUSA, len(patternsUSA))
	}
}

// loadPatternsFromFile загружает паттерны из указанного файла
func (s *Server) loadPatternsFromFile(filename string) []string {
	f, err := os.Open(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("не могу открыть %s: %v", filename, err)
		}
		return []string{}
	}
	defer f.Close()

	var list []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			list = append(list, strings.ToLower(line))
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("ошибка чтения %s: %v", filename, err)
		return []string{}
	}

	return list
}

// RouteType определяет тип маршрутизации
type RouteType int

const (
	RouteRU  RouteType = iota // прямое соединение (RU)
	RouteNL                   // через Амстердам (NL)
	RouteUSA                  // через Америку (USA)
)

// matchesPattern решает маршрут по паттернам
func (s *Server) matchesPattern(domain string) RouteType {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.patternsNL {
		if strings.Contains(domain, p) {
			log.Printf("Домен %s совпал с NL паттерном %s", domain, p)
			return RouteNL
		}
	}
	for _, p := range s.patternsUSA {
		if strings.Contains(domain, p) {
			log.Printf("Домен %s совпал с USA паттерном %s", domain, p)
			return RouteUSA
		}
	}
	return RouteRU
}

// defaultUpstream — как в исходниках: жёсткий дефолт
func defaultUpstream() string {
	return "192.168.0.200:53"
}

// loadIPMap читает JSON
func (s *Server) loadIPMap() {
	s.mapMu.Lock()
	defer s.mapMu.Unlock()

	data, err := os.ReadFile(mapFile)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Файл %s не существует, создаем новый", mapFile)
			s.ipMap = make(DomainIPMap)
			return
		}
		log.Printf("Ошибка чтения %s: %v", mapFile, err)
		s.ipMap = make(DomainIPMap)
		return
	}

	if err := json.Unmarshal(data, &s.ipMap); err != nil {
		log.Printf("Ошибка парсинга JSON из %s: %v", mapFile, err)
		s.ipMap = make(DomainIPMap)
		return
	}

	log.Printf("Загружен маппинг доменов: %d записей", len(s.ipMap))
}

func (s *Server) doSaveJSON() {
	s.mapMu.RLock()
	data, err := json.MarshalIndent(s.ipMap, "", "  ")
	s.mapMu.RUnlock()

	if err != nil {
		log.Printf("Ошибка сериализации JSON: %v", err)
		return
	}

	if err := os.WriteFile(mapFile, data, 0644); err != nil {
		log.Printf("Ошибка записи в %s: %v", mapFile, err)
		return
	}
}

func (s *Server) doSaveIPSet() {
	log.Printf("Сохраняем ipset в %s", ipsetConfPath)
	saveCmd := exec.Command("bash", "-c", "ipset save > "+ipsetConfPath)
	saveOutput, err := saveCmd.CombinedOutput()
	if err != nil {
		log.Printf("Ошибка сохранения ipset: %v, вывод: %s", err, string(saveOutput))
	} else {
		log.Printf("ipset успешно сохранен")
	}
}

func (s *Server) saveIPMap() {
	select {
	case s.saveSignal <- SaveJSON:
	default:
		log.Printf("Канал сохранения переполнен, пропускаем сигнал SaveJSON")
	}
}

func (s *Server) addDomainIP(domain, ip string) {
	s.mapMu.Lock()
	defer s.mapMu.Unlock()

	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	if s.ipMap[domain] == nil {
		s.ipMap[domain] = []string{}
	}
	for _, existingIP := range s.ipMap[domain] {
		if existingIP == ip {
			return
		}
	}
	s.ipMap[domain] = append(s.ipMap[domain], ip)
	log.Printf("Добавлен IP %s для домена %s", ip, domain)

	select {
	case s.saveSignal <- SaveJSON:
	default:
		log.Printf("Канал сохранения переполнен, пропускаем сигнал SaveJSON для %s", domain)
	}
}

// forward — проксирование к апстриму
func (s *Server) forward(req *dns.Msg, upstream string) (*dns.Msg, error) {
	log.Printf("Форвардим запрос к %s", upstream)

	c := new(dns.Client)
	c.DialTimeout = 10 * time.Second
	c.ReadTimeout = 10 * time.Second
	c.WriteTimeout = 10 * time.Second

	c.Net = "udp"
	start := time.Now()
	m, rtt, err := c.Exchange(req, upstream)

	if err != nil || (m != nil && m.Truncated) {
		if err != nil {
			log.Printf("UDP запрос к %s не удался за %v: %v, пробуем TCP", upstream, time.Since(start), err)
		} else {
			log.Printf("UDP ответ от %s усечен за %v, пробуем TCP", upstream, time.Since(start))
		}
		c.Net = "tcp"
		c.DialTimeout = 10 * time.Second
		c.ReadTimeout = 10 * time.Second
		start = time.Now()
		m, rtt, err = c.Exchange(req, upstream)
	}

	if err != nil {
		log.Printf("Ошибка форвардинга к %s: %v (общее время: %v)", upstream, err, time.Since(start))
	} else {
		if m.Truncated {
			log.Printf("Внимание: ответ от %s всё ещё усечен", upstream)
		}
		log.Printf("Получен ответ от %s за %v (RTT: %v) с %d записями (Answer: %d, Authority: %d, Additional: %d)",
			upstream, time.Since(start), rtt, len(m.Answer)+len(m.Ns)+len(m.Extra), len(m.Answer), len(m.Ns), len(m.Extra))
	}

	return m, err
}

func (s *Server) handle(w dns.ResponseWriter, req *dns.Msg) {
	var resp *dns.Msg
	var err error

	qname := ""
	qtype := ""
	if len(req.Question) > 0 {
		qname = strings.ToLower(req.Question[0].Name)
		qtype = dns.TypeToString[req.Question[0].Qtype]
		log.Printf("Получен DNS запрос %s для домена: %s", qtype, qname)
	} else {
		log.Printf("Получен DNS запрос без вопросов")
	}

	routeType := s.matchesPattern(qname)

	switch routeType {
	case RouteNL:
		log.Printf("Домен %s (%s запрос) будет проксирован через NL: %s", qname, qtype, proxyNL)
		resp, err = s.forward(req, proxyNL)
		if err == nil {
			s.processAnswers(resp, qname, RouteNL)
		}
	case RouteUSA:
		log.Printf("Домен %s (%s запрос) будет проксирован через USA: %s", qname, qtype, proxyUSA)
		resp, err = s.forward(req, proxyUSA)
		if err == nil {
			s.processAnswers(resp, qname, RouteUSA)
		}
	default: // RouteRU
		log.Printf("Домен %s (%s запрос) будет обработан через обычный upstream %s", qname, qtype, s.upstream)
		resp, err = s.forward(req, s.upstream)
		if err == nil {
			s.processAnswers(resp, qname, RouteRU)
		}
	}

	if err != nil {
		log.Printf("Ошибка обработки %s запроса для %s: %v", qtype, qname, err)
		fail := new(dns.Msg)
		fail.SetRcode(req, dns.RcodeServerFailure)
		_ = w.WriteMsg(fail)
		return
	}

	s.filterIPv6Records(resp)

	log.Printf("Отправляем %s ответ клиенту для домена %s с %d записями", qtype, qname, len(resp.Answer))
	_ = w.WriteMsg(resp)
}

// фильтрация AAAA
func (s *Server) filterIPv6Records(msg *dns.Msg) {
	var filteredAnswers []dns.RR
	removedCount := 0

	for _, ans := range msg.Answer {
		switch ans.(type) {
		case *dns.AAAA:
			removedCount++
		default:
			filteredAnswers = append(filteredAnswers, ans)
		}
	}

	if removedCount > 0 {
		msg.Answer = filteredAnswers
		log.Printf("Удалено %d IPv6 записей из ответа", removedCount)
	}
}

// processAnswers: добавление IP в ipset в зависимости от маршрута
func (s *Server) processAnswers(msg *dns.Msg, domain string, routeType RouteType) {
	log.Printf("Обрабатываем %d записей из ответа для домена %s", len(msg.Answer), domain)

	var ips []string

	for _, ans := range msg.Answer {
		switch rr := ans.(type) {
		case *dns.A:
			ip := rr.A.String()
			log.Printf("Найдена A запись: %s -> %s", rr.Hdr.Name, ip)
			ips = append(ips, ip)

			s.addDomainIP(domain, ip)

			recordDomain := strings.TrimSuffix(strings.ToLower(rr.Hdr.Name), ".")
			if recordDomain != domain {
				s.addDomainIP(recordDomain, ip)
			}

			switch routeType {
			case RouteNL:
				s.addIPToNL(ip)
			case RouteUSA:
				s.addIPToUSA(ip)
			}
		case *dns.AAAA:
			// игнорим
		}
	}

	// также заглянем в Additional и Authority
	s.processAdditionalRecords(msg.Extra, domain, routeType)
	s.processAdditionalRecords(msg.Ns, domain, routeType)

	if len(ips) > 0 {
		s.logDNS(domain, ips, routeType != RouteRU)
	}
}

func (s *Server) processAdditionalRecords(records []dns.RR, originalDomain string, routeType RouteType) {
	for _, rr := range records {
		switch ans := rr.(type) {
		case *dns.A:
			ip := ans.A.String()
			recordDomain := strings.TrimSuffix(strings.ToLower(ans.Hdr.Name), ".")
			log.Printf("Найдена дополнительная A запись: %s -> %s", recordDomain, ip)

			s.addDomainIP(recordDomain, ip)

			domainRoute := s.matchesPattern(recordDomain)
			switch domainRoute {
			case RouteNL:
				s.addIPToNL(ip)
			case RouteUSA:
				s.addIPToUSA(ip)
			}
		}
	}
}

// ЭТИ имена ipset объявлены в bot.go, здесь только используем.
func (s *Server) addIPToNL(ip string) {
	log.Printf("Добавляем IP %s в NL ipset %s", ip, ipsetNL)
	addCmd := exec.Command("ipset", "add", ipsetNL, ip, "-exist")
	if out, err := addCmd.CombinedOutput(); err != nil {
		log.Printf("Ошибка добавления IP %s в NL ipset: %v, вывод: %s", ip, err, string(out))
		return
	}
	select {
	case s.saveSignal <- SaveIPSet:
	default:
		log.Printf("Канал сохранения переполнен, пропускаем SaveIPSet для %s", ip)
	}
}

func (s *Server) addIPToUSA(ip string) {
	log.Printf("Добавляем IP %s в USA ipset %s", ip, ipsetUSA)
	addCmd := exec.Command("ipset", "add", ipsetUSA, ip, "-exist")
	if out, err := addCmd.CombinedOutput(); err != nil {
		log.Printf("Ошибка добавления IP %s в USA ipset: %v, вывод: %s", ip, err, string(out))
		return
	}
	select {
	case s.saveSignal <- SaveIPSet:
	default:
		log.Printf("Канал сохранения переполнен, пропускаем SaveIPSet для %s", ip)
	}
}

func (s *Server) backgroundSaver() {
	defer s.saveWg.Done()

	jsonPending := false
	ipsetPending := false

	ticker := time.NewTicker(saveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdownCh:
			log.Printf("Получен сигнал остановки, делаем финальный save")
			if ipsetPending {
				s.doSaveIPSet()
			}
			if jsonPending {
				s.doSaveJSON()
			}
			return
		case sig := <-s.saveSignal:
			switch sig {
			case SaveIPSet:
				ipsetPending = true
			case SaveJSON:
				jsonPending = true
			}
		case <-ticker.C:
			if ipsetPending {
				s.doSaveIPSet()
				ipsetPending = false
			}
			if jsonPending {
				s.doSaveJSON()
				jsonPending = false
			}
		}
	}
}

func (s *Server) logDNS(domain string, ips []string, proxied bool) {
	if s.db == nil {
		return
	}
	for _, ip := range ips {
		_, _ = s.db.Exec(`INSERT INTO dns_logs (domain, ip, proxied) VALUES (?, ?, ?)`,
			strings.TrimSuffix(domain, "."), ip, proxied)
	}
}
