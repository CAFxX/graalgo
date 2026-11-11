# P3-M9 wave-3 `path/filepath` corpus (tier-2)

Differential corpus for `path/filepath` as resolved through the
disk importer (real go1.27.1 stdlib source). The synthetic
`path/filepath` (`internal/frontend/synth_filepath.go`) was never
registered in `syntheticSources` and carried no Appendix-M desugar
rows, so it was dead code — deleted by the F13-P1-3 remediation
(REVIEW-2026-11-09-13). These programs pin the live disk-import
route, which is the load-bearing one. Conventions are the wave-1
ones (`../wave1/README.md`).

## Status (carried block)

All eight programs currently fail gofrontend export on the disk-import
route with `variable "lstat" used before declaration` (GOROOT
`path/filepath/path.go:307` `var lstat = os.Lstat`, used at `:364`) —
a pre-existing export bug, not caused by the synthetic deletion (the
synthetic was never wired in). Fix lane: `review-1105-filepath-lstat`.
Retest trigger: `gofrontend` accepts `00_clean_join.go`.

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
