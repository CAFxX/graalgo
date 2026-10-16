# P3-M8 wave-2 differential corpus

Go programs exercising the wave-2 synthetic stdlib packages (io, os,
syscall, reflect, unsafe, crypto/* — see
`phases/phase3/hidden_files/m8-analysis/`), differential vs stock gc
1.27.1. Run via the shared runner `run_wave.sh`, not by hand.

Pipelining note: wave 2 is built in parallel with the P3-M7 remediation
on a separate tree (`~/workspace/graalgo-m8-pipe`, branch `m8-pipeline`).
The runner resolves its own repository root from its path — it works from
any checkout. Corpus programs and synthetic-source drafts are new files;
shared-file integration (importer registration, desugars) lands at rebase
time after M7 remediation completes.

## Layout

- One subdir per package, named exactly like the Go import path's last
  element: `io/`, `os/`, `syscall/`, `reflect/`, `unsafe/`,
  `crypto/md5/`, ... (nested for crypto subpackages).
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
   program, `.panic` suffix) containing the expected first stderr line,
   e.g. `panic: reflect: ...`. The runner byte-compares graalgo's first
   (WARNING-filtered) stderr line against it.
4b. **Intentionally-divergent programs** get an empty
   `<base>.divergent` file. These are programs where graalgo
   INTENTIONALLY fail-fasts and gc does not (e.g. `unsafe.Add` traps
   under graalgo per D5/GG-0015, computes under gc). The runner skips
   the exit-code comparison for them, but stdout must still match and
   the `.panic` pin still applies. A `.divergent` marker WITHOUT a
   `.panic` file is a harness FAIL — divergence must always be a
   pinned fail-fast, never silent. The program source must document
   the divergence and its design justification.
5. Real-Go synthetic sources only: programs may import only packages the
   wave-2 importer supports; anything else is a scope error by design.
6. **Scope-limited programs** get a `<nn>_<name>.scopelimited` file (same
   base name as the program) with two lines:
   `reason: <why it can never pass the frontend>` and
   `retest: <trigger that would un-block it, e.g. WS D / P3-M9 / cgo>`.
   The runner does not run these under graalgo and counts them separately
   (neither passing nor failing) — but it still builds and runs the gc
   leg, so a program that is not even valid Go surfaces as FAIL, not
   scope-limited. A `.scopelimited` file without a `retest:` line is a
   harness FAIL. Never mark a program scope-limited to hide a corpus bug
   or a frontend bug that has an owner: the reason must name an honest
   scope boundary and the re-test trigger must name the blocker.
7. **Synthetic-reference programs** get an empty `<nn>_<name>.synthetic`
   file. These use M8-extension APIs absent from real std, so the gc leg
   cannot build them against GOROOT. The runner builds the gc leg against
   the extracted synthetic sources (`extract_synth_ref.py`) with imports
   rewritten to a temp GOPATH — the reference behavior is the synthetic
   source executed by stock gc.
7. **Hermetic file-system programs** (os, syscall): create all files
   under a fresh temp dir obtained from `os.MkdirTemp("", ...)`, clean
   up with `defer os.RemoveAll`, and never depend on the repository
   layout, the user's HOME contents, or any ambient state.

## The runner

`run_wave.sh [pkg]` — builds the phase-3 java module once, then for each
`<nn>_<name>.go` runs it under stock gc (built binary, run directly —
`go run` masks the exit code) and under `P3SyncMain`, and byte-compares
stdout, exit codes, and (for panic programs) the first stderr line.
Known JVM Unsafe `WARNING:` lines on stderr are filtered (harness noise,
cf. `p3frontend/DESIGN.md` §7). Modeled exactly on
`corpus/wave1/run_wave.sh` (same phases, same comparisons), except the
repo root resolves from the script's own path instead of a hardcoded
default.

Per-package thin wrapper (`<pkg>/run_differential.sh`):

```bash
#!/bin/bash
# Differential corpus for the wave-2 `io` package — thin wrapper
# over the shared wave-2 runner (conventions: corpus/wave2/README.md).
exec "$(dirname "$0")/../run_wave.sh" io
```

## Tmp discipline (mandatory for all wave-2 workers)

`/tmp` is a 512 MB tmpfs shared with other projects' LIVE work. It is
not scratch space:

- **NEVER** write to `/tmp` for any reason.
- **NEVER** clean `/tmp` (or any part of it) — other agents' live work
  lives there.

Every wave-2 worker must export, for **every** `go`/`java` command
(ad-hoc builds included — the phase Makefiles already route the test
temp dirs to `out/test-tmp`, this covers the rest; `run_wave.sh` sets
these itself to the wave-2 dirs):

```bash
export TMPDIR=$HOME/workspace/.tmp-graalgo-wave2
export GOCACHE=$HOME/workspace/.gocache-m8-<pkg>
```

`run_wave.sh` creates its per-run scratch with `mktemp -d` (which honors
`TMPDIR`) and removes it on exit, so setting `TMPDIR` before invoking the
runner is sufficient (the runner defaults `TMPDIR` to
`$HOME/workspace/.tmp-graalgo-wave2` when unset). Each package gets its
own `.gocache-m8-<pkg>`; never share a scratch dir with another worker.
