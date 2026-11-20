#!/usr/bin/env python3
"""The mutants of test/m1d/mutants.sh: each plants one bug in the backend's
Java source; the named test/m1d programs must then fail.

  mutants.py list            -> "name<TAB>program program..." per mutant
  mutants.py apply NAME      -> plants NAME (exit 1 if its anchor does not
                                occur exactly once: the list went stale)
"""
import sys

J = "internal/backend/truffle/src/main/java/graalgo/"
UNSAFE = J + "go/runtime/Unsafe.java"
REFLECT = J + "go/intrinsics/ReflectIntrinsics.java"
OS = J + "go/intrinsics/OsIntrinsics.java"
POLLER = J + "syscall/Poller.java"
VERBS = ["test/m1d/programs/fmt_verbs.go"]
PROBE = ["test/m1d/programs/reflect_probe.go"]
FILES = ["test/m1d/programs/os_files.go"]

MUTANTS = [
    # (name, file, anchor, replacement, programs that must fail)
    ("iface-direct-struct-as-pointer", UNSAFE,
     "            return directFlat(i.type) ? ((Flat) i.data).refs[0] : i.data;",
     "            return i.data;",
     VERBS),
    ("iface-header-words-raw", UNSAFE,
     "                return new Iface(t, loadRef(n, obj, off, t));",
     "                return new RawIface(typ, data);",
     PROBE),
    ("map-iter-elem-is-key", REFLECT,
     "        return new Object[] {it, key, elem};",
     "        return new Object[] {it, key, key};",
     VERBS),
    ("call-args-reversed", REFLECT,
     "            out[lead + i] = pt.gcSize() == 0",
     "            out[lead + a.len - 1 - i] = pt.gcSize() == 0",
     PROBE),
    ("slicecopy-copies-nothing", REFLECT,
     "        int n = Math.min(d.len, s.len);",
     "        int n = 0;",
     PROBE),
    ("os-args-all-argv0", OS,
     "new Str(argv[i].getBytes(StandardCharsets.UTF_8))",
     "new Str(argv[0].getBytes(StandardCharsets.UTF_8))",
     FILES),
    ("poller-lost-read-wakeup", POLLER,
     "                            d.ready[R] = true;\n                            wake(d, R);",
     "                            d.ready[R] = true;",
     FILES),
]


def main():
    if sys.argv[1:2] == ["list"]:
        for name, _, _, _, progs in MUTANTS:
            print(name + "\t" + " ".join(progs))
        return 0
    if len(sys.argv) == 3 and sys.argv[1] == "apply":
        for name, path, anchor, repl, _ in MUTANTS:
            if name == sys.argv[2]:
                s = open(path).read()
                if s.count(anchor) != 1:
                    print(f"mutant {name}: anchor occurs {s.count(anchor)} times in {path}", file=sys.stderr)
                    return 1
                open(path, "w").write(s.replace(anchor, repl))
                print(path)
                return 0
        print("no such mutant: " + sys.argv[2], file=sys.stderr)
        return 2
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
