# P3-M9 wave-3 differential corpus (tier-4: net)

Wave-3 corpus for the synthetic `net` package (P3-M9 tier-4 worker).
Conventions are the wave-1 ones (`../wave1/README.md`: `NN_name.go`
naming, deterministic-output rules, `.timing` / `.scopelimited` marker
semantics, the shared-runner model).

## Programs

- `00_pipe.go` — `net.Pipe`: pure Go (channels), no sockets. Pipe-leg of
  the §1.4 split. Expected to pass with no socket intrinsics.
- `01_pipe_deadline.go` (+`.timing`, gmp=1 only) — pipe read/write
  deadlines, `os.IsTimeout`.
- `02_dial_tcp_loopback.go` — TCP loopback: Listen/Accept/Dial/echo over
  127.0.0.1:0. Real sockets via the netpoller traps.
- `03_dial_timeout.go` (+`.timing`, gmp=1 only) — refused dial,
  `DialTimeout` respects timeout.
- `04_udp_loopback.go` — UDP: `ListenPacket`, `WriteTo`/`ReadFrom`,
  connected `Dial("udp")`. Connected datagrams only (sandbox blocks
  unconnected `sendto(2)` with EPERM — see fork contract).
- `05_parse_addr.go` — `ParseIP`/`ParseCIDR`/`ParseMAC`,
  `JoinHostPort`/`SplitHostPort`, `ResolveTCPAddr`/`ResolveUDPAddr`
  (IP literals only, no DNS). Pure Go.

## Scope limits

- Loopback-only: 127.0.0.1 / ::1, port 0. Never hardcode ports, never
  dial external hosts, never depend on external DNS (egress grep gate).
- DNS (`LookupHost`, `LookupIP`, `LookupPort`) is fail-fast: no resolver
  in M9. `retest:` DNS intrinsics land.
- `InterfaceAddrs` is fail-fast. `retest:` interface listing lands.
- AF_UNIX → `-EAFNOSUPPORT` (M9: TCP/UDP loopback only).
- Known D4 divergence: `fmt.Errorf` `%w` flattens (no wrap chain); the
  corpus pins error strings, not `errors.Is` chains, for the one site.
- `os.ErrDeadlineExceeded` (synthetic os) lacks `Timeout()==true`;
  `net` uses a local `timeoutError` so deadline errors match gc.

## Subpackages

- `netip/` — `net/netip` pure parsing corpus.
- `url/` — `net/url` pure parsing corpus.
- `textproto/` — `net/textproto` MIMEHeader corpus (pure parts only;
  Reader/Writer need synthetic bufio — `retest:` bufio lands).
