# Traceback and caller-frame equivalence with gc

**Status:** 2026-11-18, part of the re-issued panic requirements
GG-0070 (semantics) and GG-0071 (mechanism), both **locked 2026-11-18 on
owner sign-off** ([ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md)).
**Probed against:** gc go1.27.1 (`~/tools/go`), linux/amd64, default
`GOTRACEBACK`.

GG-0070 requires `runtime.Caller`, `runtime.Callers` and
`runtime.CallersFrames` to report what gc reports, and `runtime.Stack`,
`runtime/debug.Stack`/`PrintStack` and the fatal-panic traceback to match
gc's "after normalization". graalgo replaces gc's runtime, so the frames of
gc's own runtime functions (their files and lines under `$GOROOT/src/runtime`)
cannot be reproduced honestly. This document fixes exactly what is compared,
what is normalized, and why. The differential harness's normalizer
([normalize.go](../../test/differential/internal/harness/normalize.go)) is
the implementation; the rules below that it does not implement yet are
marked *(to implement)*.

## 1. What gc reports (probe)

The probe program called `runtime.Callers` + `CallersFrames` inside deferred
calls on each path, and printed `runtime.Stack` and fatal tracebacks. Results
(function:line, innermost first; `frames` is the helper that calls
`runtime.Callers`):

| Situation | gc go1.27.1 frames |
|---|---|
| open-coded defer, normal exit | `frames` · `f.func1` · `f` (closing-brace line) · `main.main` · `runtime.main` · `runtime.goexit` |
| chained defer (defer in a loop), normal exit | identical shape: `f.func1` · `f` (closing-brace line) · `main.main` · `runtime.main` · `runtime.goexit` — no `runtime.deferreturn` frame |
| open-coded defer run by a panic | `f.func1` · **`runtime.gopanic`** (`runtime/panic.go:859`) · `f` (the `panic` line) · `main.main` · `runtime.main` · `runtime.goexit` |
| chained defer run by a panic | identical to the open-coded case |
| defer run by `runtime.Goexit` in a goroutine | `f.func1` · **`runtime.Goexit`** (`runtime/panic.go:694`) · `f` (the `Goexit` line) · `runtime.goexit` |
| `runtime.Caller(1)` in a deferred call run by a panic | `runtime/panic.go`, line 859 — a runtime frame |

Fatal tracebacks (default `GOTRACEBACK=single`) print no runtime frame
except a mid-stack `runtime.gopanic`, which gc prints as
`panic({0x55fd70?, 0x4a7a88?})` followed by `\t$GOROOT/src/runtime/panic.go:859 +0x125`
(only when a deferred call that is itself panicking or calling
`runtime.Stack` is on the stack: a plain uncaught panic, open-coded or
chained, prints only the user frames). Nested panics print
`panic: first [recovered]` / `\tpanic: second`; `runtime.Stack` from a
deferred call prints the same frame format as the fatal traceback. Exit
status of an uncaught panic is 2.

Two consequences:

1. **Open-coding is unobservable.** At normal exit both paths report the
   same frames; on the panic path both insert `runtime.gopanic`. The
   open-coded limit (GG-0071) is therefore a performance parameter, not a
   compatibility one.
2. **Runtime frames are observable** (`runtime.Caller(1)` inside a deferred
   call returns `runtime/panic.go`), so they cannot simply be dropped: the
   frame must be there, with the right function name, at the right
   position.

## 2. Caller frames: `runtime.Caller`, `Callers`, `CallersFrames`

Definitions:

- A **runtime frame** is a frame whose function belongs to package
  `runtime` or to a package under `internal/runtime/`. Every other frame —
  user code, the standard library, `reflect` — is a **non-runtime frame**.
- The **boundary runtime frames** are the four runtime frames gc reports at
  panic, `Goexit` and goroutine boundaries: `runtime.gopanic` (between a
  deferred call run by a panic and the panicking frame), `runtime.Goexit`
  (between a deferred call run by `Goexit` and the frame that called it),
  `runtime.main` (below `main.main` on the main goroutine) and
  `runtime.goexit` (the bottom of every goroutine).

Rules, applied to the sequence of `Frame`s (equivalently to the
`(pc → file, line, function)` answers of `runtime.Caller(skip)`):

| # | Rule | Applies to |
|---|---|---|
| C1 | `Function`, `File` and `Line` compared **exactly** | every non-runtime frame |
| C2 | `Function` compared exactly; `File` and `Line` **normalized** to `runtime` / `0` (graalgo's runtime is not gc's source) | the four boundary runtime frames |
| C3 | **dropped** from both sequences before comparison | every other runtime frame |
| C4 | `Entry`, `PC` and `Func` pointer values are not compared | every frame |

Positions matter: with C3 applied, the i-th frame of graalgo's sequence must
be the i-th frame of gc's. A program that indexes `runtime.Caller(skip)`
across a frame C3 drops would observe a difference; no such case is known
in std or the corpus, and one found is a divergence to fix, not a rule to
add. File paths of standard-library frames are subject to rule T4 below.

## 3. Tracebacks: fatal panics, `runtime.Stack`, `runtime/debug.Stack`

The harness's global rules, unchanged (each normalizes a value that differs
between two runs of the *same* gc binary):

| # | Rule | Example |
|---|---|---|
| T1 | goroutine ids → `#` | `goroutine 18 [running]:`, `created by main.main in goroutine 1` |
| T2 | trailing PC offset of a location line removed | `\t/src/main.go:12 +0x1d` → `\t/src/main.go:12` |
| T3 | hex words inside a frame line's argument list, register annotations (`fp=`/`sp=`/`pc=`) and `[signal …]` lines → `0xADDR` | `main.f(0xc000014260, 0x1)`, `panic({0x55fd70?, 0x4a7a88?})` |

Rules added for the runtime boundary *(to implement)*:

| # | Rule | Why |
|---|---|---|
| T4 | the source root of a standard-library location line (gc's `$GOROOT/src/`, graalgo's stdlib root) is replaced by the token `$GOROOT/src/` on both sides; the package path, file name and line are compared exactly | graalgo's std sources may live in its fork, not under gc's GOROOT; a stdlib patch that shifts lines of a frame a traceback can show is a divergence |
| T5 | the location line under a boundary runtime frame (printed as `panic(…)` for `runtime.gopanic`, and as `runtime.Goexit()`, `runtime.main()`, `runtime.goexit()` where the traceback level prints them) → `\truntime:0` | C2 for tracebacks |
| T6 | the wait duration in a goroutine header (`[chan receive, 2 minutes]`) is removed | scheduling time, not program output |

Not normalized (a difference is a real divergence): the panic message lines
(`panic: …`, `[recovered]`, nested `\tpanic: …`, `fatal error: …`), the
order and names of the printed frames, user and std file names and line
numbers, goroutine status words, `created by` lines, `exit status 2`, and
which frames the traceback level elides (at `single`/`all`, every runtime
frame except a mid-stack `runtime.gopanic` printed as `panic(…)`).

Traceback levels: the obligation holds for `GOTRACEBACK=none`, `single`
(default) and `all`, and the same levels set through
`runtime/debug.SetTraceback`. At `system` and `crash` gc prints every
runtime frame with its registers; graalgo has different runtime frames by
construction, so at those levels only the non-runtime frames (C1, in order)
and the message lines are compared. This scoping is part of what the owner
signs off with GG-0070.

## 4. Verification of the normalizer itself

Every rule above is a place a real bug could be normalized away, so each
has a negative test in the harness: a seeded wrong function name, file or
line of a non-runtime frame; a missing or extra `runtime.gopanic` frame; a
wrong `[recovered]` marker; a wrong exit status — each must fail the
comparison after normalization (the harness's "no normalized-away bugs"
gate). Changing a rule changes the normalizer id
([normalizer_id.go](../../test/differential/internal/harness/normalizer_id.go)),
which re-baselines snapshots deliberately, never silently.
