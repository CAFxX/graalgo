package graalgo.go.loader;

import graalgo.go.types.GoType;
import java.util.HashMap;
import java.util.Map;

/**
 * The type environment of one generic instantiation (toolexec-design §3.5, D6): type parameter
 * (by its canonical key, {@code $owner.index}) → type argument. Function bodies and type
 * definitions are decoded once and built per environment.
 */
public final class TypeEnv {
    final Map<String, GoType> params = new HashMap<>();
    /** Types of generic bodies already resolved under this environment. */
    final Map<Long, GoType> cache = new HashMap<>();
    /** Identity of the instantiation: the argument keys. */
    public final String id;

    TypeEnv(String[] keys, GoType[] args) {
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < keys.length; i++) {
            params.put(keys[i], args[i]);
            sb.append(i > 0 ? "," : "").append(args[i].key);
        }
        this.id = sb.toString();
    }
}
