# The `-toolexec` compile/link path — design (lane T1)

**Status:** design, decision-complete except where §12 says a prototype is
needed. Written 2026-11-17 for track T1 of [RECOVERY.md](RECOVERY.md);
amended 2026-11-18 with the owner's decisions and the results of the two
prototypes of §12 ([abi-spike.md](abi-spike.md)) — the change list is §14.
[SPEC.md](SPEC.md) is authoritative; this document implements SPEC §3.1–3.3,
§3.6, §4.1–4.2, §4.4 and requirements GG-0001/0002/0009/0011/0066/0013/0014/
0020/0021/0028/0059. Every number below was measured on this box against
go1.27.1 linux/amd64 (the tracked release) unless it is labelled an estimate;
the measurement programs are described in §11.

## 0. Summary of decisions

| # | Decision | Why (short) |
|---|---|---|
| D1 | `graalgo` is a ~200-line launcher that `exec`s the stock `go` binary with `-toolexec` injected. No command is reimplemented. | GG-0011. All 19 commands, flags, modules, cache, `go test` protocol come from stock `cmd/go` by construction. |
| D2 | One wrapper binary, `graalgo-tool`, built by the tracked gc toolchain, handles every tool invocation. `compile` and `link` are intercepted; `asm`, `cgo`, `cover`, `vet`, `preprofile` pass through unchanged. | Spike 5 mapped the seam; §2 re-measures it. |
| D3 | **Shadow compile.** For `compile`, the wrapper first runs the stock gc compiler with the unmodified argv, then type-checks the same files with `go/types` and appends a typed-IR member to the archive gc wrote. | gc export data, build IDs, `-asmhdr`, `-symabis`, diagnostics and exit codes are stock by construction. go1.27's `go/importer` reads only unified-IR export data and no go/types-based writer for it exists (§4.1). Cost: 45 CPU-seconds for all of std, once per cache. |
| D4 | `link` is replaced: it concatenates the IR members of every archive in `importcfg.link` into a bundle, appends it to a prebuilt ELF launcher stub and patches the build ID into the stub's note. | The output is a real executable that `go run`/`go test` can exec and whose build ID stock `cmd/go` can read and rewrite. |
| D5 | The IR is a **resolved typed AST** (not SSA, not gc's unified IR), one independently decodable blob per function, in a schema-driven binary container that evolves v2's `g2bi` encoding. | SPEC §3.2/§3.3: the node factory walks the typed AST. Lazy per-function decoding keeps startup proportional to executed code. |
| D6 | Generics: generic bodies are serialized **once, uninstantiated**; the backend instantiates per distinct type-argument tuple at first use (full monomorphization in the JVM). AOT modes run the same instantiation as a link-time closure. | A Truffle tree per instantiation is what the JIT wants; no cross-package duplicate bodies; no dictionaries. |
| D7 | Types cross package boundaries by **canonical type key**; descriptors are interned at load; itabs are computed lazily at run time. No link-time itab or method-set generation. | Itabs are immutable (SPEC §3.3); the JVM is the linker. |
| D8 | The runtime boundary is the **source tree**: graalgo ships a `GOROOT` whose `src` is upstream go1.27.1 plus the fork patch stack. `runtime` and `internal/runtime/*` are replaced by facade packages whose body-less functions are the intrinsic surface; a small set of coupled packages carries minimal patches; everything else is verbatim. `internal/abi` and `internal/reflectlite` are verbatim and `reflect` is verbatim except a 4-function call-path patch, on byte-exact descriptor images and the pointer model of §3.4 (P1, answered). | GG-0009. No package is name-keyed inside the tool: the tool compiles whatever `cmd/go` hands it. Verified: stock `cmd/go` + gc `compile` + `vet` accept a facade `runtime` (§5.2); verbatim reflect on synthesized descriptors: 607/607 answers identical to gc ([abi-spike.md](abi-spike.md) §1.4). |
| D9 | Body-less functions resolve at link time, by symbol name, to (a) a Go body named by `//go:linkname`, or (b) a Java intrinsic registered under that symbol name; otherwise the link fails if the function is reachable. | GG-0020/GG-0021. Same rule for std, the fork and third-party modules. |
| D10 | The frontend is `go/parser` + `go/types` from the toolchain that builds `graalgo-tool`, with a build-time assertion that this is the tracked release. | GG-0066/GG-0013 by construction; the vendored copy under `internal/frontend/go` retires with v1. |
| D11 | v1 (`gofrontend`, JSON, `synth_*.go`, the custom `cmd/graalgo` commands, **and v1's node classes**) is deleted at one cutover gate (§9, M3), not migrated piecemeal. | The two paths share only the runtime library (scheduler, channels, timers); a hybrid would need the import overlay the recovery is removing. |
| D12 | The node factory of W4 targets a **new node package** written to the Truffle conventions of [truffle-design.md](truffle-design.md) §11–§12 (statically typed node families, DSL specializations only at dynamic sites, `TruffleLanguage`-native); the existing ~110 node classes are the semantic reference, not reused, and are deleted at M3. | Owner decision 2026-11-18 (truffle-design T13). Reusing them would carry every PE-hostile pattern of truffle-design §1.2 and the nested-`GoStruct` value model (§3.4) into the new path and force a second rewrite. Cost: +6–10 agent-days on M1a. |

## 1. The launcher (`graalgo`)

`graalgo ARGS…` does exactly this and nothing else:

1. Locate `GRAALGO_HOME` (directory of the executable, or the environment
   variable).
2. Set `GOROOT=$GRAALGO_HOME/goroot` (§5.1), `GOTOOLCHAIN=local` unless the
   user set it, `CGO_ENABLED=0` unless the user set it (until the cgo→Panama
   lane lands, §7.4).
3. Prepend `-toolexec=$GRAALGO_HOME/libexec/compile` and `-tags=purego` to
   `GOFLAGS` (user `GOFLAGS` entries come after, so they keep stock
   precedence; `cmd/go` ignores GOFLAGS entries a subcommand does not
   define).
4. If the user's argv or `GOFLAGS` already carries `-toolexec=X` (spike 5 F5:
   last one wins, no chaining), rewrite it to
   `-toolexec="$GRAALGO_HOME/libexec/compile -chain X'"` so the user's wrapper
   runs around the graalgo tool. The same merge applies to `-tags` (a user
   `-tags=a,b` becomes `-tags=a,b,purego`). These two flags are the only argv
   the launcher parses.
5. `execve` `$GOROOT/bin/go` with the original argv.

`$GRAALGO_HOME/libexec/compile` is `graalgo-tool` installed under a tool
basename, because `cmd/go` decides whether to use response files from the
basename of `argv[0]` of the command it execs (spike 5 F6a).

As built (W1, [cmd/graalgo-launcher/](../../internal/toolexec/cmd/graalgo-launcher/),
[launcher/](../../internal/toolexec/launcher/)):

- "Unless the user set it" means set in the environment **or** in the user's
  go env file (`$GOENV`, written by `go env -w`); `GOROOT/go.env` holds
  distribution defaults (`GOTOOLCHAIN=auto`), which the launcher overrides.
  Because the launcher always sets `GOFLAGS` in the environment, which hides
  the env file's `GOFLAGS`, the file's value is read and composed.
- `X'` is the user's `-toolexec` value percent-encoded (spaces and quotes
  escaped). `quoted.Split` has no escapes, so a quoted `X` could not be
  nested inside a `-toolexec` value inside a quoted `GOFLAGS` field.
- Chain semantics: graalgo-tool runs **every stock tool** it executes
  (shadow compile, pass-through tools, `-V=full` probes) as `X <tool>
  <args>`, so the user's wrapper sees the argv it sees under stock `go`.
  graalgo's own link step executes no stock tool and is not wrapped.
- argv scanning covers the subcommands that define build flags, skips the
  values of value-taking flags, and stops at `--`, at `go test`'s `-args`,
  and for `go run` at the first non-flag argument (program arguments).
- Identity: `graalgo version` prints the stock line with
  ` graalgo-<version>` appended; `go env` is stock and shows the injected
  `GOROOT`, `GOFLAGS`, `GOTOOLCHAIN` and `CGO_ENABLED`. `GRAALGO_PUREGO=0`
  drops the `purego` tag (R10).
- The launcher refuses a `GOROOT` whose `VERSION` is not the tracked release,
  and asserts (R8) that the composed `GOFLAGS` ends with graalgo-tool as
  its effective `-toolexec`.

`go tool <name>` does not go through `-toolexec` (spike 5 §4); it runs the
stock tools from `$GOROOT/pkg/tool`, which is correct for `go tool vet`,
`cover`, `pprof`, `test2json`. `go tool compile`/`link` invoked directly by a
user produce gc objects; that is documented as a non-goal.

`GOTOOLCHAIN` switching re-execs a *different* stock distribution whose
`GOROOT/src` has no fork. `graalgo-tool` detects this (the `-goversion` flag
on `compile`, or the `go` binary's `GOROOT` differing from
`$GRAALGO_HOME/goroot`) and fails with one line naming the tracked version.
As built, three checks fire: a toolchain tool outside `$GRAALGO_HOME/goroot`
(installed layout), a `-V=full` line of another release, and `compile
-goversion` of another release.
Faithful selection (SPEC §6.8) belongs to track T4.

## 2. Tool protocol (measured)

Measured with a logging wrapper on a three-package module (one generic
package, one package with a `.s` file, `package main` importing both and
nothing else), `go build`, `go vet ./...`, `go test -cover`:

| Command | compile | asm | link | vet | cover | `-V=full` probes |
|---|---|---|---|---|---|---|
| `go build` (cold cache) | 33 | 42 | 1 | 0 | 0 | 3 (compile, asm, link) |
| `go build` (warm cache) | 0 | 0 | 0 | 0 | 0 | 3 |
| `go vet ./...` | 44 | 17 | 0 | 75 | 0 | 3 (compile, asm, vet) |
| `go test -cover ./lib` | 76 | 20 | 1 | 73 | 2 | 5 |

A `package main` that imports nothing still compiles 30 std packages:
`runtime` and its 29 dependencies (the other three are the module's own). Every program therefore exercises the runtime
boundary of §5 from the first milestone.

### 2.1 `-V=full` and cache identity (GG-0028)

`cmd/go` runs `<wrapper> <tool> -V=full` for each tool on every invocation,
including fully cached ones, and hashes the whole output line into every
action ID (for release toolchains; `devel` lines use the trailing build ID
instead). Stock answers are `compile version go1.27.1` and
`link version go1.27.1`.

`graalgo-tool` answers, for every tool it is asked about:

```
<tool> version go1.27.1 graalgo-<irVersion>-<toolHash>
```

where `toolHash` is the first 16 hex digits of SHA-256 over the
`graalgo-tool` executable. For `link` the hash additionally covers the
runtime distribution manifest (`$GRAALGO_HOME/lib/MANIFEST.sha256`, the
hashes of the launcher stub and backend jars), because `go test` caches
results by the test binary's content ID and a runtime change must invalidate
them. Passed-through tools (`asm`, `cgo`, `cover`, `vet`) get the same
suffix: cheap, and it keeps graalgo and gc entries disjoint in a shared
`GOCACHE` without relying on which tool happens to be in an action's inputs.

Fork changes need no extra handling: forked sources live under `GOROOT/src`,
so their content already enters the action IDs of the packages they belong
to.

### 2.2 `compile`

Representative argv (user package; std packages add `-std`, packages with
assembly add `-symabis`/`-asmhdr` and drop `-complete`):

```
compile -o $WORK/b003/_pkg_.a -trimpath "$WORK/b003=>" -p example.com/mod1/lib
  -lang=go1.25 -complete -buildid 9kmT…/9kmT… -goversion go1.27.1 -c=16
  -nolocalimports -importcfg $WORK/b003/importcfg -pack ./lib.go
```

`TOOLEXEC_IMPORTPATH` carries the import path (for tests: `pkg [pkg.test]`).
`importcfg` holds `packagefile <path>=<archive>` and `importmap <src>=<dst>`
lines for the *direct* imports only.

Procedure:

1. Expand a response file if present (rewriting it in place without the tool
   path, spike 5 F6b; go1.27 uses GCC-style quoting, spike 3 F8).
2. Run the stock `compile` with the argv unchanged. Its stdout, stderr and
   exit status are the wrapper's. On non-zero exit, stop.
3. Parse the flags the IR pass needs: `-o`, `-p`, `-lang`, `-importcfg`,
   `-std`, `-trimpath`, `-embedcfg`, `-coveragecfg`, `-race`, and the file
   list. Unknown flags are ignored by the IR pass (gc already validated
   them).
4. Parse with `go/parser`, type-check with `go/types`
   (`Config{GoVersion: -lang, Sizes: SizesFor("gc", GOARCH)}`; per-file
   `//go:build go1.N` versions are handled by `go/types` itself). The
   importer is `importer.ForCompiler(fset, "gc", lookup)` where `lookup`
   opens the archive named by `importcfg` — i.e. it reads the export data
   the shadow compile of each dependency wrote.
5. Emit the IR (§3) and append it to `-o` as one more `ar` member named
   `__.GRAALGOIR`.
6. A type error from `go/types` after gc succeeded is an internal error
   (exit 2, message prefixed `graalgo-tool: internal:`): the two checkers are
   generated from the same source and must agree.

Why the archive stays valid for every stock consumer: `cmd/go` appends
assembly objects in-process after the compile step and then rewrites the
build ID by scanning the file for the ID string; both operate on the `ar`
container and are indifferent to an extra member. `vet`, `gopls` and
`go list -export` consumers read only `__.PKGDEF`. The content ID is a hash
of the whole file, so it covers the IR.

### 2.3 `asm`

Passed through. gc's `compile` needs the `symabis` file produced by
`asm -gensymabis` to accept body-less declarations, and `cmd/go` expects the
`.o` files to exist. The objects are packed into the archive and ignored by
the graalgo linker. Assembly never executes; §5.4 covers what replaces it.

### 2.4 `link`

```
link -o $WORK/b001/exe/a.out -importcfg $WORK/b001/importcfg.link
  -X=runtime.godebugDefault=… -buildmode=exe
  -buildid=5yOj…/axHn…/OZTI…/5yOj… -extld=gcc $WORK/b001/_pkg_.a
```

`importcfg.link` lists `packagefile` lines for the **transitive** closure
(plus a `modinfo` line). `go test` adds `-s -w` and
`-X testing.testBinary=1`.

Procedure (§4.2 for the bundle): read every archive's `__.GRAALGOIR`;
resolve symbols (§5.3); compute package init order; apply `-X`; write
`stub ++ bundle ++ trailer` to `-o`; patch the build ID.

Build ID: `cmd/go` passes an 83-byte ID
(`actionID/actionID/contentID/contentID` in base64-ish 20-character fields),
then re-reads the output, hashes it, and overwrites every occurrence of the
ID in place with the final content ID. For ELF files it reads the ID from
the `.note.go.buildid` note only. The launcher stub is therefore built with
`-ldflags=-buildid=<83-byte placeholder>` and the linker overwrites the
placeholder inside the note; a length mismatch is a hard error. Accepted and
ignored: `-s`, `-w`, `-extld`, `-extldflags`, `-linkmode`, `-compressdwarf`,
`-installsuffix`. Rejected loudly: every `-buildmode` other than `exe` and
`pie` until the corresponding lane exists (`plugin`: GG-0051, `c-shared`/
`c-archive`: interop track).

### 2.5 `vet`, `cover`, `cgo`, `preprofile`, `pack`

- **vet**: passed through (SPEC §7.10). It consumes `vet.cfg` and gc export
  data, both stock. Verified also against a facade `runtime` (§5.2).
- **cover**: passed through. `cover -pkgcfg` rewrites sources into `$WORK`
  and the subsequent `compile` receives the instrumented files plus
  `-coveragecfg`. The IR pass must then do what gc's compiler does with that
  flag: emit the coverage meta-data blob and the `init`-time registration
  call. This is milestone M5 work; until then `-coveragecfg` on a
  `compile` makes the IR pass fail with a single-line "coverage not yet
  supported" error rather than produce a binary that silently reports 0%.
- **cgo**: passed through; the generated Go files reach `compile` like any
  other source. Execution of the generated stubs belongs to the cgo→Panama
  lane (GG-0022). Until then the launcher defaults `CGO_ENABLED=0`, which is
  a stock configuration (pure-Go `net` resolver, `os/user` fallbacks).
- **preprofile** (PGO): passed through; `-pgoprofile` is ignored by the IR
  pass (the JVM profiles at run time).
- **pack**: in-process in `cmd/go`; nothing to intercept.
- The host C compiler probe arrives as a bare `gcc`; the wrapper resolves
  slash-less tool names through `PATH` (spike 5 F2).

### 2.6 Race, msan, asan

`-race` makes `cmd/go` add the `race` build tag, the `runtime/race`
dependency and `-race` on `compile`/`link`. graalgo has no detector.
Decision: `link -race` fails with one line ("the graalgo backend has no race
detector; set GRAALGO_RACE=ignore to build without one"). With that opt-in
the IR pass ignores the flag and the forked `runtime/race` is the no-op
variant. Silently building an uninstrumented binary by default would make
`go test -race` pass vacuously.

## 3. Per-package IR

### 3.1 Level: resolved typed AST

The IR is the go/ast tree annotated with everything `go/types` computed, in
resolved form. It is **not** lowered to a CFG or SSA and is not gc's unified
IR. Reasons:

- SPEC §3.2/§3.3 and GG-0014: one Truffle AST built by a node factory that
  walks the typed AST. Truffle wants structured control flow; lowering in
  the frontend would be undone in the backend.
- The existing Java [NodeFactory.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java)
  (6,100 lines) and its ~110 node classes already consume this level, which
  proves the level is sufficient; the new node package of D12 consumes the
  same level and uses them as the semantic reference.
- gc's unified IR is private to `cmd/compile`, changes every release and
  encodes gc-specific decisions (shapes, dictionaries, inlining bodies).

What the frontend resolves (so that the backend never re-implements
`go/types`):

| Source construct | IR form |
|---|---|
| identifier | one of: local (function-local slot index), package-level object of this package (declaration index), imported object (symbol reference, §3.3), builtin, universe constant |
| every expression | type id; if `go/types` reports a constant value, the exact constant (big integers and rationals as strings) instead of the expression tree |
| selector | field path (indices through embedded fields, with the implicit dereferences), static method call, interface method call, method value, method expression, or qualified identifier |
| implicit conversions | explicit nodes: value→interface, untyped constant→typed, channel direction narrowing |
| call of generic function / use of generic type | callee symbol + explicit type-argument list from `Info.Instances` |
| `range`, `switch`, `select`, `defer`, `go`, labels | kept structured, one node kind each |
| closures | function literal node with its capture list (captured locals, by reference) computed by the frontend |
| address-taken / captured locals | flagged on the declaration (the backend boxes them) |
| package-level variable initialization | ordered list from `Info.InitOrder` (§3.6) |

What the frontend deliberately does **not** do: any lowering whose result
depends on the dynamic or instantiated type of an operand (operator
selection, zero values, conversions, map/slice/channel element handling).
Those decisions are taken by the node factory when it builds a tree, because
under D6 the same IR is built again for each instantiation.

No stdlib call is desugared, no package or symbol name is special-cased
(AGENTS §4, frontend-v2 rules).

### 3.2 Container (`GIR`, evolves v2's `g2bi`)

v2's [encode.go](../../internal/frontend/v2/encode/encode.go) serializes a
generic tree of maps with string keys; every node repeats its field names
(hello-world is 915 bytes). std has 2.48 million AST nodes, so that shape
would produce hundreds of megabytes. The container therefore changes while
the primitives (varints, zig-zag integers, length-prefixed UTF-8, the magic
and version header checked loudly by
[BinaryDecoder.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/v2/BinaryDecoder.java))
stay:

```
GIR file := magic "GIR1" | irVersion | goVersion | GOOS | GOARCH | section*
sections:
  STRINGS   interned strings, referenced by index everywhere else
  PKG       import path, package name, files (trimpath-applied), direct imports,
            flags (-std, has cgo), linkname directives, embed data index
  SYMREFS   symbols this package references: (import path, name, kind)
  TYPES     structural type table; named types are (symref, type-args);
            per type (named or not) the descriptor facts of §3.4
  TYPEDEFS  for named types DECLARED here: underlying type, type parameters,
            declared methods (name -> func index), full method sets with
            embedding paths, gc layout (size, align, field offsets)
  OBJECTS   package-level consts/vars/funcs: name, type, exported, body-less
  INIT      ordered initializers + init functions (§3.6)
  FUNCINDEX per function: name, receiver, signature type, type params,
            flags (body-less, generic, closure-of), offset+length into BODIES,
            referenced symrefs (for link-time reachability)
  BODIES    concatenated per-function blobs, each independently decodable
  DATA      embedded files, large constant strings
```

Nodes inside a body blob are `kind` (varint) followed by that kind's fields
in schema order — no field names on the wire. The schema is **one checked-in
table** (`internal/toolexec/ir/schema.txt`: kind number, name, typed field
list) from which both the Go encoder and the Java decoder are generated, so
a field added on one side cannot be missed on the other. Golden vectors are
shared by the Go and Java test suites, as v2 already does for `g2bi`.

Size estimate: 4–8 bytes per node gives 10–20 MB for all of std and
1–1.5 MB for the 59-package closure of `fmt` (163 k source lines). The
per-function index is what makes startup cheap: a hello-world executes about
300 of the 5,700 functions in that closure outside the runtime (§5.5).

`irVersion` participates in the `-V=full` answer (§2.1), so an IR format
change invalidates cached archives automatically.

**As built (W2).** [ir/schema.txt](../../internal/toolexec/ir/schema.txt)
is the single checked-in table; [ir/schema/](../../internal/toolexec/ir/schema/)
parses it and generates the Go codec
([ir/gir_gen.go](../../internal/toolexec/ir/gir_gen.go)) and the Java
decoder/encoder ([internal/backend/gir/](../../internal/backend/gir/),
JDK-only) — `go generate ./ir`, run by `make generate`; a test fails when a
checked-in output is stale. Refinements of the sketch above:

- Header: `"GIR1" | schema version | 8-byte schema hash | goVersion | GOOS |
  GOARCH | section directory`; every section is present in id order.
  Varints must be minimal, bools 0/1, string 0 is `""`, bodies are
  contiguous in FUNCINDEX order — so decode followed by encode is the
  identity, which the Java tests check on every golden vector, every std
  package and every accepted fuzz mutant. Every reference is range-checked
  at decode time (table sizes are read first, so references may point
  forward); list counts are bounded by the remaining input; nesting is
  bounded by the schema's `maxdepth` (1000; std needs 36). The decoders
  throw only their format error for any input.
- `irVersion` in `-V=full` is `gir<version>.<schema hash>`, so any schema
  edit invalidates cached archives even without a version bump.
- Function literals are encoded inline in the enclosing body (a `FuncLit`
  node with its own scope); captured variables are the first locals of the
  literal's scope, bound to the listed enclosing locals.
- Positions are a per-body table (file once per body, line deltas), indexed
  1-based from every node: the packed position index of truffle-design §6.2.
- Constructs the sketch did not name: `TupleConvert` (element-wise implicit
  conversion of a multi-value expression), struct-literal field *paths*
  (go1.27 allows promoted fields as keys), and non-constant
  `unsafe.Sizeof/Alignof/Offsetof` (operands of type-parameter type) as
  builtins. Method calls carry an already adjusted receiver (`&`/`*`
  through the embedding path); calls of a method of a type-parameter value
  are `IfaceCall`s on that value, resolved per instantiation (D6).

Measured (P3, `girstat` over the 374 non-empty std packages, purego, no
cgo, this box): the emitter lowers every function body without an
unsupported construct; GIR totals 20.4 MB (bodies 72 %, strings 21 %,
types/typedefs/index 6 %), 10.7 bytes per AST node; encode 140 MB/s, Go
decode+validate 120 MB/s, Java decode+validate 94 MB/s on a cold JVM;
average body 713 bytes. No compression is needed for bundle layout.

### 3.3 Symbols and linkage

A symbol is `(import path, name)` with gc's naming: `pkg.F`, `pkg.T.M`,
`pkg.(*T).M`, `pkg.v`. The import path is the `-p` value, so `package main`
is `main` and test variants are distinct packages exactly as in gc. Function
literals and local types are not symbols; they are addressed through their
enclosing function.

`//go:linkname local remote` is recorded verbatim in `PKG`. The linker
builds one table over all packages (§5.3). Go 1.23+'s restriction that
linkname targets must be marked is enforced by the shadow gc compile and its
linker would enforce the rest; the graalgo linker reproduces the
"reference to unmarked symbol" check from the recorded directives so that
programs gc rejects are rejected here too.

### 3.4 Types, method sets, itabs across packages

- Every type has a **canonical key**: a string in gc's link-name form with
  full import paths (`map[string]*net/http.Request`), type arguments
  included, local types suffixed with their enclosing function and
  declaration index. The key is computed by the frontend and stored once per
  type-table entry.
- At load the backend interns one descriptor per key in a process-wide
  table. Two packages that both mention `[]io.Reader` get the same
  descriptor object; type identity is reference equality afterwards.
- A named type's definition (underlying type, methods, layout) lives only in
  the IR of the package that declares it. Referencing packages carry the
  key and the symref. Loading a reference forces loading the declaring
  package's `TYPEDEFS` entry, not its function bodies.
- Method sets: the frontend emits, for each named type and for each struct
  type with embedded fields that is converted to an interface, the complete
  value and pointer method sets with the embedding path for promoted
  methods. The backend synthesizes promotion wrappers from the paths. It
  never runs a method-set algorithm of its own.
- Itabs are built lazily at the first conversion or assertion of a
  (concrete descriptor, interface descriptor) pair and cached; they are
  immutable (SPEC §3.3). Nothing about itabs is computed at compile or link
  time.
- gc layout numbers (`unsafe.Sizeof/Offsetof/Alignof`) come from
  `types.SizesFor("gc", GOARCH)` and are folded into constants by the
  frontend.
- **Descriptor facts, for every type** (not only named ones), so that the
  backend can build gc's descriptor image without a compiler run
  ([typetab](../../test/spikes/abi/descview/typetab/main.go) is the
  reference): gc type string and link string, kind, size, alignment and
  field alignment, pointer bitmap (`PtrBytes`, GC mask), comparable /
  regular-memory / direct-interface flags, struct package path, field
  names, offsets, tags, embedded and exported bits, and the complete sorted
  method sets with the receiver-less signature type of each method. The
  layout of `internal/abi`'s own structs (`Type`, `UncommonType`, `Method`,
  `Name`, …) comes from the same table applied to `internal/abi`; **the
  backend hard-codes no descriptor offset**, so a Go release that moves a
  field needs no backend change.
- **Pointer model** (SPEC §3.4, GG-0061/GG-0062). Values are flat: a
  struct or array is one `long[]` of scalar leaves plus one `Object[]` of
  reference leaves, with nested structs and arrays inlined; a per-type
  **leaf table** derived from the gc layout maps a gc byte offset to a slot.
  An `unsafe.Pointer`, and every `*T` obtained from one by conversion, is
  `(referent object, gc byte offset)`. Every addressable object kind —
  flat values, arrays, descriptor images, cells, and the three header shapes
  (interface: type word + data word; string: data + len; slice: data + len
  + cap) — implements load/store of an N-byte scalar and load/store of a
  pointer word at a byte offset. A field access through a view pointer is a
  load at `ptr.offset + Offsetof(field)`. `uintptr(p)` is a stable integer
  from a range-allocating handle table (each address-taken object gets a
  range above any real user address; integer → pointer is a floor lookup).
  Leaving the referent traps with gc's `checkptr` fatal errors (SPEC §3.4).
  v1's nested-`GoStruct` model is **not** carried into W4: it is the
  measured cause of 20 of the 50 reflect-probe failures
  ([abi-spike.md](abi-spike.md) §1.2).

### 3.5 Generics

std contains 359 generic functions or methods of generic types and 1,810
instantiation sites (measured, §11). go1.27 also has generic methods
(`math/rand/v2`).

Decision D6: the declaring package's IR holds each generic body once, with
type-table entries that refer to type parameters by (owner, index).
Instantiation sites carry explicit type arguments, which may themselves
mention the enclosing function's type parameters. The backend keeps a table
`(function symbol, canonical keys of type arguments) → call target`; on a
miss it decodes the body and builds a Truffle tree under a type environment
that maps parameters to descriptors. Methods of generic types are
instantiated per method on demand, not per type.

Alternatives rejected:

- *Frontend monomorphization into the using package* (what v1's
  `mrdesugar_m5.go` does for a whole program): the using package's compile
  does not have the generic body — export data carries signatures only — so
  it would have to read and rewrite the dependency's IR, and identical
  instantiations would be emitted by many packages and deduplicated at link.
- *gc-style shapes + dictionaries*: exists to bound native code size. A
  Truffle AST specialized per concrete type is strictly better for partial
  evaluation, and trees are built only for instantiations that execute.

Consequence for closed-world AOT (T3): the linker gains an optional pass
that computes the instantiation closure from `main` over `FUNCINDEX`
symrefs and type arguments (a standard worklist; Go rejects programs with
infinite instantiation), so the image builder sees every tree. JIT mode
does not need it.

Cross-package inlining needs no IR support: every body is available to the
runtime and Truffle inlines across package boundaries. gc's inline bodies in
export data are ignored.

### 3.6 Initialization

- Within a package: `go/types` computes `Info.InitOrder` (dependency-ordered
  package-variable initializers); the frontend emits that list followed by
  the `init` functions in the order the files were given on the command line
  (cmd/go passes them sorted) and source order within a file. This replaces
  v2's source-order emission (an open item in
  [PROGRESS.md](../../internal/frontend/v2/PROGRESS.md)).
- Across packages: the linker orders packages as the gc linker does since
  go1.21 — sort by import path, then repeatedly initialize the first package
  whose imports are all initialized — and stores the sequence in the bundle
  manifest. `runtime` first.
- Package variables live in one globals object per package, created when the
  package is initialized. Initialization is eager and complete before
  `main.main`, as the language requires; only body *decoding* is lazy.
- `//go:embed`: `-embedcfg` maps patterns to files; the IR pass copies file
  contents into `DATA` and emits initializers for the `string`, `[]byte`
  and `embed.FS` variables.
- `link -X pkg.name=value`: recorded in the manifest and applied before the
  package's initializers, with gc's rule that only string variables with no
  or constant initializer are settable.
- The main module's build info reaches the program the stock way: `cmd/go`
  generates `_gomod_.go` for the main package, which is ordinary source.

## 4. Export data and the bundle

### 4.1 Export data: produced by the shadow gc compile

Consumers of export data are (i) dependent packages' type-checking, (ii)
stock `vet`, (iii) external tools via `go list -export` (gopls, staticcheck).

Measured facts:

- go1.27's `go/internal/gcimporter` contains only `ureader.go`; the indexed
  ("i") format that `golang.org/x/tools` can write from `go/types` is no
  longer readable. A graalgo-written export file would have to be unified IR,
  for which only `cmd/compile`'s types2-based writer exists.
- `importer.ForCompiler(fset, "gc", lookup)` with a lookup over
  `go list -export` archives type-checks **382 of 382** std packages from
  source in 3.9 s single-threaded.
- Building all of std with stock gc costs 45 CPU-seconds (20 s wall on 16
  cores).

Decision D3 follows: let gc write its own export data. The alternative — a
unified-IR export writer for `go/types` (estimated 1,000–1,500 lines against
`internal/pkgbits`, re-validated every release by round-tripping std) — is a
self-contained optimization that can replace the shadow compile later
without touching anything else in this design; it is listed in §12 as
optional work, not as a dependency.

The shadow compile has one further benefit that is hard to obtain otherwise:
compile errors, their order, the ten-error limit and `-gcflags` output are
gc's own.

Constraint accepted: every package, including the fork, must be valid input
for gc's compiler. §5.2 shows that this holds even for a facade `runtime`.

### 4.2 Bundle and executable layout

```
output := ELF launcher stub (prebuilt, static, gc-compiled)
          | bundle
          | trailer (bundle offset, length, "GGB1")
bundle := manifest | per-package GIR files, concatenated, uncompressed
manifest := format version, runtime manifest hash, GOOS/GOARCH,
            main package, package table (import path -> offset, length),
            init order, -X assignments,
            resolved symbol table for linknames and intrinsics (§5.3)
```

The linker copies package IR byte-for-byte; link time is I/O-bound and
linear in bundle size. Package files stay uncompressed so the runtime can
map the executable and decode function bodies in place.

The stub locates the runtime (`GRAALGO_HOME` at run time, else the path
baked at link time, as the current
[artifact.go](../../internal/driver/artifact.go) launcher does) and execs
the JVM with the bundle path, `argv[0]` set to the executable's own path.
In JIT mode this is the only packaging step. For the AOT and hybrid modes
(SPEC §3.9, track T3) the same bundle is the input to the image build; the
recommended first AOT deliverable is a **generic runtime image** built once
per graalgo release that loads bundles at startup, because it removes JVM
startup from every `go run`/`go test` without a per-program image build.

### 4.3 Backend loading

```
BundleLoader      maps the bundle, reads the manifest
PackageUnit       per package: strings, symrefs, types, objects, FUNCINDEX
                  decoded on first reference; bodies untouched
TypeTable         process-wide interning by canonical key (§3.4)
SymbolTable       symbol -> FunctionUnit | global slot | intrinsic
FunctionUnit      lazy: first call decodes the body blob and asks the node
                  factory for a RootCallTarget; generic functions keep one
                  target per type-argument tuple
NodeFactory       builds Truffle nodes from decoded IR nodes under a type
                  environment
```

The node classes are a **new package** (D12), laid out as
[truffle-design.md](truffle-design.md) §12 proposes and reviewed against its
§11 checklist; the value classes follow the flat model of §3.4. The factory
is a new class fed by the generated decoder (typed records, not
`Map<String,Object>`); the JSON
[NodeFactory.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java),
the map-based
[NodeFactoryV2.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/v2/NodeFactoryV2.java)
and the existing node classes are its *semantic* reference (which construct
needs which edge cases; their tests are ported, their structure is not), and
are deleted at cutover (M3). Call sites hold a direct reference to the callee's
`FunctionUnit`, so a not-yet-decoded callee costs one branch.

Class-path layout: one backend jar set under `$GRAALGO_HOME/lib`
(RECOVERY T0.D has flattened the former three-directory same-FQN overlay;
this design assumes the flattened module).

## 5. The standard library through this path

### 5.1 Three classes of package, defined by the source tree

`$GRAALGO_HOME/goroot` is assembled at install time: `bin`, `pkg`, `lib` and
the rest are the stock go1.27.1 distribution; `src` is the upstream tree
with the fork applied. The stock distribution itself is never modified
(AGENTS §4); the fork is stored in this repository as

- [stdlib/patches/](../../stdlib/patches/) — minimal diffs against upstream
  files of *patched* packages, and
- `stdlib/fork/<import path>/` — complete replacement directories for
  *replaced* packages (new directory, created by work package W5).

| Class | Packages | Source |
|---|---|---|
| **Replaced** | `runtime`, `internal/runtime/*` (15 packages in std's closure), `runtime/cgo`, `runtime/race` | facade packages: the exported API and linkname targets, bodies either pure Go kept from upstream or body-less (§5.3) |
| **Patched** | expected: `reflect` (4 call-path functions), `syscall` (fork/exec glue), `os/signal`, `runtime/debug`, `runtime/pprof`, `runtime/trace`, `runtime/metrics`, `internal/godebug`, `internal/cpu`, `unique`, `weak`, `iter` | upstream file + diff in `stdlib/patches` |
| **Verbatim** | everything else (target: ≥ 350 of 383) | upstream, byte-identical |

`graalgo-tool` has no list of these packages. It compiles the files `cmd/go`
gives it; the classes exist only as directories in the repository and as
the per-release fork-size audit GG-0009 asks for. During development the
same fork can be applied without assembling a GOROOT via
`go build -overlay` (§5.2), which is what the tests of the early milestones
use.

### 5.2 A facade `runtime` works with the stock driver and gc (verified)

Experiment: an overlay that deletes every file of `GOROOT/src/runtime` and
adds a 20-line `facade.go` (exported functions without bodies, one
interface, one struct) plus an empty `.s` file (so that `cmd/go` omits
`-complete`). Results with stock go1.27.1:

- `go list` reports the package with the facade's files and no imports; the
  closure of a `package main` shrinks from 31 packages to 2.
- `go build runtime` succeeds: gc's `compile -p runtime -std` accepts the
  facade.
- `go vet` of a program using the facade's API succeeds.
- `go build` of that program fails only in the stock **linker**
  (`could not look up runtime.mapinitnoop`) — the one tool this design
  replaces.

So the shadow-compile approach (D3) and the source-tree fork (D8) compose.

### 5.3 Body-less functions: one resolution rule

A function declaration without a body in the IR is resolved by the linker,
by symbol name, in this order:

1. the symbol (or the remote name of its `//go:linkname`) has a Go body in
   some package of the link → direct call;
2. the backend's intrinsic registry has an entry under that name → the call
   site becomes a call to the Java method (the registry is the versioned
   contract of SPEC §4.1; the registry file is generated from annotated Java
   methods and checked into the freeze document);
3. otherwise: if the function is reachable from `main` or an initializer
   (worklist over `FUNCINDEX` symrefs, the gc linker's deadcode rule), the
   link fails with `undefined: pkg.Func (no Go body and no graalgo
   intrinsic)`; if unreachable, it is recorded as absent and calling it
   through reflection panics with the same message.

The same rule serves std assembly functions, runtime linknames, the facade
packages and third-party modules with assembly. It is the mechanism behind
GG-0020 (the registry) and GG-0021 (by name, both directions).

Language operations (allocation, slices, maps, channels, select, interface
conversion, defer/panic, goroutine start, string conversion) are **not**
intrinsics in this sense: the node factory builds dedicated nodes for them
and they never appear as Go symbols. That is why the facade `runtime` does
not declare `makeslice`, `mapaccess1` and the rest of gc's 200-odd compiler
support routines.

### 5.4 Measured intrinsic surface (go1.27.1, linux/amd64)

Method: parse every Go file of `go list -deps <root>`, collect function
declarations without bodies, resolve each through linkname directives and
assembly `TEXT` symbols, and classify by which side provides it. "Runtime
family" = `runtime` + `internal/runtime/*`.

| Closure of | packages | Go lines | funcs | body-less funcs outside the runtime family, provided by: assembly | …by the runtime family via linkname | …by another std Go package | compiler-only / none |
|---|---|---|---|---|---|---|---|
| `fmt` | 59 | 163 k | 5,712 | 65 | 122 | 11 | 45 |
| `encoding/json` | 74 | 186 k | 6,551 | 65 | 122 | 11 | 45 |
| `net/http` | 189 | 349 k | 12,229 | 117 | 146 | 16 | 47 |
| `net/http`, `-tags purego` | 187 | 348 k | 12,163 | 78 | 145 | 16 | 47 |
| all of std | 383 | 594 k | 21,027 | 119 | 194 | 21 | 48 |
| all of std, `-tags purego` | 383 | 594 k | 20,986 | **80** | **194** | 21 | 48 |

Inside the runtime family there are a further 233 body-less functions (160
assembly) and 283–333 assembly `TEXT` symbols in total; all of that
evaporates with the replacement (SPEC §2.8).

Breakdown for all of std with `purego`:

- **Assembly-backed (80):** `sync/atomic` 36, `internal/bytealg` 10, `math`
  8, `math/big` 6, `syscall` 3, `reflect` 3, `internal/cpu` 3, `hash/crc32`
  3, `crypto/internal/boring/sig` 3, vendored `x/sys/cpu` 2, `time` 1,
  `runtime/cgo` 1, `internal/chacha8rand` 1.
- **Runtime-provided by linkname (194):** `reflect` 34, `runtime/pprof` 17,
  `sync` 16, `syscall` 15, `internal/poll` 12, `runtime/debug` 11,
  `internal/synctest` 9, `internal/sync` 8, `time` 7, `runtime/trace` 7,
  `internal/reflectlite` 7, `os/signal` 6, `os` 6, `sync/atomic` 5,
  `internal/godebug` 4, 20 further packages with 1–3 each.
- **Compiler-only (48):** 8 are `unsafe`'s pseudo-functions, 33 are
  `reflect`'s `badlinkname` aliases of its own methods (resolved by rule 1
  once method symbols are in the table), leaving `internal/abi.FuncPCABI0`,
  `FuncPCABIInternal`, `iter.newcoro`, `iter.coroswitch`,
  `reflect.compiledTypelinks` and two declarations in `net/url`/`go/types`
  test hooks.
- **Direct references** from Go source outside the family to objects inside
  it: 54 distinct objects — 33 functions (26 of them exported `runtime`
  API such as `Caller`, `Callers`, `GC`, `GOMAXPROCS`, `SetFinalizer`,
  `Stack`), 12 types, 9 constants/variables.

So the symbol-level surface for the whole standard library is
**80 + 194 + ~7 compiler-only + 33 direct = about 315 function symbols**.
SPEC §3.6's "~60–70 functions" and "52 + ~100 edges" count *semantic*
intrinsics and stdlib→runtime edges for a narrower closure; the two views
agree once grouped: the 315 symbols fall into roughly 25 families
(atomics 41, reflect support 37, semaphores/notify lists 24, netpoll 12,
timers/time 8, bytealg 10, math 14, syscall glue 18, profiling/trace/debug
36, scheduler pins and misc). The larger number is the one implementation
agents need, and the intrinsic registry must list symbols, not families.

How the minimum is reached:

- `-tags purego` by default removes 39 assembly functions (all crypto
  assembly) at no cost in fork size.
- `internal/cpu` is patched to report no optional CPU features, which makes
  the feature-gated assembly in `hash/crc32`, `math/big`'s vector paths and
  `internal/chacha8rand` unreachable; where upstream has an unconditional
  assembly implementation on amd64 (`math/big` arithmetic kernels,
  `internal/bytealg`) the function gets an intrinsic.
- `math`'s eight `arch*` functions map 1:1 to `java.lang.Math`.
- `syscall`'s three raw entry points (`RawSyscall6` underneath `Syscall`,
  `rawSyscallNoError`, `rawVforkSyscall`) are the single place where OS
  access enters; see §5.6.

### 5.5 Dynamic footprint of hello-world

`go build -cover -coverpkg=all` of `fmt.Println("hello, world", 42)` with
gc, run once: 1,175 of 6,713 instrumented functions execute; 719 are in
`runtime`, about 110 more in `internal/runtime/*`, about 40 belong to the coverage
writer itself. **About 300 functions outside the runtime family execute**
(upper bound, since the coverage writer also uses `os`, `reflect` and
`encoding/binary`): `reflect` ≤ 34, `os` ≤ 29, `internal/abi` 26, `fmt` 25,
`syscall` ≤ 24, `sync` 20, `internal/poll` 19, `internal/sync` 9,
`sync/atomic` 9, `time` 7, `internal/reflectlite` 6.

The executed set names the hard spots of milestone M1 precisely:

- `sync.Pool` (`pin`, `pinSlow`, `indexLocal`): per-P arrays indexed with
  `unsafe` pointer arithmetic. This is in-array arithmetic that the pointer
  model of §3.4 handles: **verbatim, no patch** (P1).
- `internal/abi.TypeOf`, `(*Type).Kind/Size/Elem`, `Name.ReadVarint`,
  `reflect.unpackEface`, `(*rtype).Field`: type descriptors read as memory
  through `unsafe` casts of interface headers. This is the `rtype` layout
  question of SPEC §3.6, answered by P1: byte-exact descriptor images plus
  the pointer model of §3.4, verbatim source.
- `syscall.Syscall`/`RawSyscall6`/`fcntl`/`write`: raw system calls with
  pointer arguments (§5.6).
- `sync.Mutex`/`Once`/`RWMutex`/`Map`: semaphore intrinsics and atomics;
  straightforward on the existing scheduler.

### 5.6 Package-by-package handling of the coupled set

| Package | Handling | Intrinsics |
|---|---|---|
| `runtime` | **Replaced.** Kept from upstream as pure Go: `error.go` types (`Error`, `TypeAssertionError`, bounds errors — the T0.A panic-model work needs exactly these), `Frames` iteration, `MemStats` struct, constants. Everything else is body-less. `runtime.Compiler` stays `"gc"` so that build-tag-like checks in libraries behave as under gc. | exported API (≈45 functions) + the 194 linkname targets, all as Java methods |
| `internal/runtime/atomic`, `internal/runtime/sys`, `internal/runtime/math`, `internal/runtime/maps`, `internal/runtime/exithook`, `internal/runtime/cgroup`, `internal/runtime/pprof/label`, `internal/runtime/syscall/linux`, others | **Replaced** by facades that keep only what code outside the family references (54 objects in total, §5.4). `internal/runtime/maps` keeps only the exported type names `reflect` mentions; Go maps are `GoMap` (SPEC §3.4) and the Swiss-table code is never compiled. | atomics; `sys.GetCallerPC`; `linux.Syscall6` |
| `unsafe` | no source; `Sizeof/Offsetof/Alignof` folded by the frontend, `Add/Slice/String/SliceData/StringData` are node kinds, `Pointer` conversions follow the pointer model of §3.4 (`(object, gc byte offset)`, leaf tables, range-allocated `uintptr` handles, `checkptr` traps; SPEC §3.4, GG-0062) | — |
| `sync/atomic` | **Verbatim.** 36 assembly functions + 5 linknames become intrinsics; the typed wrappers (`atomic.Int64`, `atomic.Pointer[T]`) compile as written. Intrinsics operate on the addressed field through the backend's existing cell/pointer representation. | 41 |
| `internal/sync`, `sync` | **Verbatim**, `sync.Pool` included (P1: in-array arithmetic). `internal/sync.HashTrieMap` and `sync.Map` are expected verbatim for the same reason (39 handle-only functions, no view, no patch); P5 confirms with their std tests. | semaphores, notify list, `procPin`, `canSpin`, `fatal`, `throw`: 24 |
| `internal/bytealg` | **Verbatim Go**, 10 assembly functions as intrinsics over `byte[]`/`GoString` | 10 |
| `math`, `math/bits`, `math/big` | **Verbatim.** `math` 8 intrinsics → `java.lang.Math`; `math/bits` is pure Go (the JIT recognizes the patterns; intrinsic nodes are a later optimization); `math/big` 6 kernels as intrinsics over `long[]`-backed word slices | 14 |
| `internal/cpu`, vendored `x/sys/cpu` | **Patched**: feature detection returns "none"; removes `cpuid`/`xgetbv` | 0 |
| `syscall`, `internal/syscall/unix`, `internal/poll`, `os` | **Verbatim** apart from the fork/exec glue in `syscall`. All OS access funnels into the raw syscall intrinsics, which dispatch on the syscall number through a **role table** ([abi-spike.md](abi-spike.md) §2.3): one entry per number with a "may block" bit and up to six argument roles — `Val`, `CStr`, `Bytes(lenArg, dir, filled)`, `Struct(type, dir, nestedDir, nullable)`, `Array(elemType, countArg, dir, nestedDir, filled)` — plus per-*type* nested rules (`Iovec.Base/Len`; `Msghdr.Iov/Iovlen`, `Name/Namelen`, `Control/Controllen`), a second key for `ioctl` (request), `fcntl` (command) and `ptrace` (request), and libc wrappers instead of raw `syscall(2)` for the `AllThreadsSyscall` credential numbers. **A role names a Go type and never states a size or offset**: sizes, offsets and pointer leaves come from the type table of §3.4. Pointer arguments arrive as `uintptr` handles (§3.4) and are copied in/out through a call-scoped arena around one Panama downcall to libc `syscall(2)`; no Go object is pinned and no native address outlives the call. A number missing from the table, an integer that is not inside a live object, or a length past the Go object fails loudly. The table is what makes `golang.org/x/sys/unix`, which calls `syscall.Syscall` directly, work without a fork. Measured scope: 148 distinct syscalls issued by `syscall` + `internal/syscall/unix`, 70 reachable from `os` (17 of them only in the fork/exec child), 47 from `net`. Blocking calls release the scheduler slot around the downcall (GG-SCHED-0005). **Readiness** (`internal/poll`'s 12 `runtime_poll*` functions) is a small epoll-based poller in the runtime over raw fds, driven through the same executor (`epoll_create1/ctl/wait`); goroutines waiting on it park through the scheduler's slot-releasing path and are woken through the wake lane, deadlines on the runtime timer heap (SPEC §3.5, GG-0063 — this replaces the blocking-NIO netpoll contract, which cannot accept raw fds). **Process creation** (`forkExec`): the fork/exec child path cannot run in a JVM; the glue is patched to `posix_spawn` a small helper binary shipped under `$GRAALGO_HOME/libexec` that applies `SysProcAttr` and `exec`s the target (the JDK's `jspawnhelper` pattern; SPEC §3.8, GG-0064). | 3 raw entries + ~18 glue + 12 poll |
| `time` | **Verbatim.** `now`, `runtimeNano`, `Sleep`, timer start/stop/reset bind to the existing `Timers` | 8 |
| `reflect`, `internal/reflectlite`, `internal/abi` | SPEC §3.6 decides "exact `rtype`/itab layouts"; P1 answered how ([abi-spike.md](abi-spike.md) §1). `internal/abi` and `internal/reflectlite` are **verbatim**; `reflect` is **verbatim except a 4-function patch** (`Value.call`, `callReflect`, `callMethod`, `makeMethodValue` call one invoke intrinsic with `[]Value`; the other 19 functions that manufacture gc call frames become unreachable and stay verbatim). Mechanism: (1) the backend builds, per type, the **descriptor image** gc's compiler would emit — kind-specific `internal/abi` struct, trailing `UncommonType`, parameter and method arrays, names — as bytes plus a parallel reference array, from the type table of §3.4 alone; `NameOff`/`TypeOff`/`TextOff` are IDs in a process-wide table with gc's `reflectOffs` numbering; (2) every descriptor access goes through the pointer model of §3.4. The intrinsics are exactly the body-less functions (34 runtime linknames + 3 assembly → 37 that need Java code) plus the invoke intrinsic. Residual risks, in decreasing order: descriptors built at run time by `StructOf`/`FuncOf`/`MapOf`/… (an inverse descriptor reader interns them; patching 7 constructor tails is the fallback), writes through header views (`packEface`, `atomic.Value`, `SetLen`), `typedmemmove` over leaf tables, `uintptr` tokens, `MapIter`'s embedded `maps.Iter` (W5 facade), and performance under Truffle (needs a PE-friendly addressable dispatch; measured in W9's first week). | 37 + 1 invoke |
| `iter` | **Patched**: `Pull`/`Pull2` on the scheduler's goroutine parking instead of `coroswitch` | 2 |
| `unique`, `weak`, `runtime.AddCleanup`, `SetFinalizer` | bind to `java.lang.ref` (`WeakReference`, `Cleaner`) | 6 |
| `os/signal`, `runtime/debug`, `runtime/pprof`, `runtime/trace`, `runtime/metrics`, `internal/godebug` | **Patched** thin: each has 4–17 runtime linknames; first implementation returns empty profiles/zero metrics (the API works, the data is honest-empty and documented in [limitations.md](limitations.md)); GODEBUG registration is real because std reads settings at init | ~50 |
| `runtime/cgo`, `plugin`, cgo-using packages | out of scope here (T4); `CGO_ENABLED=0` default | — |
| third-party assembly | rule §5.3; `purego` default covers the common libraries; a reachable assembly function without a Go fallback fails the link with its name | — |

## 6. `go vet`, `go test`, coverage, flags

- **`go vet`** — stock binary, stock export data. Nothing to do. vet also
  analyzes the fork's sources when a user runs `go vet std`; that is not a
  supported workflow.
- **`go test`** — `cmd/go` generates `_testmain.go`, compiles the test
  variants (`pkg [pkg.test]`, `pkg_test`), links, and runs the binary with
  `-test.*` flags, reading results from stdout and the exit status. The
  wrapper sees ordinary `compile`/`link` calls. Requirements on this design:
  the linked artifact is an executable that forwards argv, stdio, exit code
  and signals faithfully; `os.Args[0]` is the artifact path; the working
  directory is preserved (the test runs in the package directory). `-json`
  is produced by the stock `test2json` in `cmd/go`. Result caching works
  when the test binary's content ID is stable across rebuilds — the bundle
  is deterministic given identical inputs (the encoder already has a
  determinism test) — and when `link -V=full` covers the runtime (§2.1).
  `testing` itself, `flag`, `os`, `runtime/debug`, `runtime/pprof`,
  `internal/testlog`, `testing/internal/testdeps` must compile through the
  path: that is milestone M2. `-timeout` and the panic-on-timeout stack dump
  need `runtime.Stack` over all goroutines (T0.A territory).
- **Fuzzing** (`go test -fuzz`) needs `-d=libfuzzer` instrumentation and
  coverage counters in shared memory; unsupported, fails loudly at compile.
- **`-cover`** — §2.5; M5.
- **`-race`/`-msan`/`-asan`** — §2.6.
- **`-gcflags`** — reach the shadow gc compile unchanged, so `-m`, `-S`,
  `-N`, `-l` print what gc prints. They do not influence execution;
  documented.
- **`-ldflags`** — `-X` honored, `-s -w` accepted, `-H`, `-linkmode`,
  `-extldflags` accepted and ignored, `-buildmode` per §2.4.
- **`-trimpath`** — the frontend applies the `-trimpath` rewrite rules to
  file names stored in the IR exactly as gc does (`objabi.ApplyRewrites`
  semantics), because they surface in `runtime.Caller` and panic traces.
- **`-pgo`** — accepted, ignored (the JVM profiles).
- **`-overlay`, `-mod`, `-modfile`, `-C`, `-a`, `-n`, `-x`, `-work`,
  `-p`, `-v`, `-json`** — entirely inside `cmd/go`.
- **`GOOS`/`GOARCH`** — the wrapper accepts only the host pair of the
  tracked platform list (linux/amd64 first, SPEC §3.8); other values fail
  at `compile` with one line. Cross-compilation would produce IR that is
  mostly portable but the fork and intrinsics are per-GOOS.

## 7. What `graalgo-tool` looks like

```
cmd/graalgo-tool/            main: argv dispatch on the tool basename
internal/toolexec/
  argv/                      response files, flag subsets per tool, importcfg
  archive/                   ar reader/appender for __.GRAALGOIR
  shadow/                    exec of the stock tool, PATH lookup, exit status
  ir/                        schema.txt, generated encoder, canonical type keys
  emit/                      typed AST -> IR (successor of frontend/v2/emit)
  link/                      symbol table, reachability, init order, bundle, ELF build-ID patch
  stub/                      the launcher stub's main package
internal/backend/…/graalgo/loader/   BundleLoader, PackageUnit, generated decoder, node factory
stdlib/fork/, stdlib/patches/        the fork (§5.1)
```

`internal/frontend/v2` is the starting point for `emit/`: its parser and
checker stages are thin wrappers that pass all of std (§11, E5) but the
checker uses `importer.Default()` — which shells out to `go list` per import
(29 s for std versus 3.9 s with an importcfg lookup) — and passes the package
*name* where the import path belongs; both are replaced by §2.2 step 4. Its
emitter handles 18 of 60 AST node kinds; 73 of 376 std packages pass through
it without an "unsupported" error, and those are packages with almost no
code. Its `Document` model (type table, object table, funcs, globals) and
its test discipline (coverage matrix, negative tests, determinism test,
Go/Java golden vectors) carry over; the map-with-string-keys node shape does
not.

## 8. Migration: milestones

Every milestone is demonstrable end to end through
`graalgo build`/`run`/`test` (the launcher of §1) and is gated by a
differential test against gc. Effort is in focused agent-days and is an
estimate; ranges reflect the uncertainty named in §12.

| M | Deliverable | Gate (differential vs gc) | Effort |
|---|---|---|---|
| **M0** | Walking skeleton of the build side: `graalgo-tool` shadow compile + IR member + link to stub + bundle (prototype of this lane, §13). JVM execution of the bundle is **not** part of what was built. | protocol tests of §13: warm rebuild runs zero tools, cache separation, build ID, gc-identical diagnostics | done in this lane |
| **M1a** | GIR schema + generated Go encoder/Java decoder; full-language emitter (all 60 node kinds, no "unsupported"); loader + new node factory over the **new node package** (D12; flat value classes of §3.4); multi-package user modules; generics by run-time instantiation; init order; facade `runtime` v0 (println, panics, goroutines, `runtime.Gosched/GC/GOMAXPROCS`) applied by `-overlay` | the 134 corpus programs under [test/corpus/](../../test/corpus/) that import nothing, each also split across ≥ 2 packages by a mechanical test transform; emitter runs over all 376 non-empty std packages without error (compile-only gate); per node class the unit and PE tests of truffle-design §11 (T1–T4), `pe-check` bailouts = 0 on the corpus | 21–35 (15–25 + 6–10 for the new node package) |
| **M1b** | Verbatim leaf std through the path: `errors`, `internal/bytealg`, `unicode/utf8`, `unicode`, `math`, `math/bits`, `cmp`, `slices`, `sort`, `strconv`, `strings`, `bytes`, `sync/atomic`, `internal/sync`, `sync` (verbatim, P1), `iter`, `time`; intrinsic registry + link-time resolution and reachability | new corpus of programs that use these packages and report with `println`; per-package exported-function sweeps generated from gc outputs | 10–15 |
| **M1c** | `syscall` raw-entry intrinsic with the role table for the 70 numbers `os` reaches (write/read/open/close/fstat/fcntl/getpid first) and the table-vs-source gate (R15), `internal/poll`, `os` (files, args, env, exit), `io`, `io/fs`, `path` | file and stdout/stderr programs; `os.Args`, env, exit codes; the syscall table-vs-source check green | 8–12 (W8's M1c share: 5–7) |
| **M1d** | `internal/abi` + `internal/reflectlite` verbatim + `reflect` with the 4-function patch (P1) + `internal/fmtsort` + `fmt` → **hello world with `fmt` from the real stdlib** | every corpus program that imports only `fmt`, `os`, `strings`, `strconv`, `errors`, `sort`, `time`, `sync`; `fmt` verb matrix differential; imgcheck (descriptor images judged by gc's own reflect) over a ≥ 300-type corpus; reflectprobe 126/126 | 16–24 (W9 14–20 incl. W13, + `fmtsort`/`fmt` 2–4) |
| **M2** | `testing`, `flag`, `runtime/debug`, `internal/testlog`, `testdeps`; `go test` on user packages incl. `-run`, `-v`, `-json`, `-count`, caching, `-timeout` | cmd/go test-protocol scripts from the txtar suite; then **std's own tests** become the oracle: `go test` green for the M1 packages with a versioned exclusion list (GG-0009's verification plan) | 10–15 |
| **M3 — cutover** | the full differential corpus (591 programs) and snapshots pass through the new path at ≥ v1's pass rate; `cmd/graalgo` becomes the §1 launcher; **delete** `internal/frontend` v1, all 140 `synth_*.go`, the generators, the import overlay, JSON `NodeFactory`, `NodeFactoryV2`, **v1's ~110 node classes and nested-`GoStruct` value classes** (D11/D12), `p3reflect`/`p3unsafe`/`p3net`, the bridge inventory entries | zero regressions vs the v1 baseline per program, each remaining failure triaged; `check_no_bridges.py` grandfather list empty | 10–20 (dominated by triage) |
| **M4** | `encoding/*`, `bufio`, `regexp`, `text/template`, `os/exec` (spawn helper, GG-0064), `os/signal`, `net` (runtime epoll poller, GG-0063), `net/http`, `crypto/*` (purego), `context`, `log/slog` | std tests per package with exclusion list; `net/http` client/server differential; fd-blocked goroutines at cap = GOMAXPROCS never starve a runnable one (the `CapExhaustion` falsifier, ported to the poller) | 20–35 (incl. W8's remaining 5–8) |
| **M5** | `-cover`; build modes; the 929-script txtar matrix through the launcher as the GG-0001/0002 conformance gate; the linkname-diff and intrinsic-coverage audits (GG-0020/0021) wired into the upgrade runbook | txtar pass list with explained exclusions | 15–25 |

M1 in the task's sense (hello world with `fmt` from real std) is
M1a + M1b + M1c + M1d: **55–86 agent-days of work** (was 48–82: +6–10 for the
new node package on M1a, M1d re-sized from 15–30 to 16–24 by P1), **3–4
calendar weeks with the parallelism of §9**. It is hard for the reasons §5.5 lists — not because
of `fmt`, which is 25 executed functions, but because reaching it requires
the complete language in the emitter and factory, the runtime boundary, the
`sync` family, real system calls, and `reflect`'s view of type descriptors.
There is no shortcut that keeps GG-0009: every earlier attempt to get there
faster produced a synthetic package.

v1 stays untouched and green until M3. Nothing in M0–M2 modifies v1 code
paths; new code lives in new directories, including the new node package
(D12) — the runtime library is the only code both paths execute.

## 9. Work packages (parallelizable)

| WP | Scope | Depends on | Parallel with |
|---|---|---|---|
| W1 | `graalgo-tool` protocol: argv, response files, `-chain`, `-V=full`, archive member, shadow exec, launcher (§1–§2); cache-behaviour tests (warm rebuild = 0 tools, shared GOCACHE with gc) | — | all |
| W2 | GIR schema, generator for Go encoder + Java decoder, golden vectors, canonical type keys (§3.2–3.4) | — | W1, W5, W6 |
| W3 | Emitter: all node kinds, selections, constants, closures/captures, method sets, init order, embed, linkname, trimpath (§3) | W2 schema (can start on a draft) | W4 |
| W4 | Backend loader, lazy function units, new node factory, type interning, lazy itabs, generic instantiation (§4.3, §3.5); **the new node package and flat value classes** (D12, §3.4; truffle-design TR-3/TR-4 are this work done to its §11) | W2 | W3 |
| W5 | Fork: facade `runtime` + `internal/runtime/*`, patch stack, GOROOT assembly + overlay generator, fork-size audit (§5.1, §5.6) | — (API list from §5.4 measurements) | all |
| W6 | Intrinsic registry and link-time resolution/reachability (§5.3); atomics, bytealg, math, sync semaphores, time | W4 for execution; registry format independent | W5 |
| W7 | Linker: bundle, manifest, init order, `-X`, ELF stub + build-ID patch (§2.4, §4.2) | W2 | W3, W4 |
| W8 | Syscall executor (§5.6): role table for all 148 numbers from the generated sysscan skeleton + table-vs-source gate (2–3); executor in the backend incl. slot release, `EINTR`, errno, `AllThreads*` (2–3); raw-fd epoll poller, GG-0063 (2–3); spawn helper + `forkExec` patch, GG-0064 (3–4); foreign memory for `mmap`, signals glue (1–2). **10–15 agent-days**; M1c needs the first two items only (5–7) | W6, W13 | W9 |
| W9 | `reflect`/`abi` (§5.6): descriptor image builder + imgcheck gate over a ≥ 300-type corpus (3–4); runtime intrinsics over the pointer model — `typedmemmove` family, `unsafe_New*`, `ifaceE2I`, `resolve*Off`, `addReflectOff`, `typelinks`, map/chan helpers (3–4); call path: invoke intrinsic + the 4-function patch, `MakeFunc`, method values (2–3); inverse descriptor reader (2–3). **10–14 agent-days**, 14–20 with W13 | W4, W13 | W8 |
| W13 | Pointer model (§3.4), scheduled **once, before both W8 and W9**: view pointers for `(*T)(unsafe.Pointer)`, offset → slot leaf tables, header views with stores, range-allocating `uintptr` handle table, `checkptr` traps. Part of the new node package | W4 | W5, W6 |
| W10 | Test infrastructure for the new path: corpus runner through the launcher, package-splitting transform, compile-only std gate, std-test runner with exclusion list | W1 | all |
| W11 | Cutover and deletion (M3) | M2 | — |
| W12 (optional) | unified-IR export writer from `go/types` to drop the shadow compile | — | any time after M3 |

W1, W2, W5 and W10 can start immediately and in parallel. When W8 and W9
start, the two prototypes become gates: imgcheck
([descview](../../test/spikes/abi/descview/)) and the syscall
table-vs-source check ([sysscan](../../test/spikes/abi/sysscan/)) run in CI. W3 and W4 are the
two largest packages and split further by node family (expressions,
statements, declarations/types, closures+defer, concurrency) once the schema
is fixed.

## 10. Risk register

| # | Risk | Likelihood | Impact | Mitigation / trigger |
|---|---|---|---|---|
| R1 | `reflect`/`internal/abi` on descriptor images and the pointer model are not faithful or too slow | low (P1: **GO**, 607/607 and 427/427 answers identical to gc) | M1d slips | residual risks of [abi-spike.md](abi-spike.md) §1.5: run-time-built descriptors (inverse reader; 7-function patch fallback), header-view writes, `typedmemmove` over leaf tables, `uintptr` tokens, `MapIter`, performance under Truffle (15–20× gc before inline caching, measured in W9's first week). The 150–200-function patch fallback is dropped. |
| R2 | Raw-syscall marshalling: pointer arguments nested inside structs (`iovec`, `msghdr`, `epoll_event` arrays) | low (P2: **GO**, 48/48 checks; nested pointers exist only in `Iovec` and `Msghdr`) | `net`, `os/exec` slip (M4), not M1 | role model with per-type nested rules (§5.6); residual risks of [abi-spike.md](abi-spike.md) §2.6: `ioctl`/`fcntl`/`ptrace` second key, third-party numbers outside the table (fail loudly), thread-directed syscalls (correct only under `LockOSThread`), copy cost, carrier pinning in blocking downcalls; memory unsafety is R15 |
| R3 | go/types and types2 disagree on some input (gc accepts, IR pass rejects, or constant values differ) | low | internal error on that package | compile-only gate over all of std in M1a; report upstream-identical cases |
| R4 | JVM startup makes `go test ./...` on many packages unusably slow (one JVM per test binary) | high | drop-in is correct but painful | generic runtime image (§4.2) prioritized in T3; measure at M2 |
| R5 | Lazy instantiation and lazy body decoding hide backend gaps until run time | medium | late failures | "strict" link option decodes and builds every reachable function; on in CI |
| R6 | Shadow compile doubles front-end work | certain | +45 CPU-s per cold std build; ~2× per-package compile | measured acceptable; W12 removes it |
| R7 | Fork must stay gc-compilable (D3 constraint) | low | awkward facade code (empty `.s` files) | verified for `runtime`; W12 removes the constraint |
| R8 | `GOFLAGS`/argv injection edge cases (`-toolexec`, `-tags` given twice, `go env -w GOFLAGS`) | medium | wrapper bypassed → a gc binary is silently produced | the launcher checks the effective `go env GOFLAGS` before exec and refuses to continue if its wrapper is not in it; txtar matrix in M5 |
| R9 | `GOTOOLCHAIN` switch to a release without a fork | medium | confusing failure | detected and reported (§1); T4 |
| R10 | `-tags purego` default changes behaviour of a third-party package | low | perf or build difference vs gc | documented default, `GRAALGO_PUREGO=0` |
| R11 | Per-release churn of the 194 linkname edges and ~45 exported runtime functions | certain, bounded | upgrade toil | GG-0021 linkname-diff over the §11 E2 tool's output; spike 3 measured 2 changed items across 1.26→1.27 |
| R12 | cmd/go behaviour that depends on properties of a real gc binary (`go version -m`, `debug/buildinfo` on the artifact, `go install` staleness) | medium | cosmetic divergences | build ID handled (§2.4); `.go.buildinfo` patching in the stub is M5 |
| R13 | Coverage and fuzz instrumentation are compiler features | certain | `-cover` late (M5), fuzz unsupported | loud failure until implemented |
| R14 | The T0.D module flattening and T0.A lifecycle fixes land late | medium | W4 builds on a moving base | W4 touches only new packages (D12); rebases are mechanical |
| R15 | **A wrong syscall-table entry is memory-unsafe**: the kernel writes by its own notion of the struct (demonstrated: `fstat` declared with `Timespec` instead of `Stat_t` → 144 bytes into a 16-byte allocation → JVM SIGSEGV) | medium | JVM crash, silent heap corruption | (a) a **gate** comparing each entry's Go type with the pointee type sysscan derives from std's call sites (table-vs-source, CI from W8 start); (b) roles never state sizes; (c) a debug mode with guard pages after each arena allocation; (d) unknown numbers fail |
| R16 | The new node package (D12) slips M1a | medium | M1 later by up to 10 agent-days | sized (+6–10); the v1 classes are the semantic reference and their tests port; truffle-design §11 makes review mechanical |

## 11. Experiments (all on go1.27.1, linux/amd64, this box)

| # | Question | Result |
|---|---|---|
| E1 | What does `go build/vet/test -toolexec` invoke? | §2 table; argv samples in §2.2/§2.4. `-V=full` probes on every run, zero tool runs on a warm cache. |
| E2 | Body-less / assembly / linkname surface | §5.4. Tool: a 200-line Go program over `go list -deps -json` using `go/parser`; it is the seed of the GG-0021 linkname-diff tool. |
| E3 | Can `go/types` import from gc archives named by an importcfg? | Yes. `importer.ForCompiler(fset, "gc", lookup)`: 382/382 std packages (all but `unsafe`) type-check from source against export data in 3.9 s; 2,479,526 AST nodes, 21,108 function declarations, 1,981 function literals, 359 generic functions/methods, 1,810 instantiation sites. |
| E4 | Can a simple source loader type-check std in dependency order? | Yes, 382/382 in 2.5 s, same counts. |
| E5 | Does the existing v2 pipeline handle std? | parse 376/376 non-empty packages; check 376/376 (29 s because of `importer.Default()`); emit: 73/376 without error, first failures `SwitchStmt` 59, `ArrayType` 51, `FuncLit` 38, `StarExpr` 36, `DeferStmt` 23, `TypeAssertExpr` 21, `Ellipsis` 20, `MapType` 18, local `GenDecl` 15, `TypeSwitchStmt` 9, other 13. |
| E6 | Which export formats does go1.27 `go/importer` read? | unified IR only (`go/internal/gcimporter` has `ureader.go` and no indexed reader). |
| E7 | Cost of the shadow gc compile | `go build -a std`: 45 s user, 20 s wall (16 cores); with `-N -l` 31 s user. |
| E8 | Does stock cmd/go + gc accept a replaced `runtime`? | Yes (§5.2). |
| E9 | Dynamic footprint of hello-`fmt` | §5.5: 1,175 functions execute, ~300 outside the runtime family. |
| E10 | Where does cmd/go read a binary's build ID? | ELF: `.note.go.buildid` only; non-ELF: raw scan of the first 32 kB; after link it rewrites every raw occurrence of the ID (`cmd/internal/buildid`, `cmd/go/internal/work/buildid.go`). |
| E11 | Static `unsafe` inventory of `internal/abi`, `internal/reflectlite`, `reflect`, `sync*` and 7 more packages ([unsafescan](../../test/spikes/abi/unsafescan/main.go)) | layout-dependent arithmetic leaving a descriptor struct: 7 sites in 7 functions; 569 field selections and 132 pointer conversions between descriptor types; reflect: 389 of 393 functions verbatim ([abi-spike.md](abi-spike.md) §1.1) |
| E12 | Verbatim reflect on v1's object-graph model ([reflectprobe](../../test/spikes/abi/reflectprobe/main.go)) | 76 / 126 checks agree with gc; failures: byte-offset addressing (20), descriptor content (18), identity, linkname arity, other (§1.2) |
| E13 | Byte-exact descriptor images judged by gc's own reflect ([descview](../../test/spikes/abi/descview/)) | 607 / 607 answers identical; fixed `abi.Type` header byte-identical for 25 / 25 root types; transliterated reflect on the Java pointer model 427 / 427 (§1.4) |
| E14 | Syscall inventory of `os`/`net`/`os/exec` ([sysscan](../../test/spikes/abi/sysscan/main.go)) | 148 distinct syscalls, 70 from `os`, 47 from `net`; nested pointers only in `Iovec`/`Msghdr` (+ `execve` vectors in the child path) (§2.1) |
| E15 | Role-table executor through one Panama downcall ([sysexec](../../test/spikes/abi/sysexec/)) | 48 / 48 checks incl. two-level nested `sendmsg`/`recvmsg`, epoll, errno paths; 45–215 ns added per call (§2.4) |

## 12. Not decided here — needs a prototype

| # | Question | Prototype | Blocks |
|---|---|---|---|
| P1 | Can the backend present type descriptors and interface headers with gc's `abi.Type` layout through the handle table so that verbatim `internal/abi` + `reflect` work (SPEC §3.6), at acceptable cost? | **Answered 2026-11-18: GO** ([abi-spike.md](abi-spike.md) §1; E11–E13). Mechanism in §3.4/§5.6; W9 sized 14–20. | — |
| P2 | Syscall table: is "per syscall number, argument roles" enough for std's syscalls, including nested pointers? | **Answered 2026-11-18: GO** ([abi-spike.md](abi-spike.md) §2; E14–E15): 148 numbers (not ~110), role model of §5.6. The readiness question it raised is decided (GG-0063). | — |
| P3 | Actual GIR size and decode time per function for std; whether body blobs need compression | falls out of W2/W3 on the compile-only std gate | bundle layout details only |
| P4 | Truffle tree build cost for generic instantiation at first call (warm-up spikes) | measure on `slices`/`maps`/`sync/atomic.Pointer[T]` once W4 exists | none (optimization) |
| P5 | Whether `internal/sync.HashTrieMap` and `sync.Map` run verbatim on the pointer model (expected yes: handle-only, P1) | run their std tests in M1b | patch-stack size |
| P6 | How much of `runtime`'s exported API can be pure Go kept from upstream versus intrinsic | W5 first pass | fork size audit |

## 13. Prototype status (this lane)

[internal/toolexec/](../../internal/toolexec/) is a new Go module (v1 and
the existing commands are untouched) containing the walking skeleton of §2:

- [cmd/graalgo-tool/](../../internal/toolexec/cmd/graalgo-tool/) — the
  wrapper: `-V=full` identity, shadow compile, IR archive member, link to
  stub + bundle with build-ID patch, pass-through of every other tool,
  `PATH` lookup for bare tool names.
- [cmd/graalgo-stub/](../../internal/toolexec/cmd/graalgo-stub/) — the
  launcher stub.
- [archive/](../../internal/toolexec/archive/),
  [bundle/](../../internal/toolexec/bundle/) — the `ar` member and bundle
  formats.

**Verified** by the module's tests (`make test-toolexec`), which drive the
stock `go build -toolexec` over a two-package module:

| Claim of this design | Evidence |
|---|---|
| An extra `ar` member survives cmd/go's pack and build-ID rewrite and does not disturb stock consumers (§2.2) | gc compiles an importer against the extended archive; `go tool pack t` lists it; the full `go build` succeeds; with the namespacing removed, even gc's own linker consumed such archives |
| The IR pass can type-check against the shadow compile's export data through `-importcfg` (§2.2 step 4) | the bundle contains the IR of both user packages |
| Patching the ID into the stub's ELF note gives cmd/go a readable, rewritable build ID (§2.4) | `go tool buildid` returns a four-field content ID; with the patch removed the test fails **and** every warm build re-runs `link` |
| Warm cache = zero tool runs; gc and graalgo entries are disjoint in one `GOCACHE` (§2.1, GG-0028) | tool log is empty on the second build; `go list -export` returns different archives per backend; answering `-V=full` with the stock line makes the test fail (the backends then share entries) |
| Compile diagnostics are gc's (§4.1) | wrapped and stock `go build` output of an ill-typed package are byte-identical |
| Unsupported input fails the build with the package named, and no executable is written | `select` statement test |
| The stub hands the executable and argv to the runner and propagates its exit status | stand-in runner test |

**Not done, deliberately:**

- **The linked program does not execute on the JVM yet.** The stub's
  runner hand-off is tested with a stand-in. Reading the existing Java v2
  code showed that this is not a small step:
  [NodeFactoryV2.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/v2/NodeFactoryV2.java)
  returns bare statement trees with no call targets, cannot call another
  function or package, and builds `println` as "evaluate the first argument
  and discard it" — it prints nothing. A runner on top of it would be
  throw-away work in files the T0 lanes are editing; the real loader and
  factory are W4.
- Packages compiled with `-std` get no IR: the fork of §5 does not exist
  (W5). The skeleton's `-std` check is the only place where it departs from
  "compile whatever cmd/go hands over", and it disappears with W5.
- No symbol resolution, init order or `-X` at link.

**Since the skeleton (W2):** the IR member is GIR (§3.2), emitted by
[emit/](../../internal/toolexec/emit/) — the W3 starting point, which
already lowers all of std without an unsupported construct (`TestStd`);
`//go:embed` still fails loudly. Known approximations W3 must close:
method sets of unnamed struct types with promoted methods are not emitted;
keys of local types in generic functions do not include the
instantiation; constraint-interface keys flatten embedded type sets;
range assignments (`=`, not `:=`) to interface-typed variables get no
conversion node; `Const` nodes keep go/types' recorded type, which can be
untyped (shift operands, `print` arguments); the call-position convention
for `runtime.Caller` is not yet pinned against gc (TR-7).

**Since the skeleton (W1):** the launcher of §1; response-file expansion and
in-place rewriting (§2.2 step 1); `-chain`; tracked-release checks;
`-race` refused at link unless `GRAALGO_RACE=ignore`, `-msan`/`-asan`,
`-coveragecfg` and `-d=libfuzzer` refused at compile (§2.5, §2.6); tests
for `vet`/`asm`/`cgo`/`cover`/`preprofile` pass-through and for `-V=full`
cache invalidation when graalgo-tool changes.

## 14. Amendments (2026-11-18)

Applied from the owner's decisions of 2026-11-18 and
[abi-spike.md](abi-spike.md) §3 (numbering of that list in brackets):

| Change | Where |
|---|---|
| W4 builds a new idiomatic node package; v1 node classes deleted at M3; M1a +6–10 agent-days (owner decision 1; truffle-design T13) | D11, D12, §3.1, §4.3, §8 M1a/M3, §9 W4, R14, R16 |
| P1 answered: `internal/abi`/`internal/reflectlite` verbatim, `reflect` verbatim except 4 call-path functions; the 150–200-function fallback and "37–200 intrinsics" removed [1] | D8, §5.6, R1, §12 |
| Patched list without `internal/abi`, `internal/reflectlite`, `sync` (pool); P5 expected verbatim [2] | §5.1, §5.5, §5.6, §12 |
| Descriptor facts for every type; no hard-coded descriptor offset [3] | §3.2, §3.4 |
| Pointer model: `(object, gc byte offset)`, leaf tables, range-allocated `uintptr` handles; flat values, no nested `GoStruct` (SPEC §3.4, GG-0061/GG-0062) [4] | §3.4, §5.6 `unsafe` |
| Syscall table → role model; measured counts 148/70/47; R2 → low [5] | §5.6, R2, §12 |
| Readiness: runtime epoll poller through the executor (SPEC §3.5, GG-0063) replaces the blocking-NIO contract [6] | §5.6, M4 |
| fork/exec: `posix_spawn` + helper under `$GRAALGO_HOME/libexec` (SPEC §3.8, GG-0064) [7] | §5.6, M4, W8 |
| R15: syscall-table errors are memory-unsafe; table-vs-source gate [8] | §10 |
| W8 10–15 (M1c 5–7), W9 14–20; shared pointer model W13 scheduled once; descview and table-vs-source promoted to gates [9] | §8, §9 |
| Experiments E11–E15 [10] | §11 |
