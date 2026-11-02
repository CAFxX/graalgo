#!/bin/sh
# Build/test/vet gofrontend using ONLY the vendored go/* sources.
#
# The vendored tree at ./go is byte-identical to $GOROOT/src/go (see
# VENDORING.md). The go command resolves "go/ast" etc. to GOROOT, so this
# script forces every GOROOT go/* file read through the vendored copy with
# -overlay. The compiled binary therefore literally contains the vendored
# sources; any local patch to ./go changes the build AND fails the
# `diff -r` gate in VENDORING.md.
#
# Usage: ./build.sh [build|vet|test|check]   (default: build)
set -eu
cd "$(dirname "$0")"

# Pinned toolchain (R24): Makefiles export GO (the pinned go1.27.1); the
# truffle Makefile instead prepends the pinned dir to PATH. Honor GO when
# it names an executable, then fail fast unless `go version` is go1.27.1:
# a silently different toolchain would build a different gofrontend.
if [ -n "${GO:-}" ] && [ -x "$GO" ]; then
  PATH="$(dirname "$GO"):$PATH"
  export PATH
fi
GO_VER="$(go version 2>/dev/null | awk '{print $3}')"
if [ "$GO_VER" != "go1.27.1" ]; then
  echo "build.sh: requires go1.27.1, found '${GO_VER:-none}' (set GO to the pinned toolchain)" >&2
  exit 1
fi

GOROOT_DIR="$(go env GOROOT)"
python3 - "$GOROOT_DIR" "$PWD/go" > overlay.json <<'EOF'
import json, os, sys
goroot, vend = sys.argv[1], sys.argv[2]
rep = {}
for root, _, files in os.walk(vend):
    for fn in files:
        if fn.endswith('.go'):
            v = os.path.join(root, fn)
            g = os.path.join(goroot, 'src', 'go', os.path.relpath(v, vend))
            rep[g] = v
json.dump({"Replace": rep}, sys.stdout)
EOF
N=$(python3 -c "import json; print(len(json.load(open('overlay.json'))['Replace']))")
echo "build.sh: overlay maps $N vendored go/*.go files over GOROOT" >&2

OV="-overlay=$PWD/overlay.json"
# NOTE: test/vet are scoped to "." (the gofrontend package). The vendored
# ./go tree is GOROOT's own source; its tests are not ours to run.
case "${1:-build}" in
  build) exec go build $OV -o gofrontend . ;;
  vet)   exec go vet $OV . ;;
  test)  exec go test $OV . ;;
  check)
    echo "== gofmt (excluding vendored tree + generator-owned synth files) ==" >&2
    # *.go matches only this directory, so the vendored ./go/ tree (a
    # subdirectory) is never matched. Generator-owned synth files (the
    # run_gen_synth.py outputs, listed mechanically below) keep the
    # generator's formatting and are pinned by CI's git-diff drift gate,
    # not by gofmt; every other top-level *.go file is gofmt-gated here.
    # (The old gate checked exactly 3 files while claiming the whole-tree
    # scope above. On the old tree 18 synth files were gofmt-dirty: 17
    # hand-maintained sources, now gofmt-formatted, and 1 generator output
    # (synth_compress_bzip2.go), which stays dirty and is excluded here.)
    GEN_FILES="$(grep -o 'synth_[A-Za-z0-9_]*\.go' run_gen_synth.py | sort -u | tr '\n' ' ')"
    DIRTY=""
    for f in *.go; do
      case " $GEN_FILES " in
        *" $f "*) ;;
        *) if [ -n "$(gofmt -l "$f" 2>/dev/null)" ]; then DIRTY="$DIRTY $f"; fi ;;
      esac
    done
    if [ -n "$DIRTY" ]; then
      echo "gofmt: dirty files:$DIRTY" >&2; exit 1
    fi
    echo "== go vet ==" >&2
    go vet $OV .
    echo "== go test ==" >&2
    go test $OV .
    echo "== vendoring diff gate ==" >&2
    diff -r "$GOROOT_DIR/src/go" "$PWD/go" && echo "vendored tree byte-identical to GOROOT/src/go" >&2
    echo "== frontend/v1 wire-format validator ==" >&2
    go build $OV -o gofrontend .
    python3 tools/validate_frontend_v1.py --selftest
    ./gofrontend testdata/straight01.go > "${TMPDIR:-/tmp}/v1doc.json"
    python3 tools/validate_frontend_v1.py "${TMPDIR:-/tmp}/v1doc.json" \
      && echo "frontend/v1 validator: selftest + straight01.go accepted" >&2
    ;;
  *) echo "usage: build.sh [build|vet|test|check]" >&2; exit 1 ;;
esac
