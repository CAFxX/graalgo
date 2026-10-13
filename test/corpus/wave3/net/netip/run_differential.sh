#!/bin/bash
# Thin wrapper: run the wave3 runner for net/netip.
exec "$(dirname "$0")/../../run_wave3.sh" "net/netip"
