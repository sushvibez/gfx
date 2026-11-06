// Package md parses Markdown: CommonMark's block and inline structure,
// plus the GitHub extensions people expect of it — tables, task lists,
// strikethrough and bare links. It has no dependencies; rendering lives in
// md/mdrender (drawn with the framework's renderer) and md/mdui (a ui
// element), and HTML in this package is for tests and export.
//
//	doc := md.Parse(src)
//	html := md.HTML(doc)
//
// The parser never fails: anything it cannot read as structure is text,
// which is also what Markdown promises.
package md

// Kind is what a node is.
type Kind int

// Block kinds, then inline kinds.
const (
	Document Kind = iota
	Paragraph
	Heading       // Level 1–6
	ThematicBreak // a rule
	BlockQuote
	List      // Ordered, Start, Tight, Delim
	ListItem  // Task
	CodeBlock // Text, Info (the fence's info string)
	HTMLBlock // Text, raw
	Table     // Align per column; children are TableRows
	TableRow  // Header
	TableCell // Align
	// GitHub's footnotes: a definition ([^label]: ...), which the parser
	// moves to the Footnotes block at the end of the document, numbered
	// in the order they are first referred to (Start), and dropped if
	// nothing refers to them.
	FootnoteDef // Label, Start (its number)
	Footnotes   // the definitions, in order
	// A definition list (PHP Markdown Extra): terms, each followed by
	// one or more definitions on lines starting ": ".
	DefList
	DefTerm // Text, then inlines
	DefDesc

	Text
	SoftBreak
	HardBreak
	Emph
	Strong
	Strike
	Code        // Text
	Link        // Dest, Title; children are the link text
	Image       // Dest, Title; children are the alt text
	HTMLInline  // Text, raw
	FootnoteRef // Label, Start (the note's number), Level (which reference to it this is, from 1)
)

var kindNames = [...]string{"Document", "Paragraph", "Heading", "ThematicBreak", "BlockQuote",
	"List", "ListItem", "CodeBlock", "HTMLBlock", "Table", "TableRow", "TableCell",
	"FootnoteDef", "Footnotes", "DefList", "DefTerm", "DefDesc",
	"Text", "SoftBreak", "HardBreak", "Emph", "Strong", "Strike", "Code", "Link", "Image", "HTMLInline",
	"FootnoteRef"}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "Kind?"
}

// IsBlock reports whether a kind is a block (the rest are inlines).
func (k Kind) IsBlock() bool { return k <= DefDesc }

// Align is a table column's alignment.
type Align int

const (
	AlignNone Align = iota
	AlignLeft
	AlignCenter
	AlignRight
)

// Task is a list item's checkbox.
type Task int

const (
	NoTask Task = iota
	TaskOpen
	TaskDone
)

// Node is one element of the document tree.
type Node struct {
	Kind     Kind
	Children []*Node

	Text  string // Text, Code, CodeBlock, HTMLBlock, HTMLInline
	Level int    // Heading
	Info  string // CodeBlock: the fence's info string ("go", "json title=x")

	Ordered bool // List
	Start   int  // List: the first number of an ordered list
	Tight   bool // List: no blank lines between items or inside them
	Delim   byte // List: '-', '+', '*', '.' or ')'

	Task Task // ListItem

	Dest, Title string // Link, Image

	// Label is a footnote's label; ID is a heading's anchor, the slug
	// GitHub would give it ("Getting started" → "getting-started").
	Label, ID string

	// GFM marks a Document parsed with GitHub's extensions, whose HTML
	// filters the tags GitHub does not pass through (see HTML).
	GFM bool

	Align  []Align // Table: one per column
	Header bool    // TableRow: the header row
	Cell   Align   // TableCell

	// Line is the source line a block starts on, from 1.
	Line int
	// EndLine is the last line with content in the block, for whether a
	// list is loose (blank lines between its items or their blocks).
	EndLine int
}

// Walk calls fn for a node and everything under it, depth first; fn
// returning false skips that node's children.
func Walk(n *Node, fn func(*Node) bool) {
	if !fn(n) {
		return
	}
	for _, c := range n.Children {
		Walk(c, fn)
	}
}

// PlainText is the text a node shows, with no markup: for a heading's
// anchor, a link's tooltip, an image's alt.
func PlainText(n *Node) string {
	var b []byte
	Walk(n, func(c *Node) bool {
		switch c.Kind {
		case Text, Code:
			b = append(b, c.Text...)
		case SoftBreak, HardBreak:
			b = append(b, ' ')
		}
		return true
	})
	return string(b)
}
