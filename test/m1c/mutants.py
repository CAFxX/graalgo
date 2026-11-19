#!/usr/bin/env python3
"""The mutants of test/m1c/mutants.sh: each plants one bug in the backend's
Java source; the named test/m1c programs must then fail.

  mutants.py list            -> "name<TAB>program program..." per mutant
  mutants.py apply NAME      -> plants NAME (exit 1 if its anchor does not
                                occur exactly once: the list went stale)
"""
import sys

J = "internal/backend/truffle/src/main/java/graalgo/"
SYNC = J + "go/intrinsics/SyncIntrinsics.java"
BYTEALG = J + "go/intrinsics/BytealgIntrinsics.java"
KERNELS = ["test/m1c/programs/bytealg_kernels.go"]
EXEC = J + "syscall/Executor.java"
RAW = ["test/m1c/programs/syscall_raw.go"]
RLIMIT = ["test/m1c/programs/syscall_rlimit.go"]

MUTANTS = [
    # (name, file, anchor, replacement, programs that must fail)
    ("sema-lost-wakeup", SYNC,
     "        sched.unpark(w.g);\n        if (w.granted) {",
     "        if (w.granted) {",
     ["test/m1c/programs/sync_contention.go"]),
    ("sema-no-recheck", SYNC,
     "            if (w.granted || canAcquire(node, addr)) {",
     "            if (true) {",
     ["test/m1c/programs/sync_contention.go"]),
    ("notify-wrong-ticket", SYNC,
     "                    if (w.ticket == t) {",
     "                    if (!it.hasNext()) { // the last waiter, not ticket t",
     ["test/m1c/programs/sync_cond.go"]),
    ("fatal-is-panic", SYNC,
     "    static void internal_sync_fatal(Node node, Str s) {\n        throw GoProcess.get(node).fatal(s.toString());",
     "    static void internal_sync_fatal(Node node, Str s) {\n        throw graalgo.go.runtime.Panics.nilDeref(node);",
     ["test/m1c/programs/sync_unlock_unlocked.go"]),
    ("wait-reason", SYNC,
     '        acquire(node, addr, lifo, "sync.Mutex.Lock");',
     '        acquire(node, addr, lifo, "semacquire");',
     ["test/m1c/programs/sync_deadlock_mutex.go"]),
    ("compare-signed", BYTEALG,
     "Integer.signum(Arrays.compareUnsigned(",
     "Integer.signum(Arrays.compare(",
     KERNELS),
    ("compare-prefix-equal", BYTEALG,
     "        Slice x = (Slice) a, y = (Slice) b;\n        return Integer.signum(",
     "        Slice x = (Slice) a, y = (Slice) b;\n        if (x.len == 0 || y.len == 0) {\n            return 0;\n        }\n        return Integer.signum(",
     KERNELS),
    ("index-misses-last-position", BYTEALG,
     "for (int i = 0, last = n - m; i <= last; i++) {",
     "for (int i = 0, last = n - m; i < last; i++) {",
     KERNELS),
    ("indexbytestring-ignores-offset", BYTEALG,
     "return indexByte(s.b, s.off, s.len, (byte) c);",
     "return indexByte(s.b, 0, s.len, (byte) c);",
     KERNELS),
    ("makenozero-exact-cap", BYTEALG,
     "int cap = n == 0 ? 0 : graalgo.runtime.Intrinsics.nextSliceCap((int) n, 0, 1, false);",
     "int cap = (int) n;",
     KERNELS),
    # (Copying back an out buffer's unfilled tail is an equivalent mutant: the
    # executor copies out buffers in first, so the tail reads back unchanged.)
    ("exec-skips-copy-back", EXEC,
     "                    Unsafe.writeBytes(node, objs[i], offs[i], data[i], filled);",
     "",
     RAW),
    ("exec-read-fills-from-start-of-object", EXEC,
     "                    Unsafe.writeBytes(node, objs[i], offs[i], data[i], filled);",
     "                    Unsafe.writeBytes(node, objs[i], 0, data[i], filled);",
     RAW),
    ("exec-ignores-pointer-offset", EXEC,
     "                long off = Unsafe.offset(p);",
     "                long off = 0;",
     RAW),
    ("exec-struct-not-copied-back", EXEC,
     "if (segs[i] == null || r.kind == Role.K_VAL || r.kind == Role.K_CSTR || !r.out) {",
     "if (segs[i] == null || r.kind != Role.K_BYTES || !r.out) {",
     RLIMIT),
    ("exec-struct-half-size", EXEC,
     "long n = r.kind == Role.K_STRUCT ? r.size : a[r.lenArg];",
     "long n = r.kind == Role.K_STRUCT ? r.size / 2 : a[r.lenArg];",
     RLIMIT),
    ("exec-loses-errno", EXEC,
     "                return new Raw(-1, (int) ERRNO.get(state, 0L));",
     "                return new Raw(-1, 0);",
     RAW),
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
