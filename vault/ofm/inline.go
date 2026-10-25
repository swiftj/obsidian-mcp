package ofm

import (
	"bytes"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Link is a wikilink, Markdown link or embed.
type Link struct {
	Span           // the whole link, including "!" for an embed
	Embed     bool // "![[…]]" or "![…](…)"
	Markdown  bool // "[text](dest)" rather than "[[…]]"
	External  bool // a Markdown destination with a URI scheme
	InComment bool // inside a %% comment; still a link (D-006)
	// Target is the linked path without subpath: as written for wikilinks,
	// percent-decoded for Markdown links. "" links to the same note.
	Target  string
	Subpath string // "#Heading", "#H#Sub", "#^id" or "#page=3"; "" for none
	// Display is the display text; for Markdown links, the link text.
	Display    string
	HasDisplay bool
	// Width and Height are an image embed's size ("|W" or "|WxH").
	Width, Height int
	// Raw source ranges, for rewriting the link. SubpathSpan and DisplaySpan
	// are empty (Start == End) when absent.
	TargetSpan, SubpathSpan, DisplaySpan Span
}

// Tag is an inline "#tag".
type Tag struct {
	Span        // including "#"
	Name string // as written, without "#"
}

type scan struct {
	p         *parser
	s         []byte
	start     int
	end       int
	table     bool
	linksOnly bool // inside a comment: only links count
	hlOpen    int
	tagEnd    int // where the last tag ended, so "#a#b" gives two tags
}

// inline scans src[start:end], one line of paragraph, heading or table text.
func (p *parser) inline(start, end int, table bool) {
	sc := scan{p: p, s: p.src, start: start, end: end, table: table, hlOpen: -1, tagEnd: -1}
	sc.run()
}

// inlineLinks scans comment text, where only links count (O-CMT-01).
func (p *parser) inlineLinks(start, end int) {
	sc := scan{p: p, s: p.src, start: start, end: end, linksOnly: true, hlOpen: -1, tagEnd: -1}
	sc.run()
}

func (sc *scan) at(i int, prefix string) bool {
	return i+len(prefix) <= sc.end && string(sc.s[i:i+len(prefix)]) == prefix
}

func (sc *scan) run() {
	d := sc.p.d
	i := sc.start
	for i < sc.end {
		c := sc.s[i]
		switch {
		case c == '\\' && i+1 < sc.end && isASCIIPunct(sc.s[i+1]):
			i += 2
			continue
		case c == '[' || (c == '!' && sc.at(i+1, "[")):
			if j, ok := sc.link(i); ok {
				i = j
				continue
			}
		}
		if sc.linksOnly {
			i += runeLen(sc.s, i)
			continue
		}
		switch {
		case c == '`':
			n := run(sc.s[i:sc.end], '`')
			if j := sc.closingRun(i+n, '`', n); j >= 0 {
				d.Code = append(d.Code, Code{Kind: CodeInline, Span: Span{i, j}})
				i = j
			} else {
				i += n
			}
			continue
		case c == '$':
			if j := sc.inlineMath(i); j > 0 {
				d.Math = append(d.Math, Span{i, j})
				i = j
			} else {
				i += run(sc.s[i:sc.end], '$')
			}
			continue
		case c == '%' && sc.at(i, "%%"):
			if k := bytes.Index(sc.s[i+2:sc.end], []byte("%%")); k >= 0 {
				j := i + 2 + k + 2
				d.Comments = append(d.Comments, Span{i, j})
				sc.p.inlineLinks(i+2, i+2+k)
				i = j
			} else {
				i += 2
			}
			continue
		case c == '^' && sc.at(i+1, "["):
			if j := sc.closeBracket(i + 1); j > 0 {
				inner := scan{p: sc.p, s: sc.s, start: i + 2, end: j - 1, table: sc.table, hlOpen: -1, tagEnd: -1}
				inner.run()
				d.Footnotes = append(d.Footnotes, Span{i, j})
				i = j
				continue
			}
		case c == '=' && sc.at(i, "=="):
			if sc.hlOpen < 0 {
				sc.hlOpen = i
			} else {
				d.Highlights = append(d.Highlights, Span{sc.hlOpen, i + 2})
				sc.hlOpen = -1
			}
			i += 2
			continue
		case c == '<':
			if j := sc.htmlTag(i); j > 0 {
				i = j
				continue
			}
		case c == '#':
			if j, ok := sc.tag(i); ok {
				i = j
				continue
			}
		case isASCIILetter(c) && (i == sc.start || !isASCIIAlnum(sc.s[i-1])):
			if j := sc.url(i); j > 0 {
				i = j
				continue
			}
		}
		i += runeLen(sc.s, i)
	}
}

// link parses a wikilink, Markdown link, embed or footnote reference at i.
func (sc *scan) link(i int) (int, bool) {
	embed := sc.s[i] == '!'
	open := i
	if embed {
		open++
	}
	if sc.at(open, "[[") {
		return sc.wikilink(i, open, embed)
	}
	if !embed && sc.at(open, "[^") {
		if j := sc.closeBracket(open); j > 0 && !sc.at(j, "(") && !sc.at(j, ":") &&
			!bytes.ContainsAny(sc.s[open+2:j-1], " \t") && j-1 > open+2 {
			if !sc.linksOnly {
				sc.p.d.Footnotes = append(sc.p.d.Footnotes, Span{open, j})
			}
			return j, true
		}
	}
	return sc.mdlink(i, open, embed)
}

func (sc *scan) wikilink(i, open int, embed bool) (int, bool) {
	inner := open + 2
	k := bytes.Index(sc.s[inner:sc.end], []byte("]]"))
	if k < 0 {
		return 0, false
	}
	if r := bytes.Index(sc.s[inner:inner+k], []byte("[[")); r >= 0 {
		return inner + r, true // restart at the inner "[["
	}
	innerEnd := inner + k
	body := sc.s[inner:innerEnd]
	if len(bytes.TrimSpace(body)) == 0 {
		return 0, false
	}
	l := Link{Span: Span{i, innerEnd + 2}, Embed: embed, InComment: sc.linksOnly}
	targetEnd := innerEnd
	l.DisplaySpan = Span{innerEnd, innerEnd}
	if bar := bytes.IndexByte(body, '|'); bar >= 0 {
		targetEnd = inner + bar
		if sc.table && bar > 0 && body[bar-1] == '\\' {
			targetEnd--
		}
		l.DisplaySpan = Span{inner + bar + 1, innerEnd}
		l.HasDisplay = true
		l.Display = string(sc.s[l.DisplaySpan.Start:l.DisplaySpan.End])
	}
	l.TargetSpan = Span{inner, targetEnd}
	l.SubpathSpan = Span{targetEnd, targetEnd}
	if h := bytes.IndexByte(sc.s[inner:targetEnd], '#'); h >= 0 {
		l.TargetSpan.End = inner + h
		l.SubpathSpan = Span{inner + h, targetEnd}
		l.Subpath = string(sc.s[inner+h : targetEnd])
	}
	l.Target = string(sc.s[l.TargetSpan.Start:l.TargetSpan.End])
	if embed && l.HasDisplay && isImage(l.Target) {
		if w, h, ok := parseSize(l.Display); ok {
			l.Width, l.Height = w, h
			l.Display, l.HasDisplay = "", false
		}
	}
	sc.p.d.Links = append(sc.p.d.Links, l)
	return l.End, true
}

func (sc *scan) mdlink(i, open int, embed bool) (int, bool) {
	textEnd := sc.closeBracket(open) // just past "]"
	if textEnd < 0 || !sc.at(textEnd, "(") {
		return 0, false
	}
	k := textEnd + 1
	for k < sc.end && (sc.s[k] == ' ' || sc.s[k] == '\t') {
		k++
	}
	var raw Span
	if sc.at(k, "<") {
		e := bytes.IndexByte(sc.s[k+1:sc.end], '>')
		if e < 0 || bytes.IndexByte(sc.s[k+1:k+1+e], '<') >= 0 {
			return 0, false
		}
		raw = Span{k + 1, k + 1 + e}
		k = k + 2 + e
	} else {
		depth, j := 0, k
		for ; j < sc.end; j++ {
			c := sc.s[j]
			if c == '\\' && j+1 < sc.end {
				j++
				continue
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				if depth == 0 {
					break
				}
				depth--
			} else if c == ' ' || c == '\t' || c < 0x20 {
				break
			}
		}
		raw = Span{k, j}
		k = j
	}
	// An optional title, then ")".
	for k < sc.end && (sc.s[k] == ' ' || sc.s[k] == '\t') {
		k++
	}
	if k < sc.end && (sc.s[k] == '"' || sc.s[k] == '\'') && k > raw.End {
		e := bytes.IndexByte(sc.s[k+1:sc.end], sc.s[k])
		if e < 0 {
			return 0, false
		}
		k += e + 2
		for k < sc.end && (sc.s[k] == ' ' || sc.s[k] == '\t') {
			k++
		}
	}
	if !sc.at(k, ")") || raw.Len() == 0 {
		return 0, false
	}
	l := Link{
		Span:        Span{i, k + 1},
		Embed:       embed,
		Markdown:    true,
		InComment:   sc.linksOnly,
		Display:     string(sc.s[open+1 : textEnd-1]),
		HasDisplay:  true,
		DisplaySpan: Span{open + 1, textEnd - 1},
		TargetSpan:  raw,
		SubpathSpan: Span{raw.End, raw.End},
	}
	dest := sc.s[raw.Start:raw.End]
	if h := bytes.IndexByte(dest, '#'); h >= 0 {
		l.TargetSpan.End = raw.Start + h
		l.SubpathSpan = Span{raw.Start + h, raw.End}
		l.Subpath = unescape(string(dest[h:]))
	}
	l.Target = unescape(string(sc.s[l.TargetSpan.Start:l.TargetSpan.End]))
	l.External = hasScheme(l.Target)
	sc.p.d.Links = append(sc.p.d.Links, l)
	return l.End, true
}

// closeBracket returns the offset just past the "]" matching the "[" at i,
// or -1.
func (sc *scan) closeBracket(i int) int {
	depth := 0
	for j := i; j < sc.end; j++ {
		switch sc.s[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return j + 1
			}
		}
	}
	return -1
}

// closingRun returns the offset just past the first run of exactly n bytes
// equal to c at or after i, or -1.
func (sc *scan) closingRun(i int, c byte, n int) int {
	for j := i; j < sc.end; {
		if sc.s[j] != c {
			j++
			continue
		}
		m := run(sc.s[j:sc.end], c)
		if m == n {
			return j + m
		}
		j += m
	}
	return -1
}

// inlineMath returns the end of "$…$" or "$$…$$" at i, or 0 (O-MATH-01).
func (sc *scan) inlineMath(i int) int {
	if sc.at(i, "$$") {
		if k := bytes.Index(sc.s[i+2:sc.end], []byte("$$")); k > 0 {
			return i + 2 + k + 2
		}
		return 0
	}
	if i+1 >= sc.end || sc.s[i+1] == ' ' || sc.s[i+1] == '\t' || sc.s[i+1] == '$' {
		return 0
	}
	for j := i + 2; j < sc.end; j++ {
		if sc.s[j] == '\\' {
			j++
			continue
		}
		if sc.s[j] == '$' && sc.s[j-1] != ' ' && sc.s[j-1] != '\t' && (j+1 >= sc.end || !isDigit(sc.s[j+1])) {
			return j + 1
		}
	}
	return 0
}

// htmlTag returns the end of an HTML tag or autolink at i, or 0. The text
// between tags is still scanned, so links in inline HTML count (O-HTML-01).
func (sc *scan) htmlTag(i int) int {
	j := i + 1
	if j < sc.end && sc.s[j] == '/' {
		j++
	}
	if j >= sc.end || !isASCIILetter(sc.s[j]) {
		return 0
	}
	e := bytes.IndexByte(sc.s[j:sc.end], '>')
	if e < 0 || bytes.IndexByte(sc.s[j:j+e], '<') >= 0 {
		return 0
	}
	return j + e + 1
}

// url returns the end of a URL with a scheme at i, or 0, so "#frag" in it
// isn't read as a tag.
func (sc *scan) url(i int) int {
	j := i
	for j < sc.end && (isASCIIAlnum(sc.s[j]) || sc.s[j] == '+' || sc.s[j] == '.' || sc.s[j] == '-') {
		j++
	}
	if !sc.at(j, "://") {
		return 0
	}
	for j < sc.end && sc.s[j] != ' ' && sc.s[j] != '\t' && sc.s[j] != '<' {
		j++
	}
	return j
}

// tag parses "#tag" at i (O-TAG-01).
func (sc *scan) tag(i int) (int, bool) {
	if i > sc.start && i != sc.tagEnd {
		r, _ := utf8.DecodeLastRune(sc.s[sc.start:i])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '(' {
			return 0, false
		}
	}
	j := i + 1
	nonDigit := false
	for j < sc.end {
		r, size := utf8.DecodeRune(sc.s[j:sc.end])
		if !isTagRune(r) {
			break
		}
		if !unicode.IsDigit(r) {
			nonDigit = true
		}
		j += size
	}
	if j == i+1 || !nonDigit {
		return 0, false
	}
	sc.p.d.Tags = append(sc.p.d.Tags, Tag{Span: Span{i, j}, Name: string(sc.s[i+1 : j])})
	sc.tagEnd = j
	return j, true
}

// isTagRune excludes whitespace, ASCII punctuation other than "-", "_" and
// "/", and the Unicode general and supplemental punctuation blocks.
func isTagRune(r rune) bool {
	switch {
	case r == utf8.RuneError || unicode.IsSpace(r) || r < 0x20:
		return false
	case r < 0x80:
		return !strings.ContainsRune("'!\"#$%&()*+,.:;<=>?@^`{|}~[]\\", r)
	case r >= 0x2000 && r <= 0x206F, r >= 0x2E00 && r <= 0x2E7F:
		return false
	}
	return true
}

// NormTag returns a tag's comparison form: lowercased, without a leading
// "#" or trailing "/".
func NormTag(name string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimPrefix(name, "#"), "/"))
}

var imageExts = map[string]bool{"avif": true, "bmp": true, "gif": true, "jpeg": true, "jpg": true, "png": true, "svg": true, "webp": true}

func isImage(target string) bool {
	dot := strings.LastIndexByte(target, '.')
	return dot >= 0 && imageExts[strings.ToLower(target[dot+1:])]
}

// parseSize parses "W" or "WxH".
func parseSize(s string) (w, h int, ok bool) {
	ws, hs, hasH := strings.Cut(s, "x")
	w, ok = atoi(ws)
	if !ok {
		return 0, 0, false
	}
	if hasH {
		if h, ok = atoi(hs); !ok {
			return 0, 0, false
		}
	}
	return w, h, true
}

func atoi(s string) (int, bool) {
	if s == "" || len(s) > 6 {
		return 0, false
	}
	n := 0
	for _, c := range []byte(s) {
		if !isDigit(c) {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func hasScheme(s string) bool {
	if len(s) == 0 || !isASCIILetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == ':' {
			return true
		}
		if !isASCIIAlnum(c) && c != '+' && c != '.' && c != '-' {
			return false
		}
	}
	return false
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

func runeLen(s []byte, i int) int {
	if s[i] < utf8.RuneSelf {
		return 1
	}
	_, n := utf8.DecodeRune(s[i:])
	return n
}

func isDigit(c byte) bool       { return c >= '0' && c <= '9' }
func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isASCIIAlnum(c byte) bool  { return isASCIILetter(c) || isDigit(c) }

func isASCIIPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}
