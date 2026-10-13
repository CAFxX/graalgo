#!/bin/bash
# Thin wrapper for the reflect wave-2 corpus (see ../README.md).
# Delegates to the shared differential runner.
set -uo pipefail
cd "$(dirname "$0")"
exec ../run_wave.sh reflect
