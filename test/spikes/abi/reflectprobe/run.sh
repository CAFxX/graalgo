#!/usr/bin/env bash
# Runs reflectprobe one check per process under gc and under graalgo (v1
# disk-import path, verbatim GOROOT reflect) and reports per-check agreement.
# Usage: run.sh [outdir]   (needs ./graalgo built at the repo root: make build graalgo)
set -u
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../../../.." && pwd)"
out="${1:-$here/out}"
mkdir -p "$out"
GO="${GO:-go}"
cd "$here"
"$GO" build -o "$out/probe.gc" . || exit 2
JAVA_TOOL_OPTIONS=-Xmx2g nice -n 19 "$root/graalgo" build -o "$out/probe.gg" . || exit 2
n="$("$out/probe.gc" count 2>&1)"
: > "$out/gc.txt"; : > "$out/gg.txt"; : > "$out/result.txt"
pass=0
for ((k = 0; k < n; k++)); do
  want="$("$out/probe.gc" "$k" 2>&1)"
  got="$(JAVA_TOOL_OPTIONS=-Xmx2g timeout 120 nice -n 19 "$out/probe.gg" "$k" 2>&1 | grep -v '^Picked up JAVA_TOOL_OPTIONS' | head -c 600)"
  echo "$want" >> "$out/gc.txt"
  echo "$k: $got" >> "$out/gg.txt"
  if [ "$want" == "$got" ]; then
    pass=$((pass + 1)); echo "PASS $want" >> "$out/result.txt"
  else
    echo "FAIL ${want%% *} :: $(echo "$got" | head -n 1 | head -c 220)" >> "$out/result.txt"
  fi
done
echo "reflectprobe: $pass/$n checks agree with gc" | tee -a "$out/result.txt"
