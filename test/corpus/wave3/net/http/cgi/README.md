# `net/http/cgi` wave-3 corpus

Differential corpus for the net/http/cgi package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Serving a CGI program.

## Programs

- `00_basic.go` — A generated script prints headers + body; status, headers, and body pinned; missing-binary error path (logger silenced to keep stderr deterministic).

## Retest trigger

Any change to net/http/cgi handling or CGI env passing.
