# P0-fmt lane — red-green evidence

Worktree: ~/workspace/worktrees/graalgo/p0-fmt, branch wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/p0-fmt, base m9 @ 4170c21.
Build: incremental javac of changed phase1 files.

## Status by bug

### bug-016 (recovered panic formatting): FIXED in phase1
- Commit: c64adda
- Verification (phase1 graalgo-run vs gc 1.27.1): BYTE-IDENTICAL, rc=0

### bug-018 (%p on nil): FIXED in phase1
- Commit: 284e2f0
- Verification (phase1 graalgo-run vs gc 1.27.1): BYTE-IDENTICAL, rc=0

### bug-022 (%#v anonymous structs): FIXED in phase1
- Commits: e33c87f + 68b580e (embedded named types: main.Inner not struct{X:1})
- Verification (phase1 graalgo-run vs gc 1.27.1): BYTE-IDENTICAL, rc=0
- Extended matrix (embedded/named/anonymous/tagged structs): byte-identical except by-design address erasure (0x0)

### bug-015 (Stringer %v): ALREADY FIXED in phase3
- Root cause: phase1 has no method tables; Stringer dispatch requires phase3's IfaceMethodTable via FmtHandleMethods (installed by P3SyncMain).
- Verification (phase3 P3SyncMain vs gc 1.27.1):
  ```
  $ cd phases/phase3
  $ ~/tools/go/bin/go run <bug-015>/repro.go > /tmp/gc-015.out
  $ make run ARGS="--gmp=1 <bug-015>/repro.go" 2>/dev/null | grep -v "^/home/hatch/tools" > /tmp/p3-015.out
  $ diff /tmp/gc-015.out /tmp/p3-015.out && echo PASS
  PASS
  ```
  Output (both): `main.Boom boom(7)` / `boom(7)`, rc=0 — BYTE-IDENTICAL.

### bug-021 (Stringer ptr/%s/%q): ALREADY FIXED in phase3
- Same root cause as bug-015 (method dispatch is phase3-only by architecture).
- Verification (phase3 P3SyncMain vs gc 1.27.1):
  ```
  $ cd phases/phase3
  $ ~/tools/go/bin/go run <bug-021>/repro.go > /tmp/gc-021.out
  $ make run ARGS="--gmp=1 <bug-021>/repro.go" 2>/dev/null | grep -v "^/home/hatch/tools" > /tmp/p3-021.out
  $ diff /tmp/gc-021.out /tmp/p3-021.out && echo PASS
  PASS
  ```
  Output (both): `P!|P!|"P!"` / `V:x|"V:x"` / `{2}`, rc=0 — BYTE-IDENTICAL.

## Why bug-015/021 are phase3-only

Phase1's GoTypeDescriptor carries only name/kind (no methods). Method implkeys in the type table are for interface satisfaction checks, not dispatch. Actual String() invocation needs Truffle call targets built by phase3's IfaceMethodTable. Fmt.java documents: "The handler is installed by the phase-3 launcher (which has the method tables); null in phase-1-only mode." This is an architectural boundary, not a bug.

## Regression
- Phase1 `make test`: FmtVerbMatrixTest and related suites (run in background, results pending).
- Check gates: placeholder + debug-println greps clean on touched files.
