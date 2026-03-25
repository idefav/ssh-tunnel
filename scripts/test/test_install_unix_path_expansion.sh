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

EXTRACTED_FUNCTION="$TMP_DIR/expand_path.sh"

awk '
    /^expand_path\(\) \{/ { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
' "$TARGET_SCRIPT" >"$EXTRACTED_FUNCTION"

if [ ! -s "$EXTRACTED_FUNCTION" ]; then
    printf 'Failed to extract expand_path() from %s\n' "$TARGET_SCRIPT" >&2
    exit 1
fi

run_expand() {
    home_dir=$1
    input_path=$2
    HOME=$home_dir /bin/sh -c '. "$1"; expand_path "$2"' sh "$EXTRACTED_FUNCTION" "$input_path"
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

assert_eq "$(run_expand "/tmp/fake-home" "~")" \
    "/tmp/fake-home" \
    'expands "~" to HOME'

assert_eq "$(run_expand "/tmp/fake-home" "~/.ssh/id_rsa")" \
    "/tmp/fake-home/.ssh/id_rsa" \
    'expands "~/.ssh/id_rsa" without leaving a literal "~/" segment'

assert_eq "$(run_expand "/tmp/fake home" "~/.ssh/id_rsa")" \
    "/tmp/fake home/.ssh/id_rsa" \
    'preserves spaces in HOME when expanding "~/" paths'

assert_eq "$(run_expand "/tmp/fake-home" "/opt/keys/id_rsa")" \
    "/opt/keys/id_rsa" \
    'keeps absolute paths unchanged'

assert_eq "$(run_expand "/tmp/fake-home" "keys/id_rsa")" \
    "keys/id_rsa" \
    'keeps relative paths unchanged'

printf 'expand_path() regression tests passed.\n'
