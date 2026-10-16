// Package encode implements the v2 Go→Java binary interface.
//
// The v2 frontend produces an in-memory *Document (see emit package).
// To transfer it to the Java backend without JSON (his 2026-10-15 order:
// "that should obviously skip the json"), this package encodes the
// Document into a compact binary format.
//
// Format (all integers are unsigned varints, strings are UTF-8):
//
//	value := tag payload
//	tag   := 0x00 (null) | 0x01 (bool) | 0x02 (int) | 0x03 (string)
//	       | 0x04 (list) | 0x05 (map)
//	bool  := 0x00 | 0x01
//	int   := varint (zigzag for negatives: (n << 1) ^ (n >> 63))
//	string:= varint length, bytes
//	list  := varint count, values...
//	map   := varint count, (string key, value)...
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

// Encode serializes a Document into the v2 binary format.
func Encode(doc *emit.Document) ([]byte, error) {
	var buf bytes.Buffer
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
		writeVarint(buf, uint64(x))
	case uint8:
		buf.WriteByte(tagInt)
		writeVarint(buf, uint64(x))
	case uint16:
		buf.WriteByte(tagInt)
		writeVarint(buf, uint64(x))
	case uint32:
		buf.WriteByte(tagInt)
		writeVarint(buf, uint64(x))
	case uint64:
		buf.WriteByte(tagInt)
		writeVarint(buf, x)
	case float64:
		// Floats are encoded as strings to preserve exact representation.
		// The Java side parses them back. (v2 currently uses ints for
		// constants; this is for future completeness.)
		buf.WriteByte(tagString)
		s := fmt.Sprintf("%v", x)
		writeVarint(buf, uint64(len(s)))
		buf.WriteString(s)
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

// zigzag maps signed to unsigned for varint efficiency.
func zigzag(n int64) uint64 {
	return uint64((n << 1) ^ (n >> 63))
}

// writeVarint writes a varint (LEB128 unsigned).
func writeVarint(buf *bytes.Buffer, x uint64) {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], x)
	buf.Write(tmp[:n])
}
