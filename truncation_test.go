package brandprobe

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// goodSamples returns samples that must probe successfully, paired with
// their expected results.
func goodSamples() map[string][]byte {
	return map[string][]byte{
		"png":            pngSample(800, 600, false),
		"png-cgbi":       pngSample(320, 240, true),
		"jpeg-exif":      jpegSample(4000, 3000, 6, true),
		"jpeg-plain":     jpegSample(1024, 768, 1, false),
		"tiff-ii-short":  tiffSample(binary.LittleEndian, false, 640, 480, 0),
		"tiff-mm-long":   tiffSample(binary.BigEndian, true, 1024, 768, 8),
		"svg-simple":     svgSample(`<svg width="100" height="50"></svg>`),
		"svg-viewbox":    svgSample(`<svg width="200" viewBox="0 0 100 100"/>`),
		"svg-prolog":     svgSample("\xEF\xBB\xBF<?xml version=\"1.0\"?><!-- c --><!DOCTYPE svg [ <!ENTITY w \"100\"> ]>\n<svg width=\"&w;\" height=\"24\" viewBox=\"0 0 10 20\"/>"),
		"eps-plain":      []byte("%!PS-Adobe-3.0 EPSF-3.0\n%%BoundingBox: 0 0 72 144\n%%EndComments\n100 100 moveto\n"),
		"eps-hires-crlf": []byte("%!PS-Adobe-3.0 EPSF-3.0\r\n%%BoundingBox: 0 0 10 10\r\n%%HiResBoundingBox: 0 0 72 36\r\n%%EndComments\r\n"),
		"eps-atend":      []byte("%!PS-Adobe-3.0 EPSF-3.0\n%%BoundingBox: (atend)\n%%EndComments\n100 100 moveto\n%%Trailer\n%%BoundingBox: 0 0 144 72\n"),
		"eps-dos":        epsDOSVariant([]byte("%!PS-Adobe-3.0 EPSF-3.0\n%%HiResBoundingBox: 0 0 72 72\n%%EndComments\n")),
	}
}

// TestTruncation verifies that for every legal sample, truncating at any
// byte position either errors out or yields the exact same result as the
// complete file.
func TestTruncation(t *testing.T) {
	for name, data := range goodSamples() {
		t.Run(name, func(t *testing.T) {
			want, err := Probe(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("full sample failed: %v", err)
			}
			for i := 0; i < len(data); i++ {
				got, err := Probe(bytes.NewReader(data[:i]), int64(i))
				if err != nil {
					continue // error is always acceptable
				}
				if got != want {
					t.Fatalf("truncated at %d: got %+v, want %+v (or an error)", i, got, want)
				}
			}
		})
	}
}
