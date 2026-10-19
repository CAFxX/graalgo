package graalgo.frontend;

import com.oracle.truffle.api.source.Source;
import graalgo.runtime.PanicException;
import java.util.List;
import java.util.Map;

/**
 * ADR-0007 addendum D5/D9: the backend consumes the frontend/v1
 * {@code "asm"} section structurally -- parse + integrity assertion +
 * enriched {@code unimplemented} panic + program metadata retention.
 *
 * <p>Each test is demonstrably non-tautological: the negative cases
 * assert {@link FrontendException} (a neutered assertion builds the
 * program silently and the test fails), and the positive case asserts
 * the retained metadata field-for-field.
 */
public final class AsmDispositionTest {

    private static final String POS =
            "\"pos\":{\"file\":0,\"line\":1,\"col\":1,\"off\":0,\"len\":1}";

    private static String doc(String asmSection) {
        return "{"
                + "\"frontend\":\"v1\","
                + "\"generator\":{\"tool\":\"gofrontend\",\"version\":\"0.1.0\",\"goroot\":\"go1.27.1\"},"
                + "\"package\":{\"name\":\"main\",\"files\":[\"x.go\"],\"lang\":\"go1.27\"},"
                + "\"types\":[],\"objects\":[],"
                + "\"funcs\":[{"
                + "\"kind\":\"funcdecl\"," + POS + ","
                + "\"fields\":{"
                + "\"name\":\"main\",\"obj\":0,\"params\":[],"
                + "\"body\":{\"kind\":\"block\"," + POS + ",\"fields\":{\"stmts\":[]}}"
                + "}"
                + "}]"
                + (asmSection == null ? "" : ",\"asm\":" + asmSection)
                + "}";
    }

    private static String oneFile(String file, String category, String replacement, String reason) {
        return "{\"package\":\"sync/atomic\",\"files\":[{"
                + "\"file\":" + quote(file) + ","
                + "\"category\":" + quote(category) + ","
                + "\"replacement\":" + quote(replacement) + ","
                + "\"reason\":" + quote(reason)
                + "}]}";
    }

    private static String quote(String s) {
        return "\"" + s.replace("\\", "\\\\").replace("\"", "\\\"") + "\"";
    }

    private static NodeFactory build(String asmSection) {
        Source source = Source.newBuilder("go", "package main", "x.go").build();
        NodeFactory factory = new NodeFactory(source);
        factory.buildProgram(doc(asmSection));
        return factory;
    }

    public void testAsmSectionParsedAndRetained() {
        NodeFactory factory = build("[" + oneFile("asm.s", "intrinsify",
                "graalgo.runtime.Atomics", "public atomic ops") + "]");
        Map<String, List<NodeFactory.AsmDisposition>> got = factory.asmDispositions();
        List<NodeFactory.AsmDisposition> ds = got.get("sync/atomic");
        if (ds == null || ds.size() != 1) {
            throw new AssertionError("expected one disposition for sync/atomic, got " + got);
        }
        NodeFactory.AsmDisposition d = ds.get(0);
        if (!"asm.s".equals(d.file()) || !"intrinsify".equals(d.category())
                || !"graalgo.runtime.Atomics".equals(d.replacement())
                || !"public atomic ops".equals(d.reason())) {
            throw new AssertionError("disposition mismatch: " + d);
        }
    }

    public void testAsmSectionAbsentYieldsEmpty() {
        NodeFactory factory = build(null);
        if (!factory.asmDispositions().isEmpty()) {
            throw new AssertionError("absent asm section must yield empty metadata, got "
                    + factory.asmDispositions());
        }
    }

    public void testEmptyReplacementFailsProgramBuild() {
        Source source = Source.newBuilder("go", "package main", "x.go").build();
        try {
            new NodeFactory(source).buildProgram(
                    doc("[" + oneFile("asm.s", "intrinsify", "", "dangling") + "]"));
        } catch (FrontendException e) {
            if (!e.getMessage().contains("asm")) {
                throw new AssertionError("expected 'asm' in FrontendException, got <"
                        + e.getMessage() + ">");
            }
            return; // expected: program-build-time integrity assertion
        }
        throw new AssertionError("dangling replacement pointer must fail the program build");
    }

    public void testUnsupportedCategoryFailsProgramBuild() {
        Source source = Source.newBuilder("go", "package main", "x.go").build();
        try {
            new NodeFactory(source).buildProgram(
                    doc("[" + oneFile("asm.s", "unsupported", "", "no strategy") + "]"));
        } catch (FrontendException e) {
            if (!e.getMessage().contains("asm")) {
                throw new AssertionError("expected 'asm' in FrontendException, got <"
                        + e.getMessage() + ">");
            }
            return; // expected: the wire never carries non-replaced categories
        }
        throw new AssertionError("unsupported category on the wire must fail the program build");
    }

    public void testEnrichedUnimplementedMessage() {
        UnimplementedRootNode root =
                new UnimplementedRootNode("Add", "sync/atomic", "asm.s: intrinsify: fast atomics");
        try {
            root.execute(null);
        } catch (PanicException e) {
            String msg = String.valueOf(e.goValue());
            String want = "unimplemented: sync/atomic.Add:"
                    + " assembly dispositions for this package: [asm.s: intrinsify: fast atomics]";
            if (!msg.equals(want)) {
                throw new AssertionError("enriched message mismatch:\n got: " + msg
                        + "\nwant: " + want);
            }
            return; // expected: the stub panics with the enriched message
        }
        throw new AssertionError("UnimplementedRootNode must panic when called");
    }

    public void testLegacyUnimplementedMessageWithoutDispositions() {
        UnimplementedRootNode root = new UnimplementedRootNode("Add", "sync/atomic");
        try {
            root.execute(null);
        } catch (PanicException e) {
            String msg = String.valueOf(e.goValue());
            String want = "unimplemented: sync/atomic.Add (assembly function not implemented in graalgo)";
            if (!msg.equals(want)) {
                throw new AssertionError("legacy message mismatch:\n got: " + msg
                        + "\nwant: " + want);
            }
            return; // expected: unchanged generic message when no dispositions carried
        }
        throw new AssertionError("UnimplementedRootNode must panic when called");
    }
}
