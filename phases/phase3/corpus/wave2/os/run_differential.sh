#!/bin/bash
# Differential runner for the wave-2 os corpus (P3-M8).
# For each NN_*.go: runs the gc leg (go run) and the graalgo leg
# (graalgo-sync-run, args after -- from the .args fixture when present),
# then compares stdout and exit codes.
#
# Tmp discipline: NEVER /tmp. TMPDIR and GOCACHE are forced below.
# Hermetic: every program uses its own MkdirTemp + RemoveAll; nothing
# touches shared state. Permission tests skip under root (see 04).
#
# Expected divergences (documented, not failures):
#   04_path_ops.go: SETUID-OK (gc) vs SETUID-ERR (graalgo) — M8 Chmod
#     rejects setuid/setgid/sticky with a descriptive error (§6.3);
#     SKIP-PERMDENIED vs PERMDENIED-OK — root bypasses permission checks.
#   13_failfast.go: stdout is "pinned <case>" on both legs and the exit
#     code is 2 on both legs; the graalgo leg additionally panics with
#     the pinned "not in M8 scope" message on stderr (checked).
set -u

PIPE="$HOME/workspace/graalgo-m8-pipe"
OSDIR="$PIPE/phases/phase3/corpus/wave2/os"
export TMPDIR="$PIPE/phases/phase3/hidden_files/m8-tmp/os"
export GOCACHE="$HOME/workspace/.gocache-m8-os"
mkdir -p "$TMPDIR" "$GOCACHE"
GO="$HOME/tools/go/bin/go"
RUN="${GRAALGO_RUN:-$PIPE/phases/phase3/out/graalgo-sync-run}"

pass=0; fail=0; failed_cases=()

if [[ ! -x "$RUN" ]]; then
    echo "FAIL: graalgo launcher not found: $RUN"
    echo "  build phase3 first (make main), or set GRAALGO_RUN"
    exit 1
fi

# normalize <file>: expected divergences for 04.
normalize() {
    local f="$1"
    sed -e 's/^SETUID-OK$/SETUID-NORM/' -e 's/^SETUID-ERR$/SETUID-NORM/' \
        -e 's/^SKIP-PERMDENIED (root)$/PERMDENIED-NORM/' \
        -e 's/^PERMDENIED-OK$/PERMDENIED-NORM/' "$f"
}

run_case() { # <name> <go-file> <expected-exit> [extra graalgo args...]
    local name="$1" gofile="$2" want_exit="$3"
    shift 3
    local args=()
    if [[ -f "$OSDIR/$gofile.args" ]]; then
        # shellcheck disable=SC2206
        args+=($(cat "$OSDIR/$gofile.args"))
    fi
    args+=("$@")

    # Build the gc leg once: `go run` masks the program's exit code
    # (it prints "exit status N" and exits 1), so run the binary.
    local gc_bin="$TMPDIR/$name.gcbin"
    if ! (cd "$TMPDIR" && "$GO" build -o "$gc_bin" "$OSDIR/$gofile" 2>"$TMPDIR/$name.gcbuild"); then
        fail=$((fail+1)); failed_cases+=("$name")
        echo "FAIL $name (gc build):"; cat "$TMPDIR/$name.gcbuild"
        return
    fi

    local gc_out gc_exit gr_out gr_exit
    gc_out="$TMPDIR/$name.gc.out"; gr_out="$TMPDIR/$name.gr.out"
    (cd "$TMPDIR" && "$gc_bin" "${args[@]}" >"$gc_out" 2>&1)
    gc_exit=$?
    if [[ ${#args[@]} -gt 0 ]]; then
        (cd "$TMPDIR" && "$RUN" "$OSDIR/$gofile" -- "${args[@]}" >"$gr_out" 2>"$TMPDIR/$name.gr.err")
    else
        (cd "$TMPDIR" && "$RUN" "$OSDIR/$gofile" >"$gr_out" 2>"$TMPDIR/$name.gr.err")
    fi
    gr_exit=$?

    local ok=1
    [[ $gc_exit -eq "$want_exit" ]] || { ok=0; echo "  gc exit=$gc_exit, want $want_exit"; }
    [[ $gr_exit -eq "$want_exit" ]] || { ok=0; echo "  graalgo exit=$gr_exit, want $want_exit"; }
    if ! diff <(normalize "$gc_out") <(normalize "$gr_out") >"$TMPDIR/$name.diff" 2>&1; then
        ok=0
        echo "  stdout differs:"
        sed 's/^/    /' "$TMPDIR/$name.diff" | head -20
    fi
    if [[ $ok -eq 1 ]]; then
        pass=$((pass+1)); echo "PASS $name"
    else
        fail=$((fail+1)); failed_cases+=("$name"); echo "FAIL $name"
    fi
}

echo "== os corpus: plain programs =="
for n in 00 01 02 03 04 05 06 07 08 09 10 12; do
    f=$(ls "$OSDIR/${n}_"*.go)
    run_case "$n" "$(basename "$f")" 0
done

echo "== os corpus: exit code =="
run_case "11_exit3" "11_exit3.go" 3

echo "== os corpus: fail-fast pins =="
"$GO" build -o "$TMPDIR/13.gcbin" "$OSDIR/13_failfast.go" || { echo "FAIL: gc build of 13_failfast.go"; exit 1; }
for c in StartProcess Chown Lchown SyscallConn OpenRoot Executable CopyFS DirFS PipeDeadline; do
    name="13_$c"
    gc_out="$TMPDIR/$name.gc.out"; gr_out="$TMPDIR/$name.gr.out"; gr_err="$TMPDIR/$name.gr.err"
    (cd "$TMPDIR" && "$TMPDIR/13.gcbin" "$c" >"$gc_out" 2>&1); gc_exit=$?
    (cd "$TMPDIR" && "$RUN" "$OSDIR/13_failfast.go" -- "$c" >"$gr_out" 2>"$gr_err"); gr_exit=$?
    ok=1
    [[ $gc_exit -eq 2 ]] || { ok=0; echo "  gc exit=$gc_exit, want 2"; }
    [[ $gr_exit -eq 2 ]] || { ok=0; echo "  graalgo exit=$gr_exit, want 2"; }
    diff "$gc_out" "$gr_out" >/dev/null 2>&1 || { ok=0; echo "  stdout differs:"; diff "$gc_out" "$gr_out" | head -5; }
    grep -q "not in M8 scope" "$gr_err" || { ok=0; echo "  graalgo stderr lacks the pinned panic:"; head -3 "$gr_err"; }
    if [[ $ok -eq 1 ]]; then pass=$((pass+1)); echo "PASS $name"; else fail=$((fail+1)); failed_cases+=("$name"); echo "FAIL $name"; fi
done

echo
echo "$pass passed, $fail failed"
if [[ $fail -gt 0 ]]; then
    printf 'failed: %s\n' "${failed_cases[@]}"
    exit 1
fi
