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
#   debug gate.
#
# What this script guarantees about grep errors (fix-up 2026-11-02, lane H
# VETO P1): this is /bin/sh, which has no pipefail, so NO gate decision is
# ever taken from a pipeline's exit status. Every grep stage writes to a
# temp file and the script branches EXPLICITLY on that stage's exit code:
# for a match gate, 0 = match found -> FAIL, 1 = no match -> clean, and
# any other code (2 = grep error, e.g. an invalid BRE exclusion or a
# missing src/ tree) -> exit 2 loudly with the grep diagnostic on stderr.
# For an exclusion-filter stage, 0/1 both mean "stage ran cleanly" (lines
# remain / all lines filtered) and the gate continues; any other code
# exits 2 loudly. A grep error therefore can never again pass a gate
# vacuously, and it can never silently disable just one exclusion: the
# whole gate fails. Grep diagnostics are printed to stderr, never
# redirected away. Exclusion patterns are additionally validated against
# /dev/null before the match stage runs, so a typo in an exclusion fails
# the gate even in a tree with no println lines at all (otherwise the
# typo would stay latent until a println appeared).
# Exit 0 iff both gates pass; prints the offending lines on failure.
set -eu

SRC="${1:?usage: check-gates.sh <src-dir> [--debug-exclude PATTERN ...]}"
shift
EXF="$(mktemp)"
EXFF="$(mktemp)"
PH="$(mktemp)"
DBG="$(mktemp)"
DBG1="$(mktemp)"
trap 'rm -f "$EXF" "$EXFF" "$PH" "$DBG" "$DBG1"' EXIT INT TERM
while [ $# -gt 0 ]; do
  case "$1" in
    --debug-exclude) printf '%s\n' "$2" >> "$EXF"; shift 2 ;;
    --debug-exclude-fixed) printf '%s\n' "$2" >> "$EXFF"; shift 2 ;;
    *) echo "usage: check-gates.sh <src-dir> [--debug-exclude PATTERN ...] [--debug-exclude-fixed LITERAL ...]" >&2; exit 2 ;;
  esac
done

# fail_on_grep_error <stage-name> <exit-code>: any code other than the
# caller-handled 0/1 aborts the gate loudly (exit 2). Callers branch on
# 0/1 themselves; this helper is only reached for anything else.
fail_on_grep_error() {
  echo "check-gates.sh: $1: grep failed (exit $2) — gate aborted, not passed" >&2
  exit 2
}

echo "== placeholder grep gate =="
rc=0
grep -rn --include='*.java' -E 'TODO|FIXME|XXX|panic\("not implemented"\)|throw new UnsupportedOperationException\("stub' "$SRC" > "$PH" || rc=$?
case "$rc" in
  0) cat "$PH"; echo "PLACEHOLDER GATE FAILED" >&2; exit 1 ;;
  1) : ;; # no match: clean
  *) fail_on_grep_error "placeholder gate" "$rc" ;;
esac
echo "placeholder gate clean"

echo "== debug-leftover grep gate (src/main only) =="
# Stage 0: validate both exclusion files even when the match stage finds
# nothing (otherwise a typo in an exclusion would stay latent in a clean
# tree and only surface once a println appears). /dev/null input means
# 0/1 are both "patterns compiled fine"; anything else is a grep error.
for _stage in BRE fixed-string; do
  case "$_stage" in
    BRE) rc=0; grep -v -f "$EXF" /dev/null > /dev/null || rc=$? ;;
    *) rc=0; grep -v -F -f "$EXFF" /dev/null > /dev/null || rc=$? ;;
  esac
  case "$rc" in
    0|1) : ;;
    *) echo "check-gates.sh: debug gate ($_stage exclusion validation): grep failed (exit $rc) — gate aborted, not passed" >&2; exit 2 ;;
  esac
done
# NOTE: stage 0 discards only the probe's stdout (there is none: the input
# is /dev/null); its stderr passes through, so the grep diagnostic itself
# is printed alongside the abort message. No grep error is ever silenced
# into a pass.
# Stage 1: the match grep. 0 = println lines found (carry on to filters),
# 1 = none found (clean, filters need not run), other = error -> exit 2.
rc=0
grep -rn --include='*.java' -E 'System\.(out|err)\.println' "$SRC/main" > "$DBG" || rc=$?
case "$rc" in
  0) : ;;
  1) echo "debug-leftover gate clean"; exit 0 ;;
  *) fail_on_grep_error "debug gate (match stage)" "$rc" ;;
esac
# Stage 2: BRE exclusion filter. 0/1 = ran cleanly (lines remain / all
# filtered); other = error (e.g. invalid BRE in --debug-exclude) -> exit 2.
# An empty exclusion file filters nothing; that is correct, not an error.
rc=0
grep -v -f "$EXF" "$DBG" > "$DBG1" || rc=$?
case "$rc" in
  0|1) : ;;
  *) fail_on_grep_error "debug gate (BRE exclusion stage)" "$rc" ;;
esac
# Stage 3: fixed-string exclusion filter. Same exit-code contract as
# stage 2; the final result lives in $DBG.
rc=0
grep -v -F -f "$EXFF" "$DBG1" > "$DBG" || rc=$?
case "$rc" in
  0|1) : ;;
  *) fail_on_grep_error "debug gate (fixed-string exclusion stage)" "$rc" ;;
esac
# Final decision on the filtered output: nonempty = leftover println
# lines survived all exclusions -> FAIL; empty = clean.
if [ -s "$DBG" ]; then
  cat "$DBG"
  echo "DEBUG-LEFTOVER GATE FAILED" >&2; exit 1
fi
echo "debug-leftover gate clean"
