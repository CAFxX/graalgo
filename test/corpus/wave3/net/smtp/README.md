# `net/smtp` wave-3 corpus

Differential corpus for the net/smtp package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Sending mail through a fake SMTP server on loopback.

## Programs

- `00_basic.go` — Client handshake (Hello/Mail/Rcpt/Data/Quit) and SendMail; the server records the commands it saw, in order.

## Retest trigger

Any change to net/smtp client behaviour.
