#!/usr/bin/env bash
# Завершение установки, когда бинарник уже собран (например, на другой машине)
# и лежит в /opt/gramsrv/bin/telesrv.
#
# Запуск (от root):
#   bash /root/finish-install.sh

set -euo pipefail
trap 'echo "---- ОШИБКА: строка $LINENO, код $?" >&2' ERR

DIR=/opt/gramsrv
PORT="${PORT:-2398}"

log() { printf '\n==== %s\n' "$*"; }
die() { printf '\nОШИБКА: %s\n' "$*" >&2; exit 1; }

[[ ${EUID} -eq 0 ]] || die "запускать от root"
[[ -f ${DIR}/.env ]] || die "нет ${DIR}/.env — установка не дошла до конфигурации"
[[ -f ${DIR}/bin/telesrv ]] || die "нет ${DIR}/bin/telesrv — сначала загрузите бинарник"

log "1/5 остановка незавершённой сборки"
pkill -f 'go build' 2>/dev/null || true
pkill -f 'setup.sh' 2>/dev/null || true
sleep 2

log "2/5 бинарник и пользователь"
chmod +x "${DIR}/bin/telesrv"
id -u telesrv >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin -d "${DIR}" telesrv
chown -R telesrv:telesrv "${DIR}"

log "3/5 systemd"
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

log "4/5 ожидание миграций и порта"
for _ in $(seq 1 240); do
    if ss -lnt 2>/dev/null | grep -q ":${PORT} "; then break; fi
    sleep 2
done
systemctl is-active --quiet telesrv || {
    journalctl -u telesrv -n 60 --no-pager
    die "telesrv не запустился"
}
ss -lnt | grep -q ":${PORT} " || {
    journalctl -u telesrv -n 60 --no-pager
    die "порт ${PORT} не открылся"
}

log "5/5 firewall"
ufw allow 22/tcp >/dev/null 2>&1 || true
ufw allow "${PORT}/tcp" >/dev/null
ufw allow 12399/udp >/dev/null
ufw allow 12400/udp >/dev/null
ufw allow 12500:12999/udp >/dev/null
ufw --force enable >/dev/null

ADVERTISE_IP=$(grep -oP '(?<=^TELESRV_ADVERTISE_IP=).*' "${DIR}/.env" 2>/dev/null || true)
DEV_CODE=$(grep -oP '(?<=^TELESRV_DEV_AUTH_CODE=).*' "${DIR}/.env" 2>/dev/null || true)
BRAND=$(grep -oP '(?<=^TELESRV_BRAND_PRODUCT_NAME=).*' "${DIR}/.env" 2>/dev/null || true)

curl -fsSL "https://raw.githubusercontent.com/vmitin9302-ctrl/vedmagramm/main/deploy/seeds.sh" -o /root/seeds.sh 2>/dev/null || true
curl -fsSL "https://raw.githubusercontent.com/vmitin9302-ctrl/vedmagramm/main/deploy/brand.sh" -o /root/brand.sh 2>/dev/null || true

curl -fsS --max-time 5 "http://127.0.0.1:${PORT}/owpengram/server-info" >/dev/null || echo "ВНИМАНИЕ: server-info не ответил"

cat <<SUMMARY

================================================================
 Готово.

 Адрес сервера : ${ADVERTISE_IP:-IP}:${PORT}
 Код входа     : ${DEV_CODE:-см. .env}
 Название      : ${BRAND:-см. .env}

 Дальше:
 1) bash /root/brand.sh   — имя, описание и иконка «Ведьмаграм»
 2) bash /root/seeds.sh   — стикеры и реакции (долго)
 3) Откройте порт ${PORT}/tcp в панели Timeweb

 Логи   : journalctl -u telesrv -f
 Рестарт: systemctl restart telesrv
================================================================
SUMMARY
