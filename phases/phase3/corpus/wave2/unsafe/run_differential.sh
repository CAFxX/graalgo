#!/bin/bash
# Differential corpus for the wave-2 `unsafe` package — thin wrapper
# over the shared wave-2 runner (conventions: corpus/wave2/README.md).
exec "$(dirname "$0")/../run_wave.sh" unsafe
