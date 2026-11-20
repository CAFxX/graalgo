# `crypto/rc4` wave-3 corpus

Differential corpus for the crypto/rc4 package (L11, REVIEW-2026-11-18-05).

## Scope boundary

RC4 stream cipher: exact keystream pins for fixed keys,
encrypt/decrypt round-trip, Reset reproducibility, bad-key-size
rejection.

## Programs

- `00_basic.go` — RC4 keystream pins, encrypt/decrypt round-trip, Reset, bad key sizes.

## Retest trigger

Any change to crypto/rc4 keystream output, Reset, or key-size
validation.
