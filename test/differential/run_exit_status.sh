#!/usr/bin/env bash
# V2 differential: `graalgo run` vs `go run` exit contract
# (fix-p1-run-exitcode, DESIGN.md §8 V2 — the gc-parity pin for §2's table).
#
# Compares the driver (`graalgo run`, built by `make graalgo`) against the
# reference toolchain (`go run`, $DH_GO) on every exit/signal case:
#   hello-ok, os.Exit(1/2/3), panic, fatal deadlock, build failure
#   (file and package targets), missing input file, backend SIGKILL.
#
# Comparison modes (the exit contract is the pin, not the rendering):
#   exact    — stdout, stderr and exit code byte-identical.
#   trailer  — exit code identical; first and last stderr lines identical
#              (panic frame bodies are a documented graalgo-shaped
#              divergence — Main.java class Javadoc — so only the header
#              and the "exit status 2" trailer are compared).
#   lastline — exit code identical; last stderr line identical. Used
#              where pre-existing backend stderr noise (P3Log warnings —
#              backend-side, verified via direct launcher invocation)
#              makes byte-exactness unachievable on this tree, but the
#              driver's own rendering (the final "exit status N" line)
#              is still pinned.
#   contract — exit code identical; neither stderr contains an
#              "exit status" line; both stderrs non-empty (build-failure
#              and missing-input messages are frontend-specific by nature).
#
# Prerequisites: `make build` (truffle/runtime/interop classes) and
# `make graalgo` (the driver) at the repo root. This script never builds.
# Exits non-zero on any mismatch.
set -euo pipefail
cd "$(dirname "$0")"

# Scratch space: /tmp on this VM is a small tmpfs shared with other work;
# keep all temp dirs on the persistent disk (run.sh convention).
export TMPDIR="${TMPDIR:-$HOME/workspace/.tmp}"
mkdir -p "$TMPDIR"

if [ -z "${DH_GO:-}" ]; then
  if [ -x "$HOME/tools/go/bin/go" ]; then export DH_GO="$HOME/tools/go"; fi
fi
if [ -n "${DH_GO:-}" ]; then export PATH="$DH_GO/bin:$PATH"; fi
export GOTOOLCHAIN=local

ROOT="$PWD/../.."
GRAALGO="$ROOT/graalgo"
if [ ! -x "$GRAALGO" ]; then
  echo "run_exit_status.sh: $GRAALGO missing; run 'make graalgo' at the repo root first" >&2
  exit 2
fi
if [ ! -x "$ROOT/internal/backend/graalgo-sync-run" ]; then
  echo "run_exit_status.sh: internal/backend/graalgo-sync-run missing" >&2
  exit 2
fi
if [ ! -d "$ROOT/internal/backend/interop/out/classes" ]; then
  echo "run_exit_status.sh: interop classes not built; run 'make build' at the repo root first" >&2
  exit 2
fi
export GRAALGO_HOME="$ROOT"

WORK="$(mktemp -d "$TMPDIR/run-exit-status-XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

GO="$DH_GO/bin/go"
fail=0
ncases=0

# run_graalgo <timeout-s> -- <graalgo-args...>: runs the driver with a
# deadline. On expiry the whole process tree (driver + backend) is
# SIGKILLed and 124 is returned, so a backend hang fails loudly instead
# of hanging the differential forever.
run_graalgo() {
  local timeout_s="$1"
  shift
  [ "${1:-}" = "--" ] && shift
  "$GRAALGO" "$@" >"$WORK/gr.out" 2>"$WORK/gr.err" &
  local drv=$!
  local elapsed=0
  while kill -0 "$drv" 2>/dev/null; do
    if [ "$elapsed" -ge "$timeout_s" ]; then
      echo "TIMEOUT(${timeout_s}s): SIGKILLing driver tree rooted at $drv" >&2
      for c in $(pgrep -P "$drv" 2>/dev/null); do
        pkill -KILL -P "$c" 2>/dev/null || true
        kill -KILL "$c" 2>/dev/null || true
      done
      kill -KILL "$drv" 2>/dev/null || true
      wait "$drv" 2>/dev/null || true
      return 124
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done
  wait "$drv"
}

# run_both <name> <mode> -- <go-run-args...>
# Runs `go run` and `graalgo run` on the same args, capturing stdout,
# stderr and exit code of each into $WORK.
run_both() {
  local name="$1" mode="$2"
  shift 2
  [ "${1:-}" = "--" ] && shift
  set +e
  "$GO" run "$@" >"$WORK/go.out" 2>"$WORK/go.err"
  go_code=$?
  run_graalgo 240 -- run "$@"
  gr_code=$?
  set -e
  ncases=$((ncases + 1))
  case "$mode" in
    exact)
      if ! cmp -s "$WORK/go.out" "$WORK/gr.out" \
        || ! cmp -s "$WORK/go.err" "$WORK/gr.err" \
        || [ "$go_code" != "$gr_code" ]; then
        echo "FAIL[$name/exact]: go(code=$go_code) vs graalgo(code=$gr_code)"
        diff <(cat "$WORK/go.err") <(cat "$WORK/gr.err") | head -10 || true
        fail=1
      else
        echo "ok[$name/exact] code=$gr_code"
      fi
      ;;
    trailer)
      go_first="$(head -1 "$WORK/go.err")"; go_last="$(tail -1 "$WORK/go.err")"
      gr_first="$(head -1 "$WORK/gr.err")"; gr_last="$(tail -1 "$WORK/gr.err")"
      if [ "$go_code" != "$gr_code" ] \
        || [ "$go_first" != "$gr_first" ] \
        || [ "$go_last" != "$gr_last" ]; then
        echo "FAIL[$name/trailer]: go(code=$go_code first='$go_first' last='$go_last')"
        echo "                     gr(code=$gr_code first='$gr_first' last='$gr_last')"
        fail=1
      else
        echo "ok[$name/trailer] code=$gr_code last='$gr_last'"
      fi
      ;;
    lastline)
      go_last="$(tail -1 "$WORK/go.err")"
      gr_last="$(tail -1 "$WORK/gr.err")"
      if [ "$go_code" != "$gr_code" ] || [ "$go_last" != "$gr_last" ]; then
        echo "FAIL[$name/lastline]: go(code=$go_code last='$go_last')"
        echo "                      gr(code=$gr_code last='$gr_last')"
        fail=1
      else
        echo "ok[$name/lastline] code=$gr_code last='$gr_last'"
      fi
      ;;
    contract)
      if [ "$go_code" != "$gr_code" ] \
        || grep -q "exit status" "$WORK/go.err" \
        || grep -q "exit status" "$WORK/gr.err" \
        || [ ! -s "$WORK/go.err" ] \
        || [ ! -s "$WORK/gr.err" ]; then
        echo "FAIL[$name/contract]: go(code=$go_code) vs graalgo(code=$gr_code)"
        echo "  go stderr:"; head -3 "$WORK/go.err" | sed 's/^/    /'
        echo "  graalgo stderr:"; head -3 "$WORK/gr.err" | sed 's/^/    /'
        fail=1
      else
        echo "ok[$name/contract] code=$gr_code (no 'exit status' line)"
      fi
      ;;
  esac
}

# --- probe programs ---
# NOTE: hello-ok uses the println builtin (WS G intrinsic, always
# available), not fmt.Println: fmt currently fails in the frontend
# ("assign.fields: lhs/rhs length mismatch" — verified via direct
# launcher invocation, pre-existing on this tree, out of scope).
cat > "$WORK/hello.go" <<'EOF'
package main

func main() { println("hello") }
EOF
for n in 1 2 3; do
  cat > "$WORK/exit$n.go" <<EOF
package main

import "os"

func main() { os.Exit($n) }
EOF
done
cat > "$WORK/panic.go" <<'EOF'
package main

func main() { panic("boom") }
EOF
cat > "$WORK/deadlock.go" <<'EOF'
package main

import "runtime"

func main() { runtime.Goexit() }
EOF
# NOTE: the Goexit-in-main shape is the "fatal deadlock" the design
# probed (DESIGN.md §2, §5.1). A bare `select {}` instead hangs the
# backend forever: the drainer-side deadlock detector (lane L31,
# review branch wt/codereview-20261018/review-L31-deadlock-detector)
# is not merged into this tree yet. That is a pre-existing backend
# gap, out of scope for this exit-code lane.
mkdir -p "$WORK/brokenpkg"
cat > "$WORK/broken.go" <<'EOF'
package main

func main() {
	var x int = "not an int"
	_ = x
}
EOF
cp "$WORK/broken.go" "$WORK/brokenpkg/main.go"
cat > "$WORK/spin.go" <<'EOF'
package main

func main() { for {} }
EOF

echo "== exit contract: graalgo run vs go run =="
run_both "hello-ok" exact -- "$WORK/hello.go"
# os.Exit: lastline mode — the driver's "exit status N" rendering is
# pinned; pre-existing P3Log warning noise above it is tolerated.
run_both "os.Exit(1)" lastline -- "$WORK/exit1.go"
run_both "os.Exit(2)" lastline -- "$WORK/exit2.go"
run_both "os.Exit(3)" lastline -- "$WORK/exit3.go"
run_both "panic" trailer -- "$WORK/panic.go"
# deadlock: lastline mode — P3SyncMain prints "all goroutines are asleep"
# for the Goexit situation while gc prints "no goroutines (main called
# runtime.Goexit)"; pre-existing text divergence (the unmerged L31 lane
# aligned SchedMain only). The exit-1 + "exit status 2" contract is pinned.
run_both "deadlock" lastline -- "$WORK/deadlock.go"
run_both "build-failure-file" contract -- "$WORK/broken.go"
run_both "build-failure-pkg" contract -- "$WORK/brokenpkg"
run_both "missing-input" contract -- "$WORK/does-not-exist.go"

# --- self-SIGKILL ---
# The backend has no signal surface for a guest to kill itself with, so:
# gc side — a program that SIGKILLs itself under `go run` (pins gc's
# "signal: killed" rendering); graalgo side — SIGKILL the backend java
# child of a spinning `graalgo run` (pins the driver's new signal branch
# against the real launcher).
echo "== signal death =="
ncases=$((ncases + 1))
cat > "$WORK/sigkill.go" <<'EOF'
package main

import "syscall"

func main() { syscall.Kill(syscall.Getpid(), syscall.SIGKILL) }
EOF
set +e
"$GO" run "$WORK/sigkill.go" >"$WORK/go.out" 2>"$WORK/go.err"
go_code=$?
"$GRAALGO" run "$WORK/spin.go" >"$WORK/gr.out" 2>"$WORK/gr.err" &
drv=$!
child=""
for _ in $(seq 1 90); do
  child="$(pgrep -P "$drv" | head -1)"
  [ -n "$child" ] && break
  sleep 1
done
if [ -z "$child" ]; then
  echo "FAIL[sigkill]: backend child of driver pid $drv never appeared"
  kill "$drv" 2>/dev/null || true
  fail=1
else
  kill -KILL "$child"
  wait "$drv"
  gr_code=$?
  if [ "$go_code" != "1" ] || [ "$gr_code" != "1" ] \
    || ! cmp -s "$WORK/go.err" "$WORK/gr.err"; then
    echo "FAIL[sigkill]: go(code=$go_code err=$(cat "$WORK/go.err")) vs graalgo(code=$gr_code err=$(cat "$WORK/gr.err"))"
    fail=1
  else
    echo "ok[sigkill] code=1 stderr=$(cat "$WORK/gr.err" | tr -d '\n')"
  fi
fi
set -e

echo "== $ncases cases, $fail failures =="
exit "$fail"
