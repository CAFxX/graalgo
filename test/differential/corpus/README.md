# Corpus

49 single-file Go programs (`package main`, stdlib only) covering the
language surface the smoke net guards. Each program is deterministic by
construction under `GOMAXPROCS=1`:

- map iteration output is key-sorted (13)
- goroutine/channel results combine order-independently: sums (14),
  WaitGroup barriers (14, 26), done-channel broadcast (41)
- no wall-clock values, no unseeded randomness, no printed addresses
  except 20 (`%p`, exercised through the stdout address normalizer)
- time is used only for ordering guarantees (`time.After` branches whose
  outcome is deterministic, 15; a fixed sleep before `close(done)`, 41)

Programs doubled as mutation targets (see `../mutations.txt`): 01, 02, 03,
04, 05, 06, 07, 08, 09. These were written so the mutator's *first*
eligible AST site is behavior-relevant (verified in validation).

Coverage map:

| #   | Program              | Surface covered                                  |
|-----|----------------------|--------------------------------------------------|
| 01  | hello                | const, stdout (ConstChange target)               |
| 02  | arithmetic           | int/uint/float ops, overflow wrap (OpSwap target)|
| 03  | control_flow         | if/switch/for/goto/labels (CmpSwap target)       |
| 04  | for_range            | classic for, range, range-over-int (OffByOne)    |
| 05  | slices               | slicing, append, 3-index, nil (SliceBound target)|
| 06  | nested_defer         | defer LIFO, defer in loop (DropDefer target)     |
| 07  | two_sum              | nested loops, algorithm (CondNegate target)      |
| 08  | multiple_assignment  | multi-return, parallel assign (ReturnSwap)       |
| 09  | pipeline             | goroutines, channels, close (ChanOp target)      |
| 10  | closures             | closures, capture, func values                   |
| 11  | methods              | methods, pointer receivers, embedding            |
| 12  | interfaces           | dispatch, type assertion                         |
| 13  | maps_sorted          | maps, delete, comma-ok, sorted output            |
| 14  | goroutines_sum       | fan-out/fan-in, buffered channels                |
| 15  | select               | select, default, time.After                      |
| 16  | defer_panic_recover  | panic/recover, named returns                     |
| 17  | generics             | generic funcs/types, constraints                  |
| 18  | errors               | wrap, Is/As/Unwrap                               |
| 19  | sort                 | sort package, Search                             |
| 20  | fmt_formats          | printf verbs incl. %p (normalized)               |
| 21  | pointers             | address-of, deref, nil                           |
| 22  | conversions          | numeric/string/rune conversions                  |
| 23  | bitwise              | shifts, masks, iota                              |
| 24  | variadic             | variadic calls, slice spread                     |
| 25  | recursion            | recursion, memoization                           |
| 26  | sync_primitives      | Mutex, WaitGroup, Once, RWMutex                  |
| 27  | panic_trace          | unrecovered panic → normalized stack trace       |
| 28  | exit_code            | stderr + os.Exit(3)                              |
| 29  | interface_nil        | typed-nil vs nil interface                       |
| 30  | type_switch          | type switches incl. Stringer case                 |
| 31  | string_switch        | string switch, multi-case                        |
| 32  | defer_args           | defer argument evaluation timing                 |
| 33  | recover_reraise      | recover then re-panic across frames              |
| 34  | struct_reflect       | reflect types, tags                              |
| 35  | map_zero             | zero values, presence testing                    |
| 36  | matrix_mult          | arrays, nested loops                             |
| 37  | utf8                 | runes, Builder, unicode                          |
| 38  | iota_enum            | iota enumerations                                |
| 39  | named_returns        | named results, naked return                      |
| 40  | method_values        | method values/expressions                        |
| 41  | done_channel         | done-channel broadcast pattern                   |
| 42  | json                 | encoding/json, sorted map keys                   |
| 43  | range_over_func      | range-over-func iterators (1.23+)                |
| 44  | copy_builtin         | copy incl. overlapping                           |
| 45  | shadowing            | variable shadowing                               |
| 48  | range_over_func_edge | range-over-func exits: return/goto/labels,       |
|     |                      | body-defer LIFO, `=` form, iterator-misuse       |
|     |                      | panics (companion to 43)                         |
| 49  | reflect_makefunc_    | variadic reflect.MakeFunc: callback arg packing  |
|     | variadic             | for 0/1/N variadic args, CallSlice round-trip,   |
|     |                      | too-few-args panic                               |

Excluded by design (documented, not silent): map iteration order printed
raw, wall-clock timings, `unsafe`, cgo, network/file-system effects,
`runtime.GOMAXPROCS` > 1 scheduling observables — these belong to later
conformance layers, not the smoke net.
