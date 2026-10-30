# Behavior snapshots (M10 WS I)

Behavior snapshots record graalgo's **observable behavior** on the
differential corpus ([../../test/differential/corpus/](../../test/differential/corpus/))
against a pinned Go toolchain — the **upgrade baseline** (PHASES.md M10).
A future toolchain upgrade or refactor diffs against these files to detect
behavior drift.

**Backend:** the snapshots are recorded against the **product launcher
P3SyncMain** (`graalgo` backend in
[`test/differential/backends-p3.json`](../../test/differential/backends-p3.json)),
*not* the phase-1 `graalgo-run` in
[`test/differential/backends.json`](../../test/differential/backends.json).
Recording against the phase-1 frontend would enshrine its build failures
(no channel/scheduler support) as the baseline and leave P3SyncMain
regressions invisible to the per-push gate. The phase-1 config is retained
for the typeparam and srctest adapters (layer 2), which test the frontend;
it is no longer the snapshot backend.

## Layout

- `go<VERSION>.json` — one snapshot file per Go toolchain, e.g.
  [`go1.27.1.json`](go1.27.1.json). On a toolchain upgrade, record the new
  file and diff it against the old one: the diff *is* the upgrade's
  behavior delta.
- `README.md` — this file.

**Path note:** the phase-3 plan suggested
`phases/phase3/hidden_files/snapshots/` as the evidence location; these live
at `test/snapshots/` instead — snapshots are a first-class versioned
deliverable (committed, CI-gated), not internal bookkeeping.

## Format (`graalgo-behavior-snapshot/v1`)

Top-level metadata:

| Field | Meaning |
|---|---|
| `format` | `"graalgo-behavior-snapshot/v1"` — bump on any format or normalization-rule change; old files then fail the check loudly instead of drifting silently |
| `toolchain` | Go toolchain the snapshot was recorded against, e.g. `"go1.27.1"` |
| `backend` | Backend name from [`backends-p3.json`](../../test/differential/backends-p3.json) (`"graalgo"` → P3SyncMain) |
| `graalgoCommit` | graalgo git commit the snapshot was recorded from |
| `recordedAt` | RFC3339 UTC timestamp |
| `corpus` | Repo-relative corpus label (e.g. `"test/differential/corpus"`) |
| `normalizer` | Normalization rule-set id (`"harness.Normalize+backend.stderrFilters/v1"`) |
| `excluded` | Config exclusion list at record time (coverage is part of the baseline). Each entry carries `program`, `reason`, `owner` (component area owning retirement of the exclusion), and `retriage` (the trigger forcing a re-probe). `snapshot check` treats any change to any of those four fields as exclusion-list drift, and prints the excluded count and program names alongside the drift count, so "0 drifted" never reads as "all programs green" |
| `programs` | Per-program entries, sorted by name |

Per-program entry:

| Field | Meaning |
|---|---|
| `name` | Corpus program directory, e.g. `"01_hello"` |
| `args` | Program argv (the current corpus uses none; the field is the extension point) |
| `stdin` | Program stdin bytes (none today; extension point) |
| `buildOk` | Whether the backend's build step succeeded |
| `buildLog` | Normalized build log (a program that fails to build has observable behavior too — the failure is the baseline) |
| `exitCode` | Process exit code (`-1` on timeout or harness run error) |
| `timedOut` | Whether the per-program timeout fired |
| `stdout` | Normalized stdout bytes |
| `stderr` | Normalized stderr bytes |

### What is normalized away (not semantic observables)

- **Timings** — run durations are never recorded.
- **Machine paths** — the corpus root is scrubbed to `{CORPUS}`, the
  launcher path to `{GRAALGO_RUN}` (the harness already scrubs its work
  root to `{WORK}`), so a snapshot recorded on one checkout verifies on
  another.
- **Addresses / goroutine ids / `.go` line numbers** —
  [`harness.Normalize`](../../test/differential/internal/harness/normalize.go).
- **Backend stderr noise** — the backend's `stderrFilters` from
  [`backends.json`](../../test/differential/backends.json) (e.g. the Truffle
  `sun.misc.Unsafe` warnings).

Recorded **exactly**: message text, exit codes, timeout flags,
build-failure logs, panic messages.

### Recording guarantees

`snapshot record` runs every program **twice** (self-consistency gate): any
nondeterminism fails the recording instead of entering the baseline. A
program whose output is nondeterministic by design belongs on the config's
exclusion list with a reason code — never in the baseline.

## Regenerating

```bash
make snapshot        # re-record test/snapshots/go<VERSION>.json
```

Run this **only** for a toolchain upgrade or a deliberately re-baselined
behavior change, then commit the result. It requires a built tree
(`build` is a prerequisite) and takes a few minutes (each corpus program
runs twice under a fresh JVM).

## Checking

```bash
make snapshot-check  # fail on any drift from the committed baseline
```

`snapshot-check` re-runs each program once and fails on any field drift,
on corpus programs missing from the snapshot, on snapshot entries missing
from the corpus, and on exclusion-list drift. It is part of `make all`,
so per-push CI ([`ci.yml`](../../.github/workflows/ci.yml)) runs it:
**a refactor that changes observable behavior fails loudly at the commit
that caused it.** Per-release is the wrong cadence for the check — the
baseline exists precisely so refactors (per-push) can be diffed against it;
per-release would let drift accumulate unattributably. Re-*recording* is
the per-release (or deliberate-change) operation.
