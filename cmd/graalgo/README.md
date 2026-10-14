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

# Validate a package builds
./graalgo build ./...

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
- **build**: Validate that packages translate through the frontend.
  (No binary is produced — the backend JIT-compiles at run time.)
- **test**: Discover `*_test.go` files and run each `TestXxx` function
  through the backend with a minimal `testing.T` shim.
- **vet**: Run frontend static checks.
- **fmt**: Format source files with `gofmt`.

## Architecture

The driver is a thin Go program ([cmd/graalgo/](cmd/graalgo/)) that:

1. Locates the graalgo installation (`GRAALGO_HOME`, executable path, or cwd).
2. Dispatches subcommands to the existing infrastructure:
   - `run`/`build`/`vet` → [internal/backend/graalgo-run](internal/backend/graalgo-run)
     (the Java launcher wrapper)
   - `fmt` → stock `gofmt`
   - `test` → synthesizes single-file test drivers and runs them via
     `graalgo-run`
