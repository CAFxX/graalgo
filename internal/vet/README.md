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
| `structtag` | Struct field tags must conform to `reflect.StructTag.Get` (malformed pairs, duplicate keys) |
| `assign` | Useless self-assignments (`x = x`) |
| `shift` | Shifts that equal or exceed the width of the integer |
| `ifaceassert` | Impossible interface-to-interface type assertions |
| `waitgroup` | `sync.WaitGroup.Add` called from inside a new goroutine |
| `httpresponse` | `defer resp.Body.Close()` before checking error (nil dereference) |
| `defers` | `time.Since` call not deferred (evaluated immediately) |
| `loopclosure` | References to loop variables from nested functions (pre-1.22 semantics) |
| `errorsas` | Second arg to `errors.As` must be pointer to error or interface |
| `unmarshal` | Non-pointer values passed to `encoding/json.Unmarshal` etc. |
| `unusedresult` | Discarded results of pure functions (`fmt.Sprintf`, `strings.Replace`, etc.) |
| `timeformat` | `2006-02-01` instead of `2006-01-02` in time layouts |
| `appends` | Discarded `append` results |
| `slog` | Invalid `log/slog` calls (odd args, non-string keys) |
| `stdmethods` | Well-known methods (`String`, `Error`, `Read`, `Write`) with wrong signatures |
| `testinggoroutine` | `(*testing.T).Fatal` etc. from non-test goroutines |
| `unsafeptr` | Invalid `uintptr` to `unsafe.Pointer` conversions |
| `sigchanyzer` | `signal.Notify` with unbuffered channel |
| `hostport` | `net.Dial` with concatenated or port-less addresses |
| `directive` | Unknown `//go:` directives |
| `buildtag` | Malformed `// +build` lines |

## Design

- Analyzers are registered via `register()` in `init()` and run in order.
- `Pass` carries the `token.FileSet`, parsed `[]*ast.File`, and `*types.Info`.
- Type checking is best-effort: if `types.Config.Check` fails, analyzers
  still run on the AST with partial type info.
- `Run(paths)` returns diagnostics sorted by source position.

## Not yet implemented

The following `go vet` analyzers are not yet implemented:
`asmdecl` (requires assembly parser), `cgocall` (requires cgo pointer
analysis), `framepointer` (requires assembly parser), `stdversion`
(requires stdlib symbol version database).

## Notes on behavior differences

- `loopclosure`: Go 1.22+ gives each iteration its own loop variables,
  so the classic bug is fixed. We flag only explicit `=` assignments
  to outer variables captured by closures.
