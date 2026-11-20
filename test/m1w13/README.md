# test/m1w13 — the pointer-model gate (status: **new**, per-push)

Named after toolexec work package **W13**, the pointer model
([ADR-0015](../../docs/dev/adr/ADR-0015-pointer-model-w13.md), SPEC §3.4).
[run.sh](run.sh) runs programs that use package `unsafe` through
graalgo-tool on the new node package against gc.

- [programs/](programs/) — `unsafe` idioms of real code
- [checkptr/](checkptr/) — programs gc's `-d=checkptr` stops (same fatal text)
- [restricted/](restricted/) — programs gc runs and the flat value model
  stops by design ([limitations.md](../../docs/arch/limitations.md))
- [gen.py](gen.py) — generates the struct-layout corpus and the access
  program; [layout_corpus.txt](layout_corpus.txt) is gc's layout output, also
  the fixture of the backend's `LeafTableTest`

**Run:** `make test-m1w13` (~1.5 min) or `make test-m1w13 M1W13_PROGRAMS=…`.
