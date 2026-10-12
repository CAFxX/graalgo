# Hash/hmac/rand — final reconciled package table

Lane: `m9-hashwrite`, branch `wt/graalgo-p3/m9-hashwrite`, tip `cfa7407`
(vacuous sync import; tree identical to m9 tip `27eb349`).
gc reference: go1.27.1 linux/amd64 (`~/tools/go/bin/go`).
Method: official `run_wave3.sh` / `run_wave.sh` differentials, both gmp legs,
byte-identical (or normalized-identical) vs gc.

## Result

| Package | Corpus programs | gmp=1 | gmp=4 | On `cfa7407` | With mathbig `07c2731` |
|---|---|---|---|---|---|
| hash/adler32 | 00_vectors, 01_streaming, 02_marshal, 03_adversarial | 4/4 | 4/4 | PASS | PASS |
| hash/crc32 | 00_vectors, 01_streaming, 02_marshal, 03_adversarial | 4/4 | 4/4 | PASS | PASS |
| hash/crc64 | 00_vectors, 01_streaming, 02_marshal, 03_adversarial | 4/4 | 4/4 | PASS | PASS |
| hash/fnv | 00_vectors, 01_streaming, 02_marshal, 03_adversarial | 4/4 | 4/4 | PASS | PASS |
| hash/maphash | 00_structural, 01_panic, 02_comparable | 0/3 | 0/3 | **FAIL** | PASS |
| math/rand | 00_source, 01_distributions, 02_adversarial, 03_intn_panic | 4/4 | 4/4 | PASS | PASS |
| crypto/hash | 01_digest_kat, 02_streaming, 03_clone_marshal, 04_xof | 4/4 | 4/4 | PASS | PASS |
| crypto/hmac | 01_rfc_vectors, 02_reset_clone | 2/2 | 2/2 | PASS | PASS |
| crypto/rand | 01_entropy | 0/1 | 0/1 | **FAIL** | PASS |

Totals: **60/60 legs green** — 52/60 on the lane's own tip, 8/60 via the
mathbig-lane fix (verified, pending integration).

## The 8 red legs (root-caused, fix verified)

On bare `cfa7407`, `hash/maphash` (6 legs) and `crypto/rand` (2 legs) fail with
`gofrontend: method SetBytes not found on Int` /
`method ProbablyPrime not found on Int` — the corpus programs use `math/big`
(`big.NewInt().SetBytes`, `ProbablyPrime`) for Seed generation, and the
synthetic `math/big` method closure was incomplete.

Fix: mathbig lane commit `07c2731` ("fix crypto/rand synthetic method
resolution (math/big closure + reader.Read helper)"), 3 files, +26/-4,
general (not package-specific).

Verification (2026-10-11 ~23:05–23:11 UTC): `07c2731` applied **uncommitted**
onto this worktree (tree == m9 tip `27eb349`, patch applied clean), gofrontend
rebuilt, both legs re-run via the official runners:
- `hash/maphash`: **6 passed, 0 failed** (was 0/6)
- `crypto/rand`: **2 passed, 0 failed** (was 0/2)

Patch then reverted; gofrontend rebuilt back to pristine. This worktree is
unchanged at `cfa7407`.

Remaining step (integration, not lane work): land the mathbig lane on `m9`,
rebase this lane, re-run the 8 legs natively. No new lane defect exists.

## Environmental notes (not defects)

- `hash/adler32` first attempt: runner exit 143 (SIGTERM under load-shed);
  `hash/crc32` first attempt: exit 137 (OOM, box saturated). Both recovered
  cleanly on re-run (15:07 / 15:10 UTC): 8 passed each.
- No code changes were needed on this lane for any green leg.

## Evidence

- `../m9-tmp/wave3/hashwrite-diff-20261011-1315.log` — full battery on `cfa7407`
  (13:15–14:28 UTC): all green legs + the 8 math/big failures with exact errors.
- `../m9-tmp/wave3/hashwrite-diff-20261011-1302.log` — adler32/crc32 clean
  re-runs (15:07–15:13 UTC): 8 passed each.
- `../m9-tmp/wave3/hashverify-20261011-2305.log` — mathbig-patch test legs:
  maphash 6/6, crypto/rand 2/2.
- `../m9-tmp/wave3/run-hashwrite-battery.sh`, `run-hashwrite-diff.sh` — runners.
