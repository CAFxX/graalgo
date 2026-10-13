#!/bin/bash
# Differential corpus for the wave-3 `weak` package (scope-limited) —
# thin wrapper over the shared wave-3 runner (conventions:
# corpus/wave3/README.md).
exec "$(dirname "$0")/../run_wave3.sh" weak
