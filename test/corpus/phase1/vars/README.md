# Vars corpus (M7)

Differential corpus for package-level declarations: every program is a
complete `package main` using package-level `const` and/or `var`,
exercised both by stock gc (the oracle, via a freshly built binary) and
by graalgo, comparing exit code, stdout (exact), and stderr (normalized)
— see `graalgo.test.Differential`.

## Conventions

- **gofmt-clean**, single file, `fmt` import only (the D4 stub).
- **Determinism:** no maps with iteration-order-dependent output, no
  randomness, no wall-clock. `GOTOOLCHAIN=local GOMAXPROCS=1` for both
  backends.
- Programs print their results; the test compares whole-program
  observables (no `// observe:` injection — globals live outside main's
  frame, so frame-slot reads don't apply).

## Programs

| File | Tests |
|------|-------|
| gv01_const.go | package-level const (string, int); references fold |
| gv02_iota.go | iota const blocks; typed consts (`Level`) |
| gv03_var_init.go | var with composite-literal init; global map mutated across calls |
| gv04_var_zero.go | vars without init take zero values; `== nil`, `len`/`cap` of nil |
| gv05_dep_order.go | initialization follows dependency order, not source order |
| gv06_var_call.go | initializer calling a package-level function |
| gv07_global_mutate.go | global read/write/`++`/`=` across functions |
| gv08_var_string.go | global string var; reassignment |
| gv09_blank.go | `var _ = ...` initializers run for side effects, in order |

## Edge-case coverage (each pinned against a past or plausible bug)

- `gv05_dep_order`: `var a = b + 1` declared *before* `var b = 2` must
  still initialize `b` first (a=3). Source-order initialization
  miscompiles this.
- `gv04_var_zero`: `zsl == nil` on a nil slice, `len`/`cap` of nil —
  the null carrier must not be dereferenced.
- `gv09_blank`: blank initializers are evaluated (side effects kept)
  and participate in dependency ordering.
