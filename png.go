package probe

import "encoding/binary"

// parsePNG reads the IHDR chunk, tolerating ancillary chunks such as
// CgBI (iOS-optimized variant) before it.
func (p *parser) parsePNG() (Result, error) {
	off := int64(len(pngMagic))
	var hdr [8]byte
	for {
		if err := p.readAt(off, hdr[:]); err != nil {
			return Result{}, err
		}
		length := binary.BigEndian.Uint32(hdr[0:4])
		// Chunk data plus its 4-byte CRC must fit in the file.
		if uint64(length)+4 > uint64(p.size)-uint64(off)-8 {
			return Result{}, ErrCorrupt
		}
		if string(hdr[4:8]) == "IHDR" {
			if length < 8 {
				return Result{}, ErrCorrupt
			}
			var ihdr [8]byte
			if err := p.readAt(off+8, ihdr[:]); err != nil {
				return Result{}, err
			}
			w := binary.BigEndian.Uint32(ihdr[0:4])
			h := binary.BigEndian.Uint32(ihdr[4:8])
			if w == 0 || h == 0 {
				return Result{}, ErrCorrupt
			}
			return Result{
				Format: FormatPNG,
				Width:  float64(w),
				Height: float64(h),
			}, nil
		}
		off += 8 + int64(length) + 4
	}
}
