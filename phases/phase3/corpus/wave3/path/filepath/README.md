# P3-M9 wave-3 `path/filepath` corpus (tier-2)

Differential corpus for the new synthetic `path/filepath` package
(`phases/phase1/frontend/synth_filepath.go`): a linux-only port of
go1.27.1 `path/filepath` + the unix `filepathlite` core (windows/Plan9
out per GG-0023). Conventions are the wave-1 ones (`../wave1/README.md`).

## Programs

- `00_clean_join.go` — Clean (dot/dotdot, dup separators, trailing slash)
  and Join. **Spread form only**: `Join([]string{...}...)` — frontend/v1
  cannot pack variadic call arguments (WS D); the signature stays gc-exact.
- `01_base_dir_ext.go` — Base, Dir, Ext, Split edge cases.
- `02_match.go` — Match: wildcards, classes, escapes, separator scoping,
  bad patterns, non-UTF8 bytes.
- `03_glob.go` — Glob on a temp tree: patterns, sorted output, bad pattern,
  no-meta literal/missing.
- `04_walk.go` — Walk/WalkDir: order, SkipDir, symlink non-traversal,
  missing root.
- `05_abs_rel.go` — Abs, Rel, IsAbs (tail-only printing for determinism).
- `06_evalsymlinks.go` — EvalSymlinks on **relative** paths (see scope).
- `07_misc.go` — VolumeName, ToSlash, FromSlash, SplitList, IsLocal.

All programs are hermetic: `os.MkdirTemp` + `defer os.RemoveAll`, no reads
outside the temp dir.

## Scope boundary

`06_evalsymlinks.go` exercises EvalSymlinks on relative paths only.
Absolute-path EvalSymlinks is scope-limited: `os.Lstat` fails for `/`,
`/home`, `/usr` on the graalgo leg (bogus errno 9223372036854774623 —
the os worker's bug, not filepath's). Retest trigger: `os.Lstat("/")`
succeeds on the graalgo leg.
