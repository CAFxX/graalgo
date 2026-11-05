#!/bin/bash
# Differential corpus for the wave-3 `internal/bytealg` package
# (scope-limited + nogc: internal/* is not user-importable) — thin wrapper
# over the shared wave-3 runner (conventions: corpus/wave3/README.md).
# Thin wrapper: delegates to the wave-3 runner for this package dir.
# (The old tier1d harness under phases/phase3/hidden_files/m9-tmp/ is gone
# with the phases/ layout; run_wave3.sh is the live runner.)
exec "$(dirname "$0")/../../run_wave3.sh" internal/bytealg
