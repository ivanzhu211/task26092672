package probe

import (
	"io"
	"strings"
)

// be16/be32 append big-endian values.
func be16(b []byte, v uint16) []byte { return append(b, byte(v>>8), byte(v)) }
func be32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
func le16(b []byte, v uint16) []byte { return append(b, byte(v), byte(v>>8)) }
func le32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// pngChunk builds one PNG chunk (CRC is zeroed; the parser ignores it).
func pngChunk(typ string, data []byte) []byte {
	out := be32(nil, uint32(len(data)))
	out = append(out, typ...)
	out = append(out, data...)
	return append(out, 0, 0, 0, 0)
}

// buildPNG creates a PNG, optionally with a CgBI chunk before IHDR.
func buildPNG(w, h uint32, cgbi bool) []byte {
	out := append([]byte{}, pngMagic...)
	if cgbi {
		out = append(out, pngChunk("CgBI", nil)...)
	}
	ihdr := be32(nil, w)
	ihdr = be32(ihdr, h)
	ihdr = append(ihdr, 8, 6, 0, 0, 0) // depth, color, etc.
	out = append(out, pngChunk("IHDR", ihdr)...)
	out = append(out, pngChunk("IDAT", []byte{1, 2, 3})...)
	out = append(out, pngChunk("IEND", nil)...)
	return out
}

// jpegSegment builds a marker segment with a length field.
func jpegSegment(marker byte, data []byte) []byte {
	out := []byte{0xFF, marker}
	out = be16(out, uint16(len(data)+2))
	return append(out, data...)
}

// buildExifAPP1 builds an Exif APP1 payload (big-endian TIFF) with the
// given IFD0 orientation and an embedded thumbnail JPEG in IFD1.
func buildExifAPP1(orient uint16, thumbnail []byte) []byte {
	// TIFF header: MM, 42, IFD0 at offset 8.
	var tiff []byte
	tiff = append(tiff, 'M', 'M')
	tiff = be16(tiff, 42)
	tiff = be32(tiff, 8)
	// IFD0: one entry (Orientation), then offset to IFD1.
	ifd0 := be16(nil, 1)
	ifd0 = be16(ifd0, tagOrientation)
	ifd0 = be16(ifd0, typeSHORT)
	ifd0 = be32(ifd0, 1)
	ifd0 = be32(ifd0, uint32(orient)<<16)
	ifd1Off := 8 + 2 + 12 + 4
	ifd0 = be32(ifd0, uint32(ifd1Off))
	tiff = append(tiff, ifd0...)
	// IFD1: compression, thumbnail offset, thumbnail length.
	thumbOff := ifd1Off + 2 + 3*12 + 4
	ifd1 := be16(nil, 3)
	ifd1 = be16(ifd1, 0x0103) // Compression
	ifd1 = be16(ifd1, typeSHORT)
	ifd1 = be32(ifd1, 1)
	ifd1 = be32(ifd1, 6<<16)  // JPEG
	ifd1 = be16(ifd1, 0x0201) // JPEGInterchangeFormat
	ifd1 = be16(ifd1, typeLONG)
	ifd1 = be32(ifd1, 1)
	ifd1 = be32(ifd1, uint32(thumbOff))
	ifd1 = be16(ifd1, 0x0202) // JPEGInterchangeFormatLength
	ifd1 = be16(ifd1, typeLONG)
	ifd1 = be32(ifd1, 1)
	ifd1 = be32(ifd1, uint32(len(thumbnail)))
	ifd1 = be32(ifd1, 0)
	tiff = append(tiff, ifd1...)
	tiff = append(tiff, thumbnail...)
	return append([]byte("Exif\x00\x00"), tiff...)
}

// buildJPEG creates a JPEG whose main image is w x h, with an Exif
// APP1 (orientation) that embeds a thumbnail of a different size.
func buildJPEG(w, h uint16, orient uint16, withExif bool) []byte {
	thumb := []byte{0xFF, 0xD8}
	sof := []byte{8}
	sof = be16(sof, 7)   // thumbnail height
	sof = be16(sof, 13)  // thumbnail width
	sof = append(sof, 1) // components
	thumb = append(thumb, jpegSegment(0xC0, sof)...)
	thumb = append(thumb, 0xFF, 0xD9)

	out := []byte{0xFF, 0xD8}
	out = append(out, jpegSegment(0xE0, []byte("JFIF\x00\x01\x01"))...)
	if withExif {
		out = append(out, jpegSegment(0xE1, buildExifAPP1(orient, thumb))...)
	}
	main := []byte{8}
	main = be16(main, h)
	main = be16(main, w)
	main = append(main, 3)
	out = append(out, jpegSegment(0xC0, main)...)
	out = append(out, jpegSegment(0xDA, []byte{1, 2, 3})...)
	out = append(out, 0x00, 0x11, 0x22) // entropy data
	out = append(out, 0xFF, 0xD9)
	return out
}

// buildTIFF creates a classic TIFF with one IFD.
func buildTIFF(bigEndian bool, w, h uint32, useLong bool, orient int) []byte {
	var out []byte
	var a16 func([]byte, uint16) []byte
	var a32 func([]byte, uint32) []byte
	if bigEndian {
		out = []byte{'M', 'M'}
		a16, a32 = be16, be32
	} else {
		out = []byte{'I', 'I'}
		a16, a32 = le16, le32
	}
	out = a16(out, 42)
	out = a32(out, 8)
	typeOf := uint16(typeSHORT)
	if useLong {
		typeOf = typeLONG
	}
	entry := func(tag uint16, typ uint16, val uint32) []byte {
		e := a16(nil, tag)
		e = a16(e, typ)
		e = a32(e, 1)
		if typ == typeSHORT {
			e = a16(e, uint16(val))
			e = a16(e, 0)
		} else {
			e = a32(e, val)
		}
		return e
	}
	n := 2
	if orient > 0 {
		n = 3
	}
	ifd := a16(nil, uint16(n))
	ifd = append(ifd, entry(tagImageWidth, typeOf, w)...)
	ifd = append(ifd, entry(tagImageLength, typeOf, h)...)
	if orient > 0 {
		ifd = append(ifd, entry(tagOrientation, typeSHORT, uint32(orient))...)
	}
	ifd = a32(ifd, 0)
	return append(out, ifd...)
}

// buildEPS wraps PostScript source, optionally in the DOS binary
// EPS header with a dummy preview section.
func buildEPS(ps string, dosBinary bool) []byte {
	if !dosBinary {
		return []byte(ps)
	}
	head := append([]byte{}, epsDOSMagic...)
	head = le32(head, 30)              // PS section offset
	head = le32(head, uint32(len(ps))) // PS section length
	head = le32(head, 0)               // WMF offset
	head = le32(head, 0)               // WMF length
	head = le32(head, 0)               // TIFF offset
	head = le32(head, 0)               // TIFF length
	head = le16(head, 0xFFFF)
	return append(head, ps...)
}

// countingReader wraps a ReaderAt and counts bytes actually read.
type countingReader struct {
	r    io.ReaderAt
	read int64
}

func (c *countingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.read += int64(n)
	return n, err
}

// synthReader synthesizes content on demand for huge virtual files.
type synthReader struct {
	size int64
	fn   func(off int64, p []byte)
}

func (s *synthReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= s.size {
		return 0, io.EOF
	}
	if off+int64(len(p)) > s.size {
		n := int(s.size - off)
		s.fn(off, p[:n])
		return n, io.EOF
	}
	s.fn(off, p)
	return len(p), nil
}

// epsSample returns a canonical EPS text with the given line ending.
func epsSample(eol string) string {
	var b strings.Builder
	b.WriteString("%!PS-Adobe-3.0 EPSF-3.0" + eol)
	b.WriteString("%%Creator: probe-test" + eol)
	b.WriteString("%%BoundingBox: 0 0 144 72" + eol)
	b.WriteString("%%HiResBoundingBox: 0 0 144.5 72.25" + eol)
	b.WriteString("%%EndComments" + eol)
	b.WriteString("1 2 moveto" + eol)
	b.WriteString("%%EOF" + eol)
	return b.String()
}
