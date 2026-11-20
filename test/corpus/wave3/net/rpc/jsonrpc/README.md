# `net/rpc/jsonrpc` wave-3 corpus

Differential corpus for the net/rpc/jsonrpc package (L11, REVIEW-2026-11-18-05).

## Scope boundary

RPC over the JSON codec.

## Programs

- `00_basic.go` — Calls, replies, division-by-zero error, and the unknown-method error path.

## Retest trigger

Any change to net/rpc/jsonrpc codec behaviour.
