#!/bin/bash
# Differential corpus for the wave-3 `hash/crc32` package — thin wrapper
# over the shared wave-3 runner (conventions: corpus/wave3/README.md).
exec "$(dirname "$0")/../run_wave3.sh" hash/crc32
