#!/bin/bash
# Smoke test for tools/godebug-diff/godebug-diff.sh (review 2026-11-22-01 A-P1-5).
#
# Verifies the tool's infrastructure preflight: the computed classpath
# entries and the corpus dir (+ probes) must exist before any JVM launch,
# and a missing piece must fail LOUD (exit 3) instead of launching java.
#
# Usage: smoke_test.sh — run from anywhere; the positive leg needs a built
# tree (make build at the repo root) and fails with a build-first message
# on an unbuilt tree (never a silent skip).
# GRAALGO_ROOT overrides the repo root under test (default: this repo).
set -u

ROOT="${GRAALGO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
SCRIPT="$ROOT/tools/godebug-diff/godebug-diff.sh"
mkdir -p "$ROOT/.tmp"
TMP="$(mktemp -d "$ROOT/.tmp/smoke-godebug.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

PASS=0; FAIL=0
ok()   { PASS=$((PASS + 1)); echo "PASS $1"; }
bad()  { FAIL=$((FAIL + 1)); echo "FAIL $1"; }

# 1. Positive: preflight passes on a built tree, no JVM launched.
STUB="$TMP/stub-graalvm"; mkdir -p "$STUB/bin"
printf '#!/bin/bash\necho launched >> "%s/marker"\n' "$TMP" > "$STUB/bin/java"
chmod +x "$STUB/bin/java"
out=$(GRAALVM_HOME="$STUB" GRAALGO_ROOT="$ROOT" bash "$SCRIPT" --check-only 2>&1); rc=$?
if [ $rc -eq 0 ] && [ ! -e "$TMP/marker" ]; then ok "positive: --check-only exits 0 without launching the JVM"; else bad "positive: --check-only rc=$rc marker=$([ -e "$TMP/marker" ] && echo yes || echo no) out=$out"; fi

# 2. Negative: java missing -> exit 3 naming java, JVM never launched.
out=$(GRAALVM_HOME="/nonexistent-graalvm-home" GRAALGO_ROOT="$ROOT" bash "$SCRIPT" --check-only 2>&1); rc=$?
if [ $rc -eq 3 ] && echo "$out" | grep -q "java not found"; then ok "negative: missing java exits 3 naming java"; else bad "negative: missing java rc=$rc out=$out"; fi

# 3. Negative: empty root -> exit 3 naming a missing classpath entry.
EMPTY="$TMP/empty-root"; mkdir -p "$EMPTY"
out=$(GRAALVM_HOME="$STUB" GRAALGO_ROOT="$EMPTY" bash "$SCRIPT" --check-only 2>&1); rc=$?
if [ $rc -eq 3 ] && echo "$out" | grep -q "classpath entry missing"; then ok "negative: empty root exits 3 naming a missing classpath entry"; else bad "negative: empty root rc=$rc out=$out"; fi
if [ -e "$TMP/marker" ]; then bad "negative: JVM launched despite failed preflight"; else ok "negative: JVM never launched on failed preflight"; fi

echo "---"
echo "godebug-diff smoke: $PASS pass, $FAIL fail"
exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
