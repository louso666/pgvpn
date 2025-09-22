# Публичные IP серверов
PG_PUB=89.253.219.146
IPSEC_PUB=77.238.252.171
USA_PUB=172.252.212.68

# Порты (можно менять, но разнеси)
PG_IPSEC_PORT=51820
PG_USA_PORT=51821

# Адреса point-to-point линков (/30 удобно)
# Линк pg <-> ipsec
PG_IPSEC_PG_ADDR=10.10.1.1/30
PG_IPSEC_IPSEC_ADDR=10.10.1.2/30

# Линк pg <-> usa
PG_USA_PG_ADDR=10.10.2.1/30
PG_USA_USA_ADDR=10.10.2.2/30

# SSH доступ
SSH_PORT=22022
SSH_USER=root

# Дерево на ha.louso.ru
ROOT=/app/pgvpn
