package graalgo.frontend.v2;

import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.List;
import java.util.Map;

/**
 * Demo: decodes a v2 binary Document and prints its structure.
 *
 * <p>Usage: java graalgo.frontend.v2.V2DecodeDemo <file.bin>
 *
 * <p>This proves the Go→Java binary interface works end-to-end:
 * the Go frontend encodes the Document, Java decodes it.
 */
public final class V2DecodeDemo {
    public static void main(String[] args) throws Exception {
        if (args.length != 1) {
            System.err.println("usage: V2DecodeDemo <file.bin>");
            System.exit(1);
        }
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        System.out.println("Read " + data.length + " bytes");

        Map<String, Object> doc = BinaryDecoder.decodeDocument(data);

        System.out.println("frontend: " + doc.get("frontend"));
        @SuppressWarnings("unchecked")
        Map<String, Object> pkg = (Map<String, Object>) doc.get("package");
        System.out.println("package: " + pkg.get("name"));

        @SuppressWarnings("unchecked")
        List<Object> funcs = (List<Object>) doc.get("funcs");
        System.out.println("funcs: " + funcs.size());
        for (Object f : funcs) {
            @SuppressWarnings("unchecked")
            Map<String, Object> fn = (Map<String, Object>) f;
            @SuppressWarnings("unchecked")
            Map<String, Object> fields = (Map<String, Object>) fn.get("fields");
            System.out.println("  - " + fields.get("name") +
                " (kind=" + fn.get("kind") + ")");
        }

        @SuppressWarnings("unchecked")
        List<Object> types = (List<Object>) doc.get("types");
        @SuppressWarnings("unchecked")
        List<Object> objects = (List<Object>) doc.get("objects");
        System.out.println("types: " + types.size() + ", objects: " + objects.size());
        System.out.println("V2 binary interface: OK");
    }
}
