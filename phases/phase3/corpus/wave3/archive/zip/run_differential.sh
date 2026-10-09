#!/bin/bash
# Thin wrapper: run the wave3 runner for archive/zip.
exec "$(dirname "$0")/../run_wave3.sh" "archive/zip"
