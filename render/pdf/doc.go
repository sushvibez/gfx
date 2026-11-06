package pdf

// Document structure: the cross-reference tables that say where each object
// lives, the trailer that names the catalog, and the page tree.

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
)

// Document is a parsed PDF file: its object index and page list.
type Document struct {
	buf     []byte
	offsets map[int]int64      // object number -> byte offset
	inStrm  map[int]compressed // object number -> (object stream, index)
	cache   map[int]Object     // resolved objects, so a shared font is read once
	trailer Dict
	pages   []*Page
	strms   map[int][]Object // decoded object streams, by stream object number
}

// compressed locates an object inside an object stream.
type compressed struct {
	strm, idx int
}

// Page is one page of the document with the attributes it inherits.
type Page struct {
	doc       *Document
	dict      Dict
	Index     int     // 0-based page number
	Width     float32 // /MediaBox size in points
	Height    float32
	Rotate    int // 0, 90, 180 or 270
	Resources Dict
	// originX/originY are the MediaBox's lower-left corner, which is not
	// always (0, 0) — content coordinates are relative to it.
	originX, originY float32
}

// Open reads a PDF file.
func Open(path string) (*Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Load(b)
}

// Load reads a PDF from memory.
func Load(b []byte) (*Document, error) {
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		// Some files carry junk before the header; find it within the first
		// kilobyte, as readers are expected to.
		if i := bytes.Index(b[:min(1024, len(b))], []byte("%PDF-")); i > 0 {
			b = b[i:]
		} else {
			return nil, fmt.Errorf("pdf: not a PDF file")
		}
	}
	d := &Document{
		buf:     b,
		offsets: map[int]int64{},
		inStrm:  map[int]compressed{},
		cache:   map[int]Object{},
		strms:   map[int][]Object{},
	}
	if err := d.readXref(); err != nil {
		// A broken or missing xref is common in the wild; scanning for
		// "N G obj" recovers the file's objects directly.
		d.scanObjects()
	}
	if len(d.offsets) == 0 && len(d.inStrm) == 0 {
		return nil, fmt.Errorf("pdf: no objects found")
	}
	if _, ok := d.trailer["Encrypt"]; ok {
		return nil, fmt.Errorf("pdf: encrypted documents are not supported")
	}
	if err := d.readPages(); err != nil {
		return nil, err
	}
	return d, nil
}

// PageCount is the number of pages.
func (d *Document) PageCount() int { return len(d.pages) }

// Page returns a page by 0-based index (nil when out of range).
func (d *Document) Page(i int) *Page {
	if i < 0 || i >= len(d.pages) {
		return nil
	}
	return d.pages[i]
}

// Info returns a string from the document information dictionary
// (Title, Author, Subject, Keywords, Creator, Producer).
func (d *Document) Info(key Name) string {
	info, _ := d.Resolve(d.trailer["Info"]).(Dict)
	if info == nil {
		return ""
	}
	s, _ := d.Resolve(info[key]).(String)
	return decodeTextString(s)
}

// Resolve follows an indirect reference to the object it names; any other
// object is returned as it is.
func (d *Document) Resolve(o Object) Object {
	for i := 0; i < 32; i++ { // a reference cycle must not hang the reader
		r, ok := o.(Ref)
		if !ok {
			return o
		}
		o = d.object(r.Num)
	}
	return nil
}

// object reads object number num, from the file or from an object stream.
func (d *Document) object(num int) Object {
	if o, ok := d.cache[num]; ok {
		return o
	}
	d.cache[num] = nil // break cycles while this object is being read
	var out Object
	if off, ok := d.offsets[num]; ok {
		out = d.objectAt(off, num)
	} else if c, ok := d.inStrm[num]; ok {
		objs := d.objectStream(c.strm)
		if c.idx < len(objs) {
			out = objs[c.idx]
		}
	}
	d.cache[num] = out
	return out
}

// objectAt parses "num gen obj … endobj" at a byte offset.
func (d *Document) objectAt(off int64, want int) Object {
	if off < 0 || off >= int64(len(d.buf)) {
		return nil
	}
	l := &lexer{b: d.buf, pos: int(off)}
	num, err := strconv.Atoi(l.token())
	if err != nil {
		return nil
	}
	l.token() // generation
	if l.token() != "obj" {
		return nil
	}
	if want >= 0 && num != want {
		// The xref pointed at the wrong object (an incrementally updated
		// file with a stale table): fall back to a scan.
		return nil
	}
	obj, err := l.object()
	if err != nil {
		return nil
	}
	// A stream follows its dictionary.
	if dict, ok := obj.(Dict); ok && l.peekToken() == "stream" {
		l.token()
		// The keyword is followed by CRLF or LF, and nothing else.
		if l.pos < len(d.buf) && d.buf[l.pos] == '\r' {
			l.pos++
		}
		if l.pos < len(d.buf) && d.buf[l.pos] == '\n' {
			l.pos++
		}
		start := l.pos
		n := -1
		if v, ok := numOf(d.Resolve(dict["Length"])); ok {
			n = int(v)
		}
		end := start + n
		if n < 0 || end > len(d.buf) || !d.endstreamNear(end) {
			// A wrong or missing /Length: find the real end.
			end = d.findEndstream(start)
		}
		return &Stream{Dict: dict, Raw: d.buf[start:end], doc: d}
	}
	return obj
}

// endstreamNear reports whether "endstream" follows the given position,
// allowing for the whitespace producers put in front of it.
func (d *Document) endstreamNear(end int) bool {
	if end < 0 || end > len(d.buf) {
		return false
	}
	probe := d.buf[end:min(end+20, len(d.buf))]
	return bytes.Contains(probe, []byte("endstream"))
}

// findEndstream locates the end of stream data whose /Length is unusable.
func (d *Document) findEndstream(start int) int {
	if i := bytes.Index(d.buf[start:], []byte("endstream")); i >= 0 {
		end := start + i
		// Trim the EOL that belongs to the keyword, not the data.
		if end > start && d.buf[end-1] == '\n' {
			end--
		}
		if end > start && d.buf[end-1] == '\r' {
			end--
		}
		return end
	}
	return len(d.buf)
}

// objectStream decodes an /ObjStm and returns its objects in order.
func (d *Document) objectStream(num int) []Object {
	if objs, ok := d.strms[num]; ok {
		return objs
	}
	d.strms[num] = nil // a stream that references itself must not recurse
	strm, _ := d.Resolve(Ref{Num: num}).(*Stream)
	if strm == nil {
		return nil
	}
	data, err := strm.Decode()
	if err != nil {
		return nil
	}
	n := intOr(d, strm.Dict["N"], 0)
	first := intOr(d, strm.Dict["First"], 0)
	if n <= 0 || first <= 0 || first > len(data) {
		return nil
	}
	// The header is N pairs of "objnum offset", offsets relative to First.
	head := &lexer{b: data[:first]}
	offs := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if _, err := strconv.Atoi(head.token()); err != nil {
			break
		}
		off, err := strconv.Atoi(head.token())
		if err != nil {
			break
		}
		offs = append(offs, first+off)
	}
	objs := make([]Object, len(offs))
	for i, off := range offs {
		if off < 0 || off >= len(data) {
			continue
		}
		l := &lexer{b: data, pos: off}
		if o, err := l.object(); err == nil {
			objs[i] = o
		}
	}
	d.strms[num] = objs
	return objs
}

// readXref walks the cross-reference chain from startxref back through
// /Prev, filling the object index. Entries already seen win: the newest
// table in an incrementally updated file is read first.
func (d *Document) readXref() error {
	off, err := d.startxref()
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	for off > 0 && !seen[off] {
		seen[off] = true
		trailer, err := d.readXrefSection(off)
		if err != nil {
			return err
		}
		if d.trailer == nil {
			d.trailer = trailer
		} else {
			for k, v := range trailer { // older sections fill in what is missing
				if _, ok := d.trailer[k]; !ok {
					d.trailer[k] = v
				}
			}
		}
		// A hybrid file keeps some objects in a stream the classic table
		// points at; read it before /Prev so its entries are the newer ones.
		if x, ok := numOf(trailer["XRefStm"]); ok && !seen[int64(x)] {
			seen[int64(x)] = true
			_, _ = d.readXrefSection(int64(x))
		}
		prev, ok := numOf(trailer["Prev"])
		if !ok {
			break
		}
		off = int64(prev)
	}
	if d.trailer == nil {
		return fmt.Errorf("pdf: no trailer")
	}
	return nil
}

// startxref reads the byte offset at the end of the file.
func (d *Document) startxref() (int64, error) {
	tail := d.buf[max(0, len(d.buf)-2048):]
	i := bytes.LastIndex(tail, []byte("startxref"))
	if i < 0 {
		return 0, fmt.Errorf("pdf: no startxref")
	}
	l := &lexer{b: tail, pos: i + len("startxref")}
	off, err := strconv.ParseInt(l.token(), 10, 64)
	if err != nil || off <= 0 || off >= int64(len(d.buf)) {
		return 0, fmt.Errorf("pdf: bad startxref")
	}
	return off, nil
}

// readXrefSection reads one section — a classic table or an xref stream —
// and returns its trailer dictionary.
func (d *Document) readXrefSection(off int64) (Dict, error) {
	if off < 0 || off >= int64(len(d.buf)) {
		return nil, fmt.Errorf("pdf: xref offset out of range")
	}
	l := &lexer{b: d.buf, pos: int(off)}
	if l.peekToken() == "xref" {
		return d.readXrefTable(l)
	}
	// An xref stream: the object at this offset is the table.
	obj := d.objectAt(off, -1)
	strm, ok := obj.(*Stream)
	if !ok {
		return nil, fmt.Errorf("pdf: xref section is neither a table nor a stream")
	}
	if err := d.readXrefStream(strm); err != nil {
		return nil, err
	}
	return strm.Dict, nil
}

// readXrefTable reads a classic "xref" table: subsections of 20-byte entries.
func (d *Document) readXrefTable(l *lexer) (Dict, error) {
	l.token() // "xref"
	for {
		t := l.peekToken()
		if t == "trailer" {
			l.token()
			obj, err := l.object()
			if err != nil {
				return nil, err
			}
			trailer, ok := obj.(Dict)
			if !ok {
				return nil, fmt.Errorf("pdf: trailer is not a dictionary")
			}
			return trailer, nil
		}
		start, err := strconv.Atoi(l.token())
		if err != nil {
			return nil, fmt.Errorf("pdf: bad xref subsection")
		}
		count, err := strconv.Atoi(l.token())
		if err != nil {
			return nil, fmt.Errorf("pdf: bad xref subsection count")
		}
		for i := 0; i < count; i++ {
			offTok, genTok, kind := l.token(), l.token(), l.token()
			_ = genTok
			num := start + i
			if kind != "n" {
				continue // a free entry
			}
			o, err := strconv.ParseInt(offTok, 10, 64)
			if err != nil {
				continue
			}
			d.setOffset(num, o)
		}
	}
}

// readXrefStream reads a /Type /XRef stream's compressed entry table.
func (d *Document) readXrefStream(s *Stream) error {
	data, err := s.Decode()
	if err != nil {
		return err
	}
	ws, _ := d.Resolve(s.Dict["W"]).(Array)
	if len(ws) < 3 {
		return fmt.Errorf("pdf: xref stream without /W")
	}
	w := make([]int, len(ws))
	total := 0
	for i, o := range ws {
		w[i] = intOr(d, o, 0)
		total += w[i]
	}
	if total == 0 {
		return fmt.Errorf("pdf: xref stream with zero-width entries")
	}
	// /Index pairs (first, count); the default covers the whole /Size.
	var index []int
	if arr, ok := d.Resolve(s.Dict["Index"]).(Array); ok {
		for _, o := range arr {
			index = append(index, intOr(d, o, 0))
		}
	}
	if len(index) < 2 {
		index = []int{0, intOr(d, s.Dict["Size"], 0)}
	}
	pos := 0
	field := func(width int) int64 {
		v := int64(0)
		for i := 0; i < width; i++ {
			if pos < len(data) {
				v = v<<8 | int64(data[pos])
			}
			pos++
		}
		return v
	}
	for i := 0; i+1 < len(index); i += 2 {
		first, count := index[i], index[i+1]
		for j := 0; j < count && pos < len(data); j++ {
			num := first + j
			kind := int64(1) // a zero-width type field means "normal"
			if w[0] > 0 {
				kind = field(w[0])
			}
			f2, f3 := field(w[1]), field(w[2])
			switch kind {
			case 1:
				d.setOffset(num, f2)
			case 2:
				d.setInStream(num, int(f2), int(f3))
			}
		}
	}
	return nil
}

// setOffset records a direct object's offset unless a newer section
// already claimed that object number.
func (d *Document) setOffset(num int, off int64) {
	if _, ok := d.offsets[num]; ok {
		return
	}
	if _, ok := d.inStrm[num]; ok {
		return
	}
	d.offsets[num] = off
}

func (d *Document) setInStream(num, strm, idx int) {
	if _, ok := d.offsets[num]; ok {
		return
	}
	if _, ok := d.inStrm[num]; ok {
		return
	}
	d.inStrm[num] = compressed{strm: strm, idx: idx}
}

// scanObjects rebuilds the index by scanning for "N G obj", for files whose
// xref is missing or wrong. The LAST definition of an object number wins,
// which is what an incremental update means.
func (d *Document) scanObjects() {
	needle := []byte(" obj")
	for i := 0; i+4 <= len(d.buf); {
		j := bytes.Index(d.buf[i:], needle)
		if j < 0 {
			break
		}
		at := i + j
		// Walk back over "N G " to the start of the object number.
		k := at
		for f := 0; f < 2; f++ {
			for k > 0 && isWhite(d.buf[k-1]) {
				k--
			}
			for k > 0 && d.buf[k-1] >= '0' && d.buf[k-1] <= '9' {
				k--
			}
		}
		if num, err := strconv.Atoi(string(fieldAt(d.buf, k))); err == nil {
			d.offsets[num] = int64(k) // later definitions overwrite earlier ones
			delete(d.inStrm, num)
		}
		i = at + 4
	}
	// The trailer may still be readable; otherwise find the catalog directly.
	if d.trailer == nil {
		d.trailer = d.findTrailer()
	}
	// Object streams hold objects the scan cannot see; register their contents.
	for num := range d.offsets {
		if s, ok := d.Resolve(Ref{Num: num}).(*Stream); ok {
			if n, _ := d.Resolve(s.Dict["Type"]).(Name); n == "ObjStm" {
				d.registerObjStm(num, s)
			}
		}
	}
}

// registerObjStm indexes the objects inside an /ObjStm found by scanning.
func (d *Document) registerObjStm(num int, s *Stream) {
	data, err := s.Decode()
	if err != nil {
		return
	}
	first := intOr(d, s.Dict["First"], 0)
	n := intOr(d, s.Dict["N"], 0)
	if first <= 0 || first > len(data) || n <= 0 {
		return
	}
	head := &lexer{b: data[:first]}
	for i := 0; i < n; i++ {
		objNum, err := strconv.Atoi(head.token())
		if err != nil {
			return
		}
		if _, err := strconv.Atoi(head.token()); err != nil {
			return
		}
		if _, ok := d.offsets[objNum]; !ok {
			d.inStrm[objNum] = compressed{strm: num, idx: i}
		}
	}
}

// findTrailer looks for a trailer dictionary, then for any /Type /Catalog
// object, so a file with no usable trailer still opens.
func (d *Document) findTrailer() Dict {
	if i := bytes.LastIndex(d.buf, []byte("trailer")); i >= 0 {
		l := &lexer{b: d.buf, pos: i + len("trailer")}
		if obj, err := l.object(); err == nil {
			if t, ok := obj.(Dict); ok {
				return t
			}
		}
	}
	for num := range d.offsets {
		if dict, ok := d.Resolve(Ref{Num: num}).(Dict); ok {
			if n, _ := d.Resolve(dict["Type"]).(Name); n == "Catalog" {
				return Dict{"Root": Ref{Num: num}}
			}
		}
	}
	return nil
}

// fieldAt reads the run of digits starting at i.
func fieldAt(b []byte, i int) []byte {
	j := i
	for j < len(b) && b[j] >= '0' && b[j] <= '9' {
		j++
	}
	return b[i:j]
}

// readPages walks the page tree into a flat page list, carrying the
// inherited attributes (Resources, MediaBox, Rotate) down as it goes.
func (d *Document) readPages() error {
	root, _ := d.Resolve(d.trailer["Root"]).(Dict)
	var pagesObj Object
	if root != nil {
		pagesObj = root["Pages"]
	}
	pages, _ := d.Resolve(pagesObj).(Dict)
	if pages == nil {
		// No catalog: take every /Type /Page object in file order, which is
		// what a damaged file leaves behind.
		d.collectLoosePages()
		if len(d.pages) == 0 {
			return fmt.Errorf("pdf: no page tree")
		}
		return nil
	}
	d.walkPages(pages, Dict{}, 0)
	if len(d.pages) == 0 {
		d.collectLoosePages()
	}
	if len(d.pages) == 0 {
		return fmt.Errorf("pdf: no pages")
	}
	return nil
}

// inheritable are the page attributes a /Pages node passes to its children.
var inheritable = []Name{"Resources", "MediaBox", "CropBox", "Rotate"}

func (d *Document) walkPages(node Dict, inherited Dict, depth int) {
	if depth > 64 || node == nil { // a cyclic tree must not hang the reader
		return
	}
	// This node's own values override what it inherited.
	own := Dict{}
	for k, v := range inherited {
		own[k] = v
	}
	for _, k := range inheritable {
		if v, ok := node[k]; ok {
			own[k] = v
		}
	}
	kind, _ := d.Resolve(node["Type"]).(Name)
	kids, hasKids := d.Resolve(node["Kids"]).(Array)
	if kind == "Page" || (!hasKids && node["Contents"] != nil) {
		d.addPage(node, own)
		return
	}
	for _, kid := range kids {
		if kd, ok := d.Resolve(kid).(Dict); ok {
			d.walkPages(kd, own, depth+1)
		}
	}
}

// collectLoosePages finds pages without walking the tree.
func (d *Document) collectLoosePages() {
	nums := make([]int, 0, len(d.offsets)+len(d.inStrm))
	for num := range d.offsets {
		nums = append(nums, num)
	}
	for num := range d.inStrm {
		nums = append(nums, num)
	}
	slicesSort(nums)
	for _, num := range nums {
		if dict, ok := d.Resolve(Ref{Num: num}).(Dict); ok {
			if n, _ := d.Resolve(dict["Type"]).(Name); n == "Page" {
				d.addPage(dict, Dict{})
			}
		}
	}
}

// addPage appends a page, resolving its geometry.
func (d *Document) addPage(dict, inherited Dict) {
	p := &Page{doc: d, dict: dict, Index: len(d.pages), Width: 612, Height: 792}
	box := dict["MediaBox"]
	if box == nil {
		box = inherited["MediaBox"]
	}
	if arr, ok := d.Resolve(box).(Array); ok && len(arr) == 4 {
		x0, y0 := floatOr(d, arr[0], 0), floatOr(d, arr[1], 0)
		x1, y1 := floatOr(d, arr[2], 612), floatOr(d, arr[3], 792)
		if w := absf(x1 - x0); w > 1 {
			p.Width = w
		}
		if h := absf(y1 - y0); h > 1 {
			p.Height = h
		}
		p.originX, p.originY = minf(x0, x1), minf(y0, y1)
	}
	rot := dict["Rotate"]
	if rot == nil {
		rot = inherited["Rotate"]
	}
	if v, ok := numOf(d.Resolve(rot)); ok {
		p.Rotate = ((int(v)/90)%4 + 4) % 4 * 90
	}
	res := dict["Resources"]
	if res == nil {
		res = inherited["Resources"]
	}
	p.Resources, _ = d.Resolve(res).(Dict)
	d.pages = append(d.pages, p)
}

// Size is the page's size in points, with rotation applied (a page rotated
// 90 degrees is as wide as it is tall).
func (p *Page) Size() (float32, float32) {
	if p.Rotate == 90 || p.Rotate == 270 {
		return p.Height, p.Width
	}
	return p.Width, p.Height
}

// Content is the page's content streams, concatenated as the spec requires
// (a page may split its operators across several streams).
func (p *Page) Content() []byte {
	var out []byte
	switch c := p.doc.Resolve(p.dict["Contents"]).(type) {
	case *Stream:
		b, err := c.Decode()
		if err == nil {
			out = b
		}
	case Array:
		for _, o := range c {
			if s, ok := p.doc.Resolve(o).(*Stream); ok {
				if b, err := s.Decode(); err == nil {
					out = append(out, b...)
					out = append(out, '\n')
				}
			}
		}
	}
	return out
}

// helpers -------------------------------------------------------------

// numOf reads a PDF number as a float64.
func numOf(o Object) (float64, bool) {
	switch v := o.(type) {
	case int64:
		return float64(v), true
	case float64:
		return v, true
	}
	return 0, false
}

func intOr(d *Document, o Object, def int) int {
	if v, ok := numOf(d.Resolve(o)); ok {
		return int(v)
	}
	return def
}

func floatOr(d *Document, o Object, def float32) float32 {
	if v, ok := numOf(d.Resolve(o)); ok {
		return float32(v)
	}
	return def
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// slicesSort is an insertion sort over a small int slice (page recovery
// only, so the simplest thing that cannot allocate surprises).
func slicesSort(v []int) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j-1] > v[j]; j-- {
			v[j-1], v[j] = v[j], v[j-1]
		}
	}
}

// decodeTextString converts a PDF text string to Go text: UTF-16BE when it
// carries the byte-order mark, PDFDocEncoding (close enough to Latin-1)
// otherwise.
func decodeTextString(s String) string {
	if len(s) >= 2 && s[0] == 0xFE && s[1] == 0xFF {
		var out []rune
		for i := 2; i+1 < len(s); i += 2 {
			r := rune(s[i])<<8 | rune(s[i+1])
			if r >= 0xD800 && r <= 0xDBFF && i+3 < len(s) { // surrogate pair
				lo := rune(s[i+2])<<8 | rune(s[i+3])
				r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
				i += 2
			}
			out = append(out, r)
		}
		return string(out)
	}
	out := make([]rune, 0, len(s))
	for _, c := range s {
		out = append(out, rune(c))
	}
	return string(out)
}
