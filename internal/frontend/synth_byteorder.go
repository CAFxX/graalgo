// Retirement: see ../../docs/arch/synth-retirement-tracker.md#internalbyteorder for this package's root cause and retires-when trigger (internal/byteorder).
package main

const syntheticByteorderSource = (
"package byteorder\n" +
"\n" +
"// P3-M9 (wave 3): synthetic `internal/byteorder` package.\n" +
"// SOURCE: go1.27.1 GOROOT/src/internal/byteorder/byteorder.go, verbatim\n" +
"// (pure Go, no imports). Needed by math/big's marshaling paths.\n" +
"\n" +
"// Copyright 2024 The Go Authors. All rights reserved.\n" +
"// Use of this source code is governed by a BSD-style\n" +
"// license that can be found in the LICENSE file.\n" +
"\n" +
"// Package byteorder provides functions for decoding and encoding\n" +
"// little and big endian integer types from/to byte slices.\n" +
"func LEUint16(b []byte) uint16 {\n" +
"	_ = b[1] // bounds check hint to compiler; see golang.org/issue/14808\n" +
"	return uint16(b[0]) | uint16(b[1])<<8\n" +
"}\n" +
"\n" +
"func LEPutUint16(b []byte, v uint16) {\n" +
"	_ = b[1] // early bounds check to guarantee safety of writes below\n" +
"	b[0] = byte(v)\n" +
"	b[1] = byte(v >> 8)\n" +
"}\n" +
"\n" +
"func LEAppendUint16(b []byte, v uint16) []byte {\n" +
"	return append(b,\n" +
"		byte(v),\n" +
"		byte(v>>8),\n" +
"	)\n" +
"}\n" +
"\n" +
"func LEUint32(b []byte) uint32 {\n" +
"	_ = b[3] // bounds check hint to compiler; see golang.org/issue/14808\n" +
"	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24\n" +
"}\n" +
"\n" +
"func LEPutUint32(b []byte, v uint32) {\n" +
"	_ = b[3] // early bounds check to guarantee safety of writes below\n" +
"	b[0] = byte(v)\n" +
"	b[1] = byte(v >> 8)\n" +
"	b[2] = byte(v >> 16)\n" +
"	b[3] = byte(v >> 24)\n" +
"}\n" +
"\n" +
"func LEAppendUint32(b []byte, v uint32) []byte {\n" +
"	return append(b,\n" +
"		byte(v),\n" +
"		byte(v>>8),\n" +
"		byte(v>>16),\n" +
"		byte(v>>24),\n" +
"	)\n" +
"}\n" +
"\n" +
"func LEUint64(b []byte) uint64 {\n" +
"	_ = b[7] // bounds check hint to compiler; see golang.org/issue/14808\n" +
"	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |\n" +
"		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56\n" +
"}\n" +
"\n" +
"func LEPutUint64(b []byte, v uint64) {\n" +
"	_ = b[7] // early bounds check to guarantee safety of writes below\n" +
"	b[0] = byte(v)\n" +
"	b[1] = byte(v >> 8)\n" +
"	b[2] = byte(v >> 16)\n" +
"	b[3] = byte(v >> 24)\n" +
"	b[4] = byte(v >> 32)\n" +
"	b[5] = byte(v >> 40)\n" +
"	b[6] = byte(v >> 48)\n" +
"	b[7] = byte(v >> 56)\n" +
"}\n" +
"\n" +
"func LEAppendUint64(b []byte, v uint64) []byte {\n" +
"	return append(b,\n" +
"		byte(v),\n" +
"		byte(v>>8),\n" +
"		byte(v>>16),\n" +
"		byte(v>>24),\n" +
"		byte(v>>32),\n" +
"		byte(v>>40),\n" +
"		byte(v>>48),\n" +
"		byte(v>>56),\n" +
"	)\n" +
"}\n" +
"\n" +
"func BEUint16(b []byte) uint16 {\n" +
"	_ = b[1] // bounds check hint to compiler; see golang.org/issue/14808\n" +
"	return uint16(b[1]) | uint16(b[0])<<8\n" +
"}\n" +
"\n" +
"func BEPutUint16(b []byte, v uint16) {\n" +
"	_ = b[1] // early bounds check to guarantee safety of writes below\n" +
"	b[0] = byte(v >> 8)\n" +
"	b[1] = byte(v)\n" +
"}\n" +
"\n" +
"func BEAppendUint16(b []byte, v uint16) []byte {\n" +
"	return append(b,\n" +
"		byte(v>>8),\n" +
"		byte(v),\n" +
"	)\n" +
"}\n" +
"\n" +
"func BEUint32(b []byte) uint32 {\n" +
"	_ = b[3] // bounds check hint to compiler; see golang.org/issue/14808\n" +
"	return uint32(b[3]) | uint32(b[2])<<8 | uint32(b[1])<<16 | uint32(b[0])<<24\n" +
"}\n" +
"\n" +
"func BEPutUint32(b []byte, v uint32) {\n" +
"	_ = b[3] // early bounds check to guarantee safety of writes below\n" +
"	b[0] = byte(v >> 24)\n" +
"	b[1] = byte(v >> 16)\n" +
"	b[2] = byte(v >> 8)\n" +
"	b[3] = byte(v)\n" +
"}\n" +
"\n" +
"func BEAppendUint32(b []byte, v uint32) []byte {\n" +
"	return append(b,\n" +
"		byte(v>>24),\n" +
"		byte(v>>16),\n" +
"		byte(v>>8),\n" +
"		byte(v),\n" +
"	)\n" +
"}\n" +
"\n" +
"func BEUint64(b []byte) uint64 {\n" +
"	_ = b[7] // bounds check hint to compiler; see golang.org/issue/14808\n" +
"	return uint64(b[7]) | uint64(b[6])<<8 | uint64(b[5])<<16 | uint64(b[4])<<24 |\n" +
"		uint64(b[3])<<32 | uint64(b[2])<<40 | uint64(b[1])<<48 | uint64(b[0])<<56\n" +
"}\n" +
"\n" +
"func BEPutUint64(b []byte, v uint64) {\n" +
"	_ = b[7] // early bounds check to guarantee safety of writes below\n" +
"	b[0] = byte(v >> 56)\n" +
"	b[1] = byte(v >> 48)\n" +
"	b[2] = byte(v >> 40)\n" +
"	b[3] = byte(v >> 32)\n" +
"	b[4] = byte(v >> 24)\n" +
"	b[5] = byte(v >> 16)\n" +
"	b[6] = byte(v >> 8)\n" +
"	b[7] = byte(v)\n" +
"}\n" +
"\n" +
"func BEAppendUint64(b []byte, v uint64) []byte {\n" +
"	return append(b,\n" +
"		byte(v>>56),\n" +
"		byte(v>>48),\n" +
"		byte(v>>40),\n" +
"		byte(v>>32),\n" +
"		byte(v>>24),\n" +
"		byte(v>>16),\n" +
"		byte(v>>8),\n" +
"		byte(v),\n" +
"	)\n" +
"}")
