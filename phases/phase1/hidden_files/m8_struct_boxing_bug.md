# P0 Bug: Struct field write after boxing to `any` fails

**Found by:** M8 fuzzer (seed 1001, minimized)
**Severity:** P0 (any struct field write after `var x any = v` fails)
**Status:** FIXED (M8 closeout, commit 954a9bd). **The "boxing corrupts"
hypothesis below was WRONG** — direct `fmt.Println(v44)` (no interface)
crashes identically. Root cause: `Fmt.structFieldBoxed` passed the
*carrier* index (`Field.index()`, position within longs[]/objects[]) to
`GoStruct.getLong/getObject`, which expect the *field* index (position in
the descriptor's field list). Any mixed struct (e.g. {int, string}) crashed
printing; pure-longs/pure-objects structs passed by accident.
**Fix:** `structFieldBoxed` takes the field index; documents the
field-index vs carrier-index distinction. Class hunt: all other
getLong/getObject/setLong/setObject sites correct — confusion isolated.
**Red-green:** `corpus/builtins/bi20_struct_print.go` (mixed/direct/boxed/
nested/%+v/%#v); verified red on old code, green on new, gc-identical.

## Repro
```go
package main
import "fmt"
type fzPair struct { A int; B string }
func main() {
    v44 := fzPair{128, "a"}
    v44.A = 999
    fmt.Println(v44.A)  // works: prints 999
    var x any = v44     // boxing corrupts v44
    fmt.Println(x)      // works
    v44.A = 111         // FAILS: IllegalArgumentException: field A is not an objects-carried field
    fmt.Println(v44.A)
}
```

## Also fails
- `v44.A += -1000` after `fmt.Println(v44)` (original fuzzer find)
- `v44.A = 999` after `fmt.Println(v44)`

## Works
- Field writes BEFORE any boxing
- Single-field structs (no, actually fails too — tested with 2 fields; need to verify 1-field)

## Hypothesis
Assigning a struct value to `any` (or passing to `...any`) boxes the struct.
The boxing appears to corrupt the original variable's representation —
subsequent `SelectorWriteNode` calls `GoStruct.setObject` on a longs-carried
field, meaning `Field.inLongs()` returns false incorrectly (wrong descriptor
or wrong field index).

## Re-test trigger
Fix the boxing to copy (not share/corrupt), then verify:
- structplus7.go, structplus8.go, structplus9.go all pass
- Fuzzer seeds 1001, 1002, 1006, 1007, 1008 (struct field += patterns)
