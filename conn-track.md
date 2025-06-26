### TL;DR

Под root-ом проще всего вытащить «не cостоявшиеся» соединения (те, у которых **не было ответа**) напрямую из conntrack через netlink.

1. Подтягиваешь `github.com/vishvananda/netlink >= v1.3.1`.
2. Дёргаешь `netlink.ConntrackTableList(netlink.ConntrackTable, unix.AF_INET)` — это ровно то, что делает утилита `conntrack -L` ([pkg.go.dev][1], [pkg.go.dev][1]).
3. Отфильтровываешь записи с флагом **UNREPLIED** (для UDP/TCP) или TCP-состоянием **SYN\_SENT/SYN\_RECV**.
4. Profit.

---

### Минимальный рабочий пример

```go
package main

import (
	"fmt"
	"log"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func main() {
	flows, err := netlink.ConntrackTableList(netlink.ConntrackTable, unix.AF_INET)
	if err != nil {
		log.Fatalf("conntrack dump failed: %v", err)
	}

	for _, f := range flows {
		switch f.ProtoInfo.Protocol {
		case unix.IPPROTO_TCP:
			tcp := f.ProtoInfo.TCP
			// нужен ли нам REPLY?
			if f.Status&unix.NF_CT_STATE_UNREPLIED != 0 ||
				tcp.StateOrig == unix.TCP_SYN_SENT ||
				tcp.StateOrig == unix.TCP_SYN_RECV {
				fmt.Println(f) // или собираешь в структуру
			}
		case unix.IPPROTO_UDP:
			if f.Status&unix.NF_CT_STATE_UNREPLIED != 0 {
				fmt.Println(f)
			}
		}
	}
}
```

*Пояснения к полям*

* **Status & NF\_CT\_STATE\_UNREPLIED** — пакетов в обратную сторону не было → либо отфильтровали на выходе, либо бан/дроп на хосте/сети.
* **ProtoInfo.TCP.StateOrig** — оригинальное TCP-состояние. `SYN_SENT`/`SYN_RECV` — рукопожатие не дошло до `ESTABLISHED`.

Данные `ConntrackFlow` уже содержат IP/порт источника/назначения, таймаут и метку времени (`TimeStart`/`TimeStop`) ([pkg.go.dev][1]) — можешь логировать или агрегировать, как душе угодно.

---

### Часто задаваемые вопросы

| Вопрос                                                        | Жёсткая правда                                                                                                                                                       |
| ------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **/proc/net/tcp** вместо netlink?                             | Там уже *нет* записей после `connect()` с ошибкой — запись удаляется мгновенно. С netlink ты ловишь запись в ct ещё до GC.                                           |
| **Почему не видно пакетов, отфильтрованных iptables OUTPUT?** | Conntrack пишет entry **после** успешного прохода стека Netfilter. Если дропаешь на OUTPUT — в таблицу оно даже не попадает. Тут уж включай NFLOG или eBPF-трейсинг. |
| **Нужен UDP?**                                                | Аналогично: смотри `UNREPLIED`, только без TCP-состояний.                                                                                                            |
| **eBPF не круче?**                                            | Круче, но дольше писать. Поднять kprobe на `tcp_v4_connect` + мапа для errno — отдельное развлечение. Если надо *прямо сейчас* — берёшь conntrack-дамп.              |

---

### Если хочется «совсем всё» — вариант с eBPF

1. Берёшь `github.com/cilium/ebpf`.
2. kprobe на `tcp_v4_connect` и `udp_sendmsg`.
3. В программе возвращается `ret < 0` → это ошибка до передачи пакет(ов) (например `-EPERM`, `-EACCES`).
4. Данные (PID, dest IP\:port, errno) сливаешь через perf-event или ring buffer.

Но это уже из серии «хочу telemetry-hell», а не «просто узнать, кто обломался».

---

### Вывод

* **Нужен быстрый отчёт?** — хватай conntrack/netlink, фильтруй `UNREPLIED` и `SYN_*` — две страницы кода.
* **Нужен полный realtime-трейс?** — eBPF, NFLOG или pcap с iptables-LOG.

В общем, выбирай яд — я тебе способ показал.

[1]: https://pkg.go.dev/github.com/vishvananda/netlink "netlink package - github.com/vishvananda/netlink - Go Packages"
