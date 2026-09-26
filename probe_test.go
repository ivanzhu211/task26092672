package probe

import (
	"bytes"
	"errors"
	"testing"
)

func probeBytes(t *testing.T, b []byte) (Result, error) {
	t.Helper()
	return Probe(bytes.NewReader(b), int64(len(b)))
}

func mustProbe(t *testing.T, b []byte) Result {
	t.Helper()
	r, err := probeBytes(t, b)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	return r
}

func TestPNG(t *testing.T) {
	r := mustProbe(t, buildPNG(320, 200, false))
	if r.Format != FormatPNG || r.Width != 320 || r.Height != 200 {
		t.Fatalf("got %+v", r)
	}
}

func TestPNGCgBI(t *testing.T) {
	r := mustProbe(t, buildPNG(640, 480, true))
	if r.Format != FormatPNG || r.Width != 640 || r.Height != 480 {
		t.Fatalf("got %+v", r)
	}
}

// Required scenario 1: big-endian Exif, Orientation=6, APP1 embeds a
// thumbnail of a different size; the result must be the swapped main
// dimensions.
func TestJPEGExifOrientationThumbnail(t *testing.T) {
	r := mustProbe(t, buildJPEG(400, 300, 6, true))
	if r.Format != FormatJPEG || r.Width != 300 || r.Height != 400 {
		t.Fatalf("got %+v, want 300x400", r)
	}
}

func TestJPEGNoExif(t *testing.T) {
	r := mustProbe(t, buildJPEG(400, 300, 1, false))
	if r.Width != 400 || r.Height != 300 {
		t.Fatalf("got %+v", r)
	}
}

func TestJPEGOrientationOutOfRange(t *testing.T) {
	r := mustProbe(t, buildJPEG(400, 300, 9, true))
	if r.Width != 400 || r.Height != 300 {
		t.Fatalf("got %+v", r)
	}
}

func TestJPEGZeroHeightUnsupported(t *testing.T) {
	b := buildJPEG(400, 300, 1, false)
	// Locate the SOF height field and zero it: SOI + APP0 seg + marker
	// + len + precision.
	app0 := 2 + 2 + 2 + len("JFIF\x00\x01\x01")
	sofHeight := app0 + 2 + 2 + 1
	b[sofHeight] = 0
	b[sofHeight+1] = 0
	_, err := probeBytes(t, b)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}

func TestTIFFVariants(t *testing.T) {
	cases := []struct {
		name         string
		data         []byte
		wantW, wantH float64
	}{
		{"MM SHORT", buildTIFF(true, 100, 50, false, 0), 100, 50},
		{"II SHORT", buildTIFF(false, 100, 50, false, 0), 100, 50},
		{"MM LONG", buildTIFF(true, 100, 50, true, 0), 100, 50},
		{"II LONG", buildTIFF(false, 100, 50, true, 0), 100, 50},
		{"II orient 6", buildTIFF(false, 100, 50, false, 6), 50, 100},
		{"MM orient 8", buildTIFF(true, 100, 50, true, 8), 50, 100},
		{"II orient 3", buildTIFF(false, 100, 50, false, 3), 100, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := mustProbe(t, c.data)
			if r.Format != FormatTIFF || r.Width != c.wantW || r.Height != c.wantH {
				t.Fatalf("got %+v, want %vx%v", r, c.wantW, c.wantH)
			}
		})
	}
}

func TestBigTIFFUnsupported(t *testing.T) {
	b := []byte{'I', 'I', 0x2B, 0x00, 8, 0, 0, 0}
	_, err := probeBytes(t, b)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}

func TestSVG(t *testing.T) {
	cases := []struct {
		doc          string
		wantW, wantH float64
	}{
		{`<svg width="100" height="50"></svg>`, 100, 50},
		{`<svg width="72pt" height="72pt"></svg>`, 96, 96},
		{`<svg width="1in" height="1pc"></svg>`, 96, 16},
		{`<svg width="25.4mm" height="2.54cm"></svg>`, 96, 96},
		{`<svg width="100" height="50px"></svg>`, 100, 50},
		{`<svg width="100" viewBox="0 0 200 100"></svg>`, 100, 50},
		{`<svg height="30" viewBox="0 0 200 100"></svg>`, 60, 30},
		{`<svg viewBox="0 0 200 100"></svg>`, 200, 100},
		{`<svg width="100%" height="50%" viewBox="0 0 10 20"></svg>`, 10, 20},
		{`<svg width="5em" height="1ex" viewBox="0 0 8 4"></svg>`, 8, 4},
		{`<svg width="&w;" height="30" viewBox="0 0 200 100"></svg>`, 60, 30},
		{"\xef\xbb\xbf" + `<svg width="7" height="9"></svg>`, 7, 9},
		{`<?xml version="1.0"?><!-- c --><svg width="3" height="4"></svg>`, 3, 4},
		{`<!DOCTYPE svg><svg width="5" height="6"></svg>`, 5, 6},
		{`<!DOCTYPE svg [ <!ENTITY w "100"> ]><svg width="&w;" viewBox="0 0 40 20"></svg>`, 40, 20},
		{`<svg width='12' height='8'></svg>`, 12, 8},
	}
	for _, c := range cases {
		r := mustProbe(t, []byte(c.doc))
		if r.Format != FormatSVG {
			t.Fatalf("%s: format %s", c.doc, r.Format)
		}
		if !approxEq(r.Width, c.wantW) || !approxEq(r.Height, c.wantH) {
			t.Fatalf("%s: got %vx%v, want %vx%v", c.doc, r.Width, r.Height, c.wantW, c.wantH)
		}
	}
}

func approxEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func TestSVGNoIntrinsicSize(t *testing.T) {
	for _, doc := range []string{
		`<svg></svg>`,
		`<svg width="100%"></svg>`,
		`<svg width="&w;" height="&h;"></svg>`,
		`<svg viewBox="0 0 0 0"></svg>`,
	} {
		_, err := probeBytes(t, []byte(doc))
		if !errors.Is(err, ErrNoIntrinsicSize) {
			t.Fatalf("%s: got %v, want ErrNoIntrinsicSize", doc, err)
		}
	}
}

func TestEPS(t *testing.T) {
	for _, eol := range []string{"\n", "\r", "\r\n"} {
		r := mustProbe(t, []byte(epsSample(eol)))
		wantW := 144.5 * 96 / 72
		wantH := 72.25 * 96 / 72
		if r.Format != FormatEPS || !approxEq(r.Width, wantW) || !approxEq(r.Height, wantH) {
			t.Fatalf("eol %q: got %+v, want %vx%v", eol, r, wantW, wantH)
		}
	}
}

func TestEPSBoundingBoxFallback(t *testing.T) {
	ps := "%!PS-Adobe-3.0 EPSF-3.0\n%%BoundingBox: 0 0 72 36\n%%EndComments\n%%EOF\n"
	r := mustProbe(t, []byte(ps))
	if !approxEq(r.Width, 96) || !approxEq(r.Height, 48) {
		t.Fatalf("got %+v", r)
	}
}

func TestEPSAtEnd(t *testing.T) {
	ps := "%!PS-Adobe-3.0 EPSF-3.0\n" +
		"%%BoundingBox: (atend)\n" +
		"%%EndComments\n" +
		"1 2 moveto\n" +
		"%%Trailer\n" +
		"%%BoundingBox: 0 0 288 144\n" +
		"%%EOF\n"
	r := mustProbe(t, []byte(ps))
	if !approxEq(r.Width, 384) || !approxEq(r.Height, 192) {
		t.Fatalf("got %+v", r)
	}
}

func TestEPSDOSBinary(t *testing.T) {
	ps := epsSample("\r\n")
	b := buildEPS(ps, true)
	r := mustProbe(t, b)
	wantW := 144.5 * 96 / 72
	if r.Format != FormatEPS || !approxEq(r.Width, wantW) {
		t.Fatalf("got %+v", r)
	}
}

func TestEPSHeaderEndsAtFirstNonComment(t *testing.T) {
	// The BoundingBox after the first non-comment line must be ignored.
	ps := "%!PS-Adobe-3.0 EPSF-3.0\n" +
		"%%BoundingBox: 0 0 72 72\n" +
		"1 2 moveto\n" +
		"%%BoundingBox: 0 0 720 720\n"
	r := mustProbe(t, []byte(ps))
	if !approxEq(r.Width, 96) || !approxEq(r.Height, 96) {
		t.Fatalf("got %+v", r)
	}
}

func TestUnsupported(t *testing.T) {
	for _, b := range [][]byte{
		[]byte("GIF89a...."),
		[]byte("%PDF-1.7..."),
		[]byte{0, 1, 2, 3, 4, 5, 6, 7},
	} {
		if _, err := probeBytes(t, b); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("%q: got %v, want ErrUnsupported", b, err)
		}
	}
}
