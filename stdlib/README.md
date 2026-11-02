# stdlib/ — Go standard library support

This directory is a signpost, not a source tree: the stdlib work lives
elsewhere, and this file exists so the `stdlib/` links in the README and
in [docs/REPO_STRUCTURE.md](../docs/REPO_STRUCTURE.md) resolve.

- Synthetic package implementations (the honest-workaround tier) live in
  [internal/frontend/](../internal/frontend/) as `synth_*.go`
  (`package main`); each states why it is synthetic and what root-cause
  fix retires it. The full inventory is
  [docs/arch/bridge-inventory.md](../docs/arch/bridge-inventory.md).
- Stdlib vendoring tooling lives in
  [tools/stdlib-vendor/](../tools/stdlib-vendor/); fork/linkname tooling
  in [tools/stdlib-fork/](../tools/stdlib-fork/) and
  [tools/stdlib-linkdiff/](../tools/stdlib-linkdiff/).

## Upstream patches: none currently needed

There is no `patches/` directory here, deliberately (removed 2026-11-02,
lane H): zero patches are currently needed — the real sources compile
unmodified — and no patch-applying consumer exists in the tree
(`tools/stdlib-vendor`, `tools/stdlib-fork`, and the Makefile do not
reference one). If a package ever cannot compile unmodified, its patch
needs its consumer designed first; do not recreate an empty `patches/`
placeholder and do not claim a mechanism that has no consumer.
Upstream Go is never modified; fixes go into graalgo.
