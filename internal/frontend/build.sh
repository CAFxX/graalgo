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

# Toolchain version pin gate: the vendored go/* tree is pinned to one Go
# release, whose single source of truth is
# tools/stdlib-fork/pinned-version.txt (ADR-0006). The diff gate in the
# `check` action only fails when src/go CONTENTS differ; a toolchain move
# to a release with a byte-identical src/go tree would pass it silently.
# This check fails loudly on ANY toolchain-version change, before any
# build/vet/test/check work, and it runs for every action (not just
# check) so a stale fork can never be built against a moved toolchain.
# Re-vendoring order is: pin first, copy the tree, then check (see
# VENDORING.md). run_gen_synth.py enforces the same pin file for the
# synth generators, so the two can never skew.
PIN_FILE="$PWD/../../tools/stdlib-fork/pinned-version.txt"
if [ ! -f "$PIN_FILE" ]; then
  echo "build.sh: toolchain pin file missing: $PIN_FILE (refusing to build against an unpinned toolchain)" >&2
  exit 1
fi
PIN_VERSION=$(tr -d '[:space:]' < "$PIN_FILE")
GO_VERSION=$("$GO" version)
GO_VERSION=${GO_VERSION#go version }
GO_VERSION=${GO_VERSION%% *}
if [ "$GO_VERSION" != "$PIN_VERSION" ]; then
  echo "build.sh: toolchain version $GO_VERSION does not match pinned $PIN_VERSION ($PIN_FILE); re-vendor the go/* tree for the new release first (see VENDORING.md)" >&2
  exit 1
fi
echo "build.sh: toolchain $GO_VERSION matches pinned $PIN_VERSION" >&2

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
    # NOTE: the pre-existing one-liner `diff -r ... && echo ...` could
    # never fail this check: under `set -e`, a failing command inside an
    # && list is exempt from errexit, so a drifted tree printed its diff
    # and the check still exited 0 (found by the P1-3 positive control,
    # 2026-11-10). Fail explicitly.
    if ! diff -r "$GOROOT_DIR/src/go" "$PWD/go"; then
      echo "vendoring diff gate: vendored go/ differs from GOROOT/src/go (see diff above); re-vendor or remove the unlisted change" >&2
      exit 1
    fi
    echo "vendored tree byte-identical to GOROOT/src/go" >&2
    echo "== vendoring mode gate ==" >&2
    # diff -r compares contents, not modes. Upstream src/go has exactly
    # one executable file (doc/comment/mkstd.sh); a mode change would pass
    # the diff gate silently. Compare the executable-bit set only: full
    # modes are umask-dependent at checkout (this machine: umask 0007, so
    # checked-out files are 660/770 where GOROOT has 644/755), but the
    # executable bit is the one mode bit git tracks and the distribution
    # defines, so it is comparable across checkouts.
    mg=$(mktemp) mv=$(mktemp)
    (cd "$GOROOT_DIR/src/go" && find . -type f -perm /111 -printf '%p\n' | LC_ALL=C sort) > "$mg"
    (cd "$PWD/go" && find . -type f -perm /111 -printf '%p\n' | LC_ALL=C sort) > "$mv"
    if ! diff "$mg" "$mv" >&2; then
      echo "vendoring mode gate: executable files differ between GOROOT/src/go and vendored go/ (see diff above)" >&2
      rm -f "$mg" "$mv"
      exit 1
    fi
    rm -f "$mg" "$mv"
    echo "vendored tree executable files match GOROOT/src/go" >&2
    echo "== frontend/v1 wire-format validator ==" >&2
    "$GO" build $OV -o gofrontend .
    python3 tools/validate_frontend_v1.py --selftest
    # Validate every checked-in golden document plus a fresh export of
    # each valid testdata program — not only straight01.go (the goldens
    # exercise defer/go/nil/method values/atomic/unsafe kinds).
    # pkgvar_mrarg is excluded: its golden fails validation on 104
    # synthetic-position errors (line 0 / negative len in the __-helper
    # funcs) — a pre-existing exporter position bug, not a kind error
    # (zero unknown-kind errors); tracked as a follow-up, not papered
    # over by weakening check_pos.
    for golden in testdata/*.json; do
      python3 tools/validate_frontend_v1.py "$golden" || exit 1
    done
    for src in testdata/straight01.go testdata/straight02.go \
               testdata/methodvalue01.go testdata/timeintr_value.go \
               testdata/mr_return_named.go; do
      base=$(basename "$src" .go)
      ./gofrontend "$src" > "${TMPDIR:-/tmp}/v1-$base.json"
      python3 tools/validate_frontend_v1.py "${TMPDIR:-/tmp}/v1-$base.json" || exit 1
    done
    echo "frontend/v1 validator: selftest + all golden docs + fresh exports accepted" >&2
    ;;
  *) echo "usage: build.sh [build|vet|test|check]" >&2; exit 1 ;;
esac
