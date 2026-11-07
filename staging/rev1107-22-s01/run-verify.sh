#!/bin/bash
# S-01 RED->GREEN verification (resumed lane rev1107-22-s01).
# Overlays shadow the repo's prebuilt HEAD classes; repo tree untouched (read-only).
# CP order: overlay/test-classes : overlay/classes : staged test-classes (superset
# of repo test-classes; has LauncherOsArgsTest + the new test) : repo main classes.
set -u
S=/tmp/rev1107-22-s01
R=~/workspace/graalgo
GV=~/tools/graalvm-jdk-25/bin
JARS=$(ls $R/internal/backend/truffle/lib/*.jar | paste -sd: -)
MAIN_CP="$R/internal/backend/runtime/out/classes:$R/internal/backend/truffle/out/classes:$R/internal/backend/interop/out/classes:$JARS"
STAGED_TEST="$S/build/graalgo/internal/backend/runtime/out/test-classes"
JAVA_FLAGS="--enable-native-access=ALL-UNNAMED -Xss32m"
MODE="$1"   # red | green
if [ "$MODE" = red ]; then
  SRC="$S/pristine/SelectOp.java"; OUT="$S/red"
elif [ "$MODE" = green ]; then
  SRC="$S/apply-check/internal/backend/runtime/src/main/java/graalgo/runtime/SelectOp.java"; OUT="$S/green"
else echo "usage: $0 red|green"; exit 2; fi
nice -n 10 "$GV/javac" -Xlint:deprecation -Werror -cp "$MAIN_CP" -d "$OUT/classes" "$SRC" || { echo "COMPILE SelectOp FAILED ($MODE)"; exit 1; }
nice -n 10 "$GV/javac" -Xlint:deprecation -Werror -cp "$OUT/classes:$STAGED_TEST:$MAIN_CP" -d "$OUT/test-classes" \
  "$S/apply-check/internal/backend/runtime/src/test/java/graalgo/runtime/SelectSweepPoisonTest.java" || { echo "COMPILE test FAILED ($MODE)"; exit 1; }
echo "=== $MODE compile OK ==="
nice -n 10 "$GV/java" $JAVA_FLAGS -cp "$S/build/runner-classes:$OUT/test-classes:$OUT/classes:$STAGED_TEST:$MAIN_CP" \
  SingleTestRunner graalgo.runtime.SelectSweepPoisonTest
