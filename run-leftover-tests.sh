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

# Jar paths: absolute. Phase2 has NO lib/ dir of its own — it borrows
# phase1's jars. The old script captured relative lib/*.jar paths while cwd
# was phase1; after cd phases/phase2 they resolved to the nonexistent
# phases/phase2/lib/*.jar, which javac silently drops — hence
# "class file for AbstractTruffleException not found" and the secondary
# "PanicException cannot be converted to Throwable" errors.
P1_LIB="$WT/phases/phase1/lib"
CP_JARS=$(ls "$P1_LIB"/*.jar | tr '\n' ':')
P1_CLASSES="$WT/phases/phase1/out/classes"
P1_TEST_CLASSES="$WT/phases/phase1/out/test-classes"

# 2. Build phase1 (main + gofrontend)
echo "=== building phase1 ==="
cd "$WT/phases/phase1"
if ! nice -n 10 make main >>"$LOG" 2>&1; then
    echo "PHASE1 BUILD FAILED"; exit 1
fi
echo "phase1 main built"

# 3. Build phase1 test classes
echo "=== building phase1 test classes ==="
if ! nice -n 10 "$JAVA_HOME/bin/javac" -Xlint:deprecation -Werror \
    -cp "out/classes:$CP_JARS" -processorpath "$CP_JARS" \
    -d out/test-classes $(find src/test/java -name '*.java') >>"$LOG" 2>&1; then
    echo "PHASE1 TEST BUILD FAILED"; exit 1
fi
# Fail fast: the launcher must resolve as graalgo.test.SingleTestLauncher
# (a previous run died with ClassNotFoundException because the source lacked
# its package declaration and compiled into the default package).
if [ ! -f "$P1_TEST_CLASSES/graalgo/test/SingleTestLauncher.class" ]; then
    echo "LAUNCHER CLASS MISSING: $P1_TEST_CLASSES/graalgo/test/SingleTestLauncher.class not found"
    exit 1
fi
echo "phase1 test classes built (launcher resolves)"

# 4. Run TypeparamDifferentialTest (heavy: 266-file differential)
echo "=== TypeparamDifferentialTest ==="
mkdir -p "$WT/phases/phase1/out/test-tmp"
T1_START=$(date +%s)
if nice -n 10 "$JAVA_HOME/bin/java" --enable-native-access=ALL-UNNAMED -Xss32m \
    -Dgraalgo.phase1.dir="$WT/phases/phase1" \
    -Djava.io.tmpdir="$WT/phases/phase1/out/test-tmp" \
    -cp "$P1_TEST_CLASSES:$P1_CLASSES:$CP_JARS" \
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
# NOTE: $CP_JARS is absolute (phase1's lib). Phase2 has no lib/ of its own.
if ! nice -n 10 "$JAVA_HOME/bin/javac" -Xlint:deprecation -Werror \
    -cp "out/classes:$P1_CLASSES:$CP_JARS" \
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
    -cp "out/test-classes:out/classes:$P1_CLASSES:$P1_TEST_CLASSES:$CP_JARS" \
    graalgo.test.SingleTestLauncher graalgo.jfr.JfrEventsTest testEventsFlow >>"$LOG" 2>&1; then
    echo "JfrEventsTest.testEventsFlow: PASS ($(( $(date +%s) - T2_START ))s)"
else
    echo "JfrEventsTest.testEventsFlow: FAIL ($(( $(date +%s) - T2_START ))s)"
fi

echo "=== $(date -Is) pipeline done ==="
