package graalgo.runtime;

import com.oracle.truffle.api.frame.FrameDescriptor;

import graalgo.frontend.GoFuncRootNode;
import graalgo.values.FuncIds;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * I-11 (2026-10-19 review): {@code runtime.FuncForPC} must resolve PCs
 * from BOTH func-PC id spaces. The {@code internal/abi}
 * {@code FuncPCABI0}/{@code FuncPCABIInternal} intrinsics mint opaque
 * entry ids in {@link FuncIds} (from {@code 0x1000}) — the values
 * {@code reflect.Value.Pointer()} returns for funcs — while
 * {@code runtime.Callers} interns PCs in {@link StackTrace} (dense ids
 * from 1). {@link GoFuncInfo#forPc(long)} used to consult only
 * {@code StackTrace}, so a reflect-derived PC resolved to Go nil and a
 * later {@code (*Func).Name()} nil-dereferenced.
 *
 * <p>Every test below would have failed before the fix (the ABI-id cases
 * returned null) and asserts gc-parity observable behavior: a valid PC
 * from either producer resolves to a non-nil {@code *runtime.Func}
 * handle.
 */
public final class GoFuncInfoTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static GoFunc goFuncNamed(String wireName) {
        GoFuncRootNode root = new GoFuncRootNode(new FrameDescriptor(), null,
                -1, new int[0], new GoKind[0], -1, GoKind.UINT64, wireName);
        return new GoFunc(root.getCallTarget(), null);
    }

    /**
     * The P0 regression: a reflect-derived PC (an {@code internal/abi}
     * entry id) resolves instead of yielding Go nil.
     */
    public void testForPcResolvesAbiId() {
        GoFunc fn = goFuncNamed("myFunc");
        long pc = FuncIds.idFor(fn);
        check(pc != 0, "ABI id must be non-zero for a non-nil func");
        GoFuncInfo info = GoFuncInfo.forPc(pc);
        check(info != null, "forPc(abi id) resolved to null (the I-11 bug)");
        check("myFunc".equals(info.funcName()),
                "funcName: expected myFunc got " + info.funcName());
    }

    /** Same id for the same function: the entry-PC stability contract. */
    public void testAbiIdStableForSameFunction() {
        GoFunc fn = goFuncNamed("stableFunc");
        check(FuncIds.idFor(fn) == FuncIds.idFor(fn),
                "FuncIds.idFor must be stable for the same function");
    }

    /** Wire name {@code main} qualifies to {@code main.main}, like traces. */
    public void testForPcQualifiesMain() {
        long pc = FuncIds.idFor(goFuncNamed("main"));
        GoFuncInfo info = GoFuncInfo.forPc(pc);
        check(info != null, "forPc(abi id of main) resolved to null");
        check("main.main".equals(info.funcName()),
                "funcName: expected main.main got " + info.funcName());
    }

    /** The pre-existing StackTrace space keeps resolving unchanged. */
    public void testForPcResolvesStackTraceId() {
        long pc = StackTrace.intern("someFunc", "f.go", 10);
        GoFuncInfo info = GoFuncInfo.forPc(pc);
        check(info != null, "forPc(StackTrace id) resolved to null");
        check("someFunc".equals(info.funcName()),
                "funcName: expected someFunc got " + info.funcName());
    }

    /** A PC from neither space still resolves to null (Go nil). */
    public void testForPcUnknownIsNull() {
        check(GoFuncInfo.forPc(Long.MAX_VALUE) == null,
                "forPc(unknown pc) must be null");
        check(GoFuncInfo.forPc(0) == null, "forPc(0) must be null");
        check(GoFuncInfo.forPc(-1) == null, "forPc(-1) must be null");
    }
}
