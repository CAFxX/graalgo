# Vendoring record — go/* tree for gofrontend

**Pinned revision:** GOROOT `go1.27.1` (`~/tools/go`, symlink to
`go-1.27.1`), VERSION file `go1.27.1`, build time `2026-08-28T16:20:06Z`
(`go version go1.27.1 linux/amd64`).
**Vendored:** 2026-10-04 (WS B). **Tree:** `frontend/go/` = byte-identical
copy of `$GOROOT/src/go` (329 `.go` files, whole tree including
`go/internal/*`, test files included verbatim).

## Why this shape (GG-0012, GG-0044)

SPEC §3.2 / PLAN.md D1 require the `go/*` tree **reused verbatim from the
tracked release, re-vendored atomically every release, zero local patches**.
Two mechanisms enforce this, because neither alone suffices:

1. **Byte-identical copy** at `frontend/go/`. Verified by
   `diff -r "$GOROOT/src/go" frontend/go` (the `check` action of
   `build.sh`). Any local patch fails this gate — patches cannot hide.
2. **`-overlay` build** (`frontend/build.sh`): the go command resolves
   `go/ast` etc. to GOROOT, so the build maps every `$GOROOT/src/go/*.go`
   read onto the vendored copy. The compiled `gofrontend` binary therefore
   **literally contains the vendored sources** — the copy is not dead
   weight. (Proof of mechanism: a marker constant added to the vendored
   `go/token` is what the overlay-built binary prints; see WS B report.)

Module mode cannot shadow stdlib import paths (`module go` is rejected;
GOPATH mode resolves std to GOROOT), and rewriting import paths inside the
copies would itself be a local patch — the overlay is the only mechanism
that keeps the tree byte-identical *and* compiled.

## Re-vendoring (atomic, per release)

```sh
cd phases/phase1/frontend
rm -rf go && cp -r "$(go env GOROOT)/src/go" go
# update the pinned revision at the top of this file
./build.sh check   # gofmt + vet + tests + diff gate
```

One copy command = one atomic unit (SPEC: "frontend + stdlib as one unit").
No patch stack exists by construction; rebase conflicts are impossible
unless someone edits `go/` directly, which the diff gate forbids.

## D1 falsifier watch (GG-0012)

`export.go` uses only long-stable `go/*` APIs (`parser.ParseFile`,
`types.Config.Check`, `Info.{Types,Defs,Uses}`). If a re-vendoring breaks
compilation of the exporter or changes the behavior of these APIs, **STOP
and report** — that is the spike-3 toil re-measurement trigger (PLAN.md §8:
"frontend rebase toil > 200 agent-hours → bound broken"), not a cue to
adapt the exporter silently. No falsifier has triggered as of this writing.
