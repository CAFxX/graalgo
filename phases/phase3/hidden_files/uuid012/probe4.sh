#!/bin/bash
# Robust hang probe: start JVM in background, jstack while running, then kill.
set -u
GO_FILE="$1"; TIMEOUT="${2:-280}"; GMP="${3:-4}"
W="$HOME/workspace/worktrees/graalgo/m9-uuid012"
P3="$W/phases/phase3"; P2="$W/phases/phase2"; P1="$W/phases/phase1"
export PATH="$HOME/tools/go/bin:$PATH"
JAVA="$HOME/tools/graalvm-jdk-25/bin/java"
JSTACK="$HOME/tools/graalvm-jdk-25/bin/jstack"
TMPDIR="$P3/hidden_files/uuid012/tmp"; mkdir -p "$TMPDIR"; export TMPDIR
CP="$P3/out/classes:$P2/out/classes:$P1/out/classes:$(ls "$P1"/lib/*.jar | tr '\n' ':')"
TAG="uuid012-$(basename "$GO_FILE" .go)-gmp$GMP-$(date -u +%H%M%S)"
OUT="$P3/hidden_files/uuid012/$TAG.log"
cd "$(dirname "$GO_FILE")"
echo "=== $TAG start $(date -u +%FT%TZ) ===" | tee "$OUT"
"$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m -Xmx1g \
  -Djava.io.tmpdir="$TMPDIR" -cp "$CP" \
  graalgo.p3sync.launcher.P3SyncMain --gmp="$GMP" -- "$GO_FILE" > "$OUT.tmp" 2> "$OUT.err" &
JPID=$!
echo "jpid=$JPID" | tee -a "$OUT"
# Wait up to TIMEOUT
for ((i=0; i<TIMEOUT; i+=10)); do
  sleep 10
  if ! kill -0 $JPID 2>/dev/null; then
    wait $JPID; EC=$?
    echo "exit=$EC (completed)" | tee -a "$OUT"
    echo "--- stdout ---" >> "$OUT"; cat "$OUT.tmp" >> "$OUT"
    echo "COMPLETED ec=$EC" >> "$OUT"
    exit 0
  fi
done
echo "TIMEOUT after ${TIMEOUT}s — jstack:" | tee -a "$OUT"
"$JSTACK" $JPID > "$OUT.jstack" 2>&1
echo "--- jstack main/virtual threads ---" >> "$OUT"
grep -A 30 "Mounted virtual thread" "$OUT.jstack" | head -40 >> "$OUT"
grep -B2 -A 12 "\"main\" #" "$OUT.jstack" | head -20 >> "$OUT"
kill -9 $JPID 2>/dev/null
echo "HUNG" >> "$OUT"
echo "hung, see $OUT"
