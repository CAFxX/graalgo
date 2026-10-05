# P2-M4 design spike — GC interop (archived 2026-10-05)

Throwaway spike cited by `phases/phase2/adr/ADR-0002-gc-interop.md`.
Copied here from /tmp/gcspike (ephemeral) so the phase-gate evidence is durable.

## What it demonstrates (7/7 checks, all PASS at archive time)

1. Parked-vthread stack roots via the real `SchedulerCore` + `GoChan` park path:
   - ~20MB object-graph root survives forced GCs while the goroutine is parked
     on an unbuffered channel recv → roots enumerated from the parked stack.
   - A `WeakReference` to a deep leaf also survives → transitive closure traced
     exactly, no holes.
   - After the goroutine exits, both weak refs clear → scheduler/channel/timer
     structures do not over-retain dead goroutine stacks.
2. `runtime.SetFinalizer` vehicle (`java.lang.ref.Cleaner`): runs once after
   unreachability; never while reachable.

## Re-run

```
G=~/tools/graalvm-jdk-25
CP=~/workspace/graalgo/phases/phase2/out/classes:~/workspace/graalgo/phases/phase1/out/classes:$(ls ~/workspace/graalgo/phases/phase1/lib/*.jar | tr '\n' ':')
$G/bin/javac -cp "$CP" Spike.java
$G/bin/java --enable-native-access=ALL-UNNAMED -Xss32m -cp "$CP:." Spike
```

## Barrier audit (static, re-runnable)

- `grep -rn allocateMemory phases/phase1/src/main phases/phase2/src/main` → 0 hits
- `grep -rln 'sun.misc.Unsafe\|jdk.internal\|MemorySegment\|Arena'` → 0 hits in sources
- `unsafe.Pointer` = `UnsafeHandles` (`HashMap<Long, Object>`, JVM heap)
