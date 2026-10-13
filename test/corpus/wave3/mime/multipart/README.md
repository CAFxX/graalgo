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

- No synthetic `mime/multipart` source exists in frontend/v1, so the
  program is `.scopelimited`; the gc leg validates it is real
  deterministic Go. `retest:` synthetic `mime/multipart` lands.
- The `ReadForm` spill path uses the wave-3 `TMPDIR` discipline via
  `os.CreateTemp` internals; no paths are printed.
