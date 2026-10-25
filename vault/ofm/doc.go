// Package ofm scans Obsidian Flavored Markdown into typed byte spans.
//
// Parse never fails and never panics: text it doesn't recognise is a
// paragraph. Every span is a half-open byte range [Start, End) into the
// source, so callers can patch exactly the bytes they mean to change. The
// rules follow the pinned Obsidian docs and the probe results in
// docs/DECISIONS.md (D-006).
package ofm

// Span is a half-open byte range [Start, End) of the source.
type Span struct {
	Start, End int
}

// Len returns the span's length in bytes.
func (s Span) Len() int { return s.End - s.Start }

// BlockKind is the type of a block.
type BlockKind uint8

const (
	BlockParagraph BlockKind = iota + 1
	BlockHeading
	BlockThematicBreak
	BlockCode // fenced or indented
	BlockMath
	BlockComment
	BlockHTML
	BlockQuote // includes callouts
	BlockList
	BlockListItem
	BlockTable
)

var blockNames = [...]string{"", "paragraph", "heading", "thematic-break", "code", "math", "comment", "html", "quote", "list", "list-item", "table"}

func (k BlockKind) String() string {
	if int(k) < len(blockNames) {
		return blockNames[k]
	}
	return "unknown"
}

// Block is a block-level element. Children holds the blocks inside a quote
// or list item, and the items of a list.
type Block struct {
	Kind BlockKind
	Span
	Children []Block
	// ID is the block ID that labels this block ("" for none), as written.
	ID string
}

// Frontmatter is the YAML or JSON properties block at the top of a note.
type Frontmatter struct {
	Span      // from the opening "---" to the end of the closing "---" line, excluding its line ending
	Body Span // the text between the fence lines
	JSON bool // the body is a JSON object
}

// Heading is a top-level ATX or setext heading.
type Heading struct {
	Span          // the heading line(s), excluding the final line ending
	Level  int    // 1–6
	Text   string // as written, with markers and closing #s removed
	Setext bool
}

// Section is a heading plus everything up to the next heading of the same
// or a higher level (a lower or equal level number).
type Section struct {
	Span
	Heading int // index into Doc.Headings
}

// BlockID is a "^id" that labels a block.
type BlockID struct {
	ID    string // as written, without "^"
	Span         // the "^id" text
	Block Span   // the block it labels
}

// CodeKind distinguishes code spans.
type CodeKind uint8

const (
	CodeFenced CodeKind = iota + 1
	CodeIndented
	CodeInline
)

// Code is a code block or inline code span. Its content is opaque.
type Code struct {
	Kind CodeKind
	Span
	Info string // the fence info string, such as "mermaid"
}

// Callout is a blockquote that starts with "[!type]".
type Callout struct {
	Span         // the whole quote
	Type  string // as written, lowercased
	Canon string // the canonical built-in type; "note" for unknown types
	Fold  byte   // '+', '-' or 0
	Title string // the title text after the type, trimmed
	Depth int    // 1 for a top-level callout
}

// Task is a list item that starts with "[c]".
type Task struct {
	Span        // the list item
	Status rune // the character between the brackets; ' ' is incomplete
	Box    Span // the "[c]" text
}

// Doc is a scanned note.
type Doc struct {
	Src         []byte
	BOM         bool
	Frontmatter *Frontmatter
	Blocks      []Block // top-level blocks
	Headings    []Heading
	Sections    []Section
	BlockIDs    []BlockID
	Links       []Link
	Tags        []Tag
	Callouts    []Callout
	Tasks       []Task
	Code        []Code
	Comments    []Span
	Math        []Span
	Highlights  []Span
	Footnotes   []Span // "[^id]" references and "^[…]" inline footnotes
}

// BodyStart returns the offset where the note body starts: after the
// frontmatter and its line ending, or after the BOM.
func (d *Doc) BodyStart() int {
	if d.Frontmatter == nil {
		if d.BOM {
			return 3
		}
		return 0
	}
	return skipEOL(d.Src, d.Frontmatter.End)
}

// skipEOL returns the offset after the line ending at i, if there is one.
func skipEOL(src []byte, i int) int {
	if i < len(src) && src[i] == '\r' {
		i++
	}
	if i < len(src) && src[i] == '\n' {
		i++
	}
	return i
}
