# test/m1c — sync, time and syscalls on the new path (status: **new**, per-push)

Named after toolexec milestone **M1c**: `sync`'s semaphores and notify
lists, `internal/bytealg`, `time` and the syscall executor, with the
conventions of [test/m1r/](../m1r/README.md).

- [run.sh](run.sh) — runs [programs/](programs/) vs gc. A program marked
  `//m1c:requires <probe>` is PENDING until the program of that name in
  [probes/](probes/) matches gc (another lane's piece landed).
- [mutants.sh](mutants.sh) — plants the bugs in [mutants.py](mutants.py) and
  requires the gate to fail (the gate's own test).

**Add a test:** a program in [programs/](programs/); a new syscall also
needs its line in [table.txt](../../internal/toolexec/syscalls/table.txt).

**Run:** `make test-m1c` (~15 s), `make test-m1c M1C_PROGRAMS=…`, `test/m1c/mutants.sh`.
