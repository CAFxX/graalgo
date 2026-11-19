# `html/template` wave-3 corpus

Differential corpus for the html/template package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Escaping pins: same input via html/template vs text/template, attribute context, trusted template.HTML, URL query escaping. No .scopelimited programs. Only the escaping behaviour is pinned; template inheritance / parsing edge cases live with text/template.

## Programs

- `00_escaping.go` — Escaping pins vs text/template, attribute context, trusted HTML, URL escaping.

## Retest trigger

html/template escaping rules change.
