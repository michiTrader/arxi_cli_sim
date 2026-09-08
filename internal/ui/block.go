package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"arxi.local/sim/internal/state"
)

// A Block is one entry in the transcript. Blocks are append-only and become
// sealed: once a block is sealed its lines can never change, which is the promise
// inline mode depends on, because a sealed block has already been handed to the
// terminal's scrollback and nothing but a screen-clearing sequence could take it
// back. Only the last block may still be open.
//
// Render does not receive a theme. A block emits style *names*, so a theme change
// repaints without re-rendering and a golden file stays readable.
type Block interface {
	ID() string
	Sealed() bool
	Render(width int, g Glyphs) []Line
}

// ItemBlock renders one state.Item. One type with a switch rather than one type
// per kind: the kinds share all their layout decisions (marker, wrap, hanging
// indent) and differ only in which style keys they name.
type ItemBlock struct{ It state.Item }

// BlockFor wraps an item.
func BlockFor(it state.Item) Block { return ItemBlock{It: it} }

func (b ItemBlock) ID() string   { return b.It.ID }
func (b ItemBlock) Sealed() bool { return !b.It.Open }

// Render lays the item out in width columns.
func (b ItemBlock) Render(width int, g Glyphs) []Line {
	if width <= 0 {
		return nil
	}
	switch b.It.Kind {
	case state.KindPrompt:
		return banded(g.Span("prompt.marker", "prompt.marker"), b.It.Text, "prompt.text", "prompt.band", width)
	case state.KindThinking:
		if b.It.Open {
			return marked(g.Span("thinking.marker", "thinking.marker"), b.It.Text, "thinking.text", width)
		}
		return marked(g.Span("thinking.marker", "thinking.marker"), b.thoughtSummary(), "thinking.summary", width)
	case state.KindText:
		return RenderMarkdown(b.It.Text, width, g)
	case state.KindTool:
		return b.renderTool(width, g)
	case state.KindNotice:
		style := "notice.text"
		switch b.It.Notice {
		case state.NoticeBudget, state.NoticeBlocked, state.NoticeConflict:
			style = "notice.warn"
		}
		return marked(g.Span("notice.marker", "notice.marker"), b.It.Text, style, width)
	}
	return nil
}

// banded is marked with a background behind it. Every span of every row carries the band
// as its Fill and its own colour as its Style, and emit composes the two — the diff's
// mechanism, spent on the one block in the transcript the reader wrote themselves.
//
// The band runs to the right edge past the end of a short line, which is the whole point:
// a wash that stopped at the last word would draw the length of a sentence, and what the
// reader is scanning a long scroll for is a rectangle. That means banded rows are the one
// place trailing spaces are content rather than a bug, so join is not used here — its
// TrimRight would eat the band — and the padding is added before the Fill is stamped on so
// that it is stamped on the pad too. A theme that empties prompt.band gets the rows marked
// would have drawn, plus spaces the emitter has no colour to send for.
func banded(head Span, text, style, band string, width int) []Line {
	prefix := Line{head}
	w := prefix.Width()
	if w >= width {
		return nil
	}
	cont := Line{{Text: strings.Repeat(" ", w)}}
	var out []Line
	for i, ln := range WrapSpans(inlineSpans(text, style), width-w, nil) {
		p := prefix
		if i > 0 {
			p = cont
		}
		row := append(append(Line{}, p...), ln...)
		if n := width - row.Width(); n > 0 {
			row = append(row, Span{Text: strings.Repeat(" ", n)})
		}
		for j := range row {
			row[j].Fill = band
		}
		out = append(out, row)
	}
	return out
}

// marked draws a glyph and hangs the wrapped text under it.
func marked(head Span, text, style string, width int) []Line {
	prefix := Line{head}
	w := prefix.Width()
	if w >= width {
		return nil
	}
	cont := Line{{Text: strings.Repeat(" ", w)}}
	var out []Line
	for i, ln := range WrapSpans(inlineSpans(text, style), width-w, nil) {
		p := prefix
		if i > 0 {
			p = cont
		}
		out = append(out, join(p, ln))
	}
	return out
}

// join glues a prefix to a wrapped line. The trim is what keeps a marker with no
// text yet — an open reasoning block, a tool call still being announced — from
// leaving a blank in the last cell it occupies.
func join(prefix, tail Line) Line {
	return append(append(Line{}, prefix...), tail...).TrimRight()
}

// thoughtSummary is what replaces a finished reasoning block. The interesting
// number is how long the model spent, not what it said; the text stays in state
// so ctrl+o can still show it.
func (b ItemBlock) thoughtSummary() string {
	d := b.It.EndedAt - b.It.StartedAt
	s := "Thought for " + fmtDuration(d)
	if b.It.Effort != "" {
		s += " (" + b.It.Effort + " effort)"
	}
	return s
}

// renderTool draws the call and, once it has one, its result:
//
//	● Read(internal/auth/session.go)
//	  ⎿ read 214 lines
//
// The marker carries the status, which is why there are four marker keys and one
// name key: colour is the only thing that distinguishes a call that is running
// from one that failed, and it belongs on the smallest glyph on the line.
func (b ItemBlock) renderTool(width int, g Glyphs) []Line {
	marker := "tool.marker.pending"
	switch b.It.Status {
	case state.ToolOK:
		marker = "tool.marker.ok"
	case state.ToolFailed:
		marker = "tool.marker.error"
	case state.ToolDenied:
		marker = "tool.marker.denied"
	}
	head := Line{g.Span("tool.marker", marker)}
	w := head.Width()
	if w >= width {
		return nil
	}
	call := []Span{
		{Text: displayTool(b.It.Tool), Style: "tool.name"},
		{Text: "(" + formatArgs(b.It.Args) + ")", Style: "tool.args"},
	}
	cont := Line{{Text: strings.Repeat(" ", w)}}
	var out []Line
	for i, ln := range WrapSpans(call, width-w, nil) {
		p := head
		if i > 0 {
			p = cont
		}
		out = append(out, join(p, ln))
	}
	if b.It.Summary == "" && b.It.Diff == nil {
		return out
	}
	out = append(out, b.resultLines(width, w, g)...)
	// The diff is assembled by renderDiff and appended untouched: join trims
	// trailing spaces, which is right for a sentence and wrong for a row whose
	// coloured band is made of them.
	return append(out, renderDiff(b.It.Diff, width, g)...)
}

// A long result is elided rather than drawn whole: resultHead lines from its top,
// resultTail from its bottom, and one line between them saying how many were dropped.
//
// The two ends get different budgets because they do not carry the same thing. A
// command announces itself at the top — the target, the module, how many tests it
// found — and reports at the bottom, where the failure and the summary line are, so
// the tail is worth more rows than the head and is given them.
//
// The cut is measured in the result's own lines and not in the rows they wrap to: a
// count the reader can check by running the command themselves is worth more than one
// that changes with the width of their window. It is not measured in bytes either.
// Bytes are what max_output_bytes caps upstream, which is a fact about the runtime's
// budget rather than about what a screen can show.
const (
	resultHead = 3
	resultTail = 8
)

// resultLines draws the elbow and the summary under a finished call, keeping the ends
// of a result too long for the transcript to spend rows on:
//
//	⎿ go test ./... -count=1
//	  ok    arxi.local/sim/internal/event    0.004s
//	  ok    arxi.local/sim/internal/scenario 0.011s
//	  … 19 more lines
//	  --- FAIL: TestGapIsDrawnBetweenHunksOnly (0.00s)
func (b ItemBlock) resultLines(width, indent int, g Glyphs) []Line {
	if b.It.Summary == "" {
		return nil
	}
	style := "tool.result.text"
	if b.It.Status == state.ToolFailed || b.It.Status == state.ToolDenied {
		style = "tool.result.error"
	}
	elbow := Line{{Text: strings.Repeat(" ", indent)}, g.Span("tool.result", "tool.result.marker")}
	ew := elbow.Width()
	if ew >= width {
		return nil
	}
	pad := Line{{Text: strings.Repeat(" ", ew)}}
	var out []Line
	// The elbow introduces the result and not each piece of it, so it goes on the first
	// row out and the pad on every row after it, whichever piece that row came from.
	// Everything goes through the wrapper, the gap line included, so no width can overflow.
	//
	// One line of the result at a time, and not the piece whole: the hanging indent below
	// belongs to the line rather than to the piece, and a `go test` failure is the case —
	// its `--- FAIL:` header is flush left and the subtest detail under it is not. The
	// trailing newline goes first because that is what tokenize does with one, and
	// splitting a string that ends in a break would otherwise invent a blank row.
	add := func(text, key string) {
		for _, src := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			for _, ln := range wrapHanging(src, key, width-ew) {
				p := pad
				if len(out) == 0 {
					p = elbow
				}
				out = append(out, join(p, ln))
			}
		}
	}
	lines := strings.Split(b.It.Summary, "\n")
	// The threshold is head+tail+1 and not head+tail because eliding one line spends a
	// row to save a row. Above it the count is always at least two, which is why nothing
	// below writes a singular: an unreachable branch is a lie a test cannot catch.
	if len(lines) <= resultHead+resultTail+1 {
		add(b.It.Summary, style)
		return out
	}
	add(strings.Join(lines[:resultHead], "\n"), style)
	add(g.Get("tool.result.gap")+" "+strconv.Itoa(len(lines)-resultHead-resultTail)+" more lines", "tool.result.gap")
	add(strings.Join(lines[len(lines)-resultTail:], "\n"), style)
	return out
}

// wrapHanging wraps one line of a result and hangs every row after the first under
// that line's own indentation, so a line too long for the pane still reads as one
// line. Without it a continuation restarts at the result's left edge, which is where
// a `--- FAIL:` header sits, and the tail of an indented subtest detail is drawn at
// the depth of a heading — the reader is told about a failure that does not exist and
// has to reconstruct the sentence to find out. It is the hanging indent listItem
// already gives a wrapped bullet, one layer down.
//
// Two lines are left to wrap themselves, and each for a reason the wrapper already
// has. A line with no indent has nothing to hang under. A line that is nothing but
// indentation would hang a row of blanks under itself, and trailing blanks are what
// every row here is trimmed of.
//
// The third is the one worth stating: an indent with no room for the first word beside
// it is not an indent. That is what WrapSpans says about the same spaces and why
// listItem's marker gives way to its item — and kept here it would be worse than
// dropped, because the body would then wrap into whatever columns were left and a
// detail indented eight columns in a thirteen-column pane would come out one character
// to a row.
//
// The tabs are expanded before the indent is measured, because that is what the
// wrapper will do with them; counted raw, a tab-indented line would hang three
// columns short of where it starts.
func wrapHanging(src, style string, room int) []Line {
	src = stripControls(src)
	body := strings.TrimLeft(src, " ")
	indent := len(src) - len(body)
	word := body
	if i := strings.IndexByte(word, ' '); i >= 0 {
		word = word[:i]
	}
	if indent == 0 || body == "" || indent+ansi.StringWidth(word) > room {
		return WrapText(src, style, room, nil)
	}
	hang := Line{{Text: strings.Repeat(" ", indent)}}
	rows := WrapText(body, style, room-indent, nil)
	for i := range rows {
		rows[i] = join(hang, rows[i])
	}
	return rows
}

// displayTool is the name as a human reads it: the log says "read", the transcript
// says "Read".
func displayTool(name string) string {
	if name == "" {
		return "tool"
	}
	r := []rune(name)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// formatArgs prints a call's arguments. A single argument is printed bare, which
// is what makes `Read(internal/auth/session.go)` read like a sentence; several are
// printed as sorted key=value pairs, sorted because a map's order is not stable
// and a frame that changes without the state changing is a frame we cannot test.
func formatArgs(args map[string]any) string {
	switch len(args) {
	case 0:
		return ""
	case 1:
		for _, v := range args {
			return formatValue(v)
		}
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+formatValue(args[k]))
	}
	return strings.Join(parts, ", ")
}

func formatValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// fmtDuration is short on purpose: a transcript is read at a glance, and "0.4s"
// carries every bit of information "412.318ms" does.
func fmtDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
	case d < time.Minute:
		return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
	default:
		m := int(d / time.Minute)
		s := int((d % time.Minute) / time.Second)
		return strconv.Itoa(m) + "m" + strconv.Itoa(s) + "s"
	}
}
