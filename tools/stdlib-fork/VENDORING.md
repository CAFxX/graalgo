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
| Vendored stdlib tree (157M on disk for go1.27.1; 126 MiB file bytes) | `~/workspace/graalgo-stdlib/<goVersion>/` | **no** — external by design |
| File manifest (path + SHA-256 + size + mode) | `tools/stdlib-fork/vendor-manifest.json` | yes (2.2M) |
| Pinned Go version | `tools/stdlib-fork/pinned-version.txt` | yes |
| Pinning decision | `docs/dev/adr/ADR-0006-stdlib-vendoring.md` | yes |
| Red-green evidence | `tools/stdlib-fork/REDGREEN.md` | yes |

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
re-vendored atomically.

**Which tree the tools read (resolution rule).** `stdlib-excl`
(`validate`, `measure`) and `stdlib-linkdiff` resolve their stdlib
tree the same way: (1) the vendored tree for the pinned version,
`~/workspace/graalgo-stdlib/<ver>/` -- flat layout, the target dir IS
the src root, there is no `src/` subdir -- used only when the committed
`vendor-manifest.json` records the same `goVersion` as
`pinned-version.txt` (a manifest/pin skew fails loud); (2) otherwise
the live toolchain's `GOROOT/src`, announced loudly on stderr. An
explicit `--src`/`--goroot` always wins. During a re-vendor the pin
still names the OLD version until step 5, so step 4 must pass the new
tree explicitly.

Steps:

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
   git -C ~/workspace/graalgo show HEAD:tools/stdlib-fork/vendor-manifest.json > /tmp/manifest-old.json
   # compare entry sets with jq/python: added / removed / modified paths
   ```
   Review the churn for anything touching the ~78 coupled packages or
   the `go/*` packages (SPEC §3.2 requires atomic re-vendor of stdlib
   with go/*).
4. **Re-run the per-release tooling** against the new tree -- pass the
   new target dir explicitly (`--src`/`--goroot <new-target>/`; the
   default resolution still follows the old pin until step 5):
   - the linkname-diff tooling (GG-0021):
     ```sh
     cd ~/workspace/graalgo/tools/stdlib-linkdiff
     go run . --goroot ~/workspace/graalgo-stdlib/<new-version>/ \
         --json ../../tools/stdlib-fork/linkname-trial-<new-version>.json \
         --md ../../tools/stdlib-fork/linkname-trial-<new-version>.md
     ```
     Triage per its README (skip-list size vs SPEC §3.6 kill trigger,
     `routes.txt` diff, provided-snapshot diff).
   - the coupled-package measurement:
     ```sh
     cd ~/workspace/graalgo/tools/stdlib-excl
     go run . measure --src ~/workspace/graalgo-stdlib/<new-version>/ \
         --out ../../tools/stdlib-fork/coupled-packages-<new-version>.json \
         --go-version <new-version>
     ```
     Re-run the P3-D1 drift check against the new report.
   - the Plan 9 assembly inventory (P3-M6 asm strategy):
     ```sh
     cd ~/workspace/graalgo
     python3 tools/stdlib-fork/gen-asm-inventory.py --goroot <new-goroot>
     ```
     The generator enumerates every `.s`/`.S`/`.sx` file, fails on any
     uncategorized file, and regenerates the JSON inventory, the
     Markdown summary, and the embedded `zz_asm_inventory_gen.go` the
     loader enforces. (Update the generator's pinned `VERSION`
     constant to the new release first -- it names the output files.)
     Verify with
     `python3 tools/stdlib-fork/gen-asm-inventory.py --check`.
     Skipping this step leaves the loader enforcing the OLD
     version-pinned inventory: every new `.s` file fails the build as
     uncategorized.
   All three outputs must be re-baselined on the new tree before any
   frontend work consumes it.
5. **Update the pin deliberately**: write the new version into
   `tools/stdlib-fork/pinned-version.txt` and commit it together
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
3. **Deterministic manifests (same machine).** The manifest contains no
   timestamps and its entries are sorted with fixed JSON encoding, so two
   runs over identical sources on the same machine are byte-identical.
   (Same-machine qualifier: the manifest embeds absolute machine paths
   -- `sourceGoroot`, `targetDir` -- so cross-machine runs differ in
   exactly those fields.) `git diff` on the manifest is therefore a
   truthful changelog of upstream churn (step 3 above), not noise.
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
