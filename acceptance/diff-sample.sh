#!/bin/bash
GR=$HOME/workspace/worktrees/graalgo/m9-acceptance
P3="$GR/phases/phase3"; P2="$GR/phases/phase2"; P1="$GR/phases/phase1"
JAVA="$HOME/tools/graalvm-jdk-25/bin/java"
CP="$P3/out/classes:$P2/out/classes:$P1/out/classes:$(ls "$P1"/lib/*.jar | tr '\n' ':')"
export TMPDIR="$P3/hidden_files/m7-tmp/wave1"
export PATH="$HOME/tools/go/bin:$PATH"

pass=0; fail=0
for go in "$@"; do
    base=$(basename "$go" .go)
    if [ -f "${go%.go}.scopelimited" ]; then
        echo "SKIP $base (scope-limited)"
        continue
    fi
    if ! go build -o /tmp/diff-gc "$go" 2>/dev/null; then
        echo "SKIP $base (gc build failed)"
        continue
    fi
    /tmp/diff-gc > /tmp/diff-gc.out 2>&1; gc_exit=$?
    timeout 60 $JAVA --enable-native-access=ALL-UNNAMED -Xss32m \
        -Djava.io.tmpdir="$TMPDIR" -cp "$CP" \
        graalgo.p3sync.launcher.P3SyncMain --gmp=1 "$go" 2>&1 | grep -v "^WARNING" > /tmp/diff-gr.out; gr_exit=${PIPESTATUS[0]}
    if [ "$gr_exit" -eq 124 ]; then
        echo "FAIL $base: TIMEOUT"
        fail=$((fail+1))
        continue
    fi
    if cmp -s /tmp/diff-gc.out /tmp/diff-gr.out && [ "$gc_exit" = "$gr_exit" ]; then
        echo "PASS $base"
        pass=$((pass+1))
    else
        echo "FAIL $base: exit gc=$gc_exit gr=$gr_exit"
        fail=$((fail+1))
    fi
done
echo "== $pass passed, $fail failed =="
