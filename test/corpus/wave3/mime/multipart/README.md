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
  the synthetic `mime/multipart` source exists and type-checks since
  the carried type-error fix landed in `review-1118-p0-zip-multipart`
  (`synth_mime_multipart.go:934`), and the m7 desugar rows for
  `Reader.ReadForm` / `FileHeader.Open` landed in
  review-1105-mr-table-missing (A-P1-11) — but the program is blocked
  by the `Writer.CreatePart` signature mismatch (takes
  `__mpMIMEHeader`, user code passes `textproto.MIMEHeader`; observed
  2026-11-18 in the `review-1118-p0-zip-multipart` differential probe,
  0/2 legs), the carried `path/filepath` `lstat` export bug (own lane
  `review-1105-filepath-lstat`), and the hand-written
  `mime/multipart.readMIMEHeader` M9-scope panic stub (no `__`-helper,
  no desugar row; `(*Part).populateHeaders` calls it for every part).
  The gc leg validates it is real deterministic Go.
- The `ReadForm` spill path uses the wave-3 `TMPDIR` discipline via
  `os.CreateTemp` internals; no paths are printed.
