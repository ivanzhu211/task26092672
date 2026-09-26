package probe

import "encoding/binary"

const (
	tagImageWidth  = 256
	tagImageLength = 257
	tagOrientation = 274

	typeSHORT = 3
	typeLONG  = 4
)

// parseTIFF reads the first IFD of a classic TIFF (II or MM byte
// order). BigTIFF is rejected during sniffing.
func (p *parser) parseTIFF() (Result, error) {
	var head [8]byte
	if err := p.readAt(0, head[:]); err != nil {
		return Result{}, err
	}
	var bo binary.ByteOrder
	switch {
	case head[0] == 'I' && head[1] == 'I':
		bo = binary.LittleEndian
	case head[0] == 'M' && head[1] == 'M':
		bo = binary.BigEndian
	default:
		return Result{}, ErrCorrupt
	}
	if bo.Uint16(head[2:4]) != 42 {
		return Result{}, ErrCorrupt
	}
	ifdOff := int64(bo.Uint32(head[4:8]))
	w, h, orient, err := p.readTIFFIFD(bo, ifdOff)
	if err != nil {
		return Result{}, err
	}
	if orient >= 5 && orient <= 8 {
		w, h = h, w
	}
	return Result{Format: FormatTIFF, Width: w, Height: h}, nil
}

// readTIFFIFD parses the IFD at ifdOff and returns width, height and
// orientation (defaulting to 1) in pixels.
func (p *parser) readTIFFIFD(bo binary.ByteOrder, ifdOff int64) (w, h float64, orient int, err error) {
	orient = 1
	var cntBuf [2]byte
	if e := p.readAt(ifdOff, cntBuf[:]); e != nil {
		return 0, 0, 0, e
	}
	count := bo.Uint16(cntBuf[:])
	if count == 0 || count > 4096 {
		return 0, 0, 0, ErrCorrupt
	}
	entries := make([]byte, int(count)*12)
	if e := p.readAt(ifdOff+2, entries); e != nil {
		return 0, 0, 0, e
	}
	var haveW, haveH, haveO bool
	var wv, hv uint32
	for i := 0; i < int(count); i++ {
		e := entries[i*12:]
		tag := bo.Uint16(e[0:2])
		typ := bo.Uint16(e[2:4])
		num := bo.Uint32(e[4:8])
		val := e[8:12]
		switch tag {
		case tagImageWidth, tagImageLength:
			if haveW && tag == tagImageWidth || haveH && tag == tagImageLength {
				continue
			}
			v, e2 := tiffIntValue(bo, typ, num, val)
			if e2 != nil {
				return 0, 0, 0, e2
			}
			if v == 0 {
				return 0, 0, 0, ErrCorrupt
			}
			if tag == tagImageWidth {
				wv, haveW = v, true
			} else {
				hv, haveH = v, true
			}
		case tagOrientation:
			if haveO || typ != typeSHORT || num != 1 {
				continue
			}
			o := int(bo.Uint16(val[0:2]))
			if o >= 1 && o <= 8 {
				orient = o
			}
			haveO = true
		}
	}
	if !haveW || !haveH {
		return 0, 0, 0, ErrCorrupt
	}
	return float64(wv), float64(hv), orient, nil
}

// tiffIntValue extracts a single SHORT or LONG value stored inline in
// the 4-byte value field of an IFD entry.
func tiffIntValue(bo binary.ByteOrder, typ uint16, num uint32, val []byte) (uint32, error) {
	if num != 1 {
		return 0, ErrUnsupported
	}
	switch typ {
	case typeSHORT:
		return uint32(bo.Uint16(val[0:2])), nil
	case typeLONG:
		return bo.Uint32(val[0:4]), nil
	}
	return 0, ErrUnsupported
}
