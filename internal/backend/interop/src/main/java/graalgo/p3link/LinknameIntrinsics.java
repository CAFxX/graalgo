package graalgo.p3link;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Map;

import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTables;
import graalgo.p3os.GoOs;
import graalgo.p3net.GoHttp;
import graalgo.p3net.GoNet;
import graalgo.p3net.GoNetpoll;
import graalgo.p3tls.TlsRuntime;
import graalgo.p3x509.GoX509;
import graalgo.runtime.Intrinsics;
import graalgo.runtime.Linknames;
import graalgo.runtime.Timers;
import graalgo.values.GoFunc;
import graalgo.values.GoString;

/**
 * Phase-3 linkname installation (P3-M4, WS C, ADR-0004 D1/D3).
 *
 * <p>Both directions, by symbol name (SPEC §2.8):
 *
 * <ul>
 *   <li><b>Go → intrinsic.</b> The M4 gc-symbol table: gc symbol names
 *       from {@code //go:linkname} directives map to the intrinsic
 *       implementations, so identical Go source links against gc's
 *       runtime under gc and graalgo's intrinsics under graalgo:
 *       <ul>
 *         <li>{@code runtime.fastrand} → {@link Intrinsics#fastrand}
 *         <li>{@code runtime.fastrandn} → {@link Intrinsics#fastrandn}
 *         <li>{@code runtime.nanotime} → {@link Timers#nanotime}
 *       </ul>
 *       Arguments arrive in D5 boxed form; each target adapts explicitly
 *       (documented per target below).
 *   <li><b>Intrinsic → Go helper.</b>
 *       {@code graalgo.runtime.Linknames.callGo} invokes the Go function
 *       bound under a qualified Go name (see {@link #bindGoFuncs}).
 * </ul>
 *
 * <p>Also installs the {@link Intrinsics} itabFor provider (the P3-M3
 * itab machinery) and the interface method-name lists, and binds every
 * program function for {@code callGo}.
 *
 * <p>verifies: GG-0020
 */
public final class LinknameIntrinsics {
    private LinknameIntrinsics() {}

    /**
     * Installs everything linkname-related for one program build. Called
     * by the P3 launcher after {@code factory.buildProgram(json)} (and
     * after {@code IfaceMethodTable.install}, which this does not repeat).
     */
    public static void install(Json.Object doc, NodeFactory.Program program) {
        // P3-M4 remediation (P2-2): the static registries must be cleared
        // on every install — the launcher calls install per program build
        // (even empty), and without this a previous program's symbols,
        // intrinsics, and Go bindings would leak into this run.
        Linknames.clear();
        // Go → intrinsic: the M4 gc-symbol table. D5 argument types:
        // fastrandn takes an int32 (Integer).
        Linknames.register("runtime.fastrand", args -> Intrinsics.fastrand(), 0);
        Linknames.register("runtime.fastrandn",
                args -> Intrinsics.fastrandn(((Number) args[0]).intValue()), 1);
        Linknames.register("runtime.nanotime", args -> Timers.nanotime(), 0);
        // Intrinsic → Go helper by symbol name. D5: name arrives as
        // GoString; the single any-typed argument passes through boxed.
        Linknames.register("graalgo.runtime.Linknames.callGo", args -> {
            String name = new String(((GoString) args[0]).bytes(), StandardCharsets.UTF_8);
            Object[] rest = Arrays.copyOfRange(args, 1, args.length);
            return Linknames.callGo(name, rest);
        }, 2);
        // P3-M8 (wave 2): the synthetic os package's intrinsic table
        // (os.md §2.4; the symbol table is frozen — the synthetic
        // syscall worker binds to it). D5 argument types: strings
        // arrive as GoString, int64 as Long, []byte as GoSlice; each
        // GoOs target adapts explicitly (see GoOs's javadoc for the
        // calling conventions: -errno, n+1, one channel op per call).
        registerOsIntrinsics();
        // P3-M9 (RISK-M9-1/RISK-M9-2): the netpoller (internal/poll.*) and
        // socket-trap (graalgo.net.*) intrinsic tables. D5 argument types:
        // integers arrive as Long (int/uintptr are 64-bit), []byte as
        // GoSlice; each GoNet/GoNetpoll target adapts explicitly (see
        // their javadoc for the calling conventions).
        registerNetIntrinsics();
        // P3-M9 (tier-5): x509 certificate parsing (java.security.cert
        // delegation) and TLS (SSLEngine delegation) intrinsic tables.
        // D5 argument types: []byte arrives as GoSlice, strings as
        // GoString, ints/bools as Long; the X509Certificate passes through
        // as Object; each target adapts explicitly.
        registerX509Intrinsics();
        registerTlsIntrinsics();
        // P3-M9 (tier-5): the HTTP client intrinsic (java.net.http
        // delegation) for the synthetic net/http Transport.RoundTrip.
        // D5 argument types: method and URL arrive as GoString, headers
        // as GoSlice of GoString (flat k/v pairs), body as GoSlice of
        // bytes; the target adapts explicitly (see GoHttp's javadoc for
        // the calling convention).
        registerHttpIntrinsics();
        // itabFor → the P3-M3 itab machinery (ADR-0004 D3).
        Intrinsics.setItabProvider(IfaceMethodTable::itabForDescriptors);
        // Interface method-name lists for itabForIntrinsic.
        IfaceMethodTable.installInterfaces(IfaceMethodTables.resolveInterfaces(doc));
        // Go function bindings for callGo.
        bindGoFuncs(doc, program);
    }

    /**
     * Registers the 46 {@code graalgo.os.*} intrinsics implemented by
     * {@link GoOs} (P3-M8 wave 2, os.md §2.4). Arity is the __-helper
     * argument count from synth_os.go.
     */
    private static void registerOsIntrinsics() {
        // Files: open/close/read/write/readAt/writeAt/seek/truncate/sync.
        Linknames.register("graalgo.os.open", args -> GoOs.open(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.close", args -> GoOs.close(args[0]), 1);
        Linknames.register("graalgo.os.read", args -> GoOs.read(args[0], args[1]), 2);
        Linknames.register("graalgo.os.write", args -> GoOs.write(args[0], args[1]), 2);
        Linknames.register("graalgo.os.readAt", args -> GoOs.readAt(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.writeAt",
                args -> GoOs.writeAt(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.seek", args -> GoOs.seek(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.truncate", args -> GoOs.truncate(args[0], args[1]), 2);
        Linknames.register("graalgo.os.sync", args -> GoOs.sync(args[0]), 1);
        // Pipes + stdio + handle kinds.
        Linknames.register("graalgo.os.pipe", args -> GoOs.pipe(), 0);
        Linknames.register("graalgo.os.stdio", args -> GoOs.stdio(args[0]), 1);
        Linknames.register("graalgo.os.handleKind", args -> GoOs.handleKind(args[0]), 1);
        // Stat.
        Linknames.register("graalgo.os.statMode", args -> GoOs.statMode(args[0], args[1]), 2);
        Linknames.register("graalgo.os.statSize", args -> GoOs.statSize(args[0], args[1]), 2);
        Linknames.register("graalgo.os.statMtimeNs",
                args -> GoOs.statMtimeNs(args[0], args[1]), 2);
        Linknames.register("graalgo.os.statDev", args -> GoOs.statDev(args[0], args[1]), 2);
        Linknames.register("graalgo.os.statIno", args -> GoOs.statIno(args[0], args[1]), 2);
        // Directory iteration.
        Linknames.register("graalgo.os.dirNext", args -> GoOs.dirNext(args[0], args[1]), 2);
        // Path operations.
        Linknames.register("graalgo.os.mkdir", args -> GoOs.mkdir(args[0], args[1]), 2);
        Linknames.register("graalgo.os.remove", args -> GoOs.remove(args[0]), 1);
        Linknames.register("graalgo.os.rename", args -> GoOs.rename(args[0], args[1]), 2);
        Linknames.register("graalgo.os.symlink", args -> GoOs.symlink(args[0], args[1]), 2);
        Linknames.register("graalgo.os.readlink", args -> GoOs.readlink(args[0], args[1]), 2);
        Linknames.register("graalgo.os.link", args -> GoOs.link(args[0], args[1]), 2);
        Linknames.register("graalgo.os.chmod", args -> GoOs.chmod(args[0], args[1]), 2);
        Linknames.register("graalgo.os.chtimes",
                args -> GoOs.chtimes(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.truncatePath",
                args -> GoOs.truncatePath(args[0], args[1]), 2);
        // Virtual cwd.
        Linknames.register("graalgo.os.getcwd", args -> GoOs.getcwd(), 0);
        Linknames.register("graalgo.os.chdir", args -> GoOs.chdir(args[0]), 1);
        // Environment.
        Linknames.register("graalgo.os.getenvFound", args -> GoOs.getenvFound(args[0]), 1);
        Linknames.register("graalgo.os.getenv", args -> GoOs.getenv(args[0]), 1);
        Linknames.register("graalgo.os.setenv", args -> GoOs.setenv(args[0], args[1]), 2);
        Linknames.register("graalgo.os.unsetenv", args -> GoOs.unsetenv(args[0]), 1);
        Linknames.register("graalgo.os.clearenv", args -> GoOs.clearenv(), 0);
        Linknames.register("graalgo.os.environ", args -> GoOs.environ(), 0);
        // Identity.
        Linknames.register("graalgo.os.getpid", args -> GoOs.getpid(), 0);
        Linknames.register("graalgo.os.getppid", args -> GoOs.getppid(), 0);
        Linknames.register("graalgo.os.getuid", args -> GoOs.getuid(), 0);
        Linknames.register("graalgo.os.geteuid", args -> GoOs.geteuid(), 0);
        Linknames.register("graalgo.os.getgid", args -> GoOs.getgid(), 0);
        Linknames.register("graalgo.os.getegid", args -> GoOs.getegid(), 0);
        Linknames.register("graalgo.os.getgroups", args -> GoOs.getgroups(), 0);
        // Hostname, exit, argv.
        Linknames.register("graalgo.os.hostname", args -> GoOs.hostname(args[0]), 1);
        Linknames.register("graalgo.os.exit", args -> GoOs.exit(args[0]), 1);
        Linknames.register("graalgo.os.argc", args -> GoOs.argc(), 0);
        Linknames.register("graalgo.os.argv", args -> GoOs.argv(args[0]), 1);
        // P3-M8 (crypto/rand): entropy source.
        Linknames.register("graalgo.os.getrandom", args -> GoOs.getrandom(args[0]), 1);
    }

    /**
     * Registers the {@code internal/poll.*} netpoller linknames
     * (implemented by {@link GoNetpoll}) and the {@code graalgo.net.*}
     * socket traps (implemented by {@link GoNet}).
     *
     * <p>Arity is the single-result __-helper argument count. The poll
     * linkname Go declarations are single-result by v1-frontend
     * necessity (the exporter rejects multi-result declarations, even
     * for linknames): {@code runtime_pollOpen} returns the pollDesc
     * cookie (0 on failure; errno via
     * {@code internal/poll.runtime_pollOpenErrno}).
     */
    private static void registerNetIntrinsics() {
        // Netpoller (internal/poll.runtime_poll*).
        Linknames.register("internal/poll.runtime_pollServerInit",
                args -> { GoNetpoll.pollServerInit(); return null; }, 0);
        Linknames.register("internal/poll.runtime_pollOpen",
                args -> GoNetpoll.pollOpen(args[0]), 1);
        Linknames.register("internal/poll.runtime_pollOpenErrno",
                args -> GoNetpoll.pollOpenErrno(), 0);
        Linknames.register("internal/poll.runtime_pollClose",
                args -> { GoNetpoll.pollClose(args[0]); return null; }, 1);
        Linknames.register("internal/poll.runtime_pollWait",
                args -> GoNetpoll.pollWait(args[0], args[1]), 2);
        Linknames.register("internal/poll.runtime_pollWaitCanceled",
                args -> { GoNetpoll.pollWaitCanceled(args[0], args[1]); return null; }, 2);
        Linknames.register("internal/poll.runtime_pollReset",
                args -> GoNetpoll.pollReset(args[0], args[1]), 2);
        Linknames.register("internal/poll.runtime_pollSetDeadline",
                args -> { GoNetpoll.pollSetDeadline(args[0], args[1], args[2]); return null; }, 3);
        Linknames.register("internal/poll.runtime_pollUnblock",
                args -> { GoNetpoll.pollUnblock(args[0]); return null; }, 1);
        Linknames.register("internal/poll.runtime_isPollServerDescriptor",
                args -> GoNetpoll.isPollServerDescriptor(args[0]), 1);
        // Socket traps (graalgo.net.*).
        Linknames.register("graalgo.net.socket",
                args -> GoNet.socket(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.net.bind",
                args -> GoNet.bind(args[0], args[1]), 2);
        Linknames.register("graalgo.net.listen",
                args -> GoNet.listen(args[0], args[1]), 2);
        Linknames.register("graalgo.net.accept",
                args -> GoNet.accept(args[0]), 1);
        Linknames.register("graalgo.net.connect",
                args -> GoNet.connect(args[0], args[1]), 2);
        Linknames.register("graalgo.net.setsockopt",
                args -> GoNet.setsockopt(args[0], args[1], args[2], args[3]), 4);
        Linknames.register("graalgo.net.getsockopt",
                args -> GoNet.getsockopt(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.net.getsockname",
                args -> GoNet.getsockname(args[0], args[1]), 2);
        Linknames.register("graalgo.net.getpeername",
                args -> GoNet.getpeername(args[0], args[1]), 2);
        Linknames.register("graalgo.net.shutdown",
                args -> GoNet.shutdown(args[0], args[1]), 2);
        Linknames.register("graalgo.net.setnonblock",
                args -> GoNet.setnonblock(args[0], args[1]), 2);
        Linknames.register("graalgo.net.sendto",
                args -> GoNet.sendto(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.net.recvfrom",
                args -> GoNet.recvfrom(args[0], args[1], args[2]), 3);
        // P3-M9 (module-probe lane): interface enumeration for
        // net.Interface/Interfaces (github.com/google/uuid v1 node IDs).
        Linknames.register("graalgo.net.interfaces",
                args -> GoNet.interfaces(args[0]), 1);
    }

    /**
     * Registers the 26 {@code graalgo.x509.*} intrinsics implemented by
     * {@link GoX509} (P3-M9 tier-5, x509.md §2). All take a single argument
     * (the X509Certificate as Object, or DER bytes as GoSlice) and return
     * the D5-boxed field value ([]byte as GoSlice, strings as GoString,
     * ints as Long, null on absent/error).
     */
    private static void registerX509Intrinsics() {
        Linknames.register("graalgo.x509.parse",
                args -> GoX509.parse(args[0]), 1);
        Linknames.register("graalgo.x509.serialNumber",
                args -> GoX509.serialNumber(args[0]), 1);
        Linknames.register("graalgo.x509.subject",
                args -> GoX509.subject(args[0]), 1);
        Linknames.register("graalgo.x509.issuer",
                args -> GoX509.issuer(args[0]), 1);
        Linknames.register("graalgo.x509.notBefore",
                args -> GoX509.notBefore(args[0]), 1);
        Linknames.register("graalgo.x509.notAfter",
                args -> GoX509.notAfter(args[0]), 1);
        Linknames.register("graalgo.x509.version",
                args -> GoX509.version(args[0]), 1);
        Linknames.register("graalgo.x509.sigAlgOid",
                args -> GoX509.sigAlgOid(args[0]), 1);
        Linknames.register("graalgo.x509.signature",
                args -> GoX509.signature(args[0]), 1);
        Linknames.register("graalgo.x509.tbsCertificate",
                args -> GoX509.tbsCertificate(args[0]), 1);
        Linknames.register("graalgo.x509.keyUsage",
                args -> GoX509.keyUsage(args[0]), 1);
        Linknames.register("graalgo.x509.basicConstraints",
                args -> GoX509.basicConstraints(args[0]), 1);
        Linknames.register("graalgo.x509.subjectKeyId",
                args -> GoX509.subjectKeyId(args[0]), 1);
        Linknames.register("graalgo.x509.authorityKeyId",
                args -> GoX509.authorityKeyId(args[0]), 1);
        Linknames.register("graalgo.x509.publicKeyAlg",
                args -> GoX509.publicKeyAlgOid(args[0]), 1);
        Linknames.register("graalgo.x509.publicKeyEncoded",
                args -> GoX509.publicKeyEncoded(args[0]), 1);
        Linknames.register("graalgo.x509.rawIssuer",
                args -> GoX509.rawIssuer(args[0]), 1);
        Linknames.register("graalgo.x509.rawSubject",
                args -> GoX509.rawSubject(args[0]), 1);
        Linknames.register("graalgo.x509.rsaModulus",
                args -> GoX509.rsaModulus(args[0]), 1);
        Linknames.register("graalgo.x509.rsaExponent",
                args -> GoX509.rsaExponent(args[0]), 1);
        Linknames.register("graalgo.x509.ecX",
                args -> GoX509.ecX(args[0]), 1);
        Linknames.register("graalgo.x509.ecY",
                args -> GoX509.ecY(args[0]), 1);
        Linknames.register("graalgo.x509.ecFieldSize",
                args -> GoX509.ecFieldSize(args[0]), 1);
        Linknames.register("graalgo.x509.ed25519Raw",
                args -> GoX509.ed25519Raw(args[0]), 1);
        Linknames.register("graalgo.x509.parseSpki",
                args -> GoX509.parseSpki(args[0]), 1);
        Linknames.register("graalgo.x509.parsePkcs1",
                args -> GoX509.parsePkcs1(args[0]), 1);
        Linknames.register("graalgo.x509.subjectAltNames",
                args -> GoX509.subjectAltNames(args[0]), 1);
        Linknames.register("graalgo.x509.parsePrivateKey",
                args -> GoX509.parsePrivateKey(args[0]), 1);
        Linknames.register("graalgo.x509.extKeyUsage",
                args -> GoX509.extKeyUsage(args[0]), 1);
    }

    /**
     * Registers the 11 {@code graalgo.tls.*} intrinsics implemented by
     * {@link TlsRuntime} (P3-M9 tier-5, SSLEngine delegation). The Go side
     * (synth_crypto_tls.go) drives the handshake state machine; these traps
     * only implement the cryptographic operations. Engine handles are longs
     * into TlsRuntime's table; []byte arrives as GoSlice; strings as
     * GoString; ints/bools as Long.
     */
    private static void registerTlsIntrinsics() {
        Linknames.register("graalgo.tls.newClientEngine",
                args -> TlsRuntime.newClientEngine(args[0], args[1], args[2],
                        args[3], args[4]), 5);
        Linknames.register("graalgo.tls.newServerEngine",
                args -> TlsRuntime.newServerEngine(args[0], args[1], args[2],
                        args[3], args[4], args[5]), 6);
        Linknames.register("graalgo.tls.beginHandshake",
                args -> TlsRuntime.beginHandshake(args[0]), 1);
        Linknames.register("graalgo.tls.engineStatus",
                args -> TlsRuntime.engineStatus(args[0]), 1);
        Linknames.register("graalgo.tls.wrap",
                args -> TlsRuntime.wrap(args[0], args[1]), 2);
        Linknames.register("graalgo.tls.unwrap",
                args -> TlsRuntime.unwrap(args[0], args[1]), 2);
        Linknames.register("graalgo.tls.hasPending",
                args -> TlsRuntime.hasPending(args[0]), 1);
        Linknames.register("graalgo.tls.runTasks",
                args -> TlsRuntime.runTasks(args[0]), 1);
        Linknames.register("graalgo.tls.peerCertificates",
                args -> TlsRuntime.peerCertificates(args[0]), 1);
        Linknames.register("graalgo.tls.connectionState",
                args -> TlsRuntime.connectionState(args[0]), 1);
        Linknames.register("graalgo.tls.close",
                args -> { TlsRuntime.close(args[0]); return null; }, 1);
    }

    /**
     * Registers the {@code graalgo.http.client.do} intrinsic implemented
     * by {@link GoHttp} (P3-M9 tier-5, java.net.http.HttpClient
     * delegation; the synthetic net/http Transport.RoundTrip drives it).
     * D5 argument types: method and URL arrive as GoString, headers as
     * GoSlice of GoString (flat k/v pairs), body as GoSlice of bytes
     * (see {@link GoHttp#clientDo} for the calling convention); returns
     * the documented GoSlice triple (status Long, header pairs, body
     * bytes) which the Go side reads as []interface{}.
     */
    private static void registerHttpIntrinsics() {
        Linknames.register("graalgo.http.client.do",
                args -> GoHttp.clientDo(args[0], args[1], args[2], args[3]), 4);
    }

    /**
     * Binds every top-level program function under its qualified Go name
     * ({@code <package>.<name>}, e.g. {@code main.helper}) for
     * intrinsic→Go lookup. Method instantiations are out of M4 scope.
     */
    static void bindGoFuncs(Json.Object doc, NodeFactory.Program program) {
        Json.Object pkg = doc.object("package", "document");
        String pkgName = pkg.string("name", "document.package");
        Map<Integer, GoFunc> functions = program.functions();
        for (Json.Value f : doc.array("funcs", "document").items()) {
            if (!(f instanceof Json.Object fn)
                    || !"funcdecl".equals(fn.string("kind", "document.funcs"))) {
                continue;
            }
            Json.Object fields = fn.object("fields", "document.funcs");
            int objId = fields.intValue("obj", "document.funcs.fields");
            GoFunc gf = functions.get(objId);
            if (gf == null) {
                continue; // main's GoRootNode is not in functions()
            }
            String name = fields.string("name", "document.funcs.fields");
            Linknames.bindGo(pkgName + "." + name, gf);
        }
    }
}
