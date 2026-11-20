# syscalls — the syscall role table (status: **new**)

[table.txt](table.txt) says, per linux/amd64 syscall number, what each
argument word is (value, C string, buffer, pointer to a typed struct, …) and
whether it may block, so the executor
([graalgo/syscall/](../../backend/truffle/src/main/java/graalgo/syscall/))
can marshal Go handles to native memory via Panama. [gen/](gen/) generates
`SyscallTable.java`; [scan.go](scan.go) derives roles from std's own call
sites and [check.go](check.go) compares them with the table.
**A wrong entry is memory-unsafe** (risk R15,
[toolexec-design.md](../../../docs/arch/toolexec-design.md) §5.6, §10).

**Add a syscall:** add the line, `make generate`, make the table-vs-source
check pass; add a program to [test/m1c/programs/](../../../test/m1c/programs/).

**Test:** `cd internal/toolexec && go test ./syscalls`; `make test-m1c`.
