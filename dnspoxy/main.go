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
	patternFile   = "/root/site"      // файл с паттернами доменов
	proxyDNS      = "10.24.0.2:53"    // DNS, через который резолвим "иностранцев"
	ipsetName     = "proxied"         // ipset-лист для чужих айпишников
	ipsetConfPath = "/etc/ipset.conf" // куда сохраняем ipset save
	mapFile       = "/root/map.json"  // файл с маппингом домен->IP
	refresh       = 5 * time.Second   // период релоада паттернов

	// Параметры для асинхронного сохранения
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
// patterns держим под RWMutex, чтобы спокойно перезагружать на лету.
type Server struct {
	mu       sync.RWMutex
	patterns []string
	upstream string
	ipMap    DomainIPMap
	mapMu    sync.RWMutex

	// Каналы для асинхронного сохранения
	saveSignal chan SaveSignal
	shutdownCh chan struct{}
	saveWg     sync.WaitGroup

	db *sql.DB // NEW: sqlite connection for logging
}

func main() {
	log.Printf("Запуск DNS прокси сервера...")

	// Открываем SQLite базу (общую с Telegram-ботом)
	db, err := sql.Open("sqlite", "/root/bot.db"+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		log.Printf("Не удалось открыть %s: %v – логирование DNS отключено", "/root/bot.db", err)
		db = nil // continue without logging
	} else {
		// Создаём таблицу, если её ещё нет
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dns_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL,
			ip TEXT NOT NULL,
			proxied BOOLEAN NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		)`)
		if err != nil {
			log.Printf("Не удалось создать таблицу dns_logs: %v – логирование DNS отключено", err)
			db.Close()
			db = nil
		}
	}

	s := &Server{
		ipMap:      make(DomainIPMap),
		saveSignal: make(chan SaveSignal, 100), // буферизованный канал
		shutdownCh: make(chan struct{}),
		db:         db, // NEW
	}

	// Загружаем маппинг доменов и IP
	s.loadIPMap()

	// Первая загрузка /root/site.
	log.Printf("Загружаем начальные паттерны из %s", patternFile)
	s.reloadPatterns()

	// Вычисляем дефолтный апстрим (первый nameserver из /etc/resolv.conf) или валимся в 192.168.0.200.
	s.upstream = defaultUpstream()
	log.Printf("Используем upstream DNS: %s", s.upstream)

	// Запускаем фоновую горутину для асинхронного сохранения
	log.Printf("Запускаем фоновую горутину для сохранения с интервалом %v", saveInterval)
	s.saveWg.Add(1)
	go s.backgroundSaver()

	// Тайкер на релоад каждые 5 сек.
	go func() {
		log.Printf("Запускаем горутину для перезагрузки паттернов каждые %v", refresh)
		ticker := time.NewTicker(refresh)
		for range ticker.C {
			s.reloadPatterns()
		}
	}()

	// Запускаем Telegram бота
	StartBot()

	// Один хендлер на всё, лень разбираться что там за тип.
	dns.HandleFunc(".", s.handle)

	// UDP и TCP — потому что клиенты бывают разные.
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

// reloadPatterns перечитывает файл с паттернами.
func (s *Server) reloadPatterns() {
	f, err := os.Open(patternFile)
	if err != nil {
		log.Printf("не могу открыть %s: %v", patternFile, err)
		return
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
		log.Printf("ошибка чтения %s: %v", patternFile, err)
		return
	}

	s.mu.Lock()
	oldCount := len(s.patterns)
	s.patterns = list
	s.mu.Unlock()

	// Логируем только если количество изменилось
	if oldCount != len(list) {
		log.Printf("Паттерны перезагружены: было %d, стало %d", oldCount, len(list))
	}
}

// matches проверяет, содержит ли домен один из паттернов.
func (s *Server) matches(domain string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.patterns {
		if strings.Contains(domain, p) {
			log.Printf("Домен %s совпал с паттерном %s", domain, p)
			return true
		}
	}
	return false
}

// defaultUpstream вытаскивает первый nameserver из /etc/resolv.conf.
func defaultUpstream() string {
	return "192.168.0.200:53"
	// log.Printf("Определяем upstream DNS из /etc/resolv.conf")

	// f, err := os.Open("/etc/resolv.conf")
	// if err != nil {
	// 	log.Printf("Не могу открыть /etc/resolv.conf: %v, использую 192.168.0.200:53", err)
	// 	return "192.168.0.200:53"
	// }
	// defer f.Close()

	// scanner := bufio.NewScanner(f)
	// for scanner.Scan() {
	// 	fields := strings.Fields(scanner.Text())
	// 	if len(fields) >= 2 && fields[0] == "nameserver" {
	// 		upstream := net.JoinHostPort(fields[1], "53")
	// 		log.Printf("Найден nameserver: %s", upstream)
	// 		return upstream
	// 	}
	// }

	// log.Printf("Nameserver не найден в /etc/resolv.conf, использую 192.168.0.200:53")
	// return "192.168.0.200:53"
}

// loadIPMap загружает маппинг доменов и IP из JSON файла
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

// doSaveJSON сохраняет маппинг доменов и IP в JSON файл
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

// doSaveIPSet сохраняет текущий ipset в файл конфигурации
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

// saveIPMap устаревшая функция - оставлена для совместимости, но теперь использует канал
func (s *Server) saveIPMap() {
	// Отправляем сигнал в канал для асинхронного сохранения
	select {
	case s.saveSignal <- SaveJSON:
	default:
		// Канал переполнен, пропускаем сигнал
		log.Printf("Канал сохранения переполнен, пропускаем сигнал SaveJSON")
	}
}

// addDomainIP добавляет IP для домена без дубликатов
func (s *Server) addDomainIP(domain, ip string) {
	s.mapMu.Lock()
	defer s.mapMu.Unlock()

	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	if s.ipMap[domain] == nil {
		s.ipMap[domain] = []string{}
	}

	// Проверяем на дубликаты
	for _, existingIP := range s.ipMap[domain] {
		if existingIP == ip {
			return // IP уже есть
		}
	}

	s.ipMap[domain] = append(s.ipMap[domain], ip)
	log.Printf("Добавлен IP %s для домена %s", ip, domain)

	// Отправляем сигнал для асинхронного сохранения JSON
	select {
	case s.saveSignal <- SaveJSON:
	default:
		// Канал переполнен, пропускаем сигнал
		log.Printf("Канал сохранения переполнен, пропускаем сигнал SaveJSON для %s", domain)
	}
}

// forward проксирует запрос к апстриму с поддержкой UDP и TCP и оптимизированными таймаутами.
func (s *Server) forward(req *dns.Msg, upstream string) (*dns.Msg, error) {
	log.Printf("Форвардим запрос к %s", upstream)

	c := new(dns.Client)

	// Настраиваем агрессивные таймауты для быстрого ответа
	c.DialTimeout = 10 * time.Second  // быстрое подключение
	c.ReadTimeout = 10 * time.Second  // быстрое чтение
	c.WriteTimeout = 10 * time.Second // быстрая запись

	// Сначала пробуем UDP с малым таймаутом
	c.Net = "udp"
	start := time.Now()
	m, rtt, err := c.Exchange(req, upstream)

	// Если UDP не сработал или ответ усечен, пробуем TCP
	if err != nil || (m != nil && m.Truncated) {
		if err != nil {
			log.Printf("UDP запрос к %s не удался за %v: %v, пробуем TCP", upstream, time.Since(start), err)
		} else {
			log.Printf("UDP ответ от %s усечен за %v, пробуем TCP", upstream, time.Since(start))
		}

		// TCP обычно медленнее, даём больше времени
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

// handle — здесь вся грязная работа.
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

	if s.matches(qname) {
		// домен из проксируемых -> резолв через 10.24.0.2
		log.Printf("Домен %s (%s запрос) будет проксирован через %s", qname, qtype, proxyDNS)
		resp, err = s.forward(req, proxyDNS)
		if err == nil {
			s.processAnswers(resp, qname, true)
		}
	} else {
		// обычная халупа -> дефолтный апстрим
		log.Printf("Домен %s (%s запрос) будет обработан через обычный upstream %s", qname, qtype, s.upstream)
		resp, err = s.forward(req, s.upstream)
		if err == nil {
			s.processAnswers(resp, qname, false)
		}
	}

	if err != nil {
		log.Printf("Ошибка обработки %s запроса для %s: %v", qtype, qname, err)
		fail := new(dns.Msg)
		fail.SetRcode(req, dns.RcodeServerFailure)
		_ = w.WriteMsg(fail)
		return
	}

	// Удаляем все AAAA записи из ответа
	s.filterIPv6Records(resp)

	log.Printf("Отправляем %s ответ клиенту для домена %s с %d записями", qtype, qname, len(resp.Answer))
	_ = w.WriteMsg(resp)
}

// filterIPv6Records удаляет все AAAA записи из ответа.
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

// processAnswers обрабатывает все типы DNS записей из ответа.
// A записи добавляются в ipset если домен проксируемый.
// CNAME записи проверяются на необходимость проксирования целевого домена.
func (s *Server) processAnswers(msg *dns.Msg, domain string, proxied bool) {
	log.Printf("Обрабатываем %d записей из ответа для домена %s", len(msg.Answer), domain)

	var ips []string
	var cnames []string

	for _, ans := range msg.Answer {
		switch rr := ans.(type) {
		case *dns.A:
			ip := rr.A.String()
			log.Printf("Найдена A запись: %s -> %s", rr.Hdr.Name, ip)
			ips = append(ips, ip)

			// Добавляем IP для исходного домена
			s.addDomainIP(domain, ip)

			// Если запись для другого домена (например, через CNAME), добавляем и для него
			recordDomain := strings.TrimSuffix(strings.ToLower(rr.Hdr.Name), ".")
			if recordDomain != domain {
				s.addDomainIP(recordDomain, ip)
			}

			// Добавляем в ipset только если домен проксируемый
			if proxied {
				s.addIP(ip)
			}

		case *dns.AAAA:
			log.Printf("Найдена AAAA запись: %s -> %s (игнорируем IPv6)", rr.Hdr.Name, rr.AAAA.String())

		case *dns.CNAME:
			target := strings.TrimSuffix(strings.ToLower(rr.Target), ".")
			log.Printf("Найдена CNAME запись: %s -> %s", rr.Hdr.Name, target)
			cnames = append(cnames, target)

			// Проверяем, нужно ли проксировать целевой домен CNAME
			if s.matches(target) {
				log.Printf("CNAME целевой домен %s совпадает с паттернами - будет проксирован", target)
			}

		case *dns.MX:
			log.Printf("Найдена MX запись: %s -> %s (приоритет %d)", rr.Hdr.Name, rr.Mx, rr.Preference)

		case *dns.TXT:
			log.Printf("Найдена TXT запись: %s -> %v", rr.Hdr.Name, rr.Txt)

		case *dns.NS:
			log.Printf("Найдена NS запись: %s -> %s", rr.Hdr.Name, rr.Ns)

		case *dns.SRV:
			log.Printf("Найдена SRV запись: %s -> %s:%d (приоритет %d, вес %d)",
				rr.Hdr.Name, rr.Target, rr.Port, rr.Priority, rr.Weight)

		case *dns.PTR:
			log.Printf("Найдена PTR запись: %s -> %s", rr.Hdr.Name, rr.Ptr)

		case *dns.SOA:
			log.Printf("Найдена SOA запись: %s", rr.Hdr.Name)

		default:
			log.Printf("Найдена DNS запись неизвестного типа: %T для %s", rr, rr.Header().Name)
		}
	}

	// Обрабатываем Additional и Authority секции для поиска дополнительных A записей
	s.processAdditionalRecords(msg.Extra, domain, proxied)
	s.processAdditionalRecords(msg.Ns, domain, proxied)

	// После обработки всех записей логируем запрос (только если есть IP) – NEW
	if len(ips) > 0 {
		s.logDNS(domain, ips, proxied)
	}
}

// processAdditionalRecords обрабатывает дополнительные записи (Additional и Authority секции)
func (s *Server) processAdditionalRecords(records []dns.RR, originalDomain string, proxied bool) {
	for _, rr := range records {
		switch ans := rr.(type) {
		case *dns.A:
			ip := ans.A.String()
			recordDomain := strings.TrimSuffix(strings.ToLower(ans.Hdr.Name), ".")
			log.Printf("Найдена дополнительная A запись: %s -> %s", recordDomain, ip)

			// Добавляем IP для домена из дополнительной записи
			s.addDomainIP(recordDomain, ip)

			// Проверяем, нужно ли проксировать этот домен
			if s.matches(recordDomain) {
				log.Printf("Дополнительный домен %s совпадает с паттернами - добавляем IP в ipset", recordDomain)
				s.addIP(ip)
			}
		}
	}
}

// addIP — синхронное добавление в ipset + асинхронное сохранение.
func (s *Server) addIP(ip string) {
	log.Printf("Добавляем IP %s в ipset %s", ip, ipsetName)

	// Команда добавления IP (синхронно для быстрого ответа)
	addCmd := exec.Command("ipset", "add", ipsetName, ip, "-exist")
	addOutput, err := addCmd.CombinedOutput()
	if err != nil {
		log.Printf("Ошибка добавления IP %s в ipset: %v, вывод: %s", ip, err, string(addOutput))
		return // не отправляем сигнал на сохранение, если добавление не удалось
	} else {
		log.Printf("IP %s успешно добавлен в ipset", ip)
	}

	// Отправляем сигнал для асинхронного сохранения ipset
	select {
	case s.saveSignal <- SaveIPSet:
	default:
		// Канал переполнен, пропускаем сигнал
		log.Printf("Канал сохранения переполнен, пропускаем сигнал SaveIPSet для %s", ip)
	}
}

// backgroundSaver — фоновая горутина для асинхронного сохранения
func (s *Server) backgroundSaver() {
	defer s.saveWg.Done()

	// Флаги для отслеживания что нужно сохранить
	needSaveIPSet := false
	needSaveJSON := false

	// Таймер для периодического сохранения
	ticker := time.NewTicker(saveInterval)
	defer ticker.Stop()

	// Таймер для батчинга (сохранение через небольшой таймаут после последнего сигнала)
	var batchTimer *time.Timer

	log.Printf("Фоновая горутина сохранения запущена")

	doSave := func() {
		if needSaveIPSet {
			log.Printf("Фоновое сохранение ipset...")
			s.doSaveIPSet()
			needSaveIPSet = false
		}
		if needSaveJSON {
			log.Printf("Фоновое сохранение JSON...")
			s.doSaveJSON()
			needSaveJSON = false
		}
	}

	for {
		select {
		case <-s.shutdownCh:
			// Принудительно сохраняем перед выходом
			doSave()
			return

		case signal := <-s.saveSignal:
			switch signal {
			case SaveIPSet:
				needSaveIPSet = true
			case SaveJSON:
				needSaveJSON = true
			}

			// Перезапускаем таймер батчинга
			if batchTimer != nil {
				batchTimer.Stop()
			}
			batchTimer = time.AfterFunc(batchTimeout, doSave)

		case <-ticker.C:
			// Периодическое сохранение
			doSave()
		}
	}
}

// logDNS сохраняет один лог DNS-запроса в базу (по одному IP).
func (s *Server) logDNS(domain string, ips []string, proxied bool) {
	if s.db == nil {
		return // логирование выключено
	}

	// Подготовленное выражение на лету (SQLite сам кеширует)
	stmt, err := s.db.Prepare(`INSERT INTO dns_logs(domain, ip, proxied) VALUES(?, ?, ?)`)
	if err != nil {
		log.Printf("prepare dns_logs insert failed: %v", err)
		return
	}
	defer stmt.Close()

	for _, ip := range ips {
		if _, err := stmt.Exec(domain, ip, proxied); err != nil {
			log.Printf("insert dns_logs failed: %v", err)
		}
	}
}

// Shutdown корректно завершает работу сервера
func (s *Server) Shutdown() {
	log.Printf("Завершаем работу DNS прокси сервера...")

	// Сигнализируем фоновой горутине о завершении
	close(s.shutdownCh)

	// Ждём завершения фоновых операций
	s.saveWg.Wait()

	if s.db != nil {
		s.db.Close()
	}

	log.Printf("DNS прокси сервер корректно завершён")
}
