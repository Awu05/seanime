#!/bin/sh
set -e

QBIT_WEBUI_PORT="${QBIT_WEBUI_PORT:-8081}"
QBIT_USERNAME="${QBIT_USERNAME:-admin}"
QBIT_PASSWORD="${QBIT_PASSWORD:-adminadmin}"

# Determine qBittorrent config directory based on user
if [ "$(id -u)" = "0" ]; then
    QBIT_CONF_DIR="/root/.config/qBittorrent"
else
    QBIT_CONF_DIR="$(eval echo ~$(whoami))/.config/qBittorrent"
fi

mkdir -p "$QBIT_CONF_DIR"

# Generate PBKDF2-HMAC-SHA512 password hash (100,000 iterations) using Python
generate_qbit_password() {
    python3 -c "
import hashlib, os, base64, sys
password = sys.argv[1].encode()
salt = os.urandom(16)
dk = hashlib.pbkdf2_hmac('sha512', password, salt, 100000)
print('@ByteArray(' + base64.b64encode(salt).decode() + ':' + base64.b64encode(dk).decode() + ')')
" "$1"
}

PASSWORD_HASH=$(generate_qbit_password "$QBIT_PASSWORD")
QBIT_CONF="$QBIT_CONF_DIR/qBittorrent.conf"

# Write qBittorrent config on first run only
if [ ! -f "$QBIT_CONF" ]; then
    cat > "$QBIT_CONF" <<EOF
[Preferences]
WebUI\Port=${QBIT_WEBUI_PORT}
WebUI\Username=${QBIT_USERNAME}
WebUI\Password_PBKDF2="${PASSWORD_HASH}"
WebUI\CSRFProtection=false
WebUI\ClickjackingProtection=false
WebUI\HostHeaderValidation=false
WebUI\LocalHostAuth=false
WebUI\MaxAuthenticationFailCount=0
WebUI\BanDuration=0

[BitTorrent]
Session\DefaultSavePath=/downloads

[Meta]
MigrationVersion=6
EOF
else
    # Helper to update or add a setting under [Preferences]
    update_setting() {
        KEY="$1"
        VALUE="$2"
        ESCAPED_KEY=$(echo "$KEY" | sed 's|\\|\\\\|g')
        if grep -q "^${ESCAPED_KEY}=" "$QBIT_CONF"; then
            sed -i "s|^${ESCAPED_KEY}=.*|${KEY}=${VALUE}|" "$QBIT_CONF"
        else
            sed -i "/^\[Preferences\]/a ${KEY}=${VALUE}" "$QBIT_CONF"
        fi
    }

    # Update credentials and port from env vars
    update_setting "WebUI\\\\Port" "${QBIT_WEBUI_PORT}"
    update_setting "WebUI\\\\Username" "${QBIT_USERNAME}"
    update_setting "WebUI\\\\Password_PBKDF2" "\"${PASSWORD_HASH}\""

    # Ensure security settings are present
    update_setting "WebUI\\\\CSRFProtection" "false"
    update_setting "WebUI\\\\ClickjackingProtection" "false"
    update_setting "WebUI\\\\HostHeaderValidation" "false"
    update_setting "WebUI\\\\LocalHostAuth" "false"
    update_setting "WebUI\\\\MaxAuthenticationFailCount" "0"
    update_setting "WebUI\\\\BanDuration" "0"
fi

# Generate supervisord config with the configured port
cat > /tmp/supervisord.conf <<EOF
[supervisord]
nodaemon=true
logfile=/var/log/supervisor/supervisord.log
pidfile=/tmp/supervisord.pid

[program:qbittorrent]
command=qbittorrent-nox --webui-port=${QBIT_WEBUI_PORT}
autostart=true
autorestart=true
priority=1
stdout_logfile=/dev/stdout
stdout_logfile_maxbytes=0
stderr_logfile=/dev/stderr
stderr_logfile_maxbytes=0

[program:seanime]
command=/app/seanime --host 0.0.0.0
directory=/app
autostart=true
autorestart=true
priority=10
startsecs=5
stdout_logfile=/dev/stdout
stdout_logfile_maxbytes=0
stderr_logfile=/dev/stderr
stderr_logfile_maxbytes=0
EOF

exec /usr/bin/supervisord -c /tmp/supervisord.conf
