package graalgo.rtstack;

import java.nio.charset.StandardCharsets;

import com.oracle.truffle.api.nodes.Node;

import graalgo.frontend.FrontendException;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoErrors;
import graalgo.runtime.GoFuncInfo;
import graalgo.runtime.GoFrames;
import graalgo.runtime.Panic;
import graalgo.runtime.StackTrace;
import graalgo.values.GoPointer;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Shared helpers for the {@code runtime} stack-inspection wire nodes
 * ({@code runtimecallers}, {@code runtimecaller},
 * {@code runtimecallersframes}, {@code runtimeframesnext},
 * {@code runtimefuncforpc}, {@code runtimefuncname},
 * {@code runtimefuncentry}, {@code runtimefuncfileline}).
 */
final class RtStack {
    private RtStack() {}

    static GoString goString(String s) {
        return new GoString(s.getBytes(StandardCharsets.UTF_8));
    }

    /** Index of a named field in a struct descriptor's longs[] carrier. */
    static int longIndex(GoStructDescriptor d, String field, String what) {
        for (GoStructDescriptor.Field f : d.fields()) {
            if (f.name().equals(field)) {
                if (!f.inLongs()) {
                    throw new FrontendException(
                            what + ": field " + field + " is not longs-carried");
                }
                return f.index();
            }
        }
        throw new FrontendException(what + ": no field " + field);
    }

    /** Index of a named field in a struct descriptor's objects[] carrier. */
    static int objIndex(GoStructDescriptor d, String field, String what) {
        for (GoStructDescriptor.Field f : d.fields()) {
            if (f.name().equals(field)) {
                if (f.inLongs()) {
                    throw new FrontendException(
                            what + ": field " + field + " is not objects-carried");
                }
                return f.index();
            }
        }
        throw new FrontendException(what + ": no field " + field);
    }

    /**
     * Unwraps a {@code *runtime.Func} Go value to its handle. A nil
     * {@code *Func} (Java null, or a nil {@link GoPointer}) raises gc's
     * nil-dereference panic, like any method call on a nil pointer.
     */
    static GoFuncInfo derefFunc(Object v, Node site) {
        GoFuncInfo info = null;
        if (v instanceof GoPointer p && !p.isNil()) {
            Object r = p.referent();
            if (r instanceof GoFuncInfo g) {
                info = g;
            }
        }
        if (info == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, site);
            throw new AssertionError("unreachable: Panic.throwPanic returned");
        }
        return info;
    }

    /**
     * Unwraps a {@code *runtime.Frames} Go value to its handle (nil
     * raises gc's nil-dereference panic).
     */
    static GoFrames derefFrames(Object v, Node site) {
        GoFrames gf = null;
        if (v instanceof GoPointer p && !p.isNil()) {
            Object r = p.referent();
            if (r instanceof GoFrames g) {
                gf = g;
            }
        }
        if (gf == null) {
            Panic.throwPanic(GoErrors.nilDereference(), null, null, site);
            throw new AssertionError("unreachable: Panic.throwPanic returned");
        }
        return gf;
    }

    /** Builds the {@code runtime.Frame} struct value for an interned PC. */
    static GoStruct buildFrame(GoStructDescriptor frameDesc, long pc) {
        StackTrace.PcInfo info = StackTrace.lookup(pc);
        if (info == null) {
            return GoStruct.zero(frameDesc);
        }
        long[] longs = new long[frameDesc.longsCount()];
        Object[] objs = new Object[frameDesc.objectsCount()];
        longs[longIndex(frameDesc, "PC", "frame")] = pc;
        GoFuncInfo fi = GoFuncInfo.forPc(pc);
        objs[objIndex(frameDesc, "Func", "frame")] =
                fi == null ? null : new GoPointer(fi);
        objs[objIndex(frameDesc, "Function", "frame")] =
                goString(StackTrace.qualifyFuncName(info.funcName()));
        objs[objIndex(frameDesc, "File", "frame")] = goString(info.file());
        longs[longIndex(frameDesc, "Line", "frame")] = info.line();
        longs[longIndex(frameDesc, "Entry", "frame")] =
                StackTrace.entryFor(info.funcName());
        return new GoStruct(frameDesc, longs, objs);
    }

    /**
     * The type-table id of a named field of the struct underlying a
     * (possibly named) struct type id — for resolving nested struct
     * descriptors (the {@code frame} field of {@code framesNextPair}).
     */
    static int structFieldTypeId(NodeFactory.BuildCtx ctx, int typeId,
            String fieldName, String path) {
        int sid = typeId;
        for (int steps = 0; ; steps++) {
            if (steps > 8) {
                throw new FrontendException(
                        path + ": type chain too deep resolving struct");
            }
            var entry = ctx.types.entry(sid, path);
            String k = entry.string("kind", path);
            if ("named".equals(k)) {
                sid = entry.object("detail", path).intValue("underlying", path);
            } else if ("alias".equals(k)) {
                sid = entry.object("detail", path).intValue("rhs", path);
            } else {
                if (!"struct".equals(k)) {
                    throw new FrontendException(
                            path + ": expected struct type, found " + k);
                }
                break;
            }
        }
        var entry = ctx.types.entry(sid, path);
        var fields = entry.object("detail", path).array("fields", path);
        for (int i = 0; i < fields.size(); i++) {
            var fv = fields.get(i);
            if (!(fv instanceof Json.Object fo)) {
                throw new FrontendException(
                        path + ": struct field " + i + " is not an object");
            }
            if (fieldName.equals(fo.string("name", path))) {
                return fo.intValue("type", path);
            }
        }
        throw new FrontendException(path + ": struct has no field " + fieldName);
    }
}
