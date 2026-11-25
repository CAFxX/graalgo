#!/usr/bin/env python3
"""asm inventory extractor for spike 03-versiontax (surface a: asm/cgo).

For each load-bearing package dir in a Go source tree, lists every .s file
and the TEXT/DATA symbols defined in it, mirroring the format of spike 01's
inventory/symbols_loadbearing.txt.

Usage: asm_inventory.py <go_src_root> <out_file>
"""
import os, re, sys

PACKAGES = [
    "src/runtime",
    "src/sync",
    "src/reflect",
    "src/net",
    "src/internal/runtime/atomic",
    "src/internal/bytealg",
    "src/internal/cpu",
    "src/crypto/internal/fips140",
    "src/math/bits",
]

# TEXT ·name(SB), ...  or  DATA ·name+0x10(SB)/8, $0
TEXT_RE = re.compile(r"^TEXT\s+·?([^\(,\s]+)\(")
DATA_RE = re.compile(r"^DATA\s+·?([^\+,\s]+)\+")
# also match symbols without the · middle dot (e.g. NAME lines in some files)
GLOBL_RE = re.compile(r"^GLOBL\s+·?([^,\s]+),")

def symbols_in(path):
    syms = []
    try:
        with open(path, errors="replace") as f:
            for line in f:
                line = line.strip()
                m = TEXT_RE.match(line) or DATA_RE.match(line) or GLOBL_RE.match(line)
                if m:
                    s = m.group(1)
                    if s not in syms:
                        syms.append(s)
    except OSError:
        pass
    return syms

def main():
    root, out = sys.argv[1], sys.argv[2]
    with open(out, "w") as fh:
        for pkg in PACKAGES:
            d = os.path.join(root, pkg)
            fh.write(f"=== PKG {pkg} ===\n")
            if not os.path.isdir(d):
                fh.write("(missing)\n")
                continue
            files = sorted(f for f in os.listdir(d) if f.endswith(".s"))
            fh.write(f"({len(files)} .s files)\n")
            for fn in files:
                syms = symbols_in(os.path.join(d, fn))
                fh.write(f"  {fn:<30} {' '.join(syms)}\n")

if __name__ == "__main__":
    main()
