package brandprobe

import "encoding/binary"

// probeTIFF reads the first IFD of a classic TIFF (II or MM byte order).
// Width and height may be SHORT or LONG. Orientation follows the same
// rules as JPEG. BigTIFF is rejected during format detection.
func probeTIFF(b *budgetReader) (Result, error) {
	head, err := b.bytes(0, 8)
	if err != nil {
		return Result{}, err
	}
	var bo binary.ByteOrder
	switch string(head[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return Result{}, ErrCorrupt
	}
	if bo.Uint16(head[2:4]) != 42 {
		return Result{}, ErrCorrupt
	}
	ifd := int64(bo.Uint32(head[4:8]))
	if ifd < 8 {
		return Result{}, ErrCorrupt
	}
	cntBuf, err := b.bytes(ifd, 2)
	if err != nil {
		return Result{}, err
	}
	count := int64(bo.Uint16(cntBuf))
	if count <= 0 {
		return Result{}, ErrCorrupt
	}
	entries, err := b.bytes(ifd+2, count*12)
	if err != nil {
		return Result{}, err
	}

	var w, h uint32
	haveW, haveH := false, false
	orientation := 1
	for i := int64(0); i < count; i++ {
		e := entries[i*12:]
		tag := bo.Uint16(e[0:2])
		typ := bo.Uint16(e[2:4])
		num := bo.Uint32(e[4:8])
		switch tag {
		case 0x0100, 0x0101:
			v, ok := tiffShortOrLong(e, bo, typ, num)
			if !ok {
				return Result{}, ErrCorrupt
			}
			if v == 0 {
				return Result{}, ErrCorrupt
			}
			if tag == 0x0100 {
				w, haveW = v, true
			} else {
				h, haveH = v, true
			}
		case 0x0112:
			v, ok := tiffShortOrLong(e, bo, typ, num)
			if !ok || v < 1 || v > 8 {
				v = 1
			}
			orientation = int(v)
		}
	}
	if !haveW || !haveH {
		return Result{}, ErrCorrupt
	}
	if orientation >= 5 && orientation <= 8 {
		w, h = h, w
	}
	return Result{Format: FormatTIFF, Width: float64(w), Height: float64(h)}, nil
}

// tiffShortOrLong extracts a single SHORT or LONG value stored inline in
// the 4-byte value field of an IFD entry.
func tiffShortOrLong(e []byte, bo binary.ByteOrder, typ uint16, num uint32) (uint32, bool) {
	if num != 1 {
		return 0, false
	}
	switch typ {
	case 3: // SHORT
		return uint32(bo.Uint16(e[8:10])), true
	case 4: // LONG
		return bo.Uint32(e[8:12]), true
	}
	return 0, false
}
