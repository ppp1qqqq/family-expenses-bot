#!/bin/sh
# Включает пароль на панель, если заданы WEB_USER и WEB_PASSWORD. Без них панель открыта (локальный запуск).
# Скрипт запускается официальным entrypoint образа nginx до старта сервера.
set -eu

conf=/etc/nginx/auth.conf

if [ -n "${WEB_PASSWORD:-}" ]; then
    printf '%s:%s\n' "${WEB_USER:-admin}" "$(openssl passwd -apr1 "$WEB_PASSWORD")" > /etc/nginx/.htpasswd
    printf 'auth_basic "Family expenses";\nauth_basic_user_file /etc/nginx/.htpasswd;\n' > "$conf"
    echo "basic-auth: enabled for user ${WEB_USER:-admin}"
else
    : > "$conf"
    echo "basic-auth: disabled (WEB_PASSWORD is empty)"
fi
