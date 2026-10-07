# Generator reproducibility note (coordinator self-attack, 2026-10-07)

Re-running `gen_synth_syscall.py` (with `~/tools/go/bin` on PATH for gofmt)
reproduces `phases/phase1/frontend/synth_syscall.go` byte-identically EXCEPT
for one doc-comment line (gofmt doc-comment list reflow: `//   - methods,`
vs `//     + methods,`). Zero semantic drift; the checked-in file is
gofmt-clean under go1.27.1's gofmt.

The checked-in file is the reference. If the generator is re-run, diff the
output and confirm the only delta is that comment line before accepting.
