// FastCycleDaemon.java — persistent JVM for sub-second edit-compile-run cycles.
//
// Protocol (line-based, TCP on localhost):
//   AUTH <token>
//   PING
//     -> PONG
//   COMPILE <outdir> <classpath> <processorpath> <file1> <file2> ...
//     -> OK | FAIL\n<diagnostics>
//   RUN <classpath> <mainclass> <arg1> ...
//     -> EXIT:<code>\n<output>
//   STOP
//     -> BYE (then exits)
//
// The AUTH line is mandatory on every connection and precedes the
// command line. The token is generated per daemon start, written to
// ~/.fastcycle.token with owner-only (0600) permissions, and read by
// the fast-cycle client. Loopback TCP alone is NOT an authorization
// boundary — any local uid can connect to 127.0.0.1 — so the token is
// what makes the daemon per-user: only a process running as the same
// user (able to read the 0600 token file) can ask it to compile or run
// code. Unauthenticated connections get AUTH_REQUIRED and nothing else.
//
// COMPILE uses the in-process JavaCompiler API (no JVM startup).
// Annotation processors on the processorpath run in-process by design,
// so COMPILE is trusted same-user input only, like any local build.
//
// RUN/RUNTEST execute the guest in a fresh SUBPROCESS (never in the
// daemon JVM). That closes all three in-process guest hazards at once:
//   - a guest System.exit kills only the subprocess (its exit code is
//     reported as EXIT:<code>, exactly like a real process run);
//   - guest stdout/stderr are the subprocess's streams, captured by
//     the daemon — no global System.out/err swapping, which was
//     racy across daemon threads and never restored on System.exit;
//   - guest classloaders live and die with the subprocess (the test
//     runner closes its URLClassLoader explicitly), so no loader or
//     static state leaks into the daemon between runs.

import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.nio.file.attribute.PosixFilePermission;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.*;
import javax.tools.*;

public class FastCycleDaemon {
    static final int PORT = 47321;

    /** Per-start secret, hex-encoded (256 bits). */
    private static final String TOKEN = generateToken();
    private static final Path TOKEN_FILE =
            Paths.get(System.getProperty("user.home"), ".fastcycle.token");

    private static String generateToken() {
        byte[] raw = new byte[32];
        new SecureRandom().nextBytes(raw);
        StringBuilder sb = new StringBuilder(raw.length * 2);
        for (byte b : raw) {
            sb.append(Character.forDigit((b >> 4) & 0xF, 16));
            sb.append(Character.forDigit(b & 0xF, 16));
        }
        return sb.toString();
    }

    public static void main(String[] args) throws Exception {
        // Pre-warm the compiler
        ToolProvider.getSystemJavaCompiler();

        // Publish the token owner-only BEFORE accepting connections:
        // a connection that arrives before the token file exists cannot
        // authenticate, and a world-readable token would be no token.
        Files.writeString(TOKEN_FILE, TOKEN + "\n", StandardCharsets.UTF_8);
        try {
            Files.setPosixFilePermissions(TOKEN_FILE,
                    EnumSet.of(PosixFilePermission.OWNER_READ,
                            PosixFilePermission.OWNER_WRITE));
        } catch (UnsupportedOperationException ignored) {
            // Non-POSIX filesystem: the token file lives in $HOME, which
            // is per-user by construction; permissions are best effort.
        }

        ServerSocket server = new ServerSocket();
        server.bind(new InetSocketAddress("127.0.0.1", PORT));
        System.out.println("FastCycleDaemon listening on 127.0.0.1:" + PORT);
        System.out.flush();

        // Write the port file so clients can discover us
        Path portFile = Paths.get(System.getProperty("user.home"), ".fastcycle.port");
        Files.writeString(portFile, String.valueOf(PORT));

        while (true) {
            try (Socket client = server.accept()) {
                handle(client);
            } catch (Exception e) {
                e.printStackTrace();
            }
        }
    }

    static void handle(Socket client) throws Exception {
        BufferedReader in = new BufferedReader(
            new InputStreamReader(client.getInputStream(), StandardCharsets.UTF_8));
        PrintWriter out = new PrintWriter(
            new OutputStreamWriter(client.getOutputStream(), StandardCharsets.UTF_8), true);

        // First line MUST be the auth token. Constant-time compare so
        // the token cannot be guessed byte-by-byte from timing.
        String auth = in.readLine();
        if (auth == null || !auth.startsWith("AUTH ")) {
            out.println("AUTH_REQUIRED");
            return;
        }
        String presented = auth.substring("AUTH ".length()).trim();
        if (!MessageDigest.isEqual(
                presented.getBytes(StandardCharsets.UTF_8),
                TOKEN.getBytes(StandardCharsets.UTF_8))) {
            out.println("AUTH_REQUIRED");
            return;
        }

        String line = in.readLine();
        if (line == null) return;

        // Simple space-split; paths with spaces are not supported (matches Makefile assumptions)
        String[] parts = line.split(" ");
        String cmd = parts[0];

        switch (cmd) {
            case "PING" -> out.println("PONG");
            case "STOP" -> {
                out.println("BYE");
                out.flush();
                try {
                    Files.deleteIfExists(TOKEN_FILE);
                } catch (IOException ignored) {
                    // Best effort; the next daemon start overwrites it.
                }
                System.exit(0);
            }
            case "COMPILE" -> {
                // COMPILE <outdir> <classpath> <processorpath> <files...>
                if (parts.length < 5) {
                    out.println("FAIL\nusage: COMPILE <outdir> <classpath> <processorpath> <files...>");
                    return;
                }
                String outdir = parts[1];
                String cp = parts[2];
                String pp = parts[3];
                List<String> files = Arrays.asList(parts).subList(4, parts.length);
                out.println(doCompile(outdir, cp, pp, files));
            }
            case "RUN" -> {
                // RUN <classpath> <mainclass> <args...>
                // Runs main(String[]) in a subprocess: its System.exit
                // is the subprocess's exit code, never the daemon's.
                if (parts.length < 3) {
                    out.println("EXIT:2\nusage: RUN <classpath> <mainclass> [args...]");
                    return;
                }
                String cp = parts[1];
                String mainClass = parts[2];
                String[] runArgs = Arrays.asList(parts).subList(3, parts.length)
                    .toArray(new String[0]);
                out.println(doRun(cp, mainClass, runArgs));
            }
            case "RUNTEST" -> {
                // RUNTEST <classpath> <testclass> [method] [-- <prop>=<val> ...]
                // Runs the test methods in a subprocess via TestRunner
                // (below): no in-process reflection, no System.exit risk.
                if (parts.length < 3) {
                    out.println("EXIT:2\nusage: RUNTEST <classpath> <testclass> [method] [-- <prop>=<val>...]");
                    return;
                }
                String cp = parts[1];
                String testClass = parts[2];
                String method = null;
                List<String> props = new ArrayList<>();
                boolean inProps = false;
                for (int i = 3; i < parts.length; i++) {
                    if (parts[i].equals("--")) {
                        inProps = true;
                        continue;
                    }
                    if (inProps) {
                        props.add(parts[i]);
                    } else if (method == null) {
                        method = parts[i];
                    }
                }
                out.println(doRunTest(cp, testClass, method, props));
            }
            default -> out.println("UNKNOWN:" + cmd);
        }
    }

    static String doCompile(String outdir, String cp, String pp, List<String> files) {
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        if (compiler == null) {
            return "FAIL\nNo system Java compiler (not a JDK?)";
        }

        DiagnosticCollector<JavaFileObject> diagnostics = new DiagnosticCollector<>();
        try (StandardJavaFileManager fm = compiler.getStandardFileManager(diagnostics, null, null)) {
            List<String> options = new ArrayList<>(List.of(
                "-d", outdir,
                "-cp", cp,
                "-processorpath", pp,
                "-Xlint:deprecation", "-Werror"
            ));
            Iterable<? extends JavaFileObject> units =
                fm.getJavaFileObjectsFromStrings(files);
            JavaCompiler.CompilationTask task = compiler.getTask(
                null, fm, diagnostics, options, null, units);
            boolean ok = task.call();
            if (ok) {
                return "OK";
            } else {
                StringBuilder sb = new StringBuilder("FAIL\n");
                for (Diagnostic<? extends JavaFileObject> d : diagnostics.getDiagnostics()) {
                    sb.append(d.getKind()).append(": ");
                    if (d.getSource() != null) {
                        sb.append(d.getSource().getName());
                    }
                    sb.append(":").append(d.getLineNumber()).append(": ");
                    sb.append(d.getMessage(null)).append("\n");
                }
                return sb.toString().stripTrailing();
            }
        } catch (IOException e) {
            return "FAIL\n" + e.getMessage();
        }
    }

    /** The java binary of the JVM running the daemon. */
    static String javaBin() {
        return Paths.get(System.getProperty("java.home"), "bin", "java").toString();
    }

    /**
     * Runs a command as a subprocess, merging stderr into stdout, and
     * returns the wire format {@code EXIT:<code>\n<output>}. The
     * subprocess inherits the daemon's environment (e.g.
     * GRAALGO_GOFRONTEND) and stdin (the daemon is started with
     * stdin from /dev/null, so guest stdin reads EOF, as before).
     */
    static String runSubprocess(List<String> cmd) {
        try {
            ProcessBuilder pb = new ProcessBuilder(cmd);
            pb.redirectErrorStream(true);
            pb.redirectInput(ProcessBuilder.Redirect.INHERIT);
            Process p = pb.start();
            String output;
            try (InputStream in = p.getInputStream()) {
                output = new String(in.readAllBytes(), StandardCharsets.UTF_8);
            }
            int code = p.waitFor();
            return "EXIT:" + code + "\n" + output.stripTrailing();
        } catch (Exception e) {
            StringWriter sw = new StringWriter();
            e.printStackTrace(new PrintWriter(sw));
            return "EXIT:1\n" + sw.toString().stripTrailing();
        }
    }

    static String doRun(String cp, String mainClass, String[] args) {
        List<String> cmd = new ArrayList<>(List.of(
                javaBin(), "-Xss32m", "--enable-native-access=ALL-UNNAMED",
                "-cp", cp, mainClass));
        cmd.addAll(Arrays.asList(args));
        return runSubprocess(cmd);
    }

    static String doRunTest(String cp, String testClass, String methodName,
            List<String> props) {
        // The runner class lives in the daemon's own classpath; the
        // guest classpath is appended after it (test classes first in
        // the guest cp, as the client sends it).
        String fullCp = System.getProperty("java.class.path") + File.pathSeparator + cp;
        List<String> cmd = new ArrayList<>(List.of(
                javaBin(), "-Xss32m", "--enable-native-access=ALL-UNNAMED",
                "-cp", fullCp, "FastCycleDaemon$TestRunner",
                testClass, methodName == null ? "" : methodName, cp));
        cmd.addAll(props);
        return runSubprocess(cmd);
    }

    /**
     * Test runner executed in the guest subprocess (see doRunTest).
     * Args: testClass, method ("" = all test* methods), guest
     * classpath, then prop=value pairs applied as system properties
     * for the run. Loads the test class in a fresh URLClassLoader
     * over the guest classpath (never the runner's own system
     * loader, so recompiled test classes are picked up), which is
     * closed before exiting; the exit code is 0 iff no test failed.
     */
    public static class TestRunner {
        public static void main(String[] args) {
            if (args.length < 3) {
                System.err.println("usage: TestRunner <testclass> <method> <guestcp> [prop=val ...]");
                System.exit(2);
            }
            String testClass = args[0];
            String methodName = args[1].isEmpty() ? null : args[1];
            String guestCp = args[2];
            for (int i = 3; i < args.length; i++) {
                int eq = args[i].indexOf('=');
                if (eq > 0) {
                    System.setProperty(args[i].substring(0, eq), args[i].substring(eq + 1));
                }
            }
            int run = 0;
            int failed = 0;
            URLClassLoader cl = null;
            try {
                String[] entries = guestCp.split(":");
                java.net.URL[] urls = new java.net.URL[entries.length];
                for (int i = 0; i < entries.length; i++) {
                    urls[i] = Paths.get(entries[i]).toUri().toURL();
                }
                cl = new URLClassLoader(urls, ClassLoader.getPlatformClassLoader());
                Thread.currentThread().setContextClassLoader(cl);
                Class<?> cls = Class.forName(testClass, true, cl);
                List<java.lang.reflect.Method> methods = new ArrayList<>();
                for (java.lang.reflect.Method m : cls.getMethods()) {
                    if (methodName != null) {
                        if (m.getName().equals(methodName) && m.getParameterCount() == 0) {
                            methods.add(m);
                        }
                    } else if (m.getName().startsWith("test") && m.getParameterCount() == 0
                            && java.lang.reflect.Modifier.isPublic(m.getModifiers())) {
                        methods.add(m);
                    }
                }
                for (java.lang.reflect.Method m : methods) {
                    run++;
                    try {
                        Object instance = cls.getDeclaredConstructor().newInstance();
                        m.invoke(instance);
                        System.out.println("PASS " + cls.getSimpleName() + "." + m.getName());
                    } catch (Exception e) {
                        failed++;
                        Throwable cause = e.getCause() != null ? e.getCause() : e;
                        System.out.println("FAIL " + cls.getSimpleName() + "." + m.getName() + ": " + cause);
                    }
                }
                System.out.println(run + " tests, " + failed + " failures");
            } catch (Exception e) {
                e.printStackTrace(System.out);
                failed = 1;
            } finally {
                if (cl != null) {
                    try {
                        cl.close();
                    } catch (IOException ignored) {
                        // Best effort; the subprocess exits anyway.
                    }
                }
            }
            System.exit(failed > 0 ? 1 : 0);
        }
    }

    // Marker for code that calls System.exit (not currently used, but
    // documents the contract: run targets should return, not exit —
    // and since RUN now uses a subprocess, an exit is harmless anyway).
    static class SystemExitException extends RuntimeException {
        final int code;
        SystemExitException(int code) { this.code = code; }
    }
}
