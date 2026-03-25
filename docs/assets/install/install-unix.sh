#!/bin/sh

set -eu

REPO_OWNER="idefav"
REPO_NAME="ssh-tunnel"
RELEASE_API_URL="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
RELEASE_DOWNLOAD_BASE="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest/download"

INSTALL_BINARY_PATH="/usr/local/bin/ssh-tunnel"
CONFIG_PATH="/etc/ssh-tunnel/config.properties"
SYSTEMD_UNIT_PATH="/etc/systemd/system/ssh-tunnel.service"
SYSV_SCRIPT_PATH="/etc/init.d/ssh-tunnel"
DARWIN_PLIST_PATH="/Library/LaunchDaemons/com.idefav.ssh-tunnel.plist"

DRY_RUN=0
PROMPT_INPUT="/dev/tty"

if [ ! -r "$PROMPT_INPUT" ]; then
    PROMPT_INPUT="/dev/stdin"
fi

while [ "$#" -gt 0 ]; do
    case "$1" in
        --dry-run)
            DRY_RUN=1
            ;;
        *)
            printf 'Unsupported option: %s\n' "$1" >&2
            exit 1
            ;;
    esac
    shift
done

log() {
    printf '%s\n' "$*"
}

fail() {
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

download_to_stdout() {
    url=$1
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url"
        return
    fi
    if command -v wget >/dev/null 2>&1; then
        wget -qO- "$url"
        return
    fi
    fail "curl or wget is required"
}

download_to_file() {
    url=$1
    output=$2
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" -o "$output"
        return
    fi
    if command -v wget >/dev/null 2>&1; then
        wget -qO "$output" "$url"
        return
    fi
    fail "curl or wget is required"
}

expand_path() {
    case "$1" in
        "~")
            printf '%s\n' "$HOME"
            ;;
        "~/"*)
            # Avoid using "~" in parameter-expansion patterns because macOS /bin/sh
            # can keep the literal "~/" and produce "$HOME/~/" style paths.
            relative_path=${1#?}
            relative_path=${relative_path#/}
            printf '%s/%s\n' "$HOME" "$relative_path"
            ;;
        *)
            printf '%s\n' "$1"
            ;;
    esac
}

prompt_default() {
    prompt_text=$1
    default_value=$2
    while :; do
        printf '%s [%s]: ' "$prompt_text" "$default_value" >&2
        IFS= read -r answer <"$PROMPT_INPUT"
        if [ -n "$answer" ]; then
            printf '%s\n' "$answer"
            return
        fi
        if [ -n "$default_value" ]; then
            printf '%s\n' "$default_value"
            return
        fi
    done
}

prompt_required() {
    prompt_text=$1
    while :; do
        printf '%s: ' "$prompt_text" >&2
        IFS= read -r answer <"$PROMPT_INPUT"
        if [ -n "$answer" ]; then
            printf '%s\n' "$answer"
            return
        fi
        log "Value cannot be empty."
    done
}

is_numeric_port() {
    case "$1" in
        ''|*[!0-9]*)
            return 1
            ;;
    esac
    [ "$1" -ge 1 ] && [ "$1" -le 65535 ]
}

port_in_use() {
    port=$1
    if command -v ss >/dev/null 2>&1; then
        ss -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "(^|:)$port$"
        return $?
    fi
    if command -v lsof >/dev/null 2>&1; then
        lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
        return $?
    fi
    if command -v netstat >/dev/null 2>&1; then
        netstat -an 2>/dev/null | grep -E "(^|[.:])${port}[[:space:]].*LISTEN" >/dev/null 2>&1
        return $?
    fi
    if command -v python3 >/dev/null 2>&1; then
        python3 - "$port" <<'PY'
import socket
import sys

port = int(sys.argv[1])
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
try:
    sock.bind(("127.0.0.1", port))
except OSError:
    sys.exit(0)
finally:
    try:
        sock.close()
    except OSError:
        pass
sys.exit(1)
PY
        return $?
    fi
    return 1
}

choose_port() {
    label=$1
    default_port=$2
    if port_in_use "$default_port"; then
        suggested=$((default_port + 1000))
        while :; do
            value=$(prompt_default "${label} port is already in use, enter a new port" "$suggested")
            if is_numeric_port "$value"; then
                if port_in_use "$value"; then
                    log "Port $value is also in use."
                else
                    printf '%s\n' "$value"
                    return
                fi
            else
                log "Please enter a valid port between 1 and 65535."
            fi
        done
    fi
    printf '%s\n' "$default_port"
}

admin_url_from_config() {
    config_path=$1
    if [ -f "$config_path" ]; then
        admin_line=$(grep '^admin\.address=' "$config_path" 2>/dev/null | tail -n 1 || true)
        if [ -n "$admin_line" ]; then
            admin_value=${admin_line#admin.address=}
            admin_port=${admin_value##*:}
            if is_numeric_port "$admin_port"; then
                printf 'http://127.0.0.1:%s/view/version\n' "$admin_port"
                return
            fi
        fi
    fi
    printf 'http://127.0.0.1:1083/view/version\n'
}

run_root() {
    if [ "$DRY_RUN" -eq 1 ]; then
        log "[dry-run] $*"
        return 0
    fi
    if [ -n "${SUDO_CMD}" ]; then
        "${SUDO_CMD}" "$@"
        return
    fi
    "$@"
}

write_root_file() {
    destination=$1
    source=$2
    if [ "$DRY_RUN" -eq 1 ]; then
        log "[dry-run] write $destination"
        return 0
    fi
    if [ -n "${SUDO_CMD}" ]; then
        "${SUDO_CMD}" cp "$source" "$destination"
        return
    fi
    cp "$source" "$destination"
}

detect_platform() {
    uname_os=$(uname -s 2>/dev/null | tr '[:upper:]' '[:lower:]')
    case "$uname_os" in
        linux*)
            OS_NAME="linux"
            ;;
        darwin*)
            OS_NAME="darwin"
            ;;
        *)
            fail "unsupported operating system: ${uname_os}"
            ;;
    esac

    uname_arch=$(uname -m 2>/dev/null)
    case "$uname_arch" in
        x86_64|amd64)
            ARCH_NAME="amd64"
            ;;
        arm64|aarch64)
            ARCH_NAME="arm64"
            ;;
        *)
            fail "unsupported architecture: ${uname_arch}"
            ;;
    esac
}

detect_service_mode() {
    if [ "$OS_NAME" = "linux" ]; then
        if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
            SERVICE_KIND="systemd"
        elif [ -d /etc/init.d ]; then
            SERVICE_KIND="sysv"
        else
            fail "unsupported Linux init system; expected systemd or /etc/init.d"
        fi
    else
        SERVICE_KIND="launchd"
    fi
}

has_existing_installation() {
    if [ -f "$CONFIG_PATH" ] || [ -f "$INSTALL_BINARY_PATH" ]; then
        return 0
    fi
    case "$OS_NAME" in
        linux)
            [ -f "$SYSTEMD_UNIT_PATH" ] || [ -f "$SYSV_SCRIPT_PATH" ]
            return $?
            ;;
        darwin)
            [ -f "$DARWIN_PLIST_PATH" ]
            return $?
            ;;
    esac
    return 1
}

detect_platform
detect_service_mode

SUDO_CMD=""
if [ "$DRY_RUN" -eq 0 ] && [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO_CMD="sudo"
    else
        fail "please run as root or install sudo"
    fi
fi

ASSET_NAME="ssh-tunnel-svc-${OS_NAME}-${ARCH_NAME}"
RELEASE_JSON=$(download_to_stdout "$RELEASE_API_URL") || fail "failed to query latest release metadata"
RELEASE_TAG=$(printf '%s' "$RELEASE_JSON" | tr -d '\r\n' | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
[ -n "$RELEASE_TAG" ] || fail "failed to resolve latest release tag"

if has_existing_installation; then
    VERSION_URL=$(admin_url_from_config "$CONFIG_PATH")
    log "Detected an existing installation."
    log "Use the management page to update instead of re-running the one-click installer:"
    log "$VERSION_URL"
    exit 0
fi

log "Installing SSH Tunnel ${RELEASE_TAG}"
log "Detected platform: ${OS_NAME}/${ARCH_NAME}"
log "Service mode: ${SERVICE_KIND}"

SERVER_IP=$(prompt_required "Enter SSH server IP or hostname")
while :; do
    SERVER_PORT=$(prompt_default "Enter SSH server port" "22")
    if is_numeric_port "$SERVER_PORT"; then
        break
    fi
    log "Please enter a valid port between 1 and 65535."
done

LOGIN_USER=$(prompt_default "Enter SSH login username" "root")

while :; do
    SSH_KEY_INPUT=$(prompt_default "Enter SSH private key path" "~/.ssh/id_rsa")
    SSH_KEY_PATH=$(expand_path "$SSH_KEY_INPUT")
    if [ -f "$SSH_KEY_PATH" ]; then
        break
    fi
    log "Private key not found: $SSH_KEY_PATH"
done

while :; do
    BIND_CHOICE=$(prompt_default "Bind services to localhost only? (y/n)" "y")
    case "$BIND_CHOICE" in
        y|Y|yes|YES)
            BIND_SCOPE="localhost"
            SOCKS_HOST="127.0.0.1"
            HTTP_HOST="127.0.0.1"
            ADMIN_HOST="127.0.0.1"
            break
            ;;
        n|N|no|NO)
            BIND_SCOPE="all"
            SOCKS_HOST="0.0.0.0"
            HTTP_HOST="0.0.0.0"
            ADMIN_HOST=""
            break
            ;;
    esac
    log "Please answer y or n."
done

SOCKS_PORT=$(choose_port "SOCKS5 proxy" "1081")
HTTP_PORT=$(choose_port "HTTP proxy" "1082")
ADMIN_PORT=$(choose_port "Admin UI" "1083")

SOCKS_ADDR="${SOCKS_HOST}:${SOCKS_PORT}"
HTTP_ADDR="${HTTP_HOST}:${HTTP_PORT}"
if [ "$BIND_SCOPE" = "localhost" ]; then
    ADMIN_ADDR="${ADMIN_HOST}:${ADMIN_PORT}"
else
    ADMIN_ADDR=":${ADMIN_PORT}"
fi

if [ "$OS_NAME" = "linux" ]; then
    STATE_DIR="/var/lib/ssh-tunnel"
    LOG_FILE="/var/log/ssh-tunnel.log"
else
    STATE_DIR="/usr/local/var/lib/ssh-tunnel"
    LOG_FILE="/usr/local/var/log/ssh-tunnel.log"
fi

DOMAIN_FILE="${STATE_DIR}/domain.txt"
ADMIN_URL="http://127.0.0.1:${ADMIN_PORT}/view/version"

TEMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t ssh-tunnel-install)
trap 'rm -rf "${TEMP_DIR}"' EXIT HUP INT TERM

BINARY_TMP="${TEMP_DIR}/${ASSET_NAME}"
CHECKSUM_TMP="${TEMP_DIR}/SHA256SUMS"
CONFIG_TMP="${TEMP_DIR}/config.properties"
SERVICE_TMP="${TEMP_DIR}/service.conf"

if [ "$DRY_RUN" -eq 1 ]; then
    log "Dry run only. No files will be written."
    log "Latest release: ${RELEASE_TAG}"
    log "Binary asset: ${ASSET_NAME}"
    log "Binary destination: ${INSTALL_BINARY_PATH}"
    log "Config destination: ${CONFIG_PATH}"
    case "$SERVICE_KIND" in
        systemd)
            log "Service destination: ${SYSTEMD_UNIT_PATH}"
            ;;
        sysv)
            log "Service destination: ${SYSV_SCRIPT_PATH}"
            ;;
        launchd)
            log "Service destination: ${DARWIN_PLIST_PATH}"
            ;;
    esac
    log "Generated config summary:"
    log "  server.ip=${SERVER_IP}"
    log "  server.ssh.port=${SERVER_PORT}"
    log "  login.username=${LOGIN_USER}"
    log "  ssh.private_key_path=${SSH_KEY_PATH}"
    log "  local.address=${SOCKS_ADDR}"
    log "  http.local.address=${HTTP_ADDR}"
    log "  admin.address=${ADMIN_ADDR}"
    log "  home.dir=${STATE_DIR}"
    log "  log.file.path=${LOG_FILE}"
    exit 0
fi

log "Downloading ${ASSET_NAME}..."
download_to_file "${RELEASE_DOWNLOAD_BASE}/${ASSET_NAME}" "$BINARY_TMP"
download_to_file "${RELEASE_DOWNLOAD_BASE}/SHA256SUMS" "$CHECKSUM_TMP"

EXPECTED_CHECKSUM=$(awk -v name="$ASSET_NAME" '
    $2 == name { print tolower($1); exit }
    substr($2, 2) == name { print tolower($1); exit }
' "$CHECKSUM_TMP")
[ -n "$EXPECTED_CHECKSUM" ] || fail "failed to resolve expected SHA256 for ${ASSET_NAME}"

if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL_CHECKSUM=$(sha256sum "$BINARY_TMP" | awk '{print tolower($1)}')
elif command -v shasum >/dev/null 2>&1; then
    ACTUAL_CHECKSUM=$(shasum -a 256 "$BINARY_TMP" | awk '{print tolower($1)}')
elif command -v openssl >/dev/null 2>&1; then
    ACTUAL_CHECKSUM=$(openssl dgst -sha256 "$BINARY_TMP" | awk '{print tolower($NF)}')
else
    fail "no SHA256 command found (sha256sum, shasum, or openssl)"
fi

[ "$EXPECTED_CHECKSUM" = "$ACTUAL_CHECKSUM" ] || fail "SHA256 verification failed"

cat > "$CONFIG_TMP" <<EOF
home.dir=${STATE_DIR}
server.ip=${SERVER_IP}
server.ssh.port=${SERVER_PORT}
ssh.private_key_path=${SSH_KEY_PATH}
login.username=${LOGIN_USER}
local.address=${SOCKS_ADDR}
http.local.address=${HTTP_ADDR}
http.enable=false
socks5.enable=true
http.over-ssh.enable=false
http.domain-filter.enable=false
http.domain-filter.file-path=${DOMAIN_FILE}
admin.enable=true
admin.address=${ADMIN_ADDR}
retry.interval.sec=3
ssh.dial.timeout.sec=5
ssh.dest.dial.timeout.sec=3
ssh.keepalive.interval.sec=2
ssh.keepalive.count.max=2
ssh.reconnect.max.retries=20
ssh.reconnect.max.interval.sec=5
log.file.path=${LOG_FILE}
auto-update.enabled=true
auto-update.owner=${REPO_OWNER}
auto-update.repo=${REPO_NAME}
auto-update.current-version=${RELEASE_TAG}
auto-update.check-interval=3600
EOF

run_root mkdir -p "$(dirname "$INSTALL_BINARY_PATH")" "$(dirname "$CONFIG_PATH")" "$STATE_DIR" "$(dirname "$LOG_FILE")"
run_root touch "$DOMAIN_FILE"
write_root_file "$INSTALL_BINARY_PATH" "$BINARY_TMP"
run_root chmod 0755 "$INSTALL_BINARY_PATH"
write_root_file "$CONFIG_PATH" "$CONFIG_TMP"

case "$SERVICE_KIND" in
    systemd)
        cat > "$SERVICE_TMP" <<EOF
[Unit]
Description=SSH Tunnel Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_BINARY_PATH} --config=${CONFIG_PATH}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
        write_root_file "$SYSTEMD_UNIT_PATH" "$SERVICE_TMP"
        run_root chmod 0644 "$SYSTEMD_UNIT_PATH"
        run_root systemctl daemon-reload
        run_root systemctl enable ssh-tunnel
        run_root systemctl restart ssh-tunnel
        ;;
    sysv)
        cat > "$SERVICE_TMP" <<'EOF'
#!/bin/sh
### BEGIN INIT INFO
# Provides:          ssh-tunnel
# Required-Start:    $remote_fs $network
# Required-Stop:     $remote_fs $network
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
### END INIT INFO

DAEMON="/usr/local/bin/ssh-tunnel"
CONFIG="/etc/ssh-tunnel/config.properties"
PIDFILE="/var/run/ssh-tunnel.pid"

start() {
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        echo "ssh-tunnel already running"
        return 0
    fi
    "$DAEMON" --config="$CONFIG" >/dev/null 2>&1 &
    echo $! > "$PIDFILE"
}

stop() {
    if [ -f "$PIDFILE" ]; then
        kill "$(cat "$PIDFILE")" 2>/dev/null || true
        rm -f "$PIDFILE"
    fi
}

case "$1" in
    start) start ;;
    stop) stop ;;
    restart) stop; start ;;
    status)
        if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
            echo "ssh-tunnel is running"
            exit 0
        fi
        echo "ssh-tunnel is stopped"
        exit 1
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
EOF
        write_root_file "$SYSV_SCRIPT_PATH" "$SERVICE_TMP"
        run_root chmod 0755 "$SYSV_SCRIPT_PATH"
        if command -v update-rc.d >/dev/null 2>&1; then
            run_root update-rc.d ssh-tunnel defaults
        elif command -v chkconfig >/dev/null 2>&1; then
            run_root chkconfig --add ssh-tunnel
            run_root chkconfig ssh-tunnel on
        fi
        run_root service ssh-tunnel restart
        ;;
    launchd)
        cat > "$SERVICE_TMP" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.idefav.ssh-tunnel</string>
    <key>ProgramArguments</key>
    <array>
        <string>${INSTALL_BINARY_PATH}</string>
        <string>--config=${CONFIG_PATH}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>/usr/local/bin</string>
    <key>StandardOutPath</key>
    <string>${LOG_FILE}</string>
    <key>StandardErrorPath</key>
    <string>${LOG_FILE}.error</string>
</dict>
</plist>
EOF
        write_root_file "$DARWIN_PLIST_PATH" "$SERVICE_TMP"
        run_root chmod 0644 "$DARWIN_PLIST_PATH"
        run_root launchctl unload "$DARWIN_PLIST_PATH" >/dev/null 2>&1 || true
        run_root launchctl load -w "$DARWIN_PLIST_PATH"
        ;;
esac

log "Installation completed successfully."
log "Admin UI: ${ADMIN_URL}"
log "Future upgrades should be done from the management page version screen."
