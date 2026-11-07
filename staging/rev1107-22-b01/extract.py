import re, sys

MARKER = "const syntheticCryptoTlsSource = `"
for tag, path in [
    ("patched", "/tmp/rev1107-22-b01/patched/internal/frontend/synth_crypto_tls.go"),
    ("orig", "/home/hatch/workspace/graalgo/internal/frontend/synth_crypto_tls.go"),
]:
    src = open(path).read()
    start = src.index(MARKER) + len(MARKER)
    # The string runs to the final backtick in the file (EOF: "`\n").
    end = src.rindex("`")
    assert end > start, tag
    inner = src[start:end]
    assert inner.startswith("package tls"), (tag, inner[:80])
    out = f"/tmp/rev1107-22-b01/extract/tls-{tag}.go"
    open(out, "w").write(inner)
    print(tag, "inner bytes:", len(inner), "lines:", inner.count("\n"))
