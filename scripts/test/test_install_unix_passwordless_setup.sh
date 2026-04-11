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

EXTRACTED_FUNCTIONS="$TMP_DIR/passwordless_setup.sh"

extract_function() {
    function_name=$1
    awk -v target="$function_name" '
        $0 ~ ("^" target "\\(\\) \\{") { capture = 1 }
        capture { print }
        capture && /^}/ { exit }
    ' "$TARGET_SCRIPT" >>"$EXTRACTED_FUNCTIONS"
    printf '\n' >>"$EXTRACTED_FUNCTIONS"
}

extract_function "resolve_ssh_public_key"

if [ ! -s "$EXTRACTED_FUNCTIONS" ]; then
    printf 'Failed to extract passwordless SSH helpers from %s\n' "$TARGET_SCRIPT" >&2
    exit 1
fi

run_resolve() {
    test_home=$1
    private_key=$2
    public_key=$3
    temp_dir=$4
    path_override=$5
    HOME=$test_home TEMP_DIR=$temp_dir PATH=$path_override /bin/sh -c '
        SSH_KEY_PATH=$1
        SSH_PUBLIC_KEY_PATH=$2
        GENERATED_PUBLIC_KEY_PATH=""
        . "$3"
        resolve_ssh_public_key >/dev/null
        printf "%s|%s\n" "$SSH_PUBLIC_KEY_PATH" "$GENERATED_PUBLIC_KEY_PATH"
    ' sh "$private_key" "$public_key" "$EXTRACTED_FUNCTIONS"
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

EXISTING_HOME="$TMP_DIR/existing-home"
mkdir -p "$EXISTING_HOME/.ssh" "$TMP_DIR/existing-temp"
touch "$EXISTING_HOME/.ssh/id_rsa"
touch "$EXISTING_HOME/.ssh/id_rsa.pub"

assert_eq "$(run_resolve "$EXISTING_HOME" "$EXISTING_HOME/.ssh/id_rsa" "$EXISTING_HOME/.ssh/id_rsa.pub" "$TMP_DIR/existing-temp" "$PATH")" \
    "$EXISTING_HOME/.ssh/id_rsa.pub|" \
    'keeps an existing public key path unchanged'

GENERATED_HOME="$TMP_DIR/generated-home"
FAKE_BIN="$TMP_DIR/fake-bin"
mkdir -p "$GENERATED_HOME/.ssh" "$TMP_DIR/generated-temp" "$FAKE_BIN"
touch "$GENERATED_HOME/.ssh/id_rsa"
cat > "$FAKE_BIN/ssh-keygen" <<'EOF'
#!/bin/sh
printf 'ssh-rsa AAAATEST generated@example\n'
EOF
chmod 0755 "$FAKE_BIN/ssh-keygen"

generated_result=$(run_resolve "$GENERATED_HOME" "$GENERATED_HOME/.ssh/id_rsa" "" "$TMP_DIR/generated-temp" "$FAKE_BIN:$PATH")
generated_public_path=${generated_result%%|*}
generated_temp_path=${generated_result#*|}

assert_eq "$generated_public_path" \
    "$TMP_DIR/generated-temp/generated-authorized-key.pub" \
    'derives a temporary public key path when only a private key exists'

assert_eq "$generated_temp_path" \
    "$TMP_DIR/generated-temp/generated-authorized-key.pub" \
    'tracks the generated public key file for cleanup'

assert_eq "$(cat "$generated_public_path")" \
    'ssh-rsa AAAATEST generated@example' \
    'writes the generated public key content to the temporary file'

printf 'Passwordless SSH helper regression tests passed.\n'