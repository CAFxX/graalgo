package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Fmt;
import graalgo.runtime.FmtScan;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * A call to one of the {@code fmt} stub functions (WS G; verifies
 * GG-0020; P3-M10 extends the surface with the F*, Append and scan families
 * and FormatString).
 *
 * <p>The D4 symbols ({@code Print}, {@code Printf}, {@code Println},
 * {@code Sprint}, {@code Sprintf}, {@code Sprintln}, {@code Errorf}),
 * the F* family ({@code Fprint}, {@code Fprintf}, {@code Fprintln}),
 * the Append family ({@code Append}, {@code Appendf},
 * {@code Appendln}), the scan family ({@code Scan}, {@code Scanf},
 * {@code Scanln}, {@code Fscan}, {@code Fscanf}, {@code Fscanln},
 * {@code Sscan}, {@code Sscanf}, {@code Sscanln}) and
 * {@code FormatString} resolve through the gofrontend import overlay;
 * any other {@code fmt.X} is a scope error naming Phase 3. The
 * {@code Print}, {@code F*} and scan families return {@code (int,
 * error)} in Go; Phase 1 supports them in statement position only
 * (results discarded) — value-position use is a Phase-3 scope error from
 * the factory. The {@code Sprint}/{@code Append} families,
 * {@code FormatString} and {@code Errorf} return a single value.
 *
 * <p>Static type metadata (kinds, Go-syntax names) is supplied by the
 * factory from the wire type table; values are read from the children at
 * runtime.
 */
public final class FmtCallNode extends GoExprNode {
    private final String funcName;
    @Children private GoExprNode[] args;
    private final GoKind[] kinds;
    private final String[] typeNames;
    private final GoKind[] elemKinds;
    private final String[] elemTypeNames;
    // M7-remediation (R2 P0-2): element kind/name chains for nested
    // array/slice elements (e.g. [INT32] for [2][2]int, [ARRAY, INT32] for
    // [2][2][2]int); depth-general, replacing the old single elemElemKind.
    private final java.util.List<GoKind>[] elemKindChains;
    private final java.util.List<String>[] elemTypeNameChains;
    private final GoKind[] keyKinds;
    private final String[] keyTypeNames;
    // M8 closeout: key kind/name chains for nested maps (parallel to the
    // elem chains for nested arrays); e.g. map[string]map[string]int has
    // keyKindChain=[STRING], elemKindChain=[INT].
    private final java.util.List<GoKind>[] keyKindChains;
    private final java.util.List<String>[] keyTypeNameChains;

    public FmtCallNode(String funcName, GoExprNode[] args,
            GoKind[] kinds, String[] typeNames,
            GoKind[] elemKinds, String[] elemTypeNames,
            java.util.List<GoKind>[] elemKindChains,
            java.util.List<String>[] elemTypeNameChains,
            GoKind[] keyKinds, String[] keyTypeNames,
            java.util.List<GoKind>[] keyKindChains,
            java.util.List<String>[] keyTypeNameChains,
            GoKind resultKind) {
        super(resultKind);
        this.funcName = funcName;
        this.args = args;
        this.kinds = kinds;
        this.typeNames = typeNames;
        this.elemKinds = elemKinds;
        this.elemTypeNames = elemTypeNames;
        this.elemKindChains = elemKindChains;
        this.elemTypeNameChains = elemTypeNameChains;
        this.keyKinds = keyKinds;
        this.keyTypeNames = keyTypeNames;
        this.keyKindChains = keyKindChains;
        this.keyTypeNameChains = keyTypeNameChains;
    }

    // Package-private accessors for deferred fmt calls (M7): DeferNode
    // evaluates the argument children at defer time and DeferredCall
    // replays the formatting at return/unwind time from the captured
    // values plus this static metadata.
    String funcName() {
        return funcName;
    }

    GoExprNode[] fmtArgs() {
        return args;
    }

    GoKind[] argKinds() {
        return kinds;
    }

    String[] argTypeNames() {
        return typeNames;
    }

    GoKind[] argElemKinds() {
        return elemKinds;
    }

    java.util.List<GoKind>[] argElemKindChains() {
        return elemKindChains;
    }

    java.util.List<String>[] argElemTypeNameChains() {
        return elemTypeNameChains;
    }

    String[] argElemTypeNames() {
        return elemTypeNames;
    }

    GoKind[] argKeyKinds() {
        return keyKinds;
    }

    String[] argKeyTypeNames() {
        return keyTypeNames;
    }

    java.util.List<GoKind>[] argKeyKindChains() {
        return keyKindChains;
    }

    java.util.List<String>[] argKeyTypeNameChains() {
        return keyTypeNameChains;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Fmt.Arg[] fargs = new Fmt.Arg[args.length];
        for (int i = 0; i < args.length; i++) {
            fargs[i] = new Fmt.Arg(readArg(args[i], frame), kinds[i], typeNames[i],
                    keyKinds[i], keyTypeNames[i], elemKinds[i], elemTypeNames[i],
                    elemKindChains[i], elemTypeNameChains[i],
                    keyKindChains[i], keyTypeNameChains[i]);
        }
        return switch (funcName) {
            case "Sprint" -> Fmt.sprint(fargs);
            case "Sprintf" -> Fmt.sprintf(fmtString(fargs), java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
            case "Sprintln" -> Fmt.sprintln(fargs);
            case "Errorf" -> Fmt.errorf(fmtString(fargs), java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
            case "Append" -> Fmt.append(sliceArg(fargs, 0),
                    java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
            case "Appendf" -> Fmt.appendf(sliceArg(fargs, 0), fmtStringAt(fargs, 1),
                    java.util.Arrays.copyOfRange(fargs, 2, fargs.length));
            case "Appendln" -> Fmt.appendln(sliceArg(fargs, 0),
                    java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
            case "FormatString" -> Fmt.formatString(fargs[0], runeArg(fargs, 1));
            // Print/Printf/Println in statement position: discard results.
            case "Print" -> {
                Fmt.print(fargs);
                yield GoString.EMPTY;
            }
            case "Printf" -> {
                Fmt.printf(fmtString(fargs), java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
                yield GoString.EMPTY;
            }
            case "Println" -> {
                Fmt.println(fargs);
                yield GoString.EMPTY;
            }
            // P3-M10 (fix-fmt-funcs): the F* family — format like the
            // S* family, write to the writer (fargs[0]), discard (n, err).
            case "Fprint" -> {
                Fmt.fprint(fargs[0], java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
                yield GoString.EMPTY;
            }
            case "Fprintf" -> {
                Fmt.fprintf(fargs[0], fmtStringAt(fargs, 1),
                        java.util.Arrays.copyOfRange(fargs, 2, fargs.length));
                yield GoString.EMPTY;
            }
            case "Fprintln" -> {
                Fmt.fprintln(fargs[0], java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
                yield GoString.EMPTY;
            }
            // P3-M10 (fix-fmt-funcs): the scan family — statement position
            // only (results discarded); destinations are written through.
            case "Sscan" -> {
                FmtScan.sscan(fargs[0], java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
                yield GoString.EMPTY;
            }
            case "Sscanf" -> {
                FmtScan.sscanf(fargs[0], fmtStringAt(fargs, 1),
                        java.util.Arrays.copyOfRange(fargs, 2, fargs.length));
                yield GoString.EMPTY;
            }
            case "Sscanln" -> {
                FmtScan.sscanln(fargs[0], java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
                yield GoString.EMPTY;
            }
            case "Fscan" -> {
                FmtScan.fscan(fargs[0], java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
                yield GoString.EMPTY;
            }
            case "Fscanf" -> {
                FmtScan.fscanf(fargs[0], fmtStringAt(fargs, 1),
                        java.util.Arrays.copyOfRange(fargs, 2, fargs.length));
                yield GoString.EMPTY;
            }
            case "Fscanln" -> {
                FmtScan.fscanln(fargs[0], java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
                yield GoString.EMPTY;
            }
            case "Scan" -> {
                FmtScan.scanStdin(fargs);
                yield GoString.EMPTY;
            }
            case "Scanf" -> {
                FmtScan.scanfStdin(fmtString(fargs), java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
                yield GoString.EMPTY;
            }
            case "Scanln" -> {
                FmtScan.scanlnStdin(fargs);
                yield GoString.EMPTY;
            }
            default -> throw new FrontendException("unknown fmt function: " + funcName);
        };
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        // Statement position: execute for effect, discard.
        executeObject(frame);
        return 0L;
    }

    private String fmtString(Fmt.Arg[] fargs) {
        return fmtStringAt(fargs, 0);
    }

    private String fmtStringAt(Fmt.Arg[] fargs, int at) {
        if (fargs.length <= at) {
            throw new FrontendException(funcName + " requires a format string");
        }
        Object v = fargs[at].value();
        if (!(v instanceof GoString gs)) {
            throw new FrontendException(funcName + " format is not a string");
        }
        return new String(gs.bytes(), java.nio.charset.StandardCharsets.UTF_8);
    }

    private graalgo.values.GoSlice sliceArg(Fmt.Arg[] fargs, int at) {
        if (fargs.length <= at) {
            throw new FrontendException(funcName + " requires a []byte argument");
        }
        Object v = fargs[at].value();
        if (v == null) {
            return null;
        }
        if (!(v instanceof graalgo.values.GoSlice gs)) {
            throw new FrontendException(funcName + " []byte argument is not a slice");
        }
        return gs;
    }

    private int runeArg(Fmt.Arg[] fargs, int at) {
        if (fargs.length <= at) {
            throw new FrontendException(funcName + " requires a rune argument");
        }
        Object v = fargs[at].value();
        if (v instanceof Integer i) {
            return i;
        }
        if (v instanceof Long l) {
            return l.intValue();
        }
        throw new FrontendException(funcName + " rune argument is not an integer");
    }

    static Object readArg(GoExprNode child, VirtualFrame frame) {
        return switch (child.kind().slotKind()) {
            case Int -> child.executeInt(frame);
            case Long -> child.executeLong(frame);
            case Float -> child.executeFloat(frame);
            case Double -> child.executeDouble(frame);
            case Boolean -> child.executeBoolean(frame);
            case Object -> child.executeObject(frame);
            default -> throw new FrontendException("unsupported kind: " + child.kind());
        };
    }
}
