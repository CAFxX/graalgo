# P0-fmt lane — red-green evidence

Worktree: ~/workspace/worktrees/graalgo/p0-fmt, branch wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/p0-fmt, base m9 @ 4170c21.
Build: incremental javac of changed phase1 files (clean, -Werror).

## Pre-existing state (reproduced at 4170c21 before fixes)
- bug-015 (Stringer %v): ALREADY FIXED by P3-M9 RC-6 FmtHandleMethods — repro byte-identical to gc.
- bug-021 (Stringer ptr/%s/%q): ALREADY FIXED — repro byte-identical to gc.
- bug-016: BROKEN — ClassCastException GoErrorValue→GoInterface, rc=1.
- bug-018: BROKEN — `%!p(<nil>)` vs gc `0x0`.
- bug-022: BROKEN — `struct{...}` vs gc `struct { A int }`.

## Fixes (3 commits)
1. e33c87f bug-022: GoTypeStrings renders anonymous structs structurally (`struct { A int; B string "tag" }`,
   embedded as bare type, Go-quoted tags); GoQuote.quote made public.
2. 284e2f0 bug-018: printArg null branch distinguishes nil interface from typed nils by static kind
   (%p on nil ptr/chan/func/map/slice → 0x0); fmtPointer %p unwraps interfaces to dynamic kind.
3. c64adda bug-016: GoErrorValue carries gc panic-type identity (typeName/goSyntax/structForm);
   GoErrors factories model runtime/error.go (string kinds vs struct, value-based signed flag);
   Fmt formats recovered panics per gc verb rules; no more blind GoInterface.narrow() cast.

## Post-fix verification (all vs gc 1.27.1, ~/tools/go/bin/go)
- All 5 bug repros: BYTE-IDENTICAL stdout, rc=0 (diff against bug dirs' gc.out).
- Extended matrices byte-identical: panicverbs (T/v/s/q/p/#v/x on boundsError), signedprobe
  (const/int/uint/neg index), slicesign (slice bounds hi/neg).
- panicnil: identical except %p (gc real address vs graalgo erased 0x0 — standing policy).
- panicverbs2 typeassert: gc prints nondeterministic addresses (verified across runs);
  graalgo prints deterministic fallbacks (quoted Error() text, 0x0) — principled.
- Targeted regression program (anonymous struct %T/%#v incl. tags/embedded/empty/nested,
  full %p/%v/%T nil matrix, errors): byte-identical except erased address (by design).
- Check gates: placeholder + debug-println greps clean on touched files.
