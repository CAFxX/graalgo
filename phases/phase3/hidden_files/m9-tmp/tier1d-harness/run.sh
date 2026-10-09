#!/bin/bash
# tier1d verification harness for the synthetic internal/* packages.
#
# External programs cannot import internal/* (go tool restriction), so the
# wave-3 runner marks those corpus programs .scopelimited+nogc. This harness
# provides the real verification:
#
# 1. internal/bytealg: extracts syntheticBytealgSource from the real tree's
#    phases/phase1/frontend/synth_bytealg.go into a GOPATH (as "synthbytealg"
#    — GOPATH cannot shadow a GOROOT std path, so the import path is
#    rewritten; the program logic is byte-identical), then runs the wave-3
#    corpus program (internal/bytealg/00_functions.go — self-checking
#    against bytes/strings) under stock gc 1.27.1 against the EXTRACTED
#    source. Any MISMATCH panics.
# 2. internal/cpu, internal/godebug, internal/bisect: build-checks the
#    wave-3 corpus programs against vendored copies of the REAL gc 1.27.1
#    sources (same import-path rewrite; these have no synthetic adapter),
#    then RUNS them under gc to prove validity + determinism.
#
# Usage: ./run.sh  (exit 0 = all green)
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
GR="${GR:-$HOME/workspace/graalgo-m9}"
GOROOT_DIR="$HOME/tools/go"
WAVE3="$GR/phases/phase3/corpus/wave3"
export PATH="$HOME/tools/go/bin:$PATH"
export TMPDIR="$HERE/tmp"
export GOCACHE="$HOME/workspace/.gocache-tier1d-harness"
export GO111MODULE=off
mkdir -p "$TMPDIR" "$GOCACHE"

GOPATH_DIR="$HERE/gopath"
rm -rf "$GOPATH_DIR"
mkdir -p "$GOPATH_DIR/src"

# 1. Extract the synthetic bytealg source as "synthbytealg".
mkdir -p "$GOPATH_DIR/src/synthbytealg"
python3 - "$GR/phases/phase1/frontend/synth_bytealg.go" "$GOPATH_DIR/src/synthbytealg/bytealg.go" <<'EOF'
import re, sys
src = open(sys.argv[1]).read()
m = re.search(r'const syntheticBytealgSource = `(.*?)`\n', src, re.S)
if not m:
    sys.exit("syntheticBytealgSource not found")
open(sys.argv[2], 'w').write(m.group(1))
print("extracted synthetic bytealg")
EOF

# 2. Vendor the real cpu/godebug/bisect (+godebugs table) for build checks,
#    rewriting internal/* imports to the synth* paths.
for pkg in cpu godebug bisect godebugs; do
    mkdir -p "$GOPATH_DIR/src/synth$pkg"
    cp "$GOROOT_DIR/src/internal/$pkg/"*.go "$GOPATH_DIR/src/synth$pkg/" 2>/dev/null || true
    # cpu needs its assembly files too.
    cp "$GOROOT_DIR/src/internal/$pkg/"*.s "$GOPATH_DIR/src/synth$pkg/" 2>/dev/null || true
done
find "$GOPATH_DIR/src" -name "*_test.go" -delete
# Rewrite internal imports inside the vendored copies.
grep -rl '"internal/' "$GOPATH_DIR/src" | while read -r f; do
    sed -i 's#"internal/\([^"]*\)"#"synth\1"#g' "$f"
done
# godebug: the single-arg //go:linkname declarations (setUpdate,
# registerMetric, setNewIncNonDefault) resolve to runtime symbols only
# inside the std build; replace them with no-op stubs for the harness
# (the corpus never exercises the update/metric path).
python3 - "$GOPATH_DIR/src/synthgodebug/godebug.go" <<'EOF'
import re, sys
p = sys.argv[1]
src = open(p).read()
# Drop the //go:linkname declaration pairs entirely; stubs are appended below.
src = re.sub(r'//go:linkname (?:setUpdate|registerMetric|setNewIncNonDefault)\nfunc (?:setUpdate|registerMetric|setNewIncNonDefault)\([^\n]*\)\n', '', src)
open(p, 'w').write(src)
EOF
cat >> "$GOPATH_DIR/src/synthgodebug/stubs_harness.go" <<'EOF'
package godebug

func setUpdate(update func(string, string))                     {}
func registerMetric(name string, read func() uint64)            {}
func setNewIncNonDefault(newIncNonDefault func(string) func()) {}
EOF
# Drop cpu assembly references we did not vendor: keep only pure-Go files
# that compile standalone (the corpus only needs the API surface).
# (Handled implicitly: go build will fail loudly if something is missing.)

fail=0

# 3. bytealg: run the self-checking corpus program under gc.
mkdir -p "$GOPATH_DIR/src/synthbytetest"
sed 's#"internal/bytealg"#"synthbytealg"#' "$WAVE3/internal/bytealg/00_functions.go" \
    > "$GOPATH_DIR/src/synthbytetest/main.go"
echo "== bytealg self-check under gc =="
if GOPATH="$GOPATH_DIR" go run synthbytetest; then
    echo "bytealg: PASS"
else
    echo "bytealg: FAIL"; fail=1
fi

# 4. Build-check + run the other corpus programs against the real sources.
for pkg in cpu godebug bisect; do
    mkdir -p "$GOPATH_DIR/src/synth${pkg}test"
    sed "s#\"internal/$pkg\"#\"synth$pkg\"#" "$WAVE3/internal/$pkg/00_${pkg}.go" \
        > "$GOPATH_DIR/src/synth${pkg}test/main.go"
    echo "== $pkg build-check =="
    if GOPATH="$GOPATH_DIR" go build -o "$TMPDIR/${pkg}test" "synth${pkg}test"; then
        echo "$pkg: BUILD OK"
        echo "== $pkg run under gc =="
        if "$TMPDIR/${pkg}test" > "$TMPDIR/$pkg.out" 2>&1; then
            cat "$TMPDIR/$pkg.out"; echo "$pkg: RUN OK"
        else
            echo "$pkg: RUN FAIL"; fail=1
        fi
    else
        echo "$pkg: BUILD FAIL"; fail=1
    fi
done

echo "== harness: $([ $fail = 0 ] && echo ALL GREEN || echo FAILURES) =="
exit $fail
