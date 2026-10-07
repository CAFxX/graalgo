#!/usr/bin/env python3
"""Final step: rename single-result methods to exported form (required for
cross-package type assertion; Go forbids asserting unexported methods
cross-package)."""
import os, re
STAGE2 = os.path.expanduser(
    "~/workspace/graalgo-m8-pipe/phases/phase3/hidden_files/m8-tmp/crypto/stage2")
renames = [
    ("writeSingle", "WriteSingle"),
    ("marshalSingle", "MarshalSingle"),
    ("appendSingle", "AppendSingle"),
    ("cloneSinglePair", "CloneSinglePair"),
    ("readSingle", "ReadSingle"),
]
for fn in os.listdir(STAGE2):
    if not fn.endswith(".go"):
        continue
    p = os.path.join(STAGE2, fn)
    t = open(p).read()
    for old, new in renames:
        t = re.sub(r'\b%s\b' % old, new, t)
    open(p, "w").write(t)
print("export rename done")
