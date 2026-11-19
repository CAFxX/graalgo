package graalgo.go.nodes;

import com.oracle.truffle.api.CompilerDirectives;
import com.oracle.truffle.api.CompilerDirectives.CompilationFinal;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.profiles.BranchProfile;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.I64;
import graalgo.go.nodes.N.Ref;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Unsafe;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;

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
        @Child private Expr x;
        private final GoType pointee;
        @CompilationFinal private GoType identical;
        private final BranchProfile slow = BranchProfile.create();

        public ToPtr(Expr x, GoType pointee) {
            this.x = x;
            this.pointee = pointee;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Object p = x.executeGeneric(frame);
            if (p == null) {
                return null;
            } else if (p instanceof Flat f && f.type == identical && identical != null) {
                return p;
            }
            slow.enter();
            Object q = Unsafe.convert(this, p, pointee);
            if (q == p && identical == null && p instanceof Flat f && f.type != null) {
                CompilerDirectives.transferToInterpreterAndInvalidate();
                identical = f.type;
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
