# Spike 5 — cmd/go coupling: is `-toolexec` a sufficient backend seam?

**Verdict: PASS**

**Tested configuration:** stock Go 1.27.1 (`~/tools/go` → `~/tools/go-1.27.1`),
linux/amd64, `-toolexec` injected via `GOFLAGS` into every `go` invocation of
the `cmd/go` txtar suite; logging wrapper that records each intercepted
invocation and `exec`s the real tool unchanged. Wrapper v1 (sha256
`67d5d918…`, source [src/toolexec-log/main.go.v1](src/toolexec-log/main.go.v1)) for the matrix; v4
(sha256 `bf86f300…`, source [src/toolexec-log/main.go](src/toolexec-log/main.go)) for the
response-file follow-up. Full provenance in [src/SHA256SUMS](src/SHA256SUMS).

## 1. Objective

Prove or kill the study's recommended driver architecture: ship the STOCK Go
distribution with the backend swapped behind the existing `-toolexec` seam
(no driver fork, no driver reimplementation).

Kill trigger (§8.5): PASS unless argv-interception gaps are found that FORCE
a driver fork AND the fork's per-release merge burden breaks the ≤200
agent-hour upgrade bound.

## 2. Method

1. **Seam mapping (source).** Enumerated every `cfg.BuildToolexec` use in
   `src/cmd/go` (go1.27.1) and every direct `exec.Command` in the build
   pipeline to classify wrapped vs unwrapped subprocesses (§4).
2. **Harness.** A `-toolexec` wrapper in Go ([src/toolexec-log/main.go](src/toolexec-log/main.go)):
   appends one record per invocation (timestamp, pid, tool path,
   `TOOLEXEC_IMPORTPATH`, cwd, full argv, `@response-file` expansion with
   size+sha256) to `$GRAALGO_WRAPPER_LOG` under an exclusive flock, then
   `syscall.Exec`s the real tool with argv/env/cwd/stdio unchanged —
   observable behavior is therefore stock by construction; the log is the
   only side channel.
3. **Matrix.** 52 txtar scripts from `src/cmd/go/testdata/script/`,
   sampled per the rationale in [scripts/matrix.txt](scripts/matrix.txt) (full list:
   [scripts/matrix_list.txt](scripts/matrix_list.txt)), each run twice via a prebuilt `cmd/go` test
   binary — once stock, once with `GOFLAGS=-toolexec=<wrapper>` injected
   into every script's `go` invocations. A script passes in a mode iff its
   own stdout/stderr/exit-code/file assertions hold, so equal pass/fail
   across modes = identical observable behavior by the suite's definition.
   Separate `GOCACHE` per mode; serial execution; per-script wrapper logs;
   results checkpointed to [logs/matrix_results.jsonl](logs/matrix_results.jsonl) (resumable).
   Injection required a *temporary, test-harness-only* patch to
   `script_test.go` (TestMain unsets `GOFLAGS`; the patch synthesizes it
   from `GRAALGO_TOOLEXEC`) — reverted after the spike; the driver itself
   was never modified.
4. **Gap hunts.** (a) `go build -x -a` trace diff (stock vs wrapped) on a
   module with cgo + asm + pure-Go packages, checking every stock tool
   invocation has a wrapper-prefixed counterpart; (b) targeted probes:
   binary byte-identity, `go list -export` BuildID identity, synthetic
   `@response-file` observability, `go tool` bypass, test-cache silence,
   bare-`gcc` tool-ID probe.

## 3. Matrix results

52 scripts × 2 modes = 104 runs ([logs/matrix_results.jsonl](logs/matrix_results.jsonl); per-script
logs in [logs/stock/](logs/stock/), [logs/wrapped/](logs/wrapped/); wrapper logs in [logs/wraplogs/](logs/wraplogs/)).

- **Stock mode: 52/52 pass.**
- **Wrapped mode (v1): 51/52 pass.** The single divergence was
  `cgo_long_cmd` — root-caused to two stacked response-file issues (F6
  below), both fixed in wrapper v4 **without any driver change**; the
  script re-run with v4 **passes**. Effective wrapped result: 52/52.

No other divergences. A script passes in a mode iff its own
stdout/stderr/exit-code/file assertions hold, so equal pass/fail across
modes = identical observable behavior by the suite's definition.

## 4. The seam, mapped from source (go1.27.1)

**Wrapped** (driver prefixes `cfg.BuildToolexec`; wrapper sees exact argv):

| Tool | Call sites |
|---|---|
| `compile` | `work/gc.go:136` |
| `asm` | `work/gc.go:332` |
| `link` | `work/gc.go:676,724` |
| `cgo` | `work/exec.go:3088` (preamble processing), `:3337` (`-dynimport`) |
| `cover` | `work/cover.go:26`, `work/exec.go:2102` |
| `vet` (incl. `-vettool`, incl. vet-during-`test`) | `work/exec.go:1490` |
| `preprofile` (PGO) | `work/action.go:496` |
| tool `-V=full` probes (tool-ID computation) | `work/buildid.go:159` |
| host-cc `-###` probe (gcc tool-ID computation) | `work/buildid.go:221` — note: tool arrives as **bare `gcc`** |

`TOOLEXEC_IMPORTPATH` is set on every tool invocation (`work/shell.go:639`).

**NOT wrapped** (verified in source AND in `-x` traces):

| Invocation | Why not fork-forcing |
|---|---|
| host C compiler: `gcc`/`g++` compiling cgo C files (`ccompile`, `exec.go:2313`), assembling via gcc (`gas`), external linking (`gccld`), `gccSupportsFlag` probes (`exec.go:2640`) | By design: the architecture keeps the stock C toolchain (T3 §3). The driver already exposes every knob (`CC`/`CXX`/`CGO_CFLAGS`/`CGO_LDFLAGS`/…); no driver change needed. Confirmed 30/30 identical unwrapped `gcc` lines in both `-x` traces. |
| `pack` | In-process (`packInternal`, `work/gc.go:479`) — no subprocess exists to intercept; archive assembly from object files is backend-independent. |
| `go tool <name>` | Doesn't accept `-toolexec` at all (verified: `flag provided but not defined: -toolexec`); runs `$GOROOT/pkg/tool/…` directly. User-facing introspection command, not part of any build pipeline. The graalgo launcher can route `graalgo tool` itself — launcher-level, no driver change. |
| GOTOOLCHAIN re-exec | `syscall.Exec(exe, os.Args, os.Environ())` (`toolchain/exec.go`) — full env inherited, so `GOFLAGS=-toolexec` survives into the switched toolchain. Analyzed, not empirically tested (re-test trigger R4). |
| `go generate`, `go bug`, VCS commands | User/VCS commands, not toolchain tools. |

**Response files.** When argv is very long the driver rewrites it to
`tool @/tmp/…` (`passLongArgsInResponseFiles`, `work/exec.go:3663`) *after*
the `@`-rejection check in `runOut`, so the wrapper CAN see `@file` argv —
but only if the wrapper binary's basename matches a tool name (F6a), and
the wrapper must rewrite the file in place to drop the duplicated tool
path rather than expanding it in memory (F6b — the expanded argv can exceed
`MAX_ARG_STRLEN`). See F6 for the full story.

## 5. Findings

**F1 — No fork-forcing gap found.** Every backend-relevant tool invocation
(compile/asm/link/cgo/cover/vet/preprofile, plus both tool-ID probes) is
delivered to the wrapper with complete argv, env (`TOOLEXEC_IMPORTPATH`),
and cwd. The `-x` trace diff on a cgo+asm+Go module (`-a` rebuild):
compile 62/62, asm 57/57, cgo 4/4, link 1/1 invocations all wrapped; 0
unwrapped Go-tool lines in the wrapped trace; 30/30 gcc lines identically
unwrapped in both. The non-wrapped invocations (§4 table) each have a
scoped non-fork resolution. The one behavioral divergence found
(`cgo_long_cmd`, F6) was closed with wrapper-side workarounds on the stock
driver — no fork required.

**F2 — Wrapper design requirement (found empirically): bare tool names need
PATH lookup.** The driver probes the host compiler as bare `gcc -### …`
through the wrapper. `syscall.Exec` does no PATH lookup, so harness v1
failed all 182 probes; the driver degraded *deterministically* (`CC ID
ERROR=…` hashed into action IDs — build stayed correct, cache stayed
consistent within the mode). Fix (v2): `exec.LookPath` when argv[1] has no
slash — verified: exactly 1 probe interception, then driver-cached. Any
production wrapper MUST replicate `exec.Command`'s PATH resolution.

**F3 — Backend obligations surfaced (not seam gaps).** The seam delivers
these faithfully, but the backend must honor the argv contract: `-importcfg`
format, `-buildid` stamping, `-V=full` output line (`"<tool> version …"`,
stable across runs, distinct from gc's — this is what namespaces
graalgo/gc entries in a shared `GOCACHE`, T3 §4), `@file` expansion, and
export-data format compatible with the stock `vet` tool (vet actions are
wrapped, so graalgo may also substitute its own vet).

**F4 — Cache behavior is backend-separated by construction, wrapper-transparent
by measurement.** Probe A: `go build` stock vs wrapped → byte-identical
binaries (sha256 `3bc3460e…`). Probe B: `go list -export` BuildID identical.
`-V=full` probes run through the wrapper even on cache hits (3 in the
`run_hello` trial), which is the tool-ID namespacing mechanism working as
designed. `test_vet` (vet-during-test) under the wrapper: 216 `vet`
invocations intercepted, script passed.

**F5 — `-toolexec` flag composition: last one wins (replace, not chain).**
`StringsFlag.Set` does `*v, err = quoted.Split(s)` (`base/flag.go:20`) — it
*replaces*. GOFLAGS is applied first, command-line flags second, so an
explicit `go build -toolexec=X` fully replaces a `GOFLAGS=-toolexec=Y`
injection; there is no wrapper chaining. Verified empirically: with
`GOFLAGS=-toolexec=w1` and `-toolexec=v2` on the command line, the driver
invoked v2 directly with the real tools (v2's log: `tool=compile/…`, zero
w1 records; w1 never ran). Consequence for the graalgo launcher: GOFLAGS
injection is user-overridable (consistent with GOFLAGS precedence
generally); if the backend must always win, inject the flag into argv.
This also explains the matrix's `toolexec` run: the script's own
`-toolexec=$PWD/mytool` replaced the injected wrapper, so the script
exercised the stock single-wrapper path — and passed.

**F6 — Response files: two stacked issues, both worked around without a fork
(the matrix's only divergence).** `cgo_long_cmd` (issue #66456: 10000-entry
`#cgo LDFLAGS`, ~400KB argv) failed in wrapped mode with v1:
`fork/exec …/toolexec-log: argument list too long`.
  - **F6a — driver basename check.** `useResponseFile(cmd.Path)`
    (`work/exec.go:3681`) keys off the *wrapper's* basename, not the real
    tool's. A wrapper named `toolexec-log` never triggers response files →
    E2BIG on the driver's exec, before the wrapper even runs. This affects
    ANY `-toolexec` wrapper on stock go1.27.1. Workaround (verified):
    ship/install the wrapper under a tool-like name (`bin/named/compile`);
    the driver then uses response files exactly as without a wrapper.
    Fragility note: depends on the basename check, an implementation
    detail — the per-release conformance gate (R1, which includes
    `cgo_long_cmd`) would catch a change; an upstream fix would be cleaner
    long-term.
  - **F6b — the @file contains the tool path.** The driver writes
    `cmd.Args[1:]` into the response file; with `-toolexec` set,
    `cmd.Args[1]` is the tool path itself, so the file holds
    `[tool, args...]`. A wrapper that execs `[tool @file]` unchanged makes
    the tool see the tool path as a spurious extra argv[1] (cgo tried to
    process the tool binary as a Go source file → downstream E2BIG on its
    gcc exec). Fix (v4, verified): the wrapper parses the file (verbatim
    copy of the tools' `objabi.ParseArgs`), rewrites it **in place**
    dropping the tool path (verbatim copy of the driver's `encodeArg`),
    then execs `[tool @file]` — the tool expands it itself, preserving
    the driver's E2BIG avoidance (the expanded argv can exceed
    MAX_ARG_STRLEN, so full in-memory expansion is NOT safe). In-place
    rewrite is cleanup-clean: the driver deletes the file after the
    wrapper/tool exits.
  - With v4 (named `compile`): `cgo_long_cmd` **passes**; the 369,434-byte
    response file was correctly rewritten and consumed.

**F7 — Wrapper design requirements (empirical).** Any production wrapper
must: (R-a) replicate `exec.Command`'s PATH lookup for slash-less argv[1]
(the driver probes bare `gcc -###` through the wrapper; v1 failed all 182
probes, the driver deterministically hashed `CC ID ERROR=…` — correct but
different cache keys; v2 fixed, verified: exactly 1 probe, then cached);
(R-b) expand `@file` response files itself (F6b); (R-c) be installed under
a tool-like basename (F6a) OR carry the 5-line upstream fix to
`useResponseFile`.

## 6. Kill-trigger assessment

The trigger requires (a) argv-interception gaps that FORCE a driver fork and
(b) a merge-burden argument breaking the 200h bound.

(a) **Fails — no gap forces a fork.** Every gap found has a scoped non-fork
resolution, each verified empirically on the stock driver:
- gcc/host-toolchain invocations bypass the wrapper → by design (stock C
  toolchain; driver already exposes all knobs) — §4 table.
- `go tool` bypasses `-toolexec` → launcher-level concern, not a build
  pipeline — §4 table.
- Response-file E2BIG (the only behavioral divergence, F6) → wrapper
  installed under a tool-like basename + wrapper-side in-place @file
  rewrite; `cgo_long_cmd` passes with v4 on the stock driver.
- Bare-`gcc` probe → wrapper-side PATH lookup (F2/F7).

No gap was found in which the driver needs backend-aware knowledge that
argv interception cannot express. Therefore the trigger does not fire →
**PASS**. (b) is moot.

Caveat for the program (not the verdict): F6a's basename workaround depends
on a driver implementation detail (`useResponseFile`'s basename check). If
a future Go release changes that logic, long-arg builds break until the
wrapper is renamed or the driver is fixed upstream — the per-release
conformance gate (R1) covers this because `cgo_long_cmd` is in the matrix.

## 7. Scope and re-test triggers

Verdict scoped to: go1.27.1, linux/amd64, `-toolexec` injected via GOFLAGS,
logging+delegating wrapper (v1 for the matrix; v4 for the `cgo_long_cmd`
follow-up).

- **R1.** Re-run the matrix on each new Go release (new tool flags are the
  expected churn; the conformance gate is the per-release cost — mechanical).
  The matrix includes `cgo_long_cmd`, which guards the F6a basename
  workaround against driver changes.
- **R2.** If a future release moves a backend-relevant invocation off the
  `BuildToolexec` prefix (grep `BuildToolexec` in `src/cmd/go`: the complete
  list is §4's table), re-test.
- **R3.** If `useResponseFile`'s basename check changes (F6a), re-verify
  long-arg builds through the wrapper; consider upstreaming the 5-line fix.
  **Extended 2026-10-03 (spike 03 F8):** also watch `objabi.expandArgs` /
  `ParseArgs` / `encodeArg` — the response-file *wire format* changed in 1.27
  (line-split+DecodeArg → GCC-style quote-aware ParseArgs) with no
  `useResponseFile` change, corrupting 1.26-format files through the 1.27
  wrapper. On any change: re-sync the wrapper's parser against the new
  toolchain's `objabi` (verbatim port, unit-tested against a captured
  response file) and re-run the arg-shape battery (SPEC §6.1).
- **R4.** GOTOOLCHAIN switching with `-toolexec` set: analyzed (env
  inherited through `syscall.Exec`) but not empirically tested — test when a
  local toolchain-switch script exists.
- **R5.** Windows/macOS: not tested (this spike: linux/amd64 only).
- **R6.** `go test` result-caching interplay: covered by matrix scripts
  (`test_go111module_cache`, `test_cache_coverpkg_bug`); re-check their
  wraplogs if the driver changes those paths.

## 8. Artifacts

- [src/toolexec-log/main.go](src/toolexec-log/main.go) (+`.v1`, `.v2`), [src/SHA256SUMS](src/SHA256SUMS) — harness
  source, all versions, with checksums (v3 binary-only; see manifest)
- `bin/toolexec-log` (v1), `bin/toolexec-log-v2`, `bin/toolexec-log-v3`,
  `bin/toolexec-log-v4` + `bin/named/compile` (v4, tool-like name),
  `bin/gotest.exe` — built harness and test binaries
- [scripts/matrix.txt](scripts/matrix.txt), [scripts/matrix_list.txt](scripts/matrix_list.txt) — sampling rationale + list
- [scripts/analyze_trace.py](scripts/analyze_trace.py) — `-x` trace diff tool
- [run_matrix.sh](run_matrix.sh), [launch_matrix.py](launch_matrix.py) — checkpointed/resumable runner +
  double-fork launcher
- [logs/matrix_results.jsonl](logs/matrix_results.jsonl), `[logs/](logs/){stock,wrapped}/`, [logs/wraplogs/](logs/wraplogs/) —
  raw results (incl. `cgo_long_cmd.v4.log`, `.v4.wraplog` for the fix
  verification)
- `probes/` — probe workspaces, `-x` traces (`trace-stock.txt`,
  `trace-wrapped.txt`), wrapper logs
- `~/tools/go/src/cmd/go/script_test.go` — TEMPORARY test-harness patch;
  **must be reverted** (backup `/tmp/script_test.go.spike5bak`; exact diff
  recorded in §9)

## 9. Temporary tree modification (REVERTED — verified)

`~/tools/go/src/cmd/go/script_test.go` received a temporary, test-harness-only
patch (two hunks: `GRAALGO_WRAPPER_LOG` in `extraEnvKeys`; `GRAALGO_TOOLEXEC`→
synthesized `GOFLAGS` in `scriptEnv`; full diff was recorded here during the
spike). After the matrix completed, the patch was reverted by reverse-applying
both hunks. Verification: the file now hashes to
`02c7f97121a315c093432fe054433f953310cc0f6a4f23a53404b8320ff22911` —
byte-identical to the pristine hash recorded before the patch was applied,
and `grep -c GRAALGO` returns 0. The driver (`src/cmd/go` non-test source)
was never modified at any point.
