package main

import (
	"testing"
)

// TestM7CompressDesugarWiring pins the 32 P3-M9 tier-5 m7MultiResultFuncs
// entries added for the synthetic compress/* packages (Appendix M).
// It follows the TestM7StrconvJsonDesugarWiring pattern: for each entry
// it verifies, against the synthetic sources in
// synth_compress_{flate,gzip,zlib,lzw,bzip2}.go:
//
//  1. exactly one table entry exists for (pkgPath, name, recvType), with
//     the expected helper name, pair type, and pair fields in order;
//  2. the multi-result function (or method on the named receiver type)
//     exists in the synthetic package scope;
//  3. the __-helper exists in the synthetic package scope and is a
//     *types.Func;
//  4. the helper's parameters accept the multi-result call's arguments
//     positionally (the desugar passes the original call's args straight
//     through, receiver prepended for methods);
//  5. the pair struct exists with exactly the listed fields in order,
//     each field's type identical to the multi-result signature's
//     corresponding result type.
type m7CompressExpect struct {
	pkgPath    string
	name       string
	recvType   string
	helper     string
	pairType   string
	pairFields []string
}

var m7CompressTable = []m7CompressExpect{
	// compress/flate.
	{"compress/flate", "findMatch", "compressor", "__flateCompressorfindMatch", "__flateCompressorfindMatchPair", []string{"length", "offset", "ok"}},
	{"compress/flate", "tryBetterMatchAtEnd", "compressor", "__flateCompressortryBetterMatchAtEnd", "__flateCompressortryBetterMatchAtEndPair", []string{"newLen", "newOff"}},
	{"compress/flate", "write", "compressor", "__flateCompressorwrite", "__flateCompressorwritePair", []string{"n", "err"}},
	{"compress/flate", "NewWriter", "", "__flateNewWriter", "__flateNewWriterPair", []string{"w", "err"}},
	{"compress/flate", "NewWriterDict", "", "__flateNewWriterDict", "__flateNewWriterDictPair", []string{"w", "err"}},
	{"compress/flate", "Write", "Writer", "__flateWriterWrite", "__flateWriterWritePair", []string{"n", "err"}},
	{"compress/flate", "Read", "decompressor", "__flateDecompressorRead", "__flateDecompressorReadPair", []string{"n", "err"}},
	{"compress/flate", "huffSym", "decompressor", "__flateDecompressorhuffSym", "__flateDecompressorhuffSymPair", []string{"sym", "err"}},
	{"compress/flate", "headerSize", "huffmanBitWriter", "__flateHuffmanBitWriterheaderSize", "__flateHuffmanBitWriterheaderSizePair", []string{"size", "numCodegens"}},
	{"compress/flate", "dynamicSize", "huffmanBitWriter", "__flateHuffmanBitWriterdynamicSize", "__flateHuffmanBitWriterdynamicSizePair", []string{"size", "numCodegens"}},
	{"compress/flate", "storedSize", "huffmanBitWriter", "__flateHuffmanBitWriterstoredSize", "__flateHuffmanBitWriterstoredSizePair", []string{"size", "ok"}},
	{"compress/flate", "indexTokens", "huffmanBitWriter", "__flateHuffmanBitWriterindexTokens", "__flateHuffmanBitWriterindexTokensPair", []string{"numLiterals", "numOffsets"}},
	// compress/gzip.
	{"compress/gzip", "NewReader", "", "__gzipNewReader", "__gzipNewReaderPair", []string{"r", "err"}},
	{"compress/gzip", "readString", "Reader", "__gzipReaderreadString", "__gzipReaderreadStringPair", []string{"s", "err"}},
	{"compress/gzip", "readHeader", "Reader", "__gzipReaderreadHeader", "__gzipReaderreadHeaderPair", []string{"hdr", "err"}},
	{"compress/gzip", "Read", "Reader", "__gzipReaderRead", "__gzipReaderReadPair", []string{"n", "err"}},
	{"compress/gzip", "NewWriterLevel", "", "__gzipNewWriterLevel", "__gzipNewWriterLevelPair", []string{"w", "err"}},
	{"compress/gzip", "Write", "Writer", "__gzipWriterWrite", "__gzipWriterWritePair", []string{"n", "err"}},
	// compress/zlib.
	{"compress/zlib", "NewReader", "", "__zlibNewReader", "__zlibNewReaderPair", []string{"r", "err"}},
	{"compress/zlib", "NewReaderDict", "", "__zlibNewReaderDict", "__zlibNewReaderDictPair", []string{"r", "err"}},
	{"compress/zlib", "Read", "reader", "__zlibReaderRead", "__zlibReaderReadPair", []string{"n", "err"}},
	{"compress/zlib", "NewWriterLevel", "", "__zlibNewWriterLevel", "__zlibNewWriterLevelPair", []string{"w", "err"}},
	{"compress/zlib", "NewWriterLevelDict", "", "__zlibNewWriterLevelDict", "__zlibNewWriterLevelDictPair", []string{"w", "err"}},
	{"compress/zlib", "Write", "Writer", "__zlibWriterWrite", "__zlibWriterWritePair", []string{"n", "err"}},
	// compress/lzw.
	{"compress/lzw", "readLSB", "Reader", "__lzwReaderreadLSB", "__lzwReaderreadLSBPair", []string{"code", "err"}},
	{"compress/lzw", "readMSB", "Reader", "__lzwReaderreadMSB", "__lzwReaderreadMSBPair", []string{"code", "err"}},
	{"compress/lzw", "Read", "Reader", "__lzwReaderRead", "__lzwReaderReadPair", []string{"n", "err"}},
	{"compress/lzw", "Write", "Writer", "__lzwWriterWrite", "__lzwWriterWritePair", []string{"n", "err"}},
	// compress/bzip2.
	{"compress/bzip2", "Read", "reader", "__bzip2ReaderRead", "__bzip2ReaderReadPair", []string{"n", "err"}},
	{"compress/bzip2", "read", "reader", "__bzip2Readerread", "__bzip2ReaderreadPair", []string{"n", "err"}},
	{"compress/bzip2", "newHuffmanTree", "", "__bzip2NewHuffmanTree", "__bzip2NewHuffmanTreePair", []string{"t", "err"}},
	{"compress/bzip2", "buildHuffmanNode", "", "__bzip2BuildHuffmanNode", "__bzip2BuildHuffmanNodePair", []string{"nodeIndex", "err"}},
}

func TestM7CompressDesugarWiring(t *testing.T) {
	if len(m7CompressTable) != 32 {
		t.Fatalf("m7CompressTable has %d entries, want 32", len(m7CompressTable))
	}
	for _, want := range m7CompressTable {
		// Reuse the shared checker by converting to the strconv/json
		// expectation type (identical shape).
		wantSJ := m7StrconvJsonExpect{
			pkgPath:    want.pkgPath,
			name:       want.name,
			recvType:   want.recvType,
			helper:     want.helper,
			pairType:   want.pairType,
			pairFields: want.pairFields,
		}
		name := want.pkgPath + "." + want.name
		if want.recvType != "" {
			name = want.pkgPath + "." + want.recvType + "." + want.name
		}
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, wantSJ)
		})
	}
}
