# gofrontend — Go frontend service (WS B)

Type-checks a single-file Go program with the vendored `go/*` packages and
emits the `frontend/v1` typed-AST JSON document to stdout.

- Wire format: [`../../phases/phase1/docs/frontend-v1.md`](../../docs/arch/frontend-v1.md) (normative)
- Vendoring: [`VENDORING.md`](VENDORING.md) (pinned revision, re-vendor procedure)
- Java consumer: `graalgo.frontend.NodeFactory` ([`../backend/truffle/src/main/java/graalgo/frontend/`](../backend/truffle/src/main/java/graalgo/frontend/))

## Layout

| Path | Contents |
|---|---|
| [`main.go`](main.go), [`export.go`](export.go) | CLI + exporter (package `graalgo-frontend`) |
| [`export_test.go`](export_test.go), [`testdata/`](testdata/) | Golden tests; `testdata/*.go` + checked-in `*.json` goldens; `bad_*.go` scope/type error cases |
| [`go/`](go/) | Vendored `$GOROOT/src/go` tree, byte-identical (see [`VENDORING.md`](VENDORING.md)) — do not edit |
| [`build.sh`](build.sh) | Overlay build/vet/test/check wrapper (generates `overlay.json`, git-ignored) |
| [`tools/validate_frontend_v1.py`](tools/validate_frontend_v1.py) | Schema validator for emitted documents (`--selftest` for its own negative cases) |
| [`javatest/`](javatest/) | Java-side tests: `NodeFactoryTest.java`, golden JSON + `.go` pairs, negative fixtures |

## Build & test

Requires stock Go on PATH (`export PATH=~/tools/go/bin:$PATH`).

```sh
./build.sh build   # overlay build -> ./gofrontend
./build.sh check   # gofmt + go vet + go test + vendoring diff gate (all must pass)
./build.sh test    # go test only
python3 tools/validate_frontend_v1.py testdata/straight01.json   # schema check
python3 tools/validate_frontend_v1.py --selftest                 # validator self-test
```

Regenerating goldens after an *intentional* wire change (must ship with a
[`docs/frontend-v1.md`](../../docs/arch/frontend-v1.md) update in
the same change):

```sh
UPDATE_GOLDEN=1 ./build.sh test
```

## Usage

```sh
./gofrontend path/to/main.go > ast.json
```

Exit codes: 0 success · 1 usage error · 2 frontend error (parse/type/scope;
details on stderr, no JSON on stdout). Scope errors name the workstream that
owns the extension (e.g. `if` → WS E, imports → WS G).

## Disk imports: module-aware version selection

Imports not claimed by the synthetic overlay resolve from local disk
([`diskimport.go`](diskimport.go), [`diskmodule.go`](diskmodule.go)):

- **Inside a module** (a `go.mod` is found by walking up from the package
  being compiled): the version of every third-party import comes ONLY from
  that `go.mod`'s `require` list, with `replace` applied. The exact
  required version is read from the module cache; a module that `go.mod`
  does not require, a required version absent from the cache, or a
  missing replacement directory is a loud error, never a silent fallback
  to another version. Packages of the main module itself resolve to the
  module tree. Standard-library imports always resolve from the toolchain
  GOROOT, never from the cache or GOPATH.
- **Outside any module** (standalone files, corpus fixtures): legacy
  behaviour — newest version in the module cache, then `GOPATH/src`,
  then GOROOT — because there is no `go.mod` to select from.

Build inputs, not ambient defaults (review 2026-11-04, P1-04/P1-05):

- `--modcache=DIR` pins the module-cache location (default: `GOMODCACHE`,
  else the `GOPATH` default). The location can affect availability, never
  the selected version.
- `--goroot=DIR` pins the toolchain GOROOT. Without it, the CLI clears the
  ambient `GOROOT` variable at startup so stdlib resolution uses the
  toolchain that built `gofrontend` — `runtime.GOROOT()` consults the
  environment first, so ambient env would otherwise redirect it.
- `--build-tags="TAG ..."` adds build tags for disk-import file selection
  (default: `GRAALGO_BUILD_TAGS`). Tags are a build input like `-tags`: a
  build with different tags is a different build. The launcher may pass
  all three flags explicitly to pin CI builds.

`go.sum` is not consulted: version selection is `go.mod`'s job, and cache
directories are populated (and hash-verified) by `cmd/go`.

## Java side

`NodeFactory` + node classes live in
[`../backend/truffle/src/main/java/graalgo/frontend/`](../backend/truffle/src/main/java/graalgo/frontend/)
(worker-1 owns `make`/`out/` — verify to `/tmp` scratch, do not run `make`):

```sh
CP=$(ls ../../phases/phase1/lib/*.jar | tr '\n' ':')
javac -Xlint:deprecation -Werror -cp "$CP" -d /tmp/wsB-classes ../backend/truffle/src/main/java/graalgo/frontend/*.java
javac -Xlint:deprecation -Werror -cp "$CP:/tmp/wsB-classes" -d /tmp/wsB-classes javatest/NodeFactoryTest.java
java --enable-native-access=ALL-UNNAMED -cp "/tmp/wsB-classes:$CP" NodeFactoryTest javatest/
```

`NodeFactoryTest` builds both golden programs from JSON, executes them on the
Truffle runtime, and asserts gc-oracle values (`z == 42`; straight02 `sum ==
194`), node shapes, `SourceSection` ↔ exporter position agreement, plus
negative cases (unknown kind, bad version, bad op, malformed JSON, duplicate
registration) and `Json` unit checks.

## GG status

GG-0012/GG-0013/GG-0044 stay **locked**: WS B sources are landed and tested,
but activation happens at M2 when `NodeFactory` builds straight-line programs
end-to-end through the `graalgo-run` launcher (per PLAN.md). See
[`../../phases/phase1/CHECKPOINT.md`](../../docs/arch/phases/phase1/CHECKPOINT.md) (WS-B section).
