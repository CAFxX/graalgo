// Package encode implements the v2 Go→Java binary interface.
//
// The v2 frontend produces an in-memory *Document (see emit package).
// To transfer it to the Java backend without JSON (his 2026-10-15 order:
// "that should obviously skip the json"), this package encodes the
// Document into a compact binary format.
//
// Format (all integers are unsigned varints, strings are UTF-8):
//
//	document := magic version value
//	magic   := 0x67 0x32 0x62 0x69 ("g2bi": Go→Java binary interface)
//	version := varint format version (currently 1)
//	value   := tag payload
//	tag     := 0x00 (null) | 0x01 (bool) | 0x02 (int) | 0x03 (string)
//	         | 0x04 (list) | 0x05 (map)
//	bool    := 0x00 | 0x01
//	int     := varint of the zigzag-encoded 64-bit pattern:
//	           zz(x) = (x << 1) ^ (x >> 63) with ARITHMETIC right shift,
//	           applied to the 64-bit pattern reinterpreted as signed.
//	           The transform is a bijection on all 64-bit patterns, so
//	           EVERY integer width (int..int64, uint..uint64) is
//	           zigzag-encoded and the Java side un-zigzags every TAG_INT
//	           unconditionally, recovering the original pattern.
//	           (R5-P1-1: unsigned widths were previously written raw, which
//	           the decoder misread as zigzag — silent data corruption,
//	           e.g. uint64(5) decoded as -3.)
//	string  := varint length, bytes
//	list    := varint count, values...
//	map     := varint count, (string key, value)...
//
// There is deliberately NO float tag. The emitter transports float
// constants as their source spelling (Go strings, e.g. "3.14"), never as
// Go float64 values: the Java side parses the spelling per kind in
// NodeFactoryV2.buildBasicLit (float32 vs float64, hex floats, etc. —
// information a bare IEEE-754 payload would destroy). A Go float64 (or
// float32) value reaching the encoder is therefore always a bug, and the
// encoder fails loudly on it (see encodeValue) instead of emitting it
// under TAG_STRING, which the Java decoder would read as a java.lang.String
// — a silent type change (L24).
//
// The magic + version header lets the Java decoder reject version skew,
// truncation, or garbage input loudly instead of misdecoding it (R5-P2-5).
//
// The encoding is general: it handles the Document's map[string]any
// structure without hardcoding node kinds. The Java side decodes into
// generic maps/lists and the NodeFactoryV2 constructs Truffle nodes.
package encode

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	"graalgo-frontend-v2/emit"
)

const (
	tagNull   = 0x00
	tagBool   = 0x01
	tagInt    = 0x02
	tagString = 0x03
	tagList   = 0x04
	tagMap    = 0x05
)

// formatMagic identifies the v2 Go→Java binary interface on the wire.
const formatMagic = "g2bi"

// formatVersion is the current wire format version. Bump on any
// incompatible change; the Java decoder rejects unknown versions loudly.
// Keep in lockstep with BinaryDecoder.FORMAT_VERSION.
const formatVersion = 1

// Encode serializes a Document into the v2 binary format.
func Encode(doc *emit.Document) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(formatMagic)
	writeVarint(&buf, formatVersion)
	// Encode as a map with the Document's top-level fields.
	m := map[string]any{
		"frontend":  doc.Frontend,
		"generator": doc.Generator,
		"package":   doc.Package,
		"types":     doc.Types,
		"objects":   doc.Objects,
		"funcs":     doc.Funcs,
		"globals":   doc.Globals,
	}
	if err := encodeValue(&buf, m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeValue writes one value with its tag.
func encodeValue(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteByte(tagNull)
	case bool:
		buf.WriteByte(tagBool)
		if x {
			buf.WriteByte(0x01)
		} else {
			buf.WriteByte(0x00)
		}
	case int:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzag(int64(x)))
	case int8:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzag(int64(x)))
	case int16:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzag(int64(x)))
	case int32:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzag(int64(x)))
	case int64:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzag(x))
	case uint:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzagUnsigned(uint64(x)))
	case uint8:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzagUnsigned(uint64(x)))
	case uint16:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzagUnsigned(uint64(x)))
	case uint32:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzagUnsigned(uint64(x)))
	case uint64:
		buf.WriteByte(tagInt)
		writeVarint(buf, zigzagUnsigned(x))
	case string:
		buf.WriteByte(tagString)
		writeVarint(buf, uint64(len(x)))
		buf.WriteString(x)
	case []any:
		buf.WriteByte(tagList)
		writeVarint(buf, uint64(len(x)))
		for _, e := range x {
			if err := encodeValue(buf, e); err != nil {
				return err
			}
		}
	case []string:
		buf.WriteByte(tagList)
		writeVarint(buf, uint64(len(x)))
		for _, s := range x {
			buf.WriteByte(tagString)
			writeVarint(buf, uint64(len(s)))
			buf.WriteString(s)
		}
	case map[string]any:
		buf.WriteByte(tagMap)
		// Sort keys for deterministic output.
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		writeVarint(buf, uint64(len(keys)))
		for _, k := range keys {
			writeVarint(buf, uint64(len(k)))
			buf.WriteString(k)
			if err := encodeValue(buf, x[k]); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("encode: unsupported type %T", v)
	}
	return nil
}

// zigzag maps a signed 64-bit value to unsigned for varint efficiency.
// The transform (n << 1) ^ (n >> 63) — with arithmetic right shift — is a
// bijection between int64 values and 64-bit patterns, inverted by the
// Java decoder for every TAG_INT (see BinaryDecoder.readZigzagVarint).
func zigzag(n int64) uint64 {
	return uint64((n << 1) ^ (n >> 63))
}

// zigzagUnsigned applies the zigzag transform to an unsigned 64-bit value
// by reinterpreting its bit pattern as signed. The arithmetic shift is
// load-bearing: a logical shift would map uint64(1)<<63 to wire 0x01,
// which the Java side decodes as -1, not the original pattern. With the
// reinterpretation the wire format stays a bijection on 64-bit patterns
// for the full uint64 range.
func zigzagUnsigned(u uint64) uint64 {
	return zigzag(int64(u))
}

// writeVarint writes a varint (LEB128 unsigned).
func writeVarint(buf *bytes.Buffer, x uint64) {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], x)
	buf.Write(tmp[:n])
}
