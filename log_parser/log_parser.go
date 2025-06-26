package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

type LogEntry struct {
	Timestamp time.Time
	Domain    string
	IP        string
	Proxied   bool
}

// DomainIPMap хранит маппинг доменов на IP адреса
type DomainIPMap map[string][]string

func main() {
	log.Printf("Запуск парсера логов DNS сервиса...")

	// Получаем логи с удаленного сервера
	logs, err := getLogs()
	if err != nil {
		log.Fatalf("Ошибка получения логов: %v", err)
	}

	// Парсим логи
	entries := parseLogs(logs)
	log.Printf("Найдено %d записей DNS", len(entries))

	// Группируем по доменам
	domainMap := groupByDomain(entries)
	log.Printf("Уникальных доменов: %d", len(domainMap))

	// Загружаем существующий map.json
	existingMap, err := loadExistingMap()
	if err != nil {
		log.Printf("Предупреждение: не удалось загрузить существующий map.json: %v", err)
		existingMap = make(DomainIPMap)
	}

	// Объединяем данные
	mergedMap := mergeData(existingMap, domainMap)

	// Сохраняем обновленный map.json
	if err := saveMapToFile(mergedMap, "recovered_map.json"); err != nil {
		log.Fatalf("Ошибка сохранения: %v", err)
	}

	// Выводим статистику
	printStatistics(existingMap, mergedMap)

	log.Printf("✅ Парсинг завершен. Результат сохранен в recovered_map.json")
	log.Printf("Скопируйте файл на сервер: scp recovered_map.json root@176.114.88.142:/root/map.json")
}

func getLogs() (string, error) {
	log.Printf("Получение логов с удаленного сервера...")

	// Получаем логи за последний месяц
	cmd := exec.Command("ssh", "root@176.114.88.142",
		"journalctl -u dnspoxy.service --since '1 month ago' --no-pager")

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения ssh команды: %v", err)
	}

	return string(output), nil
}

func parseLogs(logs string) []LogEntry {
	var entries []LogEntry

	// Регулярные выражения для парсинга
	domainRegex := regexp.MustCompile(`(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}).*Получен DNS запрос для домена: (.+)`)
	ipRegex := regexp.MustCompile(`(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}).*Найдена A запись: (\d+\.\d+\.\d+\.\d+)`)
	proxiedRegex := regexp.MustCompile(`будет проксирован через`)

	lines := strings.Split(logs, "\n")

	var currentDomain string
	var currentTimestamp time.Time
	var currentProxied bool

	for _, line := range lines {
		// Ищем DNS запрос
		if matches := domainRegex.FindStringSubmatch(line); matches != nil {
			timestamp, err := time.Parse("2006/01/02 15:04:05", matches[1])
			if err != nil {
				continue
			}

			domain := strings.TrimSpace(strings.TrimSuffix(matches[2], "."))
			currentDomain = domain
			currentTimestamp = timestamp
			currentProxied = proxiedRegex.MatchString(line)
		}

		// Ищем A запись
		if matches := ipRegex.FindStringSubmatch(line); matches != nil && currentDomain != "" {
			timestamp, err := time.Parse("2006/01/02 15:04:05", matches[1])
			if err != nil {
				continue
			}

			// Проверяем, что A запись соответствует текущему домену (в пределах 5 секунд)
			if timestamp.Sub(currentTimestamp).Abs() <= 5*time.Second {
				entries = append(entries, LogEntry{
					Timestamp: timestamp,
					Domain:    currentDomain,
					IP:        matches[2],
					Proxied:   currentProxied,
				})
			}
		}
	}

	return entries
}

func groupByDomain(entries []LogEntry) DomainIPMap {
	domainMap := make(DomainIPMap)

	for _, entry := range entries {
		domain := strings.ToLower(entry.Domain)

		if domainMap[domain] == nil {
			domainMap[domain] = []string{}
		}

		// Проверяем на дубликаты
		found := false
		for _, existingIP := range domainMap[domain] {
			if existingIP == entry.IP {
				found = true
				break
			}
		}

		if !found {
			domainMap[domain] = append(domainMap[domain], entry.IP)
		}
	}

	return domainMap
}

func loadExistingMap() (DomainIPMap, error) {
	// Сначала попробуем загрузить с удаленного сервера
	cmd := exec.Command("scp", "root@176.114.88.142:/root/map.json", "./current_map.json")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("не удалось скопировать map.json с сервера: %v", err)
	}

	data, err := os.ReadFile("./current_map.json")
	if err != nil {
		return nil, err
	}

	var domainMap DomainIPMap
	if err := json.Unmarshal(data, &domainMap); err != nil {
		return nil, err
	}

	return domainMap, nil
}

func mergeData(existing, recovered DomainIPMap) DomainIPMap {
	merged := make(DomainIPMap)

	// Копируем существующие данные
	for domain, ips := range existing {
		merged[domain] = make([]string, len(ips))
		copy(merged[domain], ips)
	}

	// Добавляем восстановленные данные
	for domain, ips := range recovered {
		if merged[domain] == nil {
			merged[domain] = []string{}
		}

		for _, ip := range ips {
			// Проверяем на дубликаты
			found := false
			for _, existingIP := range merged[domain] {
				if existingIP == ip {
					found = true
					break
				}
			}

			if !found {
				merged[domain] = append(merged[domain], ip)
			}
		}
	}

	return merged
}

func saveMapToFile(domainMap DomainIPMap, filename string) error {
	data, err := json.MarshalIndent(domainMap, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

func printStatistics(existing, merged DomainIPMap) {
	fmt.Printf("\n📊 Статистика восстановления:\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	existingDomains := len(existing)
	existingIPs := 0
	for _, ips := range existing {
		existingIPs += len(ips)
	}

	mergedDomains := len(merged)
	mergedIPs := 0
	for _, ips := range merged {
		mergedIPs += len(ips)
	}

	newDomains := mergedDomains - existingDomains
	newIPs := mergedIPs - existingIPs

	fmt.Printf("🔍 Было доменов:      %d\n", existingDomains)
	fmt.Printf("📈 Стало доменов:     %d (+%d)\n", mergedDomains, newDomains)
	fmt.Printf("🔍 Было IP адресов:   %d\n", existingIPs)
	fmt.Printf("📈 Стало IP адресов:  %d (+%d)\n", mergedIPs, newIPs)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	// Показываем топ доменов по количеству IP
	fmt.Printf("\n📋 Топ-10 доменов по количеству IP:\n")
	type domainStat struct {
		domain string
		count  int
	}

	var stats []domainStat
	for domain, ips := range merged {
		stats = append(stats, domainStat{domain, len(ips)})
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].count > stats[j].count
	})

	for i, stat := range stats {
		if i >= 10 {
			break
		}
		fmt.Printf("   %2d. %-30s %d IP\n", i+1, stat.domain, stat.count)
	}
}
