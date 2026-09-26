package probe

import "encoding/binary"

// parseJPEG walks JPEG segments to the first SOF for dimensions and
// applies the Orientation from the first Exif APP1 segment's IFD0.
func (p *parser) parseJPEG() (Result, error) {
	pos := int64(2) // skip SOI
	var w, h uint16
	haveSOF := false
	orient := 1
	exifSeen := false
	for {
		marker, next, err := p.jpegMarker(pos)
		if err != nil {
			return Result{}, err
		}
		pos = next
		switch {
		case marker == 0xD9: // EOI
			return Result{}, ErrCorrupt
		case marker == 0xDA: // SOS: entropy-coded data follows
			if !haveSOF {
				return Result{}, ErrCorrupt
			}
			return jpegResult(w, h, orient), nil
		case marker == 0x01 || marker >= 0xD0 && marker <= 0xD7:
			// Standalone marker without a length field.
			continue
		}
		segLen, err := p.jpegSegLen(pos)
		if err != nil {
			return Result{}, err
		}
		switch {
		case isSOF(marker):
			if segLen < 7 {
				return Result{}, ErrCorrupt
			}
			var sof [5]byte
			if err := p.readAt(pos+2, sof[:]); err != nil {
				return Result{}, err
			}
			h = binary.BigEndian.Uint16(sof[1:3])
			w = binary.BigEndian.Uint16(sof[3:5])
			if h == 0 {
				// Height deferred to a DNL segment.
				return Result{}, ErrUnsupported
			}
			if w == 0 {
				return Result{}, ErrCorrupt
			}
			haveSOF = true
		case marker == 0xE1 && !exifSeen: // APP1
			o, ok, err := p.jpegExifOrientation(pos+2, pos+segLen)
			if err != nil {
				return Result{}, err
			}
			if ok {
				orient = o
				exifSeen = true
			}
		}
		pos += segLen
	}
}

func isSOF(m byte) bool {
	switch m {
	case 0xC0, 0xC1, 0xC2, 0xC3, 0xC5, 0xC6, 0xC7,
		0xC9, 0xCA, 0xCB, 0xCD, 0xCE, 0xCF:
		return true
	}
	return false
}

// jpegMarker reads the marker at pos, skipping 0xFF fill bytes, and
// returns the marker code and the offset just past it.
func (p *parser) jpegMarker(pos int64) (byte, int64, error) {
	var b [1]byte
	if err := p.readAt(pos, b[:]); err != nil {
		return 0, 0, err
	}
	if b[0] != 0xFF {
		return 0, 0, ErrCorrupt
	}
	pos++
	for {
		if err := p.readAt(pos, b[:]); err != nil {
			return 0, 0, err
		}
		if b[0] != 0xFF {
			break
		}
		pos++
	}
	if b[0] == 0x00 {
		return 0, 0, ErrCorrupt
	}
	return b[0], pos + 1, nil
}

// jpegSegLen reads the 2-byte big-endian segment length at pos and
// returns the full segment size including the length field itself.
func (p *parser) jpegSegLen(pos int64) (int64, error) {
	var b [2]byte
	if err := p.readAt(pos, b[:]); err != nil {
		return 0, err
	}
	n := int64(binary.BigEndian.Uint16(b[:]))
	if n < 2 {
		return 0, ErrCorrupt
	}
	return n, nil
}

// jpegExifOrientation parses an APP1 segment spanning [start, end) and
// returns the IFD0 Orientation if the segment carries Exif data.
// Malformed Exif payloads yield the default orientation 1.
func (p *parser) jpegExifOrientation(start, end int64) (int, bool, error) {
	if end-start < 8 {
		return 0, false, nil
	}
	var head [8]byte
	if err := p.readAt(start, head[:]); err != nil {
		return 0, false, err
	}
	if string(head[0:6]) != "Exif\x00\x00" {
		return 0, false, nil
	}
	tiff := start + 6
	var bo binary.ByteOrder
	switch {
	case head[6] == 'I' && head[7] == 'I':
		bo = binary.LittleEndian
	case head[6] == 'M' && head[7] == 'M':
		bo = binary.BigEndian
	default:
		return 1, true, nil
	}
	var rest [6]byte
	if err := p.readAt(tiff+2, rest[:]); err != nil {
		return 0, false, err
	}
	if bo.Uint16(rest[0:2]) != 42 {
		return 1, true, nil
	}
	ifd0 := tiff + int64(bo.Uint32(rest[2:6]))
	if ifd0 < tiff+8 || ifd0+2 > end {
		return 1, true, nil
	}
	var cntBuf [2]byte
	if err := p.readAt(ifd0, cntBuf[:]); err != nil {
		return 0, false, err
	}
	count := int64(bo.Uint16(cntBuf[:]))
	if count > 256 || ifd0+2+count*12 > end {
		return 1, true, nil
	}
	entries := make([]byte, count*12)
	if err := p.readAt(ifd0+2, entries); err != nil {
		return 0, false, err
	}
	for i := int64(0); i < count; i++ {
		e := entries[i*12:]
		if bo.Uint16(e[0:2]) != tagOrientation {
			continue
		}
		if bo.Uint16(e[2:4]) != typeSHORT || bo.Uint32(e[4:8]) != 1 {
			return 1, true, nil
		}
		o := int(bo.Uint16(e[8:10]))
		if o < 1 || o > 8 {
			o = 1
		}
		return o, true, nil
	}
	return 1, true, nil
}

func jpegResult(w, h uint16, orient int) Result {
	r := Result{Format: FormatJPEG, Width: float64(w), Height: float64(h)}
	if orient >= 5 && orient <= 8 {
		r.Width, r.Height = r.Height, r.Width
	}
	return r
}
