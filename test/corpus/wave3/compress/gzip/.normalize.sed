# Normalize the gzip.Reader.Multistream method value pointer printed by
# 00_roundtrip_levels.go ("saw-multistream", r.Multistream). The method value
# is a func pointer whose address is non-deterministic (ASLR, different
# codegen between gc and graalgo); byte-identity requires normalizing it.
# gc prints e.g. "0x4c04c0". ONLY the address form is normalized here.
# graalgo printing "<nil>" for the same value is NOT normalized: that is a
# genuine semantic divergence (an unmaterialized method value — calling it
# would panic under graalgo and work under gc), and normalizing it to the
# same token used to mask that divergence (R1-32). If "<nil>" reappears in
# output, report it as a graalgo defect; do not re-add a rule for it here.
s/saw-multistream 0x[0-9a-fA-F]+/saw-multistream 0xADDR/
