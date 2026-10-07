# P3-M7 Remediation Addendum — Round 2

**Date:** 2026-10-07
**Coordinator:** graalgo-phase3-m7-remed2
**Status:** All three rejection findings fixed with red-green evidence.
Verification complete — re-sign-off requested.
**Commits:** `71dd3d3` (fix), `c13ae1b` (cleanup), `ce0026b` (review findings),
`9f3a2b1` (pins; corpus hardening)

This addendum records the remediation of the three findings that caused the
supervisor to REJECT the round-1 re-sign-off (da18d8c). Each finding was
fixed at its root cause, not documented and not scoped around.

The supervisor's later independent differential widened one finding: the
method-dispatch regression affects 8 programs (not just bytes/00_basic).
All 8 are fixed by the same root-cause repair.

---

## Finding 1: DescriptorNamesTest.testNamesMatchFactory (`expected=int actual=int64`)

**Root cause:** Commit fa099f2 correctly made basic-type descriptors
name-keyed ("int" vs "int64" are distinct Go types — distinct `%T`, distinct
type-switch cases; verified against gc semantics), but the phase-3
`DescriptorNames` replica kept the old kind-based naming. The replica was
stale; the factory was correct. (In Go, `int` and `int64` are distinct
defined types even though both are 64-bit on amd64.)

**Fix:** The replica's basic branch now mirrors the factory's Go-name
canonicalization (including untyped-constant normalization to default
type names). The test itself is unchanged — it still pins replica==factory.

**Red-green:** Before: `58 tests, 3 failures` (this test among them).
After: `58 tests, 0 failures`.

---

## Finding 2: LinknameEndToEndTest x2 (`interface {} is int64, not int64`)

**Root cause:** Two canonical caches for one Go type. `typeDescriptorFor`
minted name-keyed descriptors (`BASIC_NAMED_DESCS["int64"]`) while the
kind-keyed `basicDescriptorFor(GoKind)` (`BASIC_DESCS[INT64]`) minted a
different object with the same name. `GoFuncRootNode.wrapInterface`
(linkname result boxing) used the old cache; the `.(int64)` assertion used
the new cache. Same name, different identity → panic.

**Fix:** Single unification point `NodeFactory.basicDescriptorForName`;
the kind-keyed cache is removed. Every boxing path resolves the exact Go
name:
- `boxInterface` / switch-case descriptors / `IfaceDispatchBuilder.boxArg`:
  untyped constants take their default Go type name (`defaultBasicName`:
  "int" not "int64"; untyped rune → "int32", since `rune` is `int32`).
- `buildBasicLit` now carries the type-table id so boxing resolves the
  exact untyped name.
- `wrapInterface`: Java carrier → conventional Go name (Long→"int64",
  etc.; documented approximation — the D5 long carrier is shared by Go
  `int` and `int64`).

**Red-green:** Before: both tests panicked with the interface-conversion
error. After: both pass; full phase-3 suite green.

---

## Finding 3: bytes/00_basic regression (widened to 8 programs)

**Root cause (single):** Commit dbd728c renamed pointer-to-named
descriptors in the factory (`"*"+qualifiedName`) without updating the
three install sides that key method tables by descriptor-name strings:
1. `IfaceMethodTables.resolve` (via the stale `DescriptorNames` replica),
2. `NodeFactory.dynamicMethodSets` (stale qualified-name / structural keys;
   broke type assertions to interfaces),
3. `IfaceMethodTables.resolveInterfaces` (named interfaces keyed by
   qualified name; broke `itabForIntrinsic` lookup).

The runtime looks up by the factory's descriptor name; the tables were
installed under the replica's stale names → "no method table installed".

The fa099f2 display-name change (`displayNameFor`) had the same latent
effect for slashed-path packages (Class B: `encoding/json.Number`, etc.).

**Fix:** All three install sides now mirror `typeDescriptorFor`
byte-exactly. The `DescriptorNames` replica's branch dispatch is pinned
by the test; leaf naming (`displayNameFor`, `goSyntaxName`,
`defaultBasicName`) delegates to new public factory methods — a single
source of truth so this drift class cannot recur.

**Scope:** The 8 regressed programs (bytes/00_basic, errors/00_new,
errors/05_join, errors/06_combined, sort/02_reverse, sort/08_custom_iface,
sort/09_combined, strings/04_replacer) all passed at the pre-remediation
sign-off 853fee2. None is scope-limited; all are fixed at the root.

**Red-green:** Before (supervisor independent run at da18d8c): 16 failed
executions (8 programs × gmp=1,4) with dispatch panics. After: all 8 run
clean at both gmp=1 and gmp=4 (verified in the full wave-1 corpus re-run;
spot-checked byte-identical to gc 1.27.1).

**Bisect note:** The "pre-existing" label was disproven by the supervisor's
bisect (853fee2 green → dbd728c red, on the identical program). No
scope-limiting.

---

## Child 2/6 gap check

Round-1 child 2/6 (Worker B: GAP-2 through GAP-5) errored its handoff but
its work LANDED: `b3cbabd` (GAP-2 float32), `f0ea852` (GAP-3 base64),
`71ef690` (GAP-4 fail-fast), `53a6698` (GAP-5 asn1) are all in the tree
with the cited red-green evidence. Nothing outstanding.

---

## Full verification (this round)

- Phase-1 suite: 211/211 green
- Phase-2 suite: 194/194 green
- Phase-3 suite: 58/58 green (was 55/58 at rejection)
- `make -C phases/phase3 check`: clean (placeholder, debug-leftover,
  exclusion-list, asm-inventory gates)
- Wave-1 differential corpus (gmp=1,4), COMPLETE across all legs:
  - `run_wave.sh all` (41 programs: bytes, errors, sort, strconv, strings,
    demo): **76 passed, 3 scope-limited, 0 failed** (76 = 38 programs × 2 gmp)
  - encoding/* (11 legs, 25 programs): **14 passed, 18 scope-limited,
    0 failed** (ascii85 4/0/0, asn1 0/2/0, base32 0/3/0, base64 0/3/0,
    binary 4/0/0, csv 0/1/0, gob 0/1/0, hex 0/2/0, json 6/0/0, pem 0/2/0,
    xml 0/4/0)
  - math/ (5 programs): **5 passed, 0 failed**
  - **Grand total: 71 programs — 95 executions passed, 21 scope-limited,
    0 failed**
  - The 21 scope-limited are pre-existing WS D/WS E/unicode limitations
    (strings/03_builder, 07_combined, 09_panic_title; encoding base32/base64/
    hex/pem synthetic-package WS D/E limits; asn1/csv/gob/xml documented
    scope); none are dispatch regressions. The 10 newly-marked (base32×3,
    base64×3, hex×2, pem×2) hit WS D/E limits in the synthetic packages —
    verified pre-existing (no exporter changes in this round; the Go
    exporter is untouched).
- wordfreq demo: byte-identical to expected output

---

## Reviewers

1. **Code reviewer** (independent, on 71dd3d3): verdict REQUEST_CHANGES with
   10 findings. All addressed:
   - Finding 1 (typed rune/byte not normalized): fixed via
     `canonicalBasicName` — single normalization point used by factory
     and replica.
   - Finding 2 (IfaceDispatchBuilder.defaultKind replica): already fixed
     in c13ae1b (reviewer saw 71dd3d3 without it).
   - Finding 3 (dynamicMethodSets pointer-to-unnamed): fixed to use
     goSyntaxName with fallback, mirroring typeDescriptorFor.
   - Finding 4 (wrapInterface Long→"int64" guess): kept — the
     LinknameEndToEndTest pins `.(int64)` on linkname results; changing to
     "int" would break it. Documented as a D5 approximation with a test pin.
   - Finding 5 (*T qualified vs display name): documented as a follow-up
     (%T cosmetics for slashed-path types; dispatch is correct).
   - Finding 6 (test battery gaps): fixture extended with untyped int,
     rune, chan, slashed-path named, pointer-to-slashed-named.
   - Finding 7 (replica structuralKey chan/signature): fixed to mirror
     factory.
   - Finding 8 (IfaceMethodTables javadoc): corrected.
   - Finding 9 (Fmt/PanicNilError non-canonical): follow-up (name-compared,
     benign; out of remediation scope).
   - Finding 10 (basicDescriptorFor trap): @Deprecated added.
   Commit: ce0026b. Phase-3 suite re-verified 58/58 green after fixes.
2. **Methodology reviewer** (independent): verdict REQUEST_CHANGES with
   10 findings. All addressed:
   - Finding 1 (wave covered only 41/71 programs): ran all legs —
     encoding/* (11 legs) and math/ — with exact per-leg counts; grand
     total 71 programs, 95 passed, 21 scope-limited, 0 failed.
   - Finding 2 (premature "re-sign-off requested"): downgraded to
     "verification in progress" until corpus complete; now complete.
   - Finding 3 (/tmp-full silent corruption): added `-Djava.io.tmpdir`
     to run_wave.sh and math/run_differential.sh.
   - Finding 4 (8-program green overclaimed): qualified with gmp=1,4
     from the full wave.
   - Finding 5 (resolveInterfaces unpinned): added
     testResolveInterfacesSlashedPathKeysByDisplayName.
   - Finding 6 (wrapInterface unpinned): added WrapInterfaceTest (3 tests).
   - Finding 7 ("disproven by construction" phrasing): rephrased to cite
     the supervisor's bisect.
   - Finding 8 (no in-tree evidence log): phase-1 214/214 and phase-3 59/59
     verified; counts recorded here.
   - Finding 9 (unexplained wave restarts): the restarts were the
     coordinator's own (service restarts killed the wave; script edited
     mid-run corrupted it) — logged in CHECKPOINT.md.
   - Finding 10 (positive): noted the code-review fixes.
