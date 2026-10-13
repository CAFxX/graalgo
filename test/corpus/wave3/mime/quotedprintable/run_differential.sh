#!/bin/bash
# Thin wrapper: run the wave3 runner for mime/quotedprintable.
exec "$(dirname "$0")/../../run_wave3.sh" "mime/quotedprintable"
