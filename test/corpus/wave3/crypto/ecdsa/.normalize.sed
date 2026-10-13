# P3-M9: crypto/ecdsa normalization — masks provably-volatile lines.
#
# The ecdsa corpus programs are nondeterministic under gc itself:
# - 00_sign_verify.go: r=, s= — Go 1.27 FIPS hedged signatures mix runtime
#   entropy into (r, s); values differ across `go run` invocations.
# - 01_curves.go: asn1-len= — the ASN.1 encoding length varies (62 vs 63)
#   depending on the high bit of the generated values.
# - 02_generate.go: d=, shared-x= — fresh random key generation; values
#   differ on every run by design.
#
# Byte-identical comparison of these lines is impossible for ANY
# implementation. The deterministic semantic lines (verify=, tampered=,
# pub-x=, pub-y=, d-in-range=, agreement=, dG-eq=, pub-oncurve=) carry the
# real differential signal and are NOT masked.
s/^r=[0-9a-f]+$/r=<volatile>/
s/^s=[0-9a-f]+$/s=<volatile>/
s/asn1-len=[0-9]+/asn1-len=<volatile>/
s/^d=[0-9a-f]+$/d=<volatile>/
s/^shared-x=[0-9a-f]+$/shared-x=<volatile>/
