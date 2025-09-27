# PGVPN Operations Report (September 2025)

## 1. Hosts & Access

| Host | Role | SSH | Notes |
| --- | --- | --- | --- |
| pg.louso.ru | Central hub (WireGuard, dnsproxy, policy routing) | `ssh -p 22022 root@pg.louso.ru` | Main point of management; timers and services run here. |
| ipsec.louso.ru | NL egress | `ssh -p 22022 root@ipsec.louso.ru` | Termination of `wg-ipsec`. |
| usa.louso.ru | US egress | `ssh -p 22022 root@usa.louso.ru` | Termination of `wg-usa`. |
| dacha MikroTik | Remote site | `ssh -p 22022 -i ~/.ssh/mikrotik-sync.key dev@93.171.197.180` | WG peer `wg-pg-dacha`, requires SSH allow from pg. |
| home MikroTik | Home site | аналогично; см. README §9 | WG peer `wg-home`, синх по тому же ключу. |

SSH ключ для MikroTik хранится на pg: `/etc/pgvpn/mikrotik-sync.key` с публичной частью `.key.pub`.

## 2. Основные сервисы на `pg.louso.ru`

- WireGuard: `wg-quick@wg200`, `wg-quick@wg-ipsec`, `wg-quick@wg-usa`, `wg-quick@wg-home`, `wg-quick@wg-dacha`
- DNS: `unbound.service`, `dnsproxy.service`
- Автоматизация: `pg-sync-mikrotik.timer` → `/usr/local/bin/pg-sync-mikrotik.sh`, `wg-monitor.timer`
- Целевой таргет: `pgvpn.target`

Команды проверки:
```
systemctl status wg-quick@wg200 wg-quick@wg-ipsec wg-quick@wg-usa wg-quick@wg-home wg-quick@wg-dacha
systemctl status dnsproxy unbound pg-sync-mikrotik.timer wg-monitor.timer
journalctl -u pg-sync-mikrotik.service -n 20
```

## 3. Поток синхронизации MikroTik

1. Таймер `pg-sync-mikrotik.timer` каждые 10 минут запускает `pg-sync-mikrotik.sh`.
2. Скрипт читает локальные ipset `nl_proxy`/`usa_proxy`, формирует RouterOS скрипт.
3. Через `scp`/`ssh` загружает его на MikroTik, пересоздаёт адрес-листы `pg-proxy-nl`/`pg-proxy-usa`, правила mark-routing и маршрут `0.0.0.0/0 routing-mark=pg-to-pg` → `10.10.3.1` / `10.20.2.1`.
4. dnsproxy/бот обслуживает списки паттернов и ipset на pg.

Для ручного запуска: `systemctl start pg-sync-mikrotik.service`.

## 4. Внимание / ручные задачи

- **MikroTik dacha:** добавить правило `chain=input in-interface=wg-pg-dacha protocol=tcp dst-port=22022 action=accept` и обновить peer `wg-pg-dacha` `allowed-address=10.20.2.1/32,10.200.0.0/24`. После этого `pg` сможет синхронизировать списки; `wg show wg-dacha` на pg должен показать рукопожатие.
- Аналогично убедиться в доступе с `pg` до домашнего MikroTik.

## 5. Репозиторий

- `README.md` — подробная документация по деплою, синку, трафику NL/USA.
- `spec.txt` — схема инфраструктуры, список статических/динамических файлов.
- `scripts/sync-mikrotik-address-lists.sh` — скрипт синка (используется сервисом).
- `pg.louso.ru/etc/pgvpn/mikrotik-sync.conf` — шаблон конфига на pg (параметры SSH/маршрутов).

## 6. Чек-лист проверки маршрутизации

1. `wg show` — handshakes по всем туннелям.
2. `ipset list nl_proxy` / `usa_proxy` — адреса от бота.
3. `dig @10.200.0.1 <domain>` + `ip route get <IP> mark 0x1/0x2` — подтверждение policy routing NL/USA.
4. На MikroTik: `/ip firewall address-list print where list~"pg-proxy"` и `/ip firewall mangle print where comment~"pg-sync"`.
5. `journalctl -u pg-sync-mikrotik.service` — нет `scp failed` (когда SSH открыт).

