#!/bin/bash
# Differential corpus for the wave-1 `encoding/gob` package — thin wrapper
# over the shared wave-1 runner (conventions: corpus/wave1/README.md).
exec "$(dirname "$0")/../../run_wave.sh" gob
