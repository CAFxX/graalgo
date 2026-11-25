#!/usr/bin/env python3
"""Diff stock vs wrapped `go build -x` traces to find tool invocations the
-toolexec wrapper never sees (argv-interception gaps).

Usage: analyze_trace.py trace-stock.txt trace-wrapped.txt wrapper-path
"""
import re
import sys
from collections import Counter

TOOLS = ["compile", "asm", "cgo", "link", "cover", "vet", "preprofile", "pack"]


def extract_commands(path, wrapper):
    cmds = []
    with open(path, errors="replace") as f:
        for line in f:
            line = line.rstrip("\n")
            if not line.strip():
                continue
            # In -x output, tool invocations appear as command lines. Strip
            # the wrapper prefix for the wrapped trace.
            if wrapper and wrapper in line:
                line = line.replace(wrapper + " ", "")
            # Normalize temp work dirs and absolute GOROOT/tool paths.
            line = re.sub(r"/tmp/go-build\d+", "$WORK", line)
            line = re.sub(r"\S+/pkg/tool/\S+?/(\w+)", r"TOOL/\1", line)
            line = re.sub(r"\s+\S*?/go-build\S*", " $WORK", line)
            cmds.append(canonical(line))
    return cmds


def canonical(line):
    # Classify: which tool (or gcc) does this command line invoke?
    m = re.search(r"TOOL/(\w+)", line)
    if m:
        tool = m.group(1)
        rest = line.split("TOOL/" + tool, 1)[1]
    else:
        m = re.search(r"(?:^|\s)(\S*(?:gcc|cc|clang)\S*)(?:\s|$)", line)
        if m:
            tool = "HOSTCC:" + m.group(1).split("/")[-1]
            rest = line.split(m.group(1), 1)[1]
        else:
            return None  # not a tool invocation (mkdir, cd, etc.)
    # Drop volatile bits: -o $WORK paths already normalized; keep flags.
    rest = re.sub(r"\$WORK/b\d+", "$WORK/bN", rest)
    return tool + " :: " + " ".join(rest.split())


def main():
    stock_path, wrapped_path, wrapper = sys.argv[1], sys.argv[2], sys.argv[3]
    stock = Counter(c for c in extract_commands(stock_path, None) if c)
    wrapped = Counter(c for c in extract_commands(wrapped_path, wrapper) if c)

    print(f"stock tool invocations: {sum(stock.values())} "
          f"({len(stock)} distinct)")
    print("wrapped tool invocations (wrapper stripped): "
          f"{sum(wrapped.values())} ({len(wrapped)} distinct)")
    print()
    print("== by tool ==")
    for tool in sorted(set(k.split(" ::")[0] for k in set(stock) | set(wrapped))):
        s = sum(v for k, v in stock.items() if k.startswith(tool + " ::"))
        w = sum(v for k, v in wrapped.items() if k.startswith(tool + " ::"))
        print(f"  {tool:20s} stock={s:5d} wrapped={w:5d}")
    print()
    missing = stock - wrapped
    if missing:
        print("== in STOCK trace but NOT in wrapped trace (candidate gaps) ==")
        for k, v in missing.most_common(20):
            print(f"  x{v}: {k[:160]}")
    else:
        print("== no candidate gaps: every stock tool invocation has a "
              "wrapper-stripped counterpart ==")
    extra = wrapped - stock
    if extra:
        print()
        print("== in wrapped trace but not stock (informational) ==")
        for k, v in extra.most_common(10):
            print(f"  x{v}: {k[:160]}")


if __name__ == "__main__":
    main()
