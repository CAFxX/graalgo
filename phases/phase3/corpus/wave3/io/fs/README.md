# P3-M9 wave-3 `io/fs` extension corpus (tier-2)

Differential corpus for the P3-M9 synthetic `io/fs` extension
(`phases/phase1/frontend/synth_iofs.go`): the G2 gap functions `ReadFile`,
`WalkDir`, `Glob`, `Sub` plus `ReadDir`, `Stat`, `ValidPath`, and
`os.DirFS` (implemented in `synth_os.go` over the existing os/file
abstractions). Conventions are the wave-1 ones (`../wave1/README.md`).

## Programs

- `00_readfile.go` — ReadFile: basic, empty, missing, directory, invalid path.
- `01_walkdir.go` — WalkDir: traversal, SkipDir pruning, missing root, abort.
- `02_glob.go` — Glob: patterns, classes, bad pattern, no-meta, invalid path.
- `03_sub.go` — Sub: isolation, escape rejection, nested, file/missing/invalid/dot.
- `04_readdir_stat.go` — ReadDir sorting + DirEntry/FileInfo, Stat file/dir/missing.
- `05_dirfs_errors.go` — DirFS: empty root, absolute/NUL names, PathError
  path rewriting, Open/Read/Stat/Close through fs.File.

All programs are hermetic: `os.MkdirTemp` + `defer os.RemoveAll`, no reads
outside the temp dir.

## Scope boundary

`frontend/v1` cannot pack variadic call arguments (WS D): `fs.Glob` takes a
single pattern (no issue); no variadic `io/fs` API is exercised with packed
args. Retest trigger: WS D argument-packing lands.
