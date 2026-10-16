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
#
# GO (default: "go" on PATH) selects the go binary, like the top-level
# Makefile (GO ?= $(HOME)/tools/go/bin/go, passed through at every
# invocation). It MUST report go1.27.1 — the VENDORING.md pin: a bare `go`
# on PATH can resolve to a different GOROOT and overlay the vendored go/*
# tree onto the wrong toolchain (a Frankenstein build no later gate
# catches), and with GOTOOLCHAIN=auto an older go could even try a network
# toolchain download. The script fails fast on any other version.
set -eu
cd "$(dirname "$0")"

GO="${GO:-go}"
if ! GO_VERSION="$("$GO" version 2>/dev/null)"; then
  echo "build.sh: refusing to run: cannot execute '$GO version'" >&2
  exit 1
fi
case "$GO_VERSION" in
  *" go1.27.1 "*) ;;
  *)
    echo "build.sh: refusing to run: need go1.27.1 (VENDORING.md pin), got: $GO_VERSION" >&2
    exit 1
    ;;
esac

# gofmt from the pinned toolchain when available, PATH fallback otherwise.
GOFMT="$(dirname -- "$GO")/gofmt"
[ -x "$GOFMT" ] || GOFMT="gofmt"

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
    echo "== gofmt (all non-vendored, non-generated .go files) ==" >&2
    # Excluded: the vendored ./go tree (upstream Go, not ours to format) and
    # generator outputs — files carrying the standard `// Code generated`
    # marker, plus GEN_OUTS below, which mirrors GENERATED_SYNTH in the
    # top-level Makefile (keep in sync; formatting those is the generators'
    # job). A drift between the two lists fails loudly here, not silently.
    GEN_OUTS=" synth_archive_tar.go synth_archive_zip.go synth_mime.go synth_mime_multipart.go synth_mime_quotedprintable.go synth_bufio.go synth_compress_bzip2.go synth_compress_flate.go synth_compress_gzip.go synth_compress_lzw.go synth_compress_zlib.go synth_mathbits.go synth_mathcmplx.go synth_mathrand.go synth_mathrandv2.go synth_path.go synth_regexpsyntax.go synth_textscanner.go "
    if find . -path ./go -prune -o -name '*.go' -print | while IFS= read -r f; do
      case "$GEN_OUTS" in *" $(basename "$f") "*) continue ;; esac
      head -1 "$f" | grep -q '^// Code generated' 2>/dev/null && continue
      printf '%s\n' "$f"
    done | tr '\n' '\0' | xargs -0 "$GOFMT" -l 2>/dev/null | grep .; then
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
    ./gofrontend testdata/straight01.go > "${TMPDIR:-/tmp}/v1doc.json"
    python3 tools/validate_frontend_v1.py "${TMPDIR:-/tmp}/v1doc.json" \
      && echo "frontend/v1 validator: selftest + straight01.go accepted" >&2
    ;;
  *) echo "usage: build.sh [build|vet|test|check]" >&2; exit 1 ;;
esac
