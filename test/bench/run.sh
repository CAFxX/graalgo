#!/usr/bin/env bash
# test/bench/run.sh — gc vs graalgo steady-state micro-benchmarks.
#
# Each benchmark is a self-contained `package main` under test/bench/<name>/
# that repeats its workload several times and reports the BEST round
# (steady state: later rounds run JIT-compiled code under graalgo) as
#   BENCH <name> <nanoseconds>
# on stderr, and a deterministic result on stdout. The runner builds the
# program with gc, runs it, runs it under graalgo (graalgo-sync-run, which
# requires `make build`), FAILS if the two stdouts differ, and prints a
# table of best-round times and the graalgo/gc ratio.
#
# Usage: test/bench/run.sh [name...]      (default: every benchmark)
# Env:   GO (default ~/tools/go/bin/go), GRAALGO_RUN (default
#        internal/backend/graalgo-sync-run), BENCH_TIMEOUT (seconds per
#        graalgo run, default 600), BENCH_JAVA_OPTS (extra JVM -D options,
#        passed via JAVA_TOOL_OPTIONS).
set -euo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
GO="${GO:-$HOME/tools/go/bin/go}"
GRAALGO_RUN="${GRAALGO_RUN:-$ROOT/internal/backend/graalgo-sync-run}"
BENCH_TIMEOUT="${BENCH_TIMEOUT:-600}"

if [ $# -eq 0 ]; then
  set -- $(cd "$HERE" && for d in */; do [ -f "$d/main.go" ] && echo "${d%/}"; done)
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/graalgo-bench.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

bench_ns() { # file name -> ns (or empty)
  sed -n "s/^BENCH $2 \([0-9][0-9]*\)\$/\1/p" "$1" | tail -1
}

fail=0
printf '%-14s %14s %14s %10s  %s\n' benchmark gc_ms graalgo_ms ratio output
for name in "$@"; do
  src="$HERE/$name/main.go"
  if [ ! -f "$src" ]; then
    echo "run.sh: no such benchmark: $name" >&2
    exit 2
  fi
  "$GO" build -o "$WORK/$name.gc" "$src"
  "$WORK/$name.gc" >"$WORK/$name.gc.out" 2>"$WORK/$name.gc.err"
  gs=0
  JAVA_TOOL_OPTIONS="${BENCH_JAVA_OPTS:-}" nice -n 10 timeout "$BENCH_TIMEOUT" \
    "$GRAALGO_RUN" "$src" >"$WORK/$name.gg.out" 2>"$WORK/$name.gg.err" || gs=$?
  gcns="$(bench_ns "$WORK/$name.gc.err" "$name")"
  ggns="$(bench_ns "$WORK/$name.gg.err" "$name")"
  verdict=ok
  if [ "$gs" -ne 0 ]; then
    verdict="FAIL(graalgo exit $gs)"
    fail=1
    sed 's/^/    | /' "$WORK/$name.gg.err" | head -20 >&2
  elif ! cmp -s "$WORK/$name.gc.out" "$WORK/$name.gg.out"; then
    verdict="FAIL(stdout differs from gc)"
    fail=1
    diff "$WORK/$name.gc.out" "$WORK/$name.gg.out" | head -10 >&2 || true
  elif [ -z "$gcns" ] || [ -z "$ggns" ]; then
    verdict="FAIL(missing BENCH line)"
    fail=1
  fi
  if [ -n "$gcns" ] && [ -n "$ggns" ]; then
    awk -v n="$name" -v a="$gcns" -v b="$ggns" -v v="$verdict" \
      'BEGIN { printf "%-14s %14.3f %14.3f %9.1fx  %s\n", n, a/1e6, b/1e6, b/a, v }'
  else
    printf '%-14s %14s %14s %10s  %s\n' "$name" "${gcns:--}" "${ggns:--}" - "$verdict"
  fi
done
exit "$fail"
