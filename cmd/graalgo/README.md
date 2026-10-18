# graalgo driver

The `graalgo` command: a drop-in replacement for the `go` command that
routes through graalgo's Truffle backend.

## Build

```sh
make graalgo
```

This produces `./graalgo` in the repo root.

## Usage

```sh
# Run a program
./graalgo run hello.go

# Validate a package and emit an executable artifact
./graalgo build

# Run tests
./graalgo test

# Vet
./graalgo vet

# Format
./graalgo fmt

# Version
./graalgo version
```

Set `GRAALGO_HOME` to the repo root if the binary is installed elsewhere:

```sh
export GRAALGO_HOME=/path/to/graalgo
```

## Commands

- **run**: Compile and run a Go program via the Truffle backend.
- **build**: Validate that packages translate through the frontend (the
  compile gate), then emit an executable artifact: a real binary that
  embeds the frontend output and runs it through the graalgo backend.
  (`./...` is not yet supported; build one package/directory at a time.)
- **test**: Discover `*_test.go` files and run each `TestXxx` function
  through the backend with a minimal `testing.T` shim.
- **vet**: Run frontend static checks.
- **fmt**: Format source files with `gofmt`.

## Known divergences from `go test`

- **TestMain is not supported.** `TestMain(m *testing.M)` is the test
  binary's entry point; the synthesized driver has no `testing.M` shim,
  so it is excluded from test discovery and `graalgo test` runs the
  package's tests without it (with a stderr warning naming the file).
  A `TestMain` that sets up fixtures, calls `m.Run()` conditionally, or
  exits non-zero will not have those effects.
- **`t.Fatalf`/`t.Skipf` vs guest `recover()`.** gc implements `Fatalf`
  via `runtime.Goexit`, which no guest `recover()` can intercept. The
  driver emulates fatal/skip with unexported sentinel panic values
  (`graalgoFatalPanic`/`graalgoSkipPanic`) recognized by type at the
  per-test boundary; a guest `defer`/`recover()` that swallows one lets
  the test function continue past the `Fatalf` call, where gc would stop
  it. The test is still marked failed/skipped and buffered output is
  flushed, but post-`Fatalf` execution is a known divergence.

## Architecture

The driver is a thin Go program ([cmd/graalgo/](./)) that:

1. Locates the graalgo installation (`GRAALGO_HOME`, executable path, or cwd).
2. Dispatches subcommands to the existing infrastructure:
   - `run`/`build`/`vet` → [internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run)
     (the Java launcher wrapper)
   - `fmt` → stock `gofmt`
   - `test` → synthesizes single-file test drivers and runs them via
     `graalgo-run`
