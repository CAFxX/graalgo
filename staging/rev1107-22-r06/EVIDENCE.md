# R-06 evidence — lane rev1107-22-r06 (2026-11-07 ~22:1x CET)

HEAD verified: `59f0a247` (`git rev-parse HEAD` in `~/workspace/graalgo`, main repo, read-only).
All commands under `nice -n 10`. Backoff CLEAR before builds (load 2.33, then 4.85).
Go toolchain `~/tools/go/bin/go` exists and is executable. `GOCACHE`/`GOPATH`
redirected to `/tmp/rev1107-22-r06/` (home quota closed — see below).

## 1. Mechanism re-verified from disk truth (not taken from the review)

- `tools/fuzz/exec.go:18` (HEAD): `graalgoRun = os.ExpandEnv("$HOME/workspace/graalgo/phases/phase1/graalgo-run")`.
  `ls` → No such file or directory. The whole `phases/` directory does not exist.
- `internal/backend/graalgo-run` EXISTS: executable bash launcher invoking
  `graalgo.launcher.Main` (live launcher, requires `make build` for out/classes).
- `tools/fuzz/exec.go` `runCmd`: a missing executable returns `*exec.Error`
  (not `*exec.ExitError`) → code `-1`.
- `tools/fuzz/compare.go:89-96`: `!gr.buildOK` → `vDiverged` unless
  `buildCode == 2 && isScopeError(...)`. With buildCode=-1 every seed is DIVERGED.
- No `GRAALGO_RUN` override and no startup check existed at HEAD.

## 2. Quota — the reason this is staged, not committed

Real 8 KB home write probe (`head -c 8192 /dev/urandom > ~/workspace/.quota-probe-r06`):
`Disk quota exceeded` (EDQUOT). `/var/tmp` 100% full (1.5M free). `/tmp` 478M free.
`/dev/null` is STILL a regular empty file (REVIEW-20 anomaly, not re-fixed here).
No worktree claimed, no registry entry written, no commit — per the lane brief.

## 3. RED — pristine HEAD copy, seeds 0–1 (build + run, GOCACHE in /tmp)

```
$ fuzz-red run -start 0 -count 2 -lanes 1 -workdir red-work
done: map[DIVERGED:2]      (exit 0)
journal: {"seed":0,"verdict":"DIVERGED","note":"gc(exit=0) vs graal(exit=0) stdout_eq=false minimized->fuzz_0.go probes=5"}
journal: {"seed":1,"verdict":"DIVERGED","note":"gc(exit=0) vs graal(exit=0) stdout_eq=false minimized->fuzz_1.go probes=6"}
state:   {"nextSeed":2,"counts":{"DIVERGED":2}}
```

2/2 false divergences; gc exit=0; the graalgo leg never ran (buildCode=-1).
It also saved false minimized "regressions" `fuzz_0.go` / `fuzz_1.go`.

## 4. Control — RED binary + working stub launcher still diverges

`GRAALGO_RUN=<stub> fuzz-red run ...` → still `map[DIVERGED:2]` for seeds 0–1.
The RED binary has no env override and ignores the stub: the dead default
path is load-bearing, and the fix (not the stub) is what restores signal.

## 5. GREEN — fixed copy

### 5a. Unit tests / vet / gofmt (in `fuzz-fixed/`, GOCACHE in /tmp)

```
$ go vet ./...    → exit 0 (clean)
$ go test ./...   → ok  fuzz  0.004s (exit 0; 8 tests incl. new exec_test.go)
$ gofmt -l .      → gen.go only (PRE-EXISTING at HEAD — untouched, out of scope)
```

### 5b. Fail-fast on missing launcher (`run` and `regress`)

```
$ GRAALGO_RUN=/nonexistent/graalgo-run fuzz-fixed-bin run -start 0 -count 2 ...
fuzz: graalgo-run backend not found at /nonexistent/graalgo-run: stat /nonexistent/graalgo-run: no such file or directory (set GRAALGO_RUN to the launcher path)
exit=1; workdir NOT created (check runs before any state/journal)
$ GRAALGO_RUN=/nonexistent/graalgo-run fuzz-fixed-bin regress ... → same error, exit=1
$ GRAALGO_RUN=/nonexistent/graalgo-run fuzz-fixed-bin gen -seed 0 → works (exit 0; gen needs no backends)
```

### 5c. End-to-end signal restored (stub launcher emulating a working graalgo-run)

Stub: `--check` → exit 0; run → `go run <src>` (behaviorally identical to gc,
so a correct fuzzer must report SAME). Same seeds 0–4 as the RED run plus 3 more:

```
$ GRAALGO_RUN=<stub> fuzz-fixed-bin run -start 0 -count 5 -lanes 1 -workdir green-work
done: map[SAME:5]   (exit 0)
journal: seeds 0–4 all {"verdict":"SAME"}; state {"nextSeed":5,"counts":{"SAME":5}}
no regressions dir — nothing minimized, as expected
```

RED 2/2 DIVERGED → GREEN 5/5 SAME on identical seeds.

## 6. Patch integrity

`R-06.patch` = `diff -ruN fuzz-pristine fuzz-fixed` (241 lines, 3 files:
`exec.go`, `main.go`, new `exec_test.go`). Applied with `patch -p1` to a fresh
HEAD copy in `apply-test/fuzz/` → applies cleanly, resulting tree is
byte-identical to `fuzz-fixed/` (`diff -r` clean).

## 7. What was NOT verified (needs the resume lane, quota + backend build)

- End-to-end against the REAL `internal/backend/graalgo-run`: it requires
  `make build` (out/classes) + GraalVM JDK in the worktree — heavy, and home
  quota blocks it now. The stub run proves the fuzzer wiring; it does not
  prove the real backend passes seeds. The resume lane should run
  `fuzz run` with the default launcher after `make build` (backoff-gated)
  and confirm verdicts are signal (SAME / genuine DIVERGED / SCOPE_REJECT),
  not a uniform class.
- `go test ./...` / `go vet` were run on the staged copy in /tmp, not in a
  worktree — re-run after applying in the worktree.

## 8. Out-of-scope observations (not fixed, for the coordinator)

- `test/differential/cmd/diffharness/snapshot_test.go` also references
  `phase1/graalgo-run` — same dead-path class, different tool; belongs to
  the live R-03/R-04 lane, left untouched.
- `tools/fuzz/gen.go` is unformatted at HEAD (`gofmt -l`); pre-existing,
  deliberately not reformatted (unrelated diff noise).
