# `go/parser` wave-3 corpus

Differential corpus for the go/parser package (L11, REVIEW-2026-11-18-05).

## Scope boundary

ParseFile on an embedded source string, ast.Inspect walk (FuncDecl/TypeSpec counts), deterministic positions (line:col), parse-error reporting. No .scopelimited programs. Only a small fixed source is parsed; full-file / directory parsing and comment maps are out of scope.

## Programs

- `00_parse.go` — ParseFile embedded source, ast.Inspect walk, positions, parse errors.

## Retest trigger

go/parser AST shape or position reporting changes.
