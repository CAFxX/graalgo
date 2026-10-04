#!/bin/bash
# M5 differential test: gc vs graalgo for corpus/panic.
set -u
export PATH=~/tools/go/bin:$PATH
CD=~/workspace/graalgo/phases/phase1
cd "$CD"

normalize() {
    sed -E -e 's/0x[0-9a-fA-F]+/0xADDR/g' \
           -e 's/goroutine [0-9]+ \[/goroutine # [/g' \
           -e 's/([a-zA-Z0-9_]+\.go):[0-9]+/\1:LINE/g' \
           -e 's/\+0x[0-9a-fA-F]+//g'
}

run_gg() {
    local file="$1"; shift
    ./graalgo-run "$@" "$file" >/tmp/gg.out 2>&1
    echo $?
}

pass=0; fail=0; failed=()
for f in corpus/panic/*.go; do
    name=$(basename "$f")
    gcbin=/tmp/gcbin_$$
    if ! go build -o "$gcbin" "$f" 2>/dev/null; then
        echo "SKIP $name (gc build failed)"
        rm -f "$gcbin"
        continue
    fi
    "$gcbin" >/tmp/gc.out 2>&1; gc_exit=$?
    rm -f "$gcbin"
    gc_first=$(head -1 /tmp/gc.out | normalize)

    if grep -q "^// panic:" "$f"; then
        expected=$(grep "^// panic:" "$f" | cut -d: -f2- | sed 's/^ //')
        gg_exit=$(run_gg "$f")
        gg_first=$(grep -v "WARNING" /tmp/gg.out | head -1 | normalize)
        if [[ "$expected" == fatal* ]]; then
            exp_norm=$(echo "$expected" | normalize)
        else
            exp_norm=$(echo "panic: $expected" | normalize)
        fi
        if [ "$gc_exit" == "$gg_exit" ] && [ "$gg_first" == "$exp_norm" ]; then
            pass=$((pass+1))
        else
            fail=$((fail+1)); failed+=("$name")
            echo "FAIL $name:"
            echo "  gc:   exit=$gc_exit first='$gc_first'"
            echo "  gg:   exit=$gg_exit first='$gg_first'"
            echo "  want: '$exp_norm'"
        fi
    elif grep -q "^// observe:" "$f"; then
        var=$(grep "^// observe:" "$f" | cut -d: -f2 | sed 's/^ //;s/ .*//')
        gg_exit=$(run_gg "$f" --dump-vars)
        gg_val=$(grep -v "WARNING" /tmp/gg.out | grep "^$var=" | cut -d= -f2)
        if [ -n "$gg_val" ]; then
            pass=$((pass+1))
        else
            fail=$((fail+1)); failed+=("$name")
            echo "FAIL $name: no observed value for $var (gg exit=$gg_exit)"
        fi
    else
        gg_exit=$(run_gg "$f")
        if [ "$gc_exit" == "$gg_exit" ]; then
            pass=$((pass+1))
        else
            fail=$((fail+1)); failed+=("$name")
            echo "FAIL $name: gc exit=$gc_exit vs gg exit=$gg_exit"
        fi
    fi
done
echo ""
echo "pass=$pass fail=$fail"
if [ ${#failed[@]} -gt 0 ]; then echo "failed: ${failed[*]}"; fi
