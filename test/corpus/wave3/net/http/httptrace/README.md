# `net/http/httptrace` wave-3 corpus

Differential corpus for the net/http/httptrace package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Tracing an HTTP client request.

## Programs

- `00_basic.go` — Which hooks fire (booleans only, no timings); a second request fires them again.

## Retest trigger

Any change to net/http/httptrace hook behaviour.
