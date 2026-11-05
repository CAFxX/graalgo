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
  `-tags` build tags are honored (file selection and import resolution).
- **build**: Validate that packages translate through the frontend (the
  compile gate), then emit an executable artifact: a real binary that
  embeds the frontend output and runs it through the graalgo backend.
  (`./...` is not yet supported; build one package/directory at a time.)
  `-tags` build tags are honored.
- **test**: Discover `*_test.go` files and run each `TestXxx` function
  through the backend with a minimal `testing.T` shim.
- **vet**: Run frontend static checks (one check over the whole package,
  so cross-file references resolve) plus the static analyzers in
  [internal/vet/](../../internal/vet/). `-tags` build tags are honored.
  External test files (`package foo_test`) are not vetted yet: they
  declare a different package and cannot be checked in the same unit.
- **fmt**: Format source files with `gofmt`. Accepts `-l` (list files
  whose formatting differs), `-d` (print the diff), `-s` (simplify),
  `-e` (print all errors), and `./...` (format recursively, skipping
  vendor). With no flags it rewrites files in place and prints the
  names of the files it changed, like `go fmt`.

## Architecture

The driver is a thin Go program ([cmd/graalgo/](./)) that:

1. Locates the graalgo installation (`GRAALGO_HOME`, executable path, or cwd).
2. Dispatches subcommands to the existing infrastructure:
   - `run`/`build`/`vet` → [internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run)
     (the Java launcher wrapper)
   - `fmt` → stock `gofmt`
   - `test` → synthesizes single-file test drivers and runs them via
     `graalgo-run`
