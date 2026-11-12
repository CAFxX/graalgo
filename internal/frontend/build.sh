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

# Honor GO when the caller sets it (root Makefile passes GO="$(GO)";
# default is bare `go` on PATH, as before).
GO="${GO:-go}"
case "$GO" in
  */*) GOFMT="$(dirname "$GO")/gofmt" ;;
  *) GOFMT="gofmt" ;;
esac

GOROOT_DIR="$("$GO" env GOROOT)"
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
  build) exec "$GO" build $OV -o gofrontend . ;;
  vet)   exec "$GO" vet $OV . ;;
  test)  exec "$GO" test $OV . ;;
  check)
    echo "== gofmt (excluding vendored tree) ==" >&2
    if "$GOFMT" -l main.go export.go export_test.go | grep .; then
      echo "gofmt: dirty files above" >&2; exit 1
    fi
    echo "== go vet ==" >&2
    "$GO" vet $OV .
    echo "== go test ==" >&2
    "$GO" test $OV .
    echo "== vendoring diff gate ==" >&2
    diff -r "$GOROOT_DIR/src/go" "$PWD/go" && echo "vendored tree byte-identical to GOROOT/src/go" >&2
    echo "== frontend/v1 wire-format validator ==" >&2
    "$GO" build $OV -o gofrontend .
    python3 tools/validate_frontend_v1.py --selftest
    # Validate every checked-in golden document plus a fresh export of
    # each valid testdata program — not only straight01.go (the goldens
    # exercise defer/go/nil/method values/atomic/unsafe kinds).
    for golden in testdata/*.json; do
      python3 tools/validate_frontend_v1.py "$golden" || exit 1
    done
    # Compressed goldens (uncompressed export over the GitHub API blob
    # limit) validate the same way, decompressed to a temp file — the
    # *.json glob above never matches them.
    for gz in testdata/*.json.gz; do
      [ -f "$gz" ] || continue
      gzbase=$(basename "$gz" .json.gz)
      gunzip -c "$gz" > "${TMPDIR:-/tmp}/v1-$gzbase.json"
      python3 tools/validate_frontend_v1.py "${TMPDIR:-/tmp}/v1-$gzbase.json" || exit 1
    done
    for src in testdata/straight01.go testdata/straight02.go \
               testdata/methodvalue01.go testdata/timeintr_value.go \
               testdata/mr_return_named.go testdata/pkgvar_mrarg.go; do
      base=$(basename "$src" .go)
      ./gofrontend "$src" > "${TMPDIR:-/tmp}/v1-$base.json"
      python3 tools/validate_frontend_v1.py "${TMPDIR:-/tmp}/v1-$base.json" || exit 1
    done
    echo "frontend/v1 validator: selftest + all golden docs + fresh exports accepted" >&2
    ;;
  *) echo "usage: build.sh [build|vet|test|check]" >&2; exit 1 ;;
esac
