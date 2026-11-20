# `net/http/fcgi` wave-3 corpus

Differential corpus for the net/http/fcgi package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Serving an HTTP handler over FastCGI.

## Programs

- `00_basic.go` — A loopback listener, one request through the FCGI server, status and body pinned.

## Retest trigger

Any change to net/http/fcgi serving.
