#!/bin/bash
# Spike 03-versiontax: replay the version-tax dry run (go1.26.8 -> go1.27.1).
#
#   ./run.sh          fast surfaces only: (a) asm diff, (b) reflect carryover +
#                     linkname diff + layout diff, (d) go/* API diff.
#                     Needs: src/go1.26.8/go extracted, ~/tools/go present.
#   ./run.sh --full   also: build go1.26.8 (~16 min), 16x2 txtar matrix (~30 min),
#                     harness corpus run (~2 min). Applies the temporary
#                     test-harness-only script_test.go patch for the matrix and
#                     reverts it afterwards (hash-verified).
#
# Checkpointed/resumable: each stage writes logs/<stage>.done; re-running
# skips completed stages. Safe after a VM reboot.
set -u

SPIKE=~/workspace/graalgo/spikes/03-versiontax
OLD=$SPIKE/src/go1.26.8/go
NEW=$HOME/tools/go
FULL=0
[ "${1:-}" = "--full" ] && FULL=1

export PATH=$HOME/tools/go/bin:$PATH
export GOTOOLCHAIN=local

log() { echo "$(date -u +%FT%TZ) $*"; }
stage_done() { [ -f "$SPIKE/logs/$1.done" ]; }
mark_done() { date -u +%FT%TZ > "$SPIKE/logs/$1.done"; }

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing tool: $1"; exit 1; }; }
need python3; need sha256sum

[ -d "$OLD/src" ] || { echo "missing old tree: $OLD/src (extract the tarball first)"; exit 1; }
[ -d "$NEW/src" ] || { echo "missing new tree: $NEW/src"; exit 1; }

# ---- stage 0: verify the 1.26.8 tarball checksum (once) ----
if ! stage_done tarball; then
    log "[0] verifying go1.26.8 tarball checksum against go.dev-published value"
    echo "4e39b98e42f946fa05ac8bc5b71877df97dbdb7cbb1a777b541667ad7117fd2e  /tmp/go1.26.8.src.tar.gz" \
        | sha256sum -c - || exit 1
    mark_done tarball
fi

# ---- stage A: asm/cgo surface ----
if ! stage_done asm; then
    log "[A] asm inventory + semantic diff"
    python3 "$SPIKE/src/asm_inventory.py" "$OLD" "$SPIKE/logs/asm_inv_1.26.8.txt"
    python3 "$SPIKE/src/asm_inventory.py" "$NEW" "$SPIKE/logs/asm_inv_1.27.1.txt"
    python3 "$SPIKE/src/asm_diff.py" "$SPIKE/logs/asm_inv_1.26.8.txt" \
        "$SPIKE/logs/asm_inv_1.27.1.txt" "$SPIKE/logs/asm_diff.txt"
    grep -c "^FILE-" "$SPIKE/logs/asm_diff.txt" | xargs -I{} log "[A] flagged file-level changes: {}"
    mark_done asm
fi

# ---- stage B: reflect surface ----
if ! stage_done reflect; then
    log "[B] reflect inventory + carryover"
    python3 "$SPIKE/src/reflect_inventory.py" "$OLD" "$SPIKE/logs/reflect_raw_1.26.8.txt"
    python3 "$SPIKE/src/reflect_inventory.py" "$NEW" "$SPIKE/logs/reflect_raw_1.27.1.txt"
    python3 "$SPIKE/src/reflect_carryover.py" \
        ~/workspace/graalgo/spikes/04-reflect/src_archive/categorize.py \
        "$SPIKE/logs/reflect_raw_1.27.1.txt" "$SPIKE/logs/reflect_raw_1.26.8.txt" \
        "$SPIKE/logs"
    log "[B] linkname diff (04 tooling, 1.26.8 tree)"
    python3 ~/workspace/graalgo/spikes/04-reflect/src_archive/linkname_diff.py \
        "$OLD" "$NEW" > "$SPIKE/logs/linkname_diff_1.26.8_1.27.1.txt"
    log "[B] rtype/itab layout diff"
    python3 "$SPIKE/src/layout_extract.py" "$OLD/src/internal/abi/type.go" \
        "$OLD/src/runtime/type.go" "$OLD/src/runtime/iface.go" > "$SPIKE/logs/layout_1.26.8.txt"
    python3 "$SPIKE/src/layout_extract.py" "$NEW/src/internal/abi/type.go" \
        "$NEW/src/runtime/type.go" "$NEW/src/runtime/iface.go" > "$SPIKE/logs/layout_1.27.1.txt"
    sed 's|([^)]*go/src|(|' "$SPIKE/logs/layout_1.26.8.txt" > /tmp/l_old.txt
    sed 's|([^)]*go/src|(|' "$SPIKE/logs/layout_1.27.1.txt" > /tmp/l_new.txt
    if diff -q /tmp/l_old.txt /tmp/l_new.txt >/dev/null; then log "[B] layouts: 0 changes"; else log "[B] layouts: CHANGED (see logs)"; fi
    mark_done reflect
fi

# ---- stage D: go/* API surface ----
if ! stage_done api; then
    log "[D] go/* API dump + diff"
    [ -x "$SPIKE/bin/api_dump" ] || (cd "$SPIKE" && go build -o bin/api_dump src/api_dump.go)
    "$SPIKE/bin/api_dump" "$OLD" "$SPIKE/logs/api_1.26.8.txt"
    "$SPIKE/bin/api_dump" "$NEW" "$SPIKE/logs/api_1.27.1.txt"
    diff "$SPIKE/logs/api_1.26.8.txt" "$SPIKE/logs/api_1.27.1.txt" > "$SPIKE/logs/api_diff.txt" || true
    grep -c "^[<>]" "$SPIKE/logs/api_diff.txt" | xargs -I{} log "[D] API diff lines: {}"
    mark_done api
fi

if [ "$FULL" = "0" ]; then
    log "fast replay done. Re-run with --full for the toolchain build, txtar matrix, and harness run."
    exit 0
fi

# ---- stage C/E prep: build go1.26.8 ----
if ! stage_done build126; then
    log "[build] building go1.26.8 from source (~16 min, detached-safe)"
    (cd "$OLD/src" && GOROOT_BOOTSTRAP=$HOME/tools/go PATH=$HOME/tools/go/bin:$PATH \
        ./make.bash > "$SPIKE/logs/go126_build.log" 2>&1 \
        && echo "EXIT=$?" >> "$SPIKE/logs/go126_build.log")
    "$OLD/bin/go" version || exit 1
    mark_done build126
fi

# ---- stage C: txtar matrix ----
if ! stage_done matrix; then
    log "[C] building 1.26 cmd/go test binary (temporary test-harness-only patch)"
    ST=$OLD/src/cmd/go/script_test.go
    sha256sum "$ST" > "$SPIKE/logs/script_test.go.pristine.sha256"
    python3 - "$ST" <<'EOF'
import sys
p=sys.argv[1]; s=open(p).read()
s=s.replace('\t"GCCGOTOOLDIR",       // for gccgo testing\n}',
            '\t"GCCGOTOOLDIR",       // for gccgo testing\n\t"GRAALGO_WRAPPER_LOG", // spike-03 dry run (temporary, test-harness-only)\n}')
s=s.replace('''	for _, key := range extraEnvKeys {
		if val, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+val)
		}
	}

	return env, nil
}''','''	for _, key := range extraEnvKeys {
		if val, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+val)
		}
	}
	if toolexec, ok := os.LookupEnv("GRAALGO_TOOLEXEC"); ok {
		env = append(env, "GOFLAGS=-toolexec="+toolexec)
	}

	return env, nil
}''')
open(p,"w").write(s)
EOF
    (cd "$OLD/src/cmd/go" && PATH="$OLD/bin:$PATH" GOTMPDIR="$SPIKE/tmp" TMPDIR="$SPIKE/tmp" \
        go test -c -o "$SPIKE/bin/gotest126.exe" .)
    log "[C] running 16x2 txtar matrix (detached)"
    setsid nohup bash "$SPIKE/run_matrix126.sh" > "$SPIKE/logs/matrix126_runner.out" 2>&1 < /dev/null &
    log "[C] matrix launched; waiting for completion"
    while ! grep -q "matrix run finished" "$SPIKE/logs/matrix126_runner.out" 2>/dev/null; do sleep 60; done
    log "[C] reverting temporary script_test.go patch"
    # restore pristine content from the tarball, then verify against recorded hash
    tar -xzf /tmp/go1.26.8.src.tar.gz -C /tmp/restore126 --wildcards '*/src/cmd/go/script_test.go' 2>/dev/null \
        && cp /tmp/restore126/go/src/cmd/go/script_test.go "$ST" && rm -rf /tmp/restore126
    (cd "$OLD/src/cmd/go" && sha256sum -c "$SPIKE/logs/script_test.go.pristine.sha256") \
        && log "[C] patch reverted, hash verified" || { echo "REVERT FAILED"; exit 1; }
    mark_done matrix
fi

# ---- stage E: harness corpus ----
if ! stage_done harness; then
    log "[E] harness corpus: gc-1.27.1 vs gc-1.26.8"
    (cd ~/workspace/graalgo/tools/diffharness \
        && DH_GO=$HOME/tools/go DH_GO126=$OLD PATH=$HOME/tools/go/bin:$PATH \
           TMPDIR=$HOME/workspace/.tmp \
           go run ./cmd/diffharness run \
             -config "$SPIKE/logs/backends126.json" -corpus corpus \
             -out "$SPIKE/logs/harness_126.json")
    mark_done harness
fi

log "full replay done."
