# P1 Bug: Nested map printing ClassCastException

**Found by:** M8 fuzzer (seed 1006, minimized)
**Severity:** P1 (nested maps with non-ASCII keys fail to print)
**Status:** Pre-existing (needs baseline verification)

## Repro
```go
package main
import "fmt"
func main() {
    v18 := map[string]map[string]int{"hello world": {"héllo": 3}}
    fmt.Println(v18)  // FAILS: ClassCastException: Long cannot be cast to GoInterface
}
```

## gc output
```
map[hello world:map[héllo:3]]
```

## Error
```
java.lang.ClassCastException: class java.lang.Long cannot be cast to class graalgo.values.GoInterface
```

## Hypothesis
When printing a nested map, the inner map's values (or keys) are being
unboxed incorrectly. The non-ASCII key "héllo" suggests the issue is in
how map iteration handles the inner map's entries — a Long (the int value 3)
is being cast to GoInterface.

## Re-test trigger
Fix map value boxing for nested maps, then verify mapnested.go prints correctly.
