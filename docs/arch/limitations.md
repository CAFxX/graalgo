# Known limitations (Phase 1)

Implementation limits that are fail-fast (clean error, no silent wrong
result), with re-test triggers for the phase that owns the fix.

## Stack depth (M10 self-attack)

Guest-language recursion burns host (JVM) stack: each Go call frame costs
several JVM frames through Truffle dispatch. With the pinned `-Xss32m`
(M5 stance), measured 2026-10-05: depths 1k/5k/10k/15k run clean (exit 0);
20k/30k/100k report `graalgo-run: stack overflow` on stderr, exit 2
(log: `hidden_files/m10_selfattack/DEPTH_LIMIT.log`). No JVM trace leaks.

gc grows goroutine stacks dynamically, so very deep recursion that gc
accepts is rejected here. This is an interpreter-architecture limit, not
a semantic divergence: no program produces a wrong result, it fails fast.

- Re-test trigger: Phase 4 (JIT) owns the stack strategy (explicit stack
  / segmented stacks / deeper host stack). Re-measure the depth limit
  when the execution model changes.
- Regression: `MainTest.testDeepRecursionReportsCleanStackOverflow`
  (`src/test/java/graalgo/launcher/MainTest.java`) — generates a 100k-deep
  recursion program in a temp dir, asserts exit 2 and a clean
  `stack overflow` stderr line (no JVM trace).

## bufio.Scanner SplitFunc values (L13, review 2026-10-30)

`Scanner.Scan` dispatches only the default split: a nil `SplitFunc`
means `ScanLines`, served by its `__`-helper. Any non-nil `SplitFunc`
panics at the first `Scan` with
`bufio.Scanner: SplitFunc value is not supported in frontend/v1 scope (WS D)`.
This covers custom split functions AND explicitly passed builtins
(`ScanLines`, `ScanWords`, `ScanBytes`, `ScanRunes`): passing
`ScanWords` is not equivalent to the default here, because the frontend
cannot call through a 3-result function value held in a struct field
(WS D). The failure is fail-fast and named, never a silent wrong token
stream.

- Re-test trigger: WS D lands multi-result function-value calls; then
  `Scanner.Scan` dispatches `s.split` and the corpus program below
  un-scope-limits.
- Corpus: `test/corpus/wave3/bufio/01_scanner.go` (+
  `01_scanner.scopelimited`, reason updated 2026-10-30 to this entry).

## hash/maphash values are NOT gc-identical (permanent scoped divergence, L13)

The synthetic `hash/maphash` implements the full API over its own
pure-Go seeded 64-bit hash (splitmix64 finalizer), because gc funnels
every input through `runtime.memhash` (a `//go:linkname` into the Go
runtime, AES-based on amd64), which no pure-Go synthetic can reproduce
bit-exactly. For a fixed seed the values are deterministic within a
graalgo process, but they are NOT equal to gc's values for the same
seed. Scope of the divergence: any program comparing `maphash` values
across implementations, or depending on gc's seeded values, gets
different answers. Within one process the documented properties hold
and are pinned by the corpus: chunking invariance, Write/WriteByte/
WriteString equivalence, seed sensitivity, Reset/Clone semantics,
Bytes/String/Hash consistency, zero-Seed panics
(`test/corpus/wave3/hash/maphash/`).

Numeric values cannot be pinned from user code: `Seed` is opaque and
`MakeSeed` is random, so no program can name a fixed seed. The pin is
therefore at algorithm identity: `TestSyntheticMaphashValueDivergencePinned`
(`internal/frontend/synth_hash_maphash_test.go`) fails loudly if the
splitmix64 finalizer constants or the explicit divergence note in
`internal/frontend/synth_hash_maphash.go` change — a silent algorithm
swap, or a softened divergence note, is a test failure, not drift.

- Re-test trigger: a runtime `memhash` equivalent lands (corpus README
  wording: "memhash intrinsic lands"). Until then this entry is
  permanent and explicitly scoped, not a TODO.
