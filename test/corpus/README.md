# test/corpus — the research-era differential corpus (status: **shared**, v1-era)

Go programs whose output is compared with gc. Written for the v1 pipeline,
now run on the new path: [test/m1a/](../m1a/README.md) runs every program
here that imports nothing (per push;
[girinterp](../../internal/toolexec/girinterp/README.md) too), and `make
stdlib-gate` every program that imports a package (486, manual
[stdlib-gate.yml](../../.github/workflows/stdlib-gate.yml)): test/m1r's
runner against gc, judged by [std_gate.py](std_gate.py) (tested by
[test_std_gate.py](test_std_gate.py)) against the reviewed register
[std-register.tsv](std-register.tsv), BUG-0033's triage list.
Frozen in spirit: new-path programs go to `test/m1*/programs/`.

- [phase1/](phase1/), [phase2/](phase2/) — v1-era language/scheduler corpora (`// observe:` comments)
- [wave1/](wave1/README.md), [wave2/](wave2/README.md), [wave3/](wave3/README.md) — stdlib corpora per package
  (their v1 `run_differential.sh` runners were deleted at m3-plan D3)
- topic corpora: [atomic/](atomic/), [fastrand/](fastrand/), [iface/](iface/),
  [map/](map/), [slice/](slice/), [sync/](sync/), [timectx/](timectx/),
  [traceback/](traceback/)
- [lifecycle/](lifecycle/README.md) — exit, fatal panics, deadlock detection
  vs gc; the stderr normalizer it introduced, every new-path gate's, is now
  [test/lib/normalize.py](../lib/normalize.py)

**Not here:** the snapshot corpus of `make snapshot-check`
([test/differential/corpus/](../differential/corpus/README.md)); gc's
recorded output ([test/snapshots/](../snapshots/README.md)).
Per-wave run logs in this tree are history (exempt from the doc-path gate).
