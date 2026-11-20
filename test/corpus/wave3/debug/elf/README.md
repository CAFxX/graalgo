# `debug/elf` wave-3 corpus

Differential corpus for the debug/elf package (L11, REVIEW-2026-11-18-05).

## Scope boundary

ELF header of /bin/true: class/data/type/machine/entry, section
names and program-header types; error paths for a non-ELF file and a
missing file.

## Programs

- `00_basic.go` — ELF header of /bin/true: class/type/machine/entry, sections, program headers.

## Retest trigger

Any change to debug/elf header parsing, section enumeration, or
program-header reporting.
