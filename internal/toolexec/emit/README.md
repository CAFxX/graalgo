# emit — Go → GIR lowering (status: **new**; "the frontend")

Lowers a go/types-checked package to GIR, a resolved typed AST: every
identifier resolved, every expression typed, constants folded, selectors
resolved to field paths and calls, implicit conversions explicit, closures
with capture lists, generics left uninstantiated. Anything it cannot lower
faithfully makes `Emit` fail; it never emits a placeholder.
Design: [toolexec-design.md](../../../docs/arch/toolexec-design.md) §3.

**Not here:** the GIR format ([../ir/](../ir/)); execution
([graalgo/go/build/](../../backend/truffle/src/main/java/graalgo/go/build/)
on the JVM, [../girinterp/](../girinterp/) in tests). The v1 frontend
([internal/frontend/](../../frontend/)) is legacy — never borrow from it.

**Test:** `cd internal/toolexec && go test ./emit ./girinterp` (goldens,
std compile-only sweep `std_test.go`, girinterp differential vs gc); then
`make test-m1a`.
