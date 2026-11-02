# redgreen/ — frozen red-green evidence (2026-10-06)

These four files are committed evidence, not inputs: nothing reads them
(no Makefile, workflow, script, or test *reads* this directory;
`main_test.go` cites it in comments as evidence only).

- `green.log`, `green-rerun.log` — the green leg: `go test` for
  `graalgo/tools/stdlib-linkdiff` passing.
- `red-tagfilter.log` + `mutation-tagfilter.patch` — the red leg: the
  patch disables the build-tag filter in `scan.go` ("MUTATION (red-green
  evidence): tag filter disabled"), and the log shows
  `TestBuildTagGatedExcluded` failing with the filter disabled — proving
  the test catches the regression it guards against.

Keep as a fixture. Do not regenerate per run and do not delete the red
leg: the pair is the proof that the tag-filter test is non-vacuous. If
the tag-filter test changes, replace this directory's contents and
update this note in the same change.
