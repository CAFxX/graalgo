# P0-fmt lane — red-green evidence (updated)

Worktree: ~/workspace/worktrees/graalgo/p0-fmt, branch wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/p0-fmt, base m9 @ 4170c21.
Build: incremental javac of changed phase1 files.

## Status by bug (verified via phase1 graalgo-run vs gc 1.27.1)

- bug-016 (recovered panic formatting): FIXED by c64adda — byte-identical.
- bug-018 (%p on nil): FIXED by 284e2f0 — byte-identical.
- bug-022 (%#v anonymous structs): FIXED by e33c87f + 68b580e (embedded named types) — byte-identical.
- bug-015 (Stringer %v): NOT FIXABLE in phase1 — requires method dispatch via phase3 IfaceMethodTable (FmtHandleMethods). Works in phase3 P3SyncMain path. Architectural limitation, not a bug in the fmt implementation.
- bug-021 (Stringer ptr/%s/%q): SAME as bug-015 — phase3-only.

## Why bug-015/021 are phase3-only

Phase1's GoTypeDescriptor carries only name/kind (no methods). Method implkeys in the type table are for interface satisfaction checks, not dispatch. Actual String() invocation needs Truffle call targets built by phase3's IfaceMethodTable. Fmt.java explicitly documents: "The handler is installed by the phase-3 launcher (which has the method tables); null in phase-1-only mode."

## Post-fix verification (phase1, vs gc 1.27.1)
- bug-016, bug-018, bug-022 repros: BYTE-IDENTICAL stdout, rc=0.
- Extended matrix (embedded/named/anonymous structs, nil %p matrix, panic verbs): byte-identical except by-design address erasure (0x0).
