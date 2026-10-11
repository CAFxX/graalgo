#!/bin/bash
# BUG-MODPROBE-012 probe: run a Go program under graalgo with timeout + jstack on hang.
# Usage: probe.sh <go-file> [timeout-secs] [gmp]
set -u
GO_FILE="$1"
TIMEOUT="${2:-60}"
GMP="${3:-1}"
W="$HOME/workspace/worktrees/graalgo/m9-uuid012"
P3="$W/phases/phase3"; P2="$W/phases/phase2"; P1="$W/phases/phase1"
export PATH="$HOME/tools/go/bin:$PATH"
JAVA="$HOME/tools/graalvm-jdk-25/bin/java"
JSTACK="$HOME/tools/graalvm-jdk-25/bin/jstack"
TMPDIR="$P3/hidden_files/uuid012/tmp"
mkdir -p "$TMPDIR"
export TMPDIR
CP="$P3/out/classes:$P2/out/classes:$P1/out/classes:$(ls "$P1"/lib/*.jar | tr '\n' ':')"
OUT="$P3/hidden_files/uuid012/probe-$(basename "$GO_FILE" .go)-gmp$GMP.log"
# Run from the module dir so go.mod resolves
cd "$(dirname "$GO_FILE")"
echo "=== probe $(date -u +%FT%TZ) gmp=$GMP timeout=$TIMEOUT file=$GO_FILE ===" | tee "$OUT"
timeout "$TIMEOUT" "$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m -Xmx1g \
  -Djava.io.tmpdir="$TMPDIR" -cp "$CP" \
  graalgo.p3sync.launcher.P3SyncMain --gmp="$GMP" -- "$GO_FILE" > "$OUT.tmp" 2> "$OUT.err"; EC=$?
echo "exit=$EC" | tee -a "$OUT"
if [ $EC -eq 124 ]; then
  echo "TIMEOUT — capturing jstack of lingering java processes" | tee -a "$OUT"
  for pid in $(pgrep -f "P3SyncMain" | head -3); do
    echo "--- jstack $pid ---" >> "$OUT"
    "$JSTACK" "$pid" >> "$OUT" 2>&1 | head -100 >> "$OUT"
  done
  pkill -f "P3SyncMain.*$(basename "$GO_FILE")" 2>/dev/null
else
  echo "--- stdout ---" >> "$OUT"; cat "$OUT.tmp" >> "$OUT"
  echo "--- stderr (filtered) ---" >> "$OUT"; grep -v "^WARNING: " "$OUT.err" | head -5 >> "$OUT"
fi
echo "log: $OUT"
