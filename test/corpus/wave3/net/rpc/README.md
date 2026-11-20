# `net/rpc` wave-3 corpus

Differential corpus for the net/rpc package (L11, REVIEW-2026-11-18-05).

## Scope boundary

An RPC service over net.Pipe.

## Programs

- `00_basic.go` — Method calls with reply values, an error-returning method, unknown-method/service errors, async call.

## Retest trigger

Any change to net/rpc call handling.
