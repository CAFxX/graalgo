package graalgo.language;

import com.oracle.truffle.api.source.Source;
import graalgo.frontend.NodeFactory;
import graalgo.frontend.WireKindRegistrar;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.ServiceLoader;

/**
 * Builds a v1 program from a frontend/v1 wire document: a {@link NodeFactory}
 * with every {@link WireKindRegistrar} on the class path (interop's Phase-3
 * wire kinds), the build, then each registrar's post-build installs — the
 * same sequence the launchers ran inline before TR-1.
 */
final class GoProgramBuilder {
    private GoProgramBuilder() {}

    static NodeFactory.Program build(String json, List<Path> sourceFiles) {
        NodeFactory factory = new NodeFactory(sources(sourceFiles));
        List<WireKindRegistrar> registrars = new ArrayList<>();
        for (WireKindRegistrar registrar : ServiceLoader.load(WireKindRegistrar.class)) {
            registrars.add(registrar);
            registrar.registerAll(factory);
        }
        NodeFactory.Program program = factory.buildProgram(json);
        for (WireKindRegistrar registrar : registrars) {
            registrar.afterBuild(factory, program);
        }
        return program;
    }

    /**
     * One Truffle {@code Source} per input file; the wire document's position
     * "file" index selects among them. Without files (a run from a stored
     * wire document) one empty placeholder: sources back positions only, and
     * {@code sectionFor} clamps to it.
     */
    private static Source[] sources(List<Path> files) {
        if (files.isEmpty()) {
            return new Source[] {Source.newBuilder(GoLanguage.ID, "", "wire").build()};
        }
        Source[] out = new Source[files.size()];
        for (int i = 0; i < out.length; i++) {
            Path p = files.get(i);
            String text;
            try {
                text = Files.readString(p, StandardCharsets.UTF_8);
            } catch (IOException e) {
                throw new GoParseException("cannot read input file: " + p);
            }
            out[i] = Source.newBuilder(GoLanguage.ID, text, p.getFileName().toString()).build();
        }
        return out;
    }
}
