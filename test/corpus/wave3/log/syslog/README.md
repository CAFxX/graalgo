# `log/syslog` wave-3 corpus

Differential corpus for the log/syslog package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Writing to the system logger.

## Programs

- `00_basic.go` — New with a valid priority, Write, Close, bad-priority error path, Dial defaults.

## Retest trigger

Any change to log/syslog behaviour.
