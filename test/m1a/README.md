# test/m1a — the language gate of the new path (status: **new**, per-push)

Named after toolexec milestone **M1a** (the whole language, no std). Each
program is built by stock `go build -toolexec graalgo-tool`, run as a linked
bundle on the new node package, and compared with gc on stdout, normalized
stderr and exit status.

- [run.sh](run.sh) — runs every [test/corpus/](../corpus/) program that
  imports nothing, [programs/](programs/) (areas the corpus is thin on:
  defers, Goexit, recover, range-over-func, generics, interfaces, the flat
  value model…), [multipkg/](multipkg/) modules and girinterp's programs
- [transform.py](transform.py) — makes phase-1 corpus programs print their
  `// observe:` variables (tested by [test_transform.py](test_transform.py))
- [tbnorm.py](tbnorm.py) — the stderr normalizer of programs with a
  `// m1a:traceback` line: keeps each traceback frame's name and file:line
  (tested by [test_tbnorm.py](test_tbnorm.py); ADR-0020)
- [defer_path.sh](defer_path.sh) — pins the open-coded defer limit and
  re-runs defer programs with every function forced onto the chain
- [bench.sh](bench.sh) / [bench.py](bench.py) — [test/bench/](../bench/README.md)
  workloads without std (`make bench-m1a`) and the PE gate (`make pe-check-m1a`)
- [pe_std.sh](pe_std.sh) / [pe/stdhot.go](pe/stdhot.go) — the PE gate's second
  half: the std paths of [test/bench/realistic/](../bench/realistic/README.md)
  hot with zero failed compilations, plus a generated planted violation

**Add a test:** a self-contained `package main` in [programs/](programs/)
(no imports; report with `println`). A program (a module: its `main.go`)
whose first lines hold `//m1a:ldflags <flags>` is built with
`-ldflags=<flags>` on both legs ([multipkg/xflags/](multipkg/xflags/)). **Not here:** std programs
([test/m1r/](../m1r/README.md)), `unsafe` ([test/m1w13/](../m1w13/README.md)).

**Run:** `make test-m1a` (~2.5 min) or `make test-m1a M1A_PROGRAMS="test/m1a/programs/x.go"`.
