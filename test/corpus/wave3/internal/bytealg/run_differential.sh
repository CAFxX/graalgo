#!/bin/bash
# Differential corpus for the wave-3 `internal/bytealg` package
# (scope-limited + nogc: internal/* is not user-importable) — thin wrapper
# over the shared wave-3 runner (conventions: corpus/wave3/README.md).
# Real verification: the pre-migration tier1d harness (archived).
exec "$(dirname "$0")/../../run_wave3.sh" internal/bytealg
