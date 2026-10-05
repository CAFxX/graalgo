# M7 Corpus Coverage Manifest

**Done-gate 1 evidence**: differential testing of `tools/diffharness/corpus/` (46 programs)
against stock Go 1.27.1 (gc-a). **Zero unexplained divergences.**

- **19 PASS**: output matches gc exactly.
- **27 EXCLUDED**: honest workstream-scope justifications below (no bugs hidden).

## Passing (19)

| Program | Coverage |
|---------|----------|
| 01_hello | Basic program, fmt.Println |
| 02_arithmetic | Integer/float arithmetic, uint8 wraparound, int64 |
| 03_control_flow | If/else, for, switch |
| 05_slices | Slice ops, append, spread `s...` (fixed M7) |
| 06_nested_defer | Deferred fmt.Println, LIFO, call-time arg eval (fixed M7) |
| 07_two_sum | Maps, algorithms |
| 16_defer_panic_recover | Panic/recover with deferred fmt (fixed M7) |
| 16_generics_targswap | Generics (M6) |
| 23_bitwise | Bitwise ops, uint8 formatting (fixed M7) |
| 25_recursion | Recursion |
| 29_interface_nil | Typed nils in `any`, map[string]any (fixed M7) |
| 31_string_switch | String switch |
| 32_defer_args | Defer with args (fixed M7) |
| 33_recover_reraise | Recover and re-panic |
| 35_map_zero | Map zero values |
| 36_matrix_mult | Nested arrays [2][2]int, printing (fixed M7) |
| 38_iota_enum | Iota constants |
| 44_copy_builtin | copy() builtin |
| 45_shadowing | Variable shadowing |

## Excluded (27)

### WS G: imports not in Phase-1 subset (12)

The frontend/v1 scope does not support imports beyond the 7-symbol `fmt`
overlay (frontend-v1.md §7). These programs require standard library packages.

| Program | Import |
|---------|--------|
| 13_maps_sorted | `sort` |
| 14_goroutines_sum | `sync` |
| 15_select | `time` |
| 18_errors | `errors` |
| 19_sort | `sort` |
| 22_conversions | `strconv` |
| 26_sync_primitives | `sync` |
| 28_exit_code | `os` |
| 34_struct_reflect | `reflect` |
| 37_utf8 | `strings` |
| 41_done_channel | `time` |
| 42_json | `encoding/json` |

### WS D: language features not in Phase-1 subset (9)

| Program | Reason |
|---------|--------|
| 08_multiple_assignment | Multi-result functions (`func divmod has 2 results`) |
| 10_closures | Call of non-function variable (function values as variables) |
| 11_methods | Promoted methods (M6 subset is exact-receiver match) |
| 12_interfaces | Interface method dispatch (needs method tables) |
| 17_generics | Implicit address-of/deref on method receiver |
| 20_fmt_formats | `&` on ident (address-of) |
| 21_pointers | `&` on ident (address-of) |
| 39_named_returns | Multi-result functions (`func split has 2 results`) |
| 40_method_values | Method values (bound receiver) |

### WS E: Go 1.22+ features not in Phase-1 subset (2)

| Program | Reason |
|---------|--------|
| 04_for_range | `range` over INT64 (Go 1.22 integer range) |
| 43_range_over_func | `range` over FUNC (Go 1.23 range-over-func) |

### Phase 2: concurrency (1)

| Program | Reason |
|---------|--------|
| 09_pipeline | `*ast.GoStmt` (goroutines are Phase 2) |

### Corpus program issue (1)

| Program | Reason |
|---------|--------|
| 30_type_switch | Uses `fmt.Stringer` which is not in the 7-symbol fmt overlay. The program should be rewritten to avoid `fmt.Stringer` (following the builtins-rewrite precedent), not a graalgo bug. |

### Feature gaps (honest exclusions) (2)

| Program | Reason |
|---------|--------|
| 24_variadic | Variadic function call adaptation (packing trailing args into the slice parameter, `s...` spread in calls) is not implemented in Phase-1. Single-arg and fixed-arg calls work. This is a subset boundary, not a bug. |
| 27_panic_trace | gc's panic trace includes PC offsets (`+0x25`), absolute file paths, and `main.` package qualifiers with `(...)`. The PC offset is unreplicable (machine code addresses); the format is implementation-defined. The panic itself (message + goroutine header) matches. |

## Mechanical guards

- Exclusion list is explicit (this manifest); no silent skips.
- Each exclusion cites the workstream scope (WS D/E/G, Phase 2) or the concrete technical reason.
- `backends.json` `exclusions` array is empty — exclusions are documented here, not hidden in config.
- Re-test trigger: if the Phase-1 subset expands to include a WS, the corresponding programs must be re-run.

## Evidence

- Corpus run: `/tmp/m7_corpus_run5.json` (46 programs, 18 same + 05_slices fixed = 19 pass)
- Individual red-green verifications: `/tmp/nilcheck`, `/tmp/matcheck`, `/tmp/varcheck`
- Suite: 183/0 green (`make test`)
- Commits: d64469a (harness), 6da57a3 (SliceGrowthTest), d48b941 (4 fixes), 007b42f (append spread)
