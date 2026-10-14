# Coupled-package set, re-measured on go1.27.1 (P3-M6, GG-0009)

**Verdict: the P3-D1 re-test trigger FIRES.** Measured coupled = 142 dirs vs
SPEC's ~78 → drift **82.1%**, far above the 15% threshold. The trigger fires
under every alternative reading of the check too (§4). Per the P3-M6 plan,
the coordinator must re-examine the fork-vs-overlay boundary (P3-D1).

Machine-readable list: [coupled-packages-go1.27.1.json](coupled-packages-go1.27.1.json) (version 1, in this
directory). Tool: `tools/stdlib-excl measure` (see `tools/stdlib-excl/README.md`).

## 1. The strict pure-Go check, precisely

Evaluated per package dir for **GOOS=linux GOARCH=amd64** (GG-0023: linux
first; darwin deferred; Windows out of v1 scope). File selection uses
`go/build` with cgo enabled — selection answers "what does the gc toolchain
build on linux/amd64", not "what does graalgo support" (verified: selection
counts match `go list` exactly on os/sync/net/runtime/reflect).

A dir is **pure-Go** iff all of:

1. `go/build` selects a non-empty Go file set for linux/amd64;
2. no `.s` file is selected for the build (reason `asm`);
3. no `.go` file in the dir contains `import "C"` (parsed imports; reason `cgo`);
4. no `.go` file in the dir carries a `//go:linkname` directive (parsed `//`
   comments, matching gc's pragma scan; reason `linkname`);
5. no source file in the dir (`.go` or `.s`) is excluded from the
   linux/amd64 build by a `//go:build` / `// +build` line (reason `build-tags`).

A dir with an empty selection is classed **`not-linux-amd64`** (neither pure
nor coupled): it contributes no files to a linux/amd64 build (e.g.
`syscall/js`, `internal/syscall/windows`, `crypto/x509/internal/macos`).
A dir violating any of 2–5 is **coupled**, carrying every firing reason.

Deliberate scope choices (deviations from a naive reading, justified):

- `.go` files are scanned **whole-dir including `_test.go`**. A linkname or
  cgo use in a test file still binds the package's *test binary* to a runtime
  symbol, which the intrinsic surface (GG-0020) must provide before the
  M7–M9 waves can run it. The JSON carries `non_test_reasons` as a secondary
  metric for the narrower "compiles unchanged" reading (15 of the 142
  coupled dirs are test-only coupled).
- `build-tags` fires when a dir contains *any* file excluded on
  linux/amd64, even if the dir still builds. Rationale: the fork must do
  something deliberate with those files (carry them, drop them per
  GG-0023, or gate them) — silent inclusion is not an option. §4 shows the
  relaxed reading for comparison.
- `net` is coupled (`cgo`): it ships cgo DNS-resolver files. The fork
  decision per dir may still be "build with cgo disabled" — the check flags
  the dir for triage, it does not dictate the fork strategy.

## 2. Population

All directories under `$GOROOT/src` containing ≥1 non-test `.go` file,
excluding `cmd/` (tool binaries), `vendor/` (third-party deps), and any
`testdata/` subtree (fixtures). On go1.27.1: **399 dirs** (SPEC's earlier
tree: 398 — denominator drift +1, negligible).

## 3. Results

| metric | SPEC (earlier tree) | go1.27.1 measured | drift |
|---|---|---|---|
| package dirs | 398 | 399 | +0.3% |
| pure-Go | 229 (58%) | 236 (59%) | +3.1% |
| coupled | ~78 | **142** | **+82.1%** |
| not-linux-amd64 | — | 21 | new class |
| stdlib test binaries (dirs with `_test.go`) | — | 274 | — |

Reason counts (dirs; one dir may carry several): `asm` 35, `cgo` 15,
`linkname` 69, `build-tags` 133. 47 coupled dirs carry *only* `build-tags`.

Examples: `asm` — `crypto/internal/fips140/aes`, `crypto/internal/boring/sig`;
`cgo` — `net`, `os/user`, `runtime/cgo`; `linkname` — `sync` (`pool.go`,
`rwmutex.go`), `runtime` (`alg.go`), `bytes`; `build-tags` — `archive/tar`,
`bufio`, `os` (platform-specific files alongside the linux build).

## 4. Drift analysis — the trigger fires under every reading

The original "strict pure-Go check" was never precisely defined, so the
82.1% drift is partly definitional. Robustness across alternative readings:

1. **Relaxed build-tags** (a dir with platform-specific files but a
   non-empty linux/amd64 build is pure): coupled = 95 → drift **21.8%** → fires.
2. **Non-test-only** (scan excludes `_test.go`): coupled = 127 → drift
   **62.8%** → fires.
3. **Narrowest** (non-test-only AND build-tags-alone-doesn't-couple):
   coupled = 93 → drift **19.2%** → fires.

Real (non-definitional) contributors: tree growth since the earlier
measurement (new packages: `arena`, `crypto/boring`, `simd/*` — several land
in `not-linux-amd64`), and linkname being more pervasive than ~78 suggests
(69 dirs reference runtime symbols by name).

**P3-D1 re-test verdict:** the fork-vs-overlay boundary must be re-examined.
The coupled set is 36% of the population, not ~20%; it is dominated by
`build-tags` (133 dirs — mostly "carry the linux files, drop or gate the
rest" per GG-0023) and `linkname` (69 dirs — the GG-0021 symbol-name surface
must cover these). Whether that re-opens P3-D1 (overlay graduation) or just
re-prices the fork is the coordinator's call — the numbers above are the
input.

## 5. Boundary: what this check is NOT

"Compile unchanged against the frontend + intrinsic surface" is strictly
stronger than the pure-Go check: it requires the gofrontend package-dir
compile (P3-M1 `ExportDir`) plus the GG-0020 intrinsic surface to accept
every pure dir with zero changes. **M6 delivers the pure-Go check + per-dir
reason; the compile claim is re-verified per wave in M7–M9**, where each
wave's `go test` run either confirms a pure dir compiles unchanged or moves
it into the fork with a recorded diff. A pure verdict here is therefore a
*triage input*, not a compile certificate.

## 6. Exclusion list v1 (same milestone)

[exclusions.yaml](exclusions.yaml) v1 publishes 2 entries (`runtime/cgo` → CGO,
`plugin` → UNSUPPORTED_FEATURE), each with SPEC-§3.8 evidence. Review cap
on go1.27.1: the 5% clause binds — floor(5×274/100) = **13** packages
(tighter than 25). Validator: `tools/stdlib-excl/README.md`.

## 7. Re-test triggers recorded

- P3-D1 (fork-vs-overlay): **FIRED by this measurement** — re-examination
  required before the fork scope is frozen.
- Per-release: re-run `stdlib-excl measure` on each vendored upgrade; a
  further >15% coupled-count move re-fires P3-D1.
