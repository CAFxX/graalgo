#!/bin/sh
# Regenerates be06.patch from the scratch sources vs HEAD (repo read-only).
# Usage: sh gen-patch.sh   (writes /tmp/be06-rederive/be06.patch)
set -eu
R="$HOME/workspace/graalgo"
S="/tmp/be06-rederive/src"
OUT="/tmp/be06-rederive/be06.patch"
: > "$OUT"
add() { # $1 = repo-relative path, $2 = scratch path or /dev/null for new files
    rel="$1"; src="$2"
    diff -u --label "a/$rel" --label "b/$rel" "$R/$rel" "$src" >> "$OUT" || true
}
add internal/backend/runtime/src/main/java/graalgo/launcher/Main.java \
    "$S/graalgo/launcher/Main.java"
add internal/backend/runtime/src/main/java/graalgo/sched/launcher/SchedMain.java \
    "$S/graalgo/sched/launcher/SchedMain.java"
add internal/backend/runtime/src/main/java/graalgo/launcher/README.md \
    "$S/graalgo/launcher/README.md"
add internal/backend/runtime/src/test/java/graalgo/sched/SchedTestRunner.java \
    "$S/graalgo/sched/SchedTestRunner.java"
# New file: diff against /dev/null.
diff -u --label /dev/null \
    --label b/internal/backend/runtime/src/test/java/graalgo/sched/launcher/SchedMainOsArgsTest.java \
    /dev/null "$S/graalgo/sched/launcher/SchedMainOsArgsTest.java" >> "$OUT" || true
wc -l "$OUT"
