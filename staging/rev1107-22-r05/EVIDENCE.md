# EVIDENCE.md — R-05 (`tools/mutation` dead) fix, resumed lane rev1107-22-r05

Verdict: **FIX VERIFIED RED→GREEN. BLOCKED-quota on landing only** (no worktree claimed, nothing committed/pushed; see RESUME.md).

## Mechanism re-verified at HEAD 59f0a247 (main repo, read-only)

`~/workspace/graalgo/tools/mutation/main.go` (303 lines, == `red/main.go` byte-for-byte):
- `:88` hardcoded persist dir `/home/hatch/workspace/graalgo/phases/phase1/hidden_files/mutation` with `os.MkdirAll` — resurrects the deleted tree.
- `:150` and `:197` `phase1 := filepath.Join(repo, "phases/phase1")` for the javac classpath and `runTestSuite` (`cmd.Dir = phase1`). Every Java mutant errors; Go mutants fail the same way via `runTestSuite`. Matches REVIEW-2026-11-07-07 R-05 exactly.
- Also confirmed: swallowed errors (`report, _ := json.MarshalIndent`, unchecked `os.WriteFile` ×2, `:83-94`); no test files; nothing in Makefile/CI invokes the tool.

## RED (failing evidence — reproduced by the resume lane, not just the prior lane)

Old binary `red/mutation-old` (built from HEAD `main.go`), run 2026-11-07 ~22:23 CET against the real repo with the single-mutant probe spec `red/probe-spec.json` (M-G01):

```
workdir: /run/rev1107-22-r05-tmp/mutation-3670976370
running baseline control...
fatal: baseline not green: KILLED (killed by [(no FAIL lines, non-zero exit)])
exit code: 1
```

Identical to the prior lane's staged `logs/red-run.log` (21:27). The baseline dies because `runTestSuite` runs `graalgo.test.Runner` from the nonexistent `phases/phase1` dir. No `phases/` tree was resurrected in the repo by either run (`ls ~/workspace/graalgo/phases` → no such file or directory).

## GREEN (passing evidence — all re-run by the resume lane)

Staged fix: `tools-mutation/` (== `patchcheck/tools/mutation/`, byte-identical), `mutation.patch` (1362 lines, 4 files, all under `tools/mutation/`).

1. **Patch applies cleanly to HEAD 59f0a247**: `git apply --check mutation.patch` → clean. Files: `main.go` (303→749 lines, repointed), `mutants.json` (repointed), `main_test.go` (new, 427 lines), `README.md` (new, 102 lines). `go.mod` identical to the repo's — not in the patch. No live `phases/`/`hidden_files` path in the new code (only historical mentions in comments).
2. **Unit tests — 14/14 PASS, vet clean** (resume run, GOCACHE on `/run`, 2026-11-07 ~22:23 CET): `go vet ./...` exit 0; `go test -count=1 -v ./...` → `ok graalgo.tools/mutation` (0.048s). Log: `logs/gotest-resume.log` (14 `--- PASS`, 0 FAIL). Matches the prior lane's `logs/gotest.log`.
3. **New binary `--dry-run` exit 0** (binary `mutation-new`, built from staged code): prints the repointed suite command mirroring `internal/backend/truffle/Makefile` (no `phases/` anywhere), per-mutant build plans, and prerequisite status — correctly reports MISSING `Runner.class` (`make -C internal/backend/truffle out/test-classes/.built`) and MISSING `gofrontend` (`./build.sh build` in `internal/frontend`).
4. **New binary full run fails fast, loudly, exit 1**: `fatal: missing test runner .../out/test-classes/graalgo/test/Runner.class: build it with make -C internal/backend/truffle out/test-classes/.built` — it never runs a broken suite. (A real end-to-end mutant run is impossible right now: `Runner.class` and `gofrontend` are unbuilt, and building them writes into the quota-closed home tree. That is an environment precondition, not a tool defect — the tool now names it instead of mis-reporting.)
5. **Java mutant compile leg — 4/4 COMPILE OK** (resume re-run of `java-mutant-leg.sh`): M-J01, M-J02, M-J04, M-J05 applied to live `NodeFactory.java` and compiled with the new truffle classpath (`runtime/out/classes : truffle/out/classes : truffle/lib/*.jar`). Log: `logs/java-mutant-leg-resume.log`; fresh `.class` files in `java-leg/M-J*-classes/` (mtimes 22:23–22:24).
6. **Go overlay-build leg — control + M-G01 BUILD OK** (completed by the resume lane; the prior lane's `go-leg.sh` never finished — `go-leg/control.log` 0 bytes, no `mg01` artifacts). `go build -overlay` of `internal/frontend` with the vendoring overlay alone, and with the merged overlay (330 entries, M-G01 mutated `export.go`): both succeed. Log: `logs/go-leg-resume.log`; intermediates in `/run/rev1107-22-r05-tmp/go-leg/`.

## What the fix does (design, per the review's "mutant-runner contract")

- Java mutants: compiled with `javac -Xlint:deprecation -Werror` against the live backend class trees; overlay dir first in the test classpath; only the `truffle` module has a wired suite runner — other modules ERROR loudly instead of running the wrong suite.
- Go mutants: Go overlay merging `internal/frontend/build.sh`'s vendoring map with one `Replace` entry; repo tree never modified; no 16 MB source copy per mutant.
- Suite: `graalgo.test.Runner` exactly as the truffle Makefile runs it (same runner, classpath order, `-Dgraalgo.repo.dir`, `-Xss32m`, tmpdir discipline). Baseline runs first; a red baseline is fatal.
- Persistence: no default location (the old default resurrected the deleted tree); opt-in via `-persist-dir`. All writes return errors.
- No bridges: no synthetic stdlib, no import overlays, no package-name-keyed special cases, no shims. Module routing (`javaModuleFor`) dispatches on backend module for the tool's own suite runner only, with loud ERROR for unwired modules.

## Environment notes (why some evidence is leg-based, not end-to-end)

- Home quota EDQUOT-exhausted (real 8 KB probe failed 22:06Z and again this run) → no worktree, no commit, no push.
- `/tmp` 79–100% full all run (sibling lanes hold ~470 MB) → Go build caches moved to `/run` (`/run/rev1107-22-r05-{gocache,gopath,tmp}`, 1.2 GB free there; same pattern other lanes already use). First `go vet`/`go test` attempts failed with `no space left on device` on `/tmp` — environmental, not code; rerun on `/run` passed.
- `go vet`/`go test`/`gofmt` all run under `nice -n 10`; `load-shed.sh backoff` CLEAR before heavy steps (load 1.22–7.54).
