# Behavior snapshots (SPEC §5.9, GG-0036)

A behavior snapshot records **gc's** (the reference toolchain's) observable
behavior on the differential corpus
([../../test/differential/corpus/](../../test/differential/corpus/)).
graalgo is checked against it on every push: **graalgo must match gc**.
graalgo's own output is never the baseline (format v1 recorded graalgo's
output, which baked every graalgo bug into the "expected" behavior).

Expected divergences are not hidden in the snapshot: they are explicit,
reviewed, reason-coded entries in
[`corpus-exclusions.yaml`](../../test/differential/corpus-exclusions.yaml)
(SPEC §5.6 vocabulary: `backend-specific` / `unspecified-behavior` /
`deferred-parity`). Listed programs still run; one that starts matching gc
fails the check as STALE until its entry is removed.

## Layout

- `go<VERSION>.json` — one snapshot per Go toolchain, e.g.
  [`go1.27.1.json`](go1.27.1.json). On a toolchain upgrade, record the new
  file and diff it against the old one: drift in gc's own snapshot is an
  upstream semantic change to triage.
- `README.md` — this file.

## Format (`graalgo-behavior-snapshot/v2`)

| Field | Meaning |
|---|---|
| `format` | `"graalgo-behavior-snapshot/v2"`; any other value fails the check loudly |
| `toolchain` | Reference toolchain, e.g. `"go1.27.1"` |
| `reference` | Reference backend name in [`backends-newpath.json`](../../test/differential/backends-newpath.json) (`"gc-a"`) |
| `recordedAt` | RFC3339 UTC timestamp |
| `corpus` | Repo-relative corpus label |
| `normalizer` | Normalization rule-set id; a change fails the check until re-recorded |
| `programs` | Per-program entries, sorted by name |

Per-program entry: `name`, `sourceSha256` (content address of `main.go`:
an edited, added or removed program without a re-record fails the check),
`buildOk`, `buildLog`, `exitCode` (`-1` on timeout), `timedOut`, `stdout`,
`stderr`. Output that is not valid UTF-8 is stored base64-encoded in
`stdoutBase64` / `stderrBase64` (a JSON string would silently replace the
invalid bytes with U+FFFD).

### Normalization

Only genuinely nondeterministic text is normalized
([`normalize.go`](../../test/differential/internal/harness/normalize.go)):
goroutine ids, trace PC offsets, register annotations, hex inside trace
frame argument lists, signal lines. Source line numbers in traces, computed
`0x…` values, message text, exit codes and timeouts are compared exactly.
Stdout is exact unless a program opts in through a `NORMALIZE` file (e.g.
`20_fmt_formats` prints `%p`: rule `pointers`). Machine paths (the corpus
root, the launcher path) are scrubbed to `{CORPUS}` / `{GRAALGO_RUN}`. JVM
noise on graalgo's stderr is dropped by the harness-owned `jvm` noise set
(the launchers also run with `-XX:-UsePerfData`).

## Recording

```bash
make snapshot        # re-record test/snapshots/go<VERSION>.json under gc
```

Every program runs three times under gc (self-consistency gate: any
nondeterminism fails the recording). Re-record when a corpus program is
added or edited, and on a toolchain upgrade; commit the result. No graalgo
is involved.

## Checking

```bash
make snapshot-check                                    # whole corpus
make snapshot-check SNAPSHOT_PROGRAMS=49_int_arith_edges,50_shifts
```

Verdicts per program: MATCH; DRIFT (unlisted divergence: fail); EXPECTED
(listed divergence); STALE (listed but matching gc: fail); INFRA (no
verdict — e.g. a corrupted frontend export — after retries: exit 3);
REF-DRIFT (gc itself no longer matches its snapshot: fail). It runs per
push via `make all`.
