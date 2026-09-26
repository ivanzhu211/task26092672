package probe

import (
	"bytes"
	"testing"
)

func allSamples() map[string][]byte {
	return map[string][]byte{
		"png":           buildPNG(320, 200, false),
		"png-cgbi":      buildPNG(640, 480, true),
		"jpeg-exif6":    buildJPEG(400, 300, 6, true),
		"jpeg-noexif":   buildJPEG(400, 300, 1, false),
		"jpeg-orient5":  buildJPEG(111, 222, 5, true),
		"tiff-ii-short": buildTIFF(false, 100, 50, false, 0),
		"tiff-mm-long":  buildTIFF(true, 100, 50, true, 0),
		"tiff-ii-o8":    buildTIFF(false, 100, 50, true, 8),
		"svg-plain":     []byte(`<svg width="100" height="50"></svg>`),
		"svg-bom":       []byte("\xef\xbb\xbf" + `<svg width="7" height="9"></svg>`),
		"svg-prolog":    []byte(`<?xml version="1.0"?><!-- c --><!DOCTYPE svg [ <!ENTITY w "100"> ]><svg width="&w;" viewBox="0 0 40 20"></svg>`),
		"svg-viewbox":   []byte(`<svg viewBox="0 0 200 100"></svg>`),
		"svg-units":     []byte(`<svg width="72pt" height="25.4mm"></svg>`),
		"eps-lf":        []byte(epsSample("\n")),
		"eps-cr":        []byte(epsSample("\r")),
		"eps-crlf":      []byte(epsSample("\r\n")),
		"eps-atend": []byte("%!PS-Adobe-3.0 EPSF-3.0\n" +
			"%%BoundingBox: (atend)\n" +
			"%%EndComments\n" +
			"1 2 moveto\n" +
			"%%Trailer\n" +
			"%%BoundingBox: 0 0 288 144\n" +
			"%%EOF\n"),
		"eps-dos": buildEPS(epsSample("\r\n"), true),
	}
}

// Required scenario 2: truncating any sample at any byte position
// must yield either an error or the exact same result as the
// complete file.
func TestTruncation(t *testing.T) {
	for name, data := range allSamples() {
		t.Run(name, func(t *testing.T) {
			full, err := Probe(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("complete sample failed: %v", err)
			}
			for i := 0; i < len(data); i++ {
				got, err := Probe(bytes.NewReader(data[:i]), int64(i))
				if err != nil {
					continue
				}
				if got != full {
					t.Fatalf("cut at %d: got %+v, want %+v", i, got, full)
				}
			}
		})
	}
}
