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

## Known divergences from gc

These are user-visible ways graalgo's `os` package differs from the
reference toolchain. They are also documented in the synthetic `os`
source header ([internal/frontend/synth_os.go](../../internal/frontend/synth_os.go));
this section is the user-facing copy (REVIEW-2026-11-03-16 S8: the
review found them only in source comments).

- **D1 — umask is ignored.** The JVM cannot read the process umask, so
  `Mkdir`/`OpenFile`-with-create apply the requested permission bits
  exactly (e.g. `0777` stays `0777`), where gc would mask them with
  the umask. Re-test trigger: a native umask read (Phase 4/Panama).
- **D2 — `File.Stat` stats the stored path, not the handle.** The JVM
  has no fstat-on-channel, so `Stat` on an open file re-stats the
  path it was opened with (a renamed or replaced file reports the new
  file's metadata). Pipe/stdio modes are synthesized from the handle
  kind (`S_IFIFO`/`S_IFCHR`, size 0).
- **D3 — environment overlay, partially discharged.** `Setenv`/
  `Unsetenv`/`Clearenv` mutate a JVM-side overlay over
  `System.getenv`, which native code cannot see. Since 2026-11-03,
  children spawned through `os.StartProcess`/`os/exec` **do** inherit
  the merged overlay view, so a program that `Setenv`s and then spawns
  behaves like gc in the child. Only processes spawned outside Go
  still cannot see the overlay.
- **D4 — `ReadFrom`/`WriteTo` are not streaming.** They are implemented
  as read-all + write-all, so byte/error behaviour matches gc but the
  memory profile does not (a large copy holds the whole stream).
- **D5 — `Seek` on a directory.** `(0, SEEK_SET)` rewinds and returns
  `(0, nil)`; any other directory seek returns a `PathError` with
  `EISDIR` (gc-faithful).

Two related behaviours that are now implemented, not divergences:
`os.Executable` returns the launcher's entry-point path (`argv[0]`
under `graalgo run`), and `os.StartProcess`/`FindProcess`/`Wait`/
`Kill`/`Signal` work via the JVM process API (see the `os` source
header for their remaining gaps: `SysProcAttr`, extra file
descriptors, and file-description sharing).

## Architecture

The driver is a thin Go program ([cmd/graalgo/](./)) that:

1. Locates the graalgo installation (`GRAALGO_HOME`, executable path, or cwd).
2. Dispatches subcommands to the existing infrastructure:
   - `run`/`build`/`vet` → [internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run)
     (the Java launcher wrapper)
   - `fmt` → stock `gofmt`
   - `test` → synthesizes single-file test drivers and runs them via
     `graalgo-run`
