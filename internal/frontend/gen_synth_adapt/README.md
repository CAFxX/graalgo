# `gen_synth_adapt/` — frontend-constraint adaptation patches

The [`gen_synth_*.py`](../) generators extract gc 1.27.1 sources verbatim
(plus documented mechanical rewrites). For some packages the committed
`synth_*.go` additionally carries **hand-applied frontend-constraint
adaptations** that the generator does not model — e.g. WS D (no multi-result
calls: pair-struct desugar, no `*b = Reader{...}` StarExpr assignment,
identifier LHS for desugar) and WS G (no `max` builtin).

Rather than leaving those files unwired (P1-R1-2) or encoding dozens of
ad-hoc rewrites back into the generators, each such file has an explicit
patch here:

- `synth_bufio.patch` — pair-struct `__bufioDecodeRune`, WS D/G rewrites
- `synth_compress_{bzip2,flate,gzip,lzw,zlib}.patch` — WS D adaptations
  (struct unembedding, int32 range loops, fail-fast Read helpers, ...)

[`run_gen_synth.py`](../run_gen_synth.py) runs each generator into a staging
directory, applies the matching patch (if any), and copies the result over
`internal/frontend/`. `make gen-synth` runs the driver; CI's
`git diff --exit-code` then verifies the committed files are exactly what the
pipeline produces.

## Editing a patched file

1. If the change is a **gc-source update or generator bug**: edit the
   `gen_synth_*.py` generator.
2. If the change is a **frontend-constraint adaptation**: edit the `.patch`
   file here (unified diff, `a/`/`b/` labels, applied with `patch -p1`).
3. Re-run `make gen-synth` (or `python3 internal/frontend/run_gen_synth.py`)
   and commit the regenerated `synth_*.go` together with the generator/patch
   change. Never edit a `synth_*.go` by hand — the next regeneration would
   silently revert it and CI would go red.

A patch that no longer applies means the generator's output changed under
it (e.g. a toolchain upgrade): that is a loud signal to re-derive the
adaptations, not something to force through with fuzz.
