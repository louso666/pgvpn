#!/bin/bash

# Скрипт идемпотентной установки пакетов для pg.gena.host
# Можно запускать многократно без вреда

set -e

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

log "🚀 Начинаем установку пакетов..."

# Обновляем списки пакетов
log "📦 Обновление списков пакетов..."
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq

# Основные пакеты
log "🔧 Установка основных пакетов..."
apt-get install -y \
    vim \
    htop \
    curl \
    wget \
    git \
    tree \
    mc \
    tmux \
    screen \
    unzip \
    zip \
    bash-completion \
    rsync

# Сетевые утилиты
log "🌐 Установка сетевых утилит..."
apt-get install -y \
    net-tools \
    iputils-ping \
    traceroute \
    nmap \
    iperf3 \
    tcpdump \
    netcat-openbsd \
    dnsutils \
    iptables-persistent

# WireGuard
log "🔒 Установка WireGuard..."
apt-get install -y \
    wireguard \
    wireguard-tools

# Дополнительные утилиты
log "🛠️ Установка дополнительных утилит..."
apt-get install -y \
    lsof \
    strace \
    tcpflow \
    ngrep \
    mtr-tiny \
    telnet \
    whois \
    jq \
    python3-pip \
    python3-dev

# Nginx для веб-сервера
log "🌍 Установка Nginx..."
apt-get install -y nginx

# Очистка кеша
log "🧹 Очистка кеша пакетов..."
apt-get autoremove -y
apt-get autoclean

log "✅ Установка пакетов завершена успешно" 