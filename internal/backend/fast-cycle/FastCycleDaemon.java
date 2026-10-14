// FastCycleDaemon.java — persistent JVM for sub-second edit-compile-run cycles.
//
// Protocol (line-based, TCP on localhost):
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
// recompiled classes are picked up and static state doesn't leak.

import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.*;
import javax.tools.*;

public class FastCycleDaemon {
    static final int PORT = 47321;

    public static void main(String[] args) throws Exception {
        // Pre-warm the compiler
        ToolProvider.getSystemJavaCompiler();

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

        String line = in.readLine();
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
            URLClassLoader cl = new URLClassLoader(urls, ClassLoader.getPlatformClassLoader());
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
        } catch (Exception e) {
            e.printStackTrace(ps);
            exitCode = 1;
        } finally {
            System.setOut(origOut);
            System.setErr(origErr);
        }

        String output = buf.toString(StandardCharsets.UTF_8);
        return "EXIT:" + exitCode + "\n" + output.stripTrailing();
    }

    static String doRunTest(String cp, String testClass, String methodName, Map<String, String> props) {
        PrintStream origOut = System.out;
        PrintStream origErr = System.err;
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
            URLClassLoader cl = new URLClassLoader(urls, ClassLoader.getPlatformClassLoader());
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
        } catch (Exception e) {
            e.printStackTrace(ps);
            failed = 1;
        } finally {
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

    // Marker for code that calls System.exit (not currently used, but
    // documents the contract: run targets should return, not exit).
    static class SystemExitException extends RuntimeException {
        final int code;
        SystemExitException(int code) { this.code = code; }
    }
}
