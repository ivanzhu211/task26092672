package brandprobe

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// --- sample builders (all samples are synthesized in code) ---

func pngSample(w, h uint32, cgBI bool) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	chunk := func(typ string, data []byte) {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(data)))
		buf.Write(l[:])
		buf.WriteString(typ)
		buf.Write(data)
		buf.Write([]byte{0, 0, 0, 0}) // crc (not validated)
	}
	if cgBI {
		chunk("CgBI", nil)
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // color type
	chunk("IHDR", ihdr)
	chunk("IDAT", []byte{0x78, 0x9C, 0x00})
	chunk("IEND", nil)
	return buf.Bytes()
}

// jpegSample builds a JPEG with a big-endian Exif APP1 (given orientation)
// that embeds a thumbnail JPEG with different dimensions, followed by the
// main SOF0 and SOS.
func jpegSample(w, h uint16, orientation int, withExif bool) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xD8})
	if withExif {
		var app1 bytes.Buffer
		app1.WriteString("Exif\x00\x00")
		app1.WriteString("MM")                  // big-endian TIFF
		app1.Write([]byte{0x00, 0x2A})          // magic 42
		app1.Write([]byte{0x00, 0x00, 0x00, 8}) // IFD0 offset
		// IFD0: two entries (Orientation, thumbnail offset), then next-IFD=0
		app1.Write([]byte{0x00, 0x02})
		entry := func(tag, typ uint16, num, val uint32) {
			var e [12]byte
			binary.BigEndian.PutUint16(e[0:2], tag)
			binary.BigEndian.PutUint16(e[2:4], typ)
			binary.BigEndian.PutUint32(e[4:8], num)
			binary.BigEndian.PutUint32(e[8:12], val)
			app1.Write(e[:])
		}
		entry(0x0112, 3, 1, uint32(orientation)<<16) // SHORT value in high bytes
		thumbOff := uint32(8 + 2 + 2*12 + 4)
		entry(0x0201, 4, 1, thumbOff) // JPEGInterchangeFormat
		app1.Write([]byte{0, 0, 0, 0})
		// Embedded thumbnail JPEG with different dims (99x77).
		thumb := jpegSOFBytes(99, 77)
		app1.Write(thumb)
		seg := app1.Bytes()
		buf.Write([]byte{0xFF, 0xE1})
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(len(seg)+2))
		buf.Write(l[:])
		buf.Write(seg)
	}
	buf.Write(jpegSOFBytes(w, h)[2:]) // reuse SOF segment without SOI
	// SOS
	buf.Write([]byte{0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00})
	buf.Write([]byte{0x11, 0x22, 0x33}) // entropy data
	buf.Write([]byte{0xFF, 0xD9})       // EOI
	return buf.Bytes()
}

// jpegSOFBytes returns SOI + SOF0 segment for the given dimensions.
func jpegSOFBytes(w, h uint16) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xD8})
	buf.Write([]byte{0xFF, 0xC0})
	var seg [15]byte
	binary.BigEndian.PutUint16(seg[0:2], 17)
	seg[2] = 8
	binary.BigEndian.PutUint16(seg[3:5], h)
	binary.BigEndian.PutUint16(seg[5:7], w)
	seg[7] = 3 // components
	buf.Write(seg[:8])
	buf.Write([]byte{1, 0x22, 0, 2, 0x11, 1, 3, 0x11, 1})
	return buf.Bytes()
}

func tiffSample(bo binary.ByteOrder, long bool, w, h uint32, orientation int) []byte {
	var buf bytes.Buffer
	if bo == binary.LittleEndian {
		buf.WriteString("II")
	} else {
		buf.WriteString("MM")
	}
	var tmp [4]byte
	put16 := func(v uint16) { bo.PutUint16(tmp[:2], v); buf.Write(tmp[:2]) }
	put32 := func(v uint32) { bo.PutUint32(tmp[:4], v); buf.Write(tmp[:4]) }
	put16(42)
	put32(8) // IFD0 offset
	n := 2
	if orientation != 0 {
		n = 3
	}
	put16(uint16(n))
	typ := uint16(3)
	if long {
		typ = 4
	}
	entry := func(tag uint16, val uint32) {
		put16(tag)
		put16(typ)
		put32(1)
		if long {
			put32(val)
		} else {
			put16(uint16(val))
			put16(0)
		}
	}
	entry(0x0100, w)
	entry(0x0101, h)
	if orientation != 0 {
		put16(0x0112)
		put16(3)
		put32(1)
		put16(uint16(orientation))
		put16(0)
	}
	put32(0) // next IFD
	return buf.Bytes()
}

func epsDOSVariant(ps []byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xC5, 0xD0, 0xD3, 0xC6})
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], 30)
	buf.Write(tmp[:])
	binary.LittleEndian.PutUint32(tmp[:], uint32(len(ps)))
	buf.Write(tmp[:])
	buf.Write(make([]byte, 18)) // metafile offset/len/checksum zeroed
	buf.Write(ps)
	buf.Write([]byte{0xDE, 0xAD, 0xBE, 0xEF}) // binary preview junk
	return buf.Bytes()
}

func svgSample(body string) []byte { return []byte(body) }

// --- shared test helpers ---

type errReaderAt struct{ err error }

func (e errReaderAt) ReadAt([]byte, int64) (int, error) { return 0, e.err }

func probeBytes(t *testing.T, data []byte) (Result, error) {
	t.Helper()
	return Probe(bytes.NewReader(data), int64(len(data)))
}

func mustProbe(t *testing.T, data []byte) Result {
	t.Helper()
	r, err := probeBytes(t, data)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	return r
}

func expectDims(t *testing.T, r Result, f Format, w, h float64) {
	t.Helper()
	if r.Format != f || r.Width != w || r.Height != h {
		t.Fatalf("got %v %vx%v, want %v %vx%v", r.Format, r.Width, r.Height, f, w, h)
	}
}

func expectErrIs(t *testing.T, err error, target error) {
	t.Helper()
	if err == nil || !errors.Is(err, target) {
		t.Fatalf("got error %v, want errors.Is %v", err, target)
	}
}
