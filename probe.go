// Package brandprobe probes the display dimensions of brand asset files
// (PNG, JPEG, TIFF, SVG, EPS) by reading only header structures.
package brandprobe

import (
	"bytes"
	"errors"
	"io"
)

// Format identifies the detected asset format.
type Format string

const (
	FormatPNG  Format = "png"
	FormatJPEG Format = "jpeg"
	FormatTIFF Format = "tiff"
	FormatSVG  Format = "svg"
	FormatEPS  Format = "eps"
)

// Result is the outcome of a successful Probe.
type Result struct {
	Format Format
	// Width and Height are CSS pixels (1in = 96px = 72pt).
	Width  float64
	Height float64
}

// Error categories, matchable with errors.Is.
var (
	// ErrUnsupported indicates an unsupported format or variant.
	ErrUnsupported = errors.New("brandprobe: unsupported format or variant")
	// ErrCorrupt indicates a structurally damaged file.
	ErrCorrupt = errors.New("brandprobe: corrupt structure")
	// ErrNoIntrinsicSize indicates an SVG without intrinsic dimensions.
	ErrNoIntrinsicSize = errors.New("brandprobe: no intrinsic size")
	// ErrBudgetExceeded indicates the read budget was exhausted.
	ErrBudgetExceeded = errors.New("brandprobe: read budget exceeded")
)

var (
	pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	epsMagic = []byte{0xC5, 0xD0, 0xD3, 0xC6} // DOS binary EPS header
)

// Probe detects the format of the asset in r (of total size bytes) and
// returns its display dimensions in CSS pixels. It reads at most 1 MiB
// through r and never decodes pixel data.
func Probe(r io.ReaderAt, size int64) (Result, error) {
	b := newBudgetReader(r, size)
	head, err := b.bytes(0, min64(size, 16))
	if err != nil {
		return Result{}, err
	}
	switch {
	case bytes.HasPrefix(head, pngMagic):
		return probePNG(b)
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return probeJPEG(b)
	case bytes.HasPrefix(head, []byte{'I', 'I', 0x2A, 0x00}),
		bytes.HasPrefix(head, []byte{'M', 'M', 0x00, 0x2A}):
		return probeTIFF(b)
	case bytes.HasPrefix(head, []byte{'I', 'I', 0x2B, 0x00}),
		bytes.HasPrefix(head, []byte{'M', 'M', 0x00, 0x2B}):
		return Result{}, ErrUnsupported // BigTIFF
	case bytes.HasPrefix(head, epsMagic), bytes.HasPrefix(head, []byte("%!PS")):
		return probeEPS(b)
	default:
		return probeSVG(b)
	}
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
