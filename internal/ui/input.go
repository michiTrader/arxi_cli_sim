package ui

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// The line editor, written from zero. It has to be: the input is the part of the
// interface the user touches most, and every ready-made text field either drags in
// a framework or hard-codes a keymap. This one keeps runes and an index, exposes
// every edit as a named method, and reports where the cursor ended up so the real
// terminal cursor can be put there — a fake cursor drawn as a reversed cell is the
// single most obvious tell that a TUI is not a native prompt.

// Input is a one-field editor over a rune slice.
type Input struct {
	runes  []rune
	cursor int // rune index, 0..len(runes)

	Placeholder string
	Prompt      string // glyph key for the marker; empty means input.marker
	// Title is written into the top border. It is a word about what the box is for
	// rather than what is in it — a title that tracked the contents would change width
	// as the human types, and a border that reflows under the cursor is the one thing a
	// text field must never do. Empty draws an unbroken rule, and that is the default:
	// the marker inside the box already says a prompt goes here, so "prompt" spent four
	// columns of border and a reader's attention repeating it. A caller with something
	// worth putting there — a mode, a branch, a model — sets it.
	Title   string
	history []string
	histIdx int // len(history) means "editing a fresh line"
	stash   string

	HistoryLabel bool

	// Shine is the band of light that crosses the box, and its zero value is no light at
	// all — so an editor nobody arms draws exactly the rows it drew before this field
	// existed. The caller owns both the switch and the clock: it decides when a shine is
	// appropriate (here, while the turn is the human's and the line is still empty) and
	// hands in the phase, because the editor has no idea what the program is doing and no
	// timer of its own to find out with.
	Shine Shimmer
}

// Animated reports whether the next frame of this editor differs from this one, which is
// true exactly while a shine is armed — the same question a Widget answers, asked of the
// one drawable in this package that is not one. The app ORs it in beside the widgets, so
// arming a shine is all it takes to get the ticks that move it.
func (in *Input) Animated() bool { return in.Shine.On() }

// NewInput returns an empty editor.
func NewInput() *Input {
	return &Input{Placeholder: "ask anything, or / for commands"}
}

// Text is what the human has typed.
func (in *Input) Text() string { return string(in.runes) }

// SetText replaces the line and puts the cursor at the end.
func (in *Input) SetText(s string) {
	in.runes = []rune(s)
	in.cursor = len(in.runes)
}

// CursorColumn reports the display column of the cursor in a single-line view of the
// editor. Config's scalar field uses it without borrowing the conversation input's marker,
// border, placeholder, or wrapping behavior.
func (in *Input) CursorColumn() int {
	return ansi.StringWidth(string(in.runes[:in.cursor]))
}

// Empty reports whether there is nothing to submit.
func (in *Input) Empty() bool { return strings.TrimSpace(string(in.runes)) == "" }

// Insert types text at the cursor. It takes a string rather than a rune so a
// bracketed paste is one edit and one undo step.
func (in *Input) Insert(s string) {
	r := []rune(s)
	in.runes = append(in.runes[:in.cursor], append(r, in.runes[in.cursor:]...)...)
	in.cursor += len(r)
}

// Backspace deletes the rune before the cursor.
func (in *Input) Backspace() {
	if in.cursor == 0 {
		return
	}
	in.runes = append(in.runes[:in.cursor-1], in.runes[in.cursor:]...)
	in.cursor--
}

// Delete deletes the rune under the cursor.
func (in *Input) Delete() {
	if in.cursor >= len(in.runes) {
		return
	}
	in.runes = append(in.runes[:in.cursor], in.runes[in.cursor+1:]...)
}

// Left, Right, Home and End move the cursor.
func (in *Input) Left() {
	if in.cursor > 0 {
		in.cursor--
	}
}

func (in *Input) Right() {
	if in.cursor < len(in.runes) {
		in.cursor++
	}
}

func (in *Input) Home() { in.cursor = 0 }
func (in *Input) End()  { in.cursor = len(in.runes) }

// WordLeft and WordRight move by word, skipping the run of spaces first so that
// pressing it twice from the end of "go test ./..." lands where a shell would.
func (in *Input) WordLeft() {
	for in.cursor > 0 && unicode.IsSpace(in.runes[in.cursor-1]) {
		in.cursor--
	}
	for in.cursor > 0 && !unicode.IsSpace(in.runes[in.cursor-1]) {
		in.cursor--
	}
}

func (in *Input) WordRight() {
	for in.cursor < len(in.runes) && unicode.IsSpace(in.runes[in.cursor]) {
		in.cursor++
	}
	for in.cursor < len(in.runes) && !unicode.IsSpace(in.runes[in.cursor]) {
		in.cursor++
	}
}

// KillWord, KillToEnd and KillLine are the readline verbs a terminal user already
// has in their fingers: ctrl+w, ctrl+k, ctrl+u.
func (in *Input) KillWord() {
	end := in.cursor
	in.WordLeft()
	in.runes = append(in.runes[:in.cursor], in.runes[end:]...)
}

func (in *Input) KillToEnd() { in.runes = in.runes[:in.cursor] }

func (in *Input) KillLine() {
	in.runes = in.runes[:0]
	in.cursor = 0
	in.HistoryLabel = false
}

// Submit clears the line and returns it, recording it in the history. An empty
// line returns false so the caller does not have to check twice.
func (in *Input) Submit() (string, bool) {
	if in.Empty() {
		return "", false
	}
	s := string(in.runes)
	if n := len(in.history); n == 0 || in.history[n-1] != s {
		in.history = append(in.history, s)
	}
	in.histIdx = len(in.history)
	in.stash = ""
	in.HistoryLabel = false
	in.KillLine()
	return s, true
}

// Older and Newer walk the history. The line being edited is stashed on the way
// out and restored on the way back, because losing a half-typed prompt to an
// accidental arrow key is unforgivable.
func (in *Input) Older() {
	if in.histIdx == 0 || len(in.history) == 0 {
		return
	}
	if in.histIdx == len(in.history) {
		in.stash = string(in.runes)
	}
	in.histIdx--
	in.SetText(in.history[in.histIdx])
	in.HistoryLabel = true
}

func (in *Input) Newer() {
	if in.histIdx >= len(in.history) {
		return
	}
	in.histIdx++
	if in.histIdx == len(in.history) {
		in.SetText(in.stash)
		in.HistoryLabel = false
		return
	}
	in.SetText(in.history[in.histIdx])
	in.HistoryLabel = true
}

// Render lays the input out in width columns and returns where the cursor landed,
// relative to the first line it produced.
//
// The box is the reason this is the only widget-shaped thing that is not a Widget: it
// has to be drawn around the text and the cursor has to come back out, and a Widget
// returns lines and nothing else. Everything else about it is ordinary — two rules, a
// vertical on each side of every row, and one column of air so the first letter is not
// welded to the border.
func (in *Input) Render(width int, g Glyphs) ([]Line, Cursor) {
	key := in.Prompt
	if key == "" {
		key = "input.marker"
	}
	title := in.Title
	if in.HistoryLabel && len(in.history) > 0 {
		title = "History " + strconv.Itoa(in.histIdx+1) + "/" + strconv.Itoa(len(in.history))
	}
	head := Line{g.Span(key, "input.marker")}
	hw := head.Width()
	if width <= hw {
		return nil, Cursor{Hidden: true}
	}
	blank := Line{pad(hw)}

	// The border costs a vertical and a space on each side, and it is drawn only while two
	// columns of text survive it. Two and not one because a wide grapheme is two cells and
	// HardWrap cuts rather than splits: a one-column interior cannot draw a single Japanese
	// character, so it draws none of the line at all and the human types into a box that
	// stays empty. Below the threshold the input degrades to the bare marker row it was
	// before the box existed, where that same text has the border's four columns to wrap
	// into — which is the honest answer for a 20-column terminal anyway: a frame whose
	// inside cannot hold a character is decoration, and the thing being decorated is the
	// only thing the human is looking at.
	v := g.Span("frame.v", "input.frame")
	vw := ansi.StringWidth(v.Text)
	box := 2*vw + 2
	boxed := width-hw-box >= 2
	room := width - hw
	inner := width - 2*vw
	if boxed {
		room -= box
	}

	var body []Line
	var cur Cursor
	if len(in.runes) == 0 {
		body = []Line{append(append(Line{}, head...), Span{Text: truncate(in.Placeholder, room), Style: "input.placeholder"})}
		cur = Cursor{Line: 0, Col: hw}
	} else {
		// One row per line the human made, and then however many more each of those wraps
		// into. The split is here rather than inside HardWrap because HardWrap's other
		// callers are code and preformatted output, where whoever framed the text has
		// already decided what a line is; this is the one place a break arrives inside the
		// text and means a row. An empty segment — a blank line, or a break at the very end
		// — comes back from HardWrap as a single empty Line, which is exactly the empty row
		// that break asked for.
		//
		// Wrapping is hard rather than word-aware on purpose: a cursor that lands one
		// column away from the character it is on is worse than an ugly break.
		var rows []Line
		for _, seg := range strings.Split(string(in.runes), "\n") {
			rows = append(rows, HardWrap(seg, "input.text", room, nil)...)
		}
		body = make([]Line, 0, len(rows))
		for i, r := range rows {
			p := head
			if i > 0 {
				p = blank
			}
			body = append(body, append(append(Line{}, p...), r...))
		}
		cur = in.cursorAt(room)
		cur.Col += hw
	}
	if !boxed {
		// The bare marker row spends every column it has on text, so the column just after
		// the last character of a full row is off the right edge — and a cursor on a break
		// is the one thing that reaches it. A terminal asked for a column it does not have
		// puts the cursor wherever it likes, usually the first cell of the row below, so it
		// is clamped to the last cell that exists instead: one column short of the truth, in
		// the mode that is already one border short of the interface.
		if cur.Col >= width {
			cur.Col = width - 1
		}
		return in.shine(body, width), cur
	}

	// A cursor one row past the last row of text is where the next character will go, and
	// HardWrap has no row for it: it lays out text, and there is none there yet. Unboxed
	// that row is the blank the renderer pads the frame with, which is already right;
	// inside a box it would be the bottom border, so the box grows a row rather than put
	// the cursor on its own frame.
	for cur.Line >= len(body) {
		body = append(body, append(Line{}, blank...))
	}

	out := make([]Line, 0, len(body)+2)
	out = append(out, rule("frame.tl", "frame.tr", title, width, g))
	for _, l := range body {
		// The padding is never zero — a row is at most room columns and room is two short
		// of inner — so the last cell of every line is the border glyph and never a space,
		// which is what Frame.Overflow and the no-trailing-blank rule both need.
		l = append(append(Line{v, pad(1)}, l...), pad(inner-1-l.Width()), v)
		out = append(out, l)
	}
	out = append(out, rule("frame.bl", "frame.br", "", width, g))
	cur.Line++
	cur.Col += vw + 1
	return in.shine(out, width), cur
}

// shine lights whichever columns of these rows the band is over this tick, in place, so the
// box and the bare marker row can share one call and one width.
//
// It runs last, on the finished rows, and that is deliberate: the light crosses the top
// rule, both verticals, the text and the bottom rule together, which is what makes it read
// as a reflection moving over a panel of glass rather than as a word changing colour. It is
// also what makes it visible while the line is empty — the case it exists for, where the
// only text in the box is a placeholder nobody has replaced yet.
//
// Because Shimmer.Apply only ever divides spans, the rows still measure what they measured
// and still end on the border glyph, so the padding invariant above and Frame.Overflow are
// both untouched. Nothing here can change the cursor, which is why it is answered before.
func (in *Input) shine(rows []Line, width int) []Line {
	if !in.Shine.On() {
		return rows
	}
	for i, l := range rows {
		rows[i] = in.Shine.Apply(l, width)
	}
	return rows
}

// rule draws one horizontal of the box: a corner, a run of horizontals to the far
// corner, and optionally a title let into the run one horizontal in. A title that will
// not fit with a horizontal on each side of it is dropped rather than truncated, because
// half a word in a border reads as a rendering fault, and the box says what it is for
// perfectly well by being a box.
func rule(left, right, title string, width int, g Glyphs) Line {
	l, r := g.Span(left, "input.frame"), g.Span(right, "input.frame")
	mid := width - ansi.StringWidth(l.Text) - ansi.StringWidth(r.Text)
	if mid < 0 {
		return Line{}
	}
	out := Line{l}
	// The lead-in is one whole horizontal — one column for the default glyph, two for a
	// double-width one — and the fit test asks for that much on each side of the title. A
	// single column would be spent on a space hrun could not draw the glyph in, which is a
	// title floating between two gaps rather than a word let into a rule.
	lead := max(1, ansi.StringWidth(g.Get("frame.h")))
	if t := " " + title + " "; title != "" && ansi.StringWidth(t)+2*lead <= mid {
		out = append(out, hrun(lead, g), Span{Text: t, Style: "input.title"})
		mid -= lead + ansi.StringWidth(t)
	}
	if mid > 0 {
		out = append(out, hrun(mid, g))
	}
	return append(out, r)
}

// hrun is a run of the horizontal glyph exactly n columns wide: fill with the remainder
// made up in spaces. fill cuts rather than pads, so a two-column override asked for an
// odd number of columns comes back one short — which in a rule is precisely the column
// that would leave the closing corner off the last cell of the line.
func hrun(n int, g Glyphs) Span {
	h := fill(g.Get("frame.h"), n)
	if w := ansi.StringWidth(h); w < n {
		h += strings.Repeat(" ", n-w)
	}
	return Span{Text: h, Style: "input.frame"}
}

// cursorAt walks the text the same way Render lays it out — split on the breaks, then
// hard-wrapped — so the two can never disagree about which row a rune is on.
//
// The wrap decision comes before the cursor is answered, because the two questions are
// "where is this rune drawn" and "where is the cursor" in that order. A rune that does not
// fit the row it was measured against goes on the next one, and a cursor answered first
// names the column past the end of the row above instead. Unboxed that column was off the
// right edge, where a terminal quietly clamps it and nobody noticed; inside a box it is a
// padding cell one row above the character the human is editing.
func (in *Input) cursorAt(room int) Cursor {
	line, col := 0, 0
	for i, r := range in.runes {
		// A break ends a row without being drawn on it, so a cursor on one has no cell of
		// its own. It sits on the column just after the last character of the row it ends,
		// which inside a box is a padding cell — and that is the honest place for it,
		// because what gets typed there joins the row above the break and not the row
		// below. The column can be the last one the row has, which makes it the one
		// position this function reports that no character can be at, and it is kept there
		// rather than pushed onto the next row the way the end of the text is. The next row
		// begins with the first character after the break, and a cursor on that cell would
		// be indistinguishable from a cursor on that character: two states of the editor,
		// one screen, and a left arrow that appears to do nothing. A blank cell at the end
		// of the row the break closes covers nothing up and says what it is.
		if r == '\n' {
			if i == in.cursor {
				return Cursor{Line: line, Col: col}
			}
			line, col = line+1, 0
			continue
		}
		w := ansi.StringWidth(string(r))
		if col+w > room {
			line++
			col = 0
		}
		if i == in.cursor {
			return Cursor{Line: line, Col: col}
		}
		col += w
	}
	// Past the last character, where the next one goes — on the next row when this one is
	// full, since the next character will not fit here either. Nothing else is on that row,
	// so unlike a break there is no position to lose by moving.
	if col >= room {
		return Cursor{Line: line + 1, Col: 0}
	}
	return Cursor{Line: line, Col: col}
}

// Lines reports how many visual lines the text occupies at the given wrap width.
// It is used by the app to decide whether arrow keys should move the cursor or
// walk the history.
func (in *Input) Lines(room int) int {
	if len(in.runes) == 0 {
		return 1
	}
	line, col := 0, 0
	for _, r := range in.runes {
		if r == '\n' {
			line++
			col = 0
			continue
		}
		w := ansi.StringWidth(string(r))
		if col+w > room {
			line++
			col = 0
		}
		col += w
	}
	return line + 1
}

// CursorLine reports which visual line (0-based) the cursor is on at the given
// wrap width.
func (in *Input) CursorLine(room int) int {
	return in.cursorAt(room).Line
}

// Up moves the cursor one visual line up within the text. It returns true if the
// cursor actually moved (i.e. it was not already on the first line).
func (in *Input) Up(room int) bool {
	cur := in.cursorAt(room)
	if cur.Line == 0 {
		return false
	}
	// Walk backwards to find the same column on the previous line.
	in.cursor = in.runeAtPos(cur.Line-1, cur.Col, room)
	return true
}

// Down moves the cursor one visual line down within the text. It returns true if
// the cursor actually moved (i.e. it was not already on the last line).
func (in *Input) Down(room int) bool {
	cur := in.cursorAt(room)
	last := in.Lines(room) - 1
	if cur.Line >= last {
		return false
	}
	in.cursor = in.runeAtPos(cur.Line+1, cur.Col, room)
	return true
}

// runeAtPos finds the rune index closest to the given visual line and column.
func (in *Input) runeAtPos(targetLine, targetCol, room int) int {
	line, col := 0, 0
	best := len(in.runes) // default: end of text
	for i, r := range in.runes {
		if line == targetLine {
			if col >= targetCol {
				return i
			}
			best = i
		}
		if line > targetLine {
			return best
		}
		if r == '\n' {
			if line == targetLine {
				return i // end of the target line
			}
			line++
			col = 0
			continue
		}
		w := ansi.StringWidth(string(r))
		if col+w > room {
			if line == targetLine && col >= targetCol {
				return i
			}
			line++
			col = 0
			if line == targetLine {
				best = i
			}
		}
		if line > targetLine {
			return best
		}
		col += w
	}
	return best
}

// Room returns the number of columns available for text at the given frame width,
// matching the layout Render uses. It is exported so the app can decide whether the
// cursor is on the first or last visual line without re-deriving the box arithmetic.
func (in *Input) Room(width int) int {
	hw := Line{Span{Text: "❯ "}}.Width()
	room := width - hw
	vw := 1
	box := 2*vw + 2
	if width-hw-box >= 2 {
		room -= box
	}
	if room < 1 {
		room = 1
	}
	return room
}

func truncate(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}
