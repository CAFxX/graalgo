#!/bin/bash
# Java mutant compile leg: for each java mutant in the staged mutants.json,
# apply old->new (first occurrence) to a copy of the live NodeFactory.java
# and compile it with the new truffle classpath. Exits non-zero if any
# mutant fails to compile. Run under nice -n 10.
set -u
REPO=/home/hatch/workspace/graalgo
OUT=/tmp/rev1107-22-r05/java-leg
JAVAC=/home/hatch/tools/graalvm-jdk-25/bin/javac
CP="$REPO/internal/backend/runtime/out/classes:$REPO/internal/backend/truffle/out/classes:$(ls $REPO/internal/backend/truffle/lib/*.jar | paste -sd: -)"
SRC=$REPO/internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java
cp /tmp/rev1107-22-r05/tools-mutation/mutants.json "$OUT/mutants.json"
python3 - "$OUT" "$OUT/mutants.json" <<'EOF'
import json, sys, os
out_dir, spec_path = sys.argv[1], sys.argv[2]
d = json.load(open(spec_path))
for m in d["mutants"]:
    if m["lang"] != "java":
        continue
    src = open(os.path.join("/home/hatch/workspace/graalgo", m["file"])).read()
    assert m["old"] in src, m["id"]
    mutated = src.replace(m["old"], m["new"], 1)
    d2 = os.path.join(out_dir, m["id"])
    os.makedirs(d2, exist_ok=True)
    open(os.path.join(d2, "NodeFactory.java"), "w").write(mutated)
    print(m["id"], "applied")
EOF
fail=0
for m in M-J01 M-J02 M-J04 M-J05; do
  mkdir -p "$OUT/$m-classes"
  if "$JAVAC" -Xlint:deprecation -Werror -cp "$CP" -d "$OUT/$m-classes" "$OUT/$m/NodeFactory.java" > "$OUT/$m.log" 2>&1; then
    echo "$m: COMPILE OK"
  else
    echo "$m: COMPILE FAIL (see $OUT/$m.log)"
    fail=1
  fi
done
exit $fail
