#!/usr/bin/env bash
# Максимальный импорт стикеров, реакций, кастом-эмодзи, эффектов и дайс-анимаций.
#
# Без этого шага стикеры, реакции и эмодзи в клиентах будут пустыми:
# сервер не содержит наборов, их надо один раз выгрузить из Telegram.
#
# Запуск (от root):
#   bash seeds.sh
#
# Опционально, чтобы Telegram не ответил API_ID_PUBLISHED_FLOOD:
#   TG_API_ID=123456 TG_API_HASH=... bash seeds.sh
# (свои api_id/api_hash берутся на https://my.telegram.org)
#
# Занимает от 1 до 6 часов и 1-5 ГБ диска: выгружаются все featured-наборы,
# кастом-эмодзи, реакции, эффекты сообщений, анимации дайсов и подарки.

set -euo pipefail
trap 'echo "---- ОШИБКА: строка $LINENO, код $?" >&2' ERR

DIR=/opt/gramsrv
SESSION="${SESSION:-/root/tg-seeds.session}"
SEED="${DIR}/data/sticker-seed"
export PATH="/usr/local/go/bin:${PATH}"
[[ -n "${TG_API_ID:-}" ]] && export TG_API_ID
[[ -n "${TG_API_HASH:-}" ]] && export TG_API_HASH

log() { printf '\n==== %s\n' "$*"; }
die() { printf '\nОШИБКА: %s\n' "$*" >&2; exit 1; }

[[ ${EUID} -eq 0 ]] || die "запускать от root"
cd "${DIR}" || die "нет ${DIR}, сначала запустите install.sh"
command -v go >/dev/null || die "не найден go; установите Go 1.21+ или перезапустите install.sh"

free_gb=$(df -BG --output=avail "${DIR}" | tail -1 | tr -dc '0-9')
if (( free_gb < 5 )); then
    echo "ВНИМАНИЕ: свободно ${free_gb} ГБ. Полный импорт может занять 5+ ГБ."
    read -r -p "Продолжить? [y/N] " ans
    [[ ${ans,,} == y ]] || exit 1
fi

log "0/6 сборка stickerfetch"
go build -o bin/stickerfetch ./cmd/stickerfetch
go build -o bin/giftfetch ./cmd/giftfetch

log "1/6 вход в Telegram по QR"
if [[ -f ${SESSION} ]]; then
    echo "сессия ${SESSION} уже есть — пропускаю вход"
else
    echo "Сейчас появится QR-код. Откройте Telegram на телефоне:"
    echo "  Настройки -> Устройства -> Подключить устройство — и отсканируйте."
    read -r -p "Enter, когда будете готовы... " _
    SESSION="${SESSION}" ./bin/giftfetch -login-qr
fi

run_fetch() { # <каталог> <таймаут> <спеки...>
    local out=$1 timeout=$2; shift 2
    mkdir -p "${out}"
    echo "-> ${out}: $*"
    SESSION="${SESSION}" ./bin/stickerfetch -timeout "${timeout}" "${out}" "$@" || {
        echo "ВНИМАНИЕ: шаг завершился с ошибкой, продолжаю остальные"
        return 0
    }
}

log "2/6 реакции и эффекты сообщений"
run_fetch "${SEED}" 30m reactions
run_fetch "${SEED}" 30m effects

log "3/6 системные наборы: анимированные эмодзи, дайсы, подарки"
run_fetch "${SEED}/telegram_default_stickers_export" 1h \
    animated_emoji emoji_default_statuses emoji_channel_default_statuses \
    emoji_default_topic_icons animated_emoji_animations emoji_generic_animations \
    "dice:🎲" "dice:🎯" "dice:🏀" "dice:⚽" "dice:🎳" "dice:🎰" premium_gifts

log "4/6 все featured-стикеры (долго)"
run_fetch "${SEED}/telegram_stickers_export" 6h featured_stickers

log "5/6 все featured-эмодзи (долго)"
run_fetch "${SEED}/telegram_emoji_export" 6h featured_emoji

log "6/6 права, лимит наборов, перезапуск"
# без лимита на количество наборов
if grep -q '^TELESRV_STICKER_SEED_MAX_SETS=' .env; then
    sed -i 's|^TELESRV_STICKER_SEED_MAX_SETS=.*|TELESRV_STICKER_SEED_MAX_SETS=0|' .env
else
    echo 'TELESRV_STICKER_SEED_MAX_SETS=0' >> .env
fi
chown -R telesrv:telesrv "${DIR}/data"
systemctl restart telesrv
sleep 5
systemctl is-active --quiet telesrv || {
    journalctl -u telesrv -n 60 --no-pager
    die "сервер не поднялся после импорта"
}

echo
echo "размер базы стикеров: $(du -sh "${SEED}" 2>/dev/null | awk '{print $1}')"
cat <<SUMMARY

================================================================
 Импорт завершён.

 Проверка: откройте чат в OwpenGram -> стикеры, эмодзи, реакции.
 Если чего-то нет — journalctl -u telesrv -n 100

 GIF: до 50 файлов .gif/.mp4 в ${DIR}/data/gifs
      (chown -R telesrv:telesrv ${DIR}/data/gifs, затем systemctl restart telesrv)
================================================================
SUMMARY
