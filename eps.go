package probe

import (
	"bytes"
	"encoding/binary"
	"io"
	"strconv"
	"strings"
)

// parseEPS extracts the bounding box from the DSC header comments (or
// the trailer for "(atend)"), including the DOS binary variant with
// magic C5D0D3C6 that wraps the PostScript section.
func (p *parser) parseEPS() (Result, error) {
	start, end, err := p.epsSection()
	if err != nil {
		return Result{}, err
	}
	res, atend, err := p.epsHeaderBBox(start, end)
	if err != nil {
		return Result{}, err
	}
	if atend {
		res, err = p.epsTrailerBBox(start, end)
		if err != nil {
			return Result{}, err
		}
	}
	w := (res[2] - res[0]) * 96 / 72
	h := (res[3] - res[1]) * 96 / 72
	if w <= 0 || h <= 0 {
		return Result{}, ErrCorrupt
	}
	return Result{Format: FormatEPS, Width: w, Height: h}, nil
}

// epsSection returns the [start, end) offsets of the PostScript
// section, unwrapping the DOS binary header when present.
func (p *parser) epsSection() (int64, int64, error) {
	var head [4]byte
	if err := p.readAt(0, head[:]); err != nil {
		return 0, 0, err
	}
	if !bytes.Equal(head[:], epsDOSMagic) {
		return 0, p.size, nil
	}
	var loc [8]byte
	if err := p.readAt(4, loc[:]); err != nil {
		return 0, 0, err
	}
	start := int64(binary.LittleEndian.Uint32(loc[0:4]))
	length := int64(binary.LittleEndian.Uint32(loc[4:8]))
	if start < 12 || length <= 0 || start+length > p.size {
		return 0, 0, ErrCorrupt
	}
	return start, start + length, nil
}

// epsHeaderBBox scans the DSC header comment section for
// %%BoundingBox / %%HiResBoundingBox. HiRes wins when both carry
// numeric values; a lone "(atend)" defers to the trailer.
func (p *parser) epsHeaderBBox(start, end int64) ([4]float64, bool, error) {
	var box [4]float64
	var hi, lo, hiAtend, loAtend bool
	var hiBox, loBox [4]float64
	sc := newLineScanner(p, start, end)
	for {
		line, term, err := sc.next()
		if err == io.EOF {
			// The header comment section never terminated.
			return box, false, ErrCorrupt
		}
		if err != nil {
			return box, false, err
		}
		if !strings.HasPrefix(line, "%") {
			// A non-comment line ends the header section; its own
			// completeness cannot affect the bounding box.
			break
		}
		if !term {
			// An unterminated final comment line may be a
			// truncation artifact; never trust it.
			return box, false, ErrCorrupt
		}
		if strings.TrimRight(line, " \t") == "%%EndComments" {
			break
		}
		if v, isAtend, ok := parseBBoxLine(line, "%%HiResBoundingBox:"); ok {
			if isAtend {
				hiAtend = true
			} else if !hi {
				hiBox, hi = v, true
			}
			continue
		}
		if v, isAtend, ok := parseBBoxLine(line, "%%BoundingBox:"); ok {
			if isAtend {
				loAtend = true
			} else if !lo {
				loBox, lo = v, true
			}
		}
	}
	switch {
	case hi:
		return hiBox, false, nil
	case lo:
		return loBox, false, nil
	case hiAtend || loAtend:
		return box, true, nil
	}
	return box, false, ErrCorrupt
}

// epsTrailerBBox finds the bounding box following the last %%Trailer
// line in the PostScript section, reading only a tail window.
func (p *parser) epsTrailerBBox(start, end int64) ([4]float64, error) {
	var box [4]float64
	// Back up a few bytes so the line straddling the window start can
	// be classified: a %%Trailer line is never longer than that.
	const back = 64
	window := int64(readBudget) - p.used - back
	if window <= 0 {
		return box, ErrBudgetExceeded
	}
	if sec := end - start; sec < window {
		window = sec
	}
	wStart := end - window
	bStart := wStart
	if bStart -= back; bStart < start {
		bStart = start
	}
	tail := make([]byte, end-bStart)
	if err := p.readAt(bStart, tail); err != nil {
		return box, err
	}
	// Skip ahead to a proven line boundary before splitting.
	lineStart := 0
	if bStart > start {
		if i := bytes.LastIndexAny(tail[:wStart-bStart], "\r\n"); i >= 0 {
			// The line containing wStart begins inside the back
			// buffer and is recovered in full.
			lineStart = i + 1
			if tail[i] == '\r' && i+1 < len(tail) && tail[i+1] == '\n' {
				lineStart = i + 2
			}
		} else {
			// The straddling line is longer than the back buffer,
			// so it cannot be a %%Trailer line; discard it.
			if i := bytes.IndexAny(tail, "\r\n"); i >= 0 {
				lineStart = i + 1
				if tail[i] == '\r' && i+1 < len(tail) && tail[i+1] == '\n' {
					lineStart = i + 2
				}
			} else {
				lineStart = len(tail)
			}
		}
	}
	lines, lastTerm := splitLines(tail[lineStart:])
	if !lastTerm && len(lines) > 0 {
		// The final line has no terminator: it may be truncated.
		lines = lines[:len(lines)-1]
	}
	last := -1
	for i, l := range lines {
		if strings.TrimRight(l, " \t") == "%%Trailer" {
			last = i
		}
	}
	if last < 0 {
		if wStart == start {
			return box, ErrCorrupt
		}
		return box, ErrBudgetExceeded
	}
	var hi, lo bool
	var hiBox, loBox [4]float64
	for _, l := range lines[last+1:] {
		if v, atend, ok := parseBBoxLine(l, "%%HiResBoundingBox:"); ok && !atend && !hi {
			hiBox, hi = v, true
		}
		if v, atend, ok := parseBBoxLine(l, "%%BoundingBox:"); ok && !atend && !lo {
			loBox, lo = v, true
		}
	}
	switch {
	case hi:
		return hiBox, nil
	case lo:
		return loBox, nil
	}
	return box, ErrCorrupt
}

// parseBBoxLine parses a "prefix: v0 v1 v2 v3" or "prefix: (atend)"
// DSC comment line.
func parseBBoxLine(line, prefix string) (box [4]float64, atend, ok bool) {
	if !strings.HasPrefix(line, prefix) {
		return box, false, false
	}
	rest := strings.TrimLeft(line[len(prefix):], " \t")
	if strings.HasPrefix(rest, "(atend)") {
		return box, true, true
	}
	fields := strings.Fields(rest)
	if len(fields) < 4 {
		return box, false, true
	}
	for i := 0; i < 4; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return box, false, true
		}
		box[i] = v
	}
	return box, false, true
}

// splitLines splits buf into lines terminated by CR, LF or CRLF,
// returning each line without its terminator. A final unterminated
// line is included and reported via the second return value.
func splitLines(buf []byte) ([]string, bool) {
	var lines []string
	for i := 0; i < len(buf); {
		j := i
		for j < len(buf) && buf[j] != '\r' && buf[j] != '\n' {
			j++
		}
		lines = append(lines, string(buf[i:j]))
		switch {
		case j == len(buf):
			i = j
		case buf[j] == '\r' && j+1 < len(buf) && buf[j+1] == '\n':
			i = j + 2
		default:
			i = j + 1
		}
	}
	lastTerm := len(buf) == 0 || buf[len(buf)-1] == '\r' || buf[len(buf)-1] == '\n'
	return lines, lastTerm
}

// lineScanner streams lines from a file section in bounded chunks.
type lineScanner struct {
	p     *parser
	off   int64
	end   int64
	buf   []byte
	pos   int
	drain bool
}

func newLineScanner(p *parser, start, end int64) *lineScanner {
	return &lineScanner{p: p, off: start, end: end}
}

func (s *lineScanner) fill() error {
	if s.off >= s.end {
		s.drain = true
		return nil
	}
	n := int64(chunkSize)
	if remain := s.end - s.off; remain < n {
		n = remain
	}
	base := len(s.buf)
	s.buf = append(s.buf, make([]byte, n)...)
	if err := s.p.readAt(s.off, s.buf[base:]); err != nil {
		return err
	}
	s.off += n
	return nil
}

// next returns the next line without its terminator, or io.EOF when
// the section is exhausted. The second return value reports whether
// the line was properly terminated.
func (s *lineScanner) next() (string, bool, error) {
	for {
		for i := s.pos; i < len(s.buf); i++ {
			if s.buf[i] != '\r' && s.buf[i] != '\n' {
				continue
			}
			line := string(s.buf[s.pos:i])
			if s.buf[i] == '\r' {
				if i+1 < len(s.buf) {
					if s.buf[i+1] == '\n' {
						s.pos = i + 2
					} else {
						s.pos = i + 1
					}
				} else if s.off >= s.end {
					s.pos = i + 1
				} else {
					// CRLF may straddle the chunk boundary.
					if err := s.fill(); err != nil {
						return "", false, err
					}
					continue
				}
			} else {
				s.pos = i + 1
			}
			return line, true, nil
		}
		if s.drain || s.off >= s.end {
			if s.pos < len(s.buf) {
				line := string(s.buf[s.pos:])
				s.pos = len(s.buf)
				return line, false, nil
			}
			return "", false, io.EOF
		}
		if err := s.fill(); err != nil {
			return "", false, err
		}
	}
}
