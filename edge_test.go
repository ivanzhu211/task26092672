package brandprobe

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestSVGWidthOnlyNoViewBox(t *testing.T) {
	_, err := probeBytes(t, svgSample(`<svg width="100"/>`))
	expectErrIs(t, err, ErrNoIntrinsicSize)
}

func TestSVGHeightOnlyNoViewBox(t *testing.T) {
	_, err := probeBytes(t, svgSample(`<svg height="7cm"/>`))
	expectErrIs(t, err, ErrNoIntrinsicSize)
}

func TestEmptyAndTinyInputs(t *testing.T) {
	for _, data := range [][]byte{{}, {0x89}, []byte("%!"), []byte("<")} {
		if _, err := probeBytes(t, data); err == nil {
			t.Fatalf("input %q: expected error", data)
		}
	}
}

func TestEPSDOSBadOffsets(t *testing.T) {
	ps := []byte("%!PS-Adobe-3.0 EPSF-3.0\n%%BoundingBox: 0 0 72 72\n%%EndComments\n")
	good := epsDOSVariant(ps)
	// Corrupt the PS length to run past EOF.
	binary.LittleEndian.PutUint32(good[8:12], 0xFFFFFF)
	_, err := probeBytes(t, good)
	expectErrIs(t, err, ErrCorrupt)
}

func TestTIFFMissingHeight(t *testing.T) {
	data := tiffSample(binary.LittleEndian, false, 640, 480, 0)
	// Truncate the IFD to only the width entry: adjust count to 1 and cut.
	var out bytes.Buffer
	out.Write(data[:10])          // header + count
	out.Truncate(8)               // back to count field
	out.Write([]byte{0x01, 0x00}) // count = 1
	out.Write(data[10 : 10+12])   // width entry only
	out.Write([]byte{0, 0, 0, 0}) // next IFD
	_ = out
	_ = data
	_, err := probeBytes(t, out.Bytes())
	expectErrIs(t, err, ErrCorrupt)
}

func TestJPEGExifAfterSOF(t *testing.T) {
	// APP1 placed after SOF but before SOS must still apply.
	plain := jpegSample(100, 50, 1, false)
	withExif := jpegSample(100, 50, 6, true)
	// Extract the APP1 segment from withExif (starts at offset 2).
	app1Len := int(binary.BigEndian.Uint16(withExif[4:6])) + 2
	app1 := withExif[2 : 2+app1Len]
	// plain: FFD8 then SOF segment; insert app1 after the SOF segment.
	sofLen := int(binary.BigEndian.Uint16(plain[4:6])) + 2
	var merged bytes.Buffer
	merged.Write(plain[:2+sofLen])
	merged.Write(app1)
	merged.Write(plain[2+sofLen:])
	expectDims(t, mustProbe(t, merged.Bytes()), FormatJPEG, 50, 100)
}

func TestReaderAtErrorPropagates(t *testing.T) {
	_, err := Probe(errReaderAt{err: errTestIO}, 100)
	expectErrIs(t, err, ErrCorrupt)
}

var errTestIO = errIO{}

type errIO struct{}

func (errIO) Error() string { return "io failure" }
