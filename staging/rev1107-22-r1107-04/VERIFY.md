# VERIFY — lane rev1107-22-r1107-04 (R-1107-04, P1) — green-run summary

Full evidence: `EVIDENCE.md` in this dir. HEAD `59f0a247`.

## RED (pristine HEAD, re-run this session)

- `python3 tools/validate_frontend_v1.py pkgvar_mrarg.json` → **104 schema
  errors, exit 1** (`validator-red-rerun.log`): 91× line-0 + 13× negative len.
  Independent JSON walk counts 104 invalid-pos nodes. First error:
  `funcs[8].fields.params[0].pos.len: must be non-negative int, got -2458`.

## GREEN (patched, built in /tmp)

- `./build.sh build` in `fe/` → exit 0 (`build-patched.log`).
- Fresh export `./gofrontend testdata/pkgvar_mrarg.go` → exit 0 (4,619,894 B).
- `validate_frontend_v1.py` on the fresh export → **exit 0, "valid frontend/v1"**
  (`validator-green.log`). Positions-only vs old golden: non-`pos` trees
  identical, 263 differing leaf paths all inside `pos`; 0 invalid positions.
- `./build.sh check` (300.6s): gofmt ✓, vet ✓, go test — exactly ONE failure,
  `TestSyncPoolRtatomicLowersToSyncop`, cause `stat ../../test/differential/
  corpus/46_sync_pool/main.go: no such file or directory` — the `/tmp` layout
  lacks `test/`; the file exists in the real repo. Controls with the file
  stubbed in: PATCHED **PASS** (`patched-single-test.log`), PRISTINE **PASS**
  (`pristine-single-test.log`). Environmental, not a regression.
- Remaining check stages run manually, ALL PASS (`check-stages.log`):
  vendoring identical, validator selftest 23/23 ok, all 5 `.json` goldens valid,
  NEW `.json.gz` golden valid, all 5 fresh exports valid.
- `gofmt -l export.go` clean; `sh -n build.sh` clean.
- Patch `R-1107-04.patch` (3 files) applies cleanly to pristine `59f0a247`
  and reproduces the staged files byte-identically.

## Status

Fix verified RED→GREEN. BLOCKED on home quota (EDQUOT, re-probed 22:14 CET):
no worktree, no commit, no push. Apply per `RESUME.md` on quota recovery.
