# stdlib — graalgo's standard-library fork (status: **new**)

The new path compiles **upstream Go's own std source**. The only
differences live here, and nowhere else (no tool keeps a package list):

- [fork/](fork/) — *replaced* packages, one complete directory per import
  path. Today only [fork/runtime/](fork/runtime/): the facade `runtime`
  (upstream's declarations and docs; body-less functions are resolved by the
  [intrinsic registry](../internal/toolexec/intrinsics/README.md)) and
  [fork/runtime/race/](fork/runtime/race/).
- [patches/](patches/) — minimal diffs, `patches/<import path>/<file>.patch`
  (empty today). Upstream Go is never modified in place.

[internal/toolexec/fork/](../internal/toolexec/fork/) validates and applies
both to the tracked release (`make goroot`, or the `-overlay` the test
scripts build via [test/lib/toolexec.sh](../test/lib/toolexec.sh)). It
rejects any file directly in `fork/` or `patches/` — which is why this
README sits here and not inside them.

**Not here:** v1's `synth_*.go` stand-ins ([internal/frontend/](../internal/frontend/README.md))
and the research-era [tools/stdlib-fork/](../tools/stdlib-fork/). Fixing a std
behavior usually means fixing the backend or an intrinsic ([docs/MAP.md](../docs/MAP.md)).

**Test:** `cd internal/toolexec && go test ./fork`; `make test-m1r test-m1c`
([toolexec-design.md](../docs/arch/toolexec-design.md) D8, §5).
