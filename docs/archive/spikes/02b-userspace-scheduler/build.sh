#!/bin/bash
# Build spike 2b with the pinned GraalVM JDK 25.
set -euo pipefail
JH=~/tools/graalvm-jdk-25
S=~/workspace/graalgo/spikes/02b-userspace-scheduler
OUT="$S/classes"
rm -rf "$OUT"; mkdir -p "$OUT" "$S/logs"
"$JH/bin/javac" -Xlint:-options -d "$OUT" $(find "$S/src" -name '*.java')
echo "built -> $OUT"
"$JH/bin/java" -version 2>&1 | head -1
# checksum sources alongside future logs (measurement-honesty rule)
(cd "$S" && find src build.sh run.sh -type f | sort | xargs sha256sum > logs/SOURCES.sha256)
echo "sources checksummed -> logs/SOURCES.sha256"
