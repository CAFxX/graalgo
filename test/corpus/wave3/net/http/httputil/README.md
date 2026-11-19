# `net/http/httputil` wave-3 corpus

Differential corpus for the net/http/httputil package (L11, REVIEW-2026-11-18-05).

## Scope boundary

ReverseProxy to an httptest server, DumpRequest on a fixed-Host request, DumpResponse with the varying Date header filtered. Only deterministic facts are printed (the server port varies). No .scopelimited programs. Raw dump bytes are deliberately not pinned (port + Date vary); Contains checks + filtered dumps are the pins.

## Programs

- `00_proxy.go` — ReverseProxy, DumpRequest, DumpResponse (Date filtered) vs httptest server.

## Retest trigger

httputil dump/proxy output format changes.
