package abispike;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HexFormat;
import java.util.List;
import java.util.Map;

/** The type table typetab emits (the stand-in for GIR TYPES/TYPEDEFS). */
public final class TypeTab {
    public static final class Field {
        public String name;
        public String tag;
        public int type;
        public long offset;
        public boolean embedded;
        public boolean exported;
    }

    public static final class Method {
        public String name;
        public boolean exported;
        public int type;
    }

    public static final class Type {
        public int id;
        public int kind;
        public long size;
        public int align;
        public int fieldAlign;
        public long ptrBytes;
        public byte[] gcMask;
        public String str;
        public String link;
        public boolean named;
        public String name;
        public String pkgPath;
        public boolean comparable;
        public boolean regular;
        public boolean direct;
        public int elem = -1;
        public int key = -1;
        public int slice = -1;
        public int ptrTo = -1;
        public long len;
        public int dir;
        public boolean variadic;
        public int[] in = {};
        public int[] out = {};
        public String structPkg = "";
        public String ifacePkg = "";
        public String uncommonPkg = "";
        public final List<Field> fields = new ArrayList<>();
        public final List<Method> methods = new ArrayList<>();
        public final List<Method> imethods = new ArrayList<>();
    }

    public final List<Type> types = new ArrayList<>();
    public final List<Integer> roots = new ArrayList<>();
    private final Map<String, Long> layout = new HashMap<>();

    /** Byte offset of an internal/abi struct field, e.g. {@code off("Type.Kind_")}. */
    public long off(String structDotField) {
        Long v = layout.get(structDotField);
        if (v == null) {
            throw new IllegalArgumentException("no layout for " + structDotField);
        }
        return v;
    }

    /** Size of an internal/abi struct. */
    public int sizeof(String struct) {
        return (int) off(struct + ".#size");
    }

    public static TypeTab read(Path p) throws IOException {
        TypeTab tab = new TypeTab();
        for (String line : Files.readAllLines(p)) {
            if (line.isBlank()) {
                continue;
            }
            String[] parts = line.split(" ");
            Map<String, String> kv = new HashMap<>();
            for (int i = 1; i < parts.length; i++) {
                int eq = parts[i].indexOf('=');
                kv.put(parts[i].substring(0, eq), parts[i].substring(eq + 1));
            }
            switch (parts[0]) {
                case "type" -> {
                    Type t = new Type();
                    t.id = num(kv, "id");
                    t.kind = num(kv, "kind");
                    t.size = Long.parseLong(kv.get("size"));
                    t.align = num(kv, "align");
                    t.fieldAlign = num(kv, "fieldalign");
                    t.ptrBytes = Long.parseLong(kv.get("ptrbytes"));
                    t.gcMask = HexFormat.of().parseHex(kv.get("gcmask").substring(1));
                    t.str = str(kv, "str");
                    t.link = str(kv, "link");
                    t.named = flag(kv, "named");
                    t.name = str(kv, "name");
                    t.pkgPath = str(kv, "pkgpath");
                    t.comparable = flag(kv, "comparable");
                    t.regular = flag(kv, "regular");
                    t.direct = flag(kv, "direct");
                    t.elem = opt(kv, "elem");
                    t.key = opt(kv, "key");
                    t.slice = opt(kv, "slice");
                    t.ptrTo = opt(kv, "ptrto");
                    t.len = kv.containsKey("len") ? Long.parseLong(kv.get("len")) : 0;
                    t.dir = kv.containsKey("dir") ? num(kv, "dir") : 0;
                    t.variadic = kv.containsKey("variadic") && flag(kv, "variadic");
                    t.in = ids(kv.get("in"));
                    t.out = ids(kv.get("out"));
                    t.structPkg = kv.containsKey("structpkg") ? str(kv, "structpkg") : "";
                    t.ifacePkg = kv.containsKey("ifacepkg") ? str(kv, "ifacepkg") : "";
                    t.uncommonPkg = kv.containsKey("upkg") ? str(kv, "upkg") : t.pkgPath;
                    if (t.id != tab.types.size()) {
                        throw new IOException("type ids out of order at " + t.id);
                    }
                    tab.types.add(t);
                }
                case "field" -> {
                    Field f = new Field();
                    f.name = str(kv, "name");
                    f.tag = str(kv, "tag");
                    f.type = num(kv, "type");
                    f.offset = Long.parseLong(kv.get("offset"));
                    f.embedded = flag(kv, "embedded");
                    f.exported = flag(kv, "exported");
                    tab.types.get(num(kv, "owner")).fields.add(f);
                }
                case "method", "imethod" -> {
                    Method m = new Method();
                    m.name = str(kv, "name");
                    m.exported = flag(kv, "exported");
                    m.type = num(kv, "type");
                    Type owner = tab.types.get(num(kv, "owner"));
                    (parts[0].equals("method") ? owner.methods : owner.imethods).add(m);
                }
                case "root" -> tab.roots.add(num(kv, "type"));
                case "layout" -> tab.layout.put(kv.get("struct") + ".#size", Long.parseLong(kv.get("size")));
                case "lfield" -> tab.layout.put(kv.get("struct") + "." + kv.get("name"), Long.parseLong(kv.get("offset")));
                default -> throw new IOException("unknown record: " + line);
            }
        }
        return tab;
    }

    private static int num(Map<String, String> kv, String k) {
        return Integer.parseInt(kv.get(k));
    }

    private static int opt(Map<String, String> kv, String k) {
        return kv.containsKey(k) ? num(kv, k) : -1;
    }

    private static boolean flag(Map<String, String> kv, String k) {
        return kv.get(k).equals("1");
    }

    private static String str(Map<String, String> kv, String k) {
        return new String(HexFormat.of().parseHex(kv.get(k).substring(1)), StandardCharsets.UTF_8);
    }

    private static int[] ids(String s) {
        if (s == null || s.equals("-")) {
            return new int[0];
        }
        String[] parts = s.split(",");
        int[] out = new int[parts.length];
        for (int i = 0; i < parts.length; i++) {
            out[i] = Integer.parseInt(parts[i]);
        }
        return out;
    }
}
