# wordfreq demo (P3-M7 end-to-end acceptance)

A small but realistic text-processing CLI run as a **real program** through
the graalgo launcher (`graalgo.p3sync.launcher.P3SyncMain`) — not a synthetic
corpus program. Acceptance: stdout byte-identical to stock gc 1.27.1.

`wordfreq.go` reads deterministic embedded CSV-ish input (rows
`term,doc,count,score`), normalizes terms case-insensitively, aggregates
counts and scores, computes length/score statistics, prints a text summary,
and emits a JSON report. Malformed rows are skipped with a count.

## Imports (wave-1 subset + fmt)

`strings`, `bytes`, `strconv`, `sort`, `math`, `encoding/json`, `fmt`.

Deliberate scope-avoidance (frontend/v1 + M7 scope):
- `sort.Slice` needs reflect (fail-fast) -> custom `byCount`
  `sort.Interface` (the `sort/08_custom_iface.go` pattern), no funclits.
- Only whitelisted 2-target calls (`strconv.Atoi`/`ParseFloat` via the
  mrdesugar table, `json.MarshalIndent`); the text summary is assembled
  with single-result `bytes.Join` since `bytes.Buffer.Write*` is
  multi-result (fail-fast).
- No package-level vars, no `*` deref expressions, no pointer-receiver
  calls on addressable values (WS D), no multi-result declarations.

## Running

Repo-relative, gitignored; never under /tmp (512M tmpfs) or the deleted
phases/ tree (repo AGENTS.md §5 no-debris):

```bash
GR=$(git rev-parse --show-toplevel)   # this repo's root
INTEROP=$GR/internal/backend/interop
RUNTIME=$GR/internal/backend/runtime
TRUFFLE=$GR/internal/backend/truffle
TMPDIR=$GR/.tmp/wave1/demo
JAVA=$HOME/tools/graalvm-jdk-25/bin/java
CP="$INTEROP/out/classes:$RUNTIME/out/classes:$TRUFFLE/out/classes:$(ls $TRUFFLE/lib/*.jar | tr '\n' ':')"
# gc baseline (wordfreq.expected is checked in):
go build -o $TMPDIR/wf.gc wordfreq.go && $TMPDIR/wf.gc > wordfreq.expected
# graalgo leg (same invocation pattern as ../run_wave.sh):
"$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m -Djava.io.tmpdir="$TMPDIR" -cp "$CP" \
    graalgo.p3sync.launcher.P3SyncMain --gmp=1 wordfreq.go
```

## Status (2026-10-07, worker E)

- gc 1.27.1 leg: PASS — `wordfreq.expected` captured from a built binary,
  exit 0, deterministic output.
- graalgo leg: **BLOCKED on C2's in-flight "global inits" fix.**
  The synthetic `encoding/json` package's `var jsonPow2str = jsonBuildPow2()`
  (and dependents `jsonOverThresh`/`jsonUnderThresh`) never takes effect at
  runtime: every program importing `encoding/json` panics at package init
  with `index out of range [970] with length 0` in `jsonDecimalFromParts`.
  All 6 corpus `encoding/json` programs fail identically; main-package
  global inits work fine (verified with a dedicated repro). A
  dependency-ordering fix for synthetic globals is in flight in the working
  tree (`synth_machinery.go`: `orderVarInits` over synthetic `varInits`,
  comment cites exactly this json case) and the `gofrontend` binary already
  includes it — the panic persists, so the fix is incomplete.
  Re-run the graalgo leg when C2's fix lands.

## Rework log

1. `map[string]*termStat` + `*stats[t]` deref -> `*ast.StarExpr` is a WS D
   scope error in frontend/v1. Reworked to `map[string]termStat` value
   structs with comma-ok lookup and read-modify-write (`stats[term] = st`).
   gc baseline regenerated after the rework; output unchanged in shape
   (verified field-by-field against the pre-rework run).
