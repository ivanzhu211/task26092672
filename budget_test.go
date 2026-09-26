package brandprobe

import (
	"errors"
	"testing"
)

// countingReaderAt wraps a byte-generator and counts bytes actually read.
type countingReaderAt struct {
	gen  func(off int64, p []byte)
	used int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.gen(off, p)
	c.used += int64(len(p))
	return len(p), nil
}

const bigFile = 200 << 20 // 200 MB

// svgHugeCommentGen synthesizes: <!-- + x*... + --><svg width="10" height="20"/>
func svgHugeCommentGen(off int64, p []byte) {
	const prefix = "<!--"
	const suffix = "--><svg width=\"10\" height=\"20\"/>"
	for i := range p {
		pos := off + int64(i)
		switch {
		case pos < int64(len(prefix)):
			p[i] = prefix[pos]
		case pos >= bigFile-int64(len(suffix)):
			p[i] = suffix[pos-(bigFile-int64(len(suffix)))]
		default:
			p[i] = 'x'
		}
	}
}

// jpegManySegmentsGen synthesizes: FFD8 then endless empty APP0 segments.
func jpegManySegmentsGen(off int64, p []byte) {
	for i := range p {
		pos := off + int64(i)
		switch pos {
		case 0:
			p[i] = 0xFF
		case 1:
			p[i] = 0xD8
		default:
			switch (pos - 2) % 4 {
			case 0:
				p[i] = 0xFF
			case 1:
				p[i] = 0xE0
			case 2, 3:
				p[i] = 0x00
			}
			if (pos-2)%4 == 2 {
				p[i] = 0x00
			} else if (pos-2)%4 == 3 {
				p[i] = 0x02 // segment length 2 (empty)
			}
		}
	}
}

// epsHugeBodyGen synthesizes an EPS with (atend) and a 200MB body before
// the trailer.
func epsHugeBodyGen(off int64, p []byte) {
	head := "%!PS-Adobe-3.0 EPSF-3.0\n%%BoundingBox: (atend)\n%%EndComments\n"
	tail := "%%Trailer\n%%BoundingBox: 0 0 72 72\n"
	for i := range p {
		pos := off + int64(i)
		switch {
		case pos < int64(len(head)):
			p[i] = head[pos]
		case pos >= bigFile-int64(len(tail)):
			p[i] = tail[pos-(bigFile-int64(len(tail)))]
		default:
			p[i] = '.'
			if pos%80 == 0 {
				p[i] = '\n'
			}
		}
	}
}

func TestBudgetEnforced(t *testing.T) {
	cases := map[string]func(off int64, p []byte){
		"svg-huge-comment":   svgHugeCommentGen,
		"jpeg-many-segments": jpegManySegmentsGen,
		"eps-huge-body":      epsHugeBodyGen,
	}
	for name, gen := range cases {
		t.Run(name, func(t *testing.T) {
			r := &countingReaderAt{gen: gen}
			_, err := Probe(r, bigFile)
			if !errors.Is(err, ErrBudgetExceeded) {
				t.Fatalf("got %v, want ErrBudgetExceeded", err)
			}
			if r.used > maxBudget {
				t.Fatalf("read %d bytes, budget is %d", r.used, maxBudget)
			}
			t.Logf("read %d bytes before giving up", r.used)
		})
	}
}

func TestBudgetNotExceededForSmallFiles(t *testing.T) {
	data := jpegSample(4000, 3000, 6, true)
	r := &countingReaderAt{gen: func(off int64, p []byte) {
		copy(p, data[off:int(off)+len(p)])
	}}
	res, err := Probe(r, int64(len(data)))
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	if res.Width != 3000 || res.Height != 4000 {
		t.Fatalf("unexpected result %v", res)
	}
	if r.used > maxBudget {
		t.Fatalf("read %d bytes, budget is %d", r.used, maxBudget)
	}
}
