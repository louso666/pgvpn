package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	patternFile   = "/root/site"      // файл с паттернами доменов
	proxyDNS      = "10.24.0.2:53"    // DNS, через который резолвим "иностранцев"
	ipsetName     = "proxied"         // ipset-лист для чужих айпишников
	ipsetConfPath = "/etc/ipset.conf" // куда сохраняем ipset save
	mapFile       = "/root/map.json"  // файл с маппингом домен->IP
	refresh       = 5 * time.Second   // период релоада паттернов
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
}

func main() {
	log.Printf("Запуск DNS прокси сервера...")

	s := &Server{
		ipMap: make(DomainIPMap),
	}

	// Загружаем маппинг доменов и IP
	s.loadIPMap()

	// Первая загрузка /root/site.
	log.Printf("Загружаем начальные паттерны из %s", patternFile)
	s.reloadPatterns()

	// Вычисляем дефолтный апстрим (первый nameserver из /etc/resolv.conf) или валимся в 192.168.0.200.
	s.upstream = defaultUpstream()
	log.Printf("Используем upstream DNS: %s", s.upstream)

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
	log.Printf("Определяем upstream DNS из /etc/resolv.conf")

	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		log.Printf("Не могу открыть /etc/resolv.conf: %v, использую 192.168.0.200:53", err)
		return "192.168.0.200:53"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			upstream := net.JoinHostPort(fields[1], "53")
			log.Printf("Найден nameserver: %s", upstream)
			return upstream
		}
	}

	log.Printf("Nameserver не найден в /etc/resolv.conf, использую 192.168.0.200:53")
	return "192.168.0.200:53"
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

// saveIPMap сохраняет маппинг доменов и IP в JSON файл
func (s *Server) saveIPMap() {
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

	// Сохраняем асинхронно
	go s.saveIPMap()
}

// forward проксирует запрос к апстриму.
func (s *Server) forward(req *dns.Msg, upstream string) (*dns.Msg, error) {
	log.Printf("Форвардим запрос к %s", upstream)

	c := new(dns.Client)
	c.Net = "udp"
	m, _, err := c.Exchange(req, upstream)

	if err != nil {
		log.Printf("Ошибка форвардинга к %s: %v", upstream, err)
	} else {
		log.Printf("Получен ответ от %s с %d записями", upstream, len(m.Answer))
	}

	return m, err
}

// handle — здесь вся грязная работа.
func (s *Server) handle(w dns.ResponseWriter, req *dns.Msg) {
	var resp *dns.Msg
	var err error

	qname := ""
	if len(req.Question) > 0 {
		qname = strings.ToLower(req.Question[0].Name)
		log.Printf("Получен DNS запрос для домена: %s", qname)
	} else {
		log.Printf("Получен DNS запрос без вопросов")
	}

	if s.matches(qname) {
		// домен из проксируемых -> резолв через 10.24.0.2
		log.Printf("Домен %s будет проксирован через %s", qname, proxyDNS)
		resp, err = s.forward(req, proxyDNS)
		if err == nil {
			s.processAnswers(resp, qname, true)
		}
	} else {
		// обычная халупа -> дефолтный апстрим
		log.Printf("Домен %s будет обработан через обычный upstream %s", qname, s.upstream)
		resp, err = s.forward(req, s.upstream)
		if err == nil {
			s.processAnswers(resp, qname, false)
		}
	}

	if err != nil {
		log.Printf("Ошибка обработки запроса для %s: %v", qname, err)
		fail := new(dns.Msg)
		fail.SetRcode(req, dns.RcodeServerFailure)
		_ = w.WriteMsg(fail)
		return
	}

	// Удаляем все AAAA записи из ответа
	s.filterIPv6Records(resp)

	log.Printf("Отправляем ответ клиенту для домена %s", qname)
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

// processAnswers кидает только A записи из ответа в ipset.
func (s *Server) processAnswers(msg *dns.Msg, domain string, proxied bool) {
	log.Printf("Обрабатываем %d записей из ответа для добавления в ipset", len(msg.Answer))

	var ips []string
	for _, ans := range msg.Answer {
		switch rr := ans.(type) {
		case *dns.A:
			ip := rr.A.String()
			log.Printf("Найдена A запись: %s", ip)
			ips = append(ips, ip)
			s.addDomainIP(domain, ip)
			if proxied {
				s.addIP(ip)
			}
		case *dns.AAAA:
			log.Printf("Найдена AAAA запись: %s (игнорируем IPv6)", rr.AAAA.String())
		}
	}
}

// addIP — ipset add + сохранение.
func (s *Server) addIP(ip string) {
	log.Printf("Добавляем IP %s в ipset %s", ip, ipsetName)

	// Команда добавления IP
	addCmd := exec.Command("ipset", "add", ipsetName, ip, "-exist")
	addOutput, err := addCmd.CombinedOutput()
	if err != nil {
		log.Printf("Ошибка добавления IP %s в ipset: %v, вывод: %s", ip, err, string(addOutput))
		return // не пытаемся сохранять, если добавление не удалось
	} else {
		log.Printf("IP %s успешно добавлен в ipset", ip)
	}

	log.Printf("Сохраняем ipset в %s", ipsetConfPath)
	saveCmd := exec.Command("bash", "-c", "ipset save > "+ipsetConfPath)
	saveOutput, err := saveCmd.CombinedOutput()
	if err != nil {
		log.Printf("Ошибка сохранения ipset: %v, вывод: %s", err, string(saveOutput))
	} else {
		log.Printf("ipset успешно сохранен")
	}
}
