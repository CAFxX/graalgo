#!/bin/bash
# Thin wrapper: run the wave3 runner for archive/tar.
exec "$(dirname "$0")/../run_wave3.sh" "archive/tar"
