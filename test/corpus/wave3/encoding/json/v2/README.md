# `encoding/json/v2` wave-3 corpus

Differential corpus for the encoding/json/v2 package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Semantic JSON processing.

## Programs

- `00_basic.go` — Marshal/Unmarshal of structs, maps, and slices; field tags; unknown-member rejection; MarshalWrite/UnmarshalRead; error paths.

## Retest trigger

Any change to encoding/json/v2 marshaling or unmarshaling.
