# wave-3 `embed` corpus

Scope-limited corpus for `embed`. There is no synthetic adapter: the
package's feature IS the `//go:embed` compiler directive (file reading +
pattern matching at export time, materializing `string` / `[]byte` /
`embed.FS` vars). A synthetic `embed.FS` type without the directive would
be unconstructible by programs — a lie, not an adapter.

- `00_embed.go` (+ `testdata/`) is `.scopelimited`:
  `reason: needs //go:embed directive materialization in
  exporter/P3SyncMain`, `retest: go:embed directive support lands in the
  frontend`. The gc leg still builds and runs it, proving the program is
  valid deterministic Go; the graalgo leg is skipped.
- Pins (for the retest): string/bytes/FS embedding, `ReadFile`, `ReadDir`
  (names, dir flags), `Glob`, `Sub` + `fs.ReadFile`, `Open`/`Stat`,
  missing-file error.
