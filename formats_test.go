package brandprobe

import (
	"encoding/binary"
	"testing"
)

func TestPNG(t *testing.T) {
	expectDims(t, mustProbe(t, pngSample(800, 600, false)), FormatPNG, 800, 600)
}

func TestPNGCgBI(t *testing.T) {
	expectDims(t, mustProbe(t, pngSample(320, 240, true)), FormatPNG, 320, 240)
}

func TestJPEGExifOrientationSwap(t *testing.T) {
	// Big-endian Exif, Orientation=6, thumbnail with different dims (99x77)
	// embedded in APP1: result must be the swapped main dims.
	expectDims(t, mustProbe(t, jpegSample(4000, 3000, 6, true)), FormatJPEG, 3000, 4000)
}

func TestJPEGNoExif(t *testing.T) {
	expectDims(t, mustProbe(t, jpegSample(1024, 768, 1, false)), FormatJPEG, 1024, 768)
}

func TestJPEGOrientationOutOfRange(t *testing.T) {
	expectDims(t, mustProbe(t, jpegSample(100, 50, 9, true)), FormatJPEG, 100, 50)
	expectDims(t, mustProbe(t, jpegSample(100, 50, 0, true)), FormatJPEG, 100, 50)
}

func TestJPEGDNLUnsupported(t *testing.T) {
	data := jpegSample(640, 0, 1, false) // SOF height 0 => DNL
	_, err := probeBytes(t, data)
	expectErrIs(t, err, ErrUnsupported)
}

func TestTIFFLittleEndianShort(t *testing.T) {
	expectDims(t, mustProbe(t, tiffSample(binary.LittleEndian, false, 640, 480, 0)), FormatTIFF, 640, 480)
}

func TestTIFFBigEndianLong(t *testing.T) {
	expectDims(t, mustProbe(t, tiffSample(binary.BigEndian, true, 1024, 768, 0)), FormatTIFF, 1024, 768)
}

func TestTIFFOrientation(t *testing.T) {
	expectDims(t, mustProbe(t, tiffSample(binary.BigEndian, true, 1024, 768, 8)), FormatTIFF, 768, 1024)
	expectDims(t, mustProbe(t, tiffSample(binary.LittleEndian, false, 640, 480, 3)), FormatTIFF, 640, 480)
}

func TestBigTIFFUnsupported(t *testing.T) {
	data := []byte{'I', 'I', 0x2B, 0x00, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err := probeBytes(t, data)
	expectErrIs(t, err, ErrUnsupported)
}

func TestSVGWidthHeight(t *testing.T) {
	expectDims(t, mustProbe(t, svgSample(`<svg width="100" height="50"></svg>`)), FormatSVG, 100, 50)
}

func TestSVGUnits(t *testing.T) {
	expectDims(t, mustProbe(t, svgSample(`<svg width="1in" height="72pt"/>`)), FormatSVG, 96, 96)
	r := mustProbe(t, svgSample(`<svg width="2pc" height="25.4mm"/>`))
	if r.Format != FormatSVG || r.Width != 32 || r.Height < 95.9 || r.Height > 96.1 {
		t.Fatalf("unexpected mm conversion: %v", r)
	}
	expectDims(t, mustProbe(t, svgSample(`<svg width="2.54cm" height="96px"/>`)), FormatSVG, 96, 96)
}

func TestSVGViewBoxFallbacks(t *testing.T) {
	// Only width: height from viewBox ratio.
	expectDims(t, mustProbe(t, svgSample(`<svg width="200" viewBox="0 0 100 100"/>`)), FormatSVG, 200, 200)
	// Only height: width from viewBox ratio.
	expectDims(t, mustProbe(t, svgSample(`<svg height="40" viewBox="0 0 30 10"/>`)), FormatSVG, 120, 40)
	// Neither: viewBox dims.
	expectDims(t, mustProbe(t, svgSample(`<svg viewBox="0 0 10 20"/>`)), FormatSVG, 10, 20)
}

func TestSVGPercentAndFontUnitsMissing(t *testing.T) {
	expectDims(t, mustProbe(t, svgSample(`<svg width="100%" height="50%" viewBox="0 0 10 20"/>`)), FormatSVG, 10, 20)
	expectDims(t, mustProbe(t, svgSample(`<svg width="2em" height="3ex" viewBox="0 0 5 6"/>`)), FormatSVG, 5, 6)
}

func TestSVGNoIntrinsicSize(t *testing.T) {
	_, err := probeBytes(t, svgSample(`<svg></svg>`))
	expectErrIs(t, err, ErrNoIntrinsicSize)
	_, err = probeBytes(t, svgSample(`<svg width="100%"/>`))
	expectErrIs(t, err, ErrNoIntrinsicSize)
}

func TestSVGEntityAttributeMissing(t *testing.T) {
	// Attribute referencing an entity is treated as absent.
	expectDims(t, mustProbe(t, svgSample(`<svg width="&w;" viewBox="0 0 10 20"/>`)), FormatSVG, 10, 20)
}

func TestSVGProlog(t *testing.T) {
	s := "\xEF\xBB\xBF" + `<?xml version="1.0"?>
<!-- a comment with <svg width="1" height="1"/> inside -->
<!DOCTYPE svg [ <!ENTITY w "100"> ]>
<svg width="&w;" height="24" viewBox="0 0 10 20"/>`
	expectDims(t, mustProbe(t, svgSample(s)), FormatSVG, 12, 24)
}

func TestEPSBoundingBox(t *testing.T) {
	ps := "%!PS-Adobe-3.0 EPSF-3.0\n%%BoundingBox: 0 0 72 144\n%%EndComments\n"
	expectDims(t, mustProbe(t, []byte(ps)), FormatEPS, 96, 192)
}

func TestEPSHiResPreferred(t *testing.T) {
	ps := "%!PS-Adobe-3.0 EPSF-3.0\r\n%%BoundingBox: 0 0 10 10\r\n%%HiResBoundingBox: 0 0 72 36\r\n%%EndComments\r\n"
	expectDims(t, mustProbe(t, []byte(ps)), FormatEPS, 96, 48)
}

func TestEPSCRLineEndings(t *testing.T) {
	ps := "%!PS-Adobe-3.0 EPSF-3.0\r%%BoundingBox: 0 0 36 72\r%%EndComments\r"
	expectDims(t, mustProbe(t, []byte(ps)), FormatEPS, 48, 96)
}

func TestEPSAtEnd(t *testing.T) {
	ps := "%!PS-Adobe-3.0 EPSF-3.0\n%%BoundingBox: (atend)\n%%EndComments\n" +
		"100 100 moveto\n%%Trailer\n%%BoundingBox: 0 0 144 72\n"
	expectDims(t, mustProbe(t, []byte(ps)), FormatEPS, 192, 96)
}

func TestEPSDOSBinaryVariant(t *testing.T) {
	ps := "%!PS-Adobe-3.0 EPSF-3.0\n%%HiResBoundingBox: 0 0 72 72\n%%EndComments\n"
	expectDims(t, mustProbe(t, epsDOSVariant([]byte(ps))), FormatEPS, 96, 96)
}

func TestEPSNoBoundingBox(t *testing.T) {
	ps := "%!PS-Adobe-3.0 EPSF-3.0\n%%EndComments\n%%BoundingBox: 0 0 72 72\n"
	_, err := probeBytes(t, []byte(ps))
	expectErrIs(t, err, ErrCorrupt)
}

func TestUnsupported(t *testing.T) {
	_, err := probeBytes(t, []byte("GIF89a........"))
	expectErrIs(t, err, ErrUnsupported)
	_, err = probeBytes(t, []byte{0, 1, 2, 3})
	expectErrIs(t, err, ErrUnsupported)
}
