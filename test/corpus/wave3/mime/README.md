# P3-M9 wave-3 differential corpus (tier-5: mime)

Wave-3 corpus for the stdlib `mime` package (P3-M9 tier-5 worker).
Conventions are the wave-1 ones (`../wave1/README.md`: `NN_name.go`
naming, deterministic-output rules, `.timing` / `.scopelimited` marker
semantics, the shared-runner model) plus the wave-3 delta
(`../README.md`).

## Programs

- `00_mediatype.go` — `ParseMediaType` (quoted params, case folding,
  RFC 2231 continuations incl. mixed encoded/plain sections),
  `FormatMediaType` round-trip, `WordDecoder`/`WordEncoder`
  (Q/B, multi-word, bad charset, malformed), adversarial inputs.

## Scope limits

- No synthetic `mime` source exists in frontend/v1 (imports are not
  supported in frontend/v1 scope — WS G import overlay), so the program
  is `.scopelimited`; the gc leg validates it is real deterministic Go.
  `retest:` synthetic `mime` source lands in `internal/frontend`.
- `TypeByExtension`/`ExtensionsByType` are intentionally not covered:
  they read host mime.types files (environment-dependent); the corpus
  pins only the pure parse/format/encoded-word surface.
