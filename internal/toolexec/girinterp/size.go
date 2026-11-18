package girinterp

// gc's amd64 layout and malloc size classes (internal/runtime/gc), for
// append's capacity growth, which programs can observe through cap().

var sizeClassToSize = [...]uint64{0, 8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256, 288, 320, 352, 384, 416, 448, 480, 512, 576, 640, 704, 768, 896, 1024, 1152, 1280, 1408, 1536, 1792, 2048, 2304, 2688, 3072, 3200, 3456, 4096, 4864, 5376, 6144, 6528, 6784, 6912, 8192, 9472, 9728, 10240, 10880, 12288, 13568, 14336, 16384, 18432, 19072, 20480, 21760, 24576, 27264, 28672, 32768}

const (
	maxSmallSize           = 32768
	mallocHeaderSize       = 8
	minSizeForMallocHeader = 512
	pageSize               = 8192
)

// roundupsize is runtime.roundupsize.
func roundupsize(size uint64, noscan bool) uint64 {
	req := size
	if req <= maxSmallSize-mallocHeaderSize {
		if !noscan && req > minSizeForMallocHeader {
			req += mallocHeaderSize
		}
		for _, c := range sizeClassToSize[1:] {
			if c >= req {
				return c - (req - size)
			}
		}
	}
	req += pageSize - 1
	return req &^ (pageSize - 1)
}

func (in *Interp) sizeof(t *RType) int64 {
	s, _ := in.layout(t)
	return s
}

// layout returns gc's size and alignment of t on amd64.
func (in *Interp) layout(t *RType) (size, align int64) {
	u := t.Under()
	switch u.Kind {
	case KBool, KInt8, KUint8:
		return 1, 1
	case KInt16, KUint16:
		return 2, 2
	case KInt32, KUint32, KFloat32:
		return 4, 4
	case KComplex64:
		return 8, 4
	case KInt, KInt64, KUint, KUint64, KUintptr, KFloat64, KPointer, KMap, KChan, KFunc, KUnsafePointer:
		return 8, 8
	case KComplex128:
		return 16, 8
	case KString, KInterface:
		return 16, 8
	case KSlice:
		return 24, 8
	case KArray:
		es, ea := in.layout(u.Elem)
		return es * u.Len, ea
	case KStruct:
		var off, maxAlign int64 = 0, 1
		for _, f := range u.Fields {
			fs, fa := in.layout(f.Type)
			off = (off + fa - 1) / fa * fa
			off += fs
			maxAlign = max(maxAlign, fa)
		}
		// a trailing zero-size field gets one byte of padding (gc)
		if n := len(u.Fields); n > 0 && off > 0 {
			if fs, _ := in.layout(u.Fields[n-1].Type); fs == 0 {
				off++
			}
		}
		return (off + maxAlign - 1) / maxAlign * maxAlign, maxAlign
	}
	return 8, 8
}

func (in *Interp) hasPointers(t *RType) bool {
	u := t.Under()
	switch u.Kind {
	case KString, KInterface, KPointer, KSlice, KMap, KChan, KFunc, KUnsafePointer:
		return true
	case KArray:
		return u.Len > 0 && in.hasPointers(u.Elem)
	case KStruct:
		for _, f := range u.Fields {
			if in.hasPointers(f.Type) {
				return true
			}
		}
	}
	return false
}
