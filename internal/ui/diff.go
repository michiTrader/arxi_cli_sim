package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"arxi.local/sim/internal/state"
)

// The diff view. A tool result can say "Added 10 lines, removed 1 line", and that
// is a fact about the edit; this file draws the edit itself, which is a fact about
// the file. It is the one place in the transcript where the interface stops
// reporting what the agent did and shows it.
//
// Layout, left to right:
//
//	    141  -      locks = append(locks, lk)
//	    ---  -      -----------------------
//	    │    │      the code, syntax coloured
//	    │    the sign: -, + or a blank
//	    the line number, right aligned; new-side except on a removal
//
// The band a changed row is drawn on starts at the number's first cell and runs to
// the right edge, past the end of a short line, because a band that stopped at the
// end of the text would draw the length of every line instead of the shape of the
// change. The margin on the left is left unwashed, so the bands start under the
// summary that introduced them rather than against the frame.
//
// Every span of a changed row carries the band as its Fill and its own colour as
// its Style, and emit composes the two. That is what lets a green comment sit on a
// blue row without punching a hole in it, and it is why nothing in this file has to
// know what colour anything is.

// diffIndent is the left margin. It is also where the elbow's text begins, so a
// hunk lines up under the sentence that introduced it.
const diffIndent = 4

// diffMinCode is the narrowest code column worth drawing.
const diffMinCode = 8

// renderDiff draws every hunk of one diff, or nothing at all when the terminal is
// too narrow to carry a row of code.
//
// Nothing is an honest answer here rather than a failure: the summary above has
// already said what the edit did, so a phone held in portrait keeps the sentence
// and loses the detail, instead of being handed six columns of code and a wrapped
// mess. The numbers go before the code does, for the same reason — which line
// changed is worth less than what it now says.
func renderDiff(d *state.Diff, width int, g Glyphs) []Line {
	if d == nil || len(d.Hunks) == 0 {
		return nil
	}
	numW := diffNumWidth(d)
	codeCol := diffIndent + numW + 3
	if width-codeCol < diffMinCode {
		numW, codeCol = 0, diffIndent+2
		if width-codeCol < diffMinCode {
			return nil
		}
	}
	var out []Line
	for i, h := range d.Hunks {
		if i > 0 {
			out = append(out, diffGap(numW, g))
		}
		for _, r := range h.Rows {
			out = append(out, diffRow(r, d.Lang, numW, codeCol, width)...)
		}
	}
	return out
}

// diffNumWidth is how many columns the largest number in the gutter needs. It is
// measured across the whole diff rather than per hunk, so the code column does not
// step sideways halfway down one edit.
func diffNumWidth(d *state.Diff) int {
	high := 0
	for _, h := range d.Hunks {
		for _, r := range h.Rows {
			if n := r.Num(); n > high {
				high = n
			}
		}
	}
	return len(strconv.Itoa(high))
}

// diffRow draws one row of a hunk as the one or more physical rows it needs.
//
// A row that wraps repeats its sign and blanks its number, because the second
// physical row is the same line of the file: a number there would claim a line that
// does not exist, and a sign that stopped would leave the tail of an insertion
// looking like context.
func diffRow(r state.DiffRow, lang string, numW, codeCol, width int) []Line {
	sign, fill, gutter := " ", "", "diff.gutter"
	switch r.Op {
	case state.DiffAdded:
		sign, fill, gutter = "+", "diff.added", "diff.gutter.added"
	case state.DiffRemoved:
		sign, fill, gutter = "-", "diff.removed", "diff.gutter.removed"
	}
	// Unclassified code on a changed row is left unstyled so the band answers for
	// it; on a context row there is no band, so it gets a colour of its own.
	signStyle, base := "", "diff.context"
	if fill != "" {
		signStyle, base = fill+".sign", ""
	}
	text := strings.TrimRight(expandTabs(r.Text, 4), " ")
	var out []Line
	for i, code := range HardWrapSpans(Highlight(text, lang, base), width-codeCol) {
		row := Line{{Text: strings.Repeat(" ", diffIndent)}}
		if numW > 0 {
			num := strings.Repeat(" ", numW)
			if i == 0 {
				num = padLeft(strconv.Itoa(r.Num()), numW)
			}
			row = append(row, Span{Text: num, Style: gutter, Fill: fill}, Span{Text: " ", Fill: fill})
		}
		row = append(row, Span{Text: sign, Style: signStyle, Fill: fill}, Span{Text: " ", Fill: fill})
		// The wash is stamped onto the highlighter's own spans rather than drawn
		// under them: Style stays the token's colour, Fill carries the row's, and
		// emit merges the two. Appending them untouched leaves a hole in the band
		// wherever the highlighter had an opinion.
		for _, sp := range code {
			sp.Fill = fill
			row = append(row, sp)
		}
		if fill == "" {
			// No band, so the trailing blanks of an unchanged empty line are just
			// blanks, and a line that ends in one of those is a bug elsewhere.
			out = append(out, row.TrimRight())
			continue
		}
		if n := width - row.Width(); n > 0 {
			row = append(row, Span{Text: strings.Repeat(" ", n), Fill: fill})
		}
		out = append(out, row)
	}
	return out
}

// diffGap stands in for the rows between two hunks. It is drawn in the number
// field, where the reader is already looking for a line number, so the marker
// appears exactly where the numbering skips.
func diffGap(numW int, g Glyphs) Line {
	row := Line{{Text: strings.Repeat(" ", diffIndent)}}
	if numW == 0 {
		return append(row, g.Span("diff.gap", "diff.gap"))
	}
	return append(row, Span{Text: padLeft(g.Get("diff.gap"), numW), Style: "diff.gap"})
}

// padLeft right-aligns s in w columns, and cuts it rather than overflow when it
// does not fit: a gutter is never allowed to push a row past the frame's edge.
func padLeft(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return ansi.Truncate(s, w, "")
	}
	return strings.Repeat(" ", w-sw) + s
}
