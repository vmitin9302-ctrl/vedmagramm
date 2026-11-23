#!/usr/bin/env bash
# Установка gramsrv (свой Telegram-совместимый сервер) на Ubuntu 22.04/24.04.
#
# Запуск (от root):
#   bash install.sh
#
# Параметры через переменные окружения:
#   ADVERTISE_IP  внешний IP сервера   (по умолчанию определяется автоматически)
#   PORT          порт MTProto         (по умолчанию 2398)
#   BRAND         название мессенджера (по умолчанию "Наш Телеграм")
#   DEV_CODE      код входа            (по умолчанию случайные 6 цифр)
#   GIF_PROVIDER  klipy|giphy|off      (по умолчанию off, интернет-поиск GIF)
#   GIF_API_KEY   ключ провайдера      (обязателен при GIF_PROVIDER != off)
#
# Скрипт идемпотентен: повторный запуск не ломает базу и .env.

set -euo pipefail
trap 'echo "---- ОШИБКА: строка $LINENO, код $?" >&2' ERR

# Свой форк сервера (по умолчанию собираем ваш форк с GIF-поиском):
#   REPO_URL=https://github.com/iamxvbaba/gramsrv bash install.sh   # чистый апстрим
REPO_URL="${REPO_URL:-https://github.com/vmitin9302-ctrl/vedmagramm}"
BRANCH="${BRANCH:-main}"
DIR="/opt/gramsrv"
DB_NAME="telesrv_main"
DB_USER="telesrv"
# Пароль базы фиксирован намеренно: setup.sh из репозитория ищет роль
# telesrv/telesrv. База слушает только 127.0.0.1.
DB_PASS="telesrv"
PORT="${PORT:-2398}"
BRAND="${BRAND:-Ведьмаграм}"

log() { printf '\n==== %s\n' "$*"; }
die() { printf '\nОШИБКА: %s\n' "$*" >&2; exit 1; }

[[ ${EUID} -eq 0 ]] || die "запускать от root (sudo -i)"
command -v apt-get >/dev/null || die "нужен Ubuntu/Debian"
. /etc/os-release
[[ ${ID:-} == ubuntu || ${ID:-} == debian ]] || die "поддерживается только Ubuntu/Debian"

log "1/8 пакеты"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq git curl ca-certificates python3 openssl \
    postgresql redis-server ffmpeg ufw >/dev/null

ADVERTISE_IP="${ADVERTISE_IP:-$(curl -fsS4 --max-time 10 https://api.ipify.org || true)}"
[[ -n ${ADVERTISE_IP} ]] || die "не удалось определить внешний IP, запустите: ADVERTISE_IP=1.2.3.4 bash install.sh"
DEV_CODE="${DEV_CODE:-$(shuf -i 100000-999999 -n 1)}"

log "2/8 swap (нужен для сборки на 2-4 ГБ RAM)"
mem_kb=$(awk '/MemTotal/{print $2}' /proc/meminfo)
if (( mem_kb < 3800000 )) && [[ ! -f /swapfile ]]; then
    fallocate -l 2G /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count=2048 status=none
    chmod 600 /swapfile
    mkswap /swapfile >/dev/null
    swapon /swapfile
    grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
    echo "создан /swapfile 2G"
fi

log "3/8 PostgreSQL и Redis"
systemctl enable --now postgresql redis-server >/dev/null 2>&1
for _ in $(seq 1 30); do pg_isready -q && break; sleep 1; done

if su - postgres -c "psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname='${DB_USER}'\"" | grep -q 1; then
    su - postgres -c "psql -c \"ALTER ROLE ${DB_USER} LOGIN PASSWORD '${DB_PASS}' SUPERUSER;\"" >/dev/null
else
    echo "создаю роль ${DB_USER}"
    su - postgres -c "psql -c \"CREATE ROLE ${DB_USER} LOGIN PASSWORD '${DB_PASS}' SUPERUSER;\""
fi
if ! su - postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'\"" | grep -q 1; then
    echo "создаю базу ${DB_NAME}"
    su - postgres -c "psql -c \"CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};\""
fi

log "4/8 код"
if [[ -d ${DIR}/.git ]]; then
    git -C "${DIR}" pull --ff-only || echo "ВНИМАНИЕ: не удалось обновить код, собираю текущую версию"
else
    git clone --depth 1 --branch "${BRANCH}" "${REPO_URL}" "${DIR}"
fi

log "5/8 конфигурация .env"
cd "${DIR}"
set_kv() {
    local k=$1 v=$2
    if grep -q "^${k}=" .env; then
        sed -i "s|^${k}=.*|${k}=${v}|" .env
    else
        printf '%s=%s\n' "${k}" "${v}" >> .env
    fi
}
if [[ ! -f .env ]]; then
    cp .env.example .env
    echo "создан .env из .env.example"
fi
set_kv TELESRV_ADVERTISE_IP         "${ADVERTISE_IP}"
set_kv TELESRV_LISTEN               "0.0.0.0:${PORT}"
set_kv TELESRV_POSTGRES_DSN         "postgres://${DB_USER}:${DB_PASS}@127.0.0.1:5432/${DB_NAME}?sslmode=disable"
set_kv TELESRV_REDIS_ADDR           "127.0.0.1:6379"
set_kv TELESRV_DEV_AUTH_CODE        "${DEV_CODE}"
set_kv TELESRV_ALLOW_INSECURE_DEVELOPMENT_AUTH "true"
set_kv TELESRV_BRAND_PRODUCT_NAME   "${BRAND}"
set_kv TELESRV_PUBLIC_APP_NAME      "${BRAND}"
set_kv TELESRV_BRAND_PRODUCT_USERNAME "vedmagramm"
set_kv TELESRV_DEFAULT_COUNTRY_CODE "RU"
set_kv TELESRV_PUBLIC_BASE_URL      "http://${ADVERTISE_IP}:${PORT}"
set_kv TELESRV_PUBLIC_WEB_BASE_URL  "http://${ADVERTISE_IP}:${PORT}"
set_kv TELESRV_GIF_SEED_DIR         "data/gifs"
set_kv TELESRV_STICKER_SEED_DIR     "data/sticker-seed"
# Необязательно: интернет-поиск GIF. Задайте перед запуском:
#   GIF_PROVIDER=klipy GIF_API_KEY=... bash install.sh
if [[ -n ${GIF_PROVIDER:-} ]]; then
    set_kv TELESRV_GIF_PROVIDER "${GIF_PROVIDER}"
fi
if [[ -n ${GIF_API_KEY:-} ]]; then
    set_kv TELESRV_GIF_API_KEY "${GIF_API_KEY}"
fi
chmod 600 .env

log "6/8 сборка (на 2 vCPU это 10-25 минут, это нормально)"
./setup.sh --no-start --no-demo-channels --skip-subscribe \
    --dsn "postgres://${DB_USER}:${DB_PASS}@127.0.0.1:5432/${DB_NAME}?sslmode=disable" \
    --redis-addr "127.0.0.1:6379"

log "7/8 systemd"
id -u telesrv >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin -d "${DIR}" telesrv
chown -R telesrv:telesrv "${DIR}"
cat >/etc/systemd/system/telesrv.service <<'EOF'
[Unit]
Description=gramsrv — self-hosted Telegram-compatible server
After=network-online.target postgresql.service redis-server.service
Wants=network-online.target

[Service]
User=telesrv
Group=telesrv
WorkingDirectory=/opt/gramsrv
ExecStart=/opt/gramsrv/bin/telesrv
Restart=always
RestartSec=3
LimitNOFILE=65535
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now telesrv >/dev/null 2>&1

echo "ждём, пока сервер применит миграции и откроет порт..."
for _ in $(seq 1 180); do
    if ss -lnt 2>/dev/null | grep -q ":${PORT} "; then break; fi
    sleep 2
done
systemctl is-active --quiet telesrv || {
    journalctl -u telesrv -n 40 --no-pager
    die "telesrv не запустился, смотрите лог выше"
}
if ! ss -lnt 2>/dev/null | grep -q ":${PORT} "; then
    journalctl -u telesrv -n 60 --no-pager
    die "порт ${PORT} не открылся за 6 минут — смотрите лог выше"
fi

log "8/8 firewall"
ufw allow 22/tcp >/dev/null 2>&1 || true
ufw allow "${PORT}/tcp" >/dev/null
ufw allow 12399/udp >/dev/null       # групповые звонки (SFU)
ufw allow 12400/udp >/dev/null       # TURN
ufw allow 12500:12999/udp >/dev/null # TURN relay
ufw --force enable >/dev/null

SERVER_INFO=$(curl -fsS --max-time 5 "http://127.0.0.1:${PORT}/owpengram/server-info" || true)
[[ -n ${SERVER_INFO} ]] || echo "ВНИМАНИЕ: /owpengram/server-info не ответил, проверьте journalctl -u telesrv"

curl -fsSL "https://raw.githubusercontent.com/vmitin9302-ctrl/vedmagramm/main/deploy/seeds.sh" -o /root/seeds.sh 2>/dev/null || true

cat <<SUMMARY

================================================================
 Готово.

 Адрес сервера : ${ADVERTISE_IP}:${PORT}
 Код входа     : ${DEV_CODE}
 Название      : ${BRAND}

 Дальше:
 1) В панели облака откройте порты ${PORT}/tcp и UDP 12399, 12400, 12500-12999
 2) Друзья добавляют сервер "${ADVERTISE_IP}:${PORT}" в OwpenGram
 3) Стикеры и реакции — запустите: bash /root/seeds.sh

 Логи   : journalctl -u telesrv -f
 Рестарт: systemctl restart telesrv
================================================================
SUMMARY
