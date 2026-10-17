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
| `buildtag` | Malformed `// +build` lines, `// +build` lines misplaced after the package clause |
| `asmdecl` | Mismatches between assembly files and Go declarations |
| `cgocall` | Violations of the cgo pointer passing rules |
| `framepointer` | Assembly that clobbers the frame pointer before saving it |
| `stdversion` | Uses of standard library symbols too new for the go.mod version |

## Design

- Analyzers are registered via `register()` in `init()` and run in order.
- `Pass` carries the `token.FileSet`, parsed `[]*ast.File`, and `*types.Info`.
- Type checking is best-effort: imports are resolved from source by
  `sourceImporter` ([importer.go](importer.go)) — `importer.Default()`'s
  gc export-data lookup cannot work here (`$GOROOT/pkg` no longer ships
  compiled archives), and shelling out to `go list -export` would hard-wire
  a dependency on the reference toolchain into `graalgo vet`. Resolution is
  purely by import path via `go/build` (GOROOT for stdlib, GOPATH
  otherwise); no package names are special-cased. Imports that cannot be
  resolved are reported as `could not import ...` diagnostics rather than
  silently degrading import-dependent checks, and type-gated analyzers
  treat unresolvable types as unknown rather than as violations.
- `Run(paths)` returns diagnostics sorted by source position.
- `asmdecl` and `framepointer` discover `.s` files in the directories
  containing the analyzed Go files.
- `stdversion` reads the symbol introduction database from
  `$GOROOT/api/go1*.txt` and the module version from the enclosing
  `go.mod` (raised per-file by `//go:build go1.X` constraints).

## Notes on behavior differences

- `loopclosure`: Go 1.22+ gives each iteration its own loop variables,
  so the classic bug is fixed. We flag only explicit `=` assignments
  to outer variables captured by closures.
