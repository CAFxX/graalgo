# test/fuzz — differential fuzzer of the new path (status: **new**, nightly)

Not per-push: `make fuzz-newpath N=200 SEED=1`, nightly in
[fuzz-newpath.yml](../../.github/workflows/fuzz-newpath.yml).

- [run.py](run.py) — builds each program with gc and through graalgo-tool
  ([setup.sh](setup.sh)), classifies it (match, diverge, gg-crash, gg-build,
  timeout, bad-program), dedups signatures, delta-debugs with `--minimize`
- [gen.py](gen.py) — seeded program generator
- [corpus/](corpus/) — hand-written adversarial programs by area and round
- [repro/](repro/) + [known.txt](known.txt) — minimal reproducers of open
  bugs with their pinned signatures (each cites a `BUG-NNNN` of
  [bugs.md](../../docs/dev/bugs.md)); a fixed bug's reproducer moves to the
  gate it belongs to ([test/m1a/](../m1a/README.md), [test/m1r/](../m1r/README.md), …)
- [test_fuzz.py](test_fuzz.py) — the harness's own tests

**Not here:** the v1-era fuzzers [tools/fuzz/](../../tools/fuzz/) and
[tools/fuzz-soak/](../../tools/fuzz-soak/) (they drive `graalgo-sync-run`).
