# PGVPN Infrastructure Guide

This repository tracks the configuration and automation for the pg.louso routed VPN hub and the two upstream egress nodes (`ipsec.louso.ru` in NL and `usa.louso.ru` in the US). It also contains the `dnsproxy` service that powers the Telegram bot and policy routing.

The documentation below explains how the current setup works, how to operate it, and how to recover the system when something breaks.

---

## 1. Topology Overview

```
clients ──▶ wg200 (10.200.0.1/24)
             │
             ├── policy routing → wg-ipsec (10.10.1.1 ⇄ 10.10.1.2) → ipsec.louso.ru (NL egress)
             └── policy routing → wg-usa   (10.10.2.1 ⇄ 10.10.2.2) → usa.louso.ru (US egress)

home Mikrotik (192.168.1.0/24) ─┐
                                 └─▶ wg-home (10.10.3.1/30)
country house (192.168.2.0/24) ─┘     ↕
                                    wg-dacha (10.20.2.1/30)
```

`pg.louso.ru` is the central router:
- `wg200` serves end-user WireGuard clients (10.200.0.0/24).
- `wg-ipsec` and `wg-usa` provide point-to-point links to the NL and US exit servers.
- `dnsproxy` listens on 10.200.0.1:53, populates ipset sets, and exposes the Telegram bot.
- `unbound` acts as a local recursive resolver on 127.0.0.1 for domains that do not need rerouting.

The exit servers (`ipsec.louso.ru`, `usa.louso.ru`) terminate their respective WG tunnels and run VLESS.

---

## 2. Key Services and Units on `pg.louso.ru`

| Service / Unit | Purpose |
| --- | --- |
| `wg-quick@wg200` | Main client network (10.200.0.1/24) |
| `wg-quick@wg-ipsec`, `wg-quick@wg-usa` | Transport tunnels to NL / USA |
| `unbound.service` | Local DNS resolver on 127.0.0.1 |
| `dnsproxy.service` | DNS proxy + Telegram bot. Reads pattern files and updates ipset |
| `proxy-route.service` | Configures policy routing tables `nl` (201) and `usa` (202) and the related `ip rule` entries |
| `wg-monitor.timer`/`wg-monitor.service` | Periodically checks handshakes for `wg-ipsec` and `wg-usa`, restarts on failure, notifies Telegram |
| `pgvpn.target` | Umbrella target that wants the WG interfaces, routing, dnsproxy, and watchdog |

Configuration copies live under `pg.louso.ru/etc/...` inside the repo. After editing locally, use the `scripts/push-configs.sh` workflow to sync back to the host.

---

## 3. Policy Routing and iptables

- Two ipsets are used:
  - `nl_proxy` → traffic marked `0x1`, routed via table `nl` → `wg-ipsec`.
  - `usa_proxy` → traffic marked `0x2`, routed via table `usa` → `wg-usa`.
- `dnsproxy` automatically writes IPs into these sets based on domain patterns (`/root/site_nl`, `/root/site_usa`).
- `pg.louso.ru/etc/iptables/rules.v4` installs:
  - `mangle/PREROUTING` & `mangle/OUTPUT` rules to apply marks for clients (10.200.0.0/24) and the home/dacha LANs.
  - `nat/POSTROUTING` rules to SNAT NL/USA traffic out of the respective WG interfaces and to MASQUERADE client traffic to the public interface.
  - `nat/OUTPUT` DNAT that redirects legacy queries to 192.168.0.200:53 into the local unbound at 127.0.0.1:53.
- Tables `201 nl` and `202 usa` are created by `proxy-route.service`. Check with:
  ```bash
  ip rule show
  ip route show table nl
  ip route show table usa
  ```

If policy routing stops working:
1. Ensure the ipset entries exist (`ipset list nl_proxy`, `ipset list usa_proxy`).
2. Confirm `proxy-route.service` is enabled and the rules are present (`ip rule show`).
3. Apply the saved rules: `iptables-restore < /etc/iptables/rules.v4` followed by `netfilter-persistent save`.

---

## 4. DNS Proxy and Pattern Management

`dnsproxy` (Go service in `dnsproxy/`) listens on 10.200.0.1:53 (UDP/TCP). It handles three routing modes:
- NL patterns → resolve via `10.10.1.2:53` (ipsec.louso).
- USA patterns → resolve via `10.10.2.2:53` (usa.louso).
- Direct mode → resolve via the default upstream (first resolver from `/etc/resolv.conf`, currently 127.0.0.1 via unbound).

### Pattern files
- `/root/site_nl` – domains or substrings for NL.
- `/root/site_usa` – same for USA.

The Telegram bot commands (`/add_nl`, `/add_usa`, `/remove_*`) update these files and apply ipset changes immediately. Manual editing is also possible; the service reloads patterns every 5 seconds.

`/root/map.json` keeps historic domain→IP mappings. It is regenerated automatically.

### Deploying dnsproxy
```
cd dnsproxy
bash deploy.sh   # builds binary, rsyncs to pg.louso.ru, restarts service
```
Ensure the host has Golang installed if you rebuild locally.

---

## 5. WireGuard Client Management

- The central generator is `/root/wg` on `pg.louso.ru` (`pg.louso.ru/root/wg` in repo). It auto-increments `user_index.txt`, appends peers to `/etc/wireguard/wg200.conf`, writes client configs to `/etc/wireguard/clients/<name>.conf`, and triggers `systemctl reload wg-quick@wg200`.
- The Telegram bot exposes `/wg <username>`; it runs the same script and sends the config back to the user as text and as a file attachment.
- `wg200.conf` already includes the home and dacha routers with static routes for their LANs.

When issuing new clients manually:
```
ssh -p 22022 root@pg.louso.ru
/root/wg <username>
```
The resulting file appears in `/etc/wireguard/clients/`.

---

## 6. Monitoring & Alerts

- `wg-monitor.sh` (installed to `/usr/local/bin/`) validates that `wg-ipsec` and `wg-usa` exist and have recent handshakes (default threshold 300s).
- On failure it restarts the WG interface and optionally sends a Telegram message (configure `/etc/wg-monitor.env` with `TELEGRAM_TOKEN` and `TELEGRAM_CHAT_ID`).
- Enabled by `wg-monitor.timer` which runs every 5 minutes from boot. Check status with:
  ```bash
  systemctl status wg-monitor.timer
  journalctl -u wg-monitor.service
  ```
- `dnsproxy` logs to the journal; watch via `journalctl -u dnsproxy -f`.

---

## 7. Deployment Workflow

1. **Pull current configs** (optional but recommended):
   ```bash
   bash scripts/pull-configs.sh pg.louso.ru
   ```
2. **Modify files** in the repo (Git keeps history).
3. **Build / deploy services** as needed (e.g. `dnsproxy/deploy.sh`).
4. **Push configs back**:
   ```bash
   bash scripts/push-configs.sh pg.louso.ru
   ```
   The script syncs `/etc` subtrees without deleting extra files and reloads `systemd`.
5. **Apply iptables** after a change:
   ```bash
   ssh -p 22022 root@pg.louso.ru 'iptables-restore < /etc/iptables/rules.v4 && netfilter-persistent save'
   ```
6. **Verify** using the commands in section 8.

Always commit changes in Git before or after pushing, so the repository reflects the running state.

---

## 8. Operational Check List

| Action | Command |
| --- | --- |
| List WG interfaces/peers | `wg show` |
| Inspect policy rules | `ip rule show` |
| Check mark counters | `iptables -t mangle -L PREROUTING -v` |
| Show ipsets | `ipset list nl_proxy` / `ipset list usa_proxy` |
| Test NL route | `dig @10.200.0.1 lostfilm.tv` → `ip route get <IP> mark 0x1` |
| Test USA route | `dig @10.200.0.1 youtube.com` → `ip route get <IP> mark 0x2` |
| Check watchdog | `journalctl -u wg-monitor.service --since -30m` |
| Validate DNS | `dig @127.0.0.1 example.com` (unbound) |

---

## 9. MikroTik Address List Sync

Домашний и дачный MikroTik должны отправлять только те IP, которые бот относит к NL/USA, через туннели `wg-home`/`wg-dacha`. Для этого используем `ip firewall address-list` + `mangle` → `mark-routing`. Выгрузкой ipset занимается таймер на `pg.louso.ru`.

### Сервер (pg.louso.ru)
1. Проверьте конфиг `/etc/pgvpn/mikrotik-sync.conf` (есть шаблон в репо). `PG_SSH_HOST` можно оставить пустым (локальный ipset). Впишите реальные `ROUTER_*` параметры и добавьте ключ: `ROUTER_home_SSH_OPTS="-i /etc/pgvpn/mikrotik-sync.key -o KexAlgorithms=+diffie-hellman-group14-sha1"` (аналогично для дачи).
2. Обеспечьте авторизацию по ключу на MikroTik:
   ```bash
   ssh-keygen -t rsa -b 4096 -f ~/.ssh/mikrotik-sync
   /user/ssh-keys import user=admin public-key-file=mikrotik-sync.pub  # на MikroTik
   ```
   И пропишите `ROUTER_*_SSH_OPTS="-i ~/.ssh/mikrotik-sync"` в конфиге.
3. Убедитесь, что на pg есть `/usr/local/bin/pg-sync-mikrotik.sh` (обёртка на `scripts/sync-mikrotik-address-lists.sh`).
4. Включите сервис:
   ```bash
   systemctl daemon-reload
   systemctl enable --now pg-sync-mikrotik.timer
   systemctl start pg-sync-mikrotik.service   # первая синхронизация
   systemctl status pg-sync-mikrotik.service
   ```
   Таймер запускается каждые 10 минут; лог смотрим через `journalctl -u pg-sync-mikrotik.service`.

### MikroTik подготовка
1. Включите SSH и импортируйте публичный ключ от pg (`/user/ssh-keys import`), чтобы синк работал без пароля.
2. Убедитесь, что интерфейсы WireGuard к pg уже настроены (`wg-home`, `wg-dacha`).
3. Первая синхронизация: `systemctl start pg-sync-mikrotik.service` (или вручную `CONFIG_FILE=/etc/pgvpn/mikrotik-sync.conf bash /app/pgvpn/scripts/sync-mikrotik-address-lists.sh`).
4. После импорта на MikroTik появятся:
   - address-lists `pg-proxy-nl` / `pg-proxy-usa`;
   - `mangle` правила `pg-sync nl/usa`, присваивающие `routing-mark=pg-to-pg`;
   - маршрут `0.0.0.0/0 routing-mark=pg-to-pg` через адрес pg.
   Для корректной работы убедитесь, что общий default маршрут (без mark) остается через обычный шлюз провайдера.

После запуска только адреса из списков получат routing-mark `pg-to-pg` и будут идти по туннелю к pg, где дальше сработает политика NL/USA.

### Проверка на MikroTik
```
/ip firewall address-list print where list~"pg-proxy"
/ip firewall mangle print where comment~"pg-sync"
/ip route print where comment="pg-sync to-pg"
```

В логах mark-routing можно увидеть счётчики трафика. Основной default маршрут (через ISP) остаётся нетронутым.

Если таймер не используется, скрипт можно запускать вручную или через `cron`. В текущем деплое задание уже висит на `systemd`.

---

## 9. Troubleshooting

1. **Pattern added but traffic still goes via pg (eth0)**
   - Ensure the domain resolved recently by dnsproxy (see `journalctl -u dnsproxy` or `/root/map.json`).
   - Confirm the IP is present in `ipset list usa_proxy/nl_proxy`.
   - Check `iptables -t mangle -L PREROUTING -v` for packet counters.

2. **WireGuard tunnel down**
   - Look at `wg show`, `systemctl status wg-quick@wg-usa` (or `wg-ipsec`).
   - For persistent flaps confirm `wg-monitor.timer` is running.

3. **dnsproxy errors `connection refused`**
   - Restart `unbound` or point `/etc/resolv.conf` to another resolver.
   - `systemctl restart unbound dnsproxy`.

4. **Telegram bot double-start warning**
   - Only one dnsproxy instance should run. Kill any stray processes (`pkill dnsproxy`) before restarting the systemd unit.

5. **Scripts fail after repo updates**
   - Re-run `bash scripts/pull-configs.sh` to refresh local copies.
   - Inspect `spec.txt` (updated version) for guidelines on dynamic vs static files.

---

## 10. Useful Paths

- `pg.louso.ru/etc/systemd/system/pgvpn.target` – ensures everything starts after boot.
- `pg.louso.ru/usr/local/bin/wg-monitor.sh` – watchdog logic.
- `pg.louso.ru/etc/wg-monitor.env` – Telegram settings.
- `pg.louso.ru/root/wg` – WG client provisioning script.
- `dnsproxy/` – Go sources, systemd unit, deploy script.

Keep secrets (tokens, private keys) **out of Git**. Files such as `/etc/wireguard/*.key`, `/root/site_*`, `/root/map.json`, `/root/bot.db` are dynamic and must stay on the host.

---

## 11. Quick Recovery Procedure

1. Bring up WireGuard interfaces:
   ```bash
   systemctl restart wg-quick@wg200 wg-quick@wg-ipsec wg-quick@wg-usa
   ```
2. Re-apply routing:
   ```bash
   systemctl restart proxy-route.service
   ```
3. Restart DNS stack:
   ```bash
   systemctl restart unbound dnsproxy
   ```
4. Check watchdog:
   ```bash
   systemctl restart wg-monitor.timer
   ```
5. Validate with commands from section 8.

With these steps the system should return to a known-good state.

---

_Последнее обновление: сентябрь 2025._
