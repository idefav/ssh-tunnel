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

EXTRACTED_FUNCTIONS="$TMP_DIR/ssh_key_discovery.sh"

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
extract_function "append_line"
extract_function "contains_line"
extract_function "discover_ssh_key_defaults"

if [ ! -s "$EXTRACTED_FUNCTIONS" ]; then
    printf 'Failed to extract SSH key discovery helpers from %s\n' "$TARGET_SCRIPT" >&2
    exit 1
fi

run_discovery() {
    home_dir=$1
    HOME=$home_dir /bin/sh -c '. "$1"; discover_ssh_key_defaults; printf "%s\n" "$SSH_KEY_DEFAULT_INPUT"' sh "$EXTRACTED_FUNCTIONS"
}

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

PAIR_HOME="$TMP_DIR/pair-home"
mkdir -p "$PAIR_HOME/.ssh"
touch "$PAIR_HOME/.ssh/id_rsa"
touch "$PAIR_HOME/.ssh/id_rsa.pub"
touch "$PAIR_HOME/.ssh/id_ed25519"
touch "$PAIR_HOME/.ssh/id_ed25519.pub"

assert_eq "$(run_discovery "$PAIR_HOME")" \
    '~/.ssh/id_ed25519.pub' \
    'prefers the first detected key pair public key from the priority list'

PRIVATE_ONLY_HOME="$TMP_DIR/private-only-home"
mkdir -p "$PRIVATE_ONLY_HOME/.ssh"
touch "$PRIVATE_ONLY_HOME/.ssh/id_rsa"

assert_eq "$(run_discovery "$PRIVATE_ONLY_HOME")" \
    '~/.ssh/id_rsa' \
    'falls back to a private key when no matching public key exists'

CUSTOM_PAIR_HOME="$TMP_DIR/custom-pair-home"
mkdir -p "$CUSTOM_PAIR_HOME/.ssh"
touch "$CUSTOM_PAIR_HOME/.ssh/work_key"
touch "$CUSTOM_PAIR_HOME/.ssh/work_key.pub"

assert_eq "$(run_discovery "$CUSTOM_PAIR_HOME")" \
    '~/.ssh/work_key.pub' \
    'detects non-standard key pair names by scanning .ssh for public keys'

printf 'SSH key discovery regression tests passed.\n'