# `internal/vet` — static analyzers for `graalgo vet`

This package implements `go vet`-style static analyzers that run as part of
`graalgo vet`. Each analyzer inspects the parsed AST and type information
(via `go/parser` and `go/types`) and reports diagnostics.

## Implemented analyzers

| Name | Checks |
|------|--------|
| `printf` | Format string/argument consistency for `fmt.Printf`, `Sprintf`, `Fprintf`, `Errorf`, `Scanf`, `Sscanf`, `Fscanf`, `log.Printf/Fatalf/Panicf`, `testing.T.Errorf/Fatalf` |
| `unreachable` | Code after `return`, `panic`, `break`/`continue`/`goto`, or infinite loops |
| `stringintconv` | `string(int)` conversions that yield a single rune instead of digits |
| `nilfunc` | Comparisons of func values to nil that are always true/false |
| `bools` | Redundant `x == true`, suspicious `!x == y` precedence |
| `atomic` | Direct assignment to `sync/atomic` typed values (use `Store`) |
| `copylocks` | Locks (`sync.Mutex`, `RWMutex`, `WaitGroup`, or structs containing them) passed by value |
| `lostcancel` | `context.WithCancel/WithTimeout/WithDeadline` cancel func never called |
| `composites` | Unkeyed composite literals for imported types |
| `tests` | `TestXxx` functions without `*testing.T` parameter, bad `TestMain` signature |

## Design

- Analyzers are registered via `register()` in `init()` and run in order.
- `Pass` carries the `token.FileSet`, parsed `[]*ast.File`, and `*types.Info`.
- Type checking is best-effort: if `types.Config.Check` fails, analyzers
  still run on the AST with partial type info.
- `Run(paths)` returns diagnostics sorted by source position.

## Not yet implemented

The following `go vet` analyzers are not yet implemented:
`appends`, `asmdecl`, `assign`, `buildtag`, `cgocall`, `defers`, `directive`,
`errorsas`, `framepointer`, `hostport`, `httpresponse`, `ifaceassert`,
`loopclosure`, `shift`, `sigchanyzer`, `slog`, `stdmethods`, `stdversion`,
`structtag`, `testinggoroutine`, `timeformat`, `unmarshal`, `unsafeptr`,
`unusedresult`, `waitgroup`.
