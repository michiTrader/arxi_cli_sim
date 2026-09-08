package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arxi.local/sim/internal/scenario"
	"arxi.local/sim/internal/state"
)

// These tests are the reason the renderer returns a Frame instead of writing to a
// terminal. Everything that matters about a frame — that it fits, that a sealed
// line never moves, that a half-arrived fence still draws as code — is a property
// of a string, checkable without a tty, on any machine, in milliseconds.

var update = flag.Bool("update", false, "rewrite golden files")

const firstConversation = "../../testdata/scenarios/01-first-conversation.ndjson"

// linesChanged is the second recording named here, and the only reason to name one is
// that a test needs a case the first conversation does not contain. This one draws a
// diff, so it is the only scenario whose rows mean anything past the end of their text.
const linesChanged = "../../testdata/scenarios/06-lines-changed.ndjson"

// toolFails is the recording with a tool call that comes back non-zero. It is named for
// the same reason: a failing call is drawn with the same glyph as a passing one and
// differs only in its style key, so it is the theme test that needs this one by name.
const toolFails = "../../testdata/scenarios/07-tool-fails.ndjson"

// longOutput is the recording whose tool result is longer than the transcript will
// spend rows on, so it is the only one that draws the elided middle. It holds both
// sides of the rule — a 31-line result that is cut and a 12-line one that is not —
// because the constants are only interesting at their boundary.
const longOutput = "../../testdata/scenarios/09-long-output.ndjson"

// markdownProse is the recording that says a reply is markdown: emphasis, a link and
// the url it goes to, a quote inside a quote. It is named because those are style keys
// and nothing else in the corpus draws them, and because it draws them the way a
// transcript does — through a fold, a block and the renderer — rather than by calling
// RenderMarkdown on a string, which is what stood in for it here before.
const markdownProse = "../../testdata/scenarios/10-markdown.ndjson"

// play folds the first n steps of a scenario into a state. Barriers are skipped:
// they are instructions to the player, not facts about the run.
func play(t *testing.T, path string, n int) (*state.State, *scenario.Scenario) {
	t.Helper()
	sc, err := scenario.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	if errs := scenario.Validate(sc); len(errs) > 0 {
		t.Fatalf("validate %s: %v", path, errs)
	}
	st := state.New()
	for i, step := range sc.Steps {
		if n >= 0 && i >= n {
			break
		}
		if step.Event == nil {
			continue
		}
		st.Apply(step.Event, step.At)
		st.Now = step.At
	}
	return st, sc
}

func testViewport(w int) Viewport { return Viewport{Width: w, Height: 40} }

// shortViewport is a screen the transcript does not fit on. Committing follows from
// geometry now, so at Height 40 the first conversation commits nothing whatsoever —
// every row stays live and is re-laid-out on every frame — and a test that means to
// exercise the committed path has to ask for a screen shorter than the conversation.
func shortViewport(w int) Viewport { return Viewport{Width: w, Height: 12} }

// docViewport is a width and no height, which is how Render is told the caller wants a
// document rather than a screen: nothing is held back, the whole transcript is committed
// and the live region is the chrome alone. Every sweep that means "at every step, at every
// width" wants this one. With a height the frame is capped to a screenful, so a sweep
// there checks the tail and calls it the transcript — a row that overflows two hundred
// rows up is a row that corrupts inline mode just as thoroughly, and Height 40 was quietly
// not looking at it.
func docViewport(w int) Viewport { return Viewport{Width: w} }

// render draws the same composition the player draws: the renderer adds no widgets
// of its own, so a test that wants the chrome has to install it exactly as the app
// does. Going through one helper is what keeps every test here honest about that —
// see ChromeFor for why the renderer no longer does it by itself.
func render(r *Renderer, st *state.State, in *Input, vp Viewport) Frame {
	r.Widgets = ChromeFor(st)
	return r.Render(st, in, vp)
}

// TestFrameNeverOverflows is the width discipline, enforced at every step of the
// scenario and at every width a phone or a split pane might hand us. A single
// overflowing line makes the terminal wrap a row we thought we owned, and every
// cursor position after it is wrong.
func TestFrameNeverOverflows(t *testing.T) {
	_, sc := play(t, firstConversation, 0)
	widths := []int{20, 24, 40, 60, 72, 100, 120}
	for _, w := range widths {
		for n := 0; n <= len(sc.Steps); n++ {
			st, _ := play(t, firstConversation, n)
			in := NewInput()
			if n%3 == 0 {
				in.SetText("a prompt long enough to wrap on a narrow terminal, twice over")
			}
			r := NewRenderer()
			f := render(r, st, in, docViewport(w))
			if bad := f.Overflow(); len(bad) > 0 {
				t.Fatalf("width %d after %d steps: lines %v overflow:\n%s", w, n, bad, f.Plain())
			}
			if f.Width != w {
				t.Fatalf("width %d: frame reports %d", w, f.Width)
			}
		}
	}
}

// TestNothingIsHandedOverMidSession is the inline-mode contract as it now stands, and it
// stands in place of an append-only rule that no longer has anything to be append-only
// about.
//
// Committing a row is a promise never to address it again, and a resize breaks that promise
// for us whether we like it or not: the row re-wraps inside the terminal's history, where
// nothing we write can reach it, and the frame laid out at the new width has no way to know
// how many rows the old one became. That mismatch is what reprinted the tail of the
// transcript on every step of a drag and left it there. So mid-session the promise is never
// made. The frame is the screen — every row of it, so the height below is an equality and
// not a cap — and the transcript reaches the terminal once, at the end, wrapped to the width
// the session ended on, which is the shape the user asked for: "no necesariamente se tiene
// que ir agregando las lineas fijas a la terminal sino que al finalizar la sesion se
// imprimen."
//
// A cap would be the weaker rule and it is not enough. A frame allowed to stop short leaves
// rows inside the screen that no repaint of ours addresses, and those are ours too — we
// printed them on the way down — so a reflow re-wraps them where no erase can follow, which
// is the duplicated history a slow drag produces, one band lower. Both ends therefore have
// to hold: the transcript is trimmed when it is taller than the screen and the screen is
// padded when it is not, and the two counters at the bottom insist the sweep saw each.
//
// The screen is short and the input swings between one row and five, because both of them
// used to move the boundary this test says is gone.
func TestNothingIsHandedOverMidSession(t *testing.T) {
	_, sc := play(t, firstConversation, 0)
	r := NewRenderer()
	trimmed, padded := 0, 0
	for n := 0; n <= len(sc.Steps); n++ {
		st, _ := play(t, firstConversation, n)
		in := NewInput()
		if n%4 == 0 {
			in.SetText(strings.Repeat("a prompt long enough to wrap on a narrow terminal, twice over ", 5))
		}
		vp := shortViewport(72)
		f := render(r, st, in, vp)
		if len(f.Committed) != 0 {
			t.Fatalf("after %d steps a frame with a height handed over %d rows, and a handed-over row is one a drag reflows out of reach", n, len(f.Committed))
		}
		if len(f.Live) != vp.Height {
			t.Fatalf("after %d steps the frame is %d rows on a %d-row screen: short of it leaves rows no repaint reaches, past it scrolls its own top rows into history", n, len(f.Live), vp.Height)
		}
		// The cursor is an index into Live and the emitter parks the caret with it, so a
		// cursor outside the region is a caret parked on somebody else's row.
		if f.Cursor.Line < 0 || f.Cursor.Line >= len(f.Live) {
			t.Fatalf("after %d steps the cursor is on row %d of a %d-row region", n, f.Cursor.Line, len(f.Live))
		}
		// How tall this state wanted to be, which is the one thing a screen-sized frame can
		// no longer report: at no height the whole transcript is committed and the chrome is
		// all that stays live, so the two together are the document. The chrome is the same
		// either way — no slot's availability depends on the height — so the difference
		// against vp.Height is exactly what the frame above had to trim or pad away.
		doc := render(r, st, in, docViewport(vp.Width))
		switch total := len(doc.Committed) + len(doc.Live); {
		case total > vp.Height:
			trimmed++
		case total < vp.Height:
			padded++
		}
	}
	if trimmed == 0 {
		t.Fatal("the transcript never outgrew the screen, so the trim above was never tested")
	}
	if padded == 0 {
		t.Fatal("the transcript outgrew the screen from the first frame, so the padding above was never tested")
	}
}

// scrolledTo is a short screen with the human looking somewhere other than the tail. The
// pair is deliberate — ScrollTop is read only when Scrolled is set — so a test that set the
// offset and forgot the flag would be testing the tail and saying so.
func scrolledTo(w, top int) Viewport {
	vp := shortViewport(w)
	vp.Scrolled, vp.ScrollTop = true, top
	return vp
}

// firstRowDiff says where two runs of rows stop agreeing, in text. The things it compares
// are windows onto a seventy-row conversation, and printing two of those side by side is a
// failure nobody reads.
func firstRowDiff(want, got []Line) string {
	for i := 0; i < len(want) && i < len(got); i++ {
		if want[i].Text() != got[i].Text() {
			return fmt.Sprintf("row %d of the window:\n want %q\n  got %q", i, want[i].Text(), got[i].Text())
		}
	}
	if len(want) != len(got) {
		return fmt.Sprintf("%d rows, want %d", len(got), len(want))
	}
	return ""
}

// The window into the transcript, which is the half of scrolling this layer owns: the app
// decides where the human is looking, and everything about what that means — which rows are
// on screen, what comes back in f.Scroll, what happens when the offset points past the end —
// is here. None of it was tested at all while every screen the suite asked for was one the
// conversation happened to fit on, which is why a player that could not scroll shipped green.
//
// The transcript to compare against is taken from a document frame at the same width, so
// these assertions are made against the renderer's own idea of the conversation rather than
// against a second copy of the wrapping rules. Marker strings would prove less: a window is
// right when it is the correct slice of the transcript, not when some line the test picked
// out is somewhere in it.
func TestScrollWindowsTheTranscript(t *testing.T) {
	_, sc := play(t, linesChanged, 0)
	st, _ := play(t, linesChanged, len(sc.Steps))
	in := NewInput()
	r := NewRenderer()
	const width = 72
	height := shortViewport(width).Height
	rows := render(r, st, in, docViewport(width)).Committed
	if len(rows) <= height {
		t.Fatalf("%s folds to %d rows at width %d and the screen holds %d: there is no window to move here",
			linesChanged, len(rows), width, height)
	}
	midway := len(rows) / 3
	cases := []struct {
		name  string
		vp    Viewport
		first func(keep int) int // the row the window is expected to start on
	}{
		// Following. No offset at all, the tail on screen, nothing below it — the state a
		// session spends most of its life in, and the one the zero value has to mean.
		{"following", shortViewport(width), func(keep int) int { return len(rows) - keep }},
		// The top of the conversation: the row the user could not reach.
		{"at the top", scrolledTo(width, 0), func(int) int { return 0 }},
		// Somewhere in the middle. The offset is a row index into the transcript, which is
		// what makes a page key arithmetic the app can do without knowing how text wraps.
		{"midway", scrolledTo(width, midway), func(int) int { return midway }},
		// Past the end. A page key near the tail asks for exactly this, and the answer is
		// the last window rather than a complaint.
		{"clamped", scrolledTo(width, 10000), func(keep int) int { return len(rows) - keep }},
	}
	for _, c := range cases {
		f := render(r, st, in, c.vp)
		keep := f.Scroll.Rows
		if keep <= 0 {
			t.Fatalf("%s: the chrome left %d rows for the conversation on a %d-row screen", c.name, keep, c.vp.Height)
		}
		want := c.first(keep)
		if f.Scroll.Above != want {
			t.Errorf("%s: the frame was drawn at row %d of the transcript, want %d", c.name, f.Scroll.Above, want)
		}
		if below := len(rows) - want - keep; f.Scroll.Below != below {
			t.Errorf("%s: %d rows reported below the window, want %d", c.name, f.Scroll.Below, below)
		}
		// The window is the top of the live region — no slot sits above it on a viewport
		// without FixedTop — so this is the whole claim: those rows, in that order.
		if want >= 0 && want+keep <= len(rows) {
			if diff := firstRowDiff(rows[want:want+keep], f.Live[:keep]); diff != "" {
				t.Errorf("%s: the window is not the transcript at row %d — %s", c.name, want, diff)
			}
		}
		// And it is still a screen. Windowing is not an excuse to hand a row over or to
		// stop short of the height: the first buries rows a drag can reflow out of reach,
		// the second leaves rows on screen that no repaint of ours addresses.
		if len(f.Committed) != 0 {
			t.Errorf("%s: a frame with a height handed over %d rows", c.name, len(f.Committed))
		}
		if len(f.Live) != c.vp.Height {
			t.Errorf("%s: the frame is %d rows on a %d-row screen", c.name, len(f.Live), c.vp.Height)
		}
	}
}

// panelViewport is a short screen that can hold a column beside the transcript: the shape the app
// builds on the alternate screen from a hundred columns up. It pairs with scrolledTo for the same
// reason scrolledTo pairs the flag with the offset — a reserved column is only worth looking at
// once there is a window for the bar in it to be a fraction of.
func panelViewport(w, top int) Viewport {
	vp := scrolledTo(w, top)
	vp.SidePanels = true
	return vp
}

// withBar draws what the app draws on a screen with side panels. ChromeFor is the transcript's own
// chrome and the scrollbar is not part of it — the app installs the bar itself, unconditionally,
// and leaves the dropping to Place — so this is the one helper here that adds a widget by hand,
// and it adds exactly the one App.chrome adds.
func withBar(r *Renderer, st *state.State, in *Input, vp Viewport) Frame {
	r.Widgets = append(ChromeFor(st), ScrollbarWidget{})
	return r.Render(st, in, vp)
}

// The composition half of the scrollbar, which is the half that can corrupt a terminal. How long
// the thumb is and where it sits belongs to TestTheBarSaysHowMuchAndWhere; the questions here are
// where the ink lands and what the prose gave up for it.
//
// Five claims. The transcript is wrapped to the reduced width and the window is that transcript,
// which is what makes the reservation a width the prose actually spends rather than a subtraction
// nobody collected: the rows are compared against a document folded at TranscriptWidth, so a
// renderer that took the two columns and then wrapped at the full width fails here instead of in a
// golden file six months from now.
//
// The column lands in the columns the prose gave up and nowhere else: every row of the window is
// exactly the screen's width and ends in a bar cell. Both halves matter. Short of the width, the
// bar has been drawn straight after the prose and what runs down the screen is a ragged edge in a
// different place on every row; past it, a line we thought we owned wraps and every cursor position
// under it is wrong. A screen too narrow to give anything up is the same claim at its other end,
// and it is the one where drawing the column anyway would cover the only text there is.
//
// No row ends in a bare space. The composite pads the transcript row out to the wrap point before
// the column goes on, which puts that padding inside the row instead of at the end of it — the
// only reason the trailing-blank rule survives a column at all — and a row the column has nothing
// to say about is left alone rather than padded. A blanked glyph is how that last row is reached,
// since the shipped ones draw on every row of the track.
//
// And a document reserves nothing. A fold keeps SidePanels and only zeroes the height, so every
// golden file at a hundred columns runs through a viewport that says a column can be held; if the
// reservation did not look at the height, all of them would wrap two columns early.
func TestASideColumnCostsTheTranscriptItsWidthAndNothingElse(t *testing.T) {
	_, sc := play(t, linesChanged, 0)
	st, _ := play(t, linesChanged, len(sc.Steps))
	in := NewInput()
	r := NewRenderer()
	const width = 100
	tw := TranscriptWidth(panelViewport(width, 0))
	if tw != width-SideCols {
		t.Fatalf("a %d-column screen holding side panels wraps its transcript at %d, want %d", width, tw, width-SideCols)
	}
	// The other end of the same reservation, which is a drag caught mid-flight: at two columns
	// there is nothing to give up, so the prose keeps the whole width and the column is dropped
	// rather than drawn over the only text on the screen.
	for _, narrow := range []int{1, SideCols} {
		vp := panelViewport(narrow, 0)
		if got := TranscriptWidth(vp); got != narrow {
			t.Errorf("a %d-column screen that says it can hold a column wraps its transcript at %d, want the whole %d", narrow, got, narrow)
		}
		if bad := withBar(r, st, in, vp).Overflow(); len(bad) > 0 {
			t.Errorf("a %d-column screen overflows on lines %v", narrow, bad)
		}
	}
	// The transcript the window is a window onto, folded at the width the screen wraps to rather
	// than at the screen's own width. Those two differ by exactly the reservation, which is the
	// point: at the wrong one of them this comparison would be off by a wrap on every long line.
	rows := withBar(r, st, in, docViewport(tw)).Committed
	if len(rows) <= panelViewport(width, 0).Height {
		t.Fatalf("%s folds to %d rows at width %d and the screen holds %d: there is nothing off screen here, so no bar would be drawn", linesChanged, len(rows), tw, panelViewport(width, 0).Height)
	}
	for _, top := range []int{0, len(rows) / 3, 10000} {
		vp := panelViewport(width, top)
		f := withBar(r, st, in, vp)
		where := fmt.Sprintf("looking at row %d", top)
		if bad := f.Overflow(); len(bad) > 0 {
			t.Fatalf("%s: lines %v overflow a %d-column frame:\n%s", where, bad, f.Width, f.Plain())
		}
		if f.Width != width {
			t.Errorf("%s: the frame reports width %d on a %d-column screen", where, f.Width, width)
		}
		for i, l := range f.Live {
			if endsInBareSpace(l) {
				t.Errorf("%s: row %d ends in an unstyled space: %q", where, i, l.Text())
			}
		}
		first, keep := f.Scroll.Above, f.Scroll.Rows
		if keep <= 0 || first < 0 || first+keep > len(rows) {
			t.Fatalf("%s: the frame reports rows %d..%d of a %d-row transcript", where, first, first+keep, len(rows))
		}
		// The window is the transcript at the reduced width, and then the column. A prefix rather
		// than an equality, because the row is the prose padded out to the wrap point with the two
		// reserved columns after it — and a prefix is still the whole claim about the prose, since
		// anything that moved a character of it sideways breaks it.
		for i := 0; i < keep; i++ {
			want, got := rows[first+i].Text(), f.Live[i].Text()
			if !strings.HasPrefix(got, want) {
				t.Errorf("%s: row %d of the window is not the transcript at row %d —\n want prefix %q\n         got %q", where, i, first+i, want, got)
				continue
			}
			// And the column is a column. The shipped glyphs draw on every row of it, so every row
			// of the window ends in a bar cell in the screen's last column, and both halves of that
			// have to be said: a row that stopped at the prose and put the bar straight after it
			// spends the same two cells somewhere else, and what it draws is a ragged line down the
			// middle of the screen rather than a scrollbar.
			l := f.Live[i]
			if l.Width() != width {
				t.Errorf("%s: row %d of the window is %d columns of a %d-column screen, so whatever it ends in is not in the last one", where, i, l.Width(), width)
				continue
			}
			if s := l[len(l)-1].Style; s != "scroll.track" && s != "scroll.thumb" {
				t.Errorf("%s: row %d of the window ends in %q with %d rows off screen, and the column beside it is the one place that could say so", where, i, s, f.Scroll.Above+f.Scroll.Below)
			}
		}
	}
	// A blanked glyph is this file's half of TestTheBarSurvivesItsOwnGlyphs, and it is here because
	// it is the only way a column row arrives empty — which is the case the composite has to leave
	// the transcript row alone in. Padding a row out to the wrap point and then appending nothing
	// to it is a row ending in unstyled space, and that is the one thing the emitter's erase cannot
	// tell from a row somebody typed trailing spaces into.
	blank, err := NewGlyphs(map[string]string{"scroll.track": ""}, false)
	if err != nil {
		t.Fatalf("blanking the track: %v", err)
	}
	br := NewRenderer()
	br.Glyphs = blank
	// Looking at the top of the log, so the track is all below the thumb and there is some of it.
	f := withBar(br, st, in, panelViewport(width, 0))
	if bad := f.Overflow(); len(bad) > 0 {
		t.Fatalf("with a blanked track, lines %v overflow a %d-column frame:\n%s", bad, f.Width, f.Plain())
	}
	for i, l := range f.Live {
		if endsInBareSpace(l) {
			t.Errorf("with a blanked track, row %d ends in an unstyled space: %q", i, l.Text())
		}
	}
	// The fold, which is where a reservation that ignored the height would show up: the same
	// viewport, still saying it can hold a column, with only the height taken away.
	doc := panelViewport(width, 0)
	doc.Height = 0
	if got := TranscriptWidth(doc); got != width {
		t.Errorf("a document folded from a screen with side panels wraps at %d, want the whole %d", got, width)
	}
	if diff := firstRowDiff(withBar(r, st, in, docViewport(width)).Committed, withBar(r, st, in, doc).Committed); diff != "" {
		t.Errorf("a fold that kept SidePanels is not the document at the same width — %s", diff)
	}
}

// Where the column landed, which is the frame's one answer to a pointer. Everything above is about
// ink; Side is about coordinates, and it is the only thing the app has to turn a mouse report into a
// transcript row with — so a Side a row off, or a column off, or one that survives into a frame with
// no bar in it, is a click that scrolls somewhere nobody pointed at.
//
// The claims are read off the ink rather than off the arithmetic that placed it, because agreeing
// with itself is not the claim. X and W are the reservation read back, so the bar's own cells have to
// end exactly where the two of them say the column ends. Y and H are the window's rows in the
// coordinates of Live, which is what makes row i of a mouse report row i of the frame, and they are
// compared against the rows the bar is actually drawn on — every one of them, contiguously.
//
// And the empty ones, because a frame with no column has no coordinates to offer and there are three
// ways to have none: a screen that never reserved one, a screen with no window left after the chrome,
// and a screen too narrow to give the columns up. The zero Rect is what the app tests for, so a Side
// that is stale or hopeful there is a click landing on a bar nobody drew.
func TestTheFrameSaysWhereTheSideColumnLanded(t *testing.T) {
	_, sc := play(t, linesChanged, 0)
	st, _ := play(t, linesChanged, len(sc.Steps))
	in, r := NewInput(), NewRenderer()
	const width = 100
	for _, top := range []int{0, 3, 40} {
		vp := panelViewport(width, top)
		sideOnTheInk(t, withBar(r, st, in, vp), fmt.Sprintf("a %d-row screen scrolled to %d", vp.Height, top), TranscriptWidth(vp))
	}
	// The same claims with something above the transcript, which is the only way Y is not zero — and it
	// is what the hit test's own subtraction lives or dies on. A held question needs FixedTop to be
	// placed at all, so this is the one frame here that installs its own chrome.
	vp := panelViewport(width, 3)
	vp.FixedTop = true
	r.Widgets = append(ChromeFor(st), HeaderWidget{Text: "the question being held above the transcript", Rows: 1}, ScrollbarWidget{})
	f := r.Render(st, in, vp)
	if f.Side.Y == 0 {
		t.Errorf("a screen holding a question above the transcript starts the column on row 0, so the header is not above it\n%s", f.Plain())
	}
	sideOnTheInk(t, f, "a screen holding a question above the transcript", TranscriptWidth(vp))
	// The frames with no column in them. The document is the same viewport with only the height taken
	// away, so that what it proves is about the window and not about the flag, and the short screen is
	// the case the app's own bounds test leans on: the chrome fits and nothing else does.
	short := panelViewport(width, 0)
	short.Height = 1
	doc := panelViewport(width, 0)
	doc.Height = 0
	for _, c := range []struct {
		name string
		vp   Viewport
	}{
		{"a screen holding no columns beside the transcript", scrolledTo(width, 0)},
		{"a screen with no room for a window", short},
		{"a document", doc},
		{"a screen too narrow to give the columns up", panelViewport(SideCols, 0)},
	} {
		if got := withBar(r, st, in, c.vp).Side; got != (Rect{}) {
			t.Errorf("%s carries Side %+v, and there is no bar in it to point at", c.name, got)
		}
	}
}

// sideOnTheInk holds one frame's Side against the rows the bar was drawn on. x is where the prose was
// told to stop, and the rest is read back from the frame — the run of rows ending in a bar cell, which
// has to be one run, has to end where Side says the column ends, and has to be the rows Side names.
func sideOnTheInk(t *testing.T, f Frame, where string, x int) {
	t.Helper()
	if f.Side.W != SideCols {
		t.Errorf("%s: the column is %d wide and it reserved %d", where, f.Side.W, SideCols)
	}
	if f.Side.X != x {
		t.Errorf("%s: the column starts at %d and the prose stops at %d", where, f.Side.X, x)
	}
	first, last, rows := -1, -1, 0
	for i, l := range f.Live {
		if len(l) == 0 {
			continue
		}
		if s := l[len(l)-1].Style; s != "scroll.track" && s != "scroll.thumb" {
			continue
		}
		if first < 0 {
			first = i
		}
		last, rows = i, rows+1
		if l.Width() != f.Side.X+f.Side.W {
			t.Errorf("%s: row %d ends at column %d and Side ends at %d", where, i, l.Width(), f.Side.X+f.Side.W)
		}
	}
	if first < 0 {
		t.Fatalf("%s: no row of the frame ends in the bar, so there is nothing to be beside\n%s", where, f.Plain())
	}
	if rows != last-first+1 {
		t.Fatalf("%s: the bar is drawn on %d rows between %d and %d, and a column with a gap in it is not a column", where, rows, first, last)
	}
	if f.Side.Y != first || f.Side.H != rows {
		t.Errorf("%s: Side says rows %d..%d and the bar is on %d..%d", where, f.Side.Y, f.Side.Y+f.Side.H-1, first, last)
	}
}

// A scrolled view holds still while the conversation grows underneath it. This is the whole
// reason the offset counts from the top of the transcript: rows arriving at the end change
// how far away the tail is, so an offset measured from the bottom would have to be corrected
// on every event just to keep the same text on screen — and every event that forgot would
// yank the reader somewhere they did not ask to be. Nothing here corrects anything, and that
// is the assertion.
func TestAScrolledViewHoldsStillAsRowsArrive(t *testing.T) {
	_, sc := play(t, linesChanged, 0)
	half := len(sc.Steps) / 2
	st, _ := play(t, linesChanged, half)
	in := NewInput()
	r := NewRenderer()
	vp := scrolledTo(72, 3)
	// Where to start: far enough in that the conversation already does not fit, because a
	// reader can only be somewhere other than the tail once there is something above the
	// window. It is measured rather than counted in steps — a step is not a row, and most of
	// this recording's steps are deltas that land in the middle of a line — so half the log
	// is half the events and, at this width, not yet half a screen.
	start := -1
	for n := half; n <= len(sc.Steps); n++ {
		st, _ = play(t, linesChanged, n)
		if len(render(r, st, in, docViewport(vp.Width)).Committed) >= 2*vp.Height {
			start = n
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s never folds to %d rows at width %d, so there is never a window to hold still",
			linesChanged, 2*vp.Height, vp.Width)
	}
	before := render(r, st, in, vp)
	if before.Scroll.Above != 3 || before.Scroll.Below <= 0 {
		t.Fatalf("%s after %d steps draws %d rows above and %d below a %d-row window, so nobody is scrolled here and holding still proves nothing",
			linesChanged, start, before.Scroll.Above, before.Scroll.Below, before.Scroll.Rows)
	}
	// The rest of the recording, applied exactly the way the player applies it.
	for _, step := range sc.Steps[start:] {
		if step.Event == nil {
			continue
		}
		st.Apply(step.Event, step.At)
		st.Now = step.At
	}
	after := render(r, st, in, vp)
	if after.Scroll.Above != before.Scroll.Above {
		t.Errorf("the rest of the conversation moved the window from row %d to row %d", before.Scroll.Above, after.Scroll.Above)
	}
	if after.Scroll.Below <= before.Scroll.Below {
		t.Fatalf("%d rows below the window before the conversation continued and %d after: it did not grow, so this test is watching nothing",
			before.Scroll.Below, after.Scroll.Below)
	}
	// The same rows, read row for row. The two windows can be different heights — the chrome
	// this state installs is not the chrome the earlier one did, and the window is whatever
	// the chrome leaves — so the comparison runs as far as both of them go. That is where the
	// invariant lives: same first row, same text under it.
	n := before.Scroll.Rows
	if after.Scroll.Rows < n {
		n = after.Scroll.Rows
	}
	if n <= 0 {
		t.Fatal("the chrome left no window on either frame, so there is nothing to compare")
	}
	if diff := firstRowDiff(before.Live[:n], after.Live[:n]); diff != "" {
		t.Errorf("the conversation grew and the rows on screen changed with it — %s", diff)
	}
}

func TestRendererPassesViewportHeightToStatusChrome(t *testing.T) {
	st := &state.State{
		Active: true, Actor: "builder", CWD: `D:\projects\arxi`, GitBranch: "main",
		Model: "claude-sonnet-5", ContextUsed: 81250, ContextCapacity: 200000, Effort: "high",
	}
	for _, tc := range []struct {
		height, rows int
	}{
		{8, 1},
		{9, 2},
		{14, 3},
	} {
		r := NewRenderer()
		r.Widgets = []Widget{StatusWidget{St: st}}
		f := r.Render(st, nil, Viewport{Width: 96, Height: tc.height})
		if len(f.Live) != tc.height {
			t.Fatalf("height %d produced %d live rows", tc.height, len(f.Live))
		}
		nonempty := 0
		for _, row := range f.Live {
			if strings.TrimSpace(row.Text()) != "" {
				nonempty++
			}
		}
		if nonempty != tc.rows {
			t.Errorf("height %d drew %d status rows, want %d:\n%s", tc.height, nonempty, tc.rows, f.Plain())
		}
	}
}

// TestPromptRowsNamesEveryTurnAndNothingElse pins the seam a jump key stands on. The app
// asks this question for a row to put at the top of the window, and it has to ask because
// nothing outside this package knows how far a conversation wraps at a given width — so the
// answer has to be a row of the transcript the app is about to scroll, in the unit
// Frame.Scroll counts, or the landmark lands somewhere the reader did not ask for.
//
// The document is the yardstick: at no height the whole transcript is committed, so
// Committed[i] is transcript row i. Two things are asserted about each row and the second is
// the one that costs an off-by-one — the row carries the turn's own first line, and the row
// above it is the blank between two items, which is charged to the item that follows it.
// Naming the separator instead would put a blank row at the top of the window and the
// landmark one row under it.
func TestPromptRowsNamesEveryTurnAndNothingElse(t *testing.T) {
	// A notice first, so that every turn below has an item before it and therefore a
	// separator above it. The words are distinct and appear nowhere else, which is what lets
	// a landing be checked by reading the row.
	turns := []string{
		"alpha, the first thing the reader said",
		"bravo",
		"charlie, long enough to wrap more than once at every width this test asks for, twice over",
	}
	st := state.New()
	st.AppendNotice(state.NoticeLock, "a row that is not the reader's")
	for i, text := range turns {
		st.AppendPrompt(text)
		st.AppendNotice(state.NoticeLock, fmt.Sprintf("an answer of sorts, %d", i))
	}
	r := NewRenderer()
	for _, w := range []int{24, 40, 72} {
		// The same viewport the document below is drawn with, and that is the point rather than a
		// convenience: the question is asked with a viewport precisely so that a caller cannot ask
		// it at one width and draw at another, and a test that passed the two separately would be
		// the drift it is meant to catch.
		rows := r.PromptRows(st, docViewport(w))
		doc := render(r, st, NewInput(), docViewport(w)).Committed
		if len(rows) != len(turns) {
			t.Fatalf("width %d: %d rows for %d turns of the reader's: %v", w, len(rows), len(turns), rows)
		}
		for i, row := range rows {
			if i > 0 && row <= rows[i-1] {
				t.Errorf("width %d: turn %d is at row %d, turn %d at %d: the rows only grow, and a caller walks them once", w, i-1, rows[i-1], i, row)
			}
			if row < 0 || row >= len(doc) {
				t.Fatalf("width %d: turn %d is at row %d of a %d-row transcript", w, i, row, len(doc))
			}
			if word := strings.Fields(turns[i])[0]; !strings.Contains(doc[row].Text(), word) {
				t.Errorf("width %d: row %d is %q, and turn %d begins %q", w, row, doc[row].Text(), i, word)
			}
			if row == 0 || strings.TrimSpace(doc[row-1].Text()) != "" {
				t.Errorf("width %d: row %d, just above turn %d, is %q and should be the blank between two items", w, row-1, i, doc[row-1].Text())
			}
		}
	}
	// A width too narrow to draw a turn at all names none. banded gives up when its marker
	// fills the width, so the turn occupies no row, and the row it does not occupy is the -1
	// the layout hands back — which is a number a caller indexes a document with. Every jump
	// in the player goes through here, so this filter is the difference between "there is no
	// landmark that narrow" and a panic on the first press.
	for _, w := range []int{-1, 0, 1, 2} {
		if rows := r.PromptRows(st, docViewport(w)); rows != nil {
			t.Errorf("width %d draws no turn and named %v", w, rows)
		}
	}
	if rows := r.PromptRows(nil, docViewport(72)); rows != nil {
		t.Errorf("a nil state named the rows %v", rows)
	}
}

// TestPromptInsideNamesTheTurnTheWindowIsIn pins the other half of the same seam. The pinned
// header asks which turn the top of the window has come to rest inside, and it asks for two
// things at once: whose turn it is, and how much of that turn the reader has scrolled past —
// because the header repeats exactly the rows that are gone and never a row still on screen.
//
// So the rule under test is a span and a count. A turn owns the rows from its first to the
// next turn's first, which is what keeps a header up for the whole of a long answer; and the
// count is the distance from the turn's first row down to the window, capped at the turn's own
// height. The two degenerate answers fall out of that arithmetic rather than being special
// cases: a window resting exactly on a turn's first row is a count of zero, which is the jump
// key's landing and the one place a pin would print the transcript's own next line twice, and
// a window above every turn of the reader's has no turn to name.
//
// The oracle is read off the page rather than computed the way the function computes it, so
// the two cannot agree by sharing a mistake: a block is a run of non-empty rows between the
// layout's separators, a block is the reader's if its first row carries the prompt marker, and
// the expected answer at row n comes from the last such block that starts at or above n.
func TestPromptInsideNamesTheTurnTheWindowIsIn(t *testing.T) {
	turns := []string{
		"alpha, the first thing the reader said",
		"bravo",
		"charlie, a pasted paragraph rather than a question: long enough to wrap more than twice at every width this test asks for, and long enough again that the header's two-row cap is a cap and not simply the whole of the message",
	}
	st := state.New()
	st.AppendNotice(state.NoticeLock, "a row that is not the reader's")
	for i, text := range turns {
		st.AppendPrompt(text)
		st.AppendNotice(state.NoticeLock, fmt.Sprintf("an answer of sorts, %d", i))
	}
	r := NewRenderer()
	marker := DefaultGlyphs().Get("prompt.marker")
	for _, w := range []int{24, 40, 72} {
		doc := render(r, st, NewInput(), docViewport(w)).Committed
		// The blocks, measured off the page. A separator is the one empty Line the layout puts
		// between two items; every row a block drew has at least a marker or an indent in it.
		type block struct {
			text          string
			start, height int
		}
		var mine []block
		for i := 0; i < len(doc); i++ {
			if len(doc[i]) == 0 {
				continue
			}
			first := i
			for i+1 < len(doc) && len(doc[i+1]) != 0 {
				i++
			}
			if !strings.HasPrefix(doc[first].Text(), marker) {
				continue
			}
			text := ""
			for _, want := range turns {
				if strings.Contains(doc[first].Text(), strings.Fields(want)[0]) {
					text = want
				}
			}
			if text == "" {
				t.Fatalf("width %d: the block at row %d wears the reader's marker and is none of the turns: %q", w, first, doc[first].Text())
			}
			mine = append(mine, block{text: text, start: first, height: i + 1 - first})
		}
		if len(mine) != len(turns) {
			t.Fatalf("width %d: found %d turns of the reader's in %d rows, and there are %d", w, len(mine), len(doc), len(turns))
		}
		if mine[len(mine)-1].height < HeaderRows+1 {
			t.Fatalf("width %d: the tallest turn here is %d rows, so nothing tests the cap or the count", w, mine[len(mine)-1].height)
		}
		// Every row of the document, plus one past the end, which is where a window pinned to the
		// very bottom of a finished transcript starts.
		for row := 0; row <= len(doc); row++ {
			wantText, wantRows := "", 0
			for _, b := range mine {
				if b.start <= row {
					wantText, wantRows = b.text, min(row-b.start, b.height)
				}
			}
			text, rows := r.PromptInside(st, docViewport(w), row)
			if text != wantText || rows != wantRows {
				t.Errorf("width %d, window at row %d: pinned %d rows of %q, and the turn it is inside is %q with %d rows above the edge",
					w, row, rows, text, wantText, wantRows)
			}
		}
		// And the two positions the rule is really about, named out loud so a reader of this test
		// does not have to run the oracle in their head: a window on a turn's own first row pins
		// nothing, and one row further down pins one row and no more.
		last := mine[len(mine)-1]
		if _, rows := r.PromptInside(st, docViewport(w), last.start); rows != 0 {
			t.Errorf("width %d: a window resting on the first row of a turn pinned %d rows of it, and that row is on screen", w, rows)
		}
		if _, rows := r.PromptInside(st, docViewport(w), last.start+1); rows != 1 {
			t.Errorf("width %d: one row into a turn, %d of its rows are pinned and only 1 has been lost", w, rows)
		}
	}
	// The narrow widths and the nil state, for PromptRows' reason: this is asked on a keypress,
	// and a width that draws no turn has no turn to pin rather than a turn at row -1.
	for _, w := range []int{-1, 0, 1, 2} {
		if text, rows := r.PromptInside(st, docViewport(w), 4); text != "" || rows != 0 {
			t.Errorf("width %d draws no turn and pinned %d rows of %q", w, rows, text)
		}
	}
	if text, rows := r.PromptInside(nil, docViewport(72), 4); text != "" || rows != 0 {
		t.Errorf("a nil state pinned %d rows of %q", rows, text)
	}
	// A blank turn names nothing, and the empty text is how it says so. It is not the same as
	// having drawn nothing — a message with no words in it still takes a row, the marker and the
	// band it always had, so a count comes back for it — and the header is text and not rows: the
	// widget draws no header without something to quote. Which is also the honest answer for a
	// reader scrolled inside such a turn, rather than pinning the question before it and naming
	// a turn they have already read past.
	blank := state.New()
	blank.AppendPrompt("")
	blank.AppendNotice(state.NoticeLock, "and an answer, so the turn above is not the last item")
	if text, rows := r.PromptInside(blank, docViewport(72), 99); text != "" {
		t.Errorf("a blank turn pinned %d rows of %q", rows, text)
	}
	if rows := (HeaderWidget{Text: "", Rows: 1}).Render(72, 0, DefaultGlyphs()); rows != nil {
		t.Errorf("the widget drew %d rows for a blank turn: %q", len(rows), rows[0].Text())
	}
}

// TestUnclosedFenceRendersAsCode is the reason this markdown renderer exists
// instead of a library that needs a whole document. Delta 18 of the scenario opens
// a ```go fence and delta 19 closes it; in between, a renderer that waits shows
// nothing at all for 150 ms and then jumps.
func TestUnclosedFenceRendersAsCode(t *testing.T) {
	_, sc := play(t, firstConversation, 0)
	gutter := DefaultGlyphs().Get("code.gutter")
	sawOpen := false
	for n := 1; n <= len(sc.Steps); n++ {
		st, _ := play(t, firstConversation, n)
		text := ""
		for _, it := range st.Items {
			if it.Kind == state.KindText {
				text = it.Text
			}
		}
		if strings.Count(text, "```")%2 == 0 {
			continue
		}
		sawOpen = true
		r := NewRenderer()
		f := r.Render(st, NewInput(), docViewport(72))
		if !strings.Contains(f.Plain(), gutter) {
			t.Fatalf("after %d steps an open fence drew no gutter:\n%s", n, f.Plain())
		}
	}
	if !sawOpen {
		t.Fatal("the scenario never leaves a fence open mid-stream, so this test proves nothing")
	}
}

// TestPendingToolIsVisible checks the frame shows a call before it has a result.
// A tool that appears only once it finishes is a tool the user cannot interrupt.
func TestPendingToolIsVisible(t *testing.T) {
	_, sc := play(t, firstConversation, 0)
	found := false
	for n := 1; n <= len(sc.Steps); n++ {
		st, _ := play(t, firstConversation, n)
		for _, it := range st.Items {
			if it.Kind != state.KindTool || it.Status != state.ToolPending {
				continue
			}
			found = true
			r := NewRenderer()
			plain := r.Render(st, NewInput(), docViewport(72)).Plain()
			if !strings.Contains(plain, displayTool(it.Tool)+"(") {
				t.Fatalf("after %d steps: pending %s not drawn:\n%s", n, it.Tool, plain)
			}
		}
	}
	if !found {
		t.Fatal("no tool was ever pending, so this test proves nothing")
	}
}

// TestApprovalIsAskedBelowTheInput proves the question that blocks the run is on
// screen while it blocks it, exactly once, and gone afterwards. "Gone" means the
// widget is gone: state.Apply also records the decision as a notice in the
// transcript, and that record is supposed to stay, because what the human answered
// is a fact about the run. The two are told apart by the marker, which is the only
// part of the widget no block ever draws.
//
// The count is the point of the test, not a detail of it. This assertion used to be
// a Contains, and under it the renderer synthesized an approval widget out of state
// while the player installed one too: the question was drawn twice, in the same
// slot, and every test still passed.
func TestApprovalIsAskedBelowTheInput(t *testing.T) {
	_, sc := play(t, firstConversation, 0)
	marker := DefaultGlyphs().Get("approval.marker")
	asked := 0
	for n := 1; n <= len(sc.Steps); n++ {
		st, _ := play(t, firstConversation, n)
		r := NewRenderer()
		plain := render(r, st, NewInput(), docViewport(72)).Plain()
		if ib := st.OpenInbox(); ib != nil {
			asked++
			if !strings.Contains(plain, ib.Question) {
				t.Fatalf("after %d steps: inbox %s is open but unasked:\n%s", n, ib.ID, plain)
			}
			if got := strings.Count(plain, marker); got != 1 {
				t.Fatalf("after %d steps: inbox %s is asked %d times, want 1:\n%s", n, ib.ID, got, plain)
			}
			continue
		}
		if strings.Contains(plain, marker) {
			t.Fatalf("after %d steps: an answered approval is still on screen:\n%s", n, plain)
		}
	}
	if asked == 0 {
		t.Fatal("no approval was ever pending, so this test proves nothing")
	}
}

// TestItemIDsAreUnique guards the assumption the block cache is built on. The
// renderer memoizes a sealed block under its id, so two items sharing one is not a
// cosmetic problem: the second is drawn with the first's lines and its own text
// never reaches the screen. This caught exactly that — two notices, both with no id
// at all, the run's closing diagnosis replaced by an approval that had scrolled
// past long before.
func TestItemIDsAreUnique(t *testing.T) {
	st, _ := play(t, firstConversation, -1)
	st.AppendPrompt("one")
	st.AppendPrompt("two")
	seen := map[string]int{}
	for i, it := range st.Items {
		if it.ID == "" {
			t.Errorf("item %d (kind %d) has no id", i, it.Kind)
			continue
		}
		if j, dup := seen[it.ID]; dup {
			t.Errorf("items %d and %d share the id %q", j, i, it.ID)
		}
		seen[it.ID] = i
	}
}

// TestEveryNoticeAndPromptReachesTheFrame is the same defect seen from the other
// side, and it is the check that would have failed first: whatever the cache does,
// text that state recorded has to appear on screen. The width is wide enough that
// nothing wraps, so the comparison is a plain substring.
func TestEveryNoticeAndPromptReachesTheFrame(t *testing.T) {
	st, _ := play(t, firstConversation, -1)
	r := NewRenderer()
	plain := r.Render(st, NewInput(), docViewport(200)).Plain()
	for i, it := range st.Items {
		if it.Kind != state.KindNotice && it.Kind != state.KindPrompt {
			continue
		}
		if !strings.Contains(plain, it.Text) {
			t.Errorf("item %d (%q) is in state but not in the frame:\n%s", i, it.Text, plain)
		}
	}
}

// TestNoTranscriptLineEndsInSpace is the other half of the width discipline. A
// trailing blank is invisible, so it survives review, and then it fills the last
// cell of a row and the terminal wraps a line that looked short. It is checked with
// no input, because a space the human typed is real content the cursor can sit on
// and the editor is right to keep it.
func TestNoTranscriptLineEndsInSpace(t *testing.T) {
	_, sc := play(t, firstConversation, 0)
	for _, w := range []int{20, 24, 40, 72, 120} {
		for n := 0; n <= len(sc.Steps); n++ {
			st, _ := play(t, firstConversation, n)
			r := NewRenderer()
			f := render(r, st, nil, docViewport(w))
			for _, l := range append(append([]Line{}, f.Committed...), f.Live...) {
				if endsInBareSpace(l) {
					t.Fatalf("width %d after %d steps: line ends in a space: %q", w, n, l.Text())
				}
			}
		}
	}
}

// styleProbe resolves the keys this file's assertions ask about. One theme, built
// once: the sweeps below call endsInBareSpace on every line of every frame at every
// width, and rebuilding the shipped table each time would cost more than the whole
// rest of the package's tests.
var styleProbe = DefaultTheme()

// endsInBareSpace is the trailing-blank rule stated precisely enough to survive the
// diff view. A trailing space is a bug when it is *nothing*: invisible in review,
// and it fills the last cell of a row the terminal then wraps. A trailing space
// carrying a background is not nothing — it is the right-hand end of a diff band,
// and a band that runs to the frame's edge is the entire reason one is drawn. So the
// rule is "no line ends in an unstyled space", and telling the two apart means
// resolving the span's two keys the way emit does, Style over Fill.
func endsInBareSpace(l Line) bool {
	if !strings.HasSuffix(l.Text(), " ") {
		return false
	}
	for i := len(l) - 1; i >= 0; i-- {
		if l[i].Text == "" {
			continue
		}
		st := styleProbe.Resolve(l[i].Style).Over(styleProbe.Resolve(l[i].Fill))
		return st.BG.Kind == ColorNone
	}
	return true
}

// TestChromeTallerThanTheScreenGivesUpItsTop is the last thing a frame with a height has
// to survive: a screen too short for the chrome alone. An open approval, a prompt wrapped
// inside its box, a notice and a status row are thirteen rows, and a twelve-row terminal
// has to be sent twelve. Every other height test in this file exercises the transcript
// window shrinking instead, which stops at zero and leaves this path — the one that cuts
// into the chrome — with nothing looking at it.
//
// The property is stronger than a row count, because a count is satisfied by a frame that
// dropped the input and kept the notice: whatever the short frame is, it is the *tail* of
// the same chrome drawn with no height at all. The rows given up are the top ones, in
// order, and the input the human is typing into is the last thing to go.
//
// A frame taller than the height it reports is not a cosmetic fault. On the alternate
// buffer the emitter's own repaint scrolls the buffer by the difference, which moves every
// row out from under the coordinates the next frame was computed against.
func TestChromeTallerThanTheScreenGivesUpItsTop(t *testing.T) {
	const w = 40
	st, _ := play(t, firstConversation, -1)
	st.Inboxes = append(st.Inboxes, &state.Inbox{
		ID: "inbox-1", Kind: "approval", Agent: "builder",
		Question: "run `rm -rf build/` in the workspace root?",
	})
	in := NewInput()
	in.SetText("a prompt long enough to wrap inside its box more than once, at forty columns")

	r := NewRenderer()
	r.Widgets = append(ChromeFor(st),
		NoticeWidget{Text: "the recording ended; press ctrl+d to leave"},
		StatusWidget{St: st, Phase: 3})

	// The document frame is the yardstick: at Height 0 the transcript is committed and Live
	// is the chrome by itself, which is exactly the region a screen this short has left.
	doc := r.Render(st, in, Viewport{Width: w})
	tall := len(doc.Live)
	if tall < 6 {
		t.Fatalf("the chrome is only %d rows; this test needs one that overflows:\n%s", tall, doc.Plain())
	}
	if doc.Cursor.Hidden || doc.Cursor.Line == 0 {
		t.Fatalf("the cursor is at %+v; it has to be inside the box for the trim to move it", doc.Cursor)
	}

	for h := 1; h <= tall; h++ {
		f := r.Render(st, in, Viewport{Width: w, Height: h})
		// Status is deliberately responsive: short screens keep one row, while this
		// range's taller screens gain the model row. Extend the height-zero chrome
		// yardstick with that semantic row before taking its tail.
		expected := append([]Line{}, doc.Live...)
		status := (StatusWidget{St: st, Phase: 3}).Render(w, h, r.Glyphs)
		if len(status) > 1 {
			expected = append(expected, status[1:]...)
		}
		d := len(expected) - h
		if d < 0 {
			d = 0
		}
		if len(f.Live) != h {
			t.Fatalf("height %d: the frame is %d rows\n%s", h, len(f.Live), f.Plain())
		}
		if len(f.Committed) != 0 {
			t.Errorf("height %d: %d rows were handed to scrollback", h, len(f.Committed))
		}
		// And no row of the transcript is on screen: the window is what shrank first, and it
		// has been empty since the chrome reached the height. A caller drawing a position
		// indicator has to be told that, or it reports a screenful that is not there.
		if f.Scroll.Rows != 0 || f.Scroll.Below != 0 {
			t.Errorf("height %d: scroll is %+v, want no window at all", h, f.Scroll)
		}
		for i, l := range f.Live {
			if got, want := l.Text(), expected[d+i].Text(); got != want {
				t.Errorf("height %d row %d: %q, want %q (the tail of the chrome)\n%s",
					h, i, got, want, f.Plain())
			}
		}
		// The cursor moves up with the rows and stops at the top edge. It only reaches that
		// clamp once the input's own rows are being cut, and then it sits on the first row
		// still visible — a later row of the same prompt. What it must never do is name a row
		// the frame does not have: the emitter would place the terminal cursor outside the
		// screen it just painted.
		if want := max(0, doc.Cursor.Line-d); f.Cursor.Line != want {
			t.Errorf("height %d: cursor on row %d, want %d", h, f.Cursor.Line, want)
		}
		if f.Cursor.Hidden || f.Cursor.Line >= len(f.Live) {
			t.Errorf("height %d: cursor %+v is outside a frame of %d rows", h, f.Cursor, len(f.Live))
		}
	}
}

// TestGoldenFirstConversation pins the whole transcript. Run with -update to
// rewrite it, then read the diff: this file is the interface, and a change to it
// should be as reviewable as a change to the code.
//
// No height, because "the whole transcript" is a claim a frame with a height cannot make:
// it holds a screenful and the rest of the conversation is above it, unpinned. This golden
// is the document, which is also what --instant prints and what a pipe receives.
func TestGoldenFirstConversation(t *testing.T) {
	st, _ := play(t, firstConversation, -1)
	r := NewRenderer()
	in := NewInput()
	got := render(r, st, in, docViewport(72)).Plain() + "\n"
	path := filepath.Join("testdata", "01-first-conversation.72.txt")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("wrote " + path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/ui -update)", err)
	}
	if got != string(want) {
		t.Errorf("frame differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
