# test/m1c — sync, time and syscalls on the new path (status: **new**, per-push)

Named after toolexec milestone **M1c**: `sync`'s semaphores and notify
lists, `internal/bytealg`, `time` and the syscall executor, with the
conventions of [test/m1r/](../m1r/README.md).

- [run.sh](run.sh) — runs [programs/](programs/) vs gc. A program marked
  `//m1c:requires <probe>` is PENDING until the program of that name in
  [probes/](probes/) matches gc (another lane's piece landed).
- the gate's own test: its mutants are the `M1C-*` entries of
  [tools/mutation/mutants.json](../../tools/mutation/mutants.json) (nightly
  `make mutation`; `mutants.sh` was deleted at m3-plan D3).

**Add a test:** a program in [programs/](programs/); a new syscall also
needs its line in [table.txt](../../internal/toolexec/syscalls/table.txt).

**Run:** `make test-m1c` (~15 s), `make test-m1c M1C_PROGRAMS=…`, `make mutation MUTANTS=M1C-…`.
