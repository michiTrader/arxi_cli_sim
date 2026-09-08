package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The two fixtures are the two tables the corpus already has, because the point of a
// unit test here is the arithmetic and the point of the corpus sweep is the whole
// interface: this file can ask what happens at three columns, which no terminal has.

// sampleTable is scenario 06's table with the alignment 06 does not ask for. A label
// column, a prose column wide enough to wrap and a count column pinned to its right
// edge — the shape the grid exists to draw.
const sampleTable = "| file | change | lines |\n" +
	"| --- | --- | ---: |\n" +
	"| runshow.go | ttl is omitted when there is no expiry | +9 -2 |\n" +
	"| runshow_test.go | covers the empty case | +8 |\n"

// hostileTable is scenario 02's: three columns where one holds a sentence, cells with
// code spans in them, and nothing that wraps kindly.
const hostileTable = "| what | how wide | bounded by |\n" +
	"| --- | --- | --- |\n" +
	"| `tool.args` | unbounded | nothing: a bash command can be a whole script |\n" +
	"| `tool.result.summary` | unbounded | `max_output_bytes`, which is a byte " +
	"count and not a column count |\n" +
	"| a base64 payload | 312 columns | the model's patience |\n"

func mdLines(md string, width int) []Line {
	return RenderMarkdown(md, width, DefaultGlyphs())
}

func show(lines []Line) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text()
	}
	return strings.Join(out, "\n")
}

// seps is where a line's column separators sit, in display columns rather than bytes:
// "the columns line up" is a claim about what the terminal shows, and a CJK cell makes
// a byte offset mean nothing.
func seps(s string, glyph rune) []int {
	var out []int
	for i, r := range s {
		if r == glyph {
			out = append(out, ansi.StringWidth(s[:i]))
		}
	}
	return out
}

// TestTableDrawsAGrid is the geometry: a rule under the header, and every vertical in
// the body standing under a vertical in the header. A table whose separators wander is
// worse than no table at all, because the eye follows the column and finds the wrong
// row.
func TestTableDrawsAGrid(t *testing.T) {
	lines := mdLines(sampleTable, 72)
	if len(lines) != 4 {
		t.Fatalf("a header, a rule and two rows should be four lines, got %d:\n%s", len(lines), show(lines))
	}
	rule := lines[1].Text()
	if strings.Trim(rule, "─┼") != "" {
		t.Errorf("the line under a header should be the rule, got %q", rule)
	}
	want := seps(lines[0].Text(), '│')
	if len(want) != 2 {
		t.Fatalf("three columns want two verticals, got %v in %q", want, lines[0].Text())
	}
	if got := seps(rule, '┼'); !slices.Equal(got, want) {
		t.Errorf("the rule crosses at %v, the header separates at %v:\n%s", got, want, show(lines))
	}
	for i, l := range lines[2:] {
		if got := seps(l.Text(), '│'); !slices.Equal(got, want) {
			t.Errorf("row %d separates at %v, the header at %v:\n%s", i, got, want, show(lines))
		}
	}
}

// TestTableAlignsOnTheRequestedEdge is what the colons in a delimiter row buy. The last
// column of sampleTable is ---:, so "+9 -2" and "+8" end at the same column and the row
// widths agree; left-aligned, the shorter count would be trimmed and its row would come
// out three columns shorter than the header's.
func TestTableAlignsOnTheRequestedEdge(t *testing.T) {
	lines := mdLines(sampleTable, 72)
	w := lines[0].Width()
	for i, l := range lines {
		if l.Width() != w {
			t.Errorf("line %d is %d columns wide, the header %d:\n%s", i, l.Width(), w, show(lines))
		}
	}
	for _, want := range []string{"+9 -2", "+8"} {
		found := false
		for _, l := range lines {
			found = found || strings.HasSuffix(l.Text(), want)
		}
		if !found {
			t.Errorf("%q should end its row, since its column asked for the right edge:\n%s", want, show(lines))
		}
	}
}

// TestTableFallsBackToRecords is the narrow window. Twenty columns cannot hold three
// cells and the separators between them, so the grid is dropped rather than squeezed: no
// verticals, no rule, and every row a small labelled record instead.
func TestTableFallsBackToRecords(t *testing.T) {
	lines := mdLines(sampleTable, 20)
	text := show(lines)
	if strings.ContainsAny(text, "│┼─") {
		t.Errorf("at twenty columns the grid should be gone, got:\n%s", text)
	}
	for i, l := range lines {
		if l.Width() > 20 {
			t.Errorf("record line %d is %d columns wide in a 20-column frame: %q", i, l.Width(), l.Text())
		}
	}
	for _, want := range []string{"file: runshow.go", "lines: +8"} {
		if !strings.Contains(text, want) {
			t.Errorf("a record should read %q, got:\n%s", want, text)
		}
	}
	// A blank row between records, and labels that keep the header's style: without the
	// first the records read as one long list, and without the second a value no longer
	// says which column it came from.
	blank, labelled := false, false
	for _, l := range lines {
		blank = blank || l.Width() == 0
		for _, sp := range l {
			labelled = labelled || sp.Style == "md.table.header"
		}
	}
	if !blank {
		t.Errorf("records should be blank-separated, got:\n%s", text)
	}
	if !labelled {
		t.Errorf("the labels should carry md.table.header, got:\n%s", text)
	}
}

// TestTableNeverOverflowsAnyWidth is the property the rest of the frame rests on. A line
// wider than the terminal wraps where the renderer did not choose to, and every row
// counted after it is off by one; a line ending in a space fills the last cell, which is
// how a terminal is talked into wrapping a row we thought we owned. Both fixtures at
// every width from one column up, so both forms and the crossover between them are swept.
func TestTableNeverOverflowsAnyWidth(t *testing.T) {
	for _, md := range []string{sampleTable, hostileTable} {
		for w := 1; w <= 100; w++ {
			for i, l := range mdLines(md, w) {
				if l.Width() > w {
					t.Fatalf("width %d: line %d is %d columns: %q", w, i, l.Width(), l.Text())
				}
				if s := l.Text(); strings.HasSuffix(s, " ") {
					t.Fatalf("width %d: line %d ends in a space: %q", w, i, s)
				}
			}
		}
	}
}

// TestTableNeedsItsDelimiterRow keeps prose out of the grid, which is what the delimiter
// requirement is for: markdown's own rule, and the only thing standing between a sentence
// with a pipe in it and a table. Two rows of cells are still not a table without it, and a
// header whose delimiter has not arrived yet draws as text — the same honest flicker an
// unclosed fence shows.
func TestTableNeedsItsDelimiterRow(t *testing.T) {
	rows := "| file | change |\n| runshow.go | ok |\n"
	got := show(mdLines(rows, 72))
	if strings.ContainsAny(got, "│┼") {
		t.Errorf("two rows without a delimiter are not a table, got:\n%s", got)
	}
	for _, want := range []string{"| file | change |", "| runshow.go | ok |"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q should have stayed text, got:\n%s", want, got)
		}
	}
	prose := show(mdLines("run a | b in bash\n| --- |\n", 72))
	if strings.ContainsAny(prose, "│┼") {
		t.Errorf("a sentence with a pipe in it is not a table, got %q", prose)
	}
}

// TestTableCellsKeepTheirInlineStyles: a cell is markdown too. `tool.args` in a column is
// the same code span it would be in a sentence, and its backticks are markers rather than
// text — a table that prints them has pasted the source in instead of rendering it. The
// widths are measured after the markers are gone, so this is also what keeps a column of
// code spans from being two columns too wide.
func TestTableCellsKeepTheirInlineStyles(t *testing.T) {
	lines := mdLines(hostileTable, 72)
	text := show(lines)
	if strings.Contains(text, "`") {
		t.Errorf("the backticks are markers, not cell text:\n%s", text)
	}
	found := false
	for _, l := range lines {
		for _, sp := range l {
			found = found || (sp.Style == "md.code" && strings.Contains(sp.Text, "tool.args"))
		}
	}
	if !found {
		t.Errorf("tool.args should be a code span inside its cell:\n%s", text)
	}
}

// TestTableStopsWhereTheTableStops is about the loop rather than the table: RenderMarkdown
// has to jump exactly the rows renderTable consumed. One row too far swallows the sentence
// after the table; one too few draws the last body row twice, once in the grid and once as
// prose. Both counts are asserted because either mutation passes the other's check.
func TestTableStopsWhereTheTableStops(t *testing.T) {
	text := show(mdLines(sampleTable+"and a sentence after it\n", 72))
	for _, want := range []string{"and a sentence after it", "runshow_test.go"} {
		if n := strings.Count(text, want); n != 1 {
			t.Errorf("%q appears %d times, want once:\n%s", want, n, text)
		}
	}
}

// TestTableKeepsAnEscapedPipe: a table of shell snippets is exactly where somebody needs a
// pipe inside a cell, and \| is how markdown asks for one. Splitting on it instead would
// silently turn a two-column row into three and push everything one column right.
func TestTableKeepsAnEscapedPipe(t *testing.T) {
	md := "| cmd | what |\n| --- | --- |\n" +
		`| go test ./... \| head | the first failures |` + "\n"
	text := show(mdLines(md, 72))
	if !strings.Contains(text, "go test ./... | head") {
		t.Errorf("an escaped pipe belongs to its cell:\n%s", text)
	}
}

// TestTableSeparatorsRunTheHeightOfTheRow is the wrapped row, which the width the other
// grid tests use never reaches: at forty-five columns the prose column takes two lines,
// and the verticals have to stand on both of them. A separator that stops where its cell
// runs out leaves the bottom half of a row reading as prose that wandered into the table,
// and the cells either side of the gap reading as one.
func TestTableSeparatorsRunTheHeightOfTheRow(t *testing.T) {
	lines := mdLines(sampleTable, 45)
	if len(lines) <= 4 {
		t.Fatalf("forty-five columns should wrap a row into more than four lines, got %d:\n%s", len(lines), show(lines))
	}
	want := seps(lines[0].Text(), '│')
	if len(want) != 2 {
		t.Fatalf("three columns want two verticals, got %v in %q", want, lines[0].Text())
	}
	for i, l := range lines[2:] {
		if got := seps(l.Text(), '│'); !slices.Equal(got, want) {
			t.Errorf("body line %d separates at %v, the header at %v:\n%s", i, got, want, show(lines))
		}
	}
}

// TestTableNeverCutsAWordInHalf is the rule that chooses between the two forms. WrapSpans
// cuts a word that cannot fit rather than overflowing, so a column one letter short of a
// file name draws "runshow_test.g" over "o", which reads as two files: the grid has to be
// given up before it comes to that. Swept from the narrowest frame that can still hold the
// longest cell whole — below that the cut is arithmetic and not a choice.
func TestTableNeverCutsAWordInHalf(t *testing.T) {
	for w := 17; w <= 100; w++ {
		if text := show(mdLines(sampleTable, w)); !strings.Contains(text, "runshow_test.go") {
			t.Errorf("width %d cut a file name in half:\n%s", w, text)
		}
	}
}
