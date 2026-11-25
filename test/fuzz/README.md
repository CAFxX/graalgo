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

The nightly [fuzz-soak.yml](../../.github/workflows/fuzz-soak.yml) Layer 4a
runs this generator too (`make fuzz-newpath FUZZ_CORPUS=0`, in batches, on a
seed range disjoint from fuzz-newpath.yml's). v1's fuzzers are gone:
`tools/fuzz` was deleted at m3-plan D0, `tools/fuzz-soak` (with its
`graalgo-sync-run` harness) was deleted at D3.
