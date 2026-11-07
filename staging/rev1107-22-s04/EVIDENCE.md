# Lane rev1107-22-s04 — evidence (S-04: GoFloatFmt has no direct tests)

- **Issue:** REVIEW-2026-11-07-07 S-04, carried in REVIEW-2026-11-07-22 (HEAD `59f0a247`).
- **Lane:** `rev1107-22-s04`, registry id suggested `graalgo-rev1107-22-s04` (NOT created — quota).
- **Date:** 2026-11-07 ~22:12–22:40 CET. Head at start: `59f0a247088f0e0b6db7e0623076866727dd873c`.
- **Outcome:** fix + test STAGED in `/tmp/rev1107-22-s04/` — BLOCKED-quota, nothing
  committed, no worktree claimed, no registry entry written. See `RESUME.md`.

## 1. Gap re-verified from disk truth (HEAD 59f0a247)

- `grep -rn GoFloatFmt` over all `src/test` trees: **zero references**.
- Only production callers: `BuiltinPrint.java:105-113` (`formatFloat`/`formatComplex`)
  and `PanicRendering.java:84-88` (`formatFloat`). No test dir, corpus, or tool
  references the class. Gap confirmed as filed.

## 2. What was built (staged in `deliver/`)

| Staged file | Lands at (repo-relative) | Content |
|---|---|---|
| `GoFloatFmtTest.java.new` | `internal/backend/runtime/src/test/java/graalgo/runtime/GoFloatFmtTest.java` (NEW) | 7 test methods, **747 checks**, all expected strings computed by gc (see §3) |
| `GoFloatFmt.java.patch` | `internal/backend/runtime/src/main/java/graalgo/runtime/GoFloatFmt.java` | 1-hunk fix for the P0 bug the test exposed (see §5); applies cleanly (`patch -p1 --dry-run` verified) |
| `SchedTestRunner.java.patch` | `internal/backend/runtime/src/test/java/graalgo/sched/SchedTestRunner.java` | 1-line registration of the new test class; applies cleanly |
| `genvectors.go`, `gc-vectors.txt`, `gen_test.py` | provenance (keep with lane evidence) | gc vector generator, its 768-line output, test generator |
| `checkbits.go`, `checkbits2.go` | provenance | bit-pattern verification helpers |

Test conventions follow the repo: package `graalgo.runtime`, plain public no-arg
`test*` methods, manual assert helpers (as in `TimerTest.java`), registered in
`SchedTestRunner.TESTS`. No JUnit (the repo has none for these tests). Truffle
skill consulted: `GoFloatFmt` is pure Java (no Truffle nodes); the applicable
parts are §7 (semantic tests against the reference implementation = gc) and
rule 8 (report evidence, not intuition) — both honored via gc-computed vectors.

## 3. Non-tautological vectors (gc is the oracle)

`genvectors.go` drives `strconv.FormatFloat` / `strconv.FormatComplex`
(go1.27.1, `~/tools/go/bin/go`) over IEEE bit patterns; `gen_test.py` converts
the 768 gc output lines into 747 deduplicated `check(...)` calls (values are
reconstructed in Java from the same bit patterns via
`Double.longBitsToDouble` / `Float.intBitsToFloat`).

- **Correction during generation:** 11 of my hand-written hex literals were
  wrong (e.g. "1e21" was actually 2^53, "1234567" was 1e6, "1e-5" was ~1e-8,
  "0.45" was ~0.43, "0.15" in one spot was 0.2). All were caught by decoding
  each literal with gc (`checkbits.go`/`checkbits2.go`) and fixed before
  generating vectors. This is exactly why vectors must come from gc + bit
  patterns, not from hand-typed decimals. Final vectors verified spot-checked:
  1e21→"1e+21", 1e20→"1e+20", 1e-5→"1e-05", 1234567 %g prec 7→"1234567",
  0.1 %g prec 17→"0.10000000000000001".
- Coverage: shortest %g f64+f32 (subnormals incl. 5e-324, 9.88e-324, 1.4e-45
  family, largest subnormal); %g e/f boundaries (exp −4/−5, exp ≥ eprec);
  fixed %e/%f incl. half-to-even (0.125→"0.12", 2.5→"2", 3.5→"4"); values
  rounding to zero (1e-300 %f); carries (999999.5 %f 0→"1000000"); specials
  at every verb; hex %x/%X prec −1..20 (incl. the prec-15 guard edge);
  binary %b; complex64/128 incl. subnormal and NaN/Inf components.

## 4. Verification (standalone javac harness in /tmp — repo build needs home writes)

GraalVM javac 25 (`~/tools/graalvm-jdk-25/bin/javac`, `-Xlint:deprecation -Werror`,
as the runtime Makefile uses) compiled the patched `GoFloatFmt.java` + new test
+ a `SchedTestRunner`-equivalent reflective runner; run under
`~/tools/graalvm-jdk-25/bin/java`. Full in-repo `make test` (runtime module)
could NOT run: it writes `out/` under home (quota-closed) and the joint-round
compile needs the truffle/interop trees.

- **GREEN (fixed code):** 7 tests, 0 failures — 747 checks pass.
  Log: `green-final.log` (re-run after the last header edit).
- **RED 1 (unpatched `GoFloatFmt.java` + new test):** `testFixedEAndF` FAILS —
  `formatFloat(0x3fe4000000000000, 'f', 0, 64)` expected `1` actual `0`
  (0.625 %f prec 0). Log: `red1.log`. This is the §5 bug, not a test error.
- **RED 2 (HALF_EVEN → HALF_UP perturbation):** `testGPrecision` + `testFixedEAndF`
  FAIL — 2.5 %g prec 1 expected `2` actual `3`; 2.5 %e prec 0 expected `2e+00`
  actual `3e+00`. Log: `red2.log`. Rounding tests are falsifying.
- **RED 3 (subnormal tie-break flipped even→odd):** 7 tests, 0 failures — the
  tie-break did NOT fire. Investigation: an exact shortest-tie would need the
  subnormal's exact value exactly halfway between two adjacent k-digit
  decimals, requiring 5^(k−1−e) | (2m+1) with m < 10^k — impossible for
  k ≤ 17 (f64) / ≤ 9 (f32). Corroborated by an instrumented scan of subnormal
  bit patterns 1..5000 (both sizes) + structured large subnormals: **zero ties**.
  The branch is provably unreachable defensive code (documented in the test
  header); no vector can falsify it. Logs: `red3.log`, `tie-scan.log`.
- **Gates on staged files:** no TODO/FIXME/XXX, no `System.out/err.println`.

## 5. P0 bug found by the new test (NOT in the original S-04 scope — needs reviewer scrutiny)

`GoFloatFmt.roundedParts`, `want <= 0` early return: for `%f` where `dp + prec <= 0`
(all significant digits below the last kept place) it returned zero
unconditionally, claiming "the value rounds to zero". Wrong: the value rounds
UP to one unit of 10^-prec when it exceeds half a unit (exact ties round to
even, i.e. to zero). Evidence (all gc-computed): 0.625 %f prec 0 → `"1"` (impl
gave `"0"`); 0.006 %f prec 2 → `"0.01"` (impl gave `"0.00"`); 0.9/0.99 %f 0 → `"1"`.
Only `%f` can reach `want <= 0` (`%e`: want = prec+1 ≥ 1; `%g`: want ≥ 1).

**Fix (in `GoFloatFmt.java.patch`, one hunk):** when `fmt == 'f'` and `want <= 0`,
compare the exact expansion against half a unit (`5E-(prec+1)`); if strictly
greater, return digits `"1"` with `dp = 1 - prec` (one unit of 10^-prec),
else return empty digits as before. The old comment (which asserted the
round-to-zero claim, citing "M8 closeout triage class E") is replaced with
the corrected reasoning. The fix is minimal and fully pinned by 24 new
`%f` vectors in the want<=0 class (0.5 / just-above / just-below 0.5,
0.625/0.375/0.125, 0.25/0.75 %f 1, 0.005/0.004/0.006 %f 2, …).

**Why the fix is in this lane:** leaving the test red on landing would violate
the repo's suite-green done-gate; the fix is the smallest change that makes
the gc-pinned test green, and it is flagged here (and in the test header) for
the independent reviewer to scrutinize as a separate hunk. If the coordinator
prefers, the test can land first with the bug filed as a new P0 — but do NOT
land the test while RED1 is the state of the tree.

## 6. Quota / environment (verified this run)

- Home probe (real 8 KB write, byte-count verified): **FAILED** `Disk quota exceeded`
  at ~22:14 CET (`~/quota-probe-8k.tmp` not created). /var/tmp 100% full (1.5M free).
  /tmp 483M free. Load CLEAR (3.06) at start; `load-shed.sh backoff` rc=0.
- Per the brief: NO worktree claimed, NO registry entry, NO commit. All work is
  staged under `/tmp/rev1107-22-s04/` (see `RESUME.md` for the landing recipe).
- Nothing was written to the main repo tree (all reads/compiles used /tmp copies).
