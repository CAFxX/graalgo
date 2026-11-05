# P3-M9 wave-3 differential corpus (tier-5: mime/multipart)

Wave-3 corpus for the stdlib `mime/multipart` package (P3-M9 tier-5
worker). Conventions are the wave-1 ones (`../../wave1/README.md`).

## Programs

- `01_multipart.go` — `Writer`→`Reader` round-trip over `bytes.Buffer`
  with a fixed boundary (`SetBoundary` — the default boundary is
  random and is never printed): field/file/unicode parts,
  `FormName`/`FileName`, nested multipart-in-part, `ReadForm` with a
  tiny `maxMemory` (file part spills to disk under `TMPDIR`; only
  content equality is printed), adversarial inputs (truncated message,
  wrong boundary, empty boundary).

## Scope limits

- The program is `.scopelimited` (see `01_multipart.scopelimited`):
  the synthetic `mime/multipart` source exists, and the m7 desugar
  rows for `Reader.ReadForm` / `FileHeader.Open` landed in
  review-1105-mr-table-missing (A-P1-11), but the package still does
  not type-check until the carried type-error fix lands (branch
  review-1105-multipart-typeerr: `synth_mime_multipart.go:934`). The
  gc leg validates it is real deterministic Go.
- The `ReadForm` spill path uses the wave-3 `TMPDIR` discipline via
  `os.CreateTemp` internals; no paths are printed.
