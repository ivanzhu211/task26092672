package brandprobe

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
)

// probeEPS extracts the bounding box from the header comment section of an
// EPS file, including the DOS binary variant (magic C5D0D3C6) where the
// PostScript section is located via the header's offset/length fields.
// %%HiResBoundingBox is preferred over %%BoundingBox. A value of (atend)
// is resolved from the comments following the last %%Trailer line.
func probeEPS(b *budgetReader) (Result, error) {
	psStart, psEnd := int64(0), b.size
	if b.size >= 4 {
		head, err := b.bytes(0, 4)
		if err != nil {
			return Result{}, err
		}
		if bytes.Equal(head, epsMagic) {
			hdr, err := b.bytes(0, 30)
			if err != nil {
				return Result{}, err
			}
			psStart = int64(binary.LittleEndian.Uint32(hdr[4:8]))
			psLen := int64(binary.LittleEndian.Uint32(hdr[8:12]))
			if psStart < 30 || psLen <= 0 || psStart+psLen > b.size {
				return Result{}, ErrCorrupt
			}
			psEnd = psStart + psLen
		}
	}

	lr := newLineReader(b, psStart, psEnd)
	hi, lo, err := scanEPSComments(lr, true)
	if err != nil {
		return Result{}, err
	}
	box := hi
	if box == nil {
		box = lo
	}
	if box == nil {
		return Result{}, ErrCorrupt // no bounding box in header comments
	}
	if box.atEnd {
		// Resolve from comments after the last %%Trailer in the PS section.
		trailer, err := findLastTrailer(b, psStart, psEnd)
		if err != nil {
			return Result{}, err
		}
		lr2 := newLineReader(b, trailer, psEnd)
		hi2, lo2, err := scanEPSComments(lr2, false)
		if err != nil {
			return Result{}, err
		}
		box = hi2
		if box == nil {
			box = lo2
		}
		if box == nil || box.atEnd {
			return Result{}, ErrCorrupt
		}
	}
	w := (box.urx - box.llx) * 96 / 72
	h := (box.ury - box.lly) * 96 / 72
	return Result{Format: FormatEPS, Width: w, Height: h}, nil
}

type epsBox struct {
	llx, lly, urx, ury float64
	atEnd              bool
}

// scanEPSComments reads comment lines until %%EndComments, the first
// non-comment line, or EOF, collecting the first %%HiResBoundingBox and
// %%BoundingBox values. When header is true, reaching EOF before any
// bounding box is an error; otherwise it simply reports none found.
func scanEPSComments(lr *lineReader, header bool) (hi, lo *epsBox, err error) {
	for {
		line, terminated, eof, err := lr.next()
		if err != nil {
			return nil, nil, err
		}
		if eof {
			if header && hi == nil {
				// Comment section not terminated: a later
				// %%HiResBoundingBox could still override.
				return nil, nil, ErrCorrupt
			}
			return hi, lo, nil
		}
		if !strings.HasPrefix(line, "%") {
			return hi, lo, nil // end of comment section
		}
		if strings.HasPrefix(line, "%%EndComments") {
			return hi, lo, nil
		}
		isHi := strings.HasPrefix(line, "%%HiResBoundingBox:")
		isLo := strings.HasPrefix(line, "%%BoundingBox:")
		if (isHi || isLo) && !terminated {
			// An unterminated bounding-box line cannot be told apart
			// from a truncated one; never trust it.
			return nil, nil, ErrCorrupt
		}
		if hi == nil && isHi {
			b, err := parseBBoxVal(line[len("%%HiResBoundingBox:"):])
			if err != nil {
				return nil, nil, err
			}
			hi = b
		}
		if lo == nil && isLo {
			b, err := parseBBoxVal(line[len("%%BoundingBox:"):])
			if err != nil {
				return nil, nil, err
			}
			lo = b
		}
	}
}

// parseBBoxVal parses the "llx lly urx ury" value of a bounding-box line.
func parseBBoxVal(raw string) (*epsBox, error) {
	val := strings.TrimSpace(raw)
	if val == "(atend)" {
		return &epsBox{atEnd: true}, nil
	}
	fields := strings.Fields(val)
	if len(fields) != 4 {
		return nil, ErrCorrupt
	}
	var nums [4]float64
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, ErrCorrupt
		}
		nums[i] = v
	}
	if nums[2] <= nums[0] || nums[3] <= nums[1] {
		return nil, ErrCorrupt
	}
	return &epsBox{llx: nums[0], lly: nums[1], urx: nums[2], ury: nums[3]}, nil
}

// findLastTrailer returns the offset just after the last %%Trailer line
// within [start, end).
func findLastTrailer(b *budgetReader, start, end int64) (int64, error) {
	lr := newLineReader(b, start, end)
	last := int64(-1)
	for {
		line, _, eof, err := lr.next()
		if err != nil {
			return 0, err
		}
		if eof {
			break
		}
		if strings.HasPrefix(line, "%%Trailer") {
			last = lr.offset()
		}
	}
	if last < 0 {
		return 0, ErrCorrupt // (atend) promised but no trailer
	}
	return last, nil
}

// lineReader streams lines from a bounded region of a budgetReader,
// accepting CR, LF or CRLF terminators.
type lineReader struct {
	b   *budgetReader
	off int64
	end int64
	buf []byte
	pos int
	eof bool
}

func newLineReader(b *budgetReader, start, end int64) *lineReader {
	return &lineReader{b: b, off: start, end: end}
}

// offset returns the file offset just past the last returned line.
func (lr *lineReader) offset() int64 {
	return lr.off - int64(len(lr.buf)-lr.pos)
}

// next returns the next line without its terminator. terminated reports
// whether the line ended with a CR/LF terminator (a line at EOF may not).
// eof is true when no more lines exist in the region.
func (lr *lineReader) next() (line string, terminated bool, eof bool, err error) {
	for {
		// Look for a terminator in the buffered bytes.
		for i := lr.pos; i < len(lr.buf); i++ {
			if lr.buf[i] == '\r' || lr.buf[i] == '\n' {
				line = string(lr.buf[lr.pos:i])
				// Consume terminator (CRLF counts as one).
				j := i + 1
				if lr.buf[i] == '\r' {
					if j < len(lr.buf) {
						if lr.buf[j] == '\n' {
							j++
						}
					} else if !lr.eof {
						// CR at buffer end: need one more byte to decide.
						if err := lr.fill(); err != nil {
							return "", false, false, err
						}
						if j < len(lr.buf) && lr.buf[j] == '\n' {
							j++
						}
					}
				}
				lr.pos = j
				return line, true, false, nil
			}
		}
		if lr.eof {
			if lr.pos < len(lr.buf) {
				line = string(lr.buf[lr.pos:])
				lr.pos = len(lr.buf)
				return line, false, false, nil
			}
			return "", false, true, nil
		}
		if err := lr.fill(); err != nil {
			return "", false, false, err
		}
	}
}

// fill reads the next chunk into the buffer, discarding consumed bytes.
func (lr *lineReader) fill() error {
	if lr.pos > 0 {
		lr.buf = lr.buf[lr.pos:]
		lr.pos = 0
	}
	remain := maxBudget - lr.b.used
	if remain <= 0 {
		return ErrBudgetExceeded
	}
	if lr.off >= lr.end {
		lr.eof = true
		return nil
	}
	n := min64(min64(remain, 64*1024), lr.end-lr.off)
	chunk, err := lr.b.bytes(lr.off, n)
	if err != nil {
		return err
	}
	lr.buf = append(lr.buf, chunk...)
	lr.off += n
	return nil
}
