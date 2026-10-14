# T3 — Toolchain drop-in surface for graalgo

Research track T3. Primary source: go1.27.1 tree at `~/tools/go` (VERSION: `go1.27.1`).
Scope: what "indistinguishable from the real `go`" concretely requires, three
candidate architectures for the driver/backend split, the cgo story, and the
build-cache compatibility question.

---

## 1. What drop-in concretely requires

### 1.1 The command surface

`src/cmd/go/main.go:52-70` registers 19 commands, each a `base.Command` in
`src/cmd/go/internal/<name>/`:

1. `bug` (bug/bug.go), 2. `build` (work/build.go), 3. `clean` (clean/clean.go),
4. `doc` (doc/doc.go), 5. `env` (envcmd/env.go), 6. `fix` (vet/vet.go: `CmdFix`),
7. `fmt` (fmtcmd/fmt.go), 8. `generate` (generate/generate.go), 9. `get`
(modget/get.go), 10. `install` (work/build.go: `CmdInstall`), 11. `list`
(list/list.go), 12. `mod` (modcmd/mod.go + download, edit, graph, init, tidy,
vendor, verify, why), 13. `run` (run/run.go), 14. `telemetry`
(telemetrycmd/telemetry.go), 15. `test` (test/test.go), 16. `tool` (tool/tool.go),
17. `version` (version/version.go), 18. `vet` (vet/vet.go), 19. `work`
(workcmd/work.go + edit, init, sync, use, vendor).

Drop-in means replicating the behavior of all 19, including `go help` topics
generated from `alldocs.go`, and the interactive failure modes (unknown command,
unknown flag, `-h` handling via `base.Usage`).

### 1.2 The flag surface

The shared build-flag set is defined once in
`src/cmd/go/internal/work/build.go:312` (`AddBuildFlags`) plus
`src/cmd/go/internal/base/flag.go:56` (`AddBuildFlagsNX`) and `base.AddModFlag` /
`base.AddModCommonFlags`:

`-a -p -v -n -x -C -asan -msan -race -trimpath -work -buildmode -buildvcs
-compiler -tags -toolexec -gcflags -asmflags -ldflags -gccgoflags -linkshared
-pgo -pkgdir -installsuffix -mod -modcacherw -modfile -overlay -json
-debug-actiongraph -debug-runtime-trace -debug-trace`.

Per-command extras: `go test`'s own flags (`-c -exec -o -vet`, plus the
test-binary flags forwarded as `-test.X`); `go env`'s `-json -changed -u -w`;
`go clean`'s `-cache -testcache -modcache -fuzzcache -i -r`; `go vet`'s
`-vettool`/`-fix` analog; `go generate`'s `-run -n -v -x`; `go version`'s
`-m -v -json`; `go list`'s `-f -json -m -e -deps -test -export -compiled
-find -u -versions -retracted -modinfo` and the rest.

`go test` has a custom two-phase parser (`test/testflag.go:226` `testFlags`)
because its command line is "a holding place for pkg.test's arguments": known
flags may appear before or after the package list; unknown flags after the
package list are passed to the test binary. The forward set is generated from
the real `testing` flag list (`test/genflags.go` → `test/flagdefs.go`:
`passFlagToTest`: artifacts, bench, benchmem, benchtime, blockprofile,
blockprofilerate, count, coverprofile, cpu, cpuprofile, failfast, fullpath,
fuzz, fuzzminimizetime, fuzztime, list, memprofile, memprofilerate,
mutexprofile, mutexprofilefraction, outputdir, parallel, run, short, shuffle,
skip, timeout, trace, v — each forwarded with a `test.` prefix). A reimplemented
driver must reproduce `testFlags`' package-list/flag disambiguation exactly,
including the issue-40763 edge case.

### 1.3 `go env` output variables

`envcmd/env.go:79` (`MkEnv`) is the authoritative list (sorted by name):
`GO111MODULE GOARCH GOAUTH GOCACHE GOCACHEPROG GODEBUG GOENV GOEXE GOEXPERIMENT
GOFIPS140 GOFLAGS GOHOSTARCH GOHOSTOS GOINSECURE GOMODCACHE GONOPROXY GONOSUMDB
GOOS GOPACKAGESDRIVER GOPATH GOPRIVATE GOPROXY GOROOT GOSUMDB GOTELEMETRY
GOTELEMETRYDIR GOTMPDIR GOTOOLCHAIN GOTOOLDIR GOVCS GOVERSION`, plus `GCCGO`,
one arch-specific env var (`GOAMD64` etc. via `cfg.GetArchEnv`), `AR CC CXX`,
and `CGO_ENABLED` (1/0). Modes: plain `NAME=VALUE`, `-json`, `-changed`
(only vars differing from defaults), `-w KEY=VALUE` (writes `go.env`,
`KEY=VALUE` lines, `envcmd/env.go:705`), `-u`. Any drop-in must emit the same
set, since scripts parse it.

### 1.4 Build cache layout and action-ID hashing

- **Location/format**: content-addressed disk cache, `cache/cache.go`. Entries
  live at `<dir>/<xx>/<hex(id)>-<key>` (`cache.go:104,118`); trimmed by mtime
  (5-day limit, daily scan, `trim.txt`); the `Cache` interface (`Get/Put/GetFile/
  PutBytes`…) is already abstracted, and `GOCACHEPROG` (`cache/prog.go`)
  delegates to an external cache server process — a ready-made injection point.
- **Action IDs**: `cache.ActionID` is a SHA-256 over a canonical text
  description of *all* inputs (`cache.go:31`). For a compile action
  (`work/exec.go:367`): `compile "<toolID>" <forcedGcflags> <gcflags>`,
  then `asm "<toolID>" …`, then `file <name> <content-hash>` per input, plus
  PGO profile hashes (`work/action.go:467`). Link actions hash
  `link "<toolID>" <ldflags> <buildmode>` plus every archive's build ID
  (`work/exec.go:1589`).
- **Tool IDs** (`work/buildid.go:146`): the driver runs
  `tool -V=full` — *through* `-toolexec`, so the wrapper's compiler answers —
  and uses the output line (release: version string; devel: content-ID part).
  gcc tool IDs hash `gcc -v` output (`buildid.go:207`).
- **Build IDs** stamped into archives/binaries (`work/buildid.go:28-60`):
  `actionID/contentID`; they make installed artifacts one-element cache entries
  and feed downstream action IDs.

### 1.5 Module resolution

`modfetch/proxy.go`: GOPROXY protocol (`@v/list`, `@v/<v>.info/.mod/.zip`);
`modfetch/fetch.go`: download/unzip, `go.sum` verification, sumdb checks
(`GOSUMDB`, `GONOSUMDB`, `GOFLAGS=-mod=mod/readonly/vendor` via `cfg.BuildMod`);
`GONOPROXY/GOPRIVATE` matching; `modload` builds the module graph (MVS).
`vendor/` + `-mod=vendor` consistency checks (`modcmd/vendor.go`). A drop-in
must speak the proxy protocol and the sumdb (hashes of zips, `dirhash`)
identically — this is pure network/file behavior, backend-independent.

### 1.6 `go test` semantics

- **Test binary protocol**: the driver builds a test main, runs it, and parses
  its stdout. `-json` streams `test2json` events
  (`cmd/internal/test2json/test2json.go:32`): `{Time, Action, Package, Test,
  Elapsed, Output}`; actions include `start/run/pass/fail/skip/pause/cont/bench/
  output`. Exit code: test failure → 1 (`base.SetExitStatus(1)` at
  `test/test.go:1048,1463,1527,1748`).
- **Result caching** (`test/test.go:1882-1940`): two-level lookup. `testID` =
  hash of (link-input IDs proxy or test binary content, args, `work.ExecCmd`);
  on a hit the driver loads the previous run's input list (env vars + files the
  test opened), re-hashes their current values (`computeTestInputsID`), and
  looks up `testAndInputKey(testID, testInputsID)` (`test.go:2153`). Hit →
  prints `ok pkg (cached)`. Caching is disabled for `-count=1`, `-fuzz`, std
  no-test-flag runs that open files, local-directory mode, and after
  `go clean -testcache` (expiry timestamp, `test.go:857`).
- **Conformance corpus**: `src/cmd/go/testdata/script/` contains 929 txtar
  scripts pinning exact CLI behavior. This is the de-facto drop-in test suite
  any reimplementation must pass.

### 1.7 `go vet` integration

`go vet` runs the `cmd/vet` tool over type-checked packages, configured via a
JSON `vetConfig` (`work/exec.go:1186-1277`: package files, import maps, vetx
data). `-vettool` swaps the analyzer binary (`buildid.go:151`). `go test` runs a
vet subset automatically (analyzers in `test/flagdefs.go: passAnalyzersToVet`).
Vet results feed the action graph as `vet` actions with their own cache keys
(`exec.go:1349`).

### 1.8 Exit codes

`internal/base/base.go:175-190`: `exitStatus` starts 0; errors call
`base.Fatalf`/`Errorf` → `SetExitStatus(1)`; `base.Exit()` → `os.Exit(exitStatus)`.
Usage errors (bad flags, unknown command, unfindable GOROOT) exit **2**
(`main.go:130,134,168,178`; `mainUsage`). So: 0 = success, 1 = failure
(build/test/vet/tool errors), 2 = usage error. This contract is load-bearing
for CI scripts.

### 1.9 `go version` string format

`go version go1.27.1 linux/amd64` (plain). `-m` prints module/build info from
embedded buildinfo; `-m -json` prints `debug.BuildInfo` as JSON
(`version/version.go:187`). A JVM-built binary must embed equivalent buildinfo
(the `runtime/debug` read path parses the binary's buildinfo section — backend
work, not driver work).

### 1.10 `go doc` / `go list` output stability

`go list`: `load.PackagePublic` (`load/pkg.go:63`) carries an explicit public-API
contract — *"It is okay to add fields, but not to change or remove existing
ones. Keep in sync with ../list/list.go."* `-json` output must keep every field;
`-f` templates render them. `go doc` (`doc/doc.go`) parses with `go/parser`
and renders declarations/comments — its output format is stable user-facing
text; a drop-in must reproduce the rendering (it's pure AST→text, no backend
dependence).

## 2. Three architectures

### 2.1 Where the seam actually is (before evaluating)

cmd/go has **no compiler-backend plugin API**. The precise seams, from source:

1. **`-toolexec 'cmd args'`** (`work/build.go:354`, documented `build.go:202-209`):
   *every* tool invocation — `compile`, `asm`, `cgo`, `link`, `cover`, `pack`
   (see `work/gc.go:136,332,676,724` and `work/exec.go:318`) — is prefixed with
   the wrapper argv, and `TOOLEXEC_IMPORTPATH` is set in the environment. The
   wrapper sees the exact argv the driver constructed. `toolID()` even queries
   `tool -V=full` *through* the wrapper (`buildid.go:167`), explicitly to
   support wrappers like `toolstash`. This is the intended backend-swap seam.
   (Notably, the momo track already validated -toolexec-based injection on this
   exact tree — the mechanism is proven, not speculative.)
2. **Binary replacement**: `base.Tool(name)` resolves to
   `$GOROOT/pkg/tool/$GOOS_$GOARCH/<name>` (`base/tool.go:19-36`). Dropping a
   different `compile`/`link` on disk works with zero driver changes, but is
   invisible and unconfigurable — strictly worse than -toolexec.
3. **Internal `toolchain` interface** (`work/exec.go:2209`): `gc, cc, asm,
   symabis, pack, ld, ldShared, compiler, linker`. It is selected *only* by
   `-compiler={gc,gccgo}` (`work/build.go:272-288`); there is no registration
   mechanism. A JVM backend cannot implement this interface without forking
   cmd/go.

So the real choice is: (i) reimplement the whole driver, (ii) keep the real
driver and swap tools behind -toolexec, or (iii) fork the driver and extend the
seam.

### 2.2 Option (i): reimplement the go driver on the JVM in Java

- **Drop-in fidelity**: the worst of the three. cmd/go is ~74k LOC of non-test
  Go plus 929 txtar conformance scripts. The hard parts are not the happy path
  but the long tail: `testFlags` package-list disambiguation, two-level test
  caching, GOTOOLCHAIN switching (downloading and executing other toolchains,
  `toolchain/select.go`), the module graph/MVS + sumdb, `go.work` workspaces,
  buildvcs stamping, telemetry sidecars, and exact stderr wording that scripts
  assert on. Achievable only by porting the txtar suite and iterating to green —
  a multi-person-year conformance project before it is even *allowed* to differ
  in the backend.
- **Per-release maintenance**: every Go release changes cmd/go (recent examples:
  toolchain switching, `go.work`, telemetry, `for` range experiments). A Java
  port must re-port each release's driver diff — cost scales with upstream
  churn, with no mechanical merge assist (different language).
- **cgo**: same pipeline problem as (ii) (§3), plus the driver side must be
  reimplemented too.
- **Build-cache compatibility**: action IDs are SHA-256 over canonical text
  including `-V=full` tool lines. A Java driver *could* reproduce the algorithm
  byte-exactly (it's specified in `cache.go`/`exec.go`), but any deviation in
  canonicalization silently forks the cache. Feasible, but it must be tested
  against real `go`'s cache entries, not just spec-read.

Verdict: highest cost, lowest fidelity per unit effort. Only justified if the
driver itself must be JVM-native (it doesn't — see below).

### 2.3 Option (ii): wrap/embed the REAL go distribution, swap only the backend

Ship stock `go` (the real binary + GOROOT) and interpose at the -toolexec seam:
`graalgo` is a launcher that invokes the real `go` with
`-toolexec=graalgo-backend` (plus `GOFLAGS=-toolexec=…` handling so user flags
compose) for `compile/asm/link`, while `cgo/cover/pack` stay stock.

- **Drop-in fidelity**: the driver *is* real go — CLI surface, flags, `go env`,
  module resolution, test caching, vet, exit codes, `go version`, toolchain
  switching: identical by construction. Backend swap affects only generated
  code. The main fidelity risk is the *tool argv contract*: the wrapper must
  accept every flag the driver passes to `compile`/`link`/`asm` (e.g. `-o
  -trimpath -p -complete -dwarf -pack -importcfg …`) and honor `-V=full`
  (buildid.go:146 requires a parseable `"<tool> version <ver> …"` line).
  Per-release, the contract is verified by running the txtar suite with the
  wrapper enabled — mechanical, automatable.
- **Per-release maintenance**: near-zero for the driver (ship the new release's
  `go` binary). Backend work per release: re-run the conformance gate; adapt
  only if new tool flags appeared. This is the only option compatible with the
  "always tracks the most recent Go release" requirement without a standing
  porting team.
- **cgo**: keep the stock `cgo` tool (it only does preamble parsing and
  generates `_cgo_*.go`/`_cgo_export.h`/`.cgo2.c` — no machine code) and the
  stock `gcc` invocations for C parts (`work/exec.go:2281` `gcc()`); swap only
  the Go-side `compile` and the final `link`. The link step is the crux (§3).
- **Build-cache compatibility**: automatic and safe. The backend's `-V=full`
  output differs from gc's ⇒ different `toolID` ⇒ different action IDs ⇒
  graalgo entries and gc entries never collide in a shared `GOCACHE`
  (content-hash namespacing for free). `go clean -cache`, trim logic,
  `GOCACHEPROG`, and test-result caching all keep working because they are
  driver-side. Cross-backend cache *sharing* is impossible (different codegen
  ⇒ must be different entries) — and undesirable.

Verdict: best fidelity-per-cost by far; maintenance is a conformance gate, not
a port. The cost is concentrated exactly where the value is: the backend and
the link boundary.

### 2.4 Option (iii): fork cmd/go per release, maintain the fork

- **Drop-in fidelity**: 100% at fork time (it's the real source). Allows clean
  internal changes: add a third `-compiler=jvm` value implementing the
  `toolchain` interface (exec.go:2209), or a first-class backend field next to
  `BuildToolexec`, instead of argv-level wrapping.
- **Per-release maintenance**: merge upstream cmd/go into the fork every
  release and resolve conflicts in `work/`, `modload/`, `test/`. Upstream
  churn is steady; conflicts will concentrate in exactly the files the fork
  touches (the toolchain interface and exec paths). Cheaper than (i), far more
  expensive than (ii), and every release ships a merge-risk window where
  drop-in fidelity is unverified until the txtar suite re-greens.
- **cgo / cache**: same as (ii) technically, with the freedom to thread JVM
  specifics (e.g. a `jvm` build tag, backend-aware action IDs) through the
  driver cleanly instead of via argv parsing.

Verdict: (iii) buys a cleaner seam than (ii) at the price of a standing merge
burden. It is the fallback if -toolexec proves insufficient (e.g. the driver
needs backend-aware decisions that argv interception cannot express — none
identified yet; the momo precedent suggests the seam suffices).

**Recommendation**: (ii) as the architecture, with (iii) as the documented
escape hatch. Reject (i).

## 3. The cgo story

### 3.1 The pipeline (driver side, `work/exec.go`)

For a package importing `"C"`, the driver:

1. Runs `base.Tool("cgo")` (through -toolexec) with `cgoCPPFLAGS/CFLAGS/
   CXXFLAGS/FFLAGS/LDFLAGS` derived from `#cgo` directives + env (`CFlags`,
   exec.go:2943-3010). The **cgo tool itself is pure source analysis** — it
   parses the Go+preamble and emits `objdir/_cgo_gotypes.go`,
   `objdir/<file>.cgo1.go`, `objdir/_cgo_export.c`, `objdir/<file>.cgo2.c`
   (exec.go:3010-3015). No machine code; backend-independent.
2. `_cgo_gotypes.go` records ldflags via `//go:cgo_ldflag` directives for the
   linker to forward to the host linker.
3. `gcc()` compiles the C parts (`_cgo_export.c`, `.cgo2.c`, package `.c`
   files) with the host C compiler (`cfg.DefaultCC`, exec.go:2281); `gas()`
   handles `.s` via gcc too. gcc tool IDs hash `gcc -v` (buildid.go:207), so
   swapping hosts correctly invalidates the cache.
4. The Go parts (`_cgo_gotypes.go`, `.cgo1.go`) compile with the normal Go
   compiler; the linker (internal, or external via gcc when cgo forces it —
   `preferlinkext`, exec.go:2972) combines everything. `runtime/cgo` provides
   the thread-state and callback machinery.

### 3.2 What a JVM backend does with it, per architecture

- **(ii)/(iii)**: keep stages 1 and 3 *completely stock* — real `cgo` tool, real
  host gcc, real `.o` files. Only the Go-side `compile` output format changes
  and the final link must consume JVM-compiled Go code *plus* native `.o`s.
  Two link strategies: (a) external link producing a native launcher that
  embeds/starts the JVM and loads Go code as JVM bytecode with the C objects in
  a companion `.so` reached via Panama downcalls; (b) a JVM-aware linker shim
  behind -toolexec's `link` slot. Either way the ABI boundary Go↔C (cgo's
  `//go:cgo_ldflag`, `_Cfunc_` wrappers, `runtime/cgo` thread registration)
  must be re-expressed for the JVM — this is the genuinely hard part, not the
  compilation of either side.
- **(i)**: same as above, plus reimplementing the driver's cgo action
  orchestration (flag allow-lists `checkCompilerFlags`, internal-vs-external
  link decision, `CFlags` pkg-config expansion) — pure extra cost.

### 3.3 Honest difficulty assessment

- Reusing host gcc + Panama/JNI bridge: **medium-hard**. The C compilation is
  free; the cost is the runtime bridge: cgo call semantics (thread-state `M`
  creation, `entersyscall`/`exitsyscall` equivalents, signal handling,
  traceback through native frames, finalizers on C memory). Panama covers the
  *call* mechanics; the *runtime integration* (what `runtime/cgo` does today)
  must be rebuilt against JVM threading. Expect this to be the single hardest
  integration item in the program.
- Reimplementing the C side (C→JVM bytecode, or hand-porting): **infeasible as
  a general story** — cgo exists precisely to reuse arbitrary C. Only viable
  for a curated allow-list, not drop-in.
- Pragmatic sequencing: stage 1 — cgo packages build and link with the native
  companion + Panama bridge, `runtime/cgo` replaced by a JVM-aware equivalent;
  known gaps (callbacks from C into Go on foreign threads, signal-based
  preemption interplay) documented as caveats, not silent breakage.

## 4. Build cache: byte-identical vs behavioral compatibility

**Byte-identical cache compatibility with gc is neither required nor desirable
for drop-in.** Drop-in means `go build`/`go test`/`go vet` behave identically
*as seen by the user* — same CLI, same outputs, same exit codes, same caching
*behavior* (a second `go test` prints `(cached)`). It does not mean graalgo's
cache entries are interchangeable with gc's: the action ID is a hash *of the
inputs including the tool identity*, and different codegen is a different
input. Sharing entries across backends would be a correctness bug, not a
feature.

What IS required:

1. **Same cache protocol**: same directory layout, same SHA-256 action-ID
   construction over the canonical description, same trim semantics — so
   `GOCACHE`, `go clean -cache`, `GOCACHEPROG`, and all driver-side tooling
   keep working. Under (ii) this is free (real driver).
2. **Backend-separated keys**: guaranteed automatically because `toolID()`
   hashes the backend's `-V=full` output. The backend must ensure its
   `-V=full` line is *stable across runs* (buildid.go:146-186: release builds
   use the version string precisely for reproducibility) and *distinct* from
   gc's — then graalgo and gc can even share one `GOCACHE` directory safely.
3. **Behavioral test-cache compatibility**: `go test` result caching works
   *within* graalgo (same two-level `testID`/`testInputsID` scheme, driver-side
   under (ii)); a gc-cached result must not be (and will not be) reused, since
   the test binary content hash differs.
4. **Determinism obligation on the backend**: because action IDs feed build IDs
   stamped into binaries (`go list -export` shows `BuildID`), non-deterministic
   backend output would poison caching with perpetual misses. The backend must
   be deterministic given identical inputs — a backend requirement, not a
   driver one.

**Recommendation**: keep the stock cache algorithm and directory; rely on
tool-ID namespacing for backend separation; define drop-in as *behavioral*
cache compatibility (same hits/misses pattern for the same backend) and verify
it with a cache-behavior test (build twice → second is no-op; `go test` twice
→ `(cached)`; switch backends → full rebuild, no collision).

---

## T3 key takeaways

1. Drop-in = 19 commands + the shared build-flag set (`work/build.go:312`) + `go env` vars (`envcmd/env.go:79`) + proxy/sumdb module behavior + two-level test caching + vet integration + exit codes 0/1/2 + `PackagePublic` JSON stability; the 929 txtar scripts in `testdata/script/` are the conformance suite.
2. There is no backend plugin API; the real seams are `-toolexec` (wraps every `compile`/`asm`/`cgo`/`link`/`cover`/`pack` invocation, `build.go:354`) and `$GOROOT/pkg/tool/…` binary replacement — the internal `toolchain` interface (`work/exec.go:2209`) needs a fork to extend.
3. Architecture: (ii) wrap the real `go`, swap only the backend behind `-toolexec` — driver identical by construction, per-release cost is a conformance gate, not a port; (iii) fork is the escape hatch; (i) Java reimplementation is rejected (cost/fidelity).
4. cgo: keep the stock `cgo` tool and host gcc for the C side under all options; the hard part is the link/ABI boundary and a JVM-aware `runtime/cgo` (Panama bridge) — medium-hard, the program's riskiest integration item; do not reimplement C compilation.
5. Build cache: byte-identical cross-backend sharing is wrong, not required; keep the stock hashing/layout, let differing `-V=full` tool IDs namespace backends automatically, and define drop-in as behavioral cache compatibility verified by test.
