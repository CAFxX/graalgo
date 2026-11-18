package abispike;

import abispike.Mem.Addr;
import abispike.Reflect.Value;
import abispike.Values.Flat;
import abispike.Values.Iface;
import abispike.Values.Layout;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;

/**
 * Prototype driver.
 *
 * <pre>
 *   Main dump     types.tab images.dump   # synthesized descriptors, relocatable, for imgcheck (gc's reflect reads them)
 *   Main describe types.tab java.lines    # the transliterated abi/reflect code reads the same descriptors
 *   Main bench    types.tab               # indicative ns/op of the pointer model under the plain JVM JIT
 * </pre>
 *
 * Line format is shared with imgcheck: {@code t <root> <key> <value>} and
 * {@code v <name> <key> <value>}, values hex-encoded when they are strings.
 */
public final class Main {
    private Main() {}

    public static void main(String[] args) throws Exception {
        TypeTab tab = TypeTab.read(Path.of(args[1]));
        Images images = new Images(tab, System.getenv("ABISPIKE_MUTATE") != null);
        switch (args[0]) {
            case "dump" -> Files.writeString(Path.of(args[2]), images.dump());
            case "describe" -> Files.write(Path.of(args[2]), describe(tab, images));
            case "bench" -> bench(tab, images);
            default -> throw new IllegalArgumentException(args[0]);
        }
    }

    private static String hx(String s) {
        return "x" + HexFormat.of().formatHex(s.getBytes(StandardCharsets.UTF_8));
    }

    static List<String> describe(TypeTab tab, Images images) {
        Abi abi = new Abi(tab, images);
        List<String> out = new ArrayList<>();
        for (int r = 0; r < tab.roots.size(); r++) {
            Addr t = images.desc[tab.roots.get(r)];
            String p = "t " + r + " ";
            int kind = abi.kind(t);
            out.add(p + "kind " + kind);
            out.add(p + "size " + abi.size(t));
            out.add(p + "align " + abi.align(t));
            out.add(p + "fieldalign " + abi.fieldAlign(t));
            out.add(p + "string " + hx(abi.string(t)));
            out.add(p + "name " + hx(abi.name(t)));
            out.add(p + "pkgpath " + hx(abi.pkgPath(t)));
            out.add(p + "nummethod " + abi.numMethod(t));
            for (int i = 0; i < abi.numMethod(t); i++) {
                out.add(p + "m" + i + ".name " + hx(abi.methodName(t, i)));
            }
            if (abi.elem(t) != null) {
                out.add(p + "elem " + hx(abi.string(abi.elem(t))));
            }
            switch (kind) {
                case Images.K_MAP -> out.add(p + "key " + hx(abi.string(abi.key(t))));
                case Images.K_ARRAY -> out.add(p + "len " + abi.len(t));
                case Images.K_CHAN -> out.add(p + "chandir " + abi.chanDir(t));
                case Images.K_FUNC -> {
                    out.add(p + "numin " + abi.numIn(t));
                    for (int i = 0; i < abi.numIn(t); i++) {
                        out.add(p + "in" + i + " " + hx(abi.string(abi.in(t, i))));
                    }
                    out.add(p + "numout " + abi.numOut(t));
                    for (int i = 0; i < abi.numOut(t); i++) {
                        out.add(p + "out" + i + " " + hx(abi.string(abi.out(t, i))));
                    }
                    out.add(p + "variadic " + abi.isVariadic(t));
                }
                case Images.K_STRUCT -> {
                    out.add(p + "numfield " + abi.numField(t));
                    for (int i = 0; i < abi.numField(t); i++) {
                        Addr f = abi.field(t, i);
                        Addr n = abi.fieldName(f);
                        String fp = p + "f" + i + ".";
                        out.add(fp + "name " + hx(new String(Abi.nameName(n), StandardCharsets.UTF_8)));
                        out.add(fp + "type " + hx(abi.string(abi.fieldType(f))));
                        out.add(fp + "offset " + abi.fieldOffset(f));
                        out.add(fp + "tag " + hx(new String(Abi.nameTag(n), StandardCharsets.UTF_8)));
                        out.add(fp + "embedded " + Abi.nameIsEmbedded(n));
                        out.add(fp + "exported " + Abi.nameIsExported(n));
                    }
                }
                default -> { }
            }
        }
        values(tab, images, abi, out);
        return out;
    }

    /** Indicative only: HotSpot/Graal JIT without Truffle partial evaluation, best of 5 rounds. */
    private static void bench(TypeTab tab, Images images) {
        Abi abi = new Abi(tab, images);
        Reflect rf = new Reflect(abi);
        TypeTab.Type pairT = byStr(tab, "fixture.Pair");
        Flat pair = new Flat(new Layout(tab, pairT));
        pair.storeBits(offsetOf(tab, pairT, "V"), 4, -7);
        Iface boxed = new Iface(images.desc[pairT.id], pair);
        Addr t = images.desc[pairT.id];
        int n = 20_000_000;
        long bestField = Long.MAX_VALUE, bestKind = Long.MAX_VALUE, bestString = Long.MAX_VALUE;
        long sink = 0;
        for (int round = 0; round < 5; round++) {
            long t0 = System.nanoTime();
            for (int i = 0; i < n; i++) {
                sink += rf.intOf(rf.field(rf.valueOf(boxed), 1));
            }
            long t1 = System.nanoTime();
            for (int i = 0; i < n; i++) {
                sink += abi.kind(t) + abi.size(t);
            }
            long t2 = System.nanoTime();
            for (int i = 0; i < n / 20; i++) {
                sink += abi.string(t).length();
            }
            long t3 = System.nanoTime();
            bestField = Math.min(bestField, t1 - t0);
            bestKind = Math.min(bestKind, t2 - t1);
            bestString = Math.min(bestString, (t3 - t2) * 20);
        }
        System.out.printf("bench java ValueOf(x).Field(1).Int() %.1f ns/op%n", (double) bestField / n);
        System.out.printf("bench java t.Kind()+t.Size()         %.1f ns/op%n", (double) bestKind / n);
        System.out.printf("bench java t.String()                %.1f ns/op%n", (double) bestString / n);
        if (sink == 42) {
            System.out.println();
        }
    }

    private static TypeTab.Type byStr(TypeTab tab, String str) {
        for (TypeTab.Type t : tab.types) {
            if (t.str.equals(str)) {
                return t;
            }
        }
        throw new IllegalArgumentException(str);
    }

    /** Static field offset, as compiled (non-reflect) code would use it: path of field names. */
    private static long offsetOf(TypeTab tab, TypeTab.Type t, String... path) {
        long off = 0;
        for (String name : path) {
            TypeTab.Field hit = null;
            for (TypeTab.Field f : t.fields) {
                if (f.name.equals(name)) {
                    hit = f;
                }
            }
            off += hit.offset;
            t = tab.types.get(hit.type);
        }
        return off;
    }

    /** The same sample values imgcheck builds in Go; read back through the transliterated reflect.Value code. */
    private static void values(TypeTab tab, Images images, Abi abi, List<String> out) {
        Reflect rf = new Reflect(abi);

        TypeTab.Type pairT = byStr(tab, "fixture.Pair");
        Flat pair = new Flat(new Layout(tab, pairT));
        pair.setString(offsetOf(tab, pairT, "K"), "key");
        pair.storeBits(offsetOf(tab, pairT, "V"), 4, -7);
        pair.storeBits(offsetOf(tab, pairT, "W"), 1, 200);
        Value v = rf.valueOf(new Iface(images.desc[pairT.id], pair));
        out.add("v Pair f0.string " + hx(rf.stringOf(rf.field(v, 0))));
        out.add("v Pair f0.len " + rf.lenOf(rf.field(v, 0)));
        out.add("v Pair f1.int " + rf.intOf(rf.field(v, 1)));
        out.add("v Pair f2.uint " + Long.toUnsignedString(rf.uintOf(rf.field(v, 2))));

        TypeTab.Type wideT = byStr(tab, "fixture.Wide");
        Flat wide = new Flat(new Layout(tab, wideT));
        wide.storeBits(offsetOf(tab, wideT, "F32"), 4, Float.floatToRawIntBits(1.5f));
        wide.storeBits(offsetOf(tab, wideT, "I16"), 2, -2);
        wide.storeBits(offsetOf(tab, wideT, "U64"), 8, 1L << 63);
        wide.storeBits(offsetOf(tab, wideT, "B"), 1, 1);
        wide.setString(offsetOf(tab, wideT, "S"), "wide");
        v = rf.valueOf(new Iface(images.desc[wideT.id], wide));
        out.add("v Wide f0.float " + rf.floatOf(rf.field(v, 0)));
        out.add("v Wide f1.int " + rf.intOf(rf.field(v, 1)));
        out.add("v Wide f2.uint " + Long.toUnsignedString(rf.uintOf(rf.field(v, 2))));
        out.add("v Wide f3.bool " + rf.boolOf(rf.field(v, 3)));
        out.add("v Wide f4.string " + hx(rf.stringOf(rf.field(v, 4))));

        TypeTab.Type pT = byStr(tab, "fixture.P");
        Flat p = new Flat(new Layout(tab, pT));
        p.storeBits(offsetOf(tab, pT, "X"), 8, 42);
        p.setString(offsetOf(tab, pT, "Y"), "hi");
        p.storeBits(offsetOf(tab, pT, "In", "A"), 1, 1);
        p.storeBits(offsetOf(tab, pT, "In", "B"), 8, Double.doubleToRawLongBits(2.5));
        p.storeBits(offsetOf(tab, pT, "Inner", "A"), 1, 3);
        p.storeBits(offsetOf(tab, pT, "Inner", "B"), 8, Double.doubleToRawLongBits(4.5));
        p.storeBits(offsetOf(tab, pT, "Arr") + 4, 2, 9);
        v = rf.valueOf(new Iface(images.desc[pT.id], p));
        out.add("v P f0.int " + rf.intOf(rf.field(v, 0)));
        out.add("v P f1.string " + hx(rf.stringOf(rf.field(v, 1))));
        out.add("v P f3.f0.int " + rf.intOf(rf.field(rf.field(v, 3), 0)));
        out.add("v P f3.f1.float " + rf.floatOf(rf.field(rf.field(v, 3), 1)));
        out.add("v P f12.f1.float " + rf.floatOf(rf.field(rf.field(v, 12), 1)));
        out.add("v P f9.i2.uint " + Long.toUnsignedString(rf.uintOf(rf.index(rf.field(v, 9), 2))));

        // reflect.ValueOf(&p).Elem().Field(0).SetInt(7); ...Field(3).Field(1).SetFloat(9.25); then read p natively.
        Value pv = rf.elem(rf.valueOf(new Iface(images.desc[pT.ptrTo], new Addr(p, 0))));
        rf.setInt(rf.field(pv, 0), 7);
        rf.setFloat(rf.field(rf.field(pv, 3), 1), 9.25);
        out.add("v P set.f0 " + p.loadBits(offsetOf(tab, pT, "X"), 8));
        out.add("v P set.f3.f1 " + Double.longBitsToDouble(p.loadBits(offsetOf(tab, pT, "In", "B"), 8)));
        String unexported;
        try {
            rf.setInt(rf.field(pv, 11), 1);
            unexported = "no panic";
        } catch (IllegalStateException e) {
            unexported = "panic";
        }
        out.add("v P set.unexported " + unexported);

        TypeTab.Type myInt = byStr(tab, "fixture.MyInt");
        Flat five = new Flat(new Layout(tab, myInt));
        five.storeBits(0, 8, 5);
        out.add("v MyInt int " + rf.intOf(rf.valueOf(new Iface(images.desc[myInt.id], five))));

        TypeTab.Type strT = byStr(tab, "string");
        Flat s = new Flat(new Layout(tab, strT));
        s.setString(0, "s");
        out.add("v string string " + hx(rf.stringOf(rf.valueOf(new Iface(images.desc[strT.id], s)))));

        // GG-0062: arithmetic that leaves the object traps.
        String trap;
        try {
            rf.intOf(new Value(images.desc[byStr(tab, "int").id], new Addr(p, 0).add(4096), 2 | Reflect.FLAG_INDIR));
            trap = "no trap";
        } catch (Mem.UnsafeTrap e) {
            trap = "trap";
        }
        out.add("v P escape " + trap);
    }
}
