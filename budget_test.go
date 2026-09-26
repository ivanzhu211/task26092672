package probe

import (
	"errors"
	"testing"
)

const hugeSize = 200 << 20 // 200 MiB

// Required scenario 3a: a 200MB SVG whose root element is preceded by
// an enormous comment must exhaust the read budget, not memory.
func TestBudgetSVGHugeComment(t *testing.T) {
	prefix := []byte(`<?xml version="1.0"?><!--`)
	suffix := []byte(`--><svg width="10" height="20"></svg>`)
	r := &synthReader{size: hugeSize, fn: func(off int64, p []byte) {
		for i := range p {
			pos := off + int64(i)
			switch {
			case pos < int64(len(prefix)):
				p[i] = prefix[pos]
			case pos >= hugeSize-int64(len(suffix)):
				p[i] = suffix[pos-(hugeSize-int64(len(suffix)))]
			default:
				p[i] = 'x'
			}
		}
	}}
	cr := &countingReader{r: r}
	_, err := Probe(cr, hugeSize)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("got %v, want ErrBudgetExceeded", err)
	}
	if cr.read > readBudget {
		t.Fatalf("read %d bytes, budget %d", cr.read, readBudget)
	}
}

// Required scenario 3b: a 200MB JPEG made of a huge number of tiny
// segments before the SOF must stop at the budget.
func TestBudgetJPEGManySegments(t *testing.T) {
	const segSize = 48 // 2 marker + 2 len + 44 payload
	r := &synthReader{size: hugeSize, fn: func(off int64, p []byte) {
		for i := range p {
			pos := off + int64(i)
			switch {
			case pos == 0:
				p[i] = 0xFF
			case pos == 1:
				p[i] = 0xD8
			default:
				within := (pos - 2) % segSize
				switch within {
				case 0:
					p[i] = 0xFF
				case 1:
					p[i] = 0xE2 // APP2
				case 2:
					p[i] = 0
				case 3:
					p[i] = segSize - 2
				default:
					p[i] = 0x11
				}
			}
		}
	}}
	cr := &countingReader{r: r}
	_, err := Probe(cr, hugeSize)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("got %v, want ErrBudgetExceeded", err)
	}
	if cr.read > readBudget {
		t.Fatalf("read %d bytes, budget %d", cr.read, readBudget)
	}
}

// A normal small file must stay far below the budget.
func TestBudgetSmallFiles(t *testing.T) {
	for name, data := range allSamples() {
		cr := &countingReader{r: &synthReader{
			size: int64(len(data)),
			fn: func(off int64, p []byte) {
				copy(p, data[off:int(off)+len(p)])
			},
		}}
		if _, err := Probe(cr, int64(len(data))); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if cr.read > readBudget {
			t.Fatalf("%s: read %d bytes", name, cr.read)
		}
	}
}

// Inputs larger than 200MB are rejected outright.
func TestOversizeRejected(t *testing.T) {
	r := &synthReader{size: hugeSize + 1, fn: func(off int64, p []byte) {}}
	if _, err := Probe(r, hugeSize+1); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}

// A 200MB EPS with "(atend)" whose trailer sits at the very end must
// succeed by reading only the head and tail windows.
func TestBudgetEPSAtEndHuge(t *testing.T) {
	head := []byte("%!PS-Adobe-3.0 EPSF-3.0\n%%BoundingBox: (atend)\n%%EndComments\n")
	tail := []byte("\n%%Trailer\n%%HiResBoundingBox: 0 0 72 144\n%%EOF\n")
	r := &synthReader{size: hugeSize, fn: func(off int64, p []byte) {
		for i := range p {
			pos := off + int64(i)
			switch {
			case pos < int64(len(head)):
				p[i] = head[pos]
			case pos >= hugeSize-int64(len(tail)):
				p[i] = tail[pos-(hugeSize-int64(len(tail)))]
			default:
				p[i] = ' '
			}
		}
	}}
	cr := &countingReader{r: r}
	res, err := Probe(cr, hugeSize)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	if !approxEq(res.Width, 96) || !approxEq(res.Height, 192) {
		t.Fatalf("got %+v", res)
	}
	if cr.read > readBudget {
		t.Fatalf("read %d bytes, budget %d", cr.read, readBudget)
	}
}

// An SVG whose root element starts just inside the budget still works.
func TestBudgetSVGLargeCommentOK(t *testing.T) {
	const commentLen = 600 << 10
	prefix := []byte(`<svg style="display:none"></svg>`) // decoy, not root
	_ = prefix
	doc := []byte(`<svg width="42" height="24"></svg>`)
	open := []byte(`<!--`)
	close := []byte(`-->`)
	total := int64(len(open)) + commentLen + int64(len(close)) + int64(len(doc))
	r := &synthReader{size: total, fn: func(off int64, p []byte) {
		for i := range p {
			pos := off + int64(i)
			switch {
			case pos < int64(len(open)):
				p[i] = open[pos]
			case pos < int64(len(open))+commentLen:
				p[i] = 'x'
			case pos < int64(len(open))+commentLen+int64(len(close)):
				p[i] = close[pos-int64(len(open))-commentLen]
			default:
				p[i] = doc[pos-int64(len(open))-commentLen-int64(len(close))]
			}
		}
	}}
	cr := &countingReader{r: r}
	res, err := Probe(cr, total)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	if res.Width != 42 || res.Height != 24 {
		t.Fatalf("got %+v", res)
	}
	if cr.read > readBudget {
		t.Fatalf("read %d bytes, budget %d", cr.read, readBudget)
	}
}
