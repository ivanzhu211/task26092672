package brandprobe

import "encoding/binary"

// probeJPEG walks JPEG marker segments structurally until the start of
// scan (SOS). It records the main SOF dimensions and the Orientation from
// the first Exif APP1 segment. Entropy-coded data is never parsed.
func probeJPEG(b *budgetReader) (Result, error) {
	off := int64(2) // skip SOI
	var w, h uint32
	sofFound := false
	exifSeen := false
	orientation := 1

	for {
		// Locate the next marker: 0xFF followed by a non-0xFF, non-0x00 byte.
		fb, err := b.u8(off)
		if err != nil {
			return Result{}, err
		}
		off++
		if fb != 0xFF {
			return Result{}, ErrCorrupt
		}
		var m uint8
		for {
			if m, err = b.u8(off); err != nil {
				return Result{}, err
			}
			off++
			if m != 0xFF {
				break
			}
		}
		if m == 0x00 {
			return Result{}, ErrCorrupt // stuffed byte outside entropy data
		}
		switch {
		case m == 0xD8:
			return Result{}, ErrCorrupt // duplicate SOI
		case m == 0x01 || (m >= 0xD0 && m <= 0xD7):
			continue // standalone marker without length
		case m == 0xD9:
			return Result{}, ErrCorrupt // EOI before SOS
		}

		segLen, err := b.u16(off, binary.BigEndian)
		if err != nil {
			return Result{}, err
		}
		if segLen < 2 {
			return Result{}, ErrCorrupt
		}
		dataOff := off + 2
		dataLen := int64(segLen) - 2
		if dataOff+dataLen > b.size {
			return Result{}, ErrCorrupt
		}

		isSOF := m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC
		switch {
		case isSOF:
			if dataLen < 5 {
				return Result{}, ErrCorrupt
			}
			data, err := b.bytes(dataOff, 5)
			if err != nil {
				return Result{}, err
			}
			h = uint32(binary.BigEndian.Uint16(data[1:3]))
			w = uint32(binary.BigEndian.Uint16(data[3:5]))
			if h == 0 {
				// Height deferred to a DNL segment: unsupported.
				return Result{}, ErrUnsupported
			}
			if w == 0 {
				return Result{}, ErrCorrupt
			}
			sofFound = true
		case m == 0xE1 && !exifSeen:
			if o, err := exifOrientation(b, dataOff, dataLen); err != nil {
				return Result{}, err
			} else if o != 0 {
				orientation = o
			}
			exifSeen = true
		case m == 0xDA: // SOS: entropy data follows, stop here.
			if !sofFound {
				return Result{}, ErrCorrupt
			}
			rw, rh := w, h
			if orientation >= 5 && orientation <= 8 {
				rw, rh = rh, rw
			}
			return Result{Format: FormatJPEG, Width: float64(rw), Height: float64(rh)}, nil
		}
		off = dataOff + dataLen
	}
}

// exifOrientation reads the Orientation from IFD0 of an Exif APP1 segment
// located at [off, off+length). It returns 0 when the segment is not Exif
// or carries no usable Orientation (treated as 1 by the caller). Structural
// truncation of the segment yields ErrCorrupt; a malformed or out-of-range
// Orientation value is treated as 1 per spec.
func exifOrientation(b *budgetReader, off, length int64) (int, error) {
	const sigLen = 6 // "Exif\0\0"
	if length < sigLen+8 {
		return 0, nil
	}
	head, err := b.bytes(off, sigLen+8)
	if err != nil {
		return 0, err
	}
	if string(head[:sigLen]) != "Exif\x00\x00" {
		return 0, nil
	}
	tiff := head[sigLen:]
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0, nil
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return 0, nil
	}
	ifd0 := int64(bo.Uint32(tiff[4:8]))
	if ifd0 < 8 || ifd0+2 > length-sigLen {
		return 0, nil
	}
	cntBuf, err := b.bytes(off+sigLen+ifd0, 2)
	if err != nil {
		return 0, err
	}
	count := int64(bo.Uint16(cntBuf))
	if count <= 0 {
		return 0, nil
	}
	if count > (length-sigLen-ifd0-2)/12 {
		return 0, nil // IFD0 extends past the segment: treat as absent
	}
	entries, err := b.bytes(off+sigLen+ifd0+2, count*12)
	if err != nil {
		return 0, err
	}
	for i := int64(0); i < count; i++ {
		e := entries[i*12:]
		tag := bo.Uint16(e[0:2])
		typ := bo.Uint16(e[2:4])
		num := bo.Uint32(e[4:8])
		if tag == 0x0112 && typ == 3 && num >= 1 {
			v := int(bo.Uint16(e[8:10]))
			if v < 1 || v > 8 {
				v = 1
			}
			return v, nil
		}
	}
	return 0, nil
}
