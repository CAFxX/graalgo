# Fast debug cycle

**Target:** edit → rebuild → run → result in a few seconds.

## Usage

```bash
# From the repo root:
make fast-build                    # incremental compile of changed Java files
make fast-run GO=path/to/file.go   # compile + run a Go program
make fast-test TEST=com.foo.Bar    # compile + run a Java test class
make fast-test TEST=com.foo.Bar METHOD=testX  # single test method
make fast-watch GO=path/to/file.go # rebuild + rerun on file change
```

Or directly:
```bash
internal/backend/fast-cycle/fast-cycle build
internal/backend/fast-cycle/fast-cycle run foo.go
internal/backend/fast-cycle/fast-cycle test graalgo.frontend.ArithmeticCorpusTest testArith1
internal/backend/fast-cycle/fast-cycle watch foo.go
internal/backend/fast-cycle/fast-cycle stop    # stop the daemon
internal/backend/fast-cycle/fast-cycle status  # check daemon
```

## How it works

A persistent JVM daemon (`FastCycleDaemon.java`) holds a warm JVM:

1. **Compilation** uses the in-process `JavaCompiler` API — no `javac`
   JVM startup. Only changed files are compiled (git-aware change
   detection + mtime verification). Output is byte-identical to
   `make build`.

2. **Runs** execute in a fresh subprocess per invocation — picks up
   recompiled classes, isolates static state, and a guest `System.exit`
   kills only that subprocess (its exit code is reported). The daemon
   JVM itself never loads guest classes for runs.

3. **Tests** are invoked via `RUNTEST` — test methods run in a guest
   subprocess via the daemon's `TestRunner`, which loads the test class
   in a fresh `URLClassLoader` and closes it before exiting.

4. **Authentication:** every connection must first send `AUTH <token>`,
   where the token is generated per daemon start and stored in
   `~/.fastcycle.token` with owner-only (0600) permissions. Loopback TCP
   alone authorizes nothing — only a process running as the same user
   can ask the daemon to compile or run code.

## Measured cycle times

| Operation | Baseline | Fast-cycle |
|-----------|----------|------------|
| `make build` (no changes) | 18s | 1s |
| `make build` (1 file changed) | 30s | 4s |
| Run Go program (cold) | 6s | 5s |
| Run Go program (warm) | 6s | 2s |
| Single Java test method | 36s+ | 8s |

## Correctness

- Compiled `.class` files are byte-identical to `make build` output
  (verified via md5sum).
- No test coverage is cut — the same tests run, just faster.
- The daemon is a debug tool; CI still uses `make all`.

## Limitations

- Go frontend changes (`internal/frontend/*.go`) still need
  `make generate` — the daemon handles Java only.
- The daemon holds a JVM (~500MB). Stop it with `fast-cycle stop`
  when not debugging.
- If the daemon gets into a bad state, `fast-cycle stop` then
  retry — it restarts automatically.
