#!/bin/bash
# Build the loomsim prototype with the pinned GraalVM JDK 25.
set -euo pipefail
JH=~/tools/graalvm-jdk-25
OUT="$(dirname "$0")/classes"
rm -rf "$OUT"; mkdir -p "$OUT"
"$JH/bin/javac" -d "$OUT" $(find "$(dirname "$0")/src" -name '*.java')
echo "built -> $OUT"
"$JH/bin/java" -version 2>&1 | head -1
