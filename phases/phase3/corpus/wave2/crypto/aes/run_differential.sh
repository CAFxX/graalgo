#!/bin/bash
# Thin wrapper: differential vs graalgo via the shared wave runner.
exec "$(dirname "$0")/../../run_wave.sh" "crypto/aes"
