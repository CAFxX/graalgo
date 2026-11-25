#!/bin/bash
# Spike 5 matrix runner: runs each script in scripts/matrix.txt twice
# (stock, wrapped) via the prebuilt cmd/go test binary.
#
# Checkpointing: results appended to logs/matrix_results.jsonl; scripts
# already present are skipped, so the runner is resumable after a reboot.
# Run detached: setsid nohup ./run_matrix.sh > logs/runner.out 2>&1 &
set -u

SPIKE=~/workspace/graalgo/spikes/05-cmdgo
BIN=$SPIKE/bin
WRAPPER=$BIN/toolexec-log
GOTEST=$BIN/gotest.exe
GODIR=~/tools/go/src/cmd/go
RESULTS=$SPIKE/logs/matrix_results.jsonl

export PATH=~/tools/go/bin:$PATH

# /tmp is a 512M tmpfs (2026-10-03: 81% used); keep all build/test temp
# output on the persistent filesystem instead.
export GOTMPDIR=$SPIKE/tmp
export TMPDIR=$SPIKE/tmp
mkdir -p "$GOTMPDIR"

mkdir -p "$SPIKE/logs/stock" "$SPIKE/logs/wrapped" "$SPIKE/logs/wraplogs"

# Parse the machine-readable list (one script name per line).
mapfile -t SCRIPTS < "$SPIKE/scripts/matrix_list.txt"

done_key() { # $1=script $2=mode -> 0 if already recorded
    [ -f "$RESULTS" ] && grep -q "\"script\":\"$1\",\"mode\":\"$2\"" "$RESULTS"
}

run_one() { # $1=script $2=mode
    local script=$1 mode=$2
    local out="$SPIKE/logs/$mode/${script}.log"
    local start end rc
    start=$(date +%s)
    if [ "$mode" = "wrapped" ]; then
        export GRAALGO_WRAPPER_LOG="$SPIKE/logs/wraplogs/${script}.wraplog"
        export GRAALGO_TOOLEXEC="$WRAPPER"
        export GOCACHE="$SPIKE/gocache-wrapped"
    else
        unset GRAALGO_WRAPPER_LOG
        unset GRAALGO_TOOLEXEC
        export GOCACHE="$SPIKE/gocache-stock"
    fi
    mkdir -p "$GOCACHE"
    : > "$out"
    (
        cd "$GODIR" || exit 99
        timeout 600 "$GOTEST" \
            -test.run "TestScript/^${script}\$" \
            -test.v -test.parallel 1 -test.count=1 -test.timeout 9m \
            >"$out" 2>&1
    )
    rc=$?
    end=$(date +%s)
    local pass=false
    [ $rc -eq 0 ] && pass=true
    printf '{"script":"%s","mode":"%s","pass":%s,"rc":%d,"duration_s":%d}\n' \
        "$script" "$mode" "$pass" "$rc" "$((end-start))" >> "$RESULTS"
    echo "$(date -u +%FT%TZ) [$mode] $script pass=$pass rc=$rc ${end}s>$((end-start))s"
}

echo "=== matrix run started $(date -u +%FT%TZ) ==="
echo "scripts: ${#SCRIPTS[@]} x 2 modes"

for mode in stock wrapped; do
    for script in "${SCRIPTS[@]}"; do
        if done_key "$script" "$mode"; then
            echo "skip [$mode] $script (already recorded)"
            continue
        fi
        run_one "$script" "$mode"
    done
done

echo "=== matrix run finished $(date -u +%FT%TZ) ==="
