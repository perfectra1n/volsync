#!/bin/bash
# Run without Kopia or a repository: bash mover-kopia/test_cache_corruption.sh
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# Load actual function definitions, not the entrypoint's startup/backup actions.
for function_name in is_cache_corruption_error clear_corrupted_cache; do
    source <(awk -v name="$function_name" '
        $0 ~ "^function " name " \\{" { copying = 1 }
        copying { print }
        copying && /^}$/ { exit }
    ' "$SCRIPT_DIR/entry.sh")
    declare -F "$function_name" >/dev/null
done
log_info() { printf '%s\n' "$*"; }
log_warn() { printf '%s\n' "$*" >&2; }
log_error() { printf '%s\n' "$*" >&2; }

failed=0
run_case() {
    local expected="$1" name="$2" output="$3" actual=1
    if is_cache_corruption_error "$output" >/dev/null 2>&1; then actual=0; fi
    if [[ "$actual" == "$expected" ]]; then
        printf 'PASS: %s\n' "$name"
    else
        printf 'FAIL: %s (expected %s, got %s)\n' "$name" "$expected" "$actual" >&2
        failed=$((failed + 1))
    fi
}

run_case 1 'incident: allocation failure opening an index' \
    'error loading indexes: unable to open pack index "xn0_test": mmap error: cannot allocate memory'
run_case 1 'runtime allocation failure' 'fatal error: runtime: cannot allocate memory'
run_case 1 'out of memory' 'mmap error: out of memory'
run_case 1 'ENOMEM errno' 'mmap error: errno 12'
run_case 1 'explicit ENOMEM' 'mmap error: ENOMEM'
run_case 1 'disk full' 'error loading indexes: mmap error: no space left on device (errno 28)'
run_case 1 'invalid argument' 'mmap error: invalid argument (errno 22)'
run_case 1 'permission denied' 'unable to open pack index: permission denied'
run_case 1 'generic index failure' 'error loading indexes: network timeout'
run_case 1 'generic mmap failure' 'mmap error: unknown cause'
run_case 1 'authentication failure' 'invalid credentials'
run_case 1 'empty output' ''
run_case 0 'corrupt index' 'index is corrupt'
run_case 0 'corrupt cache' 'cache is corrupt'
run_case 0 'bad index blob' 'bad index blob'
run_case 0 'case-insensitive corruption' 'BAD INDEX BLOB'
run_case 1 'allocation failure takes precedence' $'bad index blob\nmmap error: cannot allocate memory'
run_case 1 'errno takes precedence' $'cache is corrupt\nmmap error: errno 12'
run_case 1 'ENOMEM takes precedence' $'index is corrupt\nENOMEM'
run_case 0 'errno prefix is not ENOMEM' 'bad index blob: errno 123'

diagnostic=$(is_cache_corruption_error 'mmap error: cannot allocate memory' 2>&1 || true)
if [[ "$diagnostic" == *vm.max_map_count* && "$diagnostic" == *'cache recovery skipped'* ]]; then
    printf 'PASS: allocation diagnostic points to resources, not corruption\n'
else
    printf 'FAIL: allocation diagnostic is missing\n' >&2
    failed=$((failed + 1))
fi

# Exercise the real cleanup guard in a disposable cache, never real backup data.
KOPIA_CACHE_DIR=$(mktemp -d)
trap 'rm -rf -- "$KOPIA_CACHE_DIR"' EXIT
mkdir -p "$KOPIA_CACHE_DIR/own-writes" "$KOPIA_CACHE_DIR/index-blobs"
printf 'pending\n' > "$KOPIA_CACHE_DIR/own-writes/pending"
printf 'cached\n' > "$KOPIA_CACHE_DIR/index-blobs/cached"
if clear_corrupted_cache >/dev/null 2>&1; then
    printf 'FAIL: cleanup accepted pending own-writes\n' >&2
    failed=$((failed + 1))
elif [[ -f "$KOPIA_CACHE_DIR/own-writes/pending" && -f "$KOPIA_CACHE_DIR/index-blobs/cached" ]]; then
    printf 'PASS: pending own-writes blocks cleanup and preserves cache\n'
else
    printf 'FAIL: pending own-writes or cache was removed\n' >&2
    failed=$((failed + 1))
fi

# The classifier must not route resource failures into destructive recovery.
if is_cache_corruption_error 'mmap error: cannot allocate memory' >/dev/null 2>&1; then
    printf 'FAIL: allocation failure would request cache cleanup\n' >&2
    failed=$((failed + 1))
else
    printf 'PASS: allocation failure bypasses cache cleanup\n'
fi

printf 'Failures: %s\n' "$failed"
[[ "$failed" == 0 ]]
