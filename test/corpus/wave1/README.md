# P3-M7 wave-1 differential corpus

Go programs exercising the wave-1 synthetic stdlib packages (strings,
bytes, strconv, unicode/utf8, path, path/filepath, errors, io, sort —
see `phases/phase3/hidden_files/m7-CHECKLIST.md`), differential vs stock
gc 1.27.1. Run via the shared runner `run_wave.sh`, not by hand.

## Layout

- One subdir per package, named exactly like the Go import path's last
  element: `strings/`, `encoding/json/`, `sort/`, ...
- Programs: `<nn>_<name>.go` with `nn = 00, 01, ...` (two digits,
  zero-padded, ascending coverage order). `package main`, `func main`.
- Per-package `run_differential.sh`: a thin wrapper that just calls
  `../run_wave.sh <pkg>` (template below). Do NOT copy the harness logic.
- `_smoke/` is infrastructure: two trivial programs proving the harness
  runs end to end. Never treat it as a package corpus; `run_wave.sh`
  with no argument skips all `_`-prefixed dirs (they run only when named
  explicitly).

## Program rules (adversarial, non-tautological)

1. **Deterministic output only.** gc and graalgo run at different
   wall-clock times, on different threads, under different schedulers.
   Never print measured durations, pointers, map iteration order,
   goroutine ids, or anything else that varies run to run.
2. **Timing-sensitive programs print fixed strings only** (cf. the
   timectx convention: "elapsed" not "23ms"). Mark such a program with
   an empty `<nn>_<name>.timing` file next to it — the runner then runs
   it with `gmp=1` only (the multi-gmp leg adds scheduling noise for
   sleeps); every other program runs with `gmp=1` and `gmp=4`.
3. **Adversarial + combination tests.** Empty inputs, unicode, huge and
   degenerate values, malformed inputs, conflicting directives,
   cross-feature intersections (3+ features in one program where the
   package admits it). The corpus must be capable of failing — a program
   that passes under any implementation is not a test.
4. **Panic programs** get a `<base>.panic` file (same base name as the
   program, `.panic` suffix) containing gc's expected first stderr line,
   e.g. `panic: strings: ...`. The runner compares graalgo's first
   (WARNING-filtered) stderr line against the LIVE gc leg's first line —
   never the anchor alone — and the `.panic` file additionally pins gc's
   own message, so a toolchain message change fails loudly instead of
   silently pinning stale text. Non-panic programs get their full
   filtered stderr compared against the live gc leg.
5. Real-Go synthetic sources only: programs may import only packages the
   wave-1 importer supports; anything else is a scope error by design.
6. **Scope-limited programs** get a `<nn>_<name>.scopelimited` file (same
   base name as the program) with two lines:
   `reason: <why it can never pass the frontend>` and
   `retest: <trigger that would un-block it, e.g. WS D / P3-M8 / unicode>`.
   The runner does not run these under graalgo and counts them separately
   (neither passing nor failing) — but it still builds and runs the gc
   leg, so a program that is not even valid Go surfaces as FAIL, not
   scope-limited. A `.scopelimited` file without a `retest:` line is a
   harness FAIL. The rare program with no working gc leg at all
   (dual-mode xml: real std lacks `xml.ReadResult`, synthetic-ref panics
   in the Token desugar-stub) adds a third line,
   `nogc: <why no gc leg can run>`, and the runner skips the gc check.
   Never mark a program scope-limited to hide a corpus bug
   or a frontend bug that has an owner: the reason must name an honest
   scope boundary and the re-test trigger must name the blocker.
7. **Synthetic-reference programs** get an empty `<nn>_<name>.synthetic`
   file (a comment inside may document why). These programs use
   M7-extension constructors (`csv.NewReaderString`,
   `gob.NewBufferEncoder`, ...) absent from real std, so the gc leg
   cannot build them against GOROOT — a GOPATH cannot shadow the
   standard library. For these, the runner builds the gc leg against
   the extracted phase-1 synthetic sources (`extract_synth_ref.py`;
   imports rewritten to a temp GOPATH), i.e. the reference behavior is
   the synthetic source executed by stock gc.

## The runner

`run_wave.sh [pkg]` — builds the phase-3 java module once, then for each
`<nn>_<name>.go` runs it under stock gc (built binary, run directly —
`go run` masks the exit code) and under `P3SyncMain`, and byte-compares
stdout, exit codes, and (for panic programs) the first stderr line.
Known JVM Unsafe `WARNING:` lines on stderr are filtered (harness noise,
cf. `p3frontend/DESIGN.md` §7). Modeled exactly on
`corpus/timectx/run_differential.sh` (same phases, same comparisons);
only the program discovery (per-package dirs, `[0-9][0-9]_*.go` glob,
`.timing` marker) is wave-1 specific.

Per-package thin wrapper (`<pkg>/run_differential.sh`):

```bash
#!/bin/bash
# Differential corpus for the wave-1 `strings` package — thin wrapper
# over the shared wave-1 runner (conventions: corpus/wave1/README.md).
exec "$(dirname "$0")/../run_wave.sh" strings
```

## Tmp discipline (mandatory for all wave-1 workers)

`/tmp` is a 512 MB tmpfs shared with other projects' LIVE work. It is
not scratch space:

- **NEVER** write to `/tmp` for any reason.
- **NEVER** clean `/tmp` (or any part of it) — other agents' live work
  lives there.

Every wave-1 worker must export, for **every** `go`/`java` command
(ad-hoc builds included — the phase Makefiles already route the test
temp dirs to `out/test-tmp`, this covers the rest; `run_wave.sh` sets
these itself to the wave-1 dirs):

```bash
export TMPDIR=$HOME/workspace/graalgo-m7-int/phases/phase3/hidden_files/m7-tmp/<pkg>
export GOCACHE=$HOME/workspace/.gocache-m7-<pkg>
```

`run_wave.sh` creates its per-run scratch with `mktemp -d` (which honors
`TMPDIR`) and removes it on exit, so setting `TMPDIR` before invoking the
runner is sufficient. Each package gets its own `m7-tmp/<pkg>` and its
own `.gocache-m7-<pkg>`; never share a scratch dir with another worker.
