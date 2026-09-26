package brandprobe

import "encoding/binary"

// probePNG walks PNG chunks structurally (no byte-pattern scanning) until
// it reaches IHDR. A CgBI chunk before IHDR (iOS variant) is skipped like
// any other ancillary chunk.
func probePNG(b *budgetReader) (Result, error) {
	off := int64(len(pngMagic))
	for {
		var hdr [8]byte
		if err := b.readAt(hdr[:], off); err != nil {
			return Result{}, err
		}
		length := int64(binary.BigEndian.Uint32(hdr[0:4]))
		ctype := string(hdr[4:8])
		if length < 0 || off+8+length+4 > b.size {
			return Result{}, ErrCorrupt
		}
		if ctype == "IHDR" {
			if length < 13 {
				return Result{}, ErrCorrupt
			}
			data, err := b.bytes(off+8, 8)
			if err != nil {
				return Result{}, err
			}
			w := binary.BigEndian.Uint32(data[0:4])
			h := binary.BigEndian.Uint32(data[4:8])
			if w == 0 || h == 0 {
				return Result{}, ErrCorrupt
			}
			return Result{Format: FormatPNG, Width: float64(w), Height: float64(h)}, nil
		}
		off += 8 + length + 4
	}
}
