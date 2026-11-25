#!/bin/bash
# Spike 03-versiontax, surface (c): -toolexec seam re-verification on go1.26.8.
#
# Runs a 16-script sample of the spike-05 txtar matrix (logs/matrix126_sample.txt)
# twice (stock, wrapped) via a cmd/go test binary built from the 1.26.8 tree.
# Checkpointed + resumable: results appended to logs/matrix126_results.jsonl;
# scripts already recorded are skipped.
#
# Requires: go1.26.8 toolchain built (src/go1.26.8/go/bin/go),
# bin/gotest126.exe (built from src/go1.26.8/go/src/cmd/go with the temporary
# test-harness-only script_test.go patch), and the spike-05 v4 wrapper binary.
set -u

SPIKE=~/workspace/graalgo/spikes/03-versiontax
SPIKE5=~/workspace/graalgo/spikes/05-cmdgo
# F8: the wrapper's response-file code must match the WRAPPED toolchain's
# objabi format. v4 embeds go1.27.1's GCC-style ParseArgs, which corrupts
# go1.26.8's line-based response files (cgo_long_cmd failure, REPORT §3.3).
# bin/named126/compile is v4 with go1.26.8's ParseArgs/encodeArg/DecodeArg
# (src/toolexec-126/main.go), installed under the tool-like basename the
# driver's useResponseFile check requires (spike-05 F6a).
WRAPPER=$SPIKE/bin/named126/compile
GOTEST=$SPIKE/bin/gotest126.exe
GO126=$SPIKE/src/go1.26.8/go
GODIR=$GO126/src/cmd/go
RESULTS=$SPIKE/logs/matrix126_results.jsonl

export PATH=$GO126/bin:$PATH
export GOTOOLCHAIN=local
export GOTMPDIR=$SPIKE/tmp
export TMPDIR=$SPIKE/tmp
mkdir -p "$GOTMPDIR" "$SPIKE/logs/stock126" "$SPIKE/logs/wrapped126" "$SPIKE/logs/wraplogs126"

mapfile -t SCRIPTS < "$SPIKE/logs/matrix126_sample.txt"

done_key() { # $1=script $2=mode -> 0 if already recorded
    [ -f "$RESULTS" ] && grep -q "\"script\":\"$1\",\"mode\":\"$2\"" "$RESULTS"
}

run_one() { # $1=script $2=mode
    local script=$1 mode=$2
    local out="$SPIKE/logs/${mode}126/${script}.log"
    local start end rc
    start=$(date +%s)
    if [ "$mode" = "wrapped" ]; then
        export GRAALGO_WRAPPER_LOG="$SPIKE/logs/wraplogs126/${script}.wraplog"
        export GRAALGO_TOOLEXEC="$WRAPPER"
        export GOCACHE="$SPIKE/gocache126-wrapped"
    else
        unset GRAALGO_WRAPPER_LOG
        unset GRAALGO_TOOLEXEC
        export GOCACHE="$SPIKE/gocache126-stock"
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

echo "=== 1.26 matrix run started $(date -u +%FT%TZ) ==="
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

echo "=== 1.26 matrix run finished $(date -u +%FT%TZ) ==="
