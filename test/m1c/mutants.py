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
