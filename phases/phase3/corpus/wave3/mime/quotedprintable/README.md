# P3-M9 wave-3 differential corpus (tier-5: mime/quotedprintable)

Wave-3 corpus for the stdlib `mime/quotedprintable` package (P3-M9
tier-5 worker). Conventions are the wave-1 ones
(`../../wave1/README.md`).

## Programs

- `02_qp.go` — `Writer`/`Reader` round-trip: soft breaks at the 76-col
  limit, `=` encoding, trailing-whitespace encoding, non-ASCII,
  hex-digit case-insensitivity, escape-at-line-boundary, adversarial
  inputs (`=ZZ`, truncated escape, lone `=` at EOF, soft-break drop).

## Scope limits

- No synthetic `mime/quotedprintable` source exists in frontend/v1, so
  the program is `.scopelimited`; the gc leg validates it is real
  deterministic Go. `retest:` synthetic `mime/quotedprintable` lands.
