#!/bin/sh
# Shared placeholder + debug-leftover grep gates for the backend modules
# (review 2026-11-02, P2-28: the three backend Makefiles copy-pasted these
# blocks, and drift had already begun — the runtime module's extra gates
# stay in its own Makefile; only the two identical blocks live here).
#
# Usage: check-gates.sh <src-dir> [--debug-exclude <BRE pattern> ...]
#                            [--debug-exclude-fixed <literal> ...]
#   <src-dir> is the module's src/ tree (contains main/ and test/).
#   Each --debug-exclude is a grep -v BRE pattern for the debug gate
#   (one per legitimately chatty file/idiom; the rationale for each
#   exclusion stays in the calling Makefile's comment).
#   Each --debug-exclude-fixed is a grep -v -F literal string, for idioms
#   whose text is not a valid BRE (the interop fatal-error idiom: its
#   parenthesis would open an unbalanced BRE group if escaped as \(). With
#   no exclusions, every System.out/err.println in src/main fails the
#   debug gate. NOTE: an invalid -f pattern makes grep fail the pipeline
#   vacuously (exit 2, no lines checked) — never silence grep errors here.
# Exit 0 iff both gates pass; prints the offending lines on failure.
set -eu

SRC="${1:?usage: check-gates.sh <src-dir> [--debug-exclude PATTERN ...]}"
shift
EXF="$(mktemp)"
EXFF="$(mktemp)"
trap 'rm -f "$EXF" "$EXFF"' EXIT INT TERM
while [ $# -gt 0 ]; do
  case "$1" in
    --debug-exclude) printf '%s\n' "$2" >> "$EXF"; shift 2 ;;
    --debug-exclude-fixed) printf '%s\n' "$2" >> "$EXFF"; shift 2 ;;
    *) echo "usage: check-gates.sh <src-dir> [--debug-exclude PATTERN ...] [--debug-exclude-fixed LITERAL ...]" >&2; exit 2 ;;
  esac
done

echo "== placeholder grep gate =="
if grep -rn --include='*.java' -E 'TODO|FIXME|XXX|panic\("not implemented"\)|throw new UnsupportedOperationException\("stub' "$SRC"; then
  echo "PLACEHOLDER GATE FAILED" >&2; exit 1
fi
echo "placeholder gate clean"

echo "== debug-leftover grep gate (src/main only) =="
if grep -rn --include='*.java' -E 'System\.(out|err)\.println' "$SRC/main" | grep -v -f "$EXF" | grep -v -F -f "$EXFF"; then
  echo "DEBUG-LEFTOVER GATE FAILED" >&2; exit 1
fi
echo "debug-leftover gate clean"
