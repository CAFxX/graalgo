# Stdlib vendoring — procedure and rationale (P3-M6, WS F)

The graalgo Phase-3 frontend compiles the real Go standard library from
source (P3-D3, "adapted fork"). The vendored stdlib tree is the single
source of truth every downstream step reads: the adapted fork mechanics,
the linkname-diff tooling (GG-0021), the coupled-package measurement,
and the build-tag-filtered importer.

## 1. Layout

| Artifact | Location | Committed? |
|---|---|---|
| Vendoring tool (Go, no external deps) | `tools/stdlib-vendor/` | yes |
| Vendored stdlib tree (157M for go1.27.1) | `~/workspace/graalgo-stdlib/<goVersion>/` | **no** — external by design |
| File manifest (path + SHA-256 + size + mode) | `phases/phase3/stdlib-fork/vendor-manifest.json` | yes (2.2M) |
| Pinned Go version | `phases/phase3/stdlib-fork/pinned-version.txt` | yes |
| Pinning decision | `phases/phase3/adr/ADR-0006-stdlib-vendoring.md` | yes |
| Red-green evidence | `phases/phase3/stdlib-fork/REDGREEN.md` | yes |

Current pin: **go1.27.1** (vendored 2026-10-07 from
`/home/hatch/tools/go-1.27.1/src`, 12,018 files, 132,185,622 bytes).

## 2. Routine integrity check

```sh
cd ~/workspace/graalgo/tools/stdlib-vendor
go run . verify --repo ~/workspace/graalgo
```

`verify` re-hashes every file in the external tree against the in-repo
manifest and reports `MISSING` / `EXTRA` / `MODIFIED` entries. Exit 0
means the tree is exactly what the manifest describes. Run it before
any work that consumes the vendored tree if the tree may have been
touched (shared machine, restores, disk work).

## 3. Re-vendoring on a new Go release

Never edit the vendored tree by hand. A new Go release is a new pin,
re-vendored atomically. Steps:

1. **Install the new toolchain** and confirm it is the one you mean:
   `go version` and `<new-goroot>/VERSION` must agree.
2. **Vendor with explicit version drift**, into a *new* target directory:
   ```sh
   cd ~/workspace/graalgo/tools/stdlib-vendor
   go run . vendor --repo ~/workspace/graalgo \
       --goroot <new-goroot> --target ~/workspace/graalgo-stdlib/<new-version>/ \
       --allow-version-drift
   ```
   Without `--allow-version-drift` the tool refuses: the source version
   differs from `pinned-version.txt`. Vendoring into the *old* target
   without `--force` is also refused — a new release always lands in a
   fresh directory, so the old tree stays usable until the cutover.
3. **Diff the manifests** (old vs new). The manifest is deterministic,
   so the diff is exactly the upstream churn:
   ```sh
   git -C ~/workspace/graalgo show HEAD:phases/phase3/stdlib-fork/vendor-manifest.json > /tmp/manifest-old.json
   # compare entry sets with jq/python: added / removed / modified paths
   ```
   Review the churn for anything touching the ~78 coupled packages or
   the `go/*` packages (SPEC §3.2 requires atomic re-vendor of stdlib
   with go/*).
4. **Re-run the linkname-diff tooling** (GG-0021) against the new tree
   and **re-run the coupled-package measurement**. Both are sensitive
   to stdlib source changes; their outputs must be re-baselined on the
   new tree before any frontend work consumes it.
5. **Update the pin deliberately**: write the new version into
   `phases/phase3/stdlib-fork/pinned-version.txt` and commit it together
   with the new manifest. The tool intentionally does *not* move the pin
   on a `--allow-version-drift` run — the pin is a reviewed decision,
   not a side effect.
6. **Retire or keep the old tree**: downstream tooling references trees
   by path (`~/workspace/graalgo-stdlib/<version>/`), so old and new
   trees coexist. Delete the old tree only when nothing references it.

## 4. Rationale: external tree + in-repo manifest

1. **Git bloat.** 157M of third-party source does not belong in the
   repo's history. Every clone, every bisect, every CI checkout would pay
   for it forever, and it would dwarf the project's own history. The
   manifest (2.2M) is the largest in-repo artifact and it compresses
   well.
2. **Reproducibility without the weight.** The manifest records the
   exact source GOROOT, the Go version, and a SHA-256 per file. Given
   the pinned toolchain, anyone can reproduce the tree byte-for-byte;
   `verify` proves a given tree matches. Provenance lives in git; bulk
   lives on disk.
3. **Deterministic manifests.** The manifest contains no timestamps and
   its entries are sorted with fixed JSON encoding, so two runs over
   identical sources are byte-identical. `git diff` on the manifest is
   therefore a truthful changelog of upstream churn (step 3 above), not
   noise.
4. **Refusal gates, not conventions.** Version drift and target clobber
   require explicit flags (`--allow-version-drift`, `--force`). The
   common accidents — vendoring the wrong toolchain, or clobbering the
   tree another workstream is reading — are refused by the tool instead
   of documented as "be careful".

## 5. Failure modes (quick reference)

| Symptom | Cause | Fix |
|---|---|---|
| `target directory already exists and is non-empty` | target in use | use a fresh `--target`, or pass `--force` if you mean to replace it |
| `GOROOT version differs from pinned version` | wrong toolchain, or deliberate upgrade | check `go version`; for an upgrade follow §3 |
| `refusing to vendor symlink ...` | GOROOT/src is not a plain tree | investigate; Go releases ship plain files |
| `VERIFY FAILED` with MODIFIED entries | tree tampered with | re-vendor with `--force`, or restore the tree; never hand-edit |
