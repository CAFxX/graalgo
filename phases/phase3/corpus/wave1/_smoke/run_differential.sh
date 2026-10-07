#!/bin/bash
# Harness smoke test — thin wrapper over the shared wave-1 runner.
# Proves the wave-1 harness works end to end (gc build, P3SyncMain,
# stdout/exit-code/panic-line comparisons).
exec "$(dirname "$0")/../run_wave.sh" _smoke
