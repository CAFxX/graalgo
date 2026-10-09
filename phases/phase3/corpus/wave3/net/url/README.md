# P3-M9 wave-3 differential corpus (tier-4: net/url)

Wave-3 corpus for the synthetic `net/url` package. Pure Go, no sockets.
Conventions are the wave-1 ones (`../../wave1/README.md`).

## Programs

- `00_parse.go` — `Parse`, `QueryUnescape`/`PathUnescape`, `ParseQuery`,
  `URL.String`, `ResolveReference`, `JoinPath`, userinfo.

## Scope

Pure package. The `url` source is a port of gc go1.27.1
`net/url/url.go` with the WS-D transform. Adaptations:
`strings.Cut` → local `__cut` (not in M7 scope); `internal/godebug` →
local shim (defaults); `path.Join` → local `__urlPathJoin`;
`badSetPath` linkname dropped.

Known D4 divergence: `fmt.Errorf` `%w` flattens (no wrap chain); the
corpus pins error strings, not `errors.Is` chains, for the one site
(`parseHost` invalid IP-literal).
