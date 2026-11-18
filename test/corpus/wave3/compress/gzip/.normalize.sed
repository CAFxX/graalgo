# Normalize the gzip.Reader.Multistream method value pointer printed by
# 00_roundtrip_levels.go ("saw-multistream", r.Multistream). The method value
# is a func pointer whose address is non-deterministic (ASLR), so only a real
# address is normalized. graalgo printing "<nil>" (the method value is not
# materialized) is a semantic bug, BUG-0013 in docs/dev/bugs.md — it is NOT
# normalized away (global review G7: the old rule mapped "<nil>" to the same
# token as gc's address and hid it).
s/saw-multistream 0x[0-9a-fA-F]+/saw-multistream 0xADDR/
