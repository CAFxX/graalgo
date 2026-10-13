# Normalize the gzip.Reader.Multistream method value pointer printed by
# 00_roundtrip_levels.go ("saw-multistream", r.Multistream). The method value
# is a func pointer whose address is non-deterministic (ASLR, different
# codegen between gc and graalgo); byte-identity requires normalizing it.
# gc prints e.g. "0x4c04c0", graalgo prints "<nil>" when the method value
# is not materialized — both normalize to the same token.
s/saw-multistream 0x[0-9a-fA-F]+/saw-multistream 0xADDR/
s/saw-multistream <nil>/saw-multistream 0xADDR/
