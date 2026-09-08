package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/state"
)

// A Widget is chrome: unlike a Block it is repainted every frame, it never reaches
// scrollback, and it owns a rectangle rather than a position in a stream. The
// status line, the input frame, a token counter and a pending approval are all the
// same kind of thing, which is the answer to "is almost everything a widget?" —
// yes, everything except the transcript itself.
type Widget interface {
	// Name is what a user writes in config to move, restyle or disable it.
	Name() string

	// Slot is where it asks to be drawn.
	Slot() Slot

	// Fallback is where it goes when its slot is unavailable. The empty slot
	// means "leave me out"; a widget that degrades badly should say so here
	// rather than let the layout guess.
	Fallback() Slot

	// Animated reports whether it changes on its own. A frame with no animated
	// widget and no open block needs no timer, which is what lets the simulator
	// park instead of repainting on a clock.
	Animated() bool

	// Render draws it. h <= 0 means the widget may be as tall as it likes.
	Render(w, h int, g Glyphs) []Line
}

// Place decides where a widget actually goes.
func Place(wd Widget, vp Viewport) (Slot, bool) {
	s := wd.Slot()
	if s.Available(vp) {
		return s, true
	}
	if f := wd.Fallback(); f != "" && f.Available(vp) {
		return f, true
	}
	return "", false
}

// ChromeFor is the chrome the state itself demands, which today is the one open
// question. It is a function the caller invokes rather than something Render does on
// its own, so that the widget list is a value the app — and eventually a config file
// — can reorder, filter or replace. Render deriving this from state was a bug twice
// over: the app installed it too, so the question was drawn twice, and a widget that
// only exists inside the render pass is one a user cannot turn off.
//
// StatusWidget is deliberately not here even though it reads nothing but state. It
// needs a Phase that no state field carries, so whoever builds it has to set one, and
// that makes it the player's chrome rather than the state's — installed by app.chrome
// alongside the end-of-scenario notice. Which is also why it stays out of the golden
// file and out of both corpus sweeps: a fold has no wall clock to spin against.
func ChromeFor(st *state.State) []Widget {
	if st == nil {
		return nil
	}
	var out []Widget
	if ib := st.OpenInbox(); ib != nil {
		out = append(out, ApprovalWidget{Inbox: ib})
	}
	return out
}

// TasksWidget summarizes the live task list beside the input. It is a value over
// State rather than a cached count, so every frame reflects the latest task event.
type TasksWidget struct{ St *state.State }

func (TasksWidget) Name() string   { return "tasks" }
func (TasksWidget) Slot() Slot     { return SlotAboveInput }
func (TasksWidget) Fallback() Slot { return SlotBelowInput }
func (TasksWidget) Animated() bool { return false }

func (tw TasksWidget) Render(w, _ int, _ Glyphs) []Line {
	if tw.St == nil || len(tw.St.Tasks) == 0 || w <= 0 {
		return nil
	}
	completed, active, pending := 0, 0, 0
	for _, task := range tw.St.Tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case event.TaskCompleted:
			completed++
		case event.TaskActive:
			active++
		case event.TaskPending:
			pending++
		}
	}
	total := len(tw.St.Tasks)
	variants := []Line{
		{{Text: fmt.Sprintf("Tasks %d/%d", completed, total), Style: "tasks.summary"}, {Text: fmt.Sprintf(" · %d active · %d pending · ", active, pending), Style: "tasks.meta"}, {Text: "/tasks", Style: "tasks.action"}},
		{{Text: fmt.Sprintf("Tasks %d/%d", completed, total), Style: "tasks.summary"}, {Text: fmt.Sprintf(" · %d active · ", active), Style: "tasks.meta"}, {Text: "/tasks", Style: "tasks.action"}},
		{{Text: fmt.Sprintf("Tasks %d/%d · ", completed, total), Style: "tasks.summary"}, {Text: "/tasks", Style: "tasks.action"}},
		{{Text: fmt.Sprintf("Tasks %d/%d", completed, total), Style: "tasks.summary"}},
		{{Text: "Tasks", Style: "tasks.summary"}},
	}
	for _, row := range variants {
		if row.Width() <= w {
			return []Line{row.TrimRight()}
		}
	}
	return nil
}

// ApprovalWidget asks the question that is blocking the run. It sits under the
// input rather than in the transcript because it is not a fact about what
// happened; it is a thing the human still has to do, and it disappears the moment
// they do it.
type ApprovalWidget struct{ Inbox *state.Inbox }

func (ApprovalWidget) Name() string   { return "approval" }
func (ApprovalWidget) Slot() Slot     { return SlotBelowInput }
func (ApprovalWidget) Fallback() Slot { return SlotBottom }
func (ApprovalWidget) Animated() bool { return false }

func (a ApprovalWidget) Render(w, _ int, g Glyphs) []Line {
	if a.Inbox == nil || w <= 0 {
		return nil
	}
	out := marked(g.Span("approval.marker", "approval.marker"), a.Inbox.Question, "approval.question", w)
	out = append(out,
		approvalOption("y", "allow"),
		approvalOption("n", "deny"),
	)
	if a.Inbox.OnTimeout != "" {
		out = append(out, WrapSpans([]Span{
			{Text: "  timeout", Style: "approval.hint"},
			{Text: "  " + a.Inbox.OnTimeout, Style: "approval.hint"},
		}, w, nil)...)
	}
	return out
}

func approvalOption(key, hint string) Line {
	return Line{
		{Text: "  "},
		{Text: key, Style: "approval.key"},
		{Text: "  " + hint, Style: "approval.hint"},
	}.TrimRight()
}

// HeaderRows is the most of a message the header will spend on itself: enough that a sentence
// survives a narrow terminal, few enough that a pasted paragraph cannot take the screen away
// from the conversation it is only a label for.
//
// It is exported because the rows come out of the transcript window, so anything that reasons
// about how far the window can move — a page key, a wheel notch — has to know the most it can
// lose to a header that is not up yet.
const HeaderRows = 2

// HeaderWidget is the reader's own message, held above the transcript while they are scrolled
// away from it. A long conversation is searched by landmark, and the only landmark in one is
// what the human asked; once that has gone off the top of the window every row on the screen
// is an answer to a question the screen no longer names.
//
// It carries the text rather than the state, for StatusWidget's reason: which message this is
// depends on where the window is, and the window is the app's to know. And it draws whatever
// it was handed — no text or no rows and it draws nothing — so the rule about *when* a header
// is wanted lives in the single place that decides to install it, instead of half here and
// half there.
type HeaderWidget struct {
	Text string

	// Rows is how much of the message has gone past the top edge, and therefore how much of it
	// this row is allowed to repeat: the rows the reader has lost, and never a row that is also
	// on screen a line below. Drawing the whole message regardless would duplicate its second
	// line at the one scroll position where the first has just left — the pin and the transcript
	// printing the same words, one under the other, which is the fault this bounds away.
	Rows int
}

func (HeaderWidget) Name() string { return "header" }
func (HeaderWidget) Slot() Slot   { return SlotTop }

// Fallback is empty because there is no second place a pinned row can go. The top slot wants
// a surface that does not slide out from under it, and inline mode is not one: there the rows
// above the input belong to the terminal's history the moment they are printed, so a row
// "pinned" into history is the same message printed again on every frame. Inline leaves it
// out and keeps the jump keys, which reach the same landmark from the other end.
func (HeaderWidget) Fallback() Slot { return "" }

// Animated is false: this changes when the view moves, and moving the view already redraws.
func (HeaderWidget) Animated() bool { return false }

// Render draws the message the way the transcript drew it — same marker, same band, through
// the same helper — because a header is a quotation and not a summary. All the reader has to
// do with this row is recognise it, and a second styling for their own words would be a
// second thing to learn.
//
// banded already refuses a width it cannot fit a marker into, which is what answers w <= 0
// and the one- and two-column terminals: no rows, so no header, rather than a marker with
// nothing after it.
func (h HeaderWidget) Render(w, _ int, g Glyphs) []Line {
	if h.Text == "" || h.Rows <= 0 {
		return nil
	}
	rows := banded(g.Span("prompt.marker", "prompt.marker"), h.Text, "prompt.text", "prompt.band", w)
	n := min(h.Rows, HeaderRows)
	if len(rows) <= n {
		return rows
	}
	gap := g.Get("tool.result.gap")
	room := w - ansi.StringWidth(gap)
	if room <= 0 {
		return rows[:n] // no room to say anything about the cut, so it goes unsaid
	}
	// The mark lands where the words stop and not out at the right edge, where a lone glyph in
	// the last column reads as a scrollbar instead of as the end of a cut sentence. So the row
	// is trimmed back to its ink, marked, and padded out again — and both new spans carry the
	// band by hand, because banded has already run and a span added after it with no Fill
	// punches a hole in the wash.
	last := rows[n-1].TrimRight()
	if last.Width() > room {
		if cut := HardWrapSpans(last, room); len(cut) > 0 {
			last = cut[0]
		}
	}
	last = append(last, Span{Text: gap, Style: "prompt.text", Fill: "prompt.band"})
	if p := w - last.Width(); p > 0 {
		last = append(last, Span{Text: strings.Repeat(" ", p), Fill: "prompt.band"})
	}
	return append(rows[:n-1], last)
}

// ScrollReader is a widget that draws the frame's own geometry, and it is a second interface
// rather than a field on Widget because only one kind of widget has ever wanted it.
//
// The numbers are handed over here because there is nowhere else to hand them over from. How
// much of the transcript is above the window is settled inside Render, a few lines before the
// frame is assembled, and the only other holder of that arithmetic is the caller — which has
// the *previous* frame's copy. A bar built from that lags by a frame, which for a conversation
// arriving a line at a time means a bar that is permanently one notch behind the text it claims
// to measure, and worst exactly while the reader is watching it move.
//
// WithScroll returns a widget rather than setting a field so that the list a caller installed is
// never written through: the renderer holds widget values in a slice it does not own, and a
// method with a pointer receiver would make every frame a mutation of the caller's chrome.
type ScrollReader interface {
	Widget
	WithScroll(sc Scroll) Widget
}

// ScrollbarWidget is the bar down the side of the transcript: how much of the conversation is
// on the screen, and which part of it. A pager has this and a chat log does not, and the
// difference is felt the moment a reader scrolls up — without it the only way to learn how far
// from the end you are is to press a key and watch what happens.
//
// It answers two questions and it is honest about both. Its length is the window as a fraction
// of the whole log, so a screenful of a short conversation is a long bar and a screenful of a
// long one is a short one; its position is where the window sits in that log. And it can be
// dragged: Grab and Offset are the same arithmetic bar draws with, read backwards, so the
// geometry that puts the thumb on a row is the geometry that says what a click on that row
// meant. A second copy of it in the caller would agree today and drift by a row the first time
// either was touched.
type ScrollbarWidget struct {
	// Scroll is the frame's geometry, put here by the renderer through WithScroll. The zero
	// value draws nothing, which is the right answer for a widget nobody has fed: a bar with
	// no numbers behind it would be a bar claiming the whole log is on screen.
	Scroll Scroll

	// Held is whether the reader has hold of the thumb right now, and it only picks a style.
	// It is a field rather than something the widget works out for itself because a widget is
	// built fresh every frame and a drag is the one piece of this interface that outlives one:
	// whoever is tracking the pointer is already holding this, and nothing else can know it.
	Held bool
}

func (ScrollbarWidget) Name() string { return "scrollbar" }
func (ScrollbarWidget) Slot() Slot   { return SlotRight }

// Fallback is empty because a bar has no second home. Every other slot is a row, and a bar laid
// out along a row measures nothing — its length is a count of rows, so a horizontal one would be
// a length in the wrong unit. Inline mode therefore loses it, which is what the layout already
// says about the right slot, and what it keeps instead is the keyboard ladder: a page key and a
// jump reach the same places from the other end.
func (ScrollbarWidget) Fallback() Slot { return "" }

// Animated is false. The bar moves when the window moves and when a row lands, and both of
// those already redraw; a timer would repaint an unchanged column ten times a second.
func (ScrollbarWidget) Animated() bool { return false }

// WithScroll is the whole of the ScrollReader contract: a copy carrying this frame's numbers.
func (s ScrollbarWidget) WithScroll(sc Scroll) Widget { s.Scroll = sc; return s }

// Render draws the column: a track as tall as the window with a thumb somewhere in it, and no
// rows at all when there is nothing worth saying.
//
// A cell whose glyph is empty, or wider than the column it has to fit in, contributes no row
// rather than a broken one — the same answer the spinner gives an emptied cycle, and the reason
// an override cannot turn this into a frame two columns too wide. The glyph is put at the right
// of the column and the air at the left, because the air belongs between the bar and the prose.
//
// A held thumb changes style and not glyph. The reader is being told the pointer has hold of this
// thing, and a column that changed shape under the finger would look like the bar had moved
// rather than been grabbed; keeping the glyph also keeps the held state out of the one place a
// glyph override could make the column the wrong width.
func (s ScrollbarWidget) Render(w, h int, g Glyphs) []Line {
	top, n, ok := s.bar(h)
	if w <= 0 || !ok {
		return nil
	}
	out := make([]Line, h)
	for i := range out {
		glyph, style := g.Get("scroll.track"), "scroll.track"
		if i >= top && i < top+n {
			glyph, style = g.Get("scroll.thumb"), "scroll.thumb"
			if s.Held {
				style = "scroll.thumb.held"
			}
		}
		gw := ansi.StringWidth(glyph)
		if gw == 0 || gw > w {
			continue
		}
		row := Line{}
		if air := w - gw; air > 0 {
			row = append(row, pad(air))
		}
		out[i] = append(row, Span{Text: glyph, Style: style})
	}
	return out
}

// hidden is the rows the window is not showing: off the top, and off the bottom, clamped because a
// negative count is a caller's arithmetic gone wrong and a negative length is a panic waiting for a
// narrow terminal.
//
// Their sum is the one quantity here that a scroll cannot change — it is the whole log minus the
// window — which is what makes a drag safe to compute from whatever frame is on the screen: several
// motion reports folded into one frame all measure against the same total.
func (s ScrollbarWidget) hidden() (above, below int) {
	return max(0, s.Scroll.Above), max(0, s.Scroll.Below)
}

// ends is the range of the track the thumb may occupy, in rows of a column h tall: one cell is held
// back at each end that has rows behind it. So the thumb touches the top only when nothing is above
// it and the bottom only when nothing is below it, and both are true by construction rather than by
// a correction after the fact. Rounding is why that matters: the arithmetic reaches an end a row
// early on a long log, and a bar resting at the bottom while a hundred rows are still under it is
// the one error that costs the reader a keypress to find out about.
//
// Not ok means there is no bar, and there are two ways to get there. The first is that the whole
// conversation is on the screen already: a bar filling its own track says nothing, and it would say
// it in the one situation where the reader most needs to be told there is nothing hidden. The
// second is a track too short to hold the held-back cells and a thumb — a one-row track can say
// nothing about position at all, and a two-row one only while the window is at an end — so those
// draw nothing rather than a bar that lies. That is reachable only when the chrome has taken all
// but a row or two of the terminal, and a reader in that much trouble is not helped by a wrong pill.
//
// It is a method of its own because it is a claim about the track and not a step of drawing one:
// "which rows may the thumb start on, and is there a bar at all" is a question with two answers in
// it, and the second one is what Render draws nothing on and what Grab and Offset refuse on, each of
// them through bar. Offset does not call it, and the doc there says why — the held-back cells belong
// to the frame on the screen, and a drag leaves it.
func (s ScrollbarWidget) ends(h int) (lo, hi int, ok bool) {
	above, below := s.hidden()
	if h <= 0 || above+below == 0 {
		return 0, 0, false
	}
	lo, hi = 0, h
	if above > 0 {
		lo++
	}
	if below > 0 {
		hi--
	}
	return lo, hi, hi-lo >= 1
}

// bar is the geometry: how long the thumb is and where it starts, in rows of a column h tall.
// Not ok is ends' answer and is documented there.
//
// The held-back cells are taken before the thumb is measured and not after it is placed, because
// the two are the same claim: an off-screen row always leaves an empty cell, so "is there more, and
// in which direction?" is answered by the shape of the bar rather than by reading it as a
// proportion.
func (s ScrollbarWidget) bar(h int) (top, n int, ok bool) {
	lo, hi, ok := s.ends(h)
	if !ok {
		return 0, 0, false
	}
	above, below := s.hidden()
	// Rows is the window's own height, which is h on every frame this renderer builds. It is
	// read from Scroll all the same, and falls back to h only when unset, so a caller holding a
	// Scroll from somewhere else gets a bar measured against the window that Scroll describes.
	rows := s.Scroll.Rows
	if rows <= 0 {
		rows = h
	}
	// The length is the window over the whole log, and the free span is its ceiling.
	if n = h * rows / (above + rows + below); n > hi-lo {
		n = hi - lo
	}
	if n < 1 {
		n = 1
	}
	// Then the position, distributed over the starts that keep the thumb inside the free span.
	// The halved denominator is a round to nearest rather than a truncation, which is what puts
	// the middle of a log at the middle of the track instead of a row above it.
	top = lo
	if span, d := hi-n-lo, above+below; span > 0 {
		top = lo + (above*span+d/2)/d
	}
	return top, n, true
}

// Grab is what a press on row row of a column h tall means: how far into the thumb the pointer
// landed, to be handed back to Offset for every report after it, and whether the press was on the
// thumb at all. Not ok means there was nothing there to take hold of — a row outside the column, or
// a frame with no bar in it — and the caller has no drag to start.
//
// A press on bare track is a grab too, taken at the middle of the thumb, so a click above or below
// the pill jumps it under the pointer and the drag that may follow carries on from there. The
// alternative — a page per click, the way an old trough worked — needs a press and a drag to mean
// different things, and then a press that turns out to have been the first report of a drag has
// already moved the window a page somewhere the reader never pointed at.
//
// onThumb is reported because those two presses are not the same gesture. Taking hold of the pill
// must move nothing: the reader is reaching for a thing they can already see, and one row of track is
// worth many rows of transcript, so recomputing the offset from the pointer would answer with a
// neighbouring row of the coarse grid and the log would jump the instant it was touched. Only a
// press on bare track is a request to go somewhere.
//
// Whether the pointer is on the thumb is this function's question and not the caller's: the rows the
// thumb covers are bar's arithmetic, and there is one copy of it.
func (s ScrollbarWidget) Grab(h, row int) (grab int, onThumb, ok bool) {
	top, n, ok := s.bar(h)
	if !ok || row < 0 || row >= h {
		return 0, false, false
	}
	if row >= top && row < top+n {
		return row - top, true, true
	}
	return n / 2, false, true
}

// Offset is bar read backwards: the pointer is on row row of a column h tall and took hold of the
// thumb at grab, so which transcript row does the top of the window belong on? Not ok means there is
// no bar, and is ends' answer; every frame that has one can be dragged.
//
// The thumb's top goes where the pointer puts it, clamped to the rows a window can actually start it
// on, which is what makes dragging past either end of the track mean the end of the log rather than a
// row outside it. Then the three cases, which are the three shapes ends can give the track: flush at
// the top means nothing is above, flush at the bottom means nothing is below, and anything between is
// distributed over the rows left between the held-back cells with the same round to nearest that
// placed it.
//
// Those cases are the whole reason this does not call ends. A drag crosses between them — the moment
// the window reaches the top of the log the cell held back up there is released — so the track the
// pointer is moving over is not the track the thumb will be redrawn in. Inverting the frame the press
// started from lands the pill a row out of step with the finger every time an end empties, which is a
// pill that sticks and then catches up, and it happens at exactly the two positions a reader aims for
// most. Measuring against h and n, which no scroll changes, is what makes the pointer's row and the
// thumb's row the same row.
//
// What a drag cannot do is reach every offset. One row of travel is worth d/span rows of transcript,
// so on a long log the window moves in jumps and there are rows no pointer position names. That is a
// bar being a bar rather than a defect: the keys reach every row, and a drag that pretended to would
// have to keep a fraction of a row between frames, which is the one thing this widget will not do.
// The converse is bounded too, and only outside this renderer: a track taller than the window it
// measures — Rows set to something other than h — has track rows that no offset draws a thumb on, and
// then the nearest reachable one is the answer.
//
// Which leaves one freedom, worth naming because a test cannot see it. Where the jumps are long, many
// offsets draw the thumb on the same row, and every one of them puts it under the finger — so rounding
// to nearest rather than up or down does not decide where the pill lands, it decides which of the
// windows behind that row the reader is left looking at. Nearest is the one that agrees with the
// rounding bar itself uses, which is the only reason to prefer it.
func (s ScrollbarWidget) Offset(h, row, grab int) (above int, ok bool) {
	_, n, ok := s.bar(h)
	if !ok {
		return 0, false
	}
	up, down := s.hidden()
	d := up + down
	switch top := min(max(row-grab, 0), h-n); {
	case top <= 0:
		return 0, true
	case top >= h-n:
		return d, true
	default:
		// The interior, whose one held-back cell at each end is why the span is two rows short of
		// the travel and why the row it starts at is one. A span of nothing is a single interior
		// row that every offset in the middle draws, so the middle of them is the honest answer.
		step := d / 2
		if span := h - n - 2; span > 0 {
			step = ((top-1)*d + span/2) / span
		}
		return min(max(step, 1), d-1), true
	}
}

// NoticeWidget is one line of runtime remark that is not part of the transcript,
// which is what "press a key" is: true now, meaningless once the key is pressed.
type NoticeWidget struct {
	Text string
	Warn bool
}

func (NoticeWidget) Name() string   { return "notice" }
func (NoticeWidget) Slot() Slot     { return SlotBelowInput }
func (NoticeWidget) Fallback() Slot { return SlotBottom }
func (NoticeWidget) Animated() bool { return false }

// bottomMargin is one column of air at the left of the two rows that sit at the very foot
// of the frame — the end-of-scenario notice and the status line. Every other row in the
// interface is either inside the input's border or indented by a marker of its own, so
// those two were the only text in the program welded to the first cell of the terminal.
//
// A margin and not a change to marked: the same helper draws the transcript's notices,
// where the indent is load-bearing — a reasoning summary has to line up with the block it
// summarises. This is about where the frame sits in the window, so it is applied by the two
// widgets that are the frame's bottom edge and by nothing else.
const bottomMargin = 1

func (n NoticeWidget) Render(w, _ int, g Glyphs) []Line {
	if n.Text == "" || w <= bottomMargin {
		return nil
	}
	style := "notice.text"
	if n.Warn {
		style = "notice.warn"
	}
	// The margin is taken off the width before the text is wrapped, not added after, so a
	// row that reaches the right edge still reaches it: an indent that borrowed the column
	// from the far side would move the wrap point without saying so.
	out := marked(g.Span("notice.marker", "notice.marker"), n.Text, style, w-bottomMargin)
	for i, l := range out {
		out[i] = append(Line{pad(bottomMargin)}, l...)
	}
	return out
}

// StatusWidget is the row that says what the run is doing while it is doing it. A
// transcript can only say what has already happened, and between two steps of a slow
// tool call it says nothing at all — which from the outside is indistinguishable from
// a program that has hung. This is the row that tells them apart.
//
// Its Phase is why it is built by the app and not by ChromeFor: a spinner has to turn
// on a wall clock, and the only clock state carries is Now, which advances when a
// recorded step lands and therefore stands still for the whole 1400 ms of a tool call
// — exactly when the spinner is the only thing on the screen claiming the run is
// alive. Render(w, h, g) has nowhere to receive one either, so the phase is a field
// and whoever sets the field is the installer.
type StatusWidget struct {
	St *state.State

	// Phase is which frame of the spinner to draw, advanced by the caller. Keeping the
	// clock outside means Render is still a pure function of its arguments, so the same
	// frame number always draws the same row — which is what a test can pin.
	Phase int

	// Shine is the band of light that crosses the word "working", and nothing else in the
	// row: a band that swept the whole line would light the model name and the token counts
	// on its way past, which says the numbers are changing when they are not. Zero is off,
	// so a status row nobody arms draws what it always drew. Its Phase is its own and not
	// this widget's, because the two animations need not be in step — though in the player
	// they are, since one counter feeds both.
	Shine Shimmer
}

func (StatusWidget) Name() string { return "status" }
func (StatusWidget) Slot() Slot   { return SlotBottom }

// Fallback is empty because the bottom row asks nothing of the terminal: it is the last
// row of whatever the frame turned out to be, on either surface and at any width. The
// slot cannot be unavailable, so Place never reaches this — and saying so is better
// than naming a second slot that would be a lie about where the row can end up.
func (StatusWidget) Fallback() Slot { return "" }

// Animated is true exactly when there is a spinner on the screen. Everything else in
// the row changes only when an event lands, and an event already redraws. The shine needs
// no clause of its own: it is drawn on the verb "working", which is on the screen in
// exactly the frames this is already true in.
func (s StatusWidget) Animated() bool { return s.spins() }

// spins is the verb ladder's "working" branch spelled out. Written this way rather
// than as Active alone, Animated and Render cannot disagree: a frame that says
// "waiting" never asks the app for a timer, and a frame the app is ticking always has
// a spinner to show for it.
func (s StatusWidget) spins() bool { return Working(s.St) }

// Working reports whether the run is doing something right now: the state this row spells
// "working", and the exact complement of the state in which the turn is the human's.
//
// It is exported because two places have to agree about it and they are not in the same
// package. This one draws the verb; the program decides from the same answer whether to
// arm the input's shine, whose whole meaning is "the model is not working, it is your
// turn". A program that computed the second half for itself would eventually disagree with
// the first, and the screen would then say both things at once.
func Working(st *state.State) bool {
	return st != nil && st.Blocked == nil && st.Quiescent == "" && st.Active
}

// Render draws the row and gives up whatever does not fit. The segments are in the
// order a reader needs them — what the run is doing, who is doing it, what it has cost
// — and the row is cut from the right, so a 24-column terminal keeps the verb and a
// wide one keeps everything. Dropping by some priority that is not the display order
// would move the segments sideways as the window is dragged, and a status line whose
// fields swap places while you resize is harder to read than a short one.
//
// A segment is drawn whole or not at all: half a token count is worse than none. And
// the row is trimmed rather than padded to the width, because no status style carries a
// background — a run of spaces out to the edge would be a trailing blank in the last
// cell, which is how a terminal is talked into wrapping a row we thought we owned.
//
// The left margin is spent before the fitting starts, so the segments are cut against the
// width they will actually be drawn in; and it is added after the empty check, so a
// terminal too narrow for even the verb still gets no row rather than a row holding one
// space. A lone space would be a trailing blank, which is the one thing this row cannot
// end in.
func (s StatusWidget) Render(w, h int, g Glyphs) []Line {
	if s.St == nil || w <= bottomMargin {
		return nil
	}
	op := statusRow(s.operationalSegments(g, w), w, g)
	if len(op) == 0 {
		return nil // narrower than the verb: no row at all beats a blank one
	}
	rows := []Line{op}
	if h <= 8 {
		return rows
	}
	model := statusRow(s.modelSegments(w), w, g)
	provenance := s.provenanceRow(w)
	if h < 14 {
		if len(model) > 0 {
			return append(rows, model)
		}
		if len(provenance) > 0 {
			return append(rows, provenance)
		}
		return rows
	}
	if len(provenance) > 0 {
		rows = append(rows, provenance)
	}
	if len(model) > 0 {
		rows = append(rows, model)
	}
	return rows
}

func statusRow(segments []Line, w int, g Glyphs) Line {
	room := w - bottomMargin
	sep := Span{Text: " " + g.Get("status.sep") + " ", Style: "status.sep"}
	row, used := Line{}, 0
	for i, seg := range segments {
		cost := seg.Width()
		if i > 0 {
			cost += ansi.StringWidth(sep.Text)
		}
		if used+cost > room {
			break
		}
		if i > 0 {
			row = append(row, sep)
		}
		row, used = append(row, seg...), used+cost
	}
	if len(row) == 0 {
		return nil
	}
	return append(Line{pad(bottomMargin)}, row...).TrimRight()
}

// operationalSegments is the first row: live state first, then the values that
// explain the current run. Narrow terminals replace a member roster with its count.
func (s StatusWidget) operationalSegments(g Glyphs, w int) []Line {
	st := s.St
	out := []Line{s.head(g)}
	if len(st.Members) >= 2 {
		if w < 48 {
			out = append(out, Line{{Text: strconv.Itoa(len(st.Members)) + " agents", Style: "status.text"}})
		} else {
			out = append(out, s.memberCluster(g))
		}
	} else if st.Actor != "" {
		out = append(out, Line{{Text: st.Actor, Style: "status.text"}})
	}
	if st.SpentUSD > 0 || st.BudgetUSD > 0 {
		money := Line{{Text: dollars(st.SpentUSD), Style: "status.text"}}
		if st.BudgetUSD > 0 {
			money = append(money, Span{Text: " of " + dollars(st.BudgetUSD), Style: "status.dim"})
		}
		out = append(out, money)
	}
	if st.Turn > 0 {
		out = append(out, Line{{Text: "turn " + strconv.Itoa(st.Turn), Style: "status.dim"}})
	}
	if st.TokensIn > 0 {
		out = append(out, Line{{Text: compact(st.TokensIn) + " in", Style: "status.dim"}})
	}
	if st.TokensOut > 0 {
		out = append(out, Line{{Text: compact(st.TokensOut) + " out", Style: "status.dim"}})
	}
	return out
}

// segments retains the original one-row vocabulary for callers and focused tests.
// Responsive rendering uses the three semantic builders around it.
func (s StatusWidget) segments(g Glyphs) []Line {
	out := s.operationalSegments(g, 400)
	out = append(out, s.modelSegments(400)...)
	return out
}

func (s StatusWidget) modelSegments(w int) []Line {
	st := s.St
	var out []Line
	if st.Model != "" {
		model := st.Model
		if w < 48 {
			model = shortModel(model)
		}
		out = append(out, Line{{Text: model, Style: "status.text"}})
	}
	if st.ContextCapacity > 0 {
		context := compact(st.ContextUsed) + "/" + compact(st.ContextCapacity)
		if w >= 48 {
			pct := 100 * st.ContextUsed / st.ContextCapacity
			context = "context " + context + " (" + strconv.Itoa(pct) + "%)"
		}
		out = append(out, Line{{Text: context, Style: "status.dim"}})
	}
	if st.Effort != "" {
		label := st.Effort
		if w >= 48 {
			label = "effort " + label
		}
		out = append(out, Line{{Text: label, Style: "status.dim"}})
	}
	return out
}

func (s StatusWidget) provenanceRow(w int) Line {
	st := s.St
	if st.CWD == "" && st.GitBranch == "" {
		return nil
	}
	room := w - bottomMargin
	branch := st.GitBranch
	branchText := ""
	if branch != "" {
		branchText = "⑂ " + branch
	}
	pathRoom := room
	if branchText != "" {
		pathRoom -= ansi.StringWidth(branchText)
		if st.CWD != "" {
			pathRoom -= 3
		}
	}
	path := st.CWD
	if path != "" && ansi.StringWidth(path) > pathRoom {
		path = middleEllipsis(path, pathRoom)
	}
	row := Line{}
	if path != "" {
		row = append(row, Span{Text: path, Style: "status.text"})
	}
	if branchText != "" && ansi.StringWidth(branchText) <= room-row.Width() {
		if len(row) > 0 {
			row = append(row, Span{Text: "   "})
		}
		row = append(row, Span{Text: branchText, Style: "status.dim"})
	}
	if len(row) == 0 {
		return nil
	}
	return append(Line{pad(bottomMargin)}, row...).TrimRight()
}

// memberCluster is one status segment, so the ordinary right-hand cut either
// keeps every member or drops the team entirely. Within it, each member is a
// state glyph joined directly to the blueprint name; busy glyphs use the same
// phase and cycle as the run spinner, so a team works in one rhythm.
func (s StatusWidget) memberCluster(g Glyphs) Line {
	out := Line{}
	for i, m := range s.St.Members {
		if i > 0 {
			out = append(out, Span{Text: " "})
		}
		glyph, style := g.Get("status.member.idle"), "status.member.idle"
		switch {
		case m.Blocked != nil:
			glyph, style = g.Get("status.member.blocked"), "status.member.blocked"
		case m.Error != "":
			glyph, style = g.Get("status.member.failed"), "status.member.failed"
		case m.Busy:
			glyph, style = s.frame(g), "status.member.busy"
		}
		if glyph != "" {
			out = append(out, Span{Text: glyph, Style: style})
		}
		out = append(out, Span{Text: m.Name, Style: "status.member.name"})
	}
	return out
}

// head is the spinner and the verb: the one segment that is always drawn, and the
// reason Render returning nil means "not even this fits" rather than "nothing to say".
//
// The ladder reports the run's own state and only falls through to the player's. A
// finished run that is blocked says what it is blocked on, because that is the useful
// half — the notice one row up already says the recording ended, and repeating it here
// would spend the widest row on the screen agreeing with its neighbour.
func (s StatusWidget) head(g Glyphs) Line {
	out := Line{}
	// An emptied spinner cycle contributes no span at all rather than the bare space its
	// frame would have been followed by: an indent with nothing in it moves the verb a
	// column to the right for a reason no reader can see.
	if s.spins() {
		if f := s.frame(g); f != "" {
			out = append(out, Span{Text: f + " ", Style: "status.spinner"})
		}
	}
	st := s.St
	switch {
	case st.Blocked != nil:
		out = append(out, Span{Text: "blocked", Style: "status.verb"})
		if st.Blocked.On != "" {
			out = append(out, Span{Text: " on " + st.Blocked.On, Style: "status.dim"})
		}
		return out
	case st.Quiescent != "":
		return append(out, Span{Text: "waiting", Style: "status.verb"})
	case st.Active:
		// The one place a shine is drawn in this row, and the band is measured against the
		// word rather than the terminal: seven columns, so what crosses them is a glint on a
		// word and not a wave that happens to be passing through the left of the screen.
		verb := Line{{Text: "working", Style: "status.verb"}}
		return append(out, s.Shine.Apply(verb, verb.Width())...)
	case st.Finished:
		return append(out, Span{Text: "done", Style: "status.verb"})
	}
	return append(out, Span{Text: "idle", Style: "status.verb"})
}

// frame picks the spinner's rune out of the whole declared cycle. The index is by rune
// and not by byte: every frame of the default is a three-byte braille cell, so a byte
// index would cut one in thirds and put a replacement character in the corner of the
// screen. An override with no runes in it disables the spinner instead of panicking,
// which is the same answer NewGlyphs gives an empty value everywhere else.
func (s StatusWidget) frame(g Glyphs) string {
	frames := []rune(g.Get("status.spinner"))
	if len(frames) == 0 {
		return ""
	}
	i := s.Phase % len(frames)
	if i < 0 {
		i += len(frames)
	}
	return string(frames[i])
}

// pad returns n spaces as a single unstyled span, the one piece of layout every
// widget needs and nobody should write twice.
func pad(n int) Span { return Span{Text: strings.Repeat(" ", max(0, n))} }

// compact writes a token count for the corner of a screen. One decimal is worth a
// column at 12.4k, where a reader can watch it move, and worth nothing at 123.4k, where
// the leading digits have already said everything — so the decimal is dropped rather
// than carried to a width that would push every segment behind it one column over.
func compact(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 100000:
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
	default:
		return strconv.Itoa(n/1000) + "k"
	}
}

// dollars always writes both cents, even on a round number. A budget is quoted in cents,
// and a figure that changes width as it fills would move the segments behind it.
func dollars(v float64) string { return "$" + strconv.FormatFloat(v, 'f', 2, 64) }

func shortModel(model string) string {
	for _, prefix := range []string{"claude-", "anthropic/"} {
		model = strings.TrimPrefix(model, prefix)
	}
	return model
}

func middleEllipsis(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	leftWidth := (width - 1) / 2
	rightWidth := width - 1 - leftWidth
	left := ansi.Truncate(text, leftWidth, "")
	runes := []rune(text)
	right := ""
	for i := len(runes) - 1; i >= 0; i-- {
		candidate := string(runes[i:])
		if ansi.StringWidth(candidate) > rightWidth {
			break
		}
		right = candidate
	}
	return left + "…" + right
}
