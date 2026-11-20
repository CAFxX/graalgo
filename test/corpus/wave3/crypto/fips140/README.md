# `crypto/fips140` wave-3 corpus

Differential corpus for the crypto/fips140 package (L11, REVIEW-2026-11-18-05).

## Scope boundary

FIPS 140-3 module status: Enabled, Enforced, Version, and the
WithoutEnforcement closure.

## Programs

- `00_basic.go` — FIPS module status: Enabled, Enforced, Version, WithoutEnforcement.

## Retest trigger

Any change to crypto/fips140 status reporting or the FIPS module
version.
