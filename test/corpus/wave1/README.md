# P3-M7 wave-1 differential corpus

Go programs exercising the wave-1 synthetic stdlib packages (strings,
bytes, strconv, unicode/utf8, path, path/filepath, errors, io, sort —
see `phases/phase3/hidden_files/m7-CHECKLIST.md`), differential vs stock
gc 1.27.1. Run by `make stdlib-gate` on the new path (m3-plan D3; see
[../README.md](../README.md)).

## Layout

- One subdir per package, named exactly like the Go import path's last
  element: `strings/`, `encoding/json/`, `sort/`, ...
- Programs: `<nn>_<name>.go` with `nn = 00, 01, ...` (two digits,
  zero-padded, ascending coverage order). `package main`, `func main`.
- `_smoke/` is infrastructure: two trivial programs proving the harness
  runs end to end. Never treat it as a package corpus.

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
   program, `.panic` suffix) containing the expected first stderr line,
   e.g. `panic: strings: ...`. The runner byte-compares graalgo's first
   (WARNING-filtered) stderr line against it.
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

`make stdlib-gate` (m3-plan D3, which deleted v1's `run_wave.sh` and the
per-package `run_differential.sh` wrappers): every program here runs on the
new path against gc, judged against `test/corpus/std-register.tsv`.
