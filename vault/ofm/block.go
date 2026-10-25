package ofm

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

var bom = []byte{0xEF, 0xBB, 0xBF}

// maxDepth bounds container nesting (quotes, list items); deeper content is
// read as plain paragraphs.
const maxDepth = 32

// line is one source line, as seen from inside its containers.
type line struct {
	start   int // first byte of the line
	content int // first byte after container prefixes ("> ", list indentation)
	end     int // end of the line's content, before "\r\n" or "\n"
	next    int // first byte of the next line
}

type parser struct {
	d            *Doc
	src          []byte
	calloutDepth int
}

// Parse scans src. It never fails.
func Parse(src []byte) *Doc {
	d := &Doc{Src: src}
	from := 0
	if bytes.HasPrefix(src, bom) {
		d.BOM, from = true, 3
	}
	p := &parser{d: d, src: src}
	lines := p.frontmatter(splitLines(src, from))
	d.Blocks = p.blocks(lines, 0)
	d.sections()
	return d
}

func splitLines(src []byte, from int) []line {
	var out []line
	for i := from; i < len(src); {
		end, next := len(src), len(src)
		if j := bytes.IndexByte(src[i:], '\n'); j >= 0 {
			end, next = i+j, i+j+1
		}
		if end > i && src[end-1] == '\r' {
			end--
		}
		out = append(out, line{i, i, end, next})
		i = next
	}
	return out
}

func (p *parser) text(l line) []byte { return p.src[l.content:l.end] }

// frontmatter recognises a properties block (O-FM-01, O-FM-02): a line that is
// exactly "---" at the start, up to the next line that is exactly "---".
func (p *parser) frontmatter(lines []line) []line {
	if len(lines) == 0 || string(p.text(lines[0])) != "---" {
		return lines
	}
	for k := 1; k < len(lines); k++ {
		if string(p.text(lines[k])) != "---" {
			continue
		}
		fm := &Frontmatter{
			Span: Span{lines[0].start, lines[k].end},
			Body: Span{lines[0].next, lines[k].start},
		}
		fm.JSON = bytes.HasPrefix(bytes.TrimSpace(p.src[fm.Body.Start:fm.Body.End]), []byte("{"))
		p.d.Frontmatter = fm
		return lines[k+1:]
	}
	return lines
}

// blocks parses a run of lines into blocks.
func (p *parser) blocks(lines []line, depth int) []Block {
	var out []Block
	blankBefore := true
	for i := 0; i < len(lines); {
		l := lines[i]
		t := p.text(l)
		if isBlank(t) {
			i++
			blankBefore = true
			continue
		}
		var b Block
		var next int
		if cols, _ := indent(t, 4); cols >= 4 {
			b, next = p.indentedCode(lines, i)
		} else {
			_, nb := indent(t, 3)
			b, next = p.block(lines, i, nb, depth, out, blankBefore)
		}
		if b.Kind != 0 {
			out = append(out, b)
		}
		i = next
		blankBefore = false
	}
	return out
}

// block parses the block starting at lines[i], whose content starts nb bytes
// in. A zero Block means the line was consumed without producing a block.
func (p *parser) block(lines []line, i, nb, depth int, prev []Block, blankBefore bool) (Block, int) {
	l := lines[i]
	s := p.text(l)[nb:]
	sOff := l.content + nb
	switch {
	case isFenceOpen(s):
		return p.fencedCode(lines, i, s, sOff)
	case bytes.HasPrefix(s, []byte("%%")) && !bytes.Contains(s[2:], []byte("%%")):
		return p.delimited(lines, i, sOff, "%%", BlockComment)
	case bytes.HasPrefix(s, []byte("$$")) && !bytes.Contains(s[2:], []byte("$$")):
		return p.delimited(lines, i, sOff, "$$", BlockMath)
	}
	if lvl, ts, te, ok := atxHeading(s); ok {
		span := Span{sOff, l.end}
		if depth == 0 {
			p.d.Headings = append(p.d.Headings, Heading{Span: span, Level: lvl, Text: string(s[ts:te])})
		}
		p.inline(sOff+ts, sOff+te, false)
		return Block{Kind: BlockHeading, Span: span}, i + 1
	}
	if isThematicBreak(s) {
		return Block{Kind: BlockThematicBreak, Span: Span{sOff, l.end}}, i + 1
	}
	if s[0] == '>' {
		return p.quote(lines, i, sOff, depth)
	}
	if _, ok := listMarker(s); ok {
		return p.list(lines, i, depth)
	}
	if kind := htmlStart(s); kind != htmlNone {
		return p.htmlBlock(lines, i, sOff, kind)
	}
	if bytes.IndexByte(s, '|') >= 0 && i+1 < len(lines) && isDelimiterRow(p.text(lines[i+1])) {
		return p.table(lines, i, sOff)
	}
	return p.paragraph(lines, i, sOff, depth, prev, blankBefore)
}

func (p *parser) indentedCode(lines []line, i int) (Block, int) {
	last := i
	for j := i + 1; j < len(lines); j++ {
		t := p.text(lines[j])
		if isBlank(t) {
			continue
		}
		if cols, _ := indent(t, 4); cols < 4 {
			break
		}
		last = j
	}
	span := Span{lines[i].content, lines[last].end}
	p.d.Code = append(p.d.Code, Code{Kind: CodeIndented, Span: span})
	return Block{Kind: BlockCode, Span: span}, last + 1
}

func (p *parser) fencedCode(lines []line, i int, s []byte, sOff int) (Block, int) {
	ch, n := s[0], run(s, s[0])
	info := strings.TrimSpace(string(s[n:]))
	end, next := lines[len(lines)-1].end, len(lines) // unclosed: to the end of the container
	for j := i + 1; j < len(lines); j++ {
		t := p.text(lines[j])
		_, nb := indent(t, 3)
		if c := t[nb:]; run(c, ch) >= n && isBlank(c[run(c, ch):]) {
			end, next = lines[j].end, j+1
			break
		}
	}
	span := Span{sOff, end}
	p.d.Code = append(p.d.Code, Code{Kind: CodeFenced, Span: span, Info: info})
	return Block{Kind: BlockCode, Span: span}, next
}

// delimited parses a block comment or math block: from an opening "%%" or
// "$$" to the next line containing the delimiter, or to the end of the
// container when unclosed (O-CMT-01, O-MATH-01).
func (p *parser) delimited(lines []line, i, sOff int, delim string, kind BlockKind) (Block, int) {
	comment := kind == BlockComment
	if comment {
		p.inlineLinks(sOff+2, lines[i].end)
	}
	inner := Span{sOff, lines[len(lines)-1].end}
	end, next := inner.End, len(lines)
	for j := i + 1; j < len(lines); j++ {
		t := p.text(lines[j])
		k := bytes.Index(t, []byte(delim))
		if k < 0 {
			if comment {
				p.inlineLinks(lines[j].content, lines[j].end)
			}
			continue
		}
		if comment {
			p.inlineLinks(lines[j].content, lines[j].content+k)
		}
		inner.End = lines[j].content + k + len(delim)
		end, next = lines[j].end, j+1
		break
	}
	if comment {
		p.d.Comments = append(p.d.Comments, inner)
	} else {
		p.d.Math = append(p.d.Math, inner)
	}
	return Block{Kind: kind, Span: Span{sOff, end}}, next
}

func (p *parser) quote(lines []line, i, sOff, depth int) (Block, int) {
	var inner []line
	j := i
	for ; j < len(lines); j++ {
		t := p.text(lines[j])
		_, nb := indent(t, 3)
		if s := t[nb:]; len(s) > 0 && s[0] == '>' {
			c := lines[j].content + nb + 1
			if c < lines[j].end && (p.src[c] == ' ' || p.src[c] == '\t') {
				c++
			}
			inner = append(inner, line{lines[j].start, c, lines[j].end, lines[j].next})
			continue
		}
		// Lazy continuation of a paragraph inside the quote.
		if len(inner) > 0 && !isBlank(t) && !isBlank(p.text(inner[len(inner)-1])) && !p.interrupts(t) {
			inner = append(inner, lines[j])
			continue
		}
		break
	}
	b := Block{Kind: BlockQuote, Span: Span{sOff, lines[j-1].end}}
	c, isCallout := parseCallout(p.text(inner[0]))
	if isCallout {
		p.calloutDepth++
		c.Span, c.Depth = b.Span, p.calloutDepth
		p.d.Callouts = append(p.d.Callouts, c)
	}
	b.Children = p.nested(inner, depth)
	if isCallout {
		p.calloutDepth--
	}
	return b, j
}

// nested parses container content, or reads it as paragraphs past maxDepth.
func (p *parser) nested(lines []line, depth int) []Block {
	if depth+1 < maxDepth {
		return p.blocks(lines, depth+1)
	}
	if len(lines) == 0 {
		return nil
	}
	return []Block{{Kind: BlockParagraph, Span: Span{lines[0].content, lines[len(lines)-1].end}}}
}

func (p *parser) list(lines []line, i, depth int) (Block, int) {
	var items []Block
	var first marker
	j := i
	for {
		t := p.text(lines[j])
		_, nb := indent(t, 3)
		s := t[nb:]
		m, ok := listMarker(s)
		if !ok || (len(items) > 0 && !m.sameList(first)) {
			break
		}
		if len(items) == 0 {
			first = m
		}
		markerStart := lines[j].content + nb
		contentCol := indentCols(t[:nb]) + m.width
		itemLines := []line{{lines[j].start, min(markerStart+m.width, lines[j].end), lines[j].end, lines[j].next}}
		last := j
		k := j + 1
		for ; k < len(lines); k++ {
			tk := p.text(lines[k])
			if isBlank(tk) {
				itemLines = append(itemLines, line{lines[k].start, lines[k].end, lines[k].end, lines[k].next})
				continue
			}
			if cols, nbk := indent(tk, contentCol); cols >= contentCol {
				itemLines = append(itemLines, line{lines[k].start, lines[k].content + nbk, lines[k].end, lines[k].next})
				last = k
				continue
			}
			if last == k-1 && !p.interrupts(tk) && setextLevel(tk) == 0 {
				itemLines = append(itemLines, lines[k])
				last = k
				continue
			}
			break
		}
		itemLines = itemLines[:last-j+1]
		item := Block{Kind: BlockListItem, Span: Span{markerStart, lines[last].end}}
		if box, status, ok := taskBox(p.src, itemLines[0]); ok {
			p.d.Tasks = append(p.d.Tasks, Task{Span: item.Span, Status: status, Box: box})
			// The item's text starts after the box, so "[" in it can't start a link.
			itemLines[0].content = box.End
		}
		item.Children = p.nested(itemLines, depth)
		items = append(items, item)
		// The next item may follow after blank lines.
		m2 := last + 1
		for m2 < len(lines) && isBlank(p.text(lines[m2])) {
			m2++
		}
		if m2 >= len(lines) {
			j = m2
			break
		}
		if cols, _ := indent(p.text(lines[m2]), 4); cols >= 4 {
			j = last + 1
			break
		}
		if _, ok := listMarker(bytes.TrimLeft(p.text(lines[m2]), " \t")); !ok {
			j = last + 1
			break
		}
		j = m2
	}
	if len(items) == 0 { // unreachable: the caller saw a marker
		return p.paragraph(lines, i, lines[i].content, depth, nil, false)
	}
	return Block{Kind: BlockList, Span: Span{items[0].Start, items[len(items)-1].End}, Children: items}, j
}

func (p *parser) htmlBlock(lines []line, i, sOff int, kind htmlKind) (Block, int) {
	last := i
	if kind == htmlComment {
		last = len(lines) - 1
		for j := i; j < len(lines); j++ {
			t := p.text(lines[j])
			if j == i {
				t = p.src[sOff+4 : lines[j].end]
			}
			if bytes.Contains(t, []byte("-->")) {
				last = j
				break
			}
		}
	} else {
		for last+1 < len(lines) && !isBlank(p.text(lines[last+1])) {
			last++
		}
	}
	return Block{Kind: BlockHTML, Span: Span{sOff, lines[last].end}}, last + 1
}

func (p *parser) table(lines []line, i, sOff int) (Block, int) {
	p.inline(sOff, lines[i].end, true)
	j := i + 2
	for ; j < len(lines); j++ {
		t := p.text(lines[j])
		if isBlank(t) || p.interrupts(t) {
			break
		}
		p.inline(lines[j].content, lines[j].end, true)
	}
	return Block{Kind: BlockTable, Span: Span{sOff, lines[j-1].end}}, j
}

func (p *parser) paragraph(lines []line, i, sOff, depth int, prev []Block, blankBefore bool) (Block, int) {
	j := i + 1
	setext := 0
	for ; j < len(lines); j++ {
		t := p.text(lines[j])
		if isBlank(t) {
			break
		}
		if setext = setextLevel(t); setext > 0 {
			break
		}
		if p.interrupts(t) {
			break
		}
	}
	if setext > 0 {
		var parts []string
		for k := i; k < j; k++ {
			start := lines[k].content
			if k == i {
				start = sOff
			}
			parts = append(parts, strings.TrimSpace(string(p.src[start:lines[k].end])))
			p.inline(start, lines[k].end, false)
		}
		span := Span{sOff, lines[j].end}
		if depth == 0 {
			p.d.Headings = append(p.d.Headings, Heading{Span: span, Level: setext, Text: strings.Join(parts, " "), Setext: true})
		}
		return Block{Kind: BlockHeading, Span: span}, j + 1
	}

	// A "^id" line on its own labels the list, quote or table before it.
	if j == i+1 && blankBefore && len(prev) > 0 {
		if id, ok := standaloneID(p.src[sOff:lines[i].end]); ok {
			switch pb := &prev[len(prev)-1]; pb.Kind {
			case BlockList, BlockQuote, BlockTable:
				idStart := sOff + bytes.IndexByte(p.src[sOff:lines[i].end], '^')
				pb.ID = id
				p.d.BlockIDs = append(p.d.BlockIDs, BlockID{ID: id, Span: Span{idStart, idStart + 1 + len(id)}, Block: pb.Span})
				return Block{}, j
			}
		}
	}

	b := Block{Kind: BlockParagraph, Span: Span{sOff, lines[j-1].end}}
	for k := i; k < j; k++ {
		start, end := lines[k].content, lines[k].end
		if k == i {
			start = sOff
		}
		if k == j-1 {
			if at, id, ok := trailingID(p.src[start:end]); ok {
				b.ID = id
				idStart := start + at
				p.d.BlockIDs = append(p.d.BlockIDs, BlockID{ID: id, Span: Span{idStart, idStart + 1 + len(id)}, Block: b.Span})
				end = idStart
			}
		}
		p.inline(start, end, false)
	}
	return b, j
}

// interrupts reports whether t starts a block that ends a paragraph.
func (p *parser) interrupts(t []byte) bool {
	if cols, _ := indent(t, 4); cols >= 4 {
		return false
	}
	_, nb := indent(t, 3)
	s := t[nb:]
	if len(s) == 0 {
		return false
	}
	if isFenceOpen(s) || isThematicBreak(s) || s[0] == '>' ||
		bytes.HasPrefix(s, []byte("%%")) || bytes.HasPrefix(s, []byte("$$")) || htmlStart(s) != htmlNone {
		return true
	}
	if _, _, _, ok := atxHeading(s); ok {
		return true
	}
	if m, ok := listMarker(s); ok && !isBlank(s[m.width:]) {
		return true
	}
	return false
}

// sections derives heading sections from the top-level headings.
func (d *Doc) sections() {
	for k, h := range d.Headings {
		end := len(d.Src)
		for _, n := range d.Headings[k+1:] {
			if n.Level <= h.Level {
				end = n.Start
				break
			}
		}
		d.Sections = append(d.Sections, Section{Span: Span{h.Start, end}, Heading: k})
	}
}

// --- line-level recognisers ---

func isBlank(t []byte) bool {
	for _, c := range t {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}

// indent counts leading whitespace columns (tabs to the next multiple of 4),
// stopping once max columns are reached. It returns the columns and bytes.
func indent(t []byte, max int) (cols, n int) {
	for n < len(t) && cols < max {
		switch t[n] {
		case ' ':
			cols++
		case '\t':
			cols += 4 - cols%4
		default:
			return cols, n
		}
		n++
	}
	return cols, n
}

func indentCols(t []byte) int {
	c, _ := indent(t, len(t)*4+1)
	return c
}

// run counts the leading bytes of s equal to c.
func run(s []byte, c byte) int {
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	return n
}

func isFenceOpen(s []byte) bool {
	if len(s) < 3 || (s[0] != '`' && s[0] != '~') {
		return false
	}
	n := run(s, s[0])
	return n >= 3 && (s[0] != '`' || bytes.IndexByte(s[n:], '`') < 0)
}

func atxHeading(s []byte) (level, textStart, textEnd int, ok bool) {
	n := run(s, '#')
	if n < 1 || n > 6 || (n < len(s) && s[n] != ' ' && s[n] != '\t') {
		return 0, 0, 0, false
	}
	start, end := n, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	// A closing run of #s, preceded by a space or filling the text, is dropped.
	k := end
	for k > start && s[k-1] == '#' {
		k--
	}
	if k < end && (k == start || s[k-1] == ' ' || s[k-1] == '\t') {
		end = k
		for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
			end--
		}
	}
	return n, start, end, true
}

func isThematicBreak(s []byte) bool {
	var c byte
	n := 0
	for _, b := range s {
		switch {
		case b == ' ' || b == '\t':
		case (b == '-' || b == '*' || b == '_') && (c == 0 || b == c):
			c = b
			n++
		default:
			return false
		}
	}
	return n >= 3
}

// setextLevel returns 1 or 2 if t is a setext underline, otherwise 0.
func setextLevel(t []byte) int {
	if cols, _ := indent(t, 4); cols >= 4 {
		return 0
	}
	s := bytes.TrimSpace(t)
	if len(s) == 0 || (s[0] != '=' && s[0] != '-') || run(s, s[0]) != len(s) {
		return 0
	}
	if s[0] == '=' {
		return 1
	}
	return 2
}

type marker struct {
	width   int  // marker plus the spaces after it, in bytes
	ordered bool // "1." or "1)" rather than "-", "*" or "+"
	ch      byte // the bullet, or the ordered delimiter
}

func (m marker) sameList(o marker) bool { return m.ordered == o.ordered && m.ch == o.ch }

func listMarker(s []byte) (marker, bool) {
	var m marker
	n := 0
	switch {
	case len(s) > 0 && (s[0] == '-' || s[0] == '*' || s[0] == '+'):
		m.ch, n = s[0], 1
	default:
		for n < len(s) && n < 9 && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		if n == 0 || n >= len(s) || (s[n] != '.' && s[n] != ')') {
			return m, false
		}
		m.ordered, m.ch = true, s[n]
		n++
	}
	if n < len(s) && s[n] != ' ' && s[n] != '\t' {
		return m, false
	}
	sp := 0
	for n+sp < len(s) && s[n+sp] == ' ' {
		sp++
	}
	if sp == 0 && n+sp < len(s) && s[n] == '\t' {
		sp = 1
	}
	if sp > 4 || n+sp == len(s) {
		sp = min(sp, 1)
	}
	m.width = n + sp
	return m, true
}

// taskBox recognises "[c]" at the start of a list item's content, followed
// by a space or the end of the line (O-TASK-01).
func taskBox(src []byte, l line) (Span, rune, bool) {
	s := src[l.content:l.end]
	if len(s) < 3 || s[0] != '[' {
		return Span{}, 0, false
	}
	r, size := utf8.DecodeRune(s[1:])
	if r == utf8.RuneError || 1+size >= len(s) || s[1+size] != ']' || r == '\n' {
		return Span{}, 0, false
	}
	end := 2 + size
	if end < len(s) && s[end] != ' ' && s[end] != '\t' {
		return Span{}, 0, false
	}
	return Span{l.content, l.content + end}, r, true
}

type htmlKind uint8

const (
	htmlNone htmlKind = iota
	htmlComment
	htmlBlockTag
)

// htmlBlockTags are the CommonMark block-level tag names that start an HTML
// block, which runs to the next blank line.
var htmlBlockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "base": true, "basefont": true, "blockquote": true,
	"body": true, "caption": true, "center": true, "col": true, "colgroup": true, "dd": true,
	"details": true, "dialog": true, "dir": true, "div": true, "dl": true, "dt": true,
	"fieldset": true, "figcaption": true, "figure": true, "footer": true, "form": true, "frame": true,
	"frameset": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"head": true, "header": true, "hr": true, "html": true, "iframe": true, "legend": true,
	"li": true, "link": true, "main": true, "menu": true, "menuitem": true, "nav": true,
	"noframes": true, "ol": true, "optgroup": true, "option": true, "p": true, "param": true,
	"pre": true, "script": true, "search": true, "section": true, "style": true, "summary": true,
	"table": true, "tbody": true, "td": true, "textarea": true, "tfoot": true, "th": true,
	"thead": true, "title": true, "tr": true, "track": true, "ul": true,
}

func htmlStart(s []byte) htmlKind {
	if bytes.HasPrefix(s, []byte("<!--")) {
		return htmlComment
	}
	if len(s) < 2 || s[0] != '<' {
		return htmlNone
	}
	i := 1
	if s[i] == '/' {
		i++
	}
	j := i
	for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9') {
		j++
	}
	if j == i || !htmlBlockTags[strings.ToLower(string(s[i:j]))] {
		return htmlNone
	}
	if j == len(s) || s[j] == ' ' || s[j] == '\t' || s[j] == '>' || bytes.HasPrefix(s[j:], []byte("/>")) {
		return htmlBlockTag
	}
	return htmlNone
}

func isDelimiterRow(t []byte) bool {
	s := bytes.TrimSpace(t)
	if bytes.IndexByte(s, '|') < 0 || bytes.IndexByte(s, '-') < 0 {
		return false
	}
	for _, c := range s {
		if c != '|' && c != '-' && c != ':' && c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}

func isIDByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
}

// trailingID finds " ^id" at the end of a line (O-BLK-01). It returns the
// offset of "^" within t.
func trailingID(t []byte) (at int, id string, ok bool) {
	end := len(t)
	for end > 0 && (t[end-1] == ' ' || t[end-1] == '\t') {
		end--
	}
	k := end
	for k > 0 && isIDByte(t[k-1]) {
		k--
	}
	if k == end || k < 2 || t[k-1] != '^' || (t[k-2] != ' ' && t[k-2] != '\t') {
		return 0, "", false
	}
	return k - 1, string(t[k:end]), true
}

// standaloneID recognises a line holding only "^id".
func standaloneID(t []byte) (string, bool) {
	s := bytes.TrimSpace(t)
	if len(s) < 2 || s[0] != '^' {
		return "", false
	}
	for _, c := range s[1:] {
		if !isIDByte(c) {
			return "", false
		}
	}
	return string(s[1:]), true
}
