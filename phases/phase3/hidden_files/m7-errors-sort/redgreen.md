# P3-M7 errors+sort — red-green evidence

Non-tautological evidence: each item shows a FAIL produced by a real
defect (or a real scope boundary), then the PASS after the fix. Test
commands and the exact failure signatures are recorded so a reviewer can
reproduce.

## 1. Synthetic source bugs caught before commit

### 1a. `reverseRange`: `j++` instead of `j--`

Transcription slip while porting gc's `zsortinterface.go`: the
decrement loop incremented `j`, so `reverseRange` never terminated its
loop correctly (infinite loop / wrong reversal for the decreasingHint
path). Caught by re-reading the port against gc line-by-line before the
first export test; fixed to `j--`. The differential corpus covers it:
`so06_adversarial_shapes.go` reverse-sorts 100 elements (the
decreasingHint path calls `reverseRange`), and `so02_reverse.go`
exercises `Reverse` — both byte-compare against gc. A port without the
fix cannot pass them.

### 1b. `Search` methods: receiver/parameter shadowing

First draft named the receiver `x` in
`func (x IntSlice) Search(x int) int { return SearchInts(x, x) }` — the
parameter shadows the receiver, a type error (`int` as `[]int`). Caught
by the gc type-check of the extracted synthetic source (the
`srccheck` probe: parse const → `go/types` check); renamed receivers to
`p` per gc. The p3frontend drift test (`TestSortSyntheticSignatures`)
re-checks the method signatures against the phase-1 source, so a
regression fails fast there too.

### 1c. v1 scope rejections found by the export test (all red, then green)

`TestSyntheticErrorsSortM7` (phase-1 export test) went red four times,
once per v1 scope rule the first draft violated — each is a REAL
rejection the exporter produces, not a tautology:

1. **Variadic packing**: `errors.Join(err1, nil, err2)` →
   `gofrontend: call to variadic function without ... spread (argument
   packing) is not supported in frontend/v1 scope (WS D)`. Fixed: the
   test (and all corpus programs) use explicit `...` spread. Carried v1
   limitation, documented in both corpus READMEs.
2. **`*p` assignment**: `*swaps++` / `*r ^=` →
   `gofrontend: assignment to *ast.StarExpr is not supported in
   frontend/v1 scope (WS D)`. Fixed: swap count threaded by value
   through the `sortOrder2Result`/`sortMedianResult` carriers; the
   xorshift step inlined into `breakPatterns` (deterministic,
   length-seeded, as in gc).
3. **`&x` on identifiers**: `&sc` →
   `gofrontend: & on *ast.Ident is not supported in frontend/v1 scope
   (WS D)`. Fixed by the same by-value threading (no address-taking).
4. **Function-local `const`**: `const maxInsertion = 12` etc. →
   `gofrontend: only var/type declarations are supported in frontend/v1
   scope (WS G/H)`. Fixed: vars with a `// NB` comment at each site.

After the four fixes the test is green, and every synthetic funcdecl
(`New`/`Is`/`Unwrap`/`Join`, `Sort`/`Stable`/`Search`/…,
`__sortPartition`, `sortMedian`, …) appears in the wire doc while the
multi-result `Find` and generic `AsType` are correctly absent (skipped
at export, WS D).

### 1d. `sortOrder2` swap-count threading (behavioral, not just scope)

The by-value rewrite of the `*int` swap counter is the riskiest port
edit (it changes how `choosePivot`'s hint is computed). Evidence it is
faithful: `so06_adversarial_shapes.go` drives the hint paths
(sorted → `increasingHint` → `partialInsertionSort`; reverse →
`decreasingHint` → `reverseRange`; all-equal → `partitionEqual`) and
`so07_large.go` sorts 5000 shuffled ints — all byte-identical to gc's
pdqsort. A wrong hint or count would change tie-breaking/partitioning
and fail the byte comparison.

## 2. Runtime limitation found by the differential corpus (red → green)

`errors/04_unwrap.go` initially failed under graalgo (exit 1) while gc
exited 0:

```
graalgo-run: fatal internal error in goroutine: java.lang.NullPointerException:
Cannot invoke "graalgo.values.GoInterface.isNil()" because "<parameter1>" is null
```

Minimal reproducer (`nil1.go`): `errors.Unwrap(nilErr)` with a nil
`error` argument. Root cause: the P3 runtime represents a nil interface
argument as Java null; `TypeAssertNode` casts to `GoInterface` and calls
`gi.isNil()` on the null → NPE. (Interface `==` in `BinaryOpNode`
handles Java null, so `errors.Is` with nil args was unaffected.)

Fix (behavior-preserving, in the synthetic source): explicit nil guards
in `Unwrap` and at the top of the `errorsIs` loop (the latter covers nil
entries from a user `Unwrap() []error`). gc returns nil/false on those
paths without ever asserting, so the guards change nothing observable
under gc; under graalgo they avoid the NPE. After the fix the full
errors corpus is green (14/14), including the adversarial `er03`
(uncomparable target/err — the `recover`-based comparability probe).

Re-test trigger: if the runtime ever makes type assertions nil-safe
(`TypeAssertNode` null check), the guards become redundant but remain
correct — no action needed.

## 3. Stale-binary trap found during validation (red → green)

While validating the `Unwrap` nil guard, the corpus kept failing with
the pre-guard NPE even though the source was fixed. Root cause:
`phases/phase1/Makefile`'s `GOFRONTEND_SRC` listed only
`main.go export.go build.sh go.mod` — changes to `synth_*.go` or
`synth_machinery.go` never triggered a `gofrontend` rebuild, so the
corpus silently tested a stale binary (timestamps confirmed:
binary 02:27, source 02:32). Additionally, the phase-3 make treats
phase-1 as frozen via the `out/classes/.built` marker and never
re-runs the phase-1 make at all.

Fix: `GOFRONTEND_SRC := $(wildcard frontend/*.go) frontend/build.sh
frontend/go.mod` (with a comment explaining why the explicit list is a
trap). After the fix + a manual `./build.sh build`, the guard took
effect and the corpus went green. This was a real silent-wrong-behavior
trap, not a test artifact: any future `synth_*.go` change would have hit
it.

## 4. Shared-tree contamination caught by the worktree rule

Mid-task, two other wave-1 workers' in-flight files broke the shared
`phase1/frontend` build (`synth_strings.go`: unterminated raw string;
`synth_enc_csv_gob.go`: syntax errors), and the strings worker's
uncommitted `synth_machinery.go`/`export.go` hunks were swept into a
`git diff`-generated patch. Per the AGENTS.md lesson, all validation
moved to an isolated worktree (pristine HEAD + only my files, verified
by `git diff --stat` showing exactly my 4 modified files and no foreign
hunks). The main-tree commits below were assembled from the worktree
state, hunk-by-hunk.

## 5. Carried divergences (fail-fast, never silent) + re-test triggers

| Member | Status | Re-test trigger |
|---|---|---|
| `errors.As`, `errors.AsType` | exact gc 1.27.1 signatures, panic bodies (need `reflect`) | reflect lands in v1 synthetic scope |
| `sort.Find` | exact gc signature, panic body; declaration skipped at export, call sites fail with named M7 scope error (multi-result, WS D) | WS D lands multi-result calls |
| `sort.Slice`, `sort.SliceStable`, `sort.SliceIsSorted` | exact gc signatures, panic bodies (need `reflect`) | reflect lands |
| `errors.Join` call shape | v1 requires explicit `...` spread (no variadic packing, WS D) | WS D lands argument packing |
| synthetic `reverse` | explicit delegation instead of embedding `Interface` (unobservable via public API) | none (documented) |
| `errors.Is` comparability | probed via `recover` (no reflect); `er03` proves gc-parity | reflect lands (then use `reflectlite`-equivalent) |

No member is silently omitted: the p3frontend drift tests
(`TestErrorsSyntheticSignatures`, `TestSortSyntheticSignatures`)
compare the mirror's full exported scope (names + rendered types,
including the generic `AsType`) against the phase-1 source, and fail on
any extra or missing name.

## 6. Suite results (final tree)

- phase-1 `go test .`: green (includes the new
  `TestSyntheticErrorsSortM7` + `TestSyntheticSortFindFailsFast`).
- p3frontend `go test ./...`: green (includes 8 new drift/importer
  tests).
- Differential corpus: errors 14/14 (7 programs × gmp=1,4), sort 20/20
  (10 programs × gmp=1,4) — byte-identical stdout, exit codes.
