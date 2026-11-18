#!/usr/bin/env bash
# P1 prototype check (docs/arch/abi-spike.md). Three views of the same types
# must agree:
#   gc reflect on compiler-emitted descriptors          (oracle)
#   gc reflect on the Java-synthesized descriptors      (imgcheck)
#   transliterated abi/reflect code on the Java pointer model (Main describe)
# and a deliberately corrupted descriptor must be rejected.
# Usage: run.sh [workdir]. Needs go1.27.1 and a JDK on PATH; all light processes.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
work="${1:-$here/out}"
mkdir -p "$work/classes"
cd "$here"
go run ./typetab fixture > "$work/types.tab"
javac -Xlint:all -Werror -d "$work/classes" java/abispike/*.java
java -Xmx256m -cp "$work/classes" abispike.Main dump "$work/types.tab" "$work/images.dump"
java -Xmx256m -cp "$work/classes" abispike.Main describe "$work/types.tab" "$work/java.lines"
go build -ldflags=-checklinkname=0 -o "$work/imgcheck" ./imgcheck
"$work/imgcheck" -oracle "$work/oracle.lines" "$work/images.dump"
# Every line the Java side computed must be, verbatim, one of gc's answers.
missing="$(grep -Fvxf "$work/oracle.lines" "$work/java.lines" || true)"
if [ -n "$missing" ]; then
  echo "descview: Java-side answers that differ from gc:"; echo "$missing"; exit 1
fi
echo "descview: Java pointer model gave $(wc -l < "$work/java.lines") answers, all identical to gc's"
# Failability: one corrupted field offset must be caught by gc's reflect.
ABISPIKE_MUTATE=1 java -Xmx256m -cp "$work/classes" abispike.Main dump "$work/types.tab" "$work/mutant.dump"
if "$work/imgcheck" "$work/mutant.dump" > "$work/mutant.log" 2>&1; then
  echo "descview: corrupted descriptor was NOT detected"; exit 1
fi
echo "descview: corrupted descriptor rejected ($(grep -c MISMATCH "$work/mutant.log") mismatches)"
