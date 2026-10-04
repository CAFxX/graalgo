package graalgo.frontend;

import java.util.ArrayList;
import java.util.LinkedHashMap;

/**
 * Minimal hand-rolled JSON reader for the frontend/v1 wire format.
 *
 * <p>Choice rationale (no new third-party deps without asking): the v1
 * documents need only objects, arrays, strings, numbers, booleans and null,
 * and the reader must produce path-aware errors for malformed input. A ~200
 * line recursive-descent reader covers that with zero dependency risk and is
 * fully unit-tested via NodeFactoryTest. If a later workstream needs schema
 * validation or streaming on the Java side, revisit (jackson would be the
 * candidate) — do not grow this into a general JSON library silently.
 */
public final class Json {
    private Json() {}

    public sealed interface Value permits Object, Array, Str, Num, Bool, Null {}

    public record Object(LinkedHashMap<String, Value> members) implements Value {
        public boolean has(String key) {
            return members.containsKey(key);
        }

        public Value opt(String key) {
            return members.get(key);
        }

        public Value get(String key, String path) {
            Value v = members.get(key);
            if (v == null) {
                throw new FrontendException(path + ": missing required key \"" + key + "\"");
            }
            return v;
        }

        private Value typed(String key, String path, Class<?> want) {
            Value v = get(key, path);
            if (!want.isInstance(v)) {
                throw new FrontendException(
                        path + ": key \"" + key + "\" must be " + want.getSimpleName()
                                + ", got " + v.getClass().getSimpleName());
            }
            return v;
        }

        public String string(String key, String path) {
            return ((Str) typed(key, path, Str.class)).value();
        }

        public long longInt(String key, String path) {
            return ((Num) typed(key, path, Num.class)).asLong(path + "." + key);
        }

        public int intValue(String key, String path) {
            long l = longInt(key, path);
            if (l != (int) l) {
                throw new FrontendException(path + ": key \"" + key + "\" out of int range: " + l);
            }
            return (int) l;
        }

        public boolean bool(String key, String path) {
            return ((Bool) typed(key, path, Bool.class)).value();
        }

        public Object object(String key, String path) {
            return (Object) typed(key, path, Object.class);
        }

        public Array array(String key, String path) {
            return (Array) typed(key, path, Array.class);
        }
    }

    public record Array(ArrayList<Value> items) implements Value {
        public int size() {
            return items.size();
        }

        public Value get(int i) {
            return items.get(i);
        }
    }

    public record Str(String value) implements Value {}

    public record Num(String text, boolean integral, long longValue, double doubleValue)
            implements Value {
        public long asLong(String path) {
            if (!integral) {
                throw new FrontendException(path + ": expected integer, got " + text);
            }
            return longValue;
        }
    }

    public record Bool(boolean value) implements Value {}

    public record Null() implements Value {}

    /** Parses one complete JSON document; trailing content is an error. */
    public static Value parse(String text) {
        Parser p = new Parser(text);
        Value v = p.value();
        p.ws();
        if (!p.eof()) {
            throw p.error("trailing content after JSON document");
        }
        return v;
    }

    private static final class Parser {
        private final String s;
        private int i;

        Parser(String s) {
            this.s = s;
        }

        boolean eof() {
            return i >= s.length();
        }

        FrontendException error(String msg) {
            int line = 1, col = 1;
            for (int k = 0; k < i && k < s.length(); k++) {
                if (s.charAt(k) == '\n') {
                    line++;
                    col = 1;
                } else {
                    col++;
                }
            }
            return new FrontendException("JSON parse error at line " + line + ", col " + col + ": " + msg);
        }

        void ws() {
            while (!eof()) {
                char c = s.charAt(i);
                if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
                    i++;
                } else {
                    return;
                }
            }
        }

        char peek() {
            if (eof()) {
                throw error("unexpected end of input");
            }
            return s.charAt(i);
        }

        void expect(char c) {
            if (eof() || s.charAt(i) != c) {
                throw error("expected '" + c + "'");
            }
            i++;
        }

        Value value() {
            ws();
            char c = peek();
            return switch (c) {
                case '{' -> object();
                case '[' -> array();
                case '"' -> new Str(string());
                case 't' -> literal("true", new Bool(true));
                case 'f' -> literal("false", new Bool(false));
                case 'n' -> literal("null", new Null());
                default -> {
                    if (c == '-' || (c >= '0' && c <= '9')) {
                        yield number();
                    }
                    throw error("unexpected character '" + c + "'");
                }
            };
        }

        Value literal(String word, Value v) {
            if (s.startsWith(word, i)) {
                i += word.length();
                return v;
            }
            throw error("invalid literal");
        }

        Object object() {
            expect('{');
            LinkedHashMap<String, Value> m = new LinkedHashMap<>();
            ws();
            if (!eof() && s.charAt(i) == '}') {
                i++;
                return new Object(m);
            }
            while (true) {
                ws();
                if (eof() || s.charAt(i) != '"') {
                    throw error("expected string key");
                }
                String k = string();
                ws();
                expect(':');
                m.put(k, value());
                ws();
                char c = peek();
                if (c == ',') {
                    i++;
                    continue;
                }
                if (c == '}') {
                    i++;
                    return new Object(m);
                }
                throw error("expected ',' or '}'");
            }
        }

        Array array() {
            expect('[');
            ArrayList<Value> items = new ArrayList<>();
            ws();
            if (!eof() && s.charAt(i) == ']') {
                i++;
                return new Array(items);
            }
            while (true) {
                items.add(value());
                ws();
                char c = peek();
                if (c == ',') {
                    i++;
                    continue;
                }
                if (c == ']') {
                    i++;
                    return new Array(items);
                }
                throw error("expected ',' or ']'");
            }
        }

        String string() {
            expect('"');
            StringBuilder sb = new StringBuilder();
            while (true) {
                if (eof()) {
                    throw error("unterminated string");
                }
                char c = s.charAt(i++);
                if (c == '"') {
                    return sb.toString();
                }
                if (c == '\\') {
                    if (eof()) {
                        throw error("unterminated escape");
                    }
                    char e = s.charAt(i++);
                    switch (e) {
                        case '"', '\\', '/' -> sb.append(e);
                        case 'b' -> sb.append('\b');
                        case 'f' -> sb.append('\f');
                        case 'n' -> sb.append('\n');
                        case 'r' -> sb.append('\r');
                        case 't' -> sb.append('\t');
                        case 'u' -> {
                            if (i + 4 > s.length()) {
                                throw error("bad \\u escape");
                            }
                            try {
                                sb.append((char) Integer.parseInt(s.substring(i, i + 4), 16));
                            } catch (NumberFormatException ex) {
                                throw error("bad \\u escape");
                            }
                            i += 4;
                        }
                        default -> throw error("bad escape \\" + e);
                    }
                } else {
                    sb.append(c);
                }
            }
        }

        Value number() {
            int start = i;
            if (!eof() && s.charAt(i) == '-') {
                i++;
            }
            while (!eof() && Character.isDigit(s.charAt(i))) {
                i++;
            }
            boolean integral = true;
            if (!eof() && s.charAt(i) == '.') {
                integral = false;
                i++;
                while (!eof() && Character.isDigit(s.charAt(i))) {
                    i++;
                }
            }
            if (!eof() && (s.charAt(i) == 'e' || s.charAt(i) == 'E')) {
                integral = false;
                i++;
                if (!eof() && (s.charAt(i) == '+' || s.charAt(i) == '-')) {
                    i++;
                }
                while (!eof() && Character.isDigit(s.charAt(i))) {
                    i++;
                }
            }
            String text = s.substring(start, i);
            if (integral) {
                try {
                    return new Num(text, true, Long.parseLong(text), 0.0);
                } catch (NumberFormatException ex) {
                    throw error("integer out of range: " + text);
                }
            }
            try {
                return new Num(text, false, 0L, Double.parseDouble(text));
            } catch (NumberFormatException ex) {
                throw error("bad number: " + text);
            }
        }
    }
}
