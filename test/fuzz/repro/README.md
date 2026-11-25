# Fuzz reproducers

Minimal reproducers of open bugs found by the fuzzer, each listed in
[known.txt](../known.txt) with its pinned signature and citing a `BUG-NNNN`
of [bugs.md](../../../docs/dev/bugs.md). `make fuzz-newpath` runs every
`*.go` here with the corpus. A fixed bug's reproducer moves to the gate it
belongs to, or is deleted when a gate's program already covers it. None is
open at the moment.
