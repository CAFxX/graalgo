#!/bin/bash
# Differential corpus for the wave-3 `internal/bytealg` package
# (scope-limited + nogc: internal/* is not user-importable) — thin wrapper
# over the shared wave-3 runner (conventions: corpus/wave3/README.md).
# Exit status as run_wave3.sh (0 pass, 1 divergence, 3 infra error).
# The program's own self-check needs a gc-vs-synthetic-source harness that
# does not exist in the tree (see 00_functions.go).
exec "$(dirname "$0")/../../run_wave3.sh" internal/bytealg
