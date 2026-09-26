// Package probe detects the display dimensions of brand asset files
// (PNG, JPEG, TIFF, SVG, EPS) by reading only their header structures.
package probe

import (
	"bytes"
	"errors"
	"io"
)

const (
	// maxFileSize is the largest accepted input: 200 MiB.
	maxFileSize = 200 << 20
	// readBudget is the maximum number of bytes a single Probe call may
	// read through the ReaderAt: 1 MiB.
	readBudget = 1 << 20
)

var (
	// ErrUnsupported reports an unsupported format or variant.
	ErrUnsupported = errors.New("probe: unsupported format or variant")
	// ErrCorrupt reports structurally damaged or truncated input.
	ErrCorrupt = errors.New("probe: corrupt or truncated data")
	// ErrNoIntrinsicSize reports an SVG without intrinsic dimensions.
	ErrNoIntrinsicSize = errors.New("probe: svg has no intrinsic size")
	// ErrBudgetExceeded reports that the read budget was exhausted
	// before the dimensions could be determined.
	ErrBudgetExceeded = errors.New("probe: read budget exceeded")
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
	// Width and Height are CSS px (1in = 96px = 72pt).
	Width  float64
	Height float64
}

// Probe inspects r (of total size bytes) and returns the display
// dimensions of the asset. It reads at most 1 MiB through r.
func Probe(r io.ReaderAt, size int64) (Result, error) {
	if r == nil || size < 0 || size > maxFileSize {
		return Result{}, ErrUnsupported
	}
	p := &parser{ra: r, size: size}
	return p.probe()
}

// parser carries the bounded read state for one Probe call.
type parser struct {
	ra   io.ReaderAt
	size int64
	used int64
}

// readAt reads buf fully at off, accounting against the budget.
// Short reads and EOF map to ErrCorrupt (truncated input).
func (p *parser) readAt(off int64, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	if off < 0 || off+int64(len(buf)) > p.size {
		return ErrCorrupt
	}
	if p.used+int64(len(buf)) > readBudget {
		return ErrBudgetExceeded
	}
	n, err := p.ra.ReadAt(buf, off)
	p.used += int64(n)
	if err != nil {
		return ErrCorrupt
	}
	if n != len(buf) {
		return ErrCorrupt
	}
	return nil
}

var (
	pngMagic     = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	tiffMagicII  = []byte{'I', 'I', 0x2A, 0x00}
	tiffMagicMM  = []byte{'M', 'M', 0x00, 0x2A}
	bigtiffMagII = []byte{'I', 'I', 0x2B, 0x00}
	bigtiffMagMM = []byte{'M', 'M', 0x00, 0x2B}
	epsDOSMagic  = []byte{0xC5, 0xD0, 0xD3, 0xC6}
	utf8BOM      = []byte{0xEF, 0xBB, 0xBF}
)

// probe sniffs the format and dispatches to a format parser.
func (p *parser) probe() (Result, error) {
	var head [8]byte
	n := int64(len(head))
	if p.size < n {
		n = p.size
	}
	if err := p.readAt(0, head[:n]); err != nil {
		return Result{}, err
	}
	h := head[:n]
	switch {
	case bytes.Equal(h, pngMagic):
		return p.parsePNG()
	case len(h) >= 3 && h[0] == 0xFF && h[1] == 0xD8 && h[2] == 0xFF:
		return p.parseJPEG()
	case bytes.HasPrefix(h, tiffMagicII) || bytes.HasPrefix(h, tiffMagicMM):
		return p.parseTIFF()
	case bytes.HasPrefix(h, bigtiffMagII) || bytes.HasPrefix(h, bigtiffMagMM):
		return Result{}, ErrUnsupported
	case bytes.HasPrefix(h, epsDOSMagic) || bytes.HasPrefix(h, []byte("%!")):
		return p.parseEPS()
	case looksLikeXML(h):
		return p.parseSVG()
	}
	return Result{}, ErrUnsupported
}

// looksLikeXML reports whether the head bytes are consistent with an
// XML/SVG document: optional UTF-8 BOM, optional whitespace, then '<'.
func looksLikeXML(h []byte) bool {
	h = bytes.TrimPrefix(h, utf8BOM)
	for _, b := range h {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		}
		return b == '<'
	}
	return false
}
