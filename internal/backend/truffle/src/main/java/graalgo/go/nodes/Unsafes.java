package graalgo.go.nodes;

import com.oracle.truffle.api.CompilerDirectives;
import com.oracle.truffle.api.CompilerDirectives.CompilationFinal;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.ExplodeLoop;
import com.oracle.truffle.api.profiles.BranchProfile;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.I64;
import graalgo.go.nodes.N.Ref;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Unsafe;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.Ptr;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import java.util.Arrays;

/**
 * Package {@code unsafe} as node kinds (toolexec-design §5.6; SPEC §3.4, GG-0062): the
 * conversions between {@code unsafe.Pointer}, typed pointers and {@code uintptr}, and the
 * builtins {@code Add}, {@code Slice}, {@code SliceData}, {@code String}, {@code StringData}
 * ({@code Sizeof}, {@code Alignof} and {@code Offsetof} are constants of the factory, from the
 * type's gc layout). The work is {@link Unsafe}'s, behind boundaries: none of this is typed
 * code's fast path except the identity case of {@link ToPtr}.
 */
public final class Unsafes {
    private Unsafes() {}

    /**
     * {@code (*U)(p)}, {@code p} an {@code unsafe.Pointer}: the checked identity of ADR-0015
     * decision 1. The common case — a pointer to the start of an object whose leaves are
     * {@code U}'s — is cached on the referent's type and returns the operand.
     */
    public static final class ToPtr extends Ref {
        /** Up to this many locations (referent type, leaf position) whose conversion is the identity. */
        static final int HITS = 4;

        /** A location whose conversion returned the pointer itself. */
        record Hit(GoType type, int prim, int ref, int byt) {}

        private static final Hit[] NONE = new Hit[0];

        @Child private Expr x;
        private final GoType pointee;
        private final boolean sliced;
        @CompilationFinal(dimensions = 1) private Hit[] hits = NONE;
        private final BranchProfile slow = BranchProfile.create();

        /**
         * {@code sliced}: the conversion is the operand of a slice expression,
         * {@code (*[N]T)(p)[lo:hi:max]}. gc's checkptr then checks the elements the slice takes
         * instead of the whole array type (cmd/compile walkCheckPtrAlignment), which is how
         * {@code internal/abi} reaches a descriptor's method and parameter arrays through
         * {@code *[1<<16]T}: the array may overrun the referent.
         */
        public ToPtr(Expr x, GoType pointee, boolean sliced) {
            this.x = x;
            this.pointee = pointee;
            this.sliced = sliced;
        }

        /**
         * The conversion returns its operand for a typed pointer to a location whose leaves the
         * pointee's coincide with ({@link Unsafe#typed}). That depends only on the referent's
         * type for a pointer to the start of an object, and on the type and the leaf position
         * for an interior pointer into an object of fixed size (not a backing store, whose
         * extent varies): up to {@link #HITS} such locations are checked inline. {@code reflect}
         * converts {@code Value.ptr} on every access, mostly interior pointers.
         */
        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Object p = x.executeGeneric(frame);
            if (p == null) {
                return null;
            }
            Hit[] hs = hits;
            if (p instanceof Flat f) {
                for (Hit h : hs) {
                    if (h.type == f.type && h.prim == 0 && h.ref == 0 && h.byt == 0) {
                        return p;
                    }
                }
            } else if (p instanceof Ptr q) {
                GoType t = q.obj().type;
                for (Hit h : hs) {
                    if (h.type == t && h.prim == q.prim() && h.ref == q.ref() && h.byt == q.byt()) {
                        return p;
                    }
                }
            }
            slow.enter();
            Object q = sliced ? Unsafe.convertSliced(this, p, pointee) : Unsafe.convert(this, p, pointee);
            if (q == p && hs.length < HITS) {
                Hit h = null;
                if (p instanceof Flat f && f.type != null) {
                    h = new Hit(f.type, 0, 0, 0);
                } else if (p instanceof Ptr r && r.obj().type != null && r.obj().type.kind != GoType.BACKING) {
                    h = new Hit(r.obj().type, r.prim(), r.ref(), r.byt());
                }
                if (h != null) {
                    CompilerDirectives.transferToInterpreterAndInvalidate();
                    Hit[] n = Arrays.copyOf(hs, hs.length + 1);
                    n[hs.length] = h;
                    hits = n;
                }
            }
            return q;
        }
    }

    /** {@code uintptr(p)}. */
    public static final class PtrToInt extends I64 {
        @Child private Expr x;

        public PtrToInt(Expr x) {
            this.x = x;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return Unsafe.toInt(this, x.executeGeneric(frame));
        }
    }

    /** {@code unsafe.Pointer(u)}. */
    public static final class IntToPtr extends Ref {
        @Child private I64 x;

        public IntToPtr(I64 x) {
            this.x = x;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Unsafe.fromInt(this, x.executeLong(frame));
        }
    }

    /** {@code unsafe.Add(p, n)}. */
    public static final class Add extends Ref {
        @Child private Expr ptr;
        @Child private I64 n;

        public Add(Expr ptr, I64 n) {
            this.ptr = ptr;
            this.n = n;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Object p = ptr.executeGeneric(frame);
            return Unsafe.add(this, p, n.executeLong(frame));
        }
    }

    /** {@code unsafe.SliceData(s)}. */
    public static final class SliceData extends Ref {
        @Child private Expr s;

        public SliceData(Expr s) {
            this.s = s;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Unsafe.sliceData((Slice) s.executeGeneric(frame));
        }
    }

    /** {@code unsafe.StringData(s)}. */
    public static final class StringData extends Ref {
        @Child private Expr s;

        public StringData(Expr s) {
            this.s = s;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Unsafe.strData(this, (Str) s.executeGeneric(frame));
        }
    }

    /** {@code unsafe.String(ptr, len)}: gc's two run-time panics, then the string over the bytes. */
    public static final class MakeString extends Ref {
        @Child private Expr ptr;
        @Child private I64 len;

        public MakeString(Expr ptr, I64 len) {
            this.ptr = ptr;
            this.len = len;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Object p = ptr.executeGeneric(frame);
            long n = len.executeLong(frame);
            if (n < 0) {
                throw Panics.error(this, "unsafe.String: len out of range");
            } else if (p == null && n != 0) {
                throw Panics.error(this, "unsafe.String: ptr is nil and len is not zero");
            }
            return Unsafe.stringOver(this, p, n);
        }
    }

    /** {@code unsafe.Slice(ptr, len)}: gc's two run-time panics, then the slice over the elements. */
    public static final class MakeSlice extends Ref {
        @Child private Expr ptr;
        @Child private I64 len;
        private final GoType elem;
        private final long maxLen;

        public MakeSlice(Expr ptr, I64 len, GoType elem) {
            this.ptr = ptr;
            this.len = len;
            this.elem = elem;
            long size = elem.gcSize();
            // gc: len * size must not overflow the address space; a Java array bounds it sooner.
            this.maxLen = size == 0 ? Integer.MAX_VALUE : Math.min(Integer.MAX_VALUE, Long.MAX_VALUE / size);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Object p = ptr.executeGeneric(frame);
            long n = len.executeLong(frame);
            if (n < 0) {
                throw Panics.error(this, "unsafe.Slice: len out of range");
            } else if (p == null) {
                if (n != 0) {
                    throw Panics.error(this, "unsafe.Slice: ptr is nil and len is not zero");
                }
                return Slice.NIL;
            } else if (n > maxLen) {
                throw Panics.error(this, "unsafe.Slice: len out of range");
            }
            return Unsafe.sliceOver(this, p, n, n, elem, Unsafe.SLICE_STRADDLES);
        }
    }
}
