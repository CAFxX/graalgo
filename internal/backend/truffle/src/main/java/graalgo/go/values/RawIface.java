package graalgo.go.values;

/**
 * An interface slot whose two gc words do not form an interface value at the moment: code that
 * writes the header word by word through a view ({@code atomic.Value} stores a marker pointer
 * in the type word, then the data word, then the type) leaves this in the slot between its
 * stores (ADR-0015 decision 2). Typed code never reads such a slot — in gc it would read a
 * corrupt interface.
 */
public record RawIface(Object typ, Object data) {
}
