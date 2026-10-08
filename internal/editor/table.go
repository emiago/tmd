package editor

import (
	"strings"

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
		cells = append(cells, projectedCell{value, pos, end, f})
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
	widths := make([]int, columns)
	type row struct {
		segment text.Segment
		cells   [][]projectedCell
		widths  []int
		anchors []int
	}
	var rows []row
	i := 0
	for node := table.FirstChild(); node != nil; node = node.NextSibling() {
		if i == 1 {
			i++ // The delimiter is not an AST row.
		}
		seg := segments[i]
		r := row{seg, make([][]projectedCell, columns), make([]int, columns), make([]int, columns)}
		line := p.lines[lineAt(p.lines, seg.Start)]
		column := 0
		for n := node.FirstChild(); n != nil; n = n.NextSibling() {
			cell := n.(*extast.TableCell)
			anchor := line.end
			if cell.Lines().Len() > 0 {
				anchor = cell.Lines().At(0).Start
			}
			r.anchors[column] = anchor
			r.cells[column], r.widths[column] = p.tableCell(source, cell, i == 0)
			widths[column] = max(widths[column], r.widths[column], 1)
			column++
		}
		rows = append(rows, r)
		i++
	}
	for _, r := range rows {
		lineIndex := lineAt(p.lines, r.segment.Start)
		line := p.lines[lineIndex]
		var cells []projectedCell
		add := func(value string, anchor int) {
			cells = append(cells, projectedCell{value, anchor, anchor, 0})
		}
		// Preserve container markers and indentation outside the table.
		add(source[line.start:r.segment.Start], line.start)
		add("│", r.anchors[0])
		for column, content := range r.cells {
			padding := widths[column] - r.widths[column]
			left := 0
			switch table.Alignments[column] {
			case extast.AlignRight:
				left = padding
			case extast.AlignCenter:
				left = padding / 2
			}
			add(strings.Repeat(" ", left+1), r.anchors[column])
			cells = append(cells, content...)
			end := r.anchors[column]
			if len(content) > 0 {
				end = content[len(content)-1].end
			}
			add(strings.Repeat(" ", padding-left+1)+"│", end)
		}
		p.tableLines[lineIndex] = cells
	}
	seg := segments[1]
	lineIndex := lineAt(p.lines, seg.Start)
	line := p.lines[lineIndex]
	var rule strings.Builder
	rule.WriteString(source[line.start:seg.Start])
	rule.WriteString("├")
	for column, width := range widths {
		if column > 0 {
			rule.WriteString("┼")
		}
		rule.WriteString(strings.Repeat("─", width+2))
	}
	rule.WriteString("┤")
	p.tableLines[lineIndex] = []projectedCell{{rule.String(), seg.Start, line.end, 0}}
}
