# P3-M9 wave-3 differential corpus (tier-4: net/netip)

Wave-3 corpus for the synthetic `net/netip` package. Pure Go, no
sockets. Conventions are the wave-1 ones (`../../wave1/README.md`).

## Programs

- `00_parse.go` — `ParseAddr`/`ParsePrefix`/`ParseAddrPort`/
  `AddrFromSlice`, `Addr` methods (`Is4`/`Is6`/`IsLoopback`/`String`).

## Scope

Pure package. The `netip` source is a near-verbatim port of gc
go1.27.1 `net/netip/netip.go` + `uint128.go` with the WS-D transform
(multi-result funcs → `__`-helpers returning pair/quad structs).
