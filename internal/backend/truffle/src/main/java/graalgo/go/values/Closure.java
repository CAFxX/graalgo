package graalgo.go.values;

import com.oracle.truffle.api.RootCallTarget;

/**
 * A Go func value: {@code {RootCallTarget, env}} (SPEC §3.4). {@code env} is the closure's
 * capture environment in the {@link Env} layout its target expects: null without captures, the
 * captured value itself for one, an {@code Object[]} for more. Captured variables are held by
 * reference through their {@link Flat} boxes (a fresh box per iteration for Go 1.22 loop
 * variables) — never the creating function's activation frame. The nil func is Java
 * {@code null}.
 */
public record Closure(RootCallTarget target, Object env) {
}
