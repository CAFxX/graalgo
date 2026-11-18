# ADR-0010 — Temporary launcher deviations from drop-in (GG-0001, GG-0046)

**Date:** 2026-11-18
**Status:** accepted 2026-11-18 as *temporary* deviations (coordinator
instruction relaying the program owner's decision from the global
spec-conformance review, item B1). None of them amends a requirement:
GG-0001, GG-0002, GG-0011 and GG-0046 keep their text, and every deviation
below is an open failure against them until its retirement trigger fires.
**Scope:** the W1 launcher ([toolexec-design.md](../../arch/toolexec-design.md)
§1, as built in [launcher.go](../../../internal/toolexec/launcher/launcher.go)
and [cmd/graalgo-launcher/](../../../internal/toolexec/cmd/graalgo-launcher/));
[SPEC.md](../../arch/SPEC.md) §1.3, §2.1, §2.8, §3.7, §6.8, which mark the
affected text `[DEVIATION, ADR-0010]`.

## Context

`alias go=graalgo` must be indistinguishable from stock `go` at the CLI
(GG-0001) and in build/test/module behaviour (GG-0002), with faithful
`GOTOOLCHAIN` selection and a version-faithful `go version` (GG-0046). The
launcher landed with W1 (2026-11-18) makes five choices that are observable
through stock commands. Each one is there because a later work package has
not landed yet; none is wanted permanently. The review found them
implemented and documented in the design but not recorded against the
requirements they break.

Verified against the tree at `cbb2ebd0`: `launcher.Compose` sets
`GOTOOLCHAIN=local` and `CGO_ENABLED=0` unless the user set them (in the
environment or the `go env -w` file), prepends `-toolexec=…/libexec/compile`
and `-tags=purego` to `GOFLAGS`, and the launcher's `version` path prints the
stock line with ` graalgo-<version>` appended
(`TestVersionAppended` in `internal/toolexec/cmd/graalgo-launcher`).

## Deviations

| # | Deviation | Observable difference from stock `go` | Why now | Retirement trigger | Retirement action |
|---|---|---|---|---|---|
| DV1 | `go version` appends ` graalgo-<version>` | `go version` is not byte-identical (scripts that parse or compare it, txtar `stdout` assertions); `runtime.Version()` is **not** affected | users and bug reports need to tell the backend apart | the M5 txtar matrix (toolexec-design §8) runs through the launcher | print the stock line; backend identity stays available through `-V=full` (already in the cache identity, §2.1) and `$GRAALGO_HOME`. Keeping the suffix permanently would need an amendment of GG-0001/GG-0046 (owner decision) |
| DV2 | `GOTOOLCHAIN=local` default | a module whose `go`/`toolchain` line is newer than go1.27.1 fails with stock's "requires go >= …; GOTOOLCHAIN=local" instead of switching; `go env GOTOOLCHAIN` prints `local` (stock: `auto`); an explicit switch to another release is refused by graalgo-tool's tracked-release checks | a switched-to stock distribution has no stdlib fork and no graalgo-tool, so switching would silently build with gc (R9) | a graalgo toolchain for another Go release can be selected by the stock selection logic — graalgo distributions published in the `golang.org/toolchain` module shape or as `go1.N.M` launchers on `PATH` — **or** the stdlib fork reaches zero patches so a stock switched `GOROOT` can be served (track T4, SPEC §6.8) | drop the default; implement faithful selection over graalgo distributions (GG-0046) |
| DV3 | `-tags=purego` injected into `GOFLAGS` | build-tag-sensitive file selection differs (std crypto and third-party modules with `purego` variants pick their pure-Go code); `go env GOFLAGS` and `go list` output differ; once `.go.buildinfo` is written (R12, M5) binaries record `-tags=purego`; performance differs from gc's assembly paths | assembly never executes on the backend; without the tag 39 std crypto assembly functions (and third-party assembly) need intrinsics before the packages link (toolexec-design §5.4) | intrinsics (or verbatim Go fallbacks reachable through the link-time resolution rule, §5.3) exist for every std assembly function on the tracked platform, and the third-party policy of §5.3 (a reachable assembly function without a Go body fails the link, naming it) is accepted as the only behaviour | drop the tag; `GRAALGO_PUREGO` disappears |
| DV4 | `CGO_ENABLED=0` default | `go env CGO_ENABLED` prints `0` where stock prints `1` (host C compiler present); `net` uses the pure-Go resolver, `os/user` its fallbacks; packages with `import "C"` are excluded by build constraints; binaries record `CGO_ENABLED=0` once buildinfo is written | cgo-generated stubs cannot execute until the cgo→Panama mapping exists (GG-0022) | WP-CGO2 of [RECOVERY.md](../../arch/RECOVERY.md) passes its acceptance gate (the Phase-4 cgo corpus on linux/amd64) | default to stock's `CGO_ENABLED` rule |
| DV5 | `-toolexec` visible in `go env` | `go env GOFLAGS` (and `go env -json`) shows `-toolexec=$GRAALGO_HOME/libexec/compile` and `-tags=purego`; a user `-toolexec` is rewritten into graalgo-tool's `-chain` form; txtar scripts that assert an empty `GOFLAGS` or inspect the `-toolexec` value differ | GG-0011 places the backend behind the `-toolexec` seam, and `GOFLAGS` is the only stock channel that reaches every subcommand | the M5 txtar matrix: every script that differs *only* because of the injected `GOFLAGS` is recorded | owner decision between (a) keeping `-toolexec` and hiding the injection (would need a driver-side change, i.e. GG-0011's escape hatch) and (b) installing graalgo-tool as the distribution's own `pkg/tool/<os>_<arch>/{compile,link}` so no injection is needed (an amendment of GG-0011's seam wording). Until then the deviation stands |

`GOROOT=$GRAALGO_HOME/goroot` is **not** a deviation: graalgo is its own
distribution, and stock `go` reports its own `GOROOT` too.

## Consequences

- SPEC §2.1, §2.8, §3.7 and §6.8 carry `[DEVIATION, ADR-0010]` where their
  text is affected; §1.3 defines the label. The requirements are unchanged.
- The M5 txtar matrix and the CLI-surface diff (GG-0001's verification
  plan) report these differences as known failures with this ADR as the
  reason, never as exclusions: an exclusion would hide a deviation that has
  a retirement trigger.
- [RECOVERY.md](../../arch/RECOVERY.md)'s coverage matrix assigns each
  retirement to a work package (WP-DEV).
