# `net/http/cookiejar` wave-3 corpus

Differential corpus for the net/http/cookiejar package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Cookie storage.

## Programs

- `00_basic.go` — SetCookies/Cookies round-trip, domain and path matching, expiry, overwrite, Secure flag.

## Retest trigger

Any change to net/http/cookiejar storage or matching.
