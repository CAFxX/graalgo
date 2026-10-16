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
    echo "== gofmt (excluding vendored tree) ==" >&2
    if gofmt -l main.go export.go export_test.go | grep .; then
      echo "gofmt: dirty files above" >&2; exit 1
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
    # R2-P2-19: validate EVERY testdata fixture, not just straight01.go — a
    # wire-format regression that only manifests with multi-result calls,
    # generics, or channel ops must fail here. bad_*.go fixtures are error
    # cases (TestGolden skips them too); everything else must export and
    # validate.
    V1DOCS=""
    for f in testdata/*.go; do
      case "$f" in testdata/bad_*) continue;; esac
      b=$(basename "$f" .go)
      ./gofrontend "$f" > "${TMPDIR:-/tmp}/v1doc-$b.json"
      V1DOCS="$V1DOCS ${TMPDIR:-/tmp}/v1doc-$b.json"
    done
    # shellcheck disable=SC2086 (V1DOCS is a deliberate word-split file list)
    python3 tools/validate_frontend_v1.py $V1DOCS \
      && echo "frontend/v1 validator: selftest + all testdata fixtures accepted" >&2
    ;;
  *) echo "usage: build.sh [build|vet|test|check]" >&2; exit 1 ;;
esac
