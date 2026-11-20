# `crypto/x509/pkix` wave-3 corpus

Differential corpus for the crypto/x509/pkix package (L11, REVIEW-2026-11-18-05).

## Scope boundary

pkix.Name <-> RDNSequence round-trip (exact DER pins), ExtraNames,
Extension marshal, empty-name encoding.

## Programs

- `00_basic.go` — pkix.Name RDN round-trip (exact DER pins), ExtraNames, Extension marshal.

## Retest trigger

Any change to crypto/x509/pkix name encoding, RDN handling, or
Extension encoding.
