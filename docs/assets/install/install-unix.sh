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

display_path() {
    case "$1" in
        "$HOME")
            printf '~\n'
            ;;
        "$HOME"/*)
            printf '~/%s\n' "${1#$HOME/}"
            ;;
        *)
            printf '%s\n' "$1"
            ;;
    esac
}

append_line() {
    existing_lines=$1
    new_line=$2
    if [ -n "$existing_lines" ]; then
        printf '%s\n%s\n' "$existing_lines" "$new_line"
        return
    fi
    printf '%s\n' "$new_line"
}

contains_line() {
    existing_lines=$1
    target_line=$2
    if [ -z "$existing_lines" ]; then
        return 1
    fi
    printf '%s\n' "$existing_lines" | grep -Fx -- "$target_line" >/dev/null 2>&1
}

discover_ssh_key_defaults() {
    ssh_dir="${HOME}/.ssh"
    key_name_candidates="id_ed25519 id_ed25519_sk id_ecdsa id_ecdsa_sk id_rsa id_dsa identity"

    SSH_KEY_DEFAULT_INPUT=""
    SSH_KEY_DISCOVERY_LOG=""
    ssh_seen_inputs=""

    if [ ! -d "$ssh_dir" ]; then
        return 1
    fi

    for key_name in $key_name_candidates; do
        ssh_public_path="${ssh_dir}/${key_name}.pub"
        ssh_private_path="${ssh_dir}/${key_name}"
        if [ -f "$ssh_public_path" ] && [ -f "$ssh_private_path" ] && ! contains_line "$ssh_seen_inputs" "$ssh_public_path"; then
            ssh_seen_inputs=$(append_line "$ssh_seen_inputs" "$ssh_public_path")
            SSH_KEY_DISCOVERY_LOG=$(append_line "$SSH_KEY_DISCOVERY_LOG" "  - $(display_path "$ssh_public_path") -> $(display_path "$ssh_private_path")")
            if [ -z "$SSH_KEY_DEFAULT_INPUT" ]; then
                SSH_KEY_DEFAULT_INPUT=$(display_path "$ssh_public_path")
            fi
        fi
    done

    for ssh_public_path in "$ssh_dir"/*.pub; do
        [ -e "$ssh_public_path" ] || break
        ssh_private_path=${ssh_public_path%".pub"}
        if [ -f "$ssh_private_path" ] && ! contains_line "$ssh_seen_inputs" "$ssh_public_path"; then
            ssh_seen_inputs=$(append_line "$ssh_seen_inputs" "$ssh_public_path")
            SSH_KEY_DISCOVERY_LOG=$(append_line "$SSH_KEY_DISCOVERY_LOG" "  - $(display_path "$ssh_public_path") -> $(display_path "$ssh_private_path")")
            if [ -z "$SSH_KEY_DEFAULT_INPUT" ]; then
                SSH_KEY_DEFAULT_INPUT=$(display_path "$ssh_public_path")
            fi
        fi
    done

    if [ -n "$SSH_KEY_DEFAULT_INPUT" ]; then
        return 0
    fi

    for key_name in $key_name_candidates; do
        ssh_private_path="${ssh_dir}/${key_name}"
        if [ -f "$ssh_private_path" ] && ! contains_line "$ssh_seen_inputs" "$ssh_private_path"; then
            ssh_seen_inputs=$(append_line "$ssh_seen_inputs" "$ssh_private_path")
            SSH_KEY_DISCOVERY_LOG=$(append_line "$SSH_KEY_DISCOVERY_LOG" "  - $(display_path "$ssh_private_path")")
            if [ -z "$SSH_KEY_DEFAULT_INPUT" ]; then
                SSH_KEY_DEFAULT_INPUT=$(display_path "$ssh_private_path")
            fi
        fi
    done

    for ssh_private_path in "$ssh_dir"/*; do
        [ -e "$ssh_private_path" ] || break
        [ -f "$ssh_private_path" ] || continue
        case $(basename "$ssh_private_path") in
            *.pub|authorized_keys|authorized_keys2|known_hosts|known_hosts.old|config)
                continue
                ;;
        esac
        if ! contains_line "$ssh_seen_inputs" "$ssh_private_path"; then
            ssh_seen_inputs=$(append_line "$ssh_seen_inputs" "$ssh_private_path")
            SSH_KEY_DISCOVERY_LOG=$(append_line "$SSH_KEY_DISCOVERY_LOG" "  - $(display_path "$ssh_private_path")")
            if [ -z "$SSH_KEY_DEFAULT_INPUT" ]; then
                SSH_KEY_DEFAULT_INPUT=$(display_path "$ssh_private_path")
            fi
        fi
    done

    [ -n "$SSH_KEY_DEFAULT_INPUT" ]
}

refresh_ssh_key_defaults() {
    SSH_KEY_DEFAULT_INPUT="~/.ssh/id_rsa"
    SSH_KEY_DISCOVERY_LOG=""
    if discover_ssh_key_defaults; then
        log "Detected local SSH keys:"
        printf '%s\n' "$SSH_KEY_DISCOVERY_LOG"
        return 0
    fi
    return 1
}

suggest_ssh_key_comment() {
    host_name=$(hostname 2>/dev/null || uname -n 2>/dev/null || printf 'local')
    printf 'ssh-tunnel@%s\n' "$host_name"
}

generate_ssh_key_pair_auto() {
    if ! command -v ssh-keygen >/dev/null 2>&1; then
        fail "ssh-keygen is required to generate a new SSH key pair"
    fi

    while :; do
        key_path_input=$(prompt_default "Enter new SSH private key path" "~/.ssh/id_ed25519")
        generated_private_key_path=$(expand_path "$key_path_input")
        generated_public_key_path="${generated_private_key_path}.pub"
        if [ -e "$generated_private_key_path" ] || [ -e "$generated_public_key_path" ]; then
            log "Target key path already exists: $(display_path "$generated_private_key_path")"
            continue
        fi
        break
    done

    generated_key_comment=$(prompt_default "Enter SSH key comment" "$(suggest_ssh_key_comment)")

    if [ "$DRY_RUN" -eq 1 ]; then
        log "Dry run: would generate a new SSH key pair at $(display_path "$generated_private_key_path")"
        ALLOW_MISSING_SSH_KEY=1
        SSH_KEY_DEFAULT_INPUT=$(display_path "$generated_public_key_path")
        return 0
    fi

    mkdir -p "$(dirname "$generated_private_key_path")"
    ssh-keygen -t ed25519 -f "$generated_private_key_path" -N "" -C "$generated_key_comment"
    SSH_KEY_DEFAULT_INPUT=$(display_path "$generated_public_key_path")
    log "Generated SSH key pair: $(display_path "$generated_private_key_path")"
}

generate_ssh_key_pair_interactive() {
    if ! command -v ssh-keygen >/dev/null 2>&1; then
        fail "ssh-keygen is required to generate a new SSH key pair"
    fi

    if [ "$DRY_RUN" -eq 1 ]; then
        log "Dry run: would start interactive ssh-keygen and then continue with the generated key."
        ALLOW_MISSING_SSH_KEY=1
        SSH_KEY_DEFAULT_INPUT="~/.ssh/id_ed25519.pub"
        return 0
    fi

    log "Starting interactive ssh-keygen. Generate a key pair, then return to continue installation."
    ssh-keygen
}

ensure_local_ssh_key_available() {
    if refresh_ssh_key_defaults; then
        return 0
    fi

    log "No local SSH key pairs were found in $(display_path "$HOME/.ssh")."
    while :; do
        generation_mode=$(prompt_default "Generate a new SSH key pair now? (a=auto, i=interactive, m=manual)" "a")
        case "$generation_mode" in
            a|A|auto|AUTO)
                generate_ssh_key_pair_auto
                ;;
            i|I|interactive|INTERACTIVE)
                generate_ssh_key_pair_interactive
                ;;
            m|M|manual|MANUAL)
                log "Manual mode selected. Enter an existing SSH key path to continue."
                ALLOW_MISSING_SSH_KEY=0
                SSH_KEY_DEFAULT_INPUT="~/.ssh/id_ed25519"
                return 0
                ;;
            *)
                log "Please answer a, i, or m."
                continue
                ;;
        esac

        if [ "$DRY_RUN" -eq 1 ]; then
            return 0
        fi
        if refresh_ssh_key_defaults; then
            return 0
        fi
        log "No usable SSH key pair was found after generation. Please try again."
    done
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

resolve_ssh_public_key() {
    if [ -n "${SSH_PUBLIC_KEY_PATH:-}" ] && [ -f "$SSH_PUBLIC_KEY_PATH" ]; then
        return 0
    fi

    derived_public_key_path="${SSH_KEY_PATH}.pub"
    if [ -f "$derived_public_key_path" ]; then
        SSH_PUBLIC_KEY_PATH="$derived_public_key_path"
        return 0
    fi

    if ! command -v ssh-keygen >/dev/null 2>&1; then
        return 1
    fi

    GENERATED_PUBLIC_KEY_PATH="${TEMP_DIR}/generated-authorized-key.pub"
    if ssh-keygen -y -f "$SSH_KEY_PATH" >"$GENERATED_PUBLIC_KEY_PATH"; then
        SSH_PUBLIC_KEY_PATH="$GENERATED_PUBLIC_KEY_PATH"
        return 0
    fi

    rm -f "$GENERATED_PUBLIC_KEY_PATH"
    GENERATED_PUBLIC_KEY_PATH=""
    return 1
}

check_passwordless_ssh() {
    if ! command -v ssh >/dev/null 2>&1; then
        fail "ssh client is required to verify passwordless login"
    fi

    ssh -p "$SERVER_PORT" \
        -o BatchMode=yes \
        -o ConnectTimeout=10 \
        -o StrictHostKeyChecking=accept-new \
        -o LogLevel=ERROR \
        -i "$SSH_KEY_PATH" \
        "${LOGIN_USER}@${SERVER_IP}" exit >/dev/null 2>&1
}

configure_passwordless_ssh() {
    resolve_ssh_public_key || fail "failed to locate or derive a public key for ${SSH_KEY_PATH}; please provide a matching .pub file or install ssh-keygen"

    if [ ! -s "$SSH_PUBLIC_KEY_PATH" ]; then
        fail "public key is empty: $SSH_PUBLIC_KEY_PATH"
    fi

    log "Passwordless SSH is not configured. Attempting automatic setup..."
    log "Public key source: $(display_path "$SSH_PUBLIC_KEY_PATH")"

    if command -v ssh-copy-id >/dev/null 2>&1; then
        ssh-copy-id \
            -i "$SSH_PUBLIC_KEY_PATH" \
            -p "$SERVER_PORT" \
            -o StrictHostKeyChecking=accept-new \
            "${LOGIN_USER}@${SERVER_IP}"
        return
    fi

    log "ssh-copy-id not found. Falling back to direct authorized_keys update."
    cat "$SSH_PUBLIC_KEY_PATH" | ssh \
        -p "$SERVER_PORT" \
        -o ConnectTimeout=10 \
        -o StrictHostKeyChecking=accept-new \
        -o LogLevel=ERROR \
        "${LOGIN_USER}@${SERVER_IP}" \
        'umask 077 && mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys && chmod 700 ~/.ssh && chmod 600 ~/.ssh/authorized_keys && pub=$(cat) && (grep -qxF "$pub" ~/.ssh/authorized_keys || printf "%s\n" "$pub" >> ~/.ssh/authorized_keys)'
}

ensure_passwordless_ssh() {
    if [ "$DRY_RUN" -eq 1 ]; then
        log "Dry run: would verify passwordless SSH for ${LOGIN_USER}@${SERVER_IP} with $(display_path "$SSH_KEY_PATH") and auto-configure it if missing."
        return 0
    fi

    log "Checking passwordless SSH login..."
    if check_passwordless_ssh; then
        log "Passwordless SSH is already configured."
        return 0
    fi

    configure_passwordless_ssh

    log "Re-checking passwordless SSH login..."
    if check_passwordless_ssh; then
        log "Passwordless SSH configured successfully."
        return 0
    fi

    fail "automatic passwordless SSH setup failed; please verify the remote account password, SSH password authentication, and ~/.ssh/authorized_keys permissions"
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

TEMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t ssh-tunnel-install)
GENERATED_PUBLIC_KEY_PATH=""
ALLOW_MISSING_SSH_KEY=0
trap 'rm -rf "${TEMP_DIR}"' EXIT HUP INT TERM

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

SSH_PUBLIC_KEY_PATH=""
SSH_KEY_DEFAULT_INPUT="~/.ssh/id_rsa"
ensure_local_ssh_key_available

while :; do
    SSH_KEY_INPUT=$(prompt_default "Enter SSH key path (public or private)" "$SSH_KEY_DEFAULT_INPUT")
    SSH_KEY_RESOLVED=$(expand_path "$SSH_KEY_INPUT")
    case "$SSH_KEY_RESOLVED" in
        *.pub)
            SSH_PUBLIC_KEY_PATH="$SSH_KEY_RESOLVED"
            SSH_KEY_PATH=${SSH_KEY_RESOLVED%".pub"}
            if [ "$ALLOW_MISSING_SSH_KEY" -eq 1 ] && [ "$DRY_RUN" -eq 1 ]; then
                break
            fi
            if [ ! -f "$SSH_PUBLIC_KEY_PATH" ]; then
                log "Public key not found: $SSH_PUBLIC_KEY_PATH"
                continue
            fi
            if [ ! -f "$SSH_KEY_PATH" ]; then
                log "Private key not found for public key: $SSH_KEY_PATH"
                continue
            fi
            break
            ;;
        *)
            SSH_KEY_PATH="$SSH_KEY_RESOLVED"
            if [ "$ALLOW_MISSING_SSH_KEY" -eq 1 ] && [ "$DRY_RUN" -eq 1 ]; then
                SSH_PUBLIC_KEY_PATH="${SSH_KEY_PATH}.pub"
                break
            fi
            if [ -f "$SSH_KEY_PATH" ]; then
                if [ -f "${SSH_KEY_PATH}.pub" ]; then
                    SSH_PUBLIC_KEY_PATH="${SSH_KEY_PATH}.pub"
                else
                    SSH_PUBLIC_KEY_PATH=""
                fi
                break
            fi
            log "SSH key not found: $SSH_KEY_PATH"
            ;;
    esac
done

if [ -n "$SSH_PUBLIC_KEY_PATH" ]; then
    log "Using SSH public key: $(display_path "$SSH_PUBLIC_KEY_PATH")"
    log "Using SSH private key: $(display_path "$SSH_KEY_PATH")"
else
    log "Using SSH private key: $(display_path "$SSH_KEY_PATH")"
fi

ensure_passwordless_ssh

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
    if [ -n "$SSH_PUBLIC_KEY_PATH" ]; then
        log "  ssh.public_key_path.derived=${SSH_PUBLIC_KEY_PATH}"
    fi
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
