#!/bin/bash
# M8-leftovers lane: build + run P1 TypeparamDifferentialTest and P2 JfrEventsTest.testEventsFlow
# Waits for load to clear first. All output to dated log.
set -u
WT=~/workspace/worktrees/graalgo/m9-leftovers
LOGDIR="$WT/phases/phase3/hidden_files/m9-tmp/leftover-tests"
mkdir -p "$LOGDIR"
LOG="$LOGDIR/run-$(date +%Y%m%d-%H%M).log"
exec > >(tee -a "$LOG") 2>&1

echo "=== $(date -Is) M8-leftovers test pipeline start ==="
echo "log: $LOG"

# 1. Wait for load to clear (max 3h)
deadline=$(($(date +%s) + 10800))
while true; do
    if ~/workspace/system/bin/load-shed.sh backoff >/dev/null 2>&1; then
        echo "load clear at $(date -Is)"
        break
    fi
    if [ $(date +%s) -gt $deadline ]; then
        echo "TIMEOUT waiting for load to clear; aborting"
        exit 3
    fi
    sleep 60
done

export PATH="$HOME/tools/go/bin:$PATH"
export JAVA_HOME="$HOME/tools/graalvm-jdk-25"

# 2. Build phase1 (main + gofrontend)
echo "=== building phase1 ==="
cd "$WT/phases/phase1"
if ! nice -n 10 make main >>"$LOG" 2>&1; then
    echo "PHASE1 BUILD FAILED"; exit 1
fi
echo "phase1 main built"

# 3. Build phase1 test classes
echo "=== building phase1 test classes ==="
CP_JARS=$(ls lib/*.jar | tr '\n' ':')
if ! nice -n 10 "$JAVA_HOME/bin/javac" -Xlint:deprecation -Werror \
    -cp "out/classes:$CP_JARS" -processorpath "$CP_JARS" \
    -d out/test-classes $(find src/test/java -name '*.java') >>"$LOG" 2>&1; then
    echo "PHASE1 TEST BUILD FAILED"; exit 1
fi
echo "phase1 test classes built"

# 4. Run TypeparamDifferentialTest (heavy: 266-file differential)
echo "=== TypeparamDifferentialTest ==="
T1_START=$(date +%s)
if nice -n 10 "$JAVA_HOME/bin/java" --enable-native-access=ALL-UNNAMED -Xss32m \
    -Dgraalgo.phase1.dir="$WT/phases/phase1" \
    -Djava.io.tmpdir="$WT/phases/phase1/out/test-tmp" \
    -cp "out/test-classes:out/classes:$CP_JARS" \
    graalgo.test.SingleTestLauncher graalgo.frontend.TypeparamDifferentialTest >>"$LOG" 2>&1; then
    echo "TypeparamDifferentialTest: PASS ($(( $(date +%s) - T1_START ))s)"
else
    echo "TypeparamDifferentialTest: FAIL ($(( $(date +%s) - T1_START ))s)"
fi

# 5. Build phase2 (needs phase1 classes)
echo "=== building phase2 ==="
cd "$WT/phases/phase2"
if ! nice -n 10 make main >>"$LOG" 2>&1; then
    echo "PHASE2 BUILD FAILED"; exit 1
fi
echo "phase2 main built"
echo "=== building phase2 test classes ==="
P1_CLASSES="$WT/phases/phase1/out/classes"
CP2_JARS=$(ls lib/*.jar 2>/dev/null | tr '\n' ':')
if ! nice -n 10 "$JAVA_HOME/bin/javac" -Xlint:deprecation -Werror \
    -cp "out/classes:$P1_CLASSES:$CP_JARS:$CP2_JARS" \
    -d out/test-classes $(find src/test/java -name '*.java') >>"$LOG" 2>&1; then
    echo "PHASE2 TEST BUILD FAILED"; exit 1
fi
echo "phase2 test classes built"

# 6. Run JfrEventsTest.testEventsFlow (SingleTestLauncher lives in phase1 test-classes)
echo "=== JfrEventsTest.testEventsFlow ==="
T2_START=$(date +%s)
mkdir -p "$WT/phases/phase2/out/test-tmp"
if nice -n 10 "$JAVA_HOME/bin/java" --enable-native-access=ALL-UNNAMED -Xss32m \
    -Djava.io.tmpdir="$WT/phases/phase2/out/test-tmp" \
    -cp "out/test-classes:out/classes:$P1_CLASSES:$WT/phases/phase1/out/test-classes:$CP_JARS:$CP2_JARS" \
    graalgo.test.SingleTestLauncher graalgo.jfr.JfrEventsTest testEventsFlow >>"$LOG" 2>&1; then
    echo "JfrEventsTest.testEventsFlow: PASS ($(( $(date +%s) - T2_START ))s)"
else
    echo "JfrEventsTest.testEventsFlow: FAIL ($(( $(date +%s) - T2_START ))s)"
fi

echo "=== $(date -Is) pipeline done ==="
