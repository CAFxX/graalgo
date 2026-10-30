#!/usr/bin/env bash
# Regression tests for the fuzz-native gate (review21-fuzzgates).
#
# Runs the REAL test/differential/fuzz/run_fuzz_native.sh with a stub
# `go` (via DH_GO) so no corpus is grown and no backend is needed:
#  1. Empty replay corpus -> the script must exit 1 with the explicit
#     "replay produced 0 programs" verdict, never FUZZ-NATIVE GREEN.
#  2. One planted seed-corpus entry -> replay materializes 1 program,
#     the stub diffharness exits 0, and the script prints GREEN with
#     the program count.
#
# The planted seed file is created inside the fuzz testdata tree and
# removed on exit (trap); the script itself is never modified by this
# test. Leaves nothing behind (mktemp + trap).
#
# Usage: bash test/differential/fuzz/gate_regression_test.sh
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
TMP="$(mktemp -d)"
SEED_DIR="$HERE/testdata/fuzz/FuzzStringsToUpper"
SEED_FILE="$SEED_DIR/seed-regression-test"
cleanup() {
  rm -f "$SEED_FILE"
  rmdir "$SEED_DIR" "$HERE/testdata/fuzz" "$HERE/testdata" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

# Stub Go toolchain: `go test` is a no-op (no corpus grown), `go build`
# writes a stub diffharness binary that exits 0.
mkdir -p "$TMP/fakego/bin"
cat > "$TMP/fakego/bin/go" <<'EOF'
#!/bin/bash
if [ "${1:-}" = "test" ]; then exit 0; fi
if [ "${1:-}" = "build" ]; then
  out=""
  while [ $# -gt 0 ]; do
    if [ "$1" = "-o" ]; then out="$2"; shift 2; else shift; fi
  done
  printf '#!/bin/sh\nexit 0\n' > "$out"
  chmod +x "$out"
  exit 0
fi
echo "stub go: unexpected subcommand $*" >&2
exit 2
EOF
chmod +x "$TMP/fakego/bin/go"

export DH_GO="$TMP/fakego"
export TMPDIR="$TMP/tmp"
mkdir -p "$TMPDIR"

fail() { echo "FAIL: $*" >&2; exit 1; }

echo "== [1] empty replay corpus must fail =="
rc=0
"$HERE/run_fuzz_native.sh" --fuzztime 1 --out "$TMP/out-empty" > "$TMP/empty.log" 2>&1 || rc=$?
[ "$rc" -ne 0 ] || fail "empty replay exited 0"
grep -q "replay produced 0 programs" "$TMP/empty.log" || fail "empty replay lacked the explicit verdict (see $TMP/empty.log)"
grep -q "FUZZ-NATIVE GREEN" "$TMP/empty.log" && fail "empty replay printed GREEN"
echo "  ok: empty replay exits $rc with explicit verdict"

echo "== [2] one planted program must pass the gate =="
mkdir -p "$SEED_DIR"
printf 'go test fuzz v1\nstring("hello")\n' > "$SEED_FILE"
rc=0
"$HERE/run_fuzz_native.sh" --fuzztime 1 --out "$TMP/out-one" > "$TMP/one.log" 2>&1 || rc=$?
[ "$rc" -eq 0 ] || { echo "--- one-program log ---"; cat "$TMP/one.log"; fail "one-program replay exited $rc"; }
grep -q "FUZZ-NATIVE GREEN: 1 programs" "$TMP/one.log" || fail "one-program run lacked GREEN-with-count verdict"
echo "  ok: 1-program replay prints GREEN with count"

echo "ALL FUZZ-NATIVE GATE REGRESSION TESTS PASSED"
