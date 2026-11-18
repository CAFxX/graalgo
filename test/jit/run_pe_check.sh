#!/usr/bin/env bash
# test/jit/run_pe_check.sh — partial-evaluation gate over the benchmark set
# (truffle-design.md §10.2 TR-0, `make pe-check`).
#
# Runs every benchmark under test/bench/ (or the names given) under graalgo
# with the Truffle compiler in its strictest failure mode:
#   engine.CompilationFailureAction=ExitVM  any failed compilation (a
#                                           PermanentBailoutException such
#                                           as "Too deep inlining" because
#                                           PE followed a runtime helper
#                                           into JDK code, a frame escaping
#                                           PE, ...) terminates the VM
#   engine.BackgroundCompilation=false      every compilation finishes
#                                           before the program continues,
#                                           so the hot paths of each
#                                           benchmark are compiled and
#                                           checked independent of load
#   engine.TraceCompilation=true            the evidence (opt done / opt
#                                           failed lines)
# and fails when a program exits non-zero, its stdout differs from gc's,
# the trace shows any failed compilation, or nothing was compiled at all
# (a gate that compiled nothing proves nothing). Prints one line per
# benchmark with its compilation count and a total bailout count (the
# gate's number: it must be 0).
#
# test/jit/run_pe_guard.sh (`make test-jit`, per-push) covers breadth on one
# program (CompileImmediately over the stdlib it drags in); this gate covers
# the steady-state hot paths of the whole benchmark set. Performance
# warnings are not fatal yet: the v1 nodes still have many (truffle-design
# §1.2); the new node package (TR-3) makes them fatal.
#
# Requires `make build`. Env: GO, GRAALGO_RUN, PECHECK_JAVA_OPTS (default
# -Xmx2g), PECHECK_TIMEOUT (seconds per program, default 600).
set -euo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
GO="${GO:-$HOME/tools/go/bin/go}"
RUN="${GRAALGO_RUN:-$ROOT/internal/backend/graalgo-sync-run}"
BENCH="$ROOT/test/bench"
TIMEOUT="${PECHECK_TIMEOUT:-600}"

if [ $# -eq 0 ]; then
  set -- $(cd "$BENCH" && for d in */; do [ -f "$d/main.go" ] && echo "${d%/}"; done)
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/graalgo-pecheck.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

fail=0
bailouts=0
for name in "$@"; do
  src="$BENCH/$name/main.go"
  [ -f "$src" ] || { echo "pe-check: no such benchmark: $name" >&2; exit 2; }
  "$GO" build -o "$WORK/$name.gc" "$src"
  "$WORK/$name.gc" >"$WORK/$name.want" 2>/dev/null
  status=0
  JAVA_TOOL_OPTIONS="${PECHECK_JAVA_OPTS:--Xmx2g} \
 -Dpolyglot.engine.TraceCompilation=true \
 -Dpolyglot.engine.CompilationFailureAction=ExitVM \
 -Dpolyglot.engine.BackgroundCompilation=false" \
    nice -n 19 timeout "$TIMEOUT" "$RUN" "$src" >"$WORK/$name.out" 2>"$WORK/$name.err" || status=$?
  failed="$(grep -c '^\[engine\] opt failed' "$WORK/$name.err" || true)"
  done_="$(grep -c '^\[engine\] opt done' "$WORK/$name.err" || true)"
  bailouts=$((bailouts + failed))
  verdict=ok
  if [ "$failed" -ne 0 ]; then
    verdict="FAIL($failed failed compilation(s))"
    grep '^\[engine\] opt failed' "$WORK/$name.err" | cut -c1-240 >&2
    grep -A400 '^\[engine\] opt failed' "$WORK/$name.err" | grep -m 12 '^graalgo\.\|at graalgo\.' >&2 || true
  elif [ "$status" -ne 0 ]; then
    verdict="FAIL(exit $status)"
    grep -v '^\[engine\]' "$WORK/$name.err" | head -10 >&2 || true
  elif ! cmp -s "$WORK/$name.want" "$WORK/$name.out"; then
    verdict="FAIL(stdout differs from gc)"
  elif [ "$done_" -eq 0 ]; then
    verdict="FAIL(nothing compiled)"
  fi
  [ "$verdict" = ok ] || fail=1
  printf 'pe-check %-14s compilations=%-5s %s\n' "$name" "$done_" "$verdict"
done
echo "pe-check: bailouts=$bailouts"
if [ "$fail" -ne 0 ]; then
  echo "pe-check: FAIL" >&2
  exit 1
fi
echo "pe-check: OK"
