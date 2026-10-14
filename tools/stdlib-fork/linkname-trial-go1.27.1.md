# linkname-diff report — 2026-10-07T00:38:57Z

Tree: `/home/hatch/workspace/graalgo-stdlib/go1.27.1` — target: `linux/amd64` — graalgo: `/home/hatch/workspace/graalgo`

## Headline

| metric | value |
|---|---|
| linkname directives | 615 |
| SATISFIED | 8 |
| ROUTED | 1 |
| MISSING | 606 |
| **SKIP_LIST_SIZE** | **525** |
| files scanned | 3289 |
| files with linknames | 175 |
| _test.go files excluded | 1799 |
| constraint-excluded files | 1641 |
| provided symbols | 241 |

> SPEC §3.6 kill trigger: skip-list growth > 15 twice per release.
> Compare SKIP_LIST_SIZE against the previous release's snapshot.

## Routes applied

- `runtime.writeBarrier` → `const:false`: JVM owns all barriers (P2 ADR-0002 D3); no mutator barrier exists, writeBarrier.enabled ≡ false

## Skip-list (distinct MISSING targets)

| target | refs | reason |
|---|---|---|
| `arena.runtime_arena_arena_Free` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `arena.runtime_arena_arena_New` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `arena.runtime_arena_arena_Slice` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `arena.runtime_arena_heapify` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `arena.runtime_arena_newArena` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `atomic.storePointer` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.addMulVVW` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.addVV` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.addVW` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.mulAddVWW` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.shlVU` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.subVV` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.subVW` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `build.defaultReleaseTags` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `build.defaultToolTags` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `build.parseFileHeader` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `build.readComments` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `buildinfo.errNotGoExe` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cfile.getCovCounterList` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_bindm` | 1 | local name "_cgo_bindm" does not match declared identifier "x_cgo_bindm" (gc rejects this) |
| `cgo._cgo_call_symbolizer_function` | 1 | local name "_cgo_call_symbolizer_function" does not match declared identifier "x_cgo_call_symbolizer_function" (gc rejects this) |
| `cgo._cgo_call_traceback_function` | 1 | local name "_cgo_call_traceback_function" does not match declared identifier "x_cgo_call_traceback_function" (gc rejects this) |
| `cgo._cgo_callers` | 1 | local name "_cgo_callers" does not match declared identifier "x_cgo_callers" (gc rejects this) |
| `cgo._cgo_getstackbound` | 1 | local name "_cgo_getstackbound" does not match declared identifier "x_cgo_getstackbound" (gc rejects this) |
| `cgo._cgo_init` | 1 | local name "_cgo_init" does not match declared identifier "x_cgo_init" (gc rejects this) |
| `cgo._cgo_libc_setegid` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_seteuid` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setgid` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setgroups` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setregid` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setresgid` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setresuid` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setreuid` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setuid` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_mmap` | 1 | local name "_cgo_mmap" does not match declared identifier "x_cgo_mmap" (gc rejects this) |
| `cgo._cgo_munmap` | 1 | local name "_cgo_munmap" does not match declared identifier "x_cgo_munmap" (gc rejects this) |
| `cgo._cgo_notify_runtime_init_done` | 1 | local name "_cgo_notify_runtime_init_done" does not match declared identifier "x_cgo_notify_runtime_init_done" (gc rejects this) |
| `cgo._cgo_panic` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_pthread_key_created` | 1 | local name "_cgo_pthread_key_created" does not match declared identifier "x_cgo_pthread_key_created" (gc rejects this) |
| `cgo._cgo_set_traceback_functions` | 1 | local name "_cgo_set_traceback_functions" does not match declared identifier "x_cgo_set_traceback_functions" (gc rejects this) |
| `cgo._cgo_sigaction` | 1 | local name "_cgo_sigaction" does not match declared identifier "x_cgo_sigaction" (gc rejects this) |
| `cgo._cgo_sys_thread_create` | 1 | local name "_cgo_sys_thread_create" does not match declared identifier "x_cgo_sys_thread_create" (gc rejects this) |
| `cgo._cgo_thread_start` | 1 | local name "_cgo_thread_start" does not match declared identifier "x_cgo_thread_start" (gc rejects this) |
| `cgo._cgo_yield` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._crosscall2_ptr` | 1 | local name "_crosscall2_ptr" does not match declared identifier "x_crosscall2_ptr" (gc rejects this) |
| `cgo.x_cgo_bindm` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_call_symbolizer_function` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_call_traceback_function` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_callers` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_clearenv` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_getstackbound` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_init` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_mmap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_munmap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_notify_runtime_init_done` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_pthread_key_created` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_set_traceback_functions` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_setenv` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_sigaction` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_sys_thread_create` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_thread_start` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_unsetenv` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_crosscall2_ptr` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgroup.throw` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/fips140.isBypassed` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/fips140.setBypass` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/fips140.unsetBypass` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/boring.runtime_arg0` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/boring/bcache.registerCache` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140.fatal` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140.getIndicator` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140.setIndicator` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140/check/checktest.RODATA` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140deps/time.monoTime` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140hash.sha3Unwrap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/rand.SetTestingReader` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/sysrand.fatal` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/rand.fatal` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/subtle.setDITDisabled` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/subtle.setDITEnabled` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `fips140.isBypassed` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `fips140.setBypass` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `fips140.unsetBypass` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `fips140hash.sha3Unwrap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `go/types.(*Checker).infer` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `go/types.srcimporter_setUsesCgo` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `go:fipsinfo` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `godebug.registerMetric` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `godebug.setNewIncNonDefault` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `godebug.setUpdate` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `golang.org/x/net/internal/http3_test.protocolSetHTTP3` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `golang.org/x/tools/go/types/typeutil.interfaceMethod` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `golang.org/x/tools/go/types/typeutil.usedIdent` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `http.cloneMultipartFileHeader` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `http.cloneMultipartForm` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `http.cloneOrMakeHeader` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `http.cloneTLSConfig` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `http.cloneURL` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `http.cloneURLValues` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `http.parseBasicAuth` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `http.readRequest` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/bytealg.MakeNoZero` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/coverage/cfile.getCovCounterList` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/godebug.registerMetric` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/godebug.setNewIncNonDefault` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/godebug.setUpdate` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_Semacquire` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_Semrelease` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_pollClose` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_pollOpen` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_pollReset` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_pollServerInit` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_pollSetDeadline` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_pollUnblock` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_pollWait` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_pollWaitCanceled` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/reflectlite.chanlen` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/reflectlite.ifaceE2I` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/reflectlite.maplen` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/reflectlite.resolveNameOff` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/reflectlite.resolveTypeOff` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/reflectlite.typedmemmove` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/reflectlite.unsafe_New` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/atomic.casPointer` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/atomic.storePointer` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/cgroup.throw` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.errNilAssign` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.fatal` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.newarray` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.newobject` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.rand` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.typeString` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.typedmemclr` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.typedmemmove` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/sync.fatal` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/sync.runtime_SemacquireMutex` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/sync.runtime_Semrelease` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/sync.runtime_canSpin` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/sync.runtime_doSpin` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/sync.runtime_nanotime` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/sync.throw` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/synctest.IsInBubble` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/synctest.Run` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/synctest.Wait` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/synctest.acquire` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/synctest.associate` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/synctest.disassociate` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/synctest.inBubble` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/synctest.isAssociated` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/synctest.release` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/syscall/unix.gostring` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `main.main` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.clone` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.errNilAssign` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.fatal` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.newarray` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.newobject` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.rand` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.typeString` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.typedmemclr` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.typedmemmove` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.defaultNS` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.favoriteAddrFamily` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.ipToSockaddr` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.isDomainName` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.listenerBacklog` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.loopbackIP` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.newUnixFile` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/http.(*Transport).RoundTrip` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/http.serverHandler.ServeHTTP` | 1 | local name "badServeHTTP" does not match declared identifier "ServeHTTP" (gc rejects this) |
| `net/http/internal/http2_test.transportFromH1Transport` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/http_test.registerHTTP3Server` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/http_test.registerHTTP3Transport` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/textproto.readMIMEHeader` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/url.(*URL).setPath` | 1 | local name "badSetPath" does not match declared identifier "setPath" (gc rejects this) |
| `os.checkClonePidfd` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.ignoreSIGSYS` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.restoreSIGSYS` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.runtime_args` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.runtime_beforeExit` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.sigpipe` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signalWaitUntilIdle` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_disable` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_enable` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_ignore` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_ignored` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_recv` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `plugin.lastmoduleinit` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `poll.IsPollDescriptor` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `rand.fatal` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*Value).pointer` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Align` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).AssignableTo` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Bits` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).ChanDir` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Comparable` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).ConvertibleTo` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Elem` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Field` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).FieldAlign` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).FieldByIndex` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).FieldByName` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).FieldByNameFunc` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Implements` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).In` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).IsVariadic` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Key` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Kind` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Len` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Method` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).MethodByName` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Name` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).NumField` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).NumIn` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).NumMethod` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).NumOut` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Out` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).PkgPath` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Size` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).String` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).ptrTo` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.add` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.addReflectOff` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.chancap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.chanclose` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.chanlen` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.chanrecv` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.chansend0` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.gcbits` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.growslice` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.ifaceE2I` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.ifaceIndir` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.makechan` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.makemap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapaccess` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapaccess_faststr` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapassign` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapassign0` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapassign_faststr0` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapclear` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapdelete` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapdelete_faststr` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapiterelem` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapiterinit` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapiterkey` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapiternext` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.maplen` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.memclrNoHeapPointers` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.memmove` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.resolveNameOff` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.resolveTextOff` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.resolveTypeOff` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.rselect` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.rtypeOff` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.toType` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typedarrayclear` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typedmemclr` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typedmemclrpartial` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typedmemmove` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typedslicecopy` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typehash` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typelinks` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typesByString` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.unsafe_New` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.unsafe_NewArray` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.unsafeslice` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.verifyNotInHeapPtr` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.(*inlineUnwinder).srcFunc` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_clearenv` | 1 | local name "_cgo_clearenv" does not match declared identifier "x_cgo_clearenv" (gc rejects this) |
| `runtime._cgo_mmap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_munmap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_panic_internal` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_setenv` | 2 | local name "_cgo_setenv" does not match declared identifier "x_cgo_setenv" (gc rejects this) |
| `runtime._cgo_sigaction` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_unsetenv` | 2 | local name "_cgo_unsetenv" does not match declared identifier "x_cgo_unsetenv" (gc rejects this) |
| `runtime.abort` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.add` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.atomicwb` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.callers` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cgocall` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cgocallback` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.chanbuf` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cheaprand` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cheaprand64` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cheaprandn` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.checkptrBase` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cmpstring` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.convT64` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.convTslice` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.convTstring` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.divideError` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.doInit` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.entersyscall` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.entersyscallblock` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.exitsyscall` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.fastrand64` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.fcntl` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.findObject` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.findfunc` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.funcInfo.entry` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.funcline1` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.gcWriteBarrier2` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.getAuxv` | 3 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.getStaticuint64s` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.getitab` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.getm` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.goarm` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.gopanic` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.gopark` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.goready` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.gostring` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.goyield` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.growslice` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.heapObjectsCanMove` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.iscgo` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.lastmoduledatap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.lockedOSThread` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.makemap` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.makemap_small` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.makeslice` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mallocgc` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess1` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess1_fast32` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess1_fast64` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess1_faststr` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess2` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess2_fast32` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess2_fast64` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess2_faststr` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_fast32` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_fast32ptr` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_fast64` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_fast64ptr` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_faststr` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapdelete` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapdelete_fast32` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapdelete_fast64` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapdelete_faststr` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapiterinit` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapiternext` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mayMoreStackMove` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mayMoreStackPreempt` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memclrHasPointers` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memclrNoHeapPointers` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memequal` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memequal_varlen` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memhash` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memmove` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.moduledataverify1` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.morestack_noctxt` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.morestackc` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.newInlineUnwinder` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.newarray` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.nilinterhash` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.noescape` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.overflowError` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.overrideWrite` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_blockProfileInternal` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_fpunwindExpand` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_goroutineLeakProfileWithLabels` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_goroutineProfileWithLabels` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_makeProfStack` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_memProfileInternal` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_mutexProfileInternal` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_threadCreateInternal` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.procPin` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.procUnpin` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.procyield` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.rand` | 8 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.randn` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.reflectcall` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.setCrashFD` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.set_crosscall2` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.setsigsegv` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.sigpanic` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.srcFunc.name` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.strhash` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.stringHash` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.throw` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.traceAdvance` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.traceClockNow` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.typedmemmove` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.typedslicecopy` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.typehash` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.vgetrandom` | 3 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.wakep` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.write` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.zeroVal` | 3 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.SetTraceback` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.WriteHeapDump` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.freeOSMemory` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.modinfo` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.readGCStats` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setGCPercent` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setMaxStack` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setMaxThreads` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setMemoryLimit` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setPanicOnFault` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/metrics.runtime_readMetrics` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/metrics_test.runtime_readMetricNames` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.readProfile` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_FrameStartLine` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_FrameSymbolName` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_cyclesPerSecond` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_expandFinalInlineFrame` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_getProfLabel` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_goroutineLeakGC` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_goroutineleakcount` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_setProfLabel` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.appendSignalStacks` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.count` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.dec` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.eraseSecrets` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.getStack` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.inc` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.runtime_traceClockUnitsPerSecond` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.userLog` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.userRegion` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.userTaskCreate` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.userTaskEnd` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sql.convertAssign` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sql.drivers` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `subtle.setDITDisabled` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `subtle.setDITEnabled` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.fatal` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.poolCleanup` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_Semacquire` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_SemacquireMutex` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_SemacquireRWMutex` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_SemacquireRWMutexR` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_SemacquireWaitGroup` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_Semrelease` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_canSpin` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_doSpin` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_nanotime` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_notifyListAdd` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_notifyListCheck` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_notifyListNotifyAll` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_notifyListNotifyOne` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_notifyListWait` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_procPin` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_procUnpin` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_registerPoolCleanup` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.throw` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.CompareAndSwapPointer` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.StorePointer` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.SwapPointer` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.runtime_procPin` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.runtime_procUnpin` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync_test.runtime_blockUntilEmptyCleanupQueue` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `synctest.IsInBubble` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `synctest.Run` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `synctest.Wait` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `synctest.acquire` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `synctest.associate` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `synctest.disassociate` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `synctest.inBubble` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `synctest.isAssociated` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `synctest.release` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.Exit` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.Getpagesize` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.RawSyscall` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.RawSyscall6` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.Syscall` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.Syscall6` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_setegid` | 1 | local name "cgo_libc_setegid" does not match declared identifier "_cgo_libc_setegid" (gc rejects this) |
| `syscall.cgo_libc_seteuid` | 1 | local name "cgo_libc_seteuid" does not match declared identifier "_cgo_libc_seteuid" (gc rejects this) |
| `syscall.cgo_libc_setgid` | 1 | local name "cgo_libc_setgid" does not match declared identifier "_cgo_libc_setgid" (gc rejects this) |
| `syscall.cgo_libc_setgroups` | 1 | local name "cgo_libc_setgroups" does not match declared identifier "_cgo_libc_setgroups" (gc rejects this) |
| `syscall.cgo_libc_setregid` | 1 | local name "cgo_libc_setregid" does not match declared identifier "_cgo_libc_setregid" (gc rejects this) |
| `syscall.cgo_libc_setresgid` | 1 | local name "cgo_libc_setresgid" does not match declared identifier "_cgo_libc_setresgid" (gc rejects this) |
| `syscall.cgo_libc_setresuid` | 1 | local name "cgo_libc_setresuid" does not match declared identifier "_cgo_libc_setresuid" (gc rejects this) |
| `syscall.cgo_libc_setreuid` | 1 | local name "cgo_libc_setreuid" does not match declared identifier "_cgo_libc_setreuid" (gc rejects this) |
| `syscall.cgo_libc_setuid` | 1 | local name "cgo_libc_setuid" does not match declared identifier "_cgo_libc_setuid" (gc rejects this) |
| `syscall.cgocaller` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.gettimeofday` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.hasWaitingReaders` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.prlimit` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.recvfromInet4` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.recvfromInet6` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.recvmsgInet4` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.recvmsgInet6` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtimeClearenv` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtimeSetenv` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtimeUnsetenv` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_AfterExec` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_AfterFork` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_AfterForkInChild` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_BeforeExec` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_BeforeFork` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_doAllThreadsSyscall` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_envs` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.sendmsgNInet4` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.sendmsgNInet6` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.sendtoInet4` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.sendtoInet6` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.utimensat` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sysrand.fatal` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `testing.checkParallel` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `testing/synctest.testingSynctestTest` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `testlog.SetPanicOnExit0` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.Time.abs` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.absClock` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.absDate` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.monoTime` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.newTimer` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.nextStdChunk` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.now` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.registerLoadFromEmbeddedTZData` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.resetTimer` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.runtimeIsBubbled` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.runtimeNano` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.runtimeNow` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.stopTimer` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.aeadAESGCMTLS13` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.cipherSuitesTLS13` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.defaultCipherSuitesTLS13` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.defaultCipherSuitesTLS13NoAES` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.errNoCertificates` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `trace.runtime_traceClockUnitsPerSecond` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `unique.runtime_blockUntilEmptyCleanupQueue` | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `weak.runtime_makeStrongFromWeak` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `weak.runtime_registerWeakPointer` | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `x509.systemRoots` | 1 | local name "systemRoots" does not match declared identifier "once" (gc rejects this) |

## Duplicate local names

- `rand.runtime_rand`: math/rand/rand.go, math/rand/v2/rand.go
- `unix.vgetrandom`: cmd/vendor/golang.org/x/sys/unix/vgetrandom_linux.go, internal/syscall/unix/getrandom.go

## Provided symbols by source

- p3link LinknameIntrinsics.java Linknames.register: 4
- synthetic: 58
- synthetic "context": 18
- synthetic "fmt": 7
- synthetic "runtime": 4
- synthetic "sync": 22
- synthetic "sync/atomic": 74
- synthetic "time": 54
