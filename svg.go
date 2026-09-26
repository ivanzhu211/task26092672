package probe

import (
	"bytes"
	"strconv"
	"strings"
)

// parseSVG parses the XML prolog and the root <svg> element's
// width/height/viewBox attributes. Entities are never expanded;
// attributes referencing entities count as missing.
func (p *parser) parseSVG() (Result, error) {
	lx := &svgLexer{p: p}
	if err := lx.skipBOM(); err != nil {
		return Result{}, err
	}
	if err := lx.skipProlog(); err != nil {
		return Result{}, err
	}
	attrs, err := lx.readRootTag()
	if err != nil {
		return Result{}, err
	}
	return svgDimensions(attrs)
}

// svgLexer streams the head of an SVG document in chunks so that
// oversized prologs hit the read budget instead of memory.
type svgLexer struct {
	p   *parser
	off int64 // file offset of buf[0]
	buf []byte
	pos int
}

const chunkSize = 32 << 10

// fill appends the next chunk of input to buf.
func (l *svgLexer) fill() error {
	next := l.off + int64(len(l.buf))
	n := int64(chunkSize)
	if remain := l.p.size - next; remain < n {
		n = remain
	}
	if n <= 0 {
		return ErrCorrupt // truncated
	}
	l.buf = append(l.buf, make([]byte, n)...)
	return l.p.readAt(next, l.buf[len(l.buf)-int(n):])
}

// require ensures at least n bytes are buffered past pos.
func (l *svgLexer) require(n int) error {
	for len(l.buf)-l.pos < n {
		if err := l.fill(); err != nil {
			return err
		}
	}
	return nil
}

func (l *svgLexer) byteAt(i int) (byte, error) {
	if err := l.require(i + 1); err != nil {
		return 0, err
	}
	return l.buf[l.pos+i], nil
}

func (l *svgLexer) hasPrefix(s string) (bool, error) {
	if err := l.require(len(s)); err != nil {
		return false, err
	}
	return bytes.HasPrefix(l.buf[l.pos:], []byte(s)), nil
}

func (l *svgLexer) advance(n int) { l.pos += n }

func (l *svgLexer) skipBOM() error {
	ok, err := l.hasPrefix(string(utf8BOM))
	if err != nil {
		// Fewer than 3 bytes available: not a usable document.
		return err
	}
	if ok {
		l.advance(3)
	}
	return nil
}

func isXMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func (l *svgLexer) skipWS() error {
	for {
		b, err := l.byteAt(0)
		if err != nil {
			return err
		}
		if !isXMLSpace(b) {
			return nil
		}
		l.advance(1)
	}
}

// skipUntil scans forward to the first occurrence of term and
// advances past it.
func (l *svgLexer) skipUntil(term string) error {
	for {
		i := bytes.Index(l.buf[l.pos:], []byte(term))
		if i >= 0 {
			l.advance(i + len(term))
			return nil
		}
		if err := l.fill(); err != nil {
			return err
		}
	}
}

// skipProlog consumes whitespace, XML declarations, comments and the
// DOCTYPE, stopping at the start of the root element.
func (l *svgLexer) skipProlog() error {
	for {
		if err := l.skipWS(); err != nil {
			return err
		}
		b, err := l.byteAt(0)
		if err != nil {
			return err
		}
		if b != '<' {
			return ErrUnsupported
		}
		for _, pre := range []struct {
			mark string
			term string
		}{{"<?", "?>"}, {"<!--", "-->"}} {
			ok, err := l.hasPrefix(pre.mark)
			if err != nil {
				return err
			}
			if ok {
				l.advance(len(pre.mark))
				if err := l.skipUntil(pre.term); err != nil {
					return err
				}
				goto next
			}
		}
		if ok, err := l.hasPrefix("<!DOCTYPE"); err != nil {
			return err
		} else if ok {
			l.advance(len("<!DOCTYPE"))
			if err := l.skipDoctype(); err != nil {
				return err
			}
			goto next
		}
		return nil
	next:
	}
}

// skipDoctype consumes the rest of a DOCTYPE declaration, honoring
// quoted strings and an optional internal subset in [...].
func (l *svgLexer) skipDoctype() error {
	depth := 0
	var quote byte
	for i := l.pos; ; i++ {
		if i >= len(l.buf) {
			if err := l.fill(); err != nil {
				return err
			}
		}
		c := l.buf[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[':
			depth++
		case c == ']':
			depth--
		case c == '>' && depth <= 0:
			l.advance(i + 1 - l.pos)
			return nil
		}
	}
}

type svgAttrs struct {
	width, height, viewBox string
	hasW, hasH, hasVB      bool
}

// readRootTag parses the root element, which must be <svg>, and
// collects its width, height and viewBox attributes.
func (l *svgLexer) readRootTag() (svgAttrs, error) {
	var out svgAttrs
	b, err := l.byteAt(0)
	if err != nil {
		return out, err
	}
	if b != '<' {
		return out, ErrUnsupported
	}
	l.advance(1)
	name, err := l.readName()
	if err != nil {
		return out, err
	}
	if name != "svg" {
		return out, ErrUnsupported
	}
	for {
		if err := l.skipWS(); err != nil {
			return out, err
		}
		b, err := l.byteAt(0)
		if err != nil {
			return out, err
		}
		switch b {
		case '>':
			l.advance(1)
			return out, nil
		case '/':
			ok, err := l.hasPrefix("/>")
			if err != nil {
				return out, err
			}
			if !ok {
				return out, ErrCorrupt
			}
			l.advance(2)
			return out, nil
		}
		aname, err := l.readName()
		if err != nil {
			return out, err
		}
		if aname == "" {
			return out, ErrCorrupt
		}
		if err := l.skipWS(); err != nil {
			return out, err
		}
		b, err = l.byteAt(0)
		if err != nil {
			return out, err
		}
		if b != '=' {
			return out, ErrCorrupt
		}
		l.advance(1)
		if err := l.skipWS(); err != nil {
			return out, err
		}
		val, err := l.readQuoted()
		if err != nil {
			return out, err
		}
		switch aname {
		case "width":
			if !out.hasW {
				out.width, out.hasW = val, true
			}
		case "height":
			if !out.hasH {
				out.height, out.hasH = val, true
			}
		case "viewBox":
			if !out.hasVB {
				out.viewBox, out.hasVB = val, true
			}
		}
	}
}

// readName consumes an XML name and returns it.
func (l *svgLexer) readName() (string, error) {
	start := l.pos
	for {
		b, err := l.byteAt(0)
		if err != nil {
			return "", err
		}
		if isXMLSpace(b) || b == '=' || b == '/' || b == '>' || b == '<' ||
			b == '"' || b == '\'' {
			break
		}
		l.advance(1)
	}
	return string(l.buf[start:l.pos]), nil
}

// readQuoted consumes a quoted attribute value and returns it.
func (l *svgLexer) readQuoted() (string, error) {
	q, err := l.byteAt(0)
	if err != nil {
		return "", err
	}
	if q != '"' && q != '\'' {
		return "", ErrCorrupt
	}
	l.advance(1)
	start := l.pos
	for {
		b, err := l.byteAt(0)
		if err != nil {
			return "", err
		}
		if b == q {
			v := string(l.buf[start:l.pos])
			l.advance(1)
			return v, nil
		}
		l.advance(1)
	}
}

// svgDimensions computes intrinsic CSS-px dimensions from the root
// element attributes.
func svgDimensions(a svgAttrs) (Result, error) {
	var w, h float64
	var hasW, hasH bool
	if a.hasW {
		w, hasW = svgLength(a.width)
	}
	if a.hasH {
		h, hasH = svgLength(a.height)
	}
	vbW, vbH, hasVB := svgViewBox(a)
	r := Result{Format: FormatSVG}
	switch {
	case hasW && hasH:
		r.Width, r.Height = w, h
	case hasVB && hasW && vbH > 0:
		r.Width, r.Height = w, w*vbH/vbW
	case hasVB && hasH && vbW > 0:
		r.Width, r.Height = h*vbW/vbH, h
	case hasVB:
		r.Width, r.Height = vbW, vbH
	default:
		return Result{}, ErrNoIntrinsicSize
	}
	return r, nil
}

// svgLength parses an SVG length into CSS px. Percentages, em/ex and
// entity references count as missing.
func svgLength(s string) (float64, bool) {
	if strings.ContainsAny(s, "&") {
		return 0, false
	}
	num, unit := splitNumber(s)
	if num == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	switch unit {
	case "", "px":
		return v, true
	case "pt":
		return v * 96 / 72, true
	case "pc":
		return v * 16, true
	case "in":
		return v * 96, true
	case "mm":
		return v * 96 / 25.4, true
	case "cm":
		return v * 96 / 2.54, true
	}
	return 0, false
}

// splitNumber splits s into a numeric prefix and a unit suffix.
func splitNumber(s string) (num, unit string) {
	s = strings.TrimSpace(s)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return "", ""
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		exp := j
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j > exp {
			i = j
		}
	}
	return s[:i], strings.TrimSpace(s[i:])
}

// svgViewBox parses the viewBox attribute into its width and height.
func svgViewBox(a svgAttrs) (float64, float64, bool) {
	if !a.hasVB || strings.ContainsAny(a.viewBox, "&") {
		return 0, 0, false
	}
	fields := strings.FieldsFunc(a.viewBox, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == ','
	})
	if len(fields) != 4 {
		return 0, 0, false
	}
	vals := make([]float64, 4)
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return 0, 0, false
		}
		vals[i] = v
	}
	if vals[2] <= 0 || vals[3] <= 0 {
		return 0, 0, false
	}
	return vals[2], vals[3], true
}
