# `os/signal` wave-3 corpus

Differential corpus for the os/signal package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Notify for SIGUSR1, self-Kill, receive from the channel; Stop semantics; Ignore. Deterministic: fixed strings only, no wall clock. No .scopelimited programs. Only SIGUSR1/SIGUSR2 on the local process are exercised; cross-process signalling and SIGCHLD handling are out of scope.

## Programs

- `00_basic.go` — Notify SIGUSR1, self-Kill, receive; Stop; Ignore.

## Retest trigger

signal.Notify semantics change, or the backend gains signal delivery support it lacks today.
