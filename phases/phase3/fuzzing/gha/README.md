# graalgo-fuzz GHA tenant

Differential fuzzing of graalgo vs gc 1.27.1 on GitHub Actions via the
CAFxX/fanout repo.

## Layout (in fanout repo: `projects/graalgo-fuzz/`)

```
projects/graalgo-fuzz/
  Dockerfile            # Go 1.27.1 + GraalVM 25 + graalgo slim tarball
  graalgo-slim.tar.gz   # pinned graalgo sources (see below)
  driver/fuzz_driver.py # shard driver (seed ranges, canary, differential)
  aggregate.py          # local post-fetch aggregation (lives here too)
  README.md             # this file
```

## graalgo-slim.tar.gz

Built from a graalgo worktree at the pinned commit:

```bash
cd ~/workspace/graalgo-m9   # at the commit to fuzz
tar -czf graalgo-slim.tar.gz \
  phases/phase1/src phases/phase1/lib phases/phase1/frontend \
  phases/phase1/Makefile \
  phases/phase2/src phases/phase2/Makefile \
  phases/phase3/src phases/phase3/fuzzing phases/phase3/Makefile
```

Rebuild and re-push the Dockerfile whenever the pinned graalgo commit
changes (the image bakes the sources).

## Submitting a campaign

Via the PAT Git Data API pattern (`~/workspace/skills/github-pat/bin/push_fanout.py`):

1. Push `projects/graalgo-fuzz/` (Dockerfile + tarball + driver) — the
   build-image.yml workflow builds `ghcr.io/CAFxX/fanout-graalgo-fuzz:latest`.
2. Write `jobs/graalgo-fuzz/<job-id>.json` specifying the image, shard
   count, and `SEEDS_PER_SHARD` env; push via PAT.
3. Poll `results/graalgo-fuzz/<job-id>/STATUS.json`; fetch shard results;
   run `aggregate.py` locally.

## Pilot (Phase 2)

- 20 shards x 50 seeds = 1000 programs.
- Golden canary: seed 999999 must PASS per shard, else quarantine.
- Results are leads: re-verify raw logs locally before filing bugs.
