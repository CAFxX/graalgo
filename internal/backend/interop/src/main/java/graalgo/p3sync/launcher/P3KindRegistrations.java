package graalgo.p3sync.launcher;

import graalgo.frontend.NodeFactory;
import graalgo.frontend.WireKindRegistrar;
import graalgo.iface.IfaceDispatchBuilder;
import graalgo.p3builtin.nodes.ClearNode;
import graalgo.p3builtin.nodes.GoMaxProcsNode;
import graalgo.p3builtin.nodes.GoschedNode;
import graalgo.p3builtin.nodes.MinMaxNode;
import graalgo.p3builtin.nodes.NumCPUNode;
import graalgo.p3builtin.nodes.NumGoroutineNode;
import graalgo.p3chan.nodes.ChanCloseNode;
import graalgo.p3chan.nodes.ChanLenCapNode;
import graalgo.p3chan.nodes.ChanRecvNode;
import graalgo.p3chan.nodes.ChanSendNode;
import graalgo.p3chan.nodes.MakeChanNode;
import graalgo.p3chan.nodes.SelectNode;
import graalgo.p3math.nodes.MathF32BitsNode;
import graalgo.p3math.nodes.MathF32FromBitsNode;
import graalgo.p3math.nodes.MathF64BitsNode;
import graalgo.p3math.nodes.MathF64FromBitsNode;
import graalgo.p3math.nodes.MathFmaNode;
import graalgo.p3math.nodes.MathSqrtNode;
import graalgo.p3sync.nodes.AtomicAddrNode;
import graalgo.p3sync.nodes.AtomicOpNode;
import graalgo.p3sync.nodes.SyncNewNode;
import graalgo.p3sync.nodes.SyncOpNode;

import graalgo.p3time.nodes.TimeAfterFuncNode;
import graalgo.p3time.nodes.TimeAfterNode;
import graalgo.p3time.nodes.TimeCNode;
import graalgo.p3time.nodes.TimeNewNode;
import graalgo.p3time.nodes.TimeNowNode;
import graalgo.p3time.nodes.TimeOpNode;
import graalgo.p3time.nodes.TimeSleepNode;
import graalgo.p3time.nodes.TimeTickNode;
import graalgo.p3unsafe.UnsafeAddNode;
import graalgo.p3unsafe.UnsafeFromPtrNode;
import graalgo.p3unsafe.UnsafeFromUintNode;
import graalgo.p3unsafe.UnsafePtrAddNode;
import graalgo.p3unsafe.UnsafeSliceDataNode;
import graalgo.p3unsafe.UnsafeSliceNode;
import graalgo.p3unsafe.UnsafeStringDataNode;
import graalgo.p3unsafe.UnsafeStringNode;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.p3unsafe.UnsafeToPtrNode;
import graalgo.rtmemstats.RuntimeGoRootNode;
import graalgo.rtmemstats.RuntimeReadMemStatsNode;
import graalgo.rtmemstats.RuntimeStackNode;
import graalgo.rtmemstats.RuntimeVersionNode;
import graalgo.rtstack.RuntimeCallerNode;
import graalgo.rtstack.RuntimeCallersFramesNode;
import graalgo.rtstack.RuntimeCallersNode;
import graalgo.rtstack.RuntimeFramesNextNode;
import graalgo.rtstack.RuntimeFuncEntryNode;
import graalgo.rtstack.RuntimeFuncFileLineNode;
import graalgo.rtstack.RuntimeFuncForPCNode;
import graalgo.rtstack.RuntimeFuncNameNode;
import graalgo.sched.nodes.GoStatementNode;

/**
 * Shared Phase-3 wire-kind registrations.
 *
 * <p>Both the product launcher ({@link P3SyncMain}) and the basic
 * {@code graalgo-run} launcher need these: real stdlib packages (e.g. fmt
 * via sync.Pool, reflect) emit these wire kinds, and the main launcher must
 * support them.
 *
 * <p>Implements {@link WireKindRegistrar} so the runtime launcher can
 * discover this via {@link java.util.ServiceLoader} without a compile-time
 * dependency on the interop module (which would be circular: interop needs
 * the runtime module's classes).
 */
public final class P3KindRegistrations implements WireKindRegistrar {
    /**
     * Public for {@link java.util.ServiceLoader} instantiation.
     */
    public P3KindRegistrations() {}

    @Override
    public void registerAll(NodeFactory factory) {
        registerAllStatic(factory);
    }

    /**
     * Static entry point for in-module callers. The {@link WireKindRegistrar}
     * instance method above delegates here.
     */
    public static void registerAllStatic(NodeFactory factory) {
        // P3-M2b (WS D, ADR-0002): the sync wire kinds.
        factory.registerExprKind("syncnew", (node, ctx) -> SyncNewNode.build(node, ctx, factory));
        // fix-linkname (Caddy/godebug lane): the atomicaddr wire node
        factory.registerExprKind("atomicaddr",
                (node, ctx) -> AtomicAddrNode.build(node, ctx, factory));
        // P3-M4 (WS C, ADR-0004 D2): "syncop" with an "atomic.*" synctyp
        factory.registerExprKind("syncop", (node, ctx) -> {
            String synctyp = node.object("fields", "syncop")
                    .string("synctyp", "syncop.fields");
            if (synctyp.startsWith("atomic.")) {
                return AtomicOpNode.build(node, ctx, factory);
            }
            return SyncOpNode.build(node, ctx, factory);
        });
        // P3-M3 (WS D, ADR-0003): interface method dispatch.
        factory.registerExprKind("ifacemethod", new IfaceDispatchBuilder(factory));
        // P3-M5 (WS E, ADR-0005 D3): the synthetic time wire kinds.
        factory.registerExprKind("timenew", (node, ctx) -> TimeNewNode.build(node, ctx, factory));
        factory.registerExprKind("timeop", (node, ctx) -> TimeOpNode.build(node, ctx, factory));
        factory.registerExprKind("timec", (node, ctx) -> TimeCNode.build(node, ctx, factory));
        factory.registerExprKind("timenow", TimeNowNode::build);
        factory.registerExprKind("timesleep", (node, ctx) -> TimeSleepNode.build(node, ctx, factory));
        factory.registerExprKind("timeafter", (node, ctx) -> TimeAfterNode.build(node, ctx, factory));
        factory.registerExprKind("timetick", (node, ctx) -> TimeTickNode.build(node, ctx, factory));
        factory.registerExprKind("timeafterfunc",
                (node, ctx) -> TimeAfterFuncNode.build(node, ctx, factory));
        // P3-M5 (WS E, ADR-0005 Appendix C): the channel wire kinds.
        factory.registerExprKind("makechan",
                (node, ctx) -> MakeChanNode.build(node, ctx, factory));
        factory.registerStmtKind("send",
                (node, ctx) -> ChanSendNode.build(node, ctx, factory));
        factory.registerExprKind("recv",
                (node, ctx) -> ChanRecvNode.build(node, ctx, factory));
        factory.registerExprKind("chanlen",
                (node, ctx) -> ChanLenCapNode.build(node, ctx, factory, false));
        factory.registerExprKind("chancap",
                (node, ctx) -> ChanLenCapNode.build(node, ctx, factory, true));
        factory.registerStmtKind("chanclose",
                (node, ctx) -> ChanCloseNode.build(node, ctx, factory));
        factory.registerStmtKind("select",
                (node, ctx) -> SelectNode.build(node, ctx, factory));
        // P3-M7 (WS G, ADR-0007 D5): the synthetic math wire kinds.
        factory.registerExprKind("mathf64bits",
                (node, ctx) -> MathF64BitsNode.build(node, factory, ctx));
        factory.registerExprKind("mathf64frombits",
                (node, ctx) -> MathF64FromBitsNode.build(node, factory, ctx));
        factory.registerExprKind("mathf32bits",
                (node, ctx) -> MathF32BitsNode.build(node, factory, ctx));
        factory.registerExprKind("mathf32frombits",
                (node, ctx) -> MathF32FromBitsNode.build(node, factory, ctx));
        factory.registerExprKind("mathsqrt",
                (node, ctx) -> MathSqrtNode.build(node, factory, ctx));
        factory.registerExprKind("mathfma",
                (node, ctx) -> MathFmaNode.build(node, factory, ctx));
        // P4 (driver-harden): min/max, clear, runtime.NumCPU/GOMAXPROCS.
        factory.registerExprKind("minmax",
                (node, ctx) -> MinMaxNode.build(node, ctx, factory));
        factory.registerExprKind("clear",
                (node, ctx) -> ClearNode.build(node, ctx, factory));
        factory.registerExprKind("runtimenumcpu", NumCPUNode::build);
        factory.registerExprKind("runtimenumgoroutine", NumGoroutineNode::build);
        factory.registerExprKind("runtimegomaxprocs",
                (node, ctx) -> GoMaxProcsNode.build(node, ctx, factory));
        // PKG-040: runtime.Gosched() — the "runtimegosched" statement wire
        // node (Gosched returns no value); the backend yields via
        // SchedulerCore.yieldCurrent.
        factory.registerStmtKind("runtimegosched", GoschedNode::build);
        // Stack-inspection family (synthetic runtime package).
        factory.registerExprKind("runtimecallers",
                (node, ctx) -> RuntimeCallersNode.build(node, ctx, factory));
        factory.registerExprKind("runtimecaller",
                (node, ctx) -> RuntimeCallerNode.build(node, ctx, factory));
        factory.registerExprKind("runtimecallersframes",
                (node, ctx) -> RuntimeCallersFramesNode.build(node, ctx, factory));
        factory.registerExprKind("runtimeframesnext",
                (node, ctx) -> RuntimeFramesNextNode.build(node, ctx, factory));
        factory.registerExprKind("runtimefuncforpc",
                (node, ctx) -> RuntimeFuncForPCNode.build(node, ctx, factory));
        factory.registerExprKind("runtimefuncname",
                (node, ctx) -> RuntimeFuncNameNode.build(node, ctx, factory));
        factory.registerExprKind("runtimefuncentry",
                (node, ctx) -> RuntimeFuncEntryNode.build(node, ctx, factory));
        factory.registerExprKind("runtimefuncfileline",
                (node, ctx) -> RuntimeFuncFileLineNode.build(node, ctx, factory));
        // PKG-051 (MemStats family): runtime.ReadMemStats/GOROOT/Version/
        // Stack. The frontend has emitted these additive wire nodes since
        // 6bd652f4 ("backend intrinsics to follow") but no backend handler
        // was ever registered — any program reaching runtime.Version()
        // (e.g. via runtime/debug.ReadBuildInfo) died with "unsupported
        // expression kind". The handlers implement the real gc semantics
        // against JVM telemetry (graalgo.rtmemstats).
        factory.registerExprKind("runtimereadmemstats",
                (node, ctx) -> RuntimeReadMemStatsNode.build(node, ctx, factory));
        factory.registerExprKind("runtimegoroot",
                (node, ctx) -> RuntimeGoRootNode.build(node, ctx));
        factory.registerExprKind("runtimeversion",
                (node, ctx) -> RuntimeVersionNode.build(node, ctx));
        factory.registerExprKind("runtimestack",
                (node, ctx) -> RuntimeStackNode.build(node, ctx, factory));
        // P3-M8: the synthetic unsafe wire kinds.
        factory.registerExprKind("unsafefromptr",
                (node, ctx) -> UnsafeFromPtrNode.build(node, factory, ctx));
        factory.registerExprKind("unsafetooptr",
                (node, ctx) -> UnsafeToPtrNode.build(node, factory, ctx));
        factory.registerExprKind("unsafefromuint",
                (node, ctx) -> UnsafeFromUintNode.build(node, factory, ctx));
        factory.registerExprKind("unsafeslice",
                (node, ctx) -> UnsafeSliceNode.build(node, factory, ctx));
        factory.registerExprKind("unsafeslicedata",
                (node, ctx) -> UnsafeSliceDataNode.build(node, factory, ctx));
        factory.registerExprKind("unsafestring",
                (node, ctx) -> UnsafeStringNode.build(node, factory, ctx));
        factory.registerExprKind("unsafestringdata",
                (node, ctx) -> UnsafeStringDataNode.build(node, factory, ctx));
        factory.registerExprKind("unsafeadd",
                (node, ctx) -> UnsafeAddNode.build(node, factory, ctx));
        // fix-redmaster-abifuncpc: the "unsafeptradd" wire node for the
        // unsafe.Pointer(uintptr(p) + k*unsafe.Sizeof(T)) idiom (element-
        // indexed pointer arithmetic). Public extension hook — no Phase-1/
        // Phase-2 change.
        factory.registerExprKind("unsafeptradd",
                (node, ctx) -> UnsafePtrAddNode.build(node, factory, ctx));
        UnsafeTable.install();
        // P3-M8 (wave 2, reflect): the "reflectop" wire node.
        factory.registerExprKind("reflectop",
                (node, ctx) -> graalgo.p3reflect.ReflectOpNode.build(node, ctx, factory));
        // real-reflect (Phase 1): the "abifuncpc" wire node for the
        // internal/abi FuncPCABI0/FuncPCABIInternal compile intrinsics
        // (opaque stable func ids). Public extension hook — no
        // Phase-1/Phase-2 change.
        factory.registerExprKind("abifuncpc",
                (node, ctx) -> graalgo.p3abi.AbiFuncPCNode.build(node, factory, ctx));
        // P2-M1 (WS A): the go statement.
        factory.registerStmtKind("gostmt",
                (node, ctx) -> GoStatementNode.build(node, ctx, factory));
    }
}
