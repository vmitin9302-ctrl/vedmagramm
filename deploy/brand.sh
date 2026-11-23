#!/usr/bin/env bash
# Брендинг сервера: название, описание и иконка, которые видят клиенты.
#
# Запуск (от root), после server-install.sh:
#   bash /root/brand.sh
#
# Меняет только серверную часть: имя продукта в системных сообщениях и
# идентичность сервера в OwpenGram. Иконка самого приложения (на домашнем
# экране) — это пересборка клиента, отдельная задача.

set -euo pipefail
trap 'echo "---- ОШИБКА: строка $LINENO, код $?" >&2' ERR

DIR=/opt/gramsrv
RAW="https://raw.githubusercontent.com/vmitin9302-ctrl/vedmagramm/main/deploy/branding"
NAME="Ведьмаграм"
DESC="Личный мессенджер для своих"
USERNAME="vedmagramm"

log() { printf '\n==== %s\n' "$*"; }
die() { printf '\nОШИБКА: %s\n' "$*" >&2; exit 1; }

[[ ${EUID} -eq 0 ]] || die "запускать от root"
[[ -f ${DIR}/.env ]] || die "нет ${DIR}/.env — сначала запустите server-install.sh"

log "1/3 переменные бренда в .env"
cd "${DIR}"
set_kv() {
    local k=$1 v=$2
    if grep -q "^${k}=" .env; then
        sed -i "s|^${k}=.*|${k}=${v}|" .env
    else
        printf '%s=%s\n' "${k}" "${v}" >> .env
    fi
}
set_kv TELESRV_BRAND_PRODUCT_NAME      "${NAME}"
set_kv TELESRV_PUBLIC_APP_NAME         "${NAME}"
set_kv TELESRV_BRAND_PRODUCT_USERNAME  "${USERNAME}"
set_kv TELESRV_BRAND_DESKTOP_APP_NAME  "${NAME} Desktop"
set_kv TELESRV_BRAND_ANDROID_APP_NAME  "${NAME}"
set_kv TELESRV_BRAND_IOS_APP_NAME      "${NAME}"
set_kv TELESRV_BRAND_WEB_K_APP_NAME    "${NAME} Web"

log "2/3 идентичность сервера: имя, описание, иконка"
mkdir -p "${DIR}/data/identity"
cat > "${DIR}/data/identity/identity.json" <<EOF
{"name":"${NAME}","description":"${DESC}","icon_ext":".png"}
EOF
curl -fsSL "${RAW}/icon.png" -o "${DIR}/data/identity/icon.png"
chown -R telesrv:telesrv "${DIR}/data/identity"

log "3/3 перезапуск"
systemctl restart telesrv
sleep 4
systemctl is-active --quiet telesrv || {
    journalctl -u telesrv -n 40 --no-pager
    die "сервер не поднялся после смены бренда"
}

echo
echo "Проверка идентичности:"
curl -fsS "http://127.0.0.1:2398/owpengram/server-info" || true
cat <<SUMMARY

================================================================
 Бренд применён: ${NAME}

 Что изменилось:
 - системный аккаунт и сообщения подписаны «${NAME}»;
 - в OwpenGram при добавлении сервера видно «${NAME}» и иконку.

 Посмотреть: удалите сервер в OwpenGram и добавьте заново
 (или перезапустите приложение).
================================================================
SUMMARY
