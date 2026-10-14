package editor

import (
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Goldmark retains cell spans, but not row or delimiter spans. Capture the
// original lines during transformation so even entirely empty rows can be
// projected without guessing where the table starts or ends.
type tableParser struct{ parser.ParagraphTransformer }

func (p tableParser) Transform(node *ast.Paragraph, reader text.Reader, pc parser.Context) {
	segments := append([]text.Segment(nil), node.Lines().Sliced(0, node.Lines().Len())...)
	parent, previous := node.Parent(), node.PreviousSibling()
	p.ParagraphTransformer.Transform(node, reader, pc)
	if node.Parent() != nil && node.Lines().Len() == len(segments) {
		return
	}
	var next ast.Node
	if node.Parent() != nil {
		next = node.NextSibling()
	} else if previous != nil {
		next = previous.NextSibling()
	} else {
		next = parent.FirstChild()
	}
	if table, ok := next.(*extast.Table); ok {
		start := node.Lines().Len()
		table.SetAttributeString("sourceLines", segments[start:])
	}
}

type projectedCell struct {
	text       string
	start, end int
	format     format
}

type projectedTableRow struct {
	line    int
	prefix  string
	cells   [][]projectedCell
	anchors []int
}

type projectedTable struct {
	rows       []projectedTableRow
	delimiter  projectedTableRow
	widths     []int
	minimums   []int
	alignments []extast.Alignment
}

func (p *projection) tableCell(source string, cell *extast.TableCell, header bool) ([]projectedCell, int) {
	var cells []projectedCell
	width := 0
	if cell.Lines().Len() == 0 {
		return cells, width
	}
	seg := cell.Lines().At(0)
	for pos := seg.Start; pos < seg.Stop; {
		// GFM permits escaped pipes even inside a table's code spans.
		if source[pos] == '\\' && pos+1 < seg.Stop && source[pos+1] == '|' {
			pos++
		}
		if p.hidden[pos] {
			pos++
			continue
		}
		end := min(next(source, pos), seg.Stop)
		value := safeText(source[pos:end])
		w := uniseg.StringWidth(value)
		if source[pos:end] == "\t" {
			w = 4 - width%4
			value = strings.Repeat(" ", w)
		} else if w == 0 {
			value, w = "◌"+value, 1
		}
		f := p.formats[pos]
		if header {
			f |= bold
		}
		// Expand tabs into individual cells so narrow columns can wrap them.
		if source[pos:end] == "\t" {
			for j := 0; j < w; j++ {
				cells = append(cells, projectedCell{" ", pos, end, f})
			}
		} else {
			cells = append(cells, projectedCell{value, pos, end, f})
		}
		width += w
		pos = end
	}
	return cells, width
}

func (p *projection) projectTable(source string, table *extast.Table) {
	value, ok := table.AttributeString("sourceLines")
	if !ok {
		return
	}
	segments := value.([]text.Segment)
	columns := len(table.Alignments)
	t := &projectedTable{
		widths: make([]int, columns), minimums: make([]int, columns),
		alignments: table.Alignments,
	}
	i := 0
	for node := table.FirstChild(); node != nil; node = node.NextSibling() {
		if i == 1 {
			i++ // The delimiter is not an AST row.
		}
		seg := segments[i]
		lineIndex := lineAt(p.lines, seg.Start)
		line := p.lines[lineIndex]
		r := projectedTableRow{lineIndex, p.projectPrefix(source, line.start, seg.Start), make([][]projectedCell, columns), make([]int, columns)}
		column := 0
		for n := node.FirstChild(); n != nil; n = n.NextSibling() {
			cell := n.(*extast.TableCell)
			anchor := line.end
			if cell.Lines().Len() > 0 {
				anchor = cell.Lines().At(0).Start
			}
			r.anchors[column] = anchor
			var width int
			r.cells[column], width = p.tableCell(source, cell, i == 0)
			t.widths[column] = max(t.widths[column], width, 1)
			t.minimums[column] = max(t.minimums[column], 1)
			for _, c := range r.cells[column] {
				t.minimums[column] = max(t.minimums[column], uniseg.StringWidth(c.text))
			}
			column++
		}
		t.rows = append(t.rows, r)
		i++
	}
	seg := segments[1]
	lineIndex := lineAt(p.lines, seg.Start)
	line := p.lines[lineIndex]
	t.delimiter = projectedTableRow{line: lineIndex, prefix: p.projectPrefix(source, line.start, seg.Start), anchors: []int{seg.Start, line.end}}
	p.tables = append(p.tables, t)
	// Width-independent output keeps its natural column sizes.
	for _, r := range t.rows {
		p.tableLines[r.line] = t.renderRow(r, t.widths, r.prefix, r.prefix)[0]
	}
	p.tableLines[lineIndex] = t.renderRule(t.widths, t.delimiter.prefix)
}

func (p *projection) projectPrefix(source string, start, end int) string {
	var prefix strings.Builder
	for pos := start; pos < end; {
		if r, ok := p.replacements[pos]; ok && r.end <= end {
			prefix.WriteString(r.text)
			pos = r.end
		} else {
			prefix.WriteByte(source[pos])
			pos++
		}
	}
	return prefix.String()
}

func (t *projectedTable) renderRule(widths []int, prefix string) []projectedCell {
	var rule strings.Builder
	rule.WriteString(prefix)
	rule.WriteString("├")
	for column, width := range widths {
		if column > 0 {
			rule.WriteString("┼")
		}
		rule.WriteString(strings.Repeat("─", width+2))
	}
	rule.WriteString("┤")
	return []projectedCell{{rule.String(), t.delimiter.anchors[0], t.delimiter.anchors[1], 0}}
}

func (t *projectedTable) renderRow(r projectedTableRow, widths []int, prefix, continuation string) [][]projectedCell {
	wrapped := make([][][]projectedCell, len(widths))
	height := 1
	for column, content := range r.cells {
		wrapped[column] = wrapTableCell(content, widths[column])
		height = max(height, len(wrapped[column]))
	}
	rows := make([][]projectedCell, height)
	for n := range rows {
		rowAnchor := r.anchors[0]
		for _, parts := range wrapped {
			if n < len(parts) && len(parts[n]) > 0 {
				rowAnchor = parts[n][0].start
				break
			}
		}
		add := func(value string, anchor int) {
			rows[n] = append(rows[n], projectedCell{value, anchor, anchor, 0})
		}
		if n == 0 {
			add(prefix, rowAnchor)
		} else {
			add(continuation, rowAnchor)
		}
		add("│", rowAnchor)
		for column, parts := range wrapped {
			var content []projectedCell
			anchor := r.anchors[column]
			if n < len(parts) {
				content = parts[n]
				if len(content) > 0 {
					anchor = content[0].start
				}
			} else if original := r.cells[column]; len(original) > 0 {
				anchor = original[len(original)-1].end
			}
			padding := widths[column] - tableCellWidth(content)
			left := 0
			switch t.alignments[column] {
			case extast.AlignRight:
				left = padding
			case extast.AlignCenter:
				left = padding / 2
			}
			add(strings.Repeat(" ", left+1), anchor)
			rows[n] = append(rows[n], content...)
			end := anchor
			if len(content) > 0 {
				end = content[len(content)-1].end
			}
			add(strings.Repeat(" ", padding-left+1)+"│", end)
		}
	}
	return rows
}

func tableCellWidth(cells []projectedCell) int {
	width := 0
	for _, cell := range cells {
		width += uniseg.StringWidth(cell.text)
	}
	return width
}

func tableSpace(cell projectedCell) bool {
	for _, r := range cell.text {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// Keep source spans and styles attached to every grapheme. Only whitespace at
// a word-wrapping boundary is omitted from the display.
func wrapTableCell(cells []projectedCell, width int) [][]projectedCell {
	var rows [][]projectedCell
	for start := 0; start < len(cells); {
		end, used, space := start, 0, -1
		for end < len(cells) && used+uniseg.StringWidth(cells[end].text) <= width {
			if tableSpace(cells[end]) && end > start && !tableSpace(cells[end-1]) {
				space = end
			}
			used += uniseg.StringWidth(cells[end].text)
			end++
		}
		next := end
		if end < len(cells) {
			if !tableSpace(cells[end]) && space > start {
				end, next = space, space
			}
			for next < len(cells) && tableSpace(cells[next]) {
				next++
			}
		}
		rows = append(rows, cells[start:end])
		start = next
	}
	return rows
}

// A nil entry means the entire table must expose source: even its narrowest
// columns, padding, and borders cannot fit in the available terminal width.
func (p *projection) fitTables(width int) map[int][][]projectedCell {
	lines := make(map[int][][]projectedCell, len(p.tableLines))
	for _, t := range p.tables {
		prefixWidth := uniseg.StringWidth(tablePrefix(t.delimiter.prefix))
		for _, r := range t.rows {
			prefixWidth = max(prefixWidth, uniseg.StringWidth(tablePrefix(r.prefix)))
		}
		budget := width - prefixWidth - 1 - 3*len(t.widths)
		minimum, total := 0, 0
		for i, w := range t.widths {
			total += w
			minimum += t.minimums[i]
		}
		widths := append([]int(nil), t.widths...)
		if budget >= minimum {
			for total > budget {
				widest := -1
				for i, w := range widths {
					if w > t.minimums[i] && (widest < 0 || w > widths[widest]) {
						widest = i
					}
				}
				widths[widest]--
				total--
			}
		}
		for _, r := range t.rows {
			lines[r.line] = nil
			if budget >= minimum {
				// The delimiter's container prefix has continuation indentation
				// rather than the list marker that may open the header row.
				lines[r.line] = t.renderRow(r, widths, tablePrefix(r.prefix), tablePrefix(t.delimiter.prefix))
			}
		}
		lines[t.delimiter.line] = nil
		if budget >= minimum {
			lines[t.delimiter.line] = [][]projectedCell{t.renderRule(widths, tablePrefix(t.delimiter.prefix))}
		}
	}
	return lines
}

func tablePrefix(prefix string) string {
	var result strings.Builder
	x := 0
	g := uniseg.NewGraphemes(prefix)
	for g.Next() {
		value := g.Str()
		if value == "\t" {
			value = strings.Repeat(" ", 4-x%4)
		} else {
			value = safeText(value)
		}
		result.WriteString(value)
		x += uniseg.StringWidth(value)
	}
	return result.String()
}
