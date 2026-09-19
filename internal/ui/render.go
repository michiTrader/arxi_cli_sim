package ui

import (
	"strconv"

	"arxi.local/sim/internal/state"
)

// Render is a pure function of state: the same state and the same viewport produce
// the same frame, every time, with no hidden counter of what the terminal has
// already seen. That bookkeeping belongs to Emit, which is the only thing that
// knows what a terminal is. Keeping it out of here is what makes a frame testable
// as a string and what will make the render hook safe to hand to a user.

// Renderer holds the vocabulary and a cache. It is not state: dropping a Renderer
// and building a new one changes nothing but speed.
type Renderer struct {
	Glyphs Glyphs

	// Detail is the output level the transcript draws at, one of the Detail
	// constants. Its zero value is the standard level, which is what a Renderer
	// nobody told about levels has always drawn; the app sets it, and a keypress
	// changes it, because the level is a fact about the reader's wish for this
	// frame and not about the state the frame is drawn from.
	Detail DetailLevel

	// Widgets are the chrome the app installs. The transcript is not in here;
	// everything else is.
	Widgets []Widget

	// Overlay is a floating box drawn on top of the frame. Nil means none.
	Overlay *Overlay

	memo  map[string][]Line
	memoW int
}

// NewRenderer returns a renderer with the shipped glyph set.
func NewRenderer() *Renderer {
	return &Renderer{Glyphs: DefaultGlyphs(), memo: map[string][]Line{}}
}

// Render draws the whole interface.
func (r *Renderer) Render(st *state.State, in *Input, vp Viewport) Frame {
	f := Frame{Width: vp.Width, Height: vp.Height}
	if vp.Width <= 0 {
		return f
	}
	if r.memo == nil || r.memoW != vp.Width {
		r.memo, r.memoW = map[string][]Line{}, vp.Width
	}

	// The whole transcript, every frame. There is no record of what was drawn before
	// and no per-item cursor into it: an item that changed re-wraps, an item that did
	// not comes back from the memo, and the order blocks seal in stops mattering — it
	// used to decide what could be handed to scrollback, and now nothing is.
	//
	// tw is where the prose stops, which on a surface that can hold a side column is two
	// columns short of the frame. It is spent whether or not a side widget is installed, so
	// that a scrollbar appearing does not re-wrap the screen the reader is in the middle of.
	tw := TranscriptWidth(vp)
	rows, _ := r.transcript(st, tw)

	// SlotTop follows the transcript and every other slot keeps the full width. The top row is
	// the pinned copy of a message that is also in the transcript, drawn through the same helper
	// with the same wash to the same width — a quotation two columns wider than the thing it
	// quotes is a different row, and the band would run under the column beside it. The chrome
	// below the transcript answers to the frame instead: the input's border, the status line and
	// the notices are the foot of the window, not neighbours of the prose.
	var top []Line
	var topChanges []visualRange
	if vp.FixedTop {
		top, topChanges = r.slot(SlotTop, vp, tw)
	}
	above, aboveChanges := r.slot(SlotAboveInput, vp, vp.Width)
	below, belowChanges := r.slot(SlotBelowInput, vp, vp.Width)
	bottom, bottomChanges := r.slot(SlotBottom, vp, vp.Width)

	var inputRows []Line
	cur := Cursor{Hidden: true}
	gap := 0
	// A row of air between the transcript and the widgets above the input: the last
	// line of prose and the first line of chrome are about different things, and a
	// recap or task list butted against the prose reads as a continuation of it.
	sep := 0
	if len(above) > 0 {
		sep = 1
	}
	if in != nil {
		if len(rows) > 0 || len(top) > 0 || len(above) > 0 {
			gap = 1
		}
		inputRows, cur = in.Render(vp.Width, r.Glyphs)
	}
	chrome := len(top) + len(above) + sep + gap + len(inputRows) + len(below) + len(bottom)

	// Committed rows are rows the terminal keeps for good, and while a session is
	// running there are none.
	//
	// The reason is a unit mismatch no amount of care survives. A count of rows is a
	// count taken at one width; resize the terminal and the same prose wraps into a
	// different number of rows, so the count the emitter is holding and the count this
	// frame reports are not the same measurement, and every relation between them — is
	// this row new, how far above me does the region start — is wrong by however much
	// the text re-wrapped. A row already in the terminal's history cannot be repaired,
	// so the drag prints the paragraph again, once per step of the drag, and it stays.
	//
	// Owning every visible row of the main screen was tried as the answer and it is not
	// one. The visible rows are repainted correctly; the trouble is the rows that stop
	// being visible. A reflowing terminal re-wraps the main screen on every step of a
	// drag and pushes whatever no longer fits above the top edge, into scrollback, where
	// no erase of ours reaches — so a slow narrowing buries the conversation under a
	// layer of half-wrapped debris per step even though every frame we sent was correct.
	// The only sequences that would clean that up are the ones that eat the user's own
	// history, and those are banned.
	//
	// So an interactive frame is drawn on the alternate buffer, which no terminal
	// reflows: a resize there is one repaint at a new width, and nothing can leak out of
	// it. The transcript reaches the terminal exactly once, at the end, wrapped to the
	// width the session ended on. vp.Height <= 0 is that end, and every other case where
	// the caller wants a document rather than a screen: a fold, a pipe, a golden file.
	// Nothing is being held back there, so the whole transcript is history and all of it
	// is committed, and there is no screen to fill.
	if vp.Height <= 0 {
		f.Committed, rows = rows, nil
	}

	// The window into the transcript. A screen holds whatever is left over after the
	// chrome, and which rows those are is the one thing here the human decides: the tail
	// by default, and wherever they scrolled to once they have scrolled. Rows outside it
	// are not lost — they come back when the view moves or the screen grows, and all of
	// them arrive at the end — they are simply not on screen now, the way the top of a
	// long file is not on screen in a pager.
	//
	// The clamp is silent on purpose. A page key near the end of the log asks for a row
	// past the end, and the honest answer is the last window rather than an error. What
	// makes that safe is that the offset actually used goes back out in f.Scroll, so the
	// caller stores the clamped truth instead of its own guess and the view cannot drift
	// a page further away with every press.
	keep := len(rows)
	if vp.Height > 0 {
		if keep = vp.Height - chrome; keep < 0 {
			keep = 0
		}
	}
	first := len(rows) - keep
	if vp.Scrollback {
		if vp.Scrolled {
			// The caller's seam: this exact row at the top of the window, clamped only by
			// the log itself. A seam is how the window is laid back onto the screen after
			// something else painted over it, and it may end short of the tail — the rows
			// under it are the ones about to be committed, and the frame after this one
			// moves the window down over them.
			first = min(vp.ScrollTop, len(rows))
		} else {
			// The tail, and never behind it. History is written from the screen's top row,
			// so the top may not retreat over a row that is already history: ScrollTop is
			// the floor the caller keeps at the last window top, and a screen that grew —
			// a keyboard closed on a phone — would otherwise walk the top back over rows
			// the history holds. The window then comes out shorter than the screen, and
			// the fill below the input bar takes up the difference, the way a young
			// conversation always has.
			first = len(rows) - keep
			if floor := min(vp.ScrollTop, len(rows)); floor > first {
				first = floor
			}
		}
	} else if vp.Scrolled {
		first = vp.ScrollTop
		if last := len(rows) - keep; first > last {
			first = last
		}
	}
	if first < 0 {
		first = 0
	}
	window := rows[first:]
	if len(window) > keep {
		window = window[:keep]
	}
	f.Scroll = Scroll{Above: first, Below: len(rows) - first - len(window), Rows: keep}
	f.Scrollback = vp.Scrollback

	// Where that column is, for whoever has to turn a pointer into a row of it. The rows are the
	// window's own, so this is empty on every frame the column is — and it cannot be stale by the
	// time the trim below runs: dropping rows from the top needs keep to have reached zero, which
	// is the same thing as a window with no rows in it.
	if w, h := sideWidth(vp), len(window); w > 0 && h > 0 {
		f.Side = Rect{X: tw, Y: len(top), W: w, H: h}
	}

	// Then the side column, painted into the columns the transcript gave up. This fills a gap
	// rather than widening anything: tw already took the columns out of every row above, so a
	// row is padded back out to tw and the column's own row appended, and the result is exactly
	// vp.Width. Which is also why the height handed to the column is len(window) and not keep —
	// a window shorter than the screen happens exactly when the whole log fits, and a column
	// told it was taller than the rows beneath it would draw a bar into the padding.
	//
	// A row the column says nothing about is left exactly as short as it was. That is the rule
	// that keeps a frame free of trailing blanks: padding a row out to tw and stopping there
	// would end it in bare air, which is how a terminal is talked into wrapping a row we own.
	//
	// The copy is the one line here that no test can catch, and it stays anyway. A window row is a
	// Line value straight out of the memo, so appending to it in place writes into an array the
	// cache owns. Today that is invisible — each memo Line owns its own array, and the write lands
	// past that Line's length where nobody reads — but it is invisible by accident, not by rule:
	// the day a block renders its rows by slicing one buffer, row i's spare capacity is row i+1's
	// spans, and every frame after the first draws the last frame's bar. Copying costs one row.
	if col, next := r.side(vp, len(window), f.Scroll); len(col) > 0 {
		f.NextVisualChange = sooner(f.NextVisualChange, next)
		for i := range window {
			if i >= len(col) || len(col[i]) == 0 {
				continue
			}
			row := append(Line{}, window[i]...)
			if p := tw - row.Width(); p > 0 {
				row = append(row, pad(p))
			}
			window[i] = append(row, col[i]...)
		}
	}

	var changes []visualRange
	live := append([]Line{}, top...)
	changes = appendVisualRanges(changes, topChanges, 0)
	live = append(live, window...)
	changes = appendVisualRanges(changes, aboveChanges, len(live))
	if sep > 0 {
		live = append(live, Line{})
	}
	live = append(live, above...)
	if in != nil {
		if gap > 0 {
			live = append(live, Line{})
		}
		cur.Line += len(live)
		f.Cursor = cur
		if len(inputRows) > 0 {
			changes = append(changes, visualRange{len(live), len(live) + len(inputRows), visualChange(in)})
		}
		live = append(live, inputRows...)
	} else {
		f.Cursor = cur
	}
	changes = appendVisualRanges(changes, belowChanges, len(live))
	live = append(live, below...)
	changes = appendVisualRanges(changes, bottomChanges, len(live))
	live = append(live, bottom...)

	// Then fill the screen out to its height, because a frame that stops short is a
	// frame with rows outside it, and rows outside the frame are the bug. The padding
	// goes at the bottom, after the input bar: a young conversation sits at the top of
	// the screen and grows downward until it reaches the last row, which is what the
	// terminal this is compared against looks like. A padded row is genuinely blank —
	// the emitter erases every row it paints — so this is how the interface claims the
	// rows it is not using yet instead of leaving them to whatever was there before.
	//
	// Every surface with a height is padded, alternate buffer included. The emitter used
	// to pad the alternate screen itself, against its own idea of the height and with a
	// newline per row, and that divergence was a bug rather than a shortcut: a frame one
	// row too tall scrolled the buffer. Fitting the frame to the surface is this
	// function's job in every mode, and the emitter's job is to paint what it is given.
	if vp.Height > 0 {
		for len(live) < vp.Height {
			live = append(live, Line{})
		}
		// And gives up rows from the top when the chrome alone is taller than the screen.
		// keep has already gone to zero by then, so every row left is chrome, and there is
		// more of it than there used to be: an open approval, a prompt wrapped inside its
		// box, an end-of-scenario notice and a status row are thirteen rows on a twelve-row
		// terminal. Something has to go and it is the top, because the input and the row
		// under it are what the human is doing right now. Without this the frame is taller
		// than the height it reports, which on the alternate buffer scrolls the buffer by
		// the difference — the one thing a repaint is not allowed to do.
		//
		// The cursor moves with the rows and stops at the top edge. It only reaches that
		// clamp when the input's own rows are being cut, which means the human has typed a
		// prompt taller than their terminal; the cursor then sits on the first row still
		// visible, which is a later row of the same prompt.
		if d := len(live) - vp.Height; d > 0 {
			live = live[d:]
			changes = trimVisualRanges(changes, d)
			f.Cursor.Line = max(0, f.Cursor.Line-d)
			// On a scrollback surface the trim has to say what it ate, because the emitter
			// commits the rows the screen's top leaves behind and the screen's top has just
			// moved. Window rows eaten are rows the screen no longer shows and history is
			// about to hold, and naming them keeps the commit exact. Once the trim reaches
			// the chrome the top of the screen is not a transcript row at all, and no honest
			// answer exists — a negative Above stands the emitter down until a frame that
			// fits names a real row again.
			if vp.Scrollback {
				if cut := min(d, len(window)); cut == d {
					f.Scroll.Above += d
				} else {
					f.Scroll.Above = -1
				}
			}
		}
		f.NextVisualChange = visualRangesNext(changes, len(live))
	}
	f.Live = Composite(live, r.Overlay, vp.Width, r.Glyphs)
	return f
}

// transcript lays every item out in width columns and returns the rows, plus the row each
// item's first line landed on. It is the one place the transcript's shape is decided, so
// that a caller asking where an item is gets the same arithmetic the frame was built with
// rather than a second opinion that drifts from it.
//
// The blank between two items belongs to the layout and not to either item: one row before
// every item after the first, so two turns never touch. It is charged to the item that
// follows it, which is why a start is the row of that item's first *content* line and not
// the separator above it — a jump landing on the separator would put a blank row at the top
// of the window and the landmark one row below it.
//
// An item that drew nothing gets -1. It occupies no row, so there is no row to name, and
// handing back the next item's would point a landmark at the wrong turn. The separator is
// still counted by index and not by whether anything has been emitted yet, which is what
// keeps a transcript whose first item is invisible starting with the blank it always has.
//
// The starts are ascending for every item that drew something, because rows only grows.
// Callers rely on that to stop at the first match.
func (r *Renderer) transcript(st *state.State, width int) ([]Line, []int) {
	starts := make([]int, len(st.Items))
	var rows []Line
	for i, it := range st.Items {
		starts[i] = -1
		lines := r.blockLines(it, width)
		if len(lines) == 0 {
			continue
		}
		if i > 0 {
			rows = append(rows, Line{})
		}
		starts[i] = len(rows)
		rows = append(rows, lines...)
	}
	return rows, starts
}

// ItemSpans reports where every item lands: the row each item's first content line
// starts on, and the rows its own content wrapped to. starts is transcript()'s own
// answer, an item that drew nothing at -1, and heights[i] is the length of the same
// layout transcript charged to that item — the blank between two items belongs to
// neither, so the two never sum past the next start.
//
// It exists for the one caller that moves the reading position across a change that
// re-flows the whole transcript. A detail-level toggle adds and removes rows in the
// middle of the conversation, so a row number saved before the change names a
// different sentence after it; an item index and an offset inside that item survive
// the re-wrap, and can be solved against the new geometry for the same place. The
// measurement is fresh for the same reason PromptRows' is: the same items at a
// different level or a different width are different rows, and a cached row survives
// neither as a right answer.
func (r *Renderer) ItemSpans(st *state.State, vp Viewport) (starts, heights []int) {
	width := TranscriptWidth(vp)
	if st == nil || width <= 0 {
		return nil, nil
	}
	_, starts = r.transcript(st, width)
	heights = make([]int, len(st.Items))
	for i, it := range st.Items {
		heights[i] = len(r.blockLines(it, width))
	}
	return starts, heights
}

// PromptRows is where the reader's own turns are, in the rows Frame.Scroll counts.
//
// It exists so that "take me to my last message" can be a scroll offset. The app knows
// where the window is — Frame.Scroll.Above, in transcript rows — and nothing else knows
// how many rows a conversation wraps to at this width, so the question has to be asked
// here. The answer is ascending, which is what lets a caller walk it in either direction
// with one comparison, and it is a fresh measurement every time: the same items at a
// different width are at different rows, and a cached row survives a resize as a wrong
// answer.
//
// It takes the viewport rather than a width for the same reason it takes the state rather than
// a row count: the caller is not the one who knows. A surface holding a side column wraps its
// prose two columns short of the frame, so a caller passing its own width would be handed rows
// measured against a transcript the renderer never drew — right until the day the column was
// reserved, and silently a row or two out from then on. Asking the viewport keeps the answer
// and the frame the same measurement.
func (r *Renderer) PromptRows(st *state.State, vp Viewport) []int {
	width := TranscriptWidth(vp)
	if st == nil || width <= 0 {
		return nil
	}
	_, starts := r.transcript(st, width)
	var out []int
	for i, it := range st.Items {
		if it.Kind == state.KindPrompt && starts[i] >= 0 {
			out = append(out, starts[i])
		}
	}
	return out
}

// PromptInside is the reader's turn the top of the window has come to rest inside: its text,
// and how many of that turn's own rows have gone past the top edge. A zero count means none
// have, and therefore that there is nothing to pin — what the reader would be told is already
// on the screen, or there is no turn of theirs up there at all.
//
// It is the question a pinned header asks — "which of my turns am I reading?" — and it is
// asked here for PromptRows' reason: which row an item landed on depends on how its text
// wrapped, so the only honest answer is a fresh measurement at this width. Both walk the same
// ascending starts, and this one keeps the last turn that begins at or above the window.
//
// A turn is claimed from its first row until the next turn's, so the header stays put all the
// way down a long answer instead of blinking out one row below the question. Which turn owns
// the window's first row is settled by that span alone and not by how tall the question was:
// a row that is the *start* of a turn belongs to that turn, so a jump — which lands exactly
// there — reports a count of zero and pins nothing, because the landmark is on screen already.
//
// The count is what keeps that true one row further on. It is the rows the reader has actually
// lost, capped at the question's own height, so the header can repeat those and only those,
// and can never print a line that is also in the transcript directly under it.
//
// The empty string and a zero count are the answer to "there is no such turn" and to "that
// turn is blank" alike, which do not need telling apart: neither is a landmark, and the widget
// draws nothing for either.
//
// The viewport, and not a width, for PromptRows' reason: the transcript's width is the renderer's
// to know, and a caller that computed it would eventually compute it differently.
func (r *Renderer) PromptInside(st *state.State, vp Viewport, row int) (string, int) {
	width := TranscriptWidth(vp)
	if st == nil || width <= 0 {
		return "", 0
	}
	_, starts := r.transcript(st, width)
	text, hidden := "", 0
	for i, it := range st.Items {
		if it.Kind != state.KindPrompt || starts[i] < 0 {
			continue
		}
		if starts[i] > row {
			break // this turn begins below the top edge, and so does every turn after it
		}
		// Capped at the turn's own height, because past that the rows above the window belong to
		// the answer and not to the question: the count is how much of the *prompt* is lost.
		text, hidden = it.Text, min(row-starts[i], len(r.blockLines(it, width)))
	}
	return text, hidden
}

// blockLines renders one item, through the cache when the item can no longer
// change. The key carries the width because a cached line is only valid for the
// width it was wrapped to, and the output level beside it for the same reason:
// two levels of one item are two different answers, and a cache that could not
// tell them apart would draw the level the reader just left. An item with no id is
// never cached at all: a cache keyed on a shared id draws the second item with the
// first one's text, which is a corrupt transcript, whereas re-wrapping it costs a
// few microseconds.
func (r *Renderer) blockLines(it state.Item, width int) []Line {
	level := r.Detail.OrStandard()
	b := BlockFor(it)
	if ib, ok := b.(ItemBlock); ok {
		ib.Detail = level
		b = ib
	}
	if !b.Sealed() || it.ID == "" {
		return b.Render(width, r.Glyphs)
	}
	key := it.ID + "|" + strconv.Itoa(int(it.Kind)) + "|" + strconv.Itoa(int(level)) + "|" + strconv.Itoa(width)
	if lines, ok := r.memo[key]; ok {
		return lines
	}
	lines := b.Render(width, r.Glyphs)
	// A zero-value Renderer has no map, and this is reachable without Render having run
	// first: PromptRows answers a keypress and does not build a frame.
	if r.memo == nil {
		r.memo, r.memoW = map[string][]Line{}, width
	}
	r.memo[key] = lines
	return lines
}

// slot collects everything that resolved into one slot, in the order the caller
// installed it, and draws it w columns wide.
//
// The width is a parameter and not vp.Width because not every slot is as wide as the frame:
// the row above the transcript is a quotation of a row inside it and has to wrap where that
// one wrapped, while the rows below the transcript belong to the frame's own foot. Passing it
// in keeps that decision at the one call site that knows which kind of row it is asking for.
//
// Widgets are exactly what is in r.Widgets and the renderer adds none of its own.
// It used to synthesize the approval question out of state here, which drew it
// twice once the app started installing it too, and the deeper problem was not the
// duplicate: a widget conjured inside a pure render pass cannot be moved,
// restyled or switched off from a config, and this layer is the one that has to
// stay handable to a user. ChromeFor is that logic, now callable by whoever owns
// the list.
type visualRange struct {
	from, to int
	next     int
}

func appendVisualRanges(dst, src []visualRange, offset int) []visualRange {
	for _, change := range src {
		change.from += offset
		change.to += offset
		dst = append(dst, change)
	}
	return dst
}

func trimVisualRanges(changes []visualRange, rows int) []visualRange {
	out := changes[:0]
	for _, change := range changes {
		change.from -= rows
		change.to -= rows
		if change.to > 0 {
			change.from = max(0, change.from)
			out = append(out, change)
		}
	}
	return out
}

func visualRangesNext(changes []visualRange, height int) int {
	next := 0
	for _, change := range changes {
		if change.next > 0 && change.from < height && change.to > 0 {
			next = sooner(next, change.next)
		}
	}
	return next
}

func (r *Renderer) slot(s Slot, vp Viewport, w int) ([]Line, []visualRange) {
	var out []Line
	var changes []visualRange
	for _, wd := range r.Widgets {
		got, ok := Place(wd, vp)
		if !ok || got != s {
			continue
		}
		rows := wd.Render(w, vp.Height, r.Glyphs)
		if len(rows) == 0 {
			continue
		}
		from := len(out)
		out = append(out, rows...)
		changes = append(changes, visualRange{from, len(out), visualChange(wd)})
	}
	return out, changes
}

// side is the column beside the transcript, h rows tall, or nothing.
//
// The first widget that resolved into the right slot owns the column outright, and any second
// one is left out. A column is one rectangle rather than a stack of rows: two widgets in it
// would each be handed the same full height and the caller would then have to guess which of
// the two overlapping answers belongs on row nine. Dropping the second is a limit that can be
// stated; drawing both is a frame nobody can reason about.
//
// Scroll is offered here through ScrollReader rather than passed to Render, because it is the
// one thing a widget cannot be given by whoever installed it — see ScrollReader. A widget that
// does not want it is rendered exactly as it would have been anywhere else.
//
// SlotLeft is deliberately not drawn, as it has not been until now. Reserving on both sides
// would mean indenting the transcript as well as trimming it, and therefore indenting the
// pinned row above it and re-deriving every landmark against a left edge that is no longer
// column zero. There is no widget asking for that yet, and untested generality is worse than
// a stated limit.
func (r *Renderer) side(vp Viewport, h int, sc Scroll) ([]Line, int) {
	w := sideWidth(vp)
	if w <= 0 || h <= 0 {
		return nil, 0
	}
	for _, wd := range r.Widgets {
		got, ok := Place(wd, vp)
		if !ok || got != SlotRight {
			continue
		}
		if sr, ok := wd.(ScrollReader); ok {
			wd = sr.WithScroll(sc)
		}
		rows := wd.Render(w, h, r.Glyphs)
		if len(rows) == 0 {
			return nil, 0
		}
		return rows, visualChange(wd)
	}
	return nil, 0
}
