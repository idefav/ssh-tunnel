#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
TARGET_SCRIPT="$REPO_ROOT/docs/assets/install/install-unix.sh"

TMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT HUP TERM

EXTRACTED_FUNCTIONS="$TMP_DIR/ssh_key_generation.sh"

extract_function() {
    function_name=$1
    awk -v target="$function_name" '
        $0 ~ ("^" target "\\(\\) \\{") { capture = 1 }
        capture { print }
        capture && /^}/ { exit }
    ' "$TARGET_SCRIPT" >>"$EXTRACTED_FUNCTIONS"
    printf '\n' >>"$EXTRACTED_FUNCTIONS"
}

extract_function "expand_path"
extract_function "display_path"
extract_function "suggest_ssh_key_comment"
extract_function "generate_ssh_key_pair_auto"

if [ ! -s "$EXTRACTED_FUNCTIONS" ]; then
    printf 'Failed to extract SSH key generation helpers from %s\n' "$TARGET_SCRIPT" >&2
    exit 1
fi

assert_eq() {
    actual=$1
    expected=$2
    message=$3
    if [ "$actual" != "$expected" ]; then
        printf 'FAIL: %s\n' "$message" >&2
        printf 'expected: %s\n' "$expected" >&2
        printf 'actual:   %s\n' "$actual" >&2
        exit 1
    fi
}

HOME_DIR="$TMP_DIR/home"
FAKE_BIN="$TMP_DIR/fake-bin"
mkdir -p "$HOME_DIR" "$FAKE_BIN"

cat > "$FAKE_BIN/ssh-keygen" <<'EOF'
#!/bin/sh
key_path=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -f)
            shift
            key_path=$1
            ;;
    esac
    shift
done

touch "$key_path"
touch "${key_path}.pub"
EOF
chmod 0755 "$FAKE_BIN/ssh-keygen"

result=$(HOME="$HOME_DIR" PATH="$FAKE_BIN:$PATH" /bin/sh -c '
    DRY_RUN=0
    prompt_default() {
        case "$1" in
            "Enter new SSH private key path")
                printf "~/.ssh/id_ed25519\n"
                ;;
            "Enter SSH key comment")
                printf "generated@test\n"
                ;;
        esac
    }
    log() {
        :
    }
    fail() {
        printf "%s\n" "$*" >&2
        exit 1
    }
    . "$1"
    generate_ssh_key_pair_auto >/dev/null
    printf "%s|%s\n" "$SSH_KEY_DEFAULT_INPUT" "$HOME/.ssh/id_ed25519"
' sh "$EXTRACTED_FUNCTIONS")

assert_eq "$result" \
    "~/.ssh/id_ed25519.pub|$HOME_DIR/.ssh/id_ed25519" \
    'sets the generated public key as the next default selection'

if [ ! -f "$HOME_DIR/.ssh/id_ed25519" ] || [ ! -f "$HOME_DIR/.ssh/id_ed25519.pub" ]; then
    printf 'FAIL: generated SSH key files were not created by the helper\n' >&2
    exit 1
fi

printf 'SSH key generation regression tests passed.\n'