#!/bin/bash
# Go mutant build leg: build gofrontend with the vendoring overlay alone
# (control) and with the M-G01 mutant merged in (mutant leg), exactly as
# the fixed runGoMutant does. Exits non-zero if either build fails.
# Run under nice -n 10 with GOCACHE/GOPATH/TMPDIR on /tmp (home is quota-closed).
set -u
REPO=/home/hatch/workspace/graalgo
FE=$REPO/internal/frontend
OUT=/tmp/rev1107-22-r05/go-leg
GO=/home/hatch/tools/go/bin/go
export GOCACHE=/tmp/gocache GOPATH=/tmp/rev1107-22-r05/gopath TMPDIR=/tmp/rev1107-22-r05/tmp
mkdir -p "$OUT"
cd "$FE"
df -h /tmp | tail -1
echo "--- control: vendoring overlay only ---"
# /tmp is 97% full (sibling lanes): link to /dev/null — a zero exit code
# still proves compile+link of the overlay build end to end.
if "$GO" build -overlay "$FE/overlay.json" -o /dev/null . > "$OUT/control.log" 2>&1; then
  echo "control: BUILD OK"
else
  echo "control: BUILD FAIL (see $OUT/control.log)"
  tail -5 "$OUT/control.log"
  exit 1
fi
echo "--- M-G01: merged overlay (vendoring + mutated export.go) ---"
python3 - "$FE" "$OUT" <<'EOF'
import json, sys, os
fe, out = sys.argv[1], sys.argv[2]
spec = json.load(open("/tmp/rev1107-22-r05/tools-mutation/mutants.json"))
m = next(x for x in spec["mutants"] if x["id"] == "M-G01")
src = open(os.path.join(fe, "export.go")).read()
assert m["old"] in src
mutated = src.replace(m["old"], m["new"], 1)
mut_path = os.path.join(out, "mutated-export.go")
open(mut_path, "w").write(mutated)
vend = json.load(open(os.path.join(fe, "overlay.json")))["Replace"]
vend[os.path.join(fe, "export.go")] = mut_path
json.dump({"Replace": vend}, open(os.path.join(out, "overlay-mg01.json"), "w"))
print("M-G01 applied; merged overlay entries:", len(vend))
EOF
if "$GO" build -overlay "$OUT/overlay-mg01.json" -o /dev/null . > "$OUT/mg01.log" 2>&1; then
  echo "M-G01: BUILD OK"
else
  echo "M-G01: BUILD FAIL (see $OUT/mg01.log)"
  tail -5 "$OUT/mg01.log"
  exit 1
fi
df -h /tmp | tail -1
