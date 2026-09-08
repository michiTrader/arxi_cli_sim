package ui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A model terminal. It exists because the inline algorithm is a claim about a
// terminal's state — "I own exactly these rows, everything above is history" — and
// a claim like that is either checked against something that behaves like a
// terminal or it is checked by hand on a real one, once, and then broken silently
// six commits later.
//
// Every vt has a screen, because inline mode now claims one: the claim is a scroll, the
// paint is clamped to the margins, and a model with no margins has nothing to clamp at and
// nothing to check. Above the screen is scrollback, which nothing may address again.
//
// The second shape also has a width, and it is the only one that can state the mechanism
// that survived two fixes. A width means rows have a right margin, so a frame laid out for
// a geometry the terminal no longer has either wraps and scrolls or is clipped, depending
// on auto-wrap; and Resize joins the rows the terminal itself produced by wrapping and
// re-splits the paper, which is how rows leave the screen on a narrowing and come back on a
// widening. Real terminals disagree about the details and none of it is observable from
// inside a program, so this models the pessimistic policy throughout — the one that
// surfaces old rows rather than losing them.
//
// Either shape can also be switched to the alternate buffer, which is where an interactive
// run is drawn now. That buffer is a second screen with no scrollback behind it and no reflow
// when the geometry changes, and both of those absences are the reason the interface lives
// there; the main screen is parked, unchanged, until the transcript is printed onto it at the
// end. Modelling the switch is what lets a test say that in so many words.
type vt struct {
	rows   []vtRow
	cur    int
	col    int
	top    int  // first row still on screen; everything before it is scrollback
	width  int  // 0 means unbounded: no row ever reaches a right margin
	height int  // 0 means unbounded: nothing ever scrolls
	wrap   bool // DECAWM, which an absolute paint switches off around itself
	alt    bool // the alternate buffer is current and the main one is parked in saved
	saved  buffer
}

// buffer is a screen parked while the other one is current. What makes it worth modelling is
// that neither half of the switch is idempotent: a second ?1049h saves a cursor from inside
// the alternate buffer, losing the shell's own position for good, and a second ?1049l
// restores the position the first switch saved — which by then is above whatever has been
// printed since. The emitter keeps "send each exactly once" with a single bool, and nothing
// else in the program would notice if it stopped, so the model fails the test on both.
type buffer struct {
	rows          []vtRow
	cur, col, top int
}

// vtRow is a physical row and where it came from. cont marks a row the terminal made by
// wrapping the one above rather than one something asked for, which is the only thing a
// resize needs to know: those are the rows a widening terminal may join back together,
// and joining them is what lifts rows out of scrollback and back onto the screen.
type vtRow struct {
	text string
	cont bool
}

// newScreenVT is a terminal with a screen h rows tall and no right margin.
func newScreenVT(h int) *vt { return &vt{rows: []vtRow{{}}, height: h, wrap: true} }

// newReflowVT is a screen w columns wide that re-wraps its own rows when it is resized.
func newReflowVT(w, h int) *vt {
	return &vt{rows: []vtRow{{}}, width: w, height: h, wrap: true}
}

// Screen is what a human can see; Scrollback is everything that has left the top and is
// beyond reach for good. There is deliberately no accessor for the two joined together: a
// test that compares against "everything the terminal ever held" cannot tell a row we
// repainted from a row we abandoned, and telling those apart is the whole subject here.
func (v *vt) Screen() string       { return strings.Join(rowText(v.rows[v.top:]), "\n") }
func (v *vt) Scrollback() []string { return rowText(v.rows[:v.top]) }

// MainPaper is the one accessor that does join screen and history, and it is allowed to
// because it answers a different question: not "what can be repainted" but "did anything of
// ours reach the user's own screen at all". For that, a row of ours that has scrolled into
// their history is just as much a yes as one still visible — more of one, since it is the
// half they keep. While the alternate buffer is up this is the parked main screen, which is
// what makes "the interface never wrote on your terminal" a thing a test can state.
func (v *vt) MainPaper() []string {
	if v.alt {
		return rowText(v.saved.rows)
	}
	return rowText(v.rows)
}

func rowText(rows []vtRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.text)
	}
	return out
}

// scroll moves the screen down once the cursor has walked past its last row. What
// leaves the top becomes scrollback, which nothing in inline mode may address again.
func (v *vt) scroll() {
	if v.height > 0 && v.cur >= v.top+v.height {
		v.top = v.cur - v.height + 1
	}
}

// writeText puts ink on the paper, one row at a time unless the paper has a margin.
//
// The margin is where a stale frame is decided. With auto-wrap on, a row wider than the
// terminal takes a second physical row, and once the region is as tall as the screen
// that second row has to come from somewhere: the bottom of the screen moves down and
// the top of it leaves. With auto-wrap off the margin swallows the overflow instead —
// one row in, one row out, whatever the width turned out to be.
func (v *vt) writeText(s string) {
	if v.width <= 0 {
		v.put(s)
		return
	}
	for _, c := range s {
		if v.col >= v.width {
			if !v.wrap {
				v.col = v.width - 1
			} else {
				v.newline(true)
				v.col = 0
			}
		}
		v.put(string(c))
	}
}

func (v *vt) put(s string) {
	in := []rune(s)
	if len(in) == 0 {
		return
	}
	r := []rune(v.rows[v.cur].text)
	for len(r) < v.col {
		r = append(r, ' ')
	}
	for i, c := range in {
		if v.col+i < len(r) {
			r[v.col+i] = c
			continue
		}
		r = append(r, c)
	}
	v.rows[v.cur].text = string(r)
	v.col += len(in)
}

// newline moves down a row without touching the column, which is what LF does. cont
// says whether the terminal got here by wrapping a row of its own accord.
func (v *vt) newline(cont bool) {
	v.cur++
	if v.cur == len(v.rows) {
		v.rows = append(v.rows, vtRow{})
	}
	v.rows[v.cur].cont = cont
	v.scroll()
}

// feed applies the bytes an Emitter produced. It understands only the sequences we
// actually emit, and fails the test on anything else, so a new escape sequence
// cannot slip in without someone deciding what it does to the screen.
func (v *vt) feed(t *testing.T, out []byte) {
	t.Helper()
	s := string(out)
	for s != "" {
		if i := strings.IndexAny(s, "\x1b\r\n"); i != 0 {
			if i < 0 {
				v.writeText(s)
				return
			}
			v.writeText(s[:i])
			s = s[i:]
			continue
		}
		switch s[0] {
		case '\r':
			v.col = 0
			s = s[1:]
			continue
		case '\n':
			v.newline(false)
			s = s[1:]
			continue
		}
		if !strings.HasPrefix(s, "\x1b[") {
			t.Fatalf("unhandled escape %q", s[:min(8, len(s))])
		}
		j := 2
		for j < len(s) && (s[j] < '@' || s[j] > '~') {
			j++
		}
		if j == len(s) {
			t.Fatalf("unterminated escape %q", s)
		}
		v.csi(t, s[2:j], s[j])
		s = s[j+1:]
	}
}

func (v *vt) csi(t *testing.T, params string, final byte) {
	t.Helper()
	arg := func(i, def int) int {
		parts := strings.Split(params, ";")
		if i >= len(parts) || parts[i] == "" {
			return def
		}
		x, err := strconv.Atoi(parts[i])
		if err != nil {
			t.Fatalf("bad CSI parameter %q before %q", params, string(final))
		}
		return x
	}
	n := func(def int) int { return arg(0, def) }
	switch final {
	case 'm': // styling: no effect on the text
	case 'h', 'l':
		// Modes, none of which move ink — except two. Auto-wrap decides whether a row wider
		// than the terminal takes a second row or is clipped at the margin, and that is the
		// difference this file exists to check; 1049 switches screens, which is the surface
		// an interactive run is drawn on.
		switch params {
		case "?7":
			v.wrap = final == 'h'
		case "?1049":
			v.switchBuffer(t, final == 'h')
		}
	case 'H':
		// CUP, which is the whole basis of the absolute paint. Two things about it are
		// load-bearing and both are modelled here: it is 1-based and row-first, and
		// every terminal clamps it to the screen. The clamp is why a paint made of
		// nothing but CUP cannot push a row off the top however wrong its idea of the
		// geometry turned out to be — where a newline at the bottom row would.
		row, col := arg(0, 1), arg(1, 1)
		v.cur = v.top + row - 1
		if v.cur < v.top {
			v.cur = v.top
		}
		if v.height > 0 && v.cur > v.top+v.height-1 {
			v.cur = v.top + v.height - 1
		}
		for len(v.rows) <= v.cur {
			v.rows = append(v.rows, vtRow{})
		}
		if v.col = col - 1; v.col < 0 {
			v.col = 0
		}
		// The row's continuation flag is deliberately left alone. It would be reasonable to
		// clear it — we are addressing this row on purpose, so it is no continuation of the
		// one above, and xterm and VTE do appear to clear it on an erase — but a program
		// cannot ask, Windows Terminal is not obviously in that camp, and the version of
		// this model that cleared it was passing while the user's terminal was not. So the
		// pessimistic reading stands: a row we painted over may still be spliced into its
		// neighbour by a widening reflow, and the design has to survive that rather than
		// assume it away. It does, because the next frame repaints every visible row.
	case 'A':
		// Nothing emits this any more: a relative walk is a count of rows the terminal made
		// of ours, which is the arithmetic the whole bug was made of. The handler stays so
		// that a regression shows up as a wrong screen as well as by name in the two tests
		// that check the bytes for it. CUU is clamped at the top margin by every terminal.
		v.cur -= n(1)
		if v.cur < v.top {
			v.cur = v.top
		}
	case 'C':
		v.col += n(1)
	case 'J':
		// Erase-below only, in either mode. ED 2 and ED 3 are how every other TUI clears a
		// screen and they are banned outright here: on the main screen they take the user's
		// history with them, and a program that has just been handed a terminal has no
		// business deciding that what was on it before does not matter.
		if n(0) != 0 {
			t.Fatalf("erase display %d takes history that is not ours to take", n(0))
		}
		v.rows[v.cur].text = truncRunes(v.rows[v.cur].text, v.col)
		v.rows = v.rows[:v.cur+1]
	case 'K':
		v.rows[v.cur].text = truncRunes(v.rows[v.cur].text, v.col)
	case 'u':
		// The keyboard stack, and the only sequence modelled here that is about input rather
		// than about ink: CSI > 1 u pushes the Kitty disambiguate flag, CSI < 1 u pops it off.
		// Neither writes a cell nor moves the cursor, so nothing is the honest model — but only
		// with a private marker. A bare CSI u is SCORC, which restores a saved cursor, and
		// letting every final 'u' through would model a move as a no-op, which is the exact
		// class of lie this terminal exists to refuse.
		if !strings.HasPrefix(params, ">") && !strings.HasPrefix(params, "<") {
			t.Fatalf("unhandled CSI %q %q", params, string(final))
		}
	default:
		t.Fatalf("unhandled CSI %q %q", params, string(final))
	}
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// switchBuffer is ?1049h and ?1049l: take the alternate screen, or give it back.
//
// The alternate buffer starts blank, is exactly as tall as the terminal, and has no
// scrollback behind it — a row that leaves its top is destroyed rather than saved. That
// absence is the whole reason the interface is drawn there: there is nothing for a later
// reflow to splice back together and drag into view, which is what the user was seeing.
//
// Both fatals are the same kind of bug: the sequences are not idempotent, so sending either
// one twice is not a wasted write but a lost position. The emitter guards both with one bool,
// and no other test in this file would notice if that bool stopped being set.
func (v *vt) switchBuffer(t *testing.T, on bool) {
	t.Helper()
	if on {
		if v.alt {
			t.Fatal("?1049h twice: the second one saves a cursor from inside the alternate " +
				"buffer, and the shell's own position is then lost for good")
		}
		v.saved = buffer{rows: v.rows, cur: v.cur, col: v.col, top: v.top}
		v.rows, v.cur, v.col, v.top = blankRows(v.height), 0, 0, 0
		v.alt = true
		return
	}
	if !v.alt {
		t.Fatal("?1049l twice: the second one restores the position the first switch saved, " +
			"which is the shell prompt above everything printed since — the next prompt " +
			"would be drawn straight through the transcript")
	}
	v.rows, v.cur, v.col, v.top = v.saved.rows, v.saved.cur, v.saved.col, v.saved.top
	v.saved, v.alt = buffer{}, false
}

// blankRows is a screen with nothing on it. A terminal that reports no height still gets one
// row, because a buffer with none has nowhere to put a cursor.
func blankRows(h int) []vtRow {
	if h <= 0 {
		h = 1
	}
	return make([]vtRow, h)
}

// Resize sends the geometry change to whichever buffer is current. Both get one, because a
// terminal has to have the main screen at the right size for the switch back — that is the
// user's own paper being re-wrapped by their terminal, which is between the two of them. We
// never address a row of it until the handover at the end.
func (v *vt) Resize(w, h int) {
	if !v.alt {
		v.reflow(w, h)
		return
	}
	main := &vt{rows: v.saved.rows, cur: v.saved.cur, col: v.saved.col, top: v.saved.top,
		width: v.width, height: v.height, wrap: v.wrap}
	main.reflow(w, h)
	v.saved = buffer{rows: main.rows, cur: main.cur, col: main.col, top: main.top}
	v.resizeAlt(w, h)
}

// resizeAlt is the alternate buffer's answer to a drag, and the answer is almost nothing: no
// join, no re-split, no anchoring. Rows keep the index they had from the top, a narrowing
// clips them at the margin, a growth adds blank rows at the bottom, and a shrink drops the
// rows that no longer fit.
//
// Every absence in that list is deliberate and each one is a bug that cannot happen here.
// Nothing is joined because the buffer does not record that a row was made by wrapping, so a
// widening has nothing to recover — a row we painted at a width that is gone stays the width
// it was until the next frame overwrites it, instead of being spliced into its neighbour and
// pushed back into view. Nothing is anchored to the bottom because there is no scrollback to
// anchor away from. This is the property that makes the alternate screen the surface a drag
// cannot corrupt, and it is the reason the interface moved here rather than being tuned.
func (v *vt) resizeAlt(w, h int) {
	rows := blankRows(h)
	for i := range rows {
		if i >= len(v.rows) {
			break
		}
		text := v.rows[i].text
		if w > 0 {
			text = truncRunes(text, w)
		}
		rows[i] = vtRow{text: text}
	}
	v.rows, v.top = rows, 0
	if v.cur >= len(v.rows) {
		v.cur = len(v.rows) - 1
	}
	if w > 0 && v.col >= w {
		v.col = w - 1
	}
	v.width, v.height = w, h
}

// reflow is the main screen reflowing, which is the half of a drag no program can see and
// the half that made the bug permanent. Rows the terminal produced by wrapping are
// joined back onto the row they came from, the paper is re-split at the new width, and
// the result is anchored to its bottom. So a narrowing pushes rows into scrollback and a
// widening lifts them back onto the screen — including rows we wrote at a width that
// had already changed, which is how one stale paint keeps coming back into view.
//
// Terminals disagree about every part of this and a program cannot ask. Bottom-anchoring
// and joining every wrapped run is the pessimistic reading: it is the one that surfaces
// old rows rather than quietly losing them, so it is the one worth testing against. The
// split counts runes, which is exact where a real terminal would refuse to straddle a
// double-width grapheme across the margin; nothing in the emitter counts rows any more, so
// there is no arithmetic left for the difference to matter to.
func (v *vt) reflow(w, h int) {
	// Join, remembering where the caret sits inside its logical line.
	var lines []string
	caret, off := 0, 0
	for i, r := range v.rows {
		if r.cont && len(lines) > 0 {
			last := len(lines) - 1
			if i == v.cur {
				caret, off = last, len([]rune(lines[last]))+v.col
			}
			lines[last] += r.text
			continue
		}
		if i == v.cur {
			caret, off = len(lines), v.col
		}
		lines = append(lines, r.text)
	}

	// Re-split. A logical line always takes at least one row, however empty it is.
	v.rows = nil
	first := 0
	for li, s := range lines {
		if li == caret {
			first = len(v.rows)
		}
		r := []rune(s)
		for start := 0; ; {
			end := len(r)
			if w > 0 && end-start > w {
				end = start + w
			}
			v.rows = append(v.rows, vtRow{text: string(r[start:end]), cont: start > 0})
			if start = end; start >= len(r) {
				break
			}
		}
	}
	if len(v.rows) == 0 {
		v.rows = []vtRow{{}}
	}

	// The caret keeps its offset inside its logical line, which is the only thing a
	// terminal that reflows can honestly do with it.
	v.cur, v.col = first, off
	if w > 0 {
		v.cur, v.col = first+off/w, off%w
	}
	if v.cur >= len(v.rows) {
		v.cur = len(v.rows) - 1
	}
	v.width, v.height = w, h
	if v.top = 0; h > 0 && len(v.rows) > h {
		v.top = len(v.rows) - h
	}
	if v.cur < v.top {
		v.cur = v.top
	}
}

// TestInlineEmitRebuildsEveryFrame is the load-bearing test of the emit layer. It
// plays the scenario one step at a time, feeds each frame's bytes to the model
// terminal, and demands the screen be the frame. Every inline bug lives here: a row
// painted at the wrong absolute position, an erase that forgot a row, a frame that
// covers less than the screen and leaves rows behind that nothing will ever repaint.
//
// The screen is deliberately shorter than the transcript, so the run passes through both
// shapes a frame can have: a young conversation that does not fill the screen and is
// padded out to it, and a grown one whose tail is cut to fit. Padding is the half that is
// new and the half the report was about — the rows a shorter frame would have left above
// itself are rows we printed, and they are what a drag was duplicating. The input is
// several rows tall every fourth frame, which swings the chrome and with it the boundary
// between the two.
func TestInlineEmitRebuildsEveryFrame(t *testing.T) {
	vp := shortViewport(72)
	_, sc := play(t, firstConversation, 0)
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Profile: ProfileTrueColor}
	screen := newScreenVT(vp.Height)
	screen.feed(t, e.Enter())

	r := NewRenderer()
	padded, filled := 0, 0
	for n := 0; n <= len(sc.Steps); n++ {
		st, _ := play(t, firstConversation, n)
		in := NewInput()
		if n%4 == 0 {
			in.SetText(strings.Repeat("a prompt long enough to wrap on a narrow terminal, twice over ", 5))
		}
		f := render(r, st, in, vp)
		if len(f.Live) != f.Height {
			t.Fatalf("after %d steps the frame is %d rows on a %d-row screen; anything it does not cover is a row of ours nothing can repaint", n, len(f.Live), f.Height)
		}
		if f.Live[len(f.Live)-1].Text() == "" {
			padded++
		} else {
			filled++
		}
		screen.feed(t, e.Emit(f))
		if got, want := screen.Screen(), strings.Join(texts(f.Live), "\n"); got != want {
			t.Fatalf("after %d steps the screen and the frame disagree\n--- screen ---\n%s\n--- frame ---\n%s", n, got, want)
		}
	}
	if padded == 0 {
		t.Fatal("no frame was shorter than the screen, so the padding that claims the rest of it was never exercised")
	}
	if filled == 0 {
		t.Fatal("no frame filled the screen, so the transcript never had to be cut to fit")
	}
}

// TestEmitParksTheCursorWhereTheInputSays. The caret is the one thing a frame
// cannot draw: it is a position, and if emit puts it anywhere else the user types
// into the wrong column and every visible thing still looks right.
//
// The frame's row is a row of the screen, so it is checked against the screen's own top
// and not against the paper's: the first frame of a session scrolls the user's rows into
// their history, which is exactly what makes row one ours to address.
func TestEmitParksTheCursorWhereTheInputSays(t *testing.T) {
	vp := testViewport(40)
	st, _ := play(t, firstConversation, -1)
	for _, text := range []string{"", "short", "a prompt long enough to wrap on a narrow terminal, twice over"} {
		e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline}
		screen := newScreenVT(vp.Height)
		screen.feed(t, e.Enter())
		in := NewInput()
		in.SetText(text)
		f := NewRenderer().Render(st, in, vp)
		screen.feed(t, e.Emit(f))
		if got := screen.cur - screen.top; got != f.Cursor.Line || screen.col != f.Cursor.Col {
			t.Errorf("text %q: cursor on screen row %d col %d, frame says row %d col %d",
				text, got, screen.col, f.Cursor.Line, f.Cursor.Col)
		}
	}
}

// TestEmitNeverTakesScrollback. CSI 3 J deletes the terminal's saved lines, and a
// TUI that sends it has thrown away the user's history to make its own redraw
// easier. The model terminal rejects erase-display with any parameter but 0; this
// checks the bytes directly as well, because that is the guarantee we make to the
// user and it should fail loudly and by name.
//
// The newline count is the other half of the same guarantee, and it is the one that
// changed. A newline is the only sequence that puts a row where we can never address it
// again, so inline mode sends exactly f.Height of them, once, on the first frame: that is
// the claim, and what it moves into the user's history is the user's own screen, intact.
// Every frame after it is absolute paint and contains none at all. Counting them is
// therefore counting the rows this layer ever gave away.
func TestEmitNeverTakesScrollback(t *testing.T) {
	_, sc := play(t, firstConversation, 0)
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline}
	all := string(e.Enter())
	for n := 0; n <= len(sc.Steps); n++ {
		st, _ := play(t, firstConversation, n)
		f := NewRenderer().Render(st, NewInput(), shortViewport(72))
		out := string(e.Emit(f))
		all += out
		want := 0
		if n == 0 {
			want = f.Height
		}
		if got := strings.Count(out, "\n"); got != want {
			t.Fatalf("frame %d sent %d newlines, want %d; every one of them hands a row to the terminal for good", n, got, want)
		}
	}
	all += string(e.Exit())
	for _, banned := range []string{"\x1b[3J", "\x1b[2J", "\x1bc"} {
		if strings.Contains(all, banned) {
			t.Errorf("inline emit sent %q", banned)
		}
	}
}

// TestSameFrameSameBytes. Render is pure, so two identical frames must produce
// identical bytes for the same emitter state. Without this an animated widget can
// make the terminal work for nothing, which on a phone is battery.
//
// The one frame that is not a pure function of the frame is the first of a session, which
// carries the claim, and the last part of this pins the difference to exactly that: the
// newlines and nothing else.
func TestSameFrameSameBytes(t *testing.T) {
	st, _ := play(t, firstConversation, -1)
	f := NewRenderer().Render(st, NewInput(), testViewport(72))
	a := &Emitter{Theme: DefaultTheme(), Mode: ModeInline}
	b := &Emitter{Theme: DefaultTheme(), Mode: ModeInline}
	if string(a.Emit(f)) != string(b.Emit(f)) {
		t.Fatal("the same frame emitted different bytes")
	}
	first := string(a.Emit(f))
	if second := string(a.Emit(f)); first != second {
		t.Fatalf("repainting an unchanged frame is not stable\n first: %q\nsecond: %q", first, second)
	}

	c := &Emitter{Theme: DefaultTheme(), Mode: ModeInline}
	claimed := string(c.Emit(f))
	if !strings.Contains(claimed, strings.Repeat("\n", f.Height)) {
		t.Fatalf("the first frame of a session did not claim its %d rows", f.Height)
	}
	if rest := string(c.Emit(f)); len(claimed) != len(rest)+f.Height {
		t.Fatalf("the first frame is %d bytes and the second %d; they should differ by the %d newlines of the claim and nothing else", len(claimed), len(rest), f.Height)
	}
}

// The five tests that follow are the resize path, which is where the bug report lives. A
// real terminal re-wraps the rows it already holds when its width changes, and the vt
// here can be asked to do the same, so these tests can state what the report described:
// the tail of the transcript printed again, and again on the next drag, and staying that
// way. What they hold the emitter to is the screen and never the paper. The paper holds
// rows a narrowing reflow spilled out of the top of the screen, which the terminal wrapped
// under rules we get no vote in and no escape sequence can reach; the screen is what a
// frame redraws whole, so it is the only thing we are in a position to claim.

// TestTheTranscriptReachesTheTerminalOnceAtSessionEnd is where the original defect went.
// It used to be reachable: a row count taken at one width was used as an index into a
// frame wrapped at another, so a drag reprinted a paragraph the terminal already had and
// nothing could ever take it back. That arithmetic is gone because the thing it counted
// is gone — mid-session no row is handed over at all. The screen is repainted whole, the
// only rows in the terminal's history are the ones that were there before we started, and
// the transcript arrives in one piece when the session ends, wrapped to the width it ended
// on. Which is also the shape the user asked for: "no necesariamente se tiene que ir
// agregando las lineas fijas a la terminal sino que al finalizar la sesion se imprimen."
//
// So the property is countable now instead of arithmetical: every row of the transcript
// reaches the terminal exactly once, and the frame that puts it there is the last one.
//
// The screen is deliberately small. The first conversation is about thirty-five rows at
// these widths, so on a screen that nearly holds it almost every row has been visible at
// some point mid-session and there is hardly anything left for the handover to be the sole
// source of — the guard below counts exactly that and would have nothing to count. Twelve
// rows leaves two thirds of the transcript off the screen, which is the case the last
// frame is answerable for.
func TestTheTranscriptReachesTheTerminalOnceAtSessionEnd(t *testing.T) {
	const screenH = 12
	st, _ := play(t, firstConversation, -1)
	r := NewRenderer()
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Profile: ProfileTrueColor}
	screen := newScreenVT(screenH)
	screen.feed(t, e.Enter())

	var during []string  // every mid-session live region, as text
	var streams []string // and the visible bytes that drew it
	for _, w := range []int{80, 40, 72, 100, 60} {
		f := render(r, st, NewInput(), Viewport{Width: w, Height: screenH})
		if n := len(f.Committed); n != 0 {
			t.Fatalf("at width %d a mid-session frame handed over %d rows; a row we promised never to redraw is a row the next drag reflows out of reach", w, n)
		}
		out := e.Emit(f)
		screen.feed(t, out)
		streams = append(streams, visible(string(out)))
		during = append(during, strings.Join(texts(f.Live), "\n"))
		// The claim scrolled the user's own screen into their history, and that is all that
		// may be down there: it was blank before the session started, and nothing painted
		// afterwards can scroll.
		for j, row := range screen.Scrollback() {
			if row != "" {
				t.Fatalf("at width %d, row %d of the terminal's history is %q, so something of ours reached it mid-session", w, j, row)
			}
		}
	}
	// Session end, which is settle(): no widgets, no input, no height. Height is the
	// signal — a frame with none is a document, and a document has nothing left to hold.
	r.Widgets = nil
	final := r.Render(st, nil, Viewport{Width: 60})
	if len(final.Live) != 0 {
		t.Fatalf("the handover frame kept %d rows live, so the session ends still owning part of the screen", len(final.Live))
	}
	// Only rows no frame ever put on screen can state anything: the tail of the transcript
	// was on screen the whole time, so finding it in the last frame's bytes is expected and
	// finding it in an earlier frame's is not a duplicate.
	var fresh []string
	for _, line := range committedProbes(final) {
		seen := false
		for _, region := range during {
			if strings.Contains(region, line) {
				seen = true
				break
			}
		}
		if !seen {
			fresh = append(fresh, line)
		}
	}
	if len(fresh) < 8 {
		t.Fatalf("only %d rows of the transcript were never on screen mid-session, so this proves nothing", len(fresh))
	}
	out := e.Emit(final)
	screen.feed(t, out)
	printed := visible(string(out))
	for _, line := range fresh {
		if !strings.Contains(printed, line) {
			t.Fatalf("the session ended without printing %q, and no frame had it on screen either, so the user never got it", line)
		}
		for i, s := range streams {
			if strings.Contains(s, line) {
				t.Fatalf("frame %d printed %q mid-session and the handover printed it again, so the terminal now has it twice", i, line)
			}
		}
	}
}

// TestADragLeavesOneLiveRegionOnScreen is the exact shape of the bug report: the input
// bar four times over, blank rows between them, "y se queda así".
//
// The rule it states used to be the opposite one. Not walking up was the cautious choice
// while rows were frozen into the terminal as blocks sealed — a walk measured at the old
// width overshoots on a widening terminal, and the erase then eats transcript nobody can
// redraw. Nothing is frozen now, so there is nothing left to protect: every row on screen
// is a row this frame lays out again. Each is painted where it belongs, absolutely, and
// the stale copies left behind by a walk that guessed wrong are what four input bars after
// four drags actually were.
//
// So this asserts the outcome, on a screen with a bottom: after every frame the bar is on
// screen exactly once and has never reached scrollback, where nothing could erase it
// again. And it asserts the two bytes the outcome rests on being absent after the claim —
// a newline and a walk — because those are how a frame written for a width that changed a
// microsecond ago pushes its own rows into the user's history instead of merely garbling
// itself.
func TestADragLeavesOneLiveRegionOnScreen(t *testing.T) {
	const bar = "ask anything, or / for commands"
	st, _ := play(t, linesChanged, -1)
	r := NewRenderer()
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Profile: ProfileTrueColor}
	screen := newScreenVT(24)
	screen.feed(t, e.Enter())

	// Every width is at least 40 because the bar is 33 columns and a truncated one
	// would make the count below pass for the wrong reason. Both directions are here:
	// narrowing re-wraps into more rows, widening into fewer, and it was widening that
	// produced the report.
	for i, w := range []int{80, 60, 40, 50, 72, 100} {
		f := render(r, st, NewInput(), Viewport{Width: w, Height: 24})
		if len(f.Live) != f.Height {
			t.Fatalf("at width %d the frame is %d rows on a %d-row screen, so it does not cover the screen and states nothing about a drag", w, len(f.Live), f.Height)
		}
		out := e.Emit(f)
		screen.feed(t, out)

		if got := strings.Count(screen.Screen(), bar); got != 1 {
			t.Fatalf("at width %d the input bar is on screen %d times, want 1\n--- screen ---\n%s", w, got, screen.Screen())
		}
		for j, row := range screen.Scrollback() {
			if strings.Contains(row, bar) {
				t.Fatalf("at width %d the input bar reached scrollback at row %d, where nothing can erase it: %q", w, j, row)
			}
		}
		if i > 0 && strings.ContainsRune(string(out), '\n') {
			t.Fatalf("at width %d the frame sent a newline, and a newline at the bottom margin scrolls whatever the width happens to be", w)
		}
		if at := firstCSI(string(out), 'A'); at >= 0 {
			t.Fatalf("at width %d the frame walked up at byte %d, which is a guess about how many rows the terminal made of ours", w, at)
		}
	}
	// And the emitter is holding nothing a drag could have invalidated. The counters this
	// used to keep — the region's height, the caret's column, the display width of every
	// row, the geometry they were measured at — were measurements of a screen that had
	// already changed; all that is left is that the screen is claimed and how tall it was.
	if !e.claimed || e.rows != 24 {
		t.Fatalf("after the drag the emitter holds claimed=%v rows=%d, want a claim on a 24-row screen", e.claimed, e.rows)
	}
}

// TestAProgressiveDragReflowsWithoutDuplicating is the report as the user reproduced it:
// "al cambiar progresivamente el tamaño de la terminal". A slow drag is not one resize, it
// is dozens, and the terminal re-wraps the rows it already holds before it tells us. The
// old code answered each of them with a walk up from the caret, and a walk is a guess
// about how many rows the terminal made of ours; once wrong it stayed wrong, because the
// next frame walked up from a caret that was already in the wrong place. A screenful of
// input bars is what a compounded guess looks like.
//
// The screen here reflows, which is what makes this the drag and not a sequence of
// unrelated widths. The assertion is the same one every time: whatever the terminal did to
// the rows in between, the frame after it is the screen.
func TestAProgressiveDragReflowsWithoutDuplicating(t *testing.T) {
	const bar = "ask anything, or / for commands"
	st, _ := play(t, linesChanged, -1)
	r := NewRenderer()
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Profile: ProfileTrueColor}
	screen := newReflowVT(100, 24)
	screen.feed(t, e.Enter())

	for _, w := range []int{100, 60, 40, 50, 72, 100} {
		// The terminal reflows first and the frame is laid out afterwards, which is the
		// good case and the common one: app.sync ioctls immediately before every render.
		// A frame laid out for a width that is already gone is the test below this one.
		screen.Resize(w, 24)
		f := render(r, st, NewInput(), Viewport{Width: w, Height: 24})
		if len(f.Live) != f.Height {
			t.Fatalf("at width %d the frame is %d rows on a %d-row screen; it has to be the screen for the comparison below to mean anything", w, len(f.Live), f.Height)
		}
		screen.feed(t, e.Emit(f))

		want := strings.Join(texts(f.Live), "\n")
		if got := screen.Screen(); got != want {
			t.Fatalf("at width %d the screen is not the live region\n--- screen ---\n%s\n--- frame ---\n%s", w, got, want)
		}
		if got := strings.Count(screen.Screen(), bar); got != 1 {
			t.Fatalf("at width %d the input bar is on screen %d times, want 1", w, got)
		}
	}
}

// TestACornerDragReflowsWithoutDuplicating is the same drag done the way a person actually
// does it: by the corner, so the height moves too. It is worth its own test because the
// height is the one number the emitter still keeps, and it keeps it exactly once — the
// screen is claimed on the first frame and never re-claimed, so every later height has to be
// handled by the paint alone.
//
// Both directions matter and the growing one is the nastier of the two. A terminal that gets
// taller does not hand back blank rows; the pessimistic reading, which is the one modelled
// here, is that it pulls rows out of scrollback onto the top of the screen — rows from before
// the session, or rows a narrowing spilled up there mid-drag. Those are now visible, and
// under any design that owns a band rather than the screen they would stay visible and stale.
// Owning the screen is what answers them: the paint walks every row of the new height, so
// whatever the reflow dragged into view is overwritten by the frame laid out for it.
//
// What this deliberately does not assert is that scrollback stays clean. A shrink pushes rows
// off the top by definition, and once up there no erase of ours reaches them — that is the
// documented residual, and it is why the assertion is about the screen.
func TestACornerDragReflowsWithoutDuplicating(t *testing.T) {
	const bar = "ask anything, or / for commands"
	st, _ := play(t, linesChanged, -1)
	r := NewRenderer()
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Profile: ProfileTrueColor}
	screen := newReflowVT(100, 24)
	screen.feed(t, e.Enter())

	// Every width leaves room for the bar and every height leaves room for the chrome, so a
	// failure below is the drag and not a frame that never fitted in the first place.
	for _, size := range [][2]int{{100, 24}, {80, 32}, {60, 16}, {44, 40}, {72, 12}, {100, 28}} {
		w, h := size[0], size[1]
		screen.Resize(w, h)
		f := render(r, st, NewInput(), Viewport{Width: w, Height: h})
		if len(f.Live) != f.Height {
			t.Fatalf("at %dx%d the frame is %d rows, so it is not the screen and the comparison below means nothing", w, h, len(f.Live))
		}
		screen.feed(t, e.Emit(f))

		want := strings.Join(texts(f.Live), "\n")
		if got := screen.Screen(); got != want {
			t.Fatalf("at %dx%d the screen is not the live region\n--- screen ---\n%s\n--- frame ---\n%s", w, h, got, want)
		}
		if got := strings.Count(screen.Screen(), bar); got != 1 {
			t.Fatalf("at %dx%d the input bar is on screen %d times, want 1", w, h, got)
		}
		if e.rows != h {
			t.Fatalf("at %dx%d the emitter thinks the screen is %d rows tall, and Exit steps to that row", w, h, e.rows)
		}
	}
}

// TestAStaleFrameNeverScrollsTheTerminal is the gap app.sync cannot close. The size is
// asked of the terminal immediately before every render, so a frame is laid out for the
// geometry the screen had a moment ago — and during a progressive drag that moment is
// often enough for the screen to have changed again before the write lands. The frame is
// then too wide and too tall for the terminal receiving it, and how a terminal reacts to
// that is the whole bug: rows wrap past the right margin, the region grows past the
// bottom, and what scrolls off the top is our own paint, into the user's scrollback where
// no erase of ours can ever reach it again. A later widening reflow joins those rows back
// together and brings them onto the screen, which is the "y se queda así" half of the
// report: the duplicate is not redrawn, it is recovered.
//
// So a stale frame is allowed to garble itself and forbidden to move the screen. Both
// halves of that come from bytes rather than from arithmetic: CUP clamps at the bottom
// margin where a newline scrolls, and DECAWM off clips at the right margin where a wrap
// would consume the row below. Neither needs to know the terminal's real size, which is
// what makes them safe against a size we could not have known.
//
// A model terminal proves the reasoning and not the terminal. This says the emitter
// cannot lose a row to scrollback under a geometry it was never told about; only a real
// terminal under a real drag says the user stopped seeing duplicates.
func TestAStaleFrameNeverScrollsTheTerminal(t *testing.T) {
	const bar = "ask anything, or / for commands"
	st, _ := play(t, linesChanged, -1)
	r := NewRenderer()
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Profile: ProfileTrueColor}
	screen := newReflowVT(100, 24)
	screen.feed(t, e.Enter())

	// The frame the drag is about to invalidate: laid out for 100x24, correct when it was
	// rendered, and written only after the screen has already moved on. Emitting it here is
	// also what claims the screen, so the frames inside the loop are pure paint.
	stale := render(r, st, NewInput(), Viewport{Width: 100, Height: 24})
	if len(stale.Live) != stale.Height {
		t.Fatalf("the 100x24 frame is %d rows on a %d-row screen, so it does not cover the screen and this test is about a frame that does", len(stale.Live), stale.Height)
	}
	screen.feed(t, e.Emit(stale))

	for _, to := range [][2]int{{40, 24}, {40, 12}} {
		// The terminal reflows first, which is its own doing and may well push rows into
		// its history, so the count that matters is taken afterwards. What is forbidden is
		// our write adding to it.
		screen.Resize(to[0], to[1])
		before := len(screen.Scrollback())
		b := e.Emit(stale)
		out := string(b)
		screen.feed(t, b)

		if n := len(screen.Scrollback()); n != before {
			t.Fatalf("a frame laid out for 100x24 and written to a %dx%d terminal pushed %d of its own rows into scrollback, where no erase reaches them and a widening reflow brings them back", to[0], to[1], n-before)
		}
		if strings.ContainsRune(out, '\n') {
			t.Fatalf("at %dx%d the frame sent a newline, which at the bottom margin scrolls whatever the real height turns out to be", to[0], to[1])
		}
		if i := firstCSI(out, 'A'); i >= 0 {
			t.Fatalf("at %dx%d the frame walked up at byte %d, and a walk counts the rows the terminal made of ours — the one number a stale width is guaranteed to get wrong", to[0], to[1], i)
		}
		for _, want := range []string{"\x1b[?7l", "\x1b[?7h"} {
			if !strings.Contains(out, want) {
				t.Fatalf("at %dx%d the frame did not send %q, so an over-wide row wraps into the row below instead of clipping at the margin", to[0], to[1], want)
			}
		}
	}

	// And the repair, which is what makes garbling acceptable: the next frame is laid out
	// for the terminal as it now is, and it is the entire screen again. A frame that only
	// corrected the rows it had got wrong would be back to counting.
	for _, to := range [][2]int{{40, 12}, {100, 24}} {
		screen.Resize(to[0], to[1])
		f := render(r, st, NewInput(), Viewport{Width: to[0], Height: to[1]})
		if len(f.Live) != f.Height {
			t.Fatalf("at %dx%d the frame is %d rows, so the screen and the frame are not the same thing and the comparison below would state nothing", to[0], to[1], len(f.Live))
		}
		screen.feed(t, e.Emit(f))
		want := strings.Join(texts(f.Live), "\n")
		if got := screen.Screen(); got != want {
			t.Fatalf("at %dx%d the screen did not recover the live region\n--- screen ---\n%s\n--- frame ---\n%s", to[0], to[1], got, want)
		}
		if got := strings.Count(screen.Screen(), bar); got != 1 {
			t.Fatalf("at %dx%d the input bar is on screen %d times after the repair, want 1", to[0], to[1], got)
		}
	}
}

// TestEmitStaysInSyncAfterAResize answers the half of the report that was not the
// duplication: "y se queda así" — it stays that way. A resize frame that repaired the
// screen and left the counters wrong drifts on every frame after it, so from the drag
// onwards the screen has to hold exactly the live region and nothing else, at every
// remaining step of the scenario.
//
// The screen is the assertion here and the paper is not. This vt has no right margin and
// never reflows, so what lies above the screen is what the claim scrolled there before the
// first frame — wrapped to a width that no longer exists and never ours to rewrite. What
// the emitter does claim is the screen, all of it: a frame is Height rows, so the screen is
// the frame, the whole of it, at every step, with no offset to account for, because a
// mid-session frame hands nothing over and there is no prefix above it to shrink.
func TestEmitStaysInSyncAfterAResize(t *testing.T) {
	_, sc := play(t, firstConversation, 0)
	half := len(sc.Steps) / 2
	r := NewRenderer()
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Profile: ProfileTrueColor}
	screen := newScreenVT(24)
	screen.feed(t, e.Enter())

	emit := func(n int, f Frame) {
		t.Helper()
		if len(f.Committed) != 0 {
			t.Fatalf("after %d steps a mid-session frame handed over %d rows, and the screen below is only the live region", n, len(f.Committed))
		}
		screen.feed(t, e.Emit(f))
		want := strings.Join(texts(f.Live), "\n")
		if got := screen.Screen(); got != want {
			t.Fatalf("after %d steps the screen has drifted from the frame\n--- screen ---\n%s\n--- frame ---\n%s", n, got, want)
		}
	}

	mid, _ := play(t, firstConversation, half)
	emit(half, render(r, mid, NewInput(), Viewport{Width: 80, Height: 24}))

	// The drag, and then every remaining step at the width it left behind. The frame
	// immediately after the drag is the one the old code got wrong.
	emit(half, render(r, mid, NewInput(), Viewport{Width: 40, Height: 24}))
	for n := half + 1; n <= len(sc.Steps); n++ {
		st, _ := play(t, firstConversation, n)
		emit(n, render(r, st, NewInput(), Viewport{Width: 40, Height: 24}))
	}
}

// Everything above this point is inline mode, and inline mode has a residual that no amount
// of care in this package removes: a narrowing reflow carries rows of our paint out of the
// top of the screen into the user's history, where no erase of ours can reach them, and a
// widening one joins them back together and drags them into view. That is the report — "se
// logra recuperar conversacion desordenada" — and *recuperar* is the word that settles it.
// Those rows were not redrawn wrong. They were recovered, by the terminal, out of a place a
// program is not allowed to write to. The only sequences that reach them are the ones this
// project bans by name, so the answer is not a better inline paint.
//
// The three tests below are the answer that was taken instead: the alternate buffer, which
// is the surface an interactive run claims now. It has no scrollback to spill into and it
// does not reflow, and the main screen sits parked behind it untouched until the transcript
// is printed there at the end. Each test states one of those: every frame is the whole
// screen, a corner drag cannot recover anything, and the user's own paper is written exactly
// once, at the end, with everything.

// altViewport is the viewport app.sync builds for the alternate buffer, and it belongs here
// rather than beside the other viewport helpers on purpose: Render is mode-blind by design,
// so no render test may name a mode. What crosses that seam is capabilities, and these are
// the capabilities the alternate buffer happens to have — a top band that stays put, and
// side panels once there is width for them.
func altViewport(w, h int) Viewport {
	return Viewport{Width: w, Height: h, FixedTop: true, SidePanels: w >= 100}
}

// theirPrompt is the shell line the session was started from: the last thing on the user's
// screen before the interface takes over, and the thing every one of these tests checks is
// still there afterwards. It is short on purpose — narrower than the narrowest width any
// drag below uses — because a terminal reflows the parked main screen too, and a prompt that
// re-wrapped at 44 columns would show up as a change to the user's paper that we did not make.
const theirPrompt = "$ arxi-sim play 06-lines-changed.ndjson"

// TestAltEmitRebuildsEveryFrame is TestInlineEmitRebuildsEveryFrame on the other surface,
// step by step through a whole recording, and it can make one assertion the inline test
// cannot make at all: the user's own screen does not change. Inline mode's very first frame
// scrolls part of their screen into their history and there is no version of it that does
// not; here their screen is parked, and every mid-session frame has to leave it exactly as
// it was found.
//
// The scrollback assertion is the second half of the same idea, aimed at us rather than at
// the terminal. The alternate buffer has no history, so a row that leaves the top of it is
// gone — not archived, gone. A paint that walks with newlines instead of addressing rows
// absolutely would push the top of the conversation out through that hole once per frame.
func TestAltEmitRebuildsEveryFrame(t *testing.T) {
	vp := altViewport(72, 16)
	_, sc := play(t, firstConversation, 0)
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeAlt, Profile: ProfileTrueColor}
	screen := newReflowVT(vp.Width, vp.Height)
	screen.feed(t, []byte(theirPrompt+"\r\n"))
	before := strings.Join(screen.MainPaper(), "\n")
	screen.feed(t, e.Enter())

	r := NewRenderer()
	for n := 0; n <= len(sc.Steps); n++ {
		st, _ := play(t, firstConversation, n)
		in := NewInput()
		if n%4 == 0 {
			// A prompt long enough to wrap, because a frame whose rows are all short says
			// nothing about a surface whose whole subject is what happens at the margin.
			in.SetText(strings.Repeat("a question long enough to need more than one row ", 3))
		}
		f := render(r, st, in, vp)
		if len(f.Live) != f.Height {
			t.Fatalf("after %d steps the frame is %d rows on a %d-row screen; whatever it does not cover is a row of ours that nothing will repaint", n, len(f.Live), f.Height)
		}
		if len(f.Committed) != 0 {
			t.Fatalf("after %d steps a mid-session frame handed over %d rows, and a handover is the one write that reaches the user's screen", n, len(f.Committed))
		}
		screen.feed(t, e.Emit(f))
		if want, got := strings.Join(texts(f.Live), "\n"), screen.Screen(); got != want {
			t.Fatalf("after %d steps the screen is not the frame\n--- screen ---\n%s\n--- frame ---\n%s", n, got, want)
		}
		if sb := screen.Scrollback(); len(sb) != 0 {
			t.Fatalf("after %d steps, %d rows had left the top of the alternate buffer, where nothing can address them again, starting with %q", n, len(sb), sb[0])
		}
		if got := strings.Join(screen.MainPaper(), "\n"); got != before {
			t.Fatalf("after %d steps the user's own screen had changed under the interface\n--- was ---\n%s\n--- now ---\n%s", n, before, got)
		}
	}
}

// TestAnAltCornerDragCannotReachTheUsersScreen is the same slow corner drag as the inline
// test above, over the same recording, at the same six geometries — and it asserts the two
// things that test had to leave out.
//
// The first is scrollback, which inline mode documents as a residual it cannot close: a
// narrowing pushes rows off the top by definition and no erase of ours reaches them. Here it
// is not a residual but an invariant, because there is nowhere for a row to go. The second is
// the user's own paper, which stays exactly as the shell left it through the whole drag.
//
// Between them they are the report. A row that never leaves the screen cannot be recovered
// into view by a widening reflow, and a buffer that joins nothing has nothing to recover
// anyway — so the duplicates have no mechanism left. What remains true of both surfaces, and
// is asserted the same way in both, is that every frame is the whole screen at the geometry
// it was laid out for: the fix is still a repaint and not an arithmetic correction.
func TestAnAltCornerDragCannotReachTheUsersScreen(t *testing.T) {
	const bar = "ask anything, or / for commands"
	st, _ := play(t, linesChanged, -1)
	r := NewRenderer()
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeAlt, Profile: ProfileTrueColor}
	screen := newReflowVT(100, 24)
	screen.feed(t, []byte(theirPrompt+"\r\n"))
	before := strings.Join(screen.MainPaper(), "\n")
	screen.feed(t, e.Enter())

	paper := func(where string) {
		t.Helper()
		if sb := screen.Scrollback(); len(sb) != 0 {
			t.Fatalf("%s, %d rows had left the top of the alternate buffer, which keeps no history: %q", where, len(sb), sb[0])
		}
		if got := strings.Join(screen.MainPaper(), "\n"); got != before {
			t.Fatalf("%s the user's own screen was no longer theirs\n--- was ---\n%s\n--- now ---\n%s", where, before, got)
		}
	}

	for _, size := range [][2]int{{100, 24}, {80, 32}, {60, 16}, {44, 40}, {72, 12}, {100, 28}} {
		w, h := size[0], size[1]
		screen.Resize(w, h)
		f := render(r, st, NewInput(), altViewport(w, h))
		if len(f.Live) != f.Height {
			t.Fatalf("at %dx%d the frame is %d rows, so it is not the screen and the comparison below means nothing", w, h, len(f.Live))
		}
		screen.feed(t, e.Emit(f))

		want := strings.Join(texts(f.Live), "\n")
		if got := screen.Screen(); got != want {
			t.Fatalf("at %dx%d the screen is not the live region\n--- screen ---\n%s\n--- frame ---\n%s", w, h, got, want)
		}
		if got := strings.Count(screen.Screen(), bar); got != 1 {
			t.Fatalf("at %dx%d the input bar is on screen %d times, want 1\n--- screen ---\n%s", w, h, got, screen.Screen())
		}
		paper(fmt.Sprintf("at %dx%d", w, h))
	}

	// And the stale frame, which is the case app.sync narrows and cannot close: laid out
	// after the last ioctl, written after the geometry moved again. On the main screen that
	// was the dangerous one, because a row wrapped past the bottom margin scrolls into
	// history. Here the same bytes — CUP clamped at the margin, DECAWM off around the paint —
	// have nothing to scroll into, so the worst a stale frame can do is look wrong until the
	// next one lands.
	stale := render(r, st, NewInput(), altViewport(100, 28))
	screen.Resize(44, 20)
	screen.feed(t, e.Emit(stale))
	paper("after a frame laid out for 100x28 was written to a 44x20 terminal,")

	f := render(r, st, NewInput(), altViewport(44, 20))
	screen.feed(t, e.Emit(f))
	if want, got := strings.Join(texts(f.Live), "\n"), screen.Screen(); got != want {
		t.Fatalf("the frame after a stale one did not recover the screen\n--- screen ---\n%s\n--- frame ---\n%s", got, want)
	}
	paper("after the repair,")
}

// TestTheTranscriptReachesTheMainScreenOnceAtSessionEnd is the price of the alternate
// buffer and the test that was owed for it. That buffer is thrown away when it is given
// back, so an interface drawn there and nothing else leaves the user with no conversation at
// all — and that was not hypothetical: Emit sent every alt frame to the paint, the handover
// was unreachable, and -alt discarded the whole transcript at exit. The suite was green.
//
// What has to happen instead is one write to their own screen at the end: give the buffer
// back, which restores the caret the shell was at, and print the transcript from there so the
// terminal scrolls it into their history the way it scrolls any command's output. Which is
// the shape the user asked for — "al finalizar la sesion se imprimen" — arrived at from the
// other direction.
//
// The assertion is an equality over the whole of their paper rather than a search through it,
// because on this surface that is available: their prompt, then the transcript in order, then
// the row the next prompt will be drawn on. Nothing before it, nothing between, nothing twice.
func TestTheTranscriptReachesTheMainScreenOnceAtSessionEnd(t *testing.T) {
	const width, height = 72, 16
	st, _ := play(t, linesChanged, -1)
	r := NewRenderer()
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeAlt, Profile: ProfileTrueColor}
	screen := newReflowVT(100, 24)
	screen.feed(t, []byte(theirPrompt+"\r\n"))
	before := strings.Join(screen.MainPaper(), "\n")
	screen.feed(t, e.Enter())

	// A session with a drag in it, because the width the transcript is wrapped to is the
	// width the session ended on, and that is only true if the last geometry is the one that
	// counts. None of these frames may reach the paper.
	for _, size := range [][2]int{{100, 24}, {60, 20}, {width, height}} {
		screen.Resize(size[0], size[1])
		screen.feed(t, e.Emit(render(r, st, NewInput(), altViewport(size[0], size[1]))))
		if got := strings.Join(screen.MainPaper(), "\n"); got != before {
			t.Fatalf("at %dx%d a mid-session frame wrote on the user's own screen\n--- was ---\n%s\n--- now ---\n%s", size[0], size[1], before, got)
		}
	}

	// Session end, which is settle(): no widgets, no input, no height. Height is the signal —
	// a frame with none is a document, and a document holds nothing back.
	r.Widgets = nil
	final := r.Render(st, nil, Viewport{Width: width})
	if len(final.Live) != 0 {
		t.Fatalf("the handover frame kept %d rows live, so the session ends still owning part of a screen that is about to be thrown away", len(final.Live))
	}
	if len(final.Committed) == 0 {
		t.Fatal("the handover frame committed nothing, so there is no transcript for it to hand over")
	}
	if bad := final.Overflow(); len(bad) != 0 {
		t.Fatalf("rows %v of the document are wider than the %d columns it was wrapped to, so the terminal will wrap them itself and the comparison below is against the wrong shape", bad, width)
	}
	screen.feed(t, e.Emit(final))

	want := append([]string{theirPrompt}, texts(final.Committed)...)
	want = append(want, "") // the row the shell will draw its next prompt on
	if got := screen.MainPaper(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the user's screen at the end is not their prompt followed by the transcript%s", rowDiff(want, got))
	}

	// Once. A second handover — a caller that folds twice, Exit arriving after settle — finds
	// the high-water mark already past the end of the document and writes nothing more.
	paper := screen.MainPaper()
	screen.feed(t, e.Emit(final))
	if got := screen.MainPaper(); strings.Join(got, "\n") != strings.Join(paper, "\n") {
		t.Fatalf("handing the same document over a second time printed it again%s", rowDiff(paper, got))
	}

	// And Exit, whose one job here is not to send ?1049l again. The buffer has already been
	// given back; the position a second one restores is the shell prompt above everything just
	// printed, and the next prompt would be drawn straight through the transcript. The model
	// terminal fails the test on it by name, so this line is the assertion.
	screen.feed(t, e.Exit())
	if got := screen.MainPaper(); strings.Join(got, "\n") != strings.Join(paper, "\n") {
		t.Fatalf("Exit changed the user's screen after the handover was finished with it%s", rowDiff(paper, got))
	}
}

// rowDiff names the first row two papers disagree on. A sixty-row transcript printed against
// a sixty-row transcript is unreadable side by side, and the first divergence is the whole
// diagnosis: one row late means a prompt was overwritten, one row early means an erase took
// something that was not ours.
func rowDiff(want, got []string) string {
	for i := 0; i < len(want) || i < len(got); i++ {
		w, g := "<past the end>", "<past the end>"
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return fmt.Sprintf("\nrow %d of %d (want %d rows)\n--- want ---\n%q\n--- got ---\n%q", i, len(got), len(want), w, g)
		}
	}
	return ""
}

// The three tests that follow are the diff seen from the byte stream, and they play the
// recording that has one because the first conversation contains no edit at all. A diff
// is the first thing this interface draws whose rows mean something past the end of
// their text: a changed row is padded to the frame's edge with blanks that carry the
// band, so those blanks are content, and every layer below the renderer has to treat
// them as content.

// TestInlineEmitKeepsADiffBandWhole is TestInlineEmitRebuildsEveryFrame played over the
// edit. The comparison includes the pad, because texts() does not trim and neither does the
// frame, and a trailing-blank optimisation anywhere in the emit path — the kind every other
// line in the transcript would welcome — turns the band back into the length of the text,
// which is the one thing a band must never be.
func TestInlineEmitKeepsADiffBandWhole(t *testing.T) {
	const width = 72
	vp := testViewport(width)
	_, sc := play(t, linesChanged, 0)
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Profile: ProfileTrueColor}
	screen := newScreenVT(vp.Height)
	screen.feed(t, e.Enter())
	banded := 0
	for n := 0; n <= len(sc.Steps); n++ {
		st, _ := play(t, linesChanged, n)
		f := NewRenderer().Render(st, NewInput(), vp)
		screen.feed(t, e.Emit(f))
		if got, want := screen.Screen(), strings.Join(texts(f.Live), "\n"); got != want {
			t.Fatalf("after %d steps the screen and the frame disagree\n--- screen ---\n%s\n--- frame ---\n%s", n, got, want)
		}
		banded += washedRows(f, width)
	}
	if banded == 0 {
		t.Fatal("no row was padded to the frame's edge on a band, so this proves nothing")
	}
}

// TestAWashedRowIsEmittedAsInk is the claim the model terminal structurally cannot
// make: vt discards every SGR sequence, so a band that arrived as ordinary blanks round
// trips perfectly through the test above and is invisible on a real screen — and worse,
// it is then exactly what endsInBareSpace calls a bug, a trailing blank that fills the
// last cell of a row and means nothing. So the pad is checked against the bytes: the
// band resolved, the span's own style over it, and a reset behind it, because a
// background left open bleeds along the row the moment the terminal scrolls.
func TestAWashedRowIsEmittedAsInk(t *testing.T) {
	const width = 72
	st, _ := play(t, linesChanged, -1)
	th := DefaultTheme()
	f := NewRenderer().Render(st, NewInput(), testViewport(width))
	e := &Emitter{Theme: th, Mode: ModeInline, Profile: ProfileTrueColor}
	e.Enter()
	out := string(e.Emit(f))
	rows := 0
	for _, l := range append(append([]Line{}, f.Committed...), f.Live...) {
		pad, ok := washedPad(l, width)
		if !ok {
			continue
		}
		rows++
		band := th.Resolve(pad.Style).Over(th.Resolve(pad.Fill))
		if band.BG.Kind == ColorNone {
			t.Fatalf("the pad of %q resolves to no background, so it is not a band at all", l.Text())
		}
		if want := e.sgr(band) + pad.Text + ansi.ResetStyle; !strings.Contains(out, want) {
			t.Errorf("the band of %q never reached the terminal as ink: %q is not in the stream", l.Text(), want)
		}
	}
	if rows == 0 {
		t.Fatal("the frame has no row padded to the edge, so this proves nothing")
	}
}

// TestNoRowIsErasedAfterItIsPainted pins the rule emit.go states in a comment and no
// test made until there was a diff to make it with: on a row that exactly fills the
// width, deferred wrap leaves the cursor on the last column instead of moving it, so an
// erase-to-end sent *after* the text deletes the cell just painted. Every other line in
// this interface is short enough that the bug is invisible; a band pads every changed
// row to the frame's edge, so with a diff on screen it is constant. Both surfaces are
// checked, from the same frame: the paint is shared, so a fix that only held on one of them
// would be a coincidence rather than a fix, and this is the cheapest place to say so.
func TestNoRowIsErasedAfterItIsPainted(t *testing.T) {
	const width = 72
	st, _ := play(t, linesChanged, -1)
	for _, tc := range []struct {
		name string
		mode Mode
		vp   Viewport
	}{
		{"inline", ModeInline, testViewport(width)},
		{"alt", ModeAlt, altViewport(width, 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewRenderer().Render(st, NewInput(), tc.vp)
			if washedRows(f, width) == 0 {
				t.Fatal("the frame has no row padded to the edge, so this proves nothing")
			}
			e := &Emitter{Theme: DefaultTheme(), Mode: tc.mode, Profile: ProfileTrueColor}
			out := string(e.Enter()) + string(e.Emit(f))
			if painted, bad := eraseAfterText(out); bad {
				t.Errorf("an erase was sent after %q was painted, which deletes its last cell", painted)
			}
		})
	}
}

// TestSGRDowngrade pins the colour ladder. The interesting row is ProfileANSI: a
// theme written in hex has to still be legible in a 16-colour tty, which is the
// terminal a phone and an old ssh session actually give you.
func TestSGRDowngrade(t *testing.T) {
	cases := []struct {
		name    string
		style   Style
		profile Profile
		want    string
	}{
		{"indexed stays legacy", Style{FG: Idx(Cyan)}, ProfileTrueColor, "\x1b[36m"},
		{"bright indexed", Style{FG: Idx(Bright + Black)}, ProfileTrueColor, "\x1b[90m"},
		{"attrs before colour", Style{FG: Idx(Red), Attrs: AttrBold}, ProfileTrueColor, "\x1b[1;31m"},
		{"rgb truecolor", Style{FG: MustHex("#7aa2f7")}, ProfileTrueColor, "\x1b[38;2;122;162;247m"},
		{"rgb to 256", Style{FG: MustHex("#7aa2f7")}, Profile256, "\x1b[38;5;111m"},
		{"rgb to 16", Style{FG: MustHex("#7aa2f7")}, ProfileANSI, "\x1b[94m"},
		{"index 256 to 16", Style{FG: Idx(196)}, ProfileANSI, "\x1b[91m"},
		{"mono keeps attrs", Style{FG: MustHex("#7aa2f7"), Attrs: AttrBold}, ProfileMono, "\x1b[1m"},
		{"background", Style{BG: Idx(Blue)}, ProfileTrueColor, "\x1b[44m"},
		{"grey to the ramp", Style{FG: MustHex("#808080")}, Profile256, "\x1b[38;5;244m"},
	}
	for _, c := range cases {
		e := &Emitter{Profile: c.profile}
		if got := e.sgr(c.style); got != c.want {
			t.Errorf("%s: sgr = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestMonoEmitsNoColour is the promise a Profile makes, checked over the real
// theme rather than over one style: NO_COLOR means no colour, including the colour
// hidden in a 256-index that happens to be spelled without a semicolon.
func TestMonoEmitsNoColour(t *testing.T) {
	st, _ := play(t, firstConversation, -1)
	f := NewRenderer().Render(st, NewInput(), testViewport(72))
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Profile: ProfileMono}
	out := string(e.Emit(f))
	for _, sgr := range sgrParams(out) {
		switch sgr {
		case "", "1", "2", "3", "4", "7", "9":
		default:
			t.Errorf("mono profile emitted the SGR parameter %q", sgr)
		}
	}
}

// TestTheMouseIsTheReadersUnlessAskedFor is the byte half of the decision that gives the drag
// back to the terminal. Three things want the mouse and only two can have it: tracking makes a
// notch arrive as a report and takes drag-to-select away, releasing the mouse gives the drag
// back, and alternate scroll (1007) would then rewrite a notch into the arrow keys the input's
// history is bound to. So the default releases the mouse and switches 1007 off, -mouse claims
// tracking and leaves 1007 alone, and Exit undoes whichever of the two was done and not both.
func TestTheMouseIsTheReadersUnlessAskedFor(t *testing.T) {
	// Written out rather than asked of ansi.SetMode: this is the test that says what the
	// terminal actually receives, and an expectation computed from the code under test would
	// only prove the emitter agrees with itself.
	const (
		altBuffer = "\x1b[?1049h"
		tracking  = "\x1b[?1002h\x1b[?1006h"
		untrack   = "\x1b[?1006l\x1b[?1002l"
		scrollOff = "\x1b[?1007l"
		scrollOn  = "\x1b[?1007h"
	)

	// The zero value is the arrangement a reader gets, because every caller builds this struct
	// as a literal: no mouse mode is asked for, so a press stays the terminal's and a plain drag
	// selects and copies the way it does in every other program.
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeAlt}
	out := string(e.Enter())
	for _, mode := range []string{"?1000", "?1002", "?1003", "?1006", "?1015"} {
		if strings.Contains(out, mode) {
			t.Fatalf("Enter claimed %s with Mouse unset: %q — a claimed mouse is a drag that needs shift held, which is the one thing this default exists to avoid", mode, out)
		}
	}
	// And the one mode that has to be switched off rather than left alone: leaving 1007 on does
	// not give the run a wheel, it gives the input's history arrow keys nobody pressed.
	if !strings.Contains(out, scrollOff) {
		t.Fatalf("Enter left alternate scroll on: %q — a notch would arrive as up or down, so the wheel would walk the input's history instead of the conversation", out)
	}
	// Order, because 1007 is a fact about the alternate buffer: switching it off before the
	// buffer has been taken is a statement about a screen that does not exist yet.
	if i, j := strings.Index(out, altBuffer), strings.Index(out, scrollOff); i > j {
		t.Fatalf("alternate scroll was switched off before the buffer it applies to was taken: %q", out)
	}
	// Exit puts 1007 back, because Enter is what took it. There is no way to read the old value,
	// so on is the answer chosen: the default nearly every terminal ships, and the one that
	// leaves a wheel in the next less or vim this reader runs.
	back := string(e.Exit())
	if !strings.Contains(back, scrollOn) {
		t.Fatalf("Exit left alternate scroll off: %q — the next program on this terminal would lose its wheel to a mode we switched off and never restored", back)
	}
	if strings.Contains(back, untrack) {
		t.Fatalf("Exit gave back a mouse it never took: %q — releasing a mode nobody claimed says we and the terminal disagree about who has the press", back)
	}

	// -mouse takes the other side of the trade: tracking is claimed, the wheel arrives as a
	// report, and the reader holds shift to select. That arrangement is the whole of what the
	// flag buys, so it is asserted as bytes and not as a bool.
	m := &Emitter{Theme: DefaultTheme(), Mode: ModeAlt, Mouse: true}
	out = string(m.Enter())
	if !strings.Contains(out, tracking) {
		t.Fatalf("Enter claimed no mouse with Mouse set: %q — the flag exists to buy the wheel, and a terminal that was never asked sends no report", out)
	}
	// Button events and not any-motion: 1003 reports every bare move across the pane, which is
	// a stream of reports a notch does not need and a decoder has to throw away.
	if strings.Contains(out, "?1003") {
		t.Fatalf("Enter asked for any-motion tracking: %q", out)
	}
	// And 1007 is not touched here, in either direction. Tracking already suppresses the
	// translation, so switching it off would be refusing something nobody offered, and switching
	// it on would ask for the arrows the wheel report replaces.
	if strings.Contains(out, "?1007") {
		t.Fatalf("Enter touched alternate scroll while claiming the mouse: %q — tracking already suppresses it, so this is a mode changed for no reason and restored on a guess", out)
	}
	// Order, because tracking is a statement about the surface: a terminal asked to report the
	// mouse before it has been given the buffer being reported on is being told about a screen
	// that does not exist yet.
	if i, j := strings.Index(out, altBuffer), strings.Index(out, tracking); i > j {
		t.Fatalf("tracking was claimed before the alternate buffer was: %q", out)
	}

	// Exit gives the mouse back, innermost mode first, and still says nothing about 1007: Enter
	// left it alone under -mouse, so there is nothing here to undo.
	back = string(m.Exit())
	if !strings.Contains(back, untrack) {
		t.Fatalf("Exit left tracking for whoever runs next: %q — a shell whose every click is swallowed as a report is the worst thing this can hand back", back)
	}
	if strings.Contains(back, "?1007") {
		t.Fatalf("Exit changed alternate scroll after -mouse left it alone: %q — undoing what was never done is how a mode ends up on the wrong setting", back)
	}

	// Inline mode is the shell's surface and not ours. The wheel there is the terminal's own
	// scrollback and moving it is the terminal's job, so nothing is claimed, 1007 is left where
	// the terminal had it, and the selection needs no modifier. -mouse does not change that: the
	// flag buys reports on a surface we own, and inline is not one, so both values of it are
	// checked here rather than one of them being assumed to follow.
	for _, mouse := range []bool{false, true} {
		in := &Emitter{Theme: DefaultTheme(), Mode: ModeInline, Mouse: mouse}
		out := string(in.Enter())
		for _, mode := range []string{"?1000", "?1002", "?1003", "?1006", "?1015", "?1007"} {
			if strings.Contains(out, mode) {
				t.Fatalf("inline Enter touched %s with Mouse=%v: %q — the scrollback and the press belong to the shell on this surface", mode, mouse, out)
			}
		}
	}
}

// TestTheKeyboardIsAskedToDisambiguate is the byte half of the second line in the input.
// shift+enter, ctrl+enter and enter are all 0x0d in the encoding a terminal ships with, so the
// keymap's newline binding is unreachable unless this request goes out and the terminal grants
// it. What has to be true is that it goes out at all, that it asks for one flag and not the
// set, that it goes out in both modes because input belongs to the reader rather than to a
// surface, and that the entry comes back off the stack exactly once.
func TestTheKeyboardIsAskedToDisambiguate(t *testing.T) {
	// Written out rather than asked of ansi: the point of this test is what the terminal
	// receives, and an expectation computed from the code under test proves only that the
	// emitter agrees with itself.
	const (
		push = "\x1b[>1u"
		pop  = "\x1b[<1u"
	)

	for _, mode := range []Mode{ModeAlt, ModeInline} {
		e := &Emitter{Theme: DefaultTheme(), Mode: mode}
		out := string(e.Enter())
		// The flag is spelled into the wanted bytes, which is what makes this an assertion about
		// the set and not only about the request: any other value spells a different string. One
		// flag and not the set matters because the second is release events, and a decoder that
		// does not filter them fires every binding twice — one keypress quits, or sends the line
		// twice, or scrolls two notches.
		if !strings.Contains(out, push) {
			t.Fatalf("mode %d asked for no keyboard enhancement, or asked for more than the disambiguate flag: %q — without it shift+enter is the byte enter is, and the newline binding is a line of the keymap no keypress can reach", mode, out)
		}
		back := string(e.Exit())
		if !strings.Contains(back, pop) {
			t.Fatalf("mode %d left our entry on the keyboard stack: %q — the shell that comes next would read every arrow key as a sequence it does not know", mode, back)
		}
		// The keyboard is handed back before the screen is: a prompt drawn on the main screen
		// under a keyboard still in CSI u is a prompt whose arrows are broken.
		if i, j := strings.Index(back, pop), strings.Index(back, "\x1b[?1049l"); j >= 0 && i > j {
			t.Fatalf("mode %d gave the screen back before the keyboard: %q", mode, back)
		}
		// Exit runs from a defer and from the signal handler, so it runs twice. A pop is not an
		// off switch, it is a step down a stack shared with whatever launched us, and a second
		// one takes an entry that is not ours.
		if again := string(e.Exit()); strings.Contains(again, pop) {
			t.Fatalf("mode %d popped the keyboard stack twice: %q — the second pop is somebody else's entry", mode, again)
		}
		// Nothing pushed, nothing to pop: a fold, a pipe, a caller that emitted without entering.
		if fresh := (&Emitter{Theme: DefaultTheme(), Mode: mode}); strings.Contains(string(fresh.Exit()), pop) {
			t.Fatalf("mode %d popped an entry it never pushed", mode)
		}
	}

	// And it moves no ink. These bytes go out in the same breath as the frame, in front of a
	// conversation somebody is about to read, so a parser that mistook one for an erase or a
	// scroll would leave a hole in the transcript rather than a missing key.
	vp := altViewport(72, 16)
	st, _ := play(t, firstConversation, -1)
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeAlt, Profile: ProfileTrueColor}
	screen := newReflowVT(vp.Width, vp.Height)
	screen.feed(t, e.Enter())
	screen.feed(t, e.Emit(render(NewRenderer(), st, NewInput(), vp)))
	before := screen.Screen()
	for _, seq := range []string{push, pop} {
		screen.feed(t, []byte(seq))
		if got := screen.Screen(); got != before {
			t.Fatalf("%q moved ink on a painted screen:\n%s", seq, got)
		}
	}
}

// TestTheMouseModesMoveNoInk is the other half of the mouse decision. Every one of these modes
// goes out in the same write that claims the alternate buffer, in front of a conversation
// somebody is about to read, so they have to be inert on the surface: a mode misparsed as an
// erase or a scroll shows up as a hole in the transcript rather than as a missing wheel. It is
// checked against the model terminal because that is the only thing here that knows where ink
// lands.
func TestTheMouseModesMoveNoInk(t *testing.T) {
	vp := altViewport(72, 16)
	st, _ := play(t, firstConversation, -1)
	e := &Emitter{Theme: DefaultTheme(), Mode: ModeAlt, Profile: ProfileTrueColor}
	screen := newReflowVT(vp.Width, vp.Height)
	screen.feed(t, []byte(theirPrompt+"\r\n"))
	paper := strings.Join(screen.MainPaper(), "\n")
	screen.feed(t, e.Enter())
	screen.feed(t, e.Emit(render(NewRenderer(), st, NewInput(), vp)))
	before := screen.Screen()

	// Every mode either Enter or Exit can send, on either side of -mouse, fed again onto a painted
	// screen. Tracking is a subscription and alternate scroll is a setting, so re-sending either
	// is allowed, and that is what separates the modes from the frame that carried them the first
	// time. 1007 is in the list because it is the one the default run sends, which makes it the
	// one a reader who never touches the flag would see any damage from.
	for _, mode := range []string{"\x1b[?1002h\x1b[?1006h", "\x1b[?1006l\x1b[?1002l", "\x1b[?1007l", "\x1b[?1007h"} {
		screen.feed(t, []byte(mode))
		if got := screen.Screen(); got != before {
			t.Fatalf("%q changed the screen\n--- was ---\n%s\n--- now ---\n%s", mode, before, got)
		}
		if sb := screen.Scrollback(); len(sb) != 0 {
			t.Fatalf("%q pushed %d rows out of the alternate buffer, starting with %q", mode, len(sb), sb[0])
		}
		if got := strings.Join(screen.MainPaper(), "\n"); got != paper {
			t.Fatalf("%q reached the user's own screen\n--- was ---\n%s\n--- now ---\n%s", mode, paper, got)
		}
	}
}

// sgrParams pulls every parameter out of every SGR sequence in a stream.
func sgrParams(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return out
		}
		s = s[i+2:]
		j := strings.IndexAny(s, "@ABCDEFGHJKSTfhilmnst")
		if j < 0 {
			return out
		}
		if s[j] == 'm' {
			out = append(out, strings.Split(s[:j], ";")...)
		}
		s = s[j+1:]
	}
}

// visible strips the escapes out of a stream, leaving what the terminal would have
// painted. It exists because writeLine wraps every styled span in its own SGR pair, so
// a line of the frame is a contiguous string only once the escapes are gone.
func visible(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "\x1b")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		s = s[i:]
		if !strings.HasPrefix(s, "\x1b[") {
			s = s[1:]
			continue
		}
		j := 2
		for j < len(s) && (s[j] < '@' || s[j] > '~') {
			j++
		}
		if j == len(s) {
			return b.String()
		}
		s = s[j+1:]
	}
}

// firstCSI reports where the first CSI sequence with the given final byte begins, or
// -1. It parses instead of searching for a literal, because the parameters that reach
// these sequences depend on the frame.
func firstCSI(s string, final byte) int {
	for i := 0; ; {
		j := strings.Index(s[i:], "\x1b[")
		if j < 0 {
			return -1
		}
		k := i + j + 2
		for k < len(s) && (s[k] < '@' || s[k] > '~') {
			k++
		}
		if k == len(s) {
			return -1
		}
		if s[k] == final {
			return i + j
		}
		i = k + 1
	}
}

// committedProbes picks the committed lines of a frame worth searching a byte stream
// for: long enough not to turn up by accident, and absent from the live region, which
// is redrawn every frame and would otherwise answer for a reprint that never happened.
func committedProbes(f Frame) []string {
	live := map[string]bool{}
	for _, l := range f.Live {
		live[l.Text()] = true
	}
	var out []string
	for _, l := range f.Committed {
		if s := l.Text(); len([]rune(s)) >= 16 && !live[s] {
			out = append(out, s)
		}
	}
	return out
}

// washedPad returns the trailing blank span a banded row ends in. Full width is not the
// selector on its own: a line of code long enough to reach the edge by itself is exactly
// as wide and has no pad at all, so the last non-empty span also has to be blank and
// carry a Fill. Every assertion about a band goes through here, which is what keeps the
// two tests above from quietly passing on a frame that happens to contain no diff.
func washedPad(l Line, width int) (Span, bool) {
	if l.Width() != width {
		return Span{}, false
	}
	for i := len(l) - 1; i >= 0; i-- {
		if l[i].Text == "" {
			continue
		}
		if l[i].Fill == "" || strings.Trim(l[i].Text, " ") != "" {
			return Span{}, false
		}
		return l[i], true
	}
	return Span{}, false
}

// washedRows counts them across a whole frame, committed and live alike.
func washedRows(f Frame, width int) int {
	n := 0
	for _, l := range append(append([]Line{}, f.Committed...), f.Live...) {
		if _, ok := washedPad(l, width); ok {
			n++
		}
	}
	return n
}

// eraseAfterText walks a stream and reports the first row, if any, where an erase
// followed text on the same row. It tracks only what has been painted since the cursor
// last moved to a column it can be sure of: \r and \n start a row over, an absolute
// position does too, and everything else is either text to accumulate or an escape whose
// effect on the text is none. Cursor motion within a row is not modelled because emit
// never sends any — which is itself part of what this checks.
func eraseAfterText(s string) (string, bool) {
	painted := ""
	for s != "" {
		switch {
		case s[0] == '\r', s[0] == '\n':
			painted, s = "", s[1:]
		case strings.HasPrefix(s, "\x1b["):
			j := 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j == len(s) {
				return "", false
			}
			switch s[j] {
			case 'K', 'J':
				if painted != "" {
					return painted, true
				}
			case 'H':
				painted = ""
			}
			s = s[j+1:]
		case s[0] == '\x1b':
			s = s[1:]
		default:
			i := strings.IndexAny(s, "\x1b\r\n")
			if i < 0 {
				return "", false
			}
			painted, s = painted+s[:i], s[i:]
		}
	}
	return "", false
}
