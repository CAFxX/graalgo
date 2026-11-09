# linkname-diff report — 2026-11-09T04:36:14Z

Tree: `/home/hatch/workspace/graalgo-stdlib/go1.27.1` — target: `linux/amd64` — graalgo: `/home/hatch/workspace/worktrees/graalgo/review-22-linkdiff-111`

## Headline

| metric | value |
|---|---|
| linkname directives | 754 |
| SATISFIED | 295 |
| ROUTED | 1 |
| MISSING | 458 |
| **SKIP_LIST_SIZE** | **393** |
| **LIVE_SKIP_LIST_SIZE** | **155** |
| files scanned | 3289 |
| files with linknames | 175 |
| _test.go files excluded | 1799 |
| constraint-excluded files | 1641 |
| synthetic sources scanned | 130 |
| synthetic linkname directives | 139 |
| provided symbols | 5729 |

> SPEC §3.6 kill trigger: skip-list growth > 15 twice per release.
> Compare SKIP_LIST_SIZE against the previous release's snapshot.

## Routes applied

- `runtime.writeBarrier` → `const:false`: JVM owns all barriers (P2 ADR-0002 D3); no mutator barrier exists, writeBarrier.enabled ≡ false

## Skip-list (distinct MISSING targets)

`live refs` counts references from code graalgo actually compiles (non-shadowed stdlib files + synthetic sources); a target with 0 live refs is referenced only by real-stdlib files superseded by a synthetic package.

| target | refs | live refs | reason |
|---|---|---|---|
| `arena.runtime_arena_arena_Free` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `arena.runtime_arena_arena_New` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `arena.runtime_arena_arena_Slice` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `arena.runtime_arena_heapify` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `arena.runtime_arena_newArena` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.addMulVVW` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.addVV` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.addVW` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.mulAddVWW` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.shlVU` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.subVV` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `big.subVW` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_bindm` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_call_symbolizer_function` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_call_traceback_function` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_callers` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_getstackbound` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_init` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setegid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_seteuid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setgid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setgroups` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setregid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setresgid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setresuid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setreuid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_libc_setuid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_mmap` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_munmap` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_notify_runtime_init_done` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_panic` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_pthread_key_created` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_set_traceback_functions` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_sigaction` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_sys_thread_create` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_thread_start` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._cgo_yield` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo._crosscall2_ptr` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_bindm` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_call_symbolizer_function` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_call_traceback_function` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_callers` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_clearenv` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_getstackbound` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_init` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_mmap` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_munmap` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_notify_runtime_init_done` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_pthread_key_created` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_set_traceback_functions` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_setenv` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_sigaction` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_sys_thread_create` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_thread_start` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_cgo_unsetenv` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `cgo.x_crosscall2_ptr` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/fips140.isBypassed` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/fips140.setBypass` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/fips140.unsetBypass` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/boring.runtime_arg0` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/boring/bcache.registerCache` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140.fatal` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140.getIndicator` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140.setIndicator` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140/check/checktest.RODATA` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140deps/time.monoTime` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/fips140hash.sha3Unwrap` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/rand.SetTestingReader` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/internal/sysrand.fatal` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/rand.fatal` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/subtle.setDITDisabled` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `crypto/subtle.setDITEnabled` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `fips140hash.sha3Unwrap` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `go/types.(*Checker).infer` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `go/types.srcimporter_setUsesCgo` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `go:fipsinfo` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `golang.org/x/net/internal/http3_test.protocolSetHTTP3` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `golang.org/x/tools/go/types/typeutil.interfaceMethod` | 2 | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `golang.org/x/tools/go/types/typeutil.usedIdent` | 2 | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/coverage/cfile.getCovCounterList` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_Semacquire` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/poll.runtime_Semrelease` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/atomic.casPointer` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/atomic.storePointer` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/cgroup.throw` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.errNilAssign` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.fatal` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.newarray` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.newobject` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.rand` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.typeString` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.typedmemclr` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/runtime/maps.typedmemmove` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `internal/syscall/unix.gostring` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `main.main` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `maps.clone` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.defaultNS` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.favoriteAddrFamily` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.ipToSockaddr` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.isDomainName` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.listenerBacklog` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.loopbackIP` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net.newUnixFile` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/http.(*Transport).RoundTrip` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/http.serverHandler.ServeHTTP` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/http/internal/http2_test.transportFromH1Transport` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/http_test.registerHTTP3Server` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/http_test.registerHTTP3Transport` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/textproto.readMIMEHeader` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `net/url.(*URL).setPath` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.checkClonePidfd` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.ignoreSIGSYS` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.restoreSIGSYS` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.runtime_args` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.runtime_beforeExit` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os.sigpipe` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signalWaitUntilIdle` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_disable` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_enable` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_ignore` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_ignored` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `os/signal.signal_recv` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `plugin.lastmoduleinit` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `poll.IsPollDescriptor` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `rand.fatal` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*Value).pointer` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Align` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).AssignableTo` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Bits` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).ChanDir` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Comparable` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).ConvertibleTo` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Elem` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Field` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).FieldAlign` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).FieldByIndex` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).FieldByName` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).FieldByNameFunc` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Implements` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).In` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).IsVariadic` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Key` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Kind` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Len` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Method` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).MethodByName` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Name` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).NumField` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).NumIn` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).NumMethod` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).NumOut` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Out` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).PkgPath` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).Size` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).String` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.(*rtype).ptrTo` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.chanrecv` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.gcbits` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.ifaceIndir` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapaccess` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapaccess_faststr` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapassign0` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapassign_faststr0` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapdelete` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapdelete_faststr` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapiterelem` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapiterinit` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapiterkey` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.mapiternext` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.memclrNoHeapPointers` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.rselect` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typedmemclrpartial` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typehash` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.typelinks` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.unsafeslice` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `reflect.verifyNotInHeapPtr` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.(*inlineUnwinder).srcFunc` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_clearenv` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_mmap` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_munmap` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_panic_internal` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_setenv` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_sigaction` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime._cgo_unsetenv` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.abort` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.add` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.atomicwb` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.callers` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cgocall` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cgocallback` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.chanbuf` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cheaprand` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cheaprand64` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cheaprandn` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.checkptrBase` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.cmpstring` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.convT64` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.convTslice` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.convTstring` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.divideError` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.doInit` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.entersyscall` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.entersyscallblock` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.exitsyscall` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.fastrand64` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.fcntl` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.findObject` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.findfunc` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.funcInfo.entry` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.funcline1` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.gcWriteBarrier2` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.getAuxv` | 3 | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.getitab` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.getm` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.goarm` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.gopanic` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.gopark` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.goready` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.gostring` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.goyield` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.growslice` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.heapObjectsCanMove` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.iscgo` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.lastmoduledatap` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.lockedOSThread` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.makemap` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.makemap_small` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.makeslice` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mallocgc` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess1` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess1_fast32` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess1_fast64` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess1_faststr` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess2` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess2_fast32` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess2_fast64` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapaccess2_faststr` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_fast32` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_fast32ptr` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_fast64` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_fast64ptr` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapassign_faststr` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapdelete` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapdelete_fast32` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapdelete_fast64` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapdelete_faststr` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapiterinit` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mapiternext` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mayMoreStackMove` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.mayMoreStackPreempt` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memclrHasPointers` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memclrNoHeapPointers` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memequal` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memequal_varlen` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memhash` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.memmove` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.moduledataverify1` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.morestack_noctxt` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.morestackc` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.newInlineUnwinder` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.newarray` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.nilinterhash` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.noescape` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.overflowError` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.overrideWrite` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_blockProfileInternal` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_fpunwindExpand` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_goroutineLeakProfileWithLabels` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_goroutineProfileWithLabels` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_makeProfStack` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_memProfileInternal` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_mutexProfileInternal` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.pprof_threadCreateInternal` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.procPin` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.procUnpin` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.procyield` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.randn` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.reflectcall` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.setCrashFD` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.set_crosscall2` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.setsigsegv` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.sigpanic` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.srcFunc.name` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.strhash` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.stringHash` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.throw` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.traceAdvance` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.traceClockNow` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.typedmemmove` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.typedslicecopy` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.typehash` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.vgetrandom` | 3 | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.wakep` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime.zeroVal` | 3 | 2 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.SetTraceback` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.WriteHeapDump` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.freeOSMemory` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.modinfo` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.readGCStats` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setGCPercent` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setMaxStack` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setMaxThreads` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setMemoryLimit` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/debug.setPanicOnFault` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/metrics.runtime_readMetrics` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/metrics_test.runtime_readMetricNames` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.readProfile` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_FrameStartLine` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_FrameSymbolName` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_cyclesPerSecond` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_expandFinalInlineFrame` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_getProfLabel` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_goroutineLeakGC` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_goroutineleakcount` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/pprof.runtime_setProfLabel` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.appendSignalStacks` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.count` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.dec` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.eraseSecrets` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.getStack` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/secret.inc` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.runtime_traceClockUnitsPerSecond` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.userLog` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.userRegion` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.userTaskCreate` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `runtime/trace.userTaskEnd` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `subtle.setDITDisabled` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `subtle.setDITEnabled` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_canSpin` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync.runtime_doSpin` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.CompareAndSwapPointer` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.CompareAndSwapUintptr` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.StorePointer` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.StoreUintptr` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.SwapPointer` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync/atomic.SwapUintptr` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `sync_test.runtime_blockUntilEmptyCleanupQueue` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_setegid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_seteuid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_setgid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_setgroups` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_setregid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_setresgid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_setresuid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_setreuid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgo_libc_setuid` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.cgocaller` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.gettimeofday` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.hasWaitingReaders` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.prlimit` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.recvfromInet4` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.recvfromInet6` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.recvmsgInet4` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.recvmsgInet6` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtimeClearenv` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtimeSetenv` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtimeUnsetenv` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_AfterExec` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_AfterFork` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_AfterForkInChild` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_BeforeExec` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_BeforeFork` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_doAllThreadsSyscall` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.runtime_envs` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.sendmsgNInet4` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.sendmsgNInet6` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.sendtoInet4` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.sendtoInet6` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `syscall.utimensat` | 1 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `testing.checkParallel` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `testing/synctest.testingSynctestTest` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.Time.abs` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.absClock` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.absDate` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.newTimer` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.nextStdChunk` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.now` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.registerLoadFromEmbeddedTZData` | 2 | 1 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.resetTimer` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.runtimeIsBubbled` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.runtimeNano` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.runtimeNow` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `time.stopTimer` | 2 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.aeadAESGCMTLS13` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.cipherSuitesTLS13` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.defaultCipherSuitesTLS13` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.defaultCipherSuitesTLS13NoAES` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `tls.errNoCertificates` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `unique.runtime_blockUntilEmptyCleanupQueue` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `weak.runtime_makeStrongFromWeak` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `weak.runtime_registerWeakPointer` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |
| `x509.systemRoots` | 1 | 0 | target not provided by graalgo (P3-M4 intrinsic surface + synthetic packages) |

## Live skip-list by target package

### `cgo` (44)

- `cgo._cgo_bindm` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo._cgo_call_symbolizer_function` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo._cgo_call_traceback_function` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo._cgo_callers` — 1 live ref(s), e.g. runtime/cgo/callbacks_traceback.go
- `cgo._cgo_getstackbound` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo._cgo_init` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo._cgo_libc_setegid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `cgo._cgo_libc_seteuid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `cgo._cgo_libc_setgid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `cgo._cgo_libc_setgroups` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `cgo._cgo_libc_setregid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `cgo._cgo_libc_setresgid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `cgo._cgo_libc_setresuid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `cgo._cgo_libc_setreuid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `cgo._cgo_libc_setuid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `cgo._cgo_mmap` — 1 live ref(s), e.g. runtime/cgo/mmap.go
- `cgo._cgo_munmap` — 1 live ref(s), e.g. runtime/cgo/mmap.go
- `cgo._cgo_notify_runtime_init_done` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo._cgo_panic` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo._cgo_pthread_key_created` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo._cgo_set_traceback_functions` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo._cgo_sigaction` — 1 live ref(s), e.g. runtime/cgo/sigaction.go
- `cgo._cgo_sys_thread_create` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo._cgo_thread_start` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo._cgo_yield` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo._crosscall2_ptr` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo.x_cgo_bindm` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo.x_cgo_call_symbolizer_function` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo.x_cgo_call_traceback_function` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo.x_cgo_callers` — 1 live ref(s), e.g. runtime/cgo/callbacks_traceback.go
- `cgo.x_cgo_clearenv` — 1 live ref(s), e.g. runtime/cgo/clearenv.go
- `cgo.x_cgo_getstackbound` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo.x_cgo_init` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo.x_cgo_mmap` — 1 live ref(s), e.g. runtime/cgo/mmap.go
- `cgo.x_cgo_munmap` — 1 live ref(s), e.g. runtime/cgo/mmap.go
- `cgo.x_cgo_notify_runtime_init_done` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo.x_cgo_pthread_key_created` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo.x_cgo_set_traceback_functions` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `cgo.x_cgo_setenv` — 1 live ref(s), e.g. runtime/cgo/setenv.go
- `cgo.x_cgo_sigaction` — 1 live ref(s), e.g. runtime/cgo/sigaction.go
- `cgo.x_cgo_sys_thread_create` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo.x_cgo_thread_start` — 1 live ref(s), e.g. runtime/cgo/callbacks_unix.go
- `cgo.x_cgo_unsetenv` — 1 live ref(s), e.g. runtime/cgo/setenv.go
- `cgo.x_crosscall2_ptr` — 1 live ref(s), e.g. runtime/cgo/callbacks.go

### `crypto/internal/fips140/check/checktest` (1)

- `crypto/internal/fips140/check/checktest.RODATA` — 1 live ref(s), e.g. crypto/internal/fips140/check/checktest/test.go

### `crypto/internal/rand` (1)

- `crypto/internal/rand.SetTestingReader` — 1 live ref(s), e.g. testing/cryptotest/rand.go

### `go/types` (2)

- `go/types.(*Checker).infer` — 1 live ref(s), e.g. go/types/badlinkname.go
- `go/types.srcimporter_setUsesCgo` — 1 live ref(s), e.g. go/internal/srcimporter/srcimporter.go

### `golang.org/x/net/internal/http3_test` (1)

- `golang.org/x/net/internal/http3_test.protocolSetHTTP3` — 1 live ref(s), e.g. net/http/http.go

### `golang.org/x/tools/go/types/typeutil` (2)

- `golang.org/x/tools/go/types/typeutil.interfaceMethod` — 2 live ref(s), e.g. cmd/vendor/golang.org/x/tools/go/types/typeutil/callee.go, cmd/vendor/golang.org/x/tools/internal/typesinternal/classify_call.go
- `golang.org/x/tools/go/types/typeutil.usedIdent` — 2 live ref(s), e.g. cmd/vendor/golang.org/x/tools/go/types/typeutil/callee.go, cmd/vendor/golang.org/x/tools/internal/typesinternal/classify_call.go

### `net/http` (1)

- `net/http.(*Transport).RoundTrip` — 1 live ref(s), e.g. net/http/roundtrip.go

### `net/http.serverHandler` (1)

- `net/http.serverHandler.ServeHTTP` — 1 live ref(s), e.g. net/http/server.go

### `net/http/internal/http2_test` (1)

- `net/http/internal/http2_test.transportFromH1Transport` — 1 live ref(s), e.g. net/http/http2.go

### `net/http_test` (2)

- `net/http_test.registerHTTP3Server` — 1 live ref(s), e.g. vendor/golang.org/x/net/http3/http3.go
- `net/http_test.registerHTTP3Transport` — 1 live ref(s), e.g. vendor/golang.org/x/net/http3/http3.go

### `net/textproto` (1)

- `net/textproto.readMIMEHeader` — 1 live ref(s), e.g. net/http/request.go

### `reflect` (32)

- `reflect.(*Value).pointer` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Align` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).AssignableTo` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Bits` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).ChanDir` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Comparable` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).ConvertibleTo` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Elem` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Field` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).FieldAlign` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).FieldByIndex` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).FieldByName` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).FieldByNameFunc` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Implements` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).In` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).IsVariadic` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Key` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Kind` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Len` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Method` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).MethodByName` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Name` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).NumField` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).NumIn` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).NumMethod` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).NumOut` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Out` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).PkgPath` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).Size` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).String` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.(*rtype).ptrTo` — 1 live ref(s), e.g. reflect/badlinkname.go
- `reflect.ifaceIndir` — 1 live ref(s), e.g. reflect/badlinkname.go

### `runtime` (42)

- `runtime._cgo_clearenv` — 1 live ref(s), e.g. runtime/cgo/clearenv.go
- `runtime._cgo_panic_internal` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `runtime._cgo_setenv` — 1 live ref(s), e.g. runtime/cgo/setenv.go
- `runtime._cgo_unsetenv` — 1 live ref(s), e.g. runtime/cgo/setenv.go
- `runtime.doInit` — 1 live ref(s), e.g. plugin/plugin_dlopen.go
- `runtime.fcntl` — 1 live ref(s), e.g. internal/syscall/unix/fcntl_unix.go
- `runtime.getAuxv` — 2 live ref(s), e.g. cmd/vendor/golang.org/x/sys/unix/auxv.go, vendor/golang.org/x/sys/cpu/runtime_auxv_go121.go
- `runtime.iscgo` — 1 live ref(s), e.g. runtime/cgo/iscgo.go
- `runtime.lockedOSThread` — 1 live ref(s), e.g. cmd/cgo/internal/test/backdoor.go
- `runtime.mapaccess1` — 1 live ref(s), e.g. internal/runtime/maps/runtime.go
- `runtime.mapaccess1_fast32` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast32.go
- `runtime.mapaccess1_fast64` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast64.go
- `runtime.mapaccess1_faststr` — 1 live ref(s), e.g. internal/runtime/maps/runtime_faststr.go
- `runtime.mapaccess2` — 1 live ref(s), e.g. internal/runtime/maps/runtime.go
- `runtime.mapaccess2_fast32` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast32.go
- `runtime.mapaccess2_fast64` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast64.go
- `runtime.mapaccess2_faststr` — 1 live ref(s), e.g. internal/runtime/maps/runtime_faststr.go
- `runtime.mapassign` — 1 live ref(s), e.g. internal/runtime/maps/runtime.go
- `runtime.mapassign_fast32` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast32.go
- `runtime.mapassign_fast32ptr` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast32.go
- `runtime.mapassign_fast64` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast64.go
- `runtime.mapassign_fast64ptr` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast64.go
- `runtime.mapassign_faststr` — 1 live ref(s), e.g. internal/runtime/maps/runtime_faststr.go
- `runtime.mapdelete_fast32` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast32.go
- `runtime.mapdelete_fast64` — 1 live ref(s), e.g. internal/runtime/maps/runtime_fast64.go
- `runtime.mapdelete_faststr` — 1 live ref(s), e.g. internal/runtime/maps/runtime_faststr.go
- `runtime.pprof_blockProfileInternal` — 1 live ref(s), e.g. runtime/pprof/pprof.go
- `runtime.pprof_fpunwindExpand` — 1 live ref(s), e.g. runtime/pprof/pprof.go
- `runtime.pprof_goroutineLeakProfileWithLabels` — 1 live ref(s), e.g. runtime/pprof/pprof.go
- `runtime.pprof_goroutineProfileWithLabels` — 1 live ref(s), e.g. runtime/pprof/pprof.go
- `runtime.pprof_makeProfStack` — 1 live ref(s), e.g. runtime/pprof/pprof.go
- `runtime.pprof_memProfileInternal` — 1 live ref(s), e.g. runtime/pprof/pprof.go
- `runtime.pprof_mutexProfileInternal` — 1 live ref(s), e.g. runtime/pprof/pprof.go
- `runtime.pprof_threadCreateInternal` — 1 live ref(s), e.g. runtime/pprof/pprof.go
- `runtime.randn` — 1 live ref(s), e.g. sync/pool.go
- `runtime.reflectcall` — 1 live ref(s), e.g. reflect/value.go
- `runtime.setCrashFD` — 1 live ref(s), e.g. runtime/debug/stack.go
- `runtime.set_crosscall2` — 1 live ref(s), e.g. runtime/cgo/callbacks.go
- `runtime.traceAdvance` — 1 live ref(s), e.g. runtime/trace/flightrecorder.go
- `runtime.traceClockNow` — 1 live ref(s), e.g. runtime/trace/recorder.go
- `runtime.vgetrandom` — 2 live ref(s), e.g. cmd/vendor/golang.org/x/sys/unix/vgetrandom_linux.go, internal/syscall/unix/getrandom.go
- `runtime.zeroVal` — 2 live ref(s), e.g. internal/runtime/maps/runtime.go, reflect/value.go

### `runtime/pprof` (1)

- `runtime/pprof.runtime_cyclesPerSecond` — 1 live ref(s), e.g. runtime/pprof/pprof.go

### `syscall` (20)

- `syscall.cgo_libc_setegid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `syscall.cgo_libc_seteuid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `syscall.cgo_libc_setgid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `syscall.cgo_libc_setgroups` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `syscall.cgo_libc_setregid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `syscall.cgo_libc_setresgid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `syscall.cgo_libc_setresuid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `syscall.cgo_libc_setreuid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `syscall.cgo_libc_setuid` — 1 live ref(s), e.g. runtime/cgo/linux.go
- `syscall.hasWaitingReaders` — 1 live ref(s), e.g. sync/rwmutex.go
- `syscall.prlimit` — 1 live ref(s), e.g. cmd/vendor/golang.org/x/sys/unix/syscall_linux.go
- `syscall.recvfromInet4` — 1 live ref(s), e.g. internal/syscall/unix/net.go
- `syscall.recvfromInet6` — 1 live ref(s), e.g. internal/syscall/unix/net.go
- `syscall.recvmsgInet4` — 1 live ref(s), e.g. internal/syscall/unix/net.go
- `syscall.recvmsgInet6` — 1 live ref(s), e.g. internal/syscall/unix/net.go
- `syscall.sendmsgNInet4` — 1 live ref(s), e.g. internal/syscall/unix/net.go
- `syscall.sendmsgNInet6` — 1 live ref(s), e.g. internal/syscall/unix/net.go
- `syscall.sendtoInet4` — 1 live ref(s), e.g. internal/syscall/unix/net.go
- `syscall.sendtoInet6` — 1 live ref(s), e.g. internal/syscall/unix/net.go
- `syscall.utimensat` — 1 live ref(s), e.g. internal/syscall/unix/utimes.go

### `testing` (1)

- `testing.checkParallel` — 1 live ref(s), e.g. testing/cryptotest/rand.go

### `testing/synctest` (1)

- `testing/synctest.testingSynctestTest` — 1 live ref(s), e.g. testing/synctest/synctest.go

### `time` (1)

- `time.registerLoadFromEmbeddedTZData` — 1 live ref(s), e.g. time/tzdata/tzdata.go


## Duplicate local names

- `rand.runtime_rand`: math/rand/rand.go, math/rand/v2/rand.go
- `unix.vgetrandom`: cmd/vendor/golang.org/x/sys/unix/vgetrandom_linux.go, internal/syscall/unix/getrandom.go

## Synthetic sources that failed to parse

These embedded synthetic packages contribute no provided symbols and no scanned linknames in this run; the frontend's buildSyntheticPackage would panic importing them.

- synthetic "regexp/syntax": parse embedded synthetic source: synth.go:445:1: imports must appear before other declarations (and 3 more errors)

## Provided symbols by source

- p3link LinknameIntrinsics.java Linknames.register: 155
- p3link LinknameIntrinsics.java Linknames.register (graalgo.p3reflect.MapKeyHash.SYMBOL): 1
- p3link LinknameIntrinsics.java Linknames.register -> p3reflect ReflectLinknames.java Linknames.register: 15
- p3link LinknameIntrinsics.java Linknames.register -> p3reflect ReflectLinknames.java Linknames.register (pkg + ".chanlen"): 2
- p3link LinknameIntrinsics.java Linknames.register -> p3reflect ReflectLinknames.java Linknames.register (pkg + ".ifaceE2I"): 2
- p3link LinknameIntrinsics.java Linknames.register -> p3reflect ReflectLinknames.java Linknames.register (pkg + ".maplen"): 2
- p3link LinknameIntrinsics.java Linknames.register -> p3reflect ReflectLinknames.java Linknames.register (pkg + ".resolveNameOff"): 2
- p3link LinknameIntrinsics.java Linknames.register -> p3reflect ReflectLinknames.java Linknames.register (pkg + ".resolveTypeOff"): 2
- p3link LinknameIntrinsics.java Linknames.register -> p3reflect ReflectLinknames.java Linknames.register (pkg + ".typedmemmove"): 2
- p3link LinknameIntrinsics.java Linknames.register -> p3reflect ReflectLinknames.java Linknames.register (pkg + ".unsafe_New"): 2
- synthetic: 2
- synthetic "archive/tar": 40
- synthetic "archive/zip": 40
- synthetic "bufio": 56
- synthetic "bytes": 92
- synthetic "cmp": 4
- synthetic "compress/bzip2": 3
- synthetic "compress/flate": 24
- synthetic "compress/gzip": 21
- synthetic "compress/lzw": 13
- synthetic "compress/zlib": 19
- synthetic "container/heap": 6
- synthetic "container/list": 20
- synthetic "container/ring": 9
- synthetic "context": 19
- synthetic "crypto": 37
- synthetic "crypto/aes": 9
- synthetic "crypto/cipher": 19
- synthetic "crypto/des": 5
- synthetic "crypto/dsa": 13
- synthetic "crypto/ecdh": 17
- synthetic "crypto/ecdsa": 17
- synthetic "crypto/ed25519": 18
- synthetic "crypto/elliptic": 17
- synthetic "crypto/hkdf": 3
- synthetic "crypto/hmac": 3
- synthetic "crypto/internal/boring": 49
- synthetic "crypto/internal/boring/bbig": 2
- synthetic "crypto/internal/constanttime": 4
- synthetic "crypto/internal/fips140": 5
- synthetic "crypto/internal/fips140/alias": 2
- synthetic "crypto/internal/fips140/bigmod": 34
- synthetic "crypto/internal/fips140/drbg": 4
- synthetic "crypto/internal/fips140/ecdh": 15
- synthetic "crypto/internal/fips140/ecdsa": 19
- synthetic "crypto/internal/fips140/ed25519": 16
- synthetic "crypto/internal/fips140/edwards25519": 26
- synthetic "crypto/internal/fips140/edwards25519/field": 22
- synthetic "crypto/internal/fips140/hkdf": 3
- synthetic "crypto/internal/fips140/hmac": 9
- synthetic "crypto/internal/fips140/nistec": 55
- synthetic "crypto/internal/fips140/nistec/fiat": 52
- synthetic "crypto/internal/fips140/pbkdf2": 1
- synthetic "crypto/internal/fips140/rsa": 32
- synthetic "crypto/internal/fips140/sha256": 12
- synthetic "crypto/internal/fips140/sha3": 31
- synthetic "crypto/internal/fips140/sha512": 14
- synthetic "crypto/internal/fips140/subtle": 4
- synthetic "crypto/internal/fips140cache": 1
- synthetic "crypto/internal/fips140deps/byteorder": 12
- synthetic "crypto/internal/fips140hash": 2
- synthetic "crypto/internal/fips140only": 3
- synthetic "crypto/internal/rand": 3
- synthetic "crypto/md5": 4
- synthetic "crypto/pbkdf2": 1
- synthetic "crypto/rand": 4
- synthetic "crypto/rc4": 6
- synthetic "crypto/rsa": 33
- synthetic "crypto/sha1": 4
- synthetic "crypto/sha256": 7
- synthetic "crypto/sha3": 40
- synthetic "crypto/sha512": 13
- synthetic "crypto/subtle": 7
- synthetic "crypto/tls": 61
- synthetic "crypto/x509": 89
- synthetic "crypto/x509/pkix": 12
- synthetic "encoding": 6
- synthetic "encoding/ascii85": 5
- synthetic "encoding/asn1": 42
- synthetic "encoding/base32": 19
- synthetic "encoding/base64": 22
- synthetic "encoding/binary": 18
- synthetic "encoding/csv": 22
- synthetic "encoding/gob": 9
- synthetic "encoding/hex": 12
- synthetic "encoding/json": 49
- synthetic "encoding/pem": 3
- synthetic "encoding/xml": 53
- synthetic "errors": 7
- synthetic "flag": 80
- synthetic "golang.org/x/crypto/cryptobyte": 55
- synthetic "golang.org/x/crypto/cryptobyte/asn1": 19
- synthetic "hash": 6
- synthetic "hash/adler32": 3
- synthetic "hash/crc32": 12
- synthetic "hash/crc64": 8
- synthetic "hash/fnv": 6
- synthetic "hash/maphash": 23
- synthetic "internal/bytealg": 17
- synthetic "internal/byteorder": 18
- synthetic "internal/graalgo/fmtstate": 1
- synthetic "internal/nettrace": 3
- synthetic "internal/poll": 34
- synthetic "internal/singleflight": 5
- synthetic "io": 67
- synthetic "io/fs": 56
- synthetic "iter": 4
- synthetic "maps": 10
- synthetic "math": 97
- synthetic "math/big": 171
- synthetic "math/bits": 50
- synthetic "math/cmplx": 27
- synthetic "math/rand": 40
- synthetic "math/rand/v2": 52
- synthetic "mime": 10
- synthetic "mime/multipart": 27
- synthetic "mime/quotedprintable": 7
- synthetic "net": 98
- synthetic "net/netip": 80
- synthetic "net/textproto": 17
- synthetic "net/url": 48
- synthetic "os": 171
- synthetic "path": 8
- synthetic "reflect_m7": 162
- synthetic "slices": 40
- synthetic "sort": 37
- synthetic "strconv": 40
- synthetic "strings": 76
- synthetic "structs": 1
- synthetic "syscall": 1806
- synthetic "testing": 41
- synthetic "testing/iotest": 10
- synthetic "testing/quick": 9
- synthetic "text/scanner": 29
- synthetic "time": 117
- synthetic "unicode": 312
- synthetic "unicode/utf16": 7
- synthetic "unicode/utf8": 19
- synthetic "unique": 2
