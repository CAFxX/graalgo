#!/bin/bash
# Differential runner for crypto/rand corpus vs gc.
# Usage: ./run_differential.sh
set -e
DIR=$(dirname "$0")
cd "$DIR"
export PATH=$HOME/tools/go/bin:$PATH
export TMPDIR=$HOME/workspace/graalgo-m8-pipe/phases/phase3/hidden_files/m8-tmp/crypto
export GOCACHE=$HOME/workspace/.gocache-m8-crypto
pass=0; fail=0
for f in [0-9]*.go; do
  echo "=== $f ==="
  if go run "$f"; then
    pass=$((pass+1))
  else
    fail=$((fail+1))
  fi
done
echo "PASS: $pass FAIL: $fail"
[ $fail -eq 0 ]
