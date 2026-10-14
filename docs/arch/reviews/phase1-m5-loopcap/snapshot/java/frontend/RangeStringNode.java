package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RepeatingNode;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoBox;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * {@code range} over a string (M4, WS E): the index is the <em>byte</em>
 * index, the value the decoded rune. Decoding follows Go's
 * {@code unicode/utf8.DecodeRune} exactly — each invalid byte yields
 * {@code U+FFFD} with width 1 (gc-verified: {@code "A\xffB"} ranges as
 * {@code (0,'A')}, {@code (1,U+FFFD)}, {@code (2,'B')}).
 */
public final class RangeStringNode extends Node implements RepeatingNode, LabelTarget {
    private static final int RUNE_ERROR = 0xFFFD;

    @Child private GoExprNode x;
    @Child private GoStmtNode body;
    private final int keySlot;
    private final GoKind keyKind;
    private final boolean keyBoxed;
    private final boolean keyDefine;
    private final int valSlot;
    private final boolean valBoxed;
    private final boolean valDefine;
    private String label;

    private boolean started;
    private byte[] bytes;
    private int pos;

    public RangeStringNode(GoExprNode x, GoStmtNode body, int keySlot, GoKind keyKind, int valSlot) {
        this(x, body, keySlot, keyKind, false, true, valSlot, false, true);
    }

    public RangeStringNode(GoExprNode x, GoStmtNode body, int keySlot, GoKind keyKind, boolean keyBoxed,
                           boolean keyDefine, int valSlot, boolean valBoxed, boolean valDefine) {
        this.x = x;
        this.body = body;
        this.keySlot = keySlot;
        this.keyKind = keyKind;
        this.keyBoxed = keyBoxed;
        this.keyDefine = keyDefine;
        this.valSlot = valSlot;
        this.valBoxed = valBoxed;
        this.valDefine = valDefine;
    }

    @Override
    public void setLabel(String label) {
        this.label = label;
    }

    /** Resets iteration state (called before each range statement execution). */
    public void reset() {
        started = false;
    }

    /**
     * Decodes one rune at {@code bytes[off]}, Go {@code DecodeRune}
     * semantics: returns {@code (rune, width)}; invalid encodings yield
     * {@code (RUNE_ERROR, 1)}.
     */
    static int[] decodeRune(byte[] bytes, int off) {
        int n = bytes.length;
        int c0 = bytes[off] & 0xFF;
        if (c0 < 0x80) {
            return new int[]{c0, 1};
        }
        int size;
        int lo;
        int hi;
        if (c0 < 0xC2) {
            return new int[]{RUNE_ERROR, 1};
        } else if (c0 < 0xE0) {
            size = 2;
            lo = 0x80;
            hi = 0xBF;
        } else if (c0 < 0xF0) {
            size = 3;
            lo = 0x80;
            hi = 0xBF;
            if (c0 == 0xE0) {
                lo = 0xA0;
            } else if (c0 == 0xED) {
                hi = 0x9F;
            }
        } else if (c0 < 0xF5) {
            size = 4;
            lo = 0x80;
            hi = 0xBF;
            if (c0 == 0xF0) {
                lo = 0x90;
            } else if (c0 == 0xF4) {
                hi = 0x8F;
            }
        } else {
            return new int[]{RUNE_ERROR, 1};
        }
        if (off + size > n) {
            return new int[]{RUNE_ERROR, 1};
        }
        int c1 = bytes[off + 1] & 0xFF;
        if (c1 < lo || c1 > hi) {
            return new int[]{RUNE_ERROR, 1};
        }
        if (size == 2) {
            return new int[]{((c0 & 0x1F) << 6) | (c1 & 0x3F), 2};
        }
        int c2 = bytes[off + 2] & 0xFF;
        if (c2 < 0x80 || c2 > 0xBF) {
            return new int[]{RUNE_ERROR, 1};
        }
        if (size == 3) {
            int r = ((c0 & 0x0F) << 12) | ((c1 & 0x3F) << 6) | (c2 & 0x3F);
            if (r >= 0xD800 && r <= 0xDFFF) {
                return new int[]{RUNE_ERROR, 1};
            }
            return new int[]{r, 3};
        }
        int c3 = bytes[off + 3] & 0xFF;
        if (c3 < 0x80 || c3 > 0xBF) {
            return new int[]{RUNE_ERROR, 1};
        }
        int r = ((c0 & 0x07) << 18) | ((c1 & 0x3F) << 12) | ((c2 & 0x3F) << 6) | (c3 & 0x3F);
        if (r > 0x10FFFF) {
            return new int[]{RUNE_ERROR, 1};
        }
        return new int[]{r, 4};
    }

    @Override
    public boolean executeRepeating(VirtualFrame frame) {
        if (!started) {
            started = true;
            bytes = ((GoString) x.executeObject(frame)).bytes();
            pos = 0;
        }
        if (pos >= bytes.length) {
            return false;
        }
        int[] dr = decodeRune(bytes, pos);
        if (keySlot >= 0) {
            if (keyBoxed) {
                if (keyDefine) {
                    frame.setObject(keySlot, new GoBox(Long.valueOf(pos)));
                } else {
                    // `for k = range ...` into a boxed variable: reassign the
                    // same variable (write into its existing cell).
                    Boxing.setBoxedCell(frame, keySlot, keyKind, Long.valueOf(pos));
                }
            } else {
                Boxing.setSlot(frame, keySlot, keyKind, Long.valueOf(pos));
            }
        }
        if (valSlot >= 0) {
            // The rune: int32 carrier.
            if (valBoxed) {
                if (valDefine) {
                    frame.setObject(valSlot, new GoBox(Integer.valueOf(dr[0])));
                } else {
                    Boxing.setBoxedCell(frame, valSlot, GoKind.INT32, Integer.valueOf(dr[0]));
                }
            } else {
                Boxing.setSlot(frame, valSlot, GoKind.INT32, Integer.valueOf(dr[0]));
            }
        }
        pos += dr[1];
        try {
            body.executeVoid(frame);
        } catch (BreakSignal b) {
            if (b.matches(label)) {
                return false;
            }
            throw b;
        } catch (ContinueSignal c) {
            if (!c.matches(label)) {
                throw c;
            }
        }
        Safepoint.poll(); // D8 seam.
        return true;
    }
}
