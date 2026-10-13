# P3-M9 wave-3 differential corpus (tier-5: net/http)

Wave-3 corpus for the stdlib `net/http` package (P3-M9 tier-5 worker).
Conventions are the wave-1 ones (`../../wave1/README.md`): `NN_name.go`
naming, deterministic-output rules, `.timing` / `.scopelimited` marker
semantics, the shared-runner model, plus the wave-3 loopback-only policy
(`../../README.md`): bind/dial 127.0.0.1 only, port 0, never print
ephemeral ports, never print measured durations.

Per the corpus design (§1.4, §4) the corpus is split into a **pipe leg**
(full client+server over `net.Pipe` — no sockets) and a **loopback leg**
(real sockets over 127.0.0.1, `httptest`-based).

## Programs

- `00_pipe_roundtrip.go` — pipe leg: GET/POST, 200/404/500, method
  routing (405), request/response header propagation (custom `X-`
  headers, default `User-Agent`), body round-trip 0/1/64KB.
- `01_pipe_chunked.go` — pipe leg: chunked response streaming with
  `Flusher` and no `Content-Length` (client sees de-chunked body,
  `ContentLength == -1`), `Trailer` headers, `MaxBytesReader`
  enforcement, chunked request body.
- `02_pipe_adversarial.go` — pipe leg: malformed request bytes
  (`GARBAGE`, bad `Content-Length`, truncated headers, oversized header
  vs `MaxHeaderBytes`) → server emits `400 Bad Request`;
  `http.ReadRequest` on truncated/garbage input returns errors.
- `03_loopback_client_server.go` — loopback leg: keep-alive reuse
  across 3 sequential requests (same `RemoteAddr`), `http.Get` /
  `http.Post` / `HEAD` wrappers, `Server.Close` drains.
- `04_timeouts.go` (+`.timing`, gmp=1 only when it runs) — loopback
  leg: `http.Client{Timeout}` against a 2s-sleeping handler prints
  `client-timeout true` (fixed string); `Server{ReadTimeout}` against
  a slow-loris header drip closes the conn (zero response bytes).
- `05_query_form.go` — recorder leg (pure, no sockets):
  `httptest.NewRecorder`/`NewRequest`: multi-value query, `+`/`%2F`
  decoding in query vs path, `FormValue` body-over-query precedence,
  `ParseForm`, `ParseMultipartForm`, `DetectContentType` sniffing.
- `06_adversarial_loopback.go` — loopback leg: raw-socket client —
  split-packet (1-byte) header writes, pipelined requests on one conn,
  `Expect: 100-continue` handshake, `Hijacker` upgrade path.

## Scope limits

- No synthetic `net/http` source exists in frontend/v1 (imports are
  not supported in frontend/v1 scope — WS G import overlay), so the
  pipe-leg and recorder-leg programs are `.scopelimited` with
  `retest:` synthetic `net/http` lands in `internal/frontend`.
- The loopback-leg programs additionally need TCP socket syscalls +
  netpoller; `retest:` synthetic `net/http` lands AND socket/netpoll
  intrinsics land (P3-M9 WS G or later). RISK-M9-5 (`net/http` server
  concurrency under GMP) applies when the server soak begins.
- `crypto/tls` must compile before `net/http` (RISK-M9-3) — the corpus
  covers plaintext only; TLS/`ServeTLS`/h2 programs are deferred to the
  TLS track's corpus, not invented here.
- Every program's gc leg is run by the runner, so each program is
  validated as real deterministic Go with exit 0.
