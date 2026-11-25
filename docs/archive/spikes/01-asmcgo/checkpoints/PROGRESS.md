# Spike 01-asmcgo progress — COMPLETE 2026-10-03, verdict PASS

- [x] 2026-10-03: env, T4+SKILL read, inventory (symbols_loadbearing.txt)
- [x] formulations + difftest (3 seeds 20261003/1/777, 0 mismatches): bytealg 8 fns,
      atomic 50-op sequences, mem 4 fns, crypto AES-128/SHA-256; FIPS-197 vector exact
      (now asserted inside the difftest itself)
- [x] Java: VarHandle mapping + 6-invariant stress PASS (java) == PASS (go asm);
      math/bits 100013-value diff vs java.lang.Long EMPTY
- [x] stub-backend module (`stub/`, module graalgos01stub, built by stub_build.py):
      sync/atomic 95/95 PASS (-short, 9.2s, incl. TestNilDeref + hammers),
      bytes full suite PASS (83.9s), strings full suite PASS (34.6s).
      Two real bugs found+fixed via stub testing: (1) panicking op leaked stripe
      lock (defer unlock); (2) Loadp/StorepNoWB used lock(ptr) directly, missed by
      first fix — regression test harness/atomic/panic_test.go added.
      Contract subtlety: MakeNoZero replicates size-class rounding (runtime tables
      copied); IndexRabinKarp is generic [T string|[]byte].
- [x] Panama cgo demo: cgo vs JEP-454 downcall bit-identical checksum
      (14843116955214666767), strlen 19, upcall byte-square sum 190222 matches
- [x] exhaustive classification (inventory/classification.md): runtime/sync/reflect/net
      linux/amd64 fully classified; net has 0 .s files; rest sampled
      (crypto/internal/fips140, math/bits, internal/cpu)
- [x] REPORT.md + archive + sha256

Kill trigger: NOT fired. Two sketch-only classifications named with re-test
triggers (spike 2: gogo/mcall/morestack → vthreads; spike 4: makeFuncStub →
Truffle call targets).
