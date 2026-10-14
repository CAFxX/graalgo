# Red-green evidence — stdlib-vendor (P3-M6, WS F)

Tool: `tools/stdlib-vendor/` (plain Go, no external deps).
Method: each refusal/detection behavior was verified green, then the
implementation was mutated to remove the behavior (mutation noted per
case), the corresponding test was run to show it FAILS (red), and the
mutation was reverted. All mutations were restored; the suite is green
on the final tree (`go test ./...`, `go vet ./...`).

## (a) Two consecutive vendor runs produce byte-identical manifests (same machine)

- Test: `TestVendorManifestDeterministic` (fake GOROOT, three runs).
- Red mutation: added a `vendoredAt` nanosecond-timestamp field to the
  manifest schema.
- Red result: FAIL — manifests differ across consecutive runs (the test
  prints both manifests).
- Restored: green.

Qualifier: "byte-identical" is a same-machine property -- the manifest
embeds absolute machine paths (`sourceGoroot`, `targetDir`), so two runs
on different machines differ in exactly those fields.

Full-scale check on the real tree: vendored go1.27.1 twice
(`--force` on the second run); `cmp` of the two
[vendor-manifest.json](vendor-manifest.json) files: **byte-identical** (12,018 entries).

## (b) Tamper with one vendored file → `verify` fails (passing before)

- Test: `TestVerifyDetectsTamper` — vendors a fake tree, runs verify
  (expects `VERIFY OK`), appends one line to `pkg/b.go`, runs verify
  again (expects failure naming exactly `modified=[pkg/b.go]`).
- Red mutation: removed both the size check and the SHA-256 comparison
  from `verifyTree` (tamper goes undetected).
- Red result: FAIL — `verify passed after tampering with pkg/b.go`.
- Restored: green. A weaker first mutation (removing only the hash
  comparison) did NOT produce red because the size check still caught
  the append — evidence the test exercises both detection paths;
  `TestVerifyDetectsMissingAndExtra` covers missing/extra files.

Full-scale check on the real tree: `verify` over
`~/workspace/graalgo-stdlib/go1.27.1/`: **checked 12018 files, VERIFY OK**.

## (c) GOROOT version differing from the pin is refused without --allow-version-drift

- Test: `TestVersionDriftRefused` — pins go1.27.1, then vendors a
  go1.28.0 fake GOROOT: expects `ErrVersionMismatch`; then with
  `AllowVersionDrift: true`: expects success, pin unchanged, manifest
  records `versionDrift: true`.
- Red mutation: removed the version check from `vendorTree`.
- Red result: FAIL — expected `ErrVersionMismatch`, got nil.
- Restored: green.

Real-artifact check: `--goroot` pointed at a fake go1.28.0 GOROOT with
the real repo pin (go1.27.1): refused —
`GOROOT version differs from pinned version ... source is go1.28.0, pinned is go1.27.1`;
with `--allow-version-drift` it vendors (into a scratch target) and the
repo pin still reads `go1.27.1`.

## (d) Vendoring into an existing dir without --force is refused

- Test: `TestExistingTargetRefused` — vendors, vendors again without
  `Force`: expects `ErrTargetExists`; then with `Force: true`: expects
  success.
- Red mutation: removed the target-exists check from `vendorTree`.
- Red result: FAIL — expected `ErrTargetExists`, got nil.
- Restored: green.

Real-artifact check: `vendor` against the existing
`~/workspace/graalgo-stdlib/go1.27.1/` without `--force`: refused —
`target directory already exists and is non-empty (use --force)`.

## Suite status

- `go test ./...` in `tools/stdlib-vendor/`: all 8 tests pass.
- `go vet ./...`: clean.
- No placeholders; no new external dependencies (`go.mod`: stdlib only).
