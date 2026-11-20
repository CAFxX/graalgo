# `crypto/hpke` wave-3 corpus

Differential corpus for the crypto/hpke package (L11, REVIEW-2026-11-18-05).

## Scope boundary

RFC 9180 one-shot Seal/Open (DHKEM P-256 + HKDF-SHA256 + AES-128-GCM),
tampered-ciphertext and wrong-info rejection, Sender/Recipient streaming
API, ExportOnly AEAD. Ciphertext bytes are random; only lengths and
round-trip equality are pinned.

## Programs

- `00_basic.go` — HPKE Seal/Open round-trip, tampered-ciphertext rejection, Sender/Recipient, ExportOnly.

## Retest trigger

Any change to crypto/hpke Seal/Open, KEM/KDF/AEAD selection, or the
exporter.
