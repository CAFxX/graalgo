#!/bin/sh
# Regenerate the javatest goldens from the current frontend exporter.
#
# Usage: regen-goldens.sh [--check] [--gofrontend PATH]
#   default: write golden_straight01.json, golden_straight02.json and the
#            derived badkind.json, badop.json, badversion.json in this dir.
#   --check: generate to a temp dir and fail (exit 1) on ANY difference
#            from the committed files. The truffle `make javatest` target
#            uses --check so a stale golden fails CI instead of silently
#            testing an old wire format; regeneration itself never happens
#            inside a gate (commit the regenerated files instead).
#
# Provenance: the goldens are the exporter output for testdata/straight01.go
# and testdata/straight02.go (byte-identical to the sibling
# golden_straight01.go / golden_straight02.go in this dir), so the
# package.files field names straight01.go / straight02.go, as committed.
# bad*.json are golden_straight01.json with exactly one mutation each:
#   badkind:    first "vardecl" node kind -> "frobnicate"
#   badop:      first binary "+" op -> "<=>"
#   badversion: "frontend": "v1" -> "v2"
# The mutations are first-occurrence text replacements, so the derived
# files keep the exporter's formatting byte-for-byte apart from the mutation.
set -eu

HERE="$(cd -- "$(dirname -- "$0")" && pwd)"
GOFRONTEND="$HERE/../gofrontend"
CHECK=0
while [ $# -gt 0 ]; do
  case "$1" in
    --check) CHECK=1; shift ;;
    --gofrontend) GOFRONTEND="$2"; shift 2 ;;
    *) echo "usage: regen-goldens.sh [--check] [--gofrontend PATH]" >&2; exit 2 ;;
  esac
done

[ -x "$GOFRONTEND" ] || { echo "regen-goldens.sh: gofrontend not executable: $GOFRONTEND (run make generate first)" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

# Run the exporter from the testdata dir so the files field is the basename.
cd "$HERE/../testdata"
"$GOFRONTEND" straight01.go > "$TMP/golden_straight01.json"
"$GOFRONTEND" straight02.go > "$TMP/golden_straight02.json"

python3 - "$TMP/golden_straight01.json" "$TMP" <<'EOF'
import sys
src, outdir = sys.argv[1], sys.argv[2]
text = open(src).read()
def mutate(name, old, new):
    global text
    assert text.count(old) >= 1, f"{name}: pattern not found: {old!r}"
    open(f"{outdir}/{name}.json", "w").write(text.replace(old, new, 1))
mutate("badkind", '"kind": "vardecl"', '"kind": "frobnicate"')
mutate("badop", '"op": "+"', '"op": "<=>"')
mutate("badversion", '"frontend": "v1"', '"frontend": "v2"')
EOF

if [ "$CHECK" = 1 ]; then
  FAIL=0
  for f in golden_straight01.json golden_straight02.json badkind.json badop.json badversion.json; do
    if ! diff -u "$HERE/$f" "$TMP/$f" > "$TMP/$f.diff" 2>&1; then
      echo "regen-goldens.sh --check: STALE $f (regenerate with make javatest-regen and commit)" >&2
      head -20 "$TMP/$f.diff" >&2
      FAIL=1
    fi
  done
  [ "$FAIL" = 0 ] || exit 1
  echo "regen-goldens.sh --check: goldens match the current exporter"
else
  for f in golden_straight01.json golden_straight02.json badkind.json badop.json badversion.json; do
    cp "$TMP/$f" "$HERE/$f"
  done
  echo "regen-goldens.sh: goldens regenerated in $HERE"
fi
