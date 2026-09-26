package brandprobe

import (
	"bytes"
	"strconv"
	"strings"
)

// probeSVG locates the root <svg> element (skipping BOM, XML prolog,
// comments, PIs and DOCTYPE) and derives intrinsic dimensions from its
// width/height attributes and viewBox. Entities are never expanded; any
// attribute value referencing an entity is treated as absent.
func probeSVG(b *budgetReader) (Result, error) {
	var buf []byte
	off := int64(0)
	for {
		attrs, done, err := parseSVGRoot(buf)
		if err != nil {
			return Result{}, err
		}
		if done {
			return svgDimensions(attrs)
		}
		// Need more input.
		remain := maxBudget - b.used
		if remain <= 0 {
			return Result{}, ErrBudgetExceeded
		}
		if off >= b.size {
			return Result{}, ErrCorrupt // truncated mid-structure
		}
		n := min64(min64(remain, 64*1024), b.size-off)
		chunk, err := b.bytes(off, n)
		if err != nil {
			return Result{}, err
		}
		buf = append(buf, chunk...)
		off += n
	}
}

// parseSVGRoot scans buf for the root element. It returns done=false when
// more bytes are needed to decide.
func parseSVGRoot(buf []byte) (map[string]string, bool, error) {
	i := 0
	if bytes.HasPrefix(buf, []byte{0xEF, 0xBB, 0xBF}) {
		i = 3
	}
	for {
		for i < len(buf) && isSpace(buf[i]) {
			i++
		}
		if i >= len(buf) {
			return nil, false, nil
		}
		if buf[i] != '<' {
			return nil, false, ErrUnsupported
		}
		rest := buf[i:]
		switch {
		case bytes.HasPrefix(rest, []byte("<!--")):
			end := bytes.Index(rest[4:], []byte("-->"))
			if end < 0 {
				return nil, false, nil
			}
			i += 4 + end + 3
		case bytes.HasPrefix(rest, []byte("<?")):
			end := bytes.Index(rest[2:], []byte("?>"))
			if end < 0 {
				return nil, false, nil
			}
			i += 2 + end + 2
		case bytes.HasPrefix(rest, []byte("<!")):
			end, ok := skipDoctype(rest)
			if !ok {
				return nil, false, nil
			}
			i += end
		case bytes.HasPrefix(rest, []byte("<svg")):
			if len(rest) == 4 {
				return nil, false, nil
			}
			if c := rest[4]; !isSpace(c) && c != '>' && c != '/' {
				return nil, false, ErrUnsupported // e.g. <svgfoo
			}
			return parseSVGTag(rest)
		default:
			return nil, false, ErrUnsupported
		}
	}
}

// skipDoctype returns the length of a <!...> declaration (DOCTYPE or
// otherwise), honoring quoted strings and a bracketed internal subset.
func skipDoctype(s []byte) (int, bool) {
	depth := 0
	var quote byte
	for i := 2; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case '>':
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

// parseSVGTag parses the <svg ...> start tag at the beginning of s.
func parseSVGTag(s []byte) (map[string]string, bool, error) {
	// Find the end of the start tag, honoring quotes.
	quote := byte(0)
	end := -1
	for i := 4; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
		} else if c == '>' {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, false, nil // need more bytes
	}
	attrs := map[string]string{}
	i := 4
	for i < end {
		for i < end && isSpace(s[i]) {
			i++
		}
		if i >= end || s[i] == '>' || s[i] == '/' {
			break
		}
		nameStart := i
		for i < end && !isSpace(s[i]) && s[i] != '=' && s[i] != '>' && s[i] != '/' {
			i++
		}
		name := string(s[nameStart:i])
		for i < end && isSpace(s[i]) {
			i++
		}
		if i >= end || s[i] != '=' {
			// Attribute without value: malformed for our purposes.
			return nil, false, ErrCorrupt
		}
		i++
		for i < end && isSpace(s[i]) {
			i++
		}
		if i >= end || (s[i] != '"' && s[i] != '\'') {
			return nil, false, ErrCorrupt
		}
		q := s[i]
		i++
		valStart := i
		for i < end && s[i] != q {
			i++
		}
		if i >= end {
			return nil, false, ErrCorrupt
		}
		val := string(s[valStart:i])
		i++
		if strings.ContainsAny(val, "&") {
			continue // entity reference: treat as absent
		}
		switch name {
		case "width", "height", "viewBox":
			if _, dup := attrs[name]; !dup {
				attrs[name] = val
			}
		}
	}
	return attrs, true, nil
}

// svgDimensions computes intrinsic dimensions from the root attributes.
func svgDimensions(attrs map[string]string) (Result, error) {
	w, hasW := parseSVGLength(attrs["width"])
	h, hasH := parseSVGLength(attrs["height"])
	vw, vh, hasVB := parseViewBox(attrs["viewBox"])

	switch {
	case hasW && hasH:
		return Result{Format: FormatSVG, Width: w, Height: h}, nil
	case hasW && hasVB:
		return Result{Format: FormatSVG, Width: w, Height: w * vh / vw}, nil
	case hasH && hasVB:
		return Result{Format: FormatSVG, Width: h * vw / vh, Height: h}, nil
	case hasVB:
		return Result{Format: FormatSVG, Width: vw, Height: vh}, nil
	}
	return Result{}, ErrNoIntrinsicSize
}

// parseSVGLength parses an SVG length with unit px, pt, pc, mm, cm, in or
// no unit, returning CSS pixels. Percentages, em/ex, invalid or
// non-positive values report ok=false (treated as absent).
func parseSVGLength(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	// Split numeric prefix and unit suffix.
	i := 0
	for i < len(s) && !isAlpha(s[i]) && s[i] != '%' {
		i++
	}
	num, unit := s[:i], strings.TrimSpace(s[i:])
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, false
	}
	var px float64
	switch unit {
	case "", "px":
		px = f
	case "pt":
		px = f * 96 / 72
	case "pc":
		px = f * 16
	case "mm":
		px = f * 96 / 25.4
	case "cm":
		px = f * 96 / 2.54
	case "in":
		px = f * 96
	default: // %, em, ex, unknown units
		return 0, false
	}
	if px <= 0 {
		return 0, false
	}
	return px, true
}

// parseViewBox extracts the width and height from a viewBox attribute.
func parseViewBox(s string) (w, h float64, ok bool) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(fields) != 4 {
		return 0, 0, false
	}
	v := make([]float64, 4)
	for i, f := range fields {
		var err error
		v[i], err = strconv.ParseFloat(f, 64)
		if err != nil {
			return 0, 0, false
		}
	}
	if v[2] <= 0 || v[3] <= 0 {
		return 0, 0, false
	}
	return v[2], v[3], true
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
