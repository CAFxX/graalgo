# `net/mail` wave-3 corpus

Differential corpus for the net/mail package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Parsing mail addresses and messages.

## Programs

- `00_basic.go` — ParseAddress, AddressList, ReadMessage headers and body, and the error paths.

## Retest trigger

Any change to net/mail parsing.
