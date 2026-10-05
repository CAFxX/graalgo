package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.Fmt;
import graalgo.values.GoKind;
import graalgo.values.GoString;

/**
 * A call to one of the D4 {@code fmt} stub functions (WS G; verifies
 * GG-0020).
 *
 * <p>Only the seven D4 symbols resolve ({@code Print}, {@code Printf},
 * {@code Println}, {@code Sprint}, {@code Sprintf}, {@code Sprintln},
 * {@code Errorf}); the gofrontend import overlay rejects any other
 * {@code fmt.X}. The {@code Print} variants return {@code (int, error)} in
 * Go; Phase 1 supports them in statement position only (results discarded) —
 * value-position use is a Phase-3 scope error from the factory. The
 * {@code Sprint} variants and {@code Errorf} return a single value.
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

    public FmtCallNode(String funcName, GoExprNode[] args,
            GoKind[] kinds, String[] typeNames,
            GoKind[] elemKinds, String[] elemTypeNames,
            java.util.List<GoKind>[] elemKindChains,
            java.util.List<String>[] elemTypeNameChains,
            GoKind[] keyKinds, String[] keyTypeNames,
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

    @Override
    public Object executeObject(VirtualFrame frame) {
        Fmt.Arg[] fargs = new Fmt.Arg[args.length];
        for (int i = 0; i < args.length; i++) {
            fargs[i] = new Fmt.Arg(readArg(args[i], frame), kinds[i], typeNames[i],
                    keyKinds[i], keyTypeNames[i], elemKinds[i], elemTypeNames[i],
                    elemKindChains[i], elemTypeNameChains[i]);
        }
        return switch (funcName) {
            case "Sprint" -> Fmt.sprint(fargs);
            case "Sprintf" -> Fmt.sprintf(fmtString(fargs), java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
            case "Sprintln" -> Fmt.sprintln(fargs);
            case "Errorf" -> Fmt.errorf(fmtString(fargs), java.util.Arrays.copyOfRange(fargs, 1, fargs.length));
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
        if (fargs.length == 0) {
            throw new FrontendException(funcName + " requires a format string");
        }
        Object v = fargs[0].value();
        if (!(v instanceof GoString gs)) {
            throw new FrontendException(funcName + " format is not a string");
        }
        return new String(gs.bytes(), java.nio.charset.StandardCharsets.UTF_8);
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
