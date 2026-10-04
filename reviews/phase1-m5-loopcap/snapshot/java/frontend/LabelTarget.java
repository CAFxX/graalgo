package graalgo.frontend;

/**
 * A statement that can carry a Go label for labeled {@code break} /
 * {@code continue} (M4, WS E): loops and switches. The label is installed
 * by the {@code labeled} builder after the child is built.
 */
public interface LabelTarget {
    void setLabel(String label);
}
