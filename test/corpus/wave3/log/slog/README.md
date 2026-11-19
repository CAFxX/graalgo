# `log/slog` wave-3 corpus

Differential corpus for the log/slog package (L11, REVIEW-2026-11-18-05).

## Scope boundary

TextHandler + JSONHandler to bytes.Buffer with time/source stripped via ReplaceAttr (deterministic output), levels, groups, attrs, With, LogValuer, level filtering. No .scopelimited programs. log (the std log package) is covered elsewhere; slog's default handler (stderr, with wall-clock time) is deliberately not used.

## Programs

- `00_basic.go` — TextHandler+JSONHandler (time/source stripped), levels, groups, With, LogValuer, filtering.

## Retest trigger

slog handler output format changes (new keys, new level names).
