# P3-M9 wave-3 differential corpus (tier-4: net/textproto)

Wave-3 corpus for the synthetic `net/textproto` package (pure parts).
Conventions are the wave-1 ones (`../../wave1/README.md`).

## Programs

- `00_header.go` — `CanonicalMIMEHeaderKey`, `TrimString`, `MIMEHeader`
  (`Add`/`Set`/`Get`/`Values`/`Del`).

## Scope limits

Pure parts only (`header.go` + `CanonicalMIMEHeaderKey`/`TrimString`).
`Reader`/`Writer`/`Conn`/`Pipeline` are NOT ported: they need the
synthetic `bufio` package plus WS-D multi-result I/O dispatch.
`retest:` synthetic bufio lands AND WS D multi-result dispatch is
available. The `commonHeader` sync.Once optimization is omitted (pure
fast path; canonicalization is complete without it).
