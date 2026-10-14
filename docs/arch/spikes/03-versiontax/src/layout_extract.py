#!/usr/bin/env python3
"""Extract struct definitions (name + field list) from Go source files.

Used for surface b: rtype/itab layout stability check across releases.
Usage: layout_extract.py <file.go> ...  (writes name{field:type,...} blocks)
"""
import re, sys

STRUCT = re.compile(r"^type\s+(\w+)\s+struct\s*\{", re.M)
COMMENT = re.compile(r"//.*")

def fields(body):
    out = []
    depth = 0
    for raw in body.splitlines():
        line = COMMENT.sub("", raw).strip()
        if not line:
            continue
        # skip nested struct literals / func bodies is overkill; just record top-level fields
        depth += line.count("{") - line.count("}")
        if depth < 0:
            break
        if depth == 0 and re.match(r"^[A-Za-z_]\w*\s", line):
            # field: Name Type `tag`
            parts = line.split(None, 2)
            if len(parts) >= 2 and not line.startswith(("func ", "case ", "if ")):
                out.append(f"{parts[0]}:{parts[1].rstrip('`').strip('`')}")
    return out

TARGETS = {"Type", "Method", "UncommonType", "Imethod", "ArrayType", "ChanType",
           "structTypeUncommon", "InterfaceType", "SliceType", "FuncType",
           "PtrType", "StructField", "StructType", "Name",
           "rtype", "bitCursor", "_typePair", "itab", "iface", "eface",
           "itabTableType", "moduledata"}

def main():
    for path in sys.argv[1:]:
        src = open(path, errors="replace").read()
        for m in STRUCT.finditer(src):
            name = m.group(1)
            if name not in TARGETS:
                continue
            # find matching close brace
            i = m.end()
            depth = 1
            while depth and i < len(src):
                if src[i] == "{":
                    depth += 1
                elif src[i] == "}":
                    depth -= 1
                i += 1
            body = src[m.end():i-1]
            print(f"== {name} ({path}) ==")
            for f in fields(body):
                print("   ", f)

if __name__ == "__main__":
    main()
