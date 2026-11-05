// FastCycleDaemon.java — persistent JVM for sub-second edit-compile-run cycles.
//
// Protocol (line-based, TCP on localhost). The first line of every
// connection must be `AUTH <token>`, where <token> is the per-start token
// from ~/.fastcycle.port; anything else is answered AUTH:FAIL and the
// connection is closed. The command follows on the next line:
//   PING
//     -> PONG
//   COMPILE <outdir> <classpath> <processorpath> <file1> <file2> ...
//     -> OK | FAIL\n<diagnostics>
//   RUN <classpath> <mainclass> <arg1> ...
//     -> EXIT:<code>\n<output>
//   STOP
//     -> BYE (then exits)
//
// COMPILE uses the in-process JavaCompiler API (no JVM startup).
// RUN loads the main class in a fresh URLClassLoader per invocation so
// recompiled classes are picked up and static state doesn't leak. The
// loader is closed when the run finishes (try-with-resources) and the
// calling thread's context classloader is restored, so a long-lived
// daemon does not retain loaders, their classes, or their open jar
// handles across runs.
//
// TRUST BOUNDARY — dev-only tool, no sandbox:
//   The daemon binds 127.0.0.1:47321 and COMPILE/RUN/RUNTEST compile and
//   execute arbitrary code in the daemon JVM (a RUN target may call
//   System.exit and kill the daemon). Loopback-only binding keeps remote
//   hosts out, but any local process can connect, so: run it only on a
//   single-developer workstation, never on a shared/multi-user machine,
//   and never expose the port. The per-start token (random UUID, written
//   to ~/.fastcycle.port with mode 0600) keeps a casual local prober out —
//   it is NOT a security boundary against a local attacker who can read
//   your home directory.

import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.*;
import javax.tools.*;

public class FastCycleDaemon {
    static final int PORT = 47321;

    // Per-start auth token, set by main() before the accept loop; every
    // connection must present it (see the header trust boundary).
    static String authToken;

    public static void main(String[] args) throws Exception {
        // Pre-warm the compiler
        ToolProvider.getSystemJavaCompiler();

        ServerSocket server = new ServerSocket();
        server.bind(new InetSocketAddress("127.0.0.1", PORT));
        System.out.println("FastCycleDaemon listening on 127.0.0.1:" + PORT);
        System.out.flush();

        // Per-start token: clients must send `AUTH <token>` first.
        authToken = UUID.randomUUID().toString();

        // Write the port file so clients can discover us (port + token,
        // owner-readable only: the token is the only thing standing
        // between a casual local prober and arbitrary code execution).
        // NOTE: resolve the home directory from $HOME, not user.home:
        // Java's user.home is the passwd entry (/root when run as root),
        // while clients (fast-cycle) read $HOME — the two disagree here,
        // and the token must live where the client looks for it.
        String home = System.getenv("HOME");
        if (home == null || home.isEmpty()) home = System.getProperty("user.home");
        Path portFile = Paths.get(home, ".fastcycle.port");
        // The port file is load-bearing (clients read the token from it),
        // so ride out a brief write failure (e.g. a flapping disk quota)
        // instead of failing the whole daemon start on the first blip.
        // After the retries the failure propagates as before.
        IOException portFileError = null;
        for (int attempt = 0; attempt < 5; attempt++) {
            try {
                Files.writeString(portFile, PORT + " " + authToken);
                portFileError = null;
                break;
            } catch (IOException e) {
                portFileError = e;
                try {
                    Thread.sleep(1000);
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                    throw e;
                }
            }
        }
        if (portFileError != null) throw portFileError;
        try {
            Files.setPosixFilePermissions(portFile,
                java.nio.file.attribute.PosixFilePermissions.fromString("rw-------"));
        } catch (UnsupportedOperationException | IOException ignored) {
            // Non-POSIX filesystem: the file is still written; the
            // loopback binding is the remaining boundary.
        }

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

        String line = in.readLine();
        if (line == null) return;

        // First line of every connection is the per-start token (see the
        // header trust boundary). Fail closed: no token, no command.
        if (authToken == null || !line.equals("AUTH " + authToken)) {
            out.println("AUTH:FAIL");
            return;
        }
        line = in.readLine();
        if (line == null) return;

        // Simple space-split; paths with spaces are not supported (matches Makefile assumptions)
        String[] parts = line.split(" ");
        String cmd = parts[0];

        switch (cmd) {
            case "PING" -> out.println("PONG");
            case "STOP" -> {
                out.println("BYE");
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
                // Prefers a run(String[]) method returning int (no System.exit);
                // falls back to main(String[]) (may kill the daemon via System.exit).
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
                // Invokes test methods directly (no main, no System.exit).
                // If method is given, runs just that method; else all test* methods.
                // Properties after -- are set as system properties for the run.
                if (parts.length < 3) {
                    out.println("EXIT:2\nusage: RUNTEST <classpath> <testclass> [method] [-- <prop>=<val>...]");
                    return;
                }
                String cp = parts[1];
                String testClass = parts[2];
                String method = null;
                Map<String, String> props = new HashMap<>();
                boolean inProps = false;
                for (int i = 3; i < parts.length; i++) {
                    if (parts[i].equals("--")) {
                        inProps = true;
                        continue;
                    }
                    if (inProps) {
                        int eq = parts[i].indexOf('=');
                        if (eq > 0) {
                            props.put(parts[i].substring(0, eq), parts[i].substring(eq + 1));
                        }
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

    static String doRun(String cp, String mainClass, String[] args) {
        // Capture stdout/stderr
        PrintStream origOut = System.out;
        PrintStream origErr = System.err;
        ClassLoader origTccl = Thread.currentThread().getContextClassLoader();
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        PrintStream ps = new PrintStream(buf, true, StandardCharsets.UTF_8);

        int exitCode = 0;
        try {
            System.setOut(ps);
            System.setErr(ps);

            String[] cpEntries = cp.split(":");
            java.net.URL[] urls = new java.net.URL[cpEntries.length];
            for (int i = 0; i < cpEntries.length; i++) {
                urls[i] = Paths.get(cpEntries[i]).toUri().toURL();
            }
            // The loader is closed when the run finishes (try-with-resources)
            // so its jar handles are released deterministically instead of
            // lingering until GC, and the context loader is restored in the
            // finally below so the next run does not resolve classes
            // against this run's stale loader. (A close() failure after a
            // successful run is reported as EXIT:1 by the catch below —
            // the resources did not come back, so the run did not fully
            // succeed.)
            try (URLClassLoader cl = new URLClassLoader(urls, ClassLoader.getPlatformClassLoader())) {
                Thread.currentThread().setContextClassLoader(cl);
                Class<?> cls = Class.forName(mainClass, true, cl);

                // Prefer run(String[]) returning int (no System.exit) over main.
                try {
                    java.lang.reflect.Method run = cls.getDeclaredMethod("run", String[].class);
                    run.setAccessible(true);
                    if (run.getReturnType() == int.class) {
                        exitCode = (int) run.invoke(null, (Object) args);
                    } else {
                        run.invoke(null, (Object) args);
                    }
                } catch (NoSuchMethodException e) {
                    // Fall back to main (may call System.exit and kill the daemon).
                    java.lang.reflect.Method main = cls.getMethod("main", String[].class);
                    try {
                        main.invoke(null, (Object) args);
                    } catch (java.lang.reflect.InvocationTargetException ite) {
                        Throwable cause = ite.getCause();
                        cause.printStackTrace(ps);
                        exitCode = 1;
                    }
                }
            }
        } catch (Exception e) {
            e.printStackTrace(ps);
            exitCode = 1;
        } finally {
            Thread.currentThread().setContextClassLoader(origTccl);
            System.setOut(origOut);
            System.setErr(origErr);
        }

        String output = buf.toString(StandardCharsets.UTF_8);
        return "EXIT:" + exitCode + "\n" + output.stripTrailing();
    }

    static String doRunTest(String cp, String testClass, String methodName, Map<String, String> props) {
        PrintStream origOut = System.out;
        PrintStream origErr = System.err;
        ClassLoader origTccl = Thread.currentThread().getContextClassLoader();
        ByteArrayOutputStream buf = new ByteArrayOutputStream();
        PrintStream ps = new PrintStream(buf, true, StandardCharsets.UTF_8);

        // Save and set system properties
        Map<String, String> savedProps = new HashMap<>();
        for (Map.Entry<String, String> e : props.entrySet()) {
            savedProps.put(e.getKey(), System.getProperty(e.getKey()));
            System.setProperty(e.getKey(), e.getValue());
        }

        int run = 0;
        int failed = 0;
        try {
            System.setOut(ps);
            System.setErr(ps);

            String[] cpEntries = cp.split(":");
            java.net.URL[] urls = new java.net.URL[cpEntries.length];
            for (int i = 0; i < cpEntries.length; i++) {
                urls[i] = Paths.get(cpEntries[i]).toUri().toURL();
            }
            // Same lifecycle as doRun: close the loader when the run
            // finishes and restore the context loader (see doRun).
            try (URLClassLoader cl = new URLClassLoader(urls, ClassLoader.getPlatformClassLoader())) {
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
                        ps.println("PASS " + cls.getSimpleName() + "." + m.getName());
                    } catch (Exception e) {
                        failed++;
                        Throwable cause = e.getCause() != null ? e.getCause() : e;
                        ps.println("FAIL " + cls.getSimpleName() + "." + m.getName() + ": " + cause);
                    }
                }
                ps.println(run + " tests, " + failed + " failures");
            }
        } catch (Exception e) {
            e.printStackTrace(ps);
            failed = 1;
        } finally {
            Thread.currentThread().setContextClassLoader(origTccl);
            System.setOut(origOut);
            System.setErr(origErr);
            // Restore system properties
            for (Map.Entry<String, String> e : savedProps.entrySet()) {
                if (e.getValue() == null) {
                    System.clearProperty(e.getKey());
                } else {
                    System.setProperty(e.getKey(), e.getValue());
                }
            }
        }

        String output = buf.toString(StandardCharsets.UTF_8);
        return "EXIT:" + (failed > 0 ? 1 : 0) + "\n" + output.stripTrailing();
    }

}
