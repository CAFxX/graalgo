# ADR-0006 — P3-M6: stdlib vendoring — pin, external tree, in-repo manifest (WS F)

**Date:** 2026-10-07
**Status:** accepted 2026-10-07 — implemented in `tools/stdlib-vendor/`;
first pin vendored (go1.27.1, 12,018 files).
**Scope:** closes the vendoring-pipeline workstream of P3-M6 (PLAN.md §4
WS F, first bullet: "vendoring pipeline green"); feeds the adapted-fork
mechanics, the GG-0021 linkname-diff tooling, and the coupled-package
re-measurement.
**Normative basis:** PLAN.md §§2.8 (P3-D3 adapted fork), 4 (WS F),
SPEC §3.2 (stdlib re-vendored atomically with go/*).

## Context

Phase 3 compiles the real Go standard library from source instead of
synthesizing it (P3-D3). The frontend's importer resolves packages
across a vendored stdlib tree with build-tag filtering (PLAN.md WS A).
That tree must be a reproducible, tamper-evident, version-pinned input:
every downstream measurement (linkname surface, coupled-package set,
exclusion list) is only meaningful against a known stdlib revision.

The Go 1.27.1 `src` tree is 157M / 12,018 files. It is third-party
source, not project source.

## Decision

### D1. The vendored tree lives OUTSIDE the git repo

Pinned location: `~/workspace/graalgo-stdlib/<goVersion>/`
(current: `~/workspace/graalgo-stdlib/go1.27.1/`).

Rationale: committing 157M of upstream source would permanently bloat
the repo — every clone, bisect, and checkout pays for it, and it dwarfs
the project's own history. The tree is reproducible bulk, not project
history; bulk lives on disk.

### D2. Provenance lives IN the repo

Two files under `phases/phase3/stdlib-fork/` (owned by this workstream):

- `vendor-manifest.json` — one entry per file: relative path, SHA-256,
  size, permission bits; plus the tool's schema version, the Go version,
  the source GOROOT, and the target directory. The manifest is
  byte-deterministic (no timestamps, sorted entries, fixed encoding),
  so `git diff` between pins is exactly the upstream churn.
- `pinned-version.txt` — the single pinned Go version (`go1.27.1`).

The tool (`tools/stdlib-vendor/`, plain Go, no external deps) provides
`vendor` (copy + hash + write manifest/pin) and `verify` (re-hash the
external tree against the manifest; reports missing/extra/modified).

### D3. Pin semantics

- The pin is created on the first vendor run and is a **reviewed
  decision**, never a side effect: a `--allow-version-drift` run
  vendors the new tree but does *not* move the pin. Moving the pin is a
  deliberate step in the update procedure (VENDORING.md §3), committed
  together with the new manifest.
- Vendoring a GOROOT whose version differs from the pin is **refused**
  unless `--allow-version-drift` is passed — the common accident
  (wrong toolchain on PATH) fails loudly.
- Vendoring into an existing non-empty target is **refused** unless
  `--force` is passed — a new release always lands in a fresh directory
  until the cutover is deliberate.
- The tool refuses to vendor symlinks or non-regular files: Go releases
  ship plain files, and anything else means the source tree is not what
  we think it is.

### D4. `verify` is the tamper-evidence gate

Before any workstream consumes the vendored tree after the tree could
have been touched (shared machine, restore, disk work), `verify` is
run. It is cheap (single pass, ~25s for 12k files) and it names the
exact files that differ.

## Alternatives considered

1. **Commit the stdlib into the repo (possibly via git submodule).**
   Rejected: 157M in history is permanent bloat; a submodule still
   requires every consumer to fetch 157M and pins via a superproject
   commit, which is weaker provenance than a per-file hash manifest
   (a submodule pin says *which commit*, the manifest says *which
   bytes* — and ours is verified on every `verify` run).
2. **Reference the toolchain's GOROOT in place, no copy.** Rejected:
   the toolchain directory is mutable by toolchain upgrades and shared
   with every other Go user on the machine; the vendored tree must be
   immutable and owned by the project. A copy is also what lets old and
   new pins coexist during an upgrade (§3 of VENDORING.md).
3. **Manifest with timestamps / non-deterministic ordering.** Rejected:
   it would make `git diff` useless for reviewing upstream churn, which
   is one of the manifest's two jobs (the other is tamper evidence).

## Consequences

- Positive: reproducible stdlib input with cheap tamper evidence;
  upgrade procedure is mechanical and reviewable (manifest diff +
  linkname-diff re-run + coupled-package re-measurement + pin commit).
- Negative: the external tree is not backed up by git — machine loss
  means re-vendoring (mechanical: one tool invocation against the
  pinned toolchain). Documented in VENDORING.md §3.
- The `verify` gate is only as good as the discipline to run it; the
  update procedure (§3) makes it a named step before downstream work.
