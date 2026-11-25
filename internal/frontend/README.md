# gofrontend — Go frontend service (WS B)

> **v1, frozen, deleted at M3.** This is the v1 frontend. No new
> `synth_*.go` may be added; the whole directory (gofrontend, JSON wire
> format, synthetic stdlib, generators, `p3frontend/`, `v2/`) is deleted at
> the M3 cutover ([toolexec-design.md](../../docs/arch/toolexec-design.md)
> D11). New compiler work goes to [internal/toolexec/](../toolexec/).

Type-checks a single-file Go program with the vendored `go/*` packages and
emits the `frontend/v1` typed-AST JSON document to stdout.

- Wire format: [`../../docs/archive/frontend-v1.md`](../../docs/archive/frontend-v1.md) (normative)
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
./build.sh check   # gofmt + go vet + go test + vendoring diff gate + frontend/v1 wire-format validation (all must pass)
./build.sh test    # go test only
python3 tools/validate_frontend_v1.py testdata/straight01.json   # schema check (one doc; build.sh check validates all goldens + fresh exports)
python3 tools/validate_frontend_v1.py --selftest                 # validator self-test (incl. exporter kind-coverage)
```

Regenerating goldens after an *intentional* wire change (must ship with a
[`docs/archive/frontend-v1.md`](../../docs/archive/frontend-v1.md) update in
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

## Java side

`NodeFactory` + node classes live in
[`../backend/truffle/src/main/java/graalgo/frontend/`](../backend/truffle/src/main/java/graalgo/frontend/).
`NodeFactoryTest` is not in any gate (v1, frozen); to run it by hand after
`make build`:

```sh
CP=$(ls ../backend/truffle/lib/*.jar | tr '\n' ':')
OUT=$(mktemp -d)
javac -Xlint:deprecation -Werror -cp "../backend/truffle/out/classes:$CP" -d "$OUT" javatest/NodeFactoryTest.java
java --enable-native-access=ALL-UNNAMED -cp "$OUT:../backend/truffle/out/classes:$CP" NodeFactoryTest javatest/
```

`NodeFactoryTest` builds both golden programs from JSON, executes them on the
Truffle runtime, and asserts gc-oracle values (`z == 42`; straight02 `sum ==
194`), node shapes, `SourceSection` ↔ exporter position agreement, plus
negative cases (unknown kind, bad version, bad op, malformed JSON, duplicate
registration) and `Json` unit checks.

## GG status

The requirement status of GG-0069 (which superseded `GG-0066` and, before it,
`GG-0012`), GG-0013 and GG-0044 lives in [requirements/](../../requirements/)
(`tools/req trace GG-0069`); this
README does not duplicate it. History:
[CHECKPOINT.md](../../docs/archive/phases/phase1/CHECKPOINT.md) (WS-B section).
