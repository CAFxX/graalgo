package graalgo.frontend;

import java.util.ArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.source.Source;

import graalgo.p3chan.nodes.ChanCloseNode;
import graalgo.p3chan.nodes.ChanRecvNode;
import graalgo.p3chan.nodes.ChanSendNode;
import graalgo.p3chan.nodes.MakeChanNode;
import graalgo.p3chan.nodes.SelectNode;
import graalgo.runtime.ChanOps;
import graalgo.runtime.GoChan;
import graalgo.runtime.PanicException;
import graalgo.sched.SchedulerCore;
import graalgo.values.GoStruct;

/**
 * Unit tests for the P3-M5 channel builders (ADR-0005 Appendix C):
 * {@code "makechan"}, {@code "send"}, {@code "recv"},
 * {@code "chanclose"}, {@code "select"}.
 *
 * <p>Package placement is deliberate: the builders take a
 * {@link NodeFactory.BuildCtx} whose constructor is package-private, so
 * the tests live in {@code graalgo.frontend} (the
 * {@code DescriptorNamesTest} precedent) and drive each builder's
 * {@code build} method directly on parsed wire fragments — the same
 * fragments the exporter emits. Node execution runs on the real
 * scheduler so park/unpark paths are exercised, not stubbed.
 *
 * <p>verifies: GG-0017 (chan slice)
 */
public final class ChanBuilderTest {

    private interface ThrowingRunnable {
        void run() throws Exception;
    }

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static void assertEqObj(Object e, Object a, String msg) {
        if (!e.equals(a)) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    /** Runs the body, polling the condition until true or the timeout (ms) elapses. */
    private static void waitUntil(java.util.function.BooleanSupplier cond, long timeoutMs,
                                  String what) throws Exception {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (!cond.getAsBoolean()) {
            if (System.currentTimeMillis() > deadline) {
                throw new AssertionError("timed out waiting for: " + what);
            }
            Thread.sleep(5);
        }
    }

    private static void withScheduler(ThrowingRunnable body) throws Exception {
        SchedulerCore sched = new SchedulerCore(4, SchedulerCore.DEFAULT_QUANTUM_NANOS);
        sched.start();
        SchedulerCore.install(sched);
        try {
            body.run();
            sched.awaitQuiescence();
        } finally {
            SchedulerCore.install(null);
        }
    }

    // ---------- wire-building scaffolding ----------

    /** Type ids in the test type table. */
    private static final int T_INT = 0;
    private static final int T_CHAN_INT = 1;
    private static final int T_STRUCT = 2;
    private static final int T_CHAN_STRUCT = 3;
    private static final int T_BOOL = 4;

    private static final String TYPES_JSON = "["
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"int\",\"name\":\"int\"}},"
            + "{\"kind\":\"chan\",\"detail\":{\"elem\":0}},"
            + "{\"kind\":\"struct\",\"detail\":{\"fields\":[{\"name\":\"x\",\"type\":0}]}},"
            + "{\"kind\":\"chan\",\"detail\":{\"elem\":2}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"bool\",\"name\":\"bool\"}}"
            + "]";

    private static final String POS = "{\"line\":1,\"col\":1,\"len\":1}";

    private NodeFactory factory;
    private NodeFactory.BuildCtx ctx;
    private ArrayList<Json.Value> objects;

    /** Declares a test variable: slot + objid + objects entry (the exporter's vardecl). */
    private int declareObj(String name, int typeId, FrameSlotKind slotKind) {
        int slot = ctx.frames.addSlot(slotKind, name, null);
        // Wire invariant: objids are dense indices into the objects array.
        int obj = objects.size();
        ctx.objSlots.put(obj, slot);
        objects.add(Json.parse("{\"kind\":\"var\",\"name\":\"" + name + "\",\"type\":" + typeId + "}"));
        return obj;
    }

    private int slotOf(int obj) {
        return ctx.objSlots.get(obj);
    }

    private void newBuild() {
        Source source = Source.newBuilder("go", "package main\n", "test.go").build();
        factory = new NodeFactory(source);
        factory.registerExprKind("makechan", (n, c) -> MakeChanNode.build(n, c, factory));
        factory.registerStmtKind("send", (n, c) -> ChanSendNode.build(n, c, factory));
        factory.registerExprKind("recv", (n, c) -> ChanRecvNode.build(n, c, factory));
        factory.registerStmtKind("chanclose", (n, c) -> ChanCloseNode.build(n, c, factory));
        factory.registerStmtKind("select", (n, c) -> SelectNode.build(n, c, factory));
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        objects = new ArrayList<>();
        ctx = new NodeFactory.BuildCtx(source, FrameDescriptor.newBuilder(),
                new TypeTable(types), new Json.Array(objects));
    }

    private VirtualFrame newFrame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0], ctx.frames.build());
    }

    private String ident(int obj, String name, int typeId) {
        return "{\"kind\":\"ident\",\"pos\":" + POS
                + ",\"fields\":{\"name\":\"" + name + "\",\"obj\":" + obj + ",\"type\":" + typeId + "}}";
    }

    private String basiclit(String value, int typeId) {
        return "{\"kind\":\"basiclit\",\"pos\":" + POS
                + ",\"fields\":{\"const\":\"" + value + "\",\"type\":" + typeId
                + ",\"value\":\"" + value + "\"}}";
    }

    private GoExprNode buildExpr(String json) {
        return factory.buildExpr((Json.Object) Json.parse(json), ctx);
    }

    private GoStmtNode buildStmt(String json) {
        return factory.buildStmt((Json.Object) Json.parse(json), ctx);
    }

    private GoExprNode makechan(Integer sizeLit, int elemId) {
        String size = sizeLit == null ? "" : ",\"size\":" + sizeLit;
        return buildExpr("{\"kind\":\"makechan\",\"pos\":" + POS
                + ",\"fields\":{\"elem\":" + elemId + size + "}}");
    }

    private GoStmtNode send(String chanJson, String valueJson) {
        return buildStmt("{\"kind\":\"send\",\"pos\":" + POS
                + ",\"fields\":{\"chan\":" + chanJson + ",\"value\":" + valueJson + "}}");
    }

    private GoExprNode recv(String chanJson, int typeId) {
        return buildExpr("{\"kind\":\"recv\",\"pos\":" + POS
                + ",\"fields\":{\"x\":" + chanJson + ",\"type\":" + typeId + "}}");
    }

    private GoStmtNode chanclose(String chanJson) {
        return buildStmt("{\"kind\":\"chanclose\",\"pos\":" + POS
                + ",\"fields\":{\"x\":" + chanJson + "}}");
    }

    /** A "selectcase" node (the exporter's committed wire shape). */
    private String selectCase(String innerKind, String innerFields) {
        return "{\"kind\":\"selectcase\",\"pos\":" + POS
                + ",\"fields\":{\"kind\":\"" + innerKind + "\"" + innerFields + "}}";
    }

    private String blockNode(String... stmts) {
        return "{\"kind\":\"block\",\"pos\":" + POS
                + ",\"fields\":{\"stmts\":[" + String.join(",", stmts) + "]}}";
    }

    private String selectStmt(String... cases) {
        return "{\"kind\":\"select\",\"pos\":" + POS
                + ",\"fields\":{\"cases\":[" + String.join(",", cases) + "]}}";
    }

    /** Runs body on a scheduler goroutine, rethrowing failures on the test thread. */
    private void inGoroutine(ThrowingRunnable body, CountDownLatch done,
                             AtomicReference<Throwable> err) {
        SchedulerCore.current().spawn(() -> {
            try {
                body.run();
            } catch (Throwable t) {
                err.set(t);
            } finally {
                done.countDown();
            }
        });
    }

    private interface FrameBody {
        void run(VirtualFrame f) throws Exception;
    }

    /**
     * Runs {@code body} on a scheduler goroutine with a fresh frame over
     * the test descriptor. Select/send/recv that may park REQUIRE a
     * goroutine context ({@code SchedulerCore.currentGoroutine}).
     */
    private void inGoroutineFrame(FrameDescriptor fd, FrameBody body) throws Exception {
        CountDownLatch done = new CountDownLatch(1);
        AtomicReference<Throwable> err = new AtomicReference<>();
        SchedulerCore.current().spawn(() -> {
            try {
                body.run(Truffle.getRuntime().createVirtualFrame(new Object[0], fd));
            } catch (Throwable t) {
                err.set(t);
            } finally {
                done.countDown();
            }
        });
        assertTrue(done.await(10, TimeUnit.SECONDS), "goroutine finished");
        if (err.get() != null) {
            throw new AssertionError("goroutine failed", err.get());
        }
    }

    // ---------- makechan ----------

    public void testMakechanBuffered() throws Exception {
        withScheduler(() -> {
            newBuild();
            GoExprNode n = makechan(2, T_INT);
            VirtualFrame f = newFrame();
            Object ch = n.executeObject(f);
            assertTrue(ch instanceof GoChan, "makechan yields a GoChan");
            assertEq(2, ((GoChan) ch).capacity(), "buffered capacity");
        });
    }

    public void testMakechanUnbufferedWithoutSize() throws Exception {
        withScheduler(() -> {
            newBuild();
            GoExprNode n = makechan(null, T_INT);
            VirtualFrame f = newFrame();
            Object ch = n.executeObject(f);
            assertTrue(ch instanceof GoChan, "makechan yields a GoChan");
            assertEq(0, ((GoChan) ch).capacity(), "unbuffered without size");
        });
    }

    public void testMakechanNegativeSizePanics() throws Exception {
        withScheduler(() -> {
            newBuild();
            GoExprNode n = makechan(-1, T_INT);
            VirtualFrame f = newFrame();
            try {
                n.executeObject(f);
                assertTrue(false, "negative size must panic");
            } catch (PanicException pe) {
                assertEqObj("makechan: negative size", pe.goValue().toString(), "panic message");
            }
        });
    }

    /** makechan with a matching "type" field (the exporter's wire) works. */
    public void testMakechanMatchingTypeField() throws Exception {
        withScheduler(() -> {
            newBuild();
            GoExprNode n = buildExpr("{\"kind\":\"makechan\",\"pos\":" + POS
                    + ",\"fields\":{\"elem\":" + T_INT + ",\"size\":2,\"type\":" + T_CHAN_INT + "}}");
            VirtualFrame f = newFrame();
            Object ch = n.executeObject(f);
            assertTrue(ch instanceof GoChan, "makechan yields a GoChan");
            assertEq(2, ((GoChan) ch).capacity(), "buffered capacity");
        });
    }

    /** makechan with a mismatched "type" field fails fast at build. */
    public void testMakechanMismatchedTypeField() throws Exception {
        withScheduler(() -> {
            newBuild();
            try {
                buildExpr("{\"kind\":\"makechan\",\"pos\":" + POS
                        + ",\"fields\":{\"elem\":" + T_INT + ",\"type\":" + T_INT + "}}");
                assertTrue(false, "non-chan type must be rejected");
            } catch (FrontendException fe) {
                assertTrue(fe.getMessage().contains("must be a chan"), "rejection message");
            }
            try {
                buildExpr("{\"kind\":\"makechan\",\"pos\":" + POS
                        + ",\"fields\":{\"elem\":" + T_INT + ",\"type\":" + T_CHAN_STRUCT + "}}");
                assertTrue(false, "elem mismatch must be rejected");
            } catch (FrontendException fe) {
                assertTrue(fe.getMessage().contains("mismatch"), "rejection message");
            }
        });
    }

    // ---------- send / recv / close ----------

    public void testSendRecvBuffered() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            String chId = ident(chObj, "ch", T_CHAN_INT);
            GoExprNode mk = makechan(1, T_INT);
            GoStmtNode snd = send(chId, basiclit("42", T_INT));
            GoExprNode rcv = recv(chId, T_INT);
            VirtualFrame f = newFrame();
            f.setObject(slotOf(chObj), mk.executeObject(f));
            snd.executeVoid(f);
            assertEq(42L, ((Number) rcv.executeObject(f)).longValue(), "buffered round trip");
        });
    }

    public void testSendOnClosedPanics() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            String chId = ident(chObj, "ch", T_CHAN_INT);
            VirtualFrame f = newFrame();
            f.setObject(slotOf(chObj), makechan(1, T_INT).executeObject(f));
            chanclose(chId).executeVoid(f);
            try {
                send(chId, basiclit("1", T_INT)).executeVoid(f);
                assertTrue(false, "send on closed must panic");
            } catch (PanicException pe) {
                assertEqObj("send on closed channel", pe.goValue().toString(), "panic message");
            }
        });
    }

    public void testCloseOfClosedPanics() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            String chId = ident(chObj, "ch", T_CHAN_INT);
            VirtualFrame f = newFrame();
            f.setObject(slotOf(chObj), makechan(1, T_INT).executeObject(f));
            chanclose(chId).executeVoid(f);
            try {
                chanclose(chId).executeVoid(f);
                assertTrue(false, "close of closed must panic");
            } catch (PanicException pe) {
                assertEqObj("close of closed channel", pe.goValue().toString(), "panic message");
            }
        });
    }

    public void testCloseOfNilPanics() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            String chId = ident(chObj, "ch", T_CHAN_INT);
            VirtualFrame f = newFrame();
            f.setObject(slotOf(chObj), null); // nil channel
            try {
                chanclose(chId).executeVoid(f);
                assertTrue(false, "close of nil must panic");
            } catch (PanicException pe) {
                assertEqObj("close of nil channel", pe.goValue().toString(), "panic message");
            }
        });
    }

    public void testRecvOnClosedDrainedYieldsZero() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            String chId = ident(chObj, "ch", T_CHAN_INT);
            VirtualFrame f = newFrame();
            f.setObject(slotOf(chObj), makechan(1, T_INT).executeObject(f));
            send(chId, basiclit("5", T_INT)).executeVoid(f);
            chanclose(chId).executeVoid(f);
            GoExprNode rcv = recv(chId, T_INT);
            assertEq(5L, ((Number) rcv.executeObject(f)).longValue(), "drained value first");
            assertEq(0L, ((Number) rcv.executeObject(f)).longValue(), "zero after drain");
        });
    }

    public void testStructElemZeroOnClosedDrained() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_STRUCT, FrameSlotKind.Object);
            String chId = ident(chObj, "ch", T_CHAN_STRUCT);
            VirtualFrame f = newFrame();
            f.setObject(slotOf(chObj), makechan(1, T_STRUCT).executeObject(f));
            chanclose(chId).executeVoid(f);
            Object v = recv(chId, T_STRUCT).executeObject(f);
            assertTrue(v instanceof GoStruct, "struct zero value");
            assertEq(0L, ((GoStruct) v).getLong(0), "struct field zero");
        });
    }

    // ---------- select ----------

    /** select with one ready recv case: value assigned to the target. */
    public void testSelectRecvChoosesReady() throws Exception {
        withScheduler(() -> {
            newBuild();
            int aObj = declareObj("a", T_CHAN_INT, FrameSlotKind.Object);
            int bObj = declareObj("b", T_CHAN_INT, FrameSlotKind.Object);
            int vObj = declareObj("v", T_INT, FrameSlotKind.Long);
            GoStmtNode sel = buildStmt(selectStmt(
                    selectCase("recv", ",\"chan\":" + ident(aObj, "a", T_CHAN_INT)
                            + ",\"targets\":[" + vObj + "]"
                            + ",\"body\":" + blockNode()),
                    selectCase("recv", ",\"chan\":" + ident(bObj, "b", T_CHAN_INT)
                            + ",\"targets\":[" + vObj + "]"
                            + ",\"body\":" + blockNode())));
            FrameDescriptor fd = ctx.frames.build();
            int aSlot = slotOf(aObj), bSlot = slotOf(bObj), vSlot = slotOf(vObj);
            GoChan a = (GoChan) makechan(1, T_INT).executeObject(newFrame());
            GoChan b = (GoChan) makechan(1, T_INT).executeObject(newFrame());
            ChanOps.send(a, 7L);
            inGoroutineFrame(fd, f -> {
                f.setObject(aSlot, a);
                f.setObject(bSlot, b);
                sel.executeVoid(f);
                assertEq(7L, f.getLong(vSlot), "ready case's value assigned");
            });
        });
    }

    /** select with no ready case and a default: the default body runs. */
    public void testSelectDefault() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            int mObj = declareObj("m", T_INT, FrameSlotKind.Long);
            String setMarker = "{\"kind\":\"assign\",\"pos\":" + POS + ",\"fields\":{\"tok\":\"=\","
                    + "\"lhs\":[" + ident(mObj, "m", T_INT) + "],"
                    + "\"rhs\":[" + basiclit("1", T_INT) + "]}}";
            GoStmtNode sel = buildStmt(selectStmt(
                    selectCase("recv", ",\"chan\":" + ident(chObj, "ch", T_CHAN_INT)
                            + ",\"body\":" + blockNode()),
                    selectCase("default", ",\"body\":" + blockNode(setMarker))));
            FrameDescriptor fd = ctx.frames.build();
            int chSlot = slotOf(chObj), mSlot = slotOf(mObj);
            GoChan ch = (GoChan) makechan(null, T_INT).executeObject(newFrame()); // unbuffered, empty
            inGoroutineFrame(fd, f -> {
                f.setObject(chSlot, ch);
                f.setLong(mSlot, 0L);
                sel.executeVoid(f);
                assertEq(1L, f.getLong(mSlot), "default body ran");
            });
        });
    }

    /** select send case on a ready channel: value delivered. */
    public void testSelectSendCase() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            int mObj = declareObj("m", T_INT, FrameSlotKind.Long);
            String setMarker = "{\"kind\":\"assign\",\"pos\":" + POS + ",\"fields\":{\"tok\":\"=\","
                    + "\"lhs\":[" + ident(mObj, "m", T_INT) + "],"
                    + "\"rhs\":[" + basiclit("1", T_INT) + "]}}";
            GoStmtNode sel = buildStmt(selectStmt(
                    selectCase("send", ",\"chan\":" + ident(chObj, "ch", T_CHAN_INT)
                            + ",\"value\":" + basiclit("99", T_INT)
                            + ",\"body\":" + blockNode(setMarker)),
                    selectCase("default", ",\"body\":" + blockNode())));
            FrameDescriptor fd = ctx.frames.build();
            int chSlot = slotOf(chObj), mSlot = slotOf(mObj);
            GoChan ch = (GoChan) makechan(1, T_INT).executeObject(newFrame());
            inGoroutineFrame(fd, f -> {
                f.setObject(chSlot, ch);
                f.setLong(mSlot, 0L);
                sel.executeVoid(f);
                assertEq(1L, f.getLong(mSlot), "send case won over default");
                assertEq(99L, ((Number) ChanOps.recv(ch)).longValue(), "sent value arrived");
            });
        });
    }

    /** select send-on-closed panics BEFORE default (gc parity, ch11). */
    public void testSelectSendOnClosedPanicsBeforeDefault() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            GoStmtNode sel = buildStmt(selectStmt(
                    selectCase("send", ",\"chan\":" + ident(chObj, "ch", T_CHAN_INT)
                            + ",\"value\":" + basiclit("1", T_INT)
                            + ",\"body\":" + blockNode()),
                    selectCase("default", ",\"body\":" + blockNode())));
            FrameDescriptor fd = ctx.frames.build();
            int chSlot = slotOf(chObj);
            GoChan ch = (GoChan) makechan(null, T_INT).executeObject(newFrame());
            ChanOps.close(ch);
            AtomicReference<String> panicMsg = new AtomicReference<>();
            inGoroutineFrame(fd, f -> {
                f.setObject(chSlot, ch);
                try {
                    sel.executeVoid(f);
                    panicMsg.set("NO_PANIC");
                } catch (PanicException pe) {
                    panicMsg.set(pe.goValue().toString());
                }
            });
            assertEqObj("send on closed channel", panicMsg.get(), "panic message");
        });
    }

    /** A nil-channel case is dropped (gc-exact: never ready) — default wins. */
    public void testSelectNilChannelCaseDropped() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            int mObj = declareObj("m", T_INT, FrameSlotKind.Long);
            String setMarker = "{\"kind\":\"assign\",\"pos\":" + POS + ",\"fields\":{\"tok\":\"=\","
                    + "\"lhs\":[" + ident(mObj, "m", T_INT) + "],"
                    + "\"rhs\":[" + basiclit("1", T_INT) + "]}}";
            GoStmtNode sel = buildStmt(selectStmt(
                    selectCase("recv", ",\"chan\":" + ident(chObj, "ch", T_CHAN_INT)
                            + ",\"body\":" + blockNode()),
                    selectCase("default", ",\"body\":" + blockNode(setMarker))));
            FrameDescriptor fd = ctx.frames.build();
            int chSlot = slotOf(chObj), mSlot = slotOf(mObj);
            inGoroutineFrame(fd, f -> {
                f.setObject(chSlot, null); // nil channel
                f.setLong(mSlot, 0L);
                sel.executeVoid(f);
                assertEq(1L, f.getLong(mSlot), "nil case dropped, default taken");
            });
        });
    }

    /**
     * The comma-ok desugar pattern: a single-case select with
     * {@code targets: [v, ok]} on a closed, drained channel assigns
     * {@code (zero, false)}.
     */
    public void testSelectSingleCaseTwoValueRecv() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            int vObj = declareObj("v", T_INT, FrameSlotKind.Long);
            int okObj = declareObj("ok", T_BOOL, FrameSlotKind.Boolean);
            GoStmtNode sel = buildStmt(selectStmt(
                    selectCase("recv", ",\"chan\":" + ident(chObj, "ch", T_CHAN_INT)
                            + ",\"targets\":[" + vObj + "," + okObj + "]"
                            + ",\"body\":" + blockNode())));
            FrameDescriptor fd = ctx.frames.build();
            int chSlot = slotOf(chObj), vSlot = slotOf(vObj), okSlot = slotOf(okObj);
            GoChan ch = (GoChan) makechan(1, T_INT).executeObject(newFrame());
            ChanOps.close(ch);
            inGoroutineFrame(fd, f -> {
                f.setObject(chSlot, ch);
                f.setLong(vSlot, -1L);
                f.setBoolean(okSlot, true);
                sel.executeVoid(f);
                assertEq(0L, f.getLong(vSlot), "value is zero");
                assertTrue(!f.getBoolean(okSlot), "ok is false");
            });
        });
    }

    /** A select with no ready case parks scheduler-aware until a sender arrives. */
    public void testSelectBlocksUntilSend() throws Exception {
        withScheduler(() -> {
            newBuild();
            int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
            int vObj = declareObj("v", T_INT, FrameSlotKind.Long);
            GoStmtNode sel = buildStmt(selectStmt(
                    selectCase("recv", ",\"chan\":" + ident(chObj, "ch", T_CHAN_INT)
                            + ",\"targets\":[" + vObj + "]"
                            + ",\"body\":" + blockNode())));
            FrameDescriptor fd = ctx.frames.build();
            int chSlot = slotOf(chObj);
            int vSlot = slotOf(vObj);
            VirtualFrame pre = Truffle.getRuntime().createVirtualFrame(new Object[0], fd);
            GoChan ch = (GoChan) makechan(null, T_INT).executeObject(pre); // unbuffered
            CountDownLatch done = new CountDownLatch(1);
            AtomicReference<Throwable> err = new AtomicReference<>();
            inGoroutine(() -> {
                VirtualFrame f = Truffle.getRuntime().createVirtualFrame(new Object[0], fd);
                f.setObject(chSlot, ch);
                sel.executeVoid(f); // parks until the test thread sends
                assertEq(42L, f.getLong(vSlot), "woken select assigned the sent value");
            }, done, err);
            waitUntil(() -> ch.recvWaitersCount() == 1, 5000, "select parked on channel");
            ChanOps.send(ch, 42L); // test thread: the waker
            assertTrue(done.await(10, TimeUnit.SECONDS), "select goroutine finished");
            if (err.get() != null) {
                throw new AssertionError("select goroutine failed", err.get());
            }
        });
    }

    // ---------- wire rejection ----------

    public void testSelectRejectsTargetsOnSendCase() {
        newBuild();
        int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
        try {
            buildStmt(selectStmt(
                    selectCase("send", ",\"chan\":" + ident(chObj, "ch", T_CHAN_INT)
                            + ",\"value\":" + basiclit("1", T_INT)
                            + ",\"targets\":[1]"
                            + ",\"body\":" + blockNode())));
            assertTrue(false, "send case with targets must be rejected");
        } catch (FrontendException fe) {
            assertTrue(fe.getMessage().contains("takes no targets"), "rejection message");
        }
    }

    public void testSelectRejectsUndeclaredTarget() {
        newBuild();
        int chObj = declareObj("ch", T_CHAN_INT, FrameSlotKind.Object);
        try {
            buildStmt(selectStmt(
                    selectCase("recv", ",\"chan\":" + ident(chObj, "ch", T_CHAN_INT)
                            + ",\"targets\":[4242]"
                            + ",\"body\":" + blockNode())));
            assertTrue(false, "undeclared target must be rejected");
        } catch (FrontendException fe) {
            assertTrue(fe.getMessage().contains("not declared"), "rejection message");
        }
    }
}
