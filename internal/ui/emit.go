package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Emit is the other side of the seam. Render knows nothing about terminals; this
// file knows nothing about state. Between them passes a Frame, which is why the
// whole renderer is testable without a tty and why porting this simulator into
// arxi means rewriting one file.
//
// An Emitter is stateful on purpose, and the state is deliberately tiny: whether a
// surface has been claimed, how many committed lines the terminal has already been
// given, and how tall the last frame was. It used to carry more — the height of the
// live region, the caret's row and column, the display width of every row on screen —
// so that a resize could work out how far up to walk before erasing. That is gone, and
// its absence is the fix rather than a tidy-up: each of those was a count of wrapped
// rows taken at a width that had already changed by the time it was used, and a walk
// measured with them lands somewhere else, every frame, for the whole length of a
// drag. Now the frame names the absolute rows to paint and the arithmetic has nowhere
// left to be wrong. Render cannot hold even this much, because Render is pure and
// hands over the whole transcript every frame; the terminal is the thing with a memory
// here.

// Mode is where we are drawing. It exists only in this file.
type Mode uint8

const (
	// ModeInline draws under the shell prompt, and it is the streaming mode: a pipe, a
	// fold, --instant. It scrolls the screen clear on the first frame and owns every
	// visible row from then on, but it never addresses a row above the screen, so the user
	// keeps their scroll history, their selection and their copy buffer; the transcript
	// joins that history at the end of the session, in one piece, at the width the session
	// ended on.
	//
	// What it cannot survive is a resize in a terminal that reflows, and that is why it is
	// no longer the default. The rows it paints are main-screen rows: a reflowing terminal
	// re-wraps them on every step of a drag and pushes whatever no longer fits above the
	// top edge, into scrollback, where no erase of ours reaches. Every frame we send is
	// correct and the debris piles up anyway, one layer per step. The only sequences that
	// would clean it up are the ones that eat the user's own history, and those are banned.
	//
	// It stays because it is the right answer for output nobody is dragging: it needs no
	// alternate buffer, it degrades to plain text, and what it writes is what a pipe wants.
	ModeInline Mode = iota
	// ModeAlt owns the screen through the alternate buffer, and it is the interactive mode.
	// No terminal reflows the alternate buffer: a resize there is one repaint at a new
	// width, and nothing can leak out of it into the user's history. That is what makes a
	// drag survivable, and it is the same property that makes a fixed top band, side panels
	// and a scroll offset possible — the surface holds still, so the frame is the only
	// description of it. The user's history is untouched until the session ends, where the
	// transcript is printed onto the main screen in one piece.
	ModeAlt
)

// Profile is how much colour the terminal can be trusted with. Downgrading is an
// emit concern and nothing else: a theme names colours, and this is the only code
// allowed to decide that 24-bit teal has to become colour 6 because we are in a
// 16-colour tty over ssh.
type Profile uint8

const (
	ProfileTrueColor Profile = iota
	Profile256
	ProfileANSI
	ProfileMono
)

// Emitter turns frames into bytes for one terminal session.
type Emitter struct {
	Theme   *Theme
	Mode    Mode
	Profile Profile

	// Mouse asks for the wheel and pays for it with the drag. The zero value is off, which
	// makes selecting text the plain gesture it is on a machine with a pointer; see Enter for
	// the whole of that trade, which is the one thing on this struct a reader is likely to want
	// back — and for the terminal where there is no drag to pay with and the caller turns this
	// on by itself.
	Mouse bool

	committed int  // committed lines already flushed to the user's history, as a high-water mark
	claimed   bool // a surface is taken and every row on it is ours: the alternate buffer, or the screen
	rows      int  // how many rows the last frame painted, which is the screen's height
	started   bool
	pushed    bool // an entry of ours is on the terminal's keyboard stack, and only we may pop it
}

// modeAlternateScroll is DECSET 1007: the terminal's own translation of a wheel notch into
// arrow keys while the alternate buffer is up. x/ansi has constants for the mouse modes but
// not for this one, so it is spelled here once rather than as a number at each use.
const modeAlternateScroll = ansi.DECMode(1007)

// Enter is what we send before the first frame.
//
// In alt mode this is where the claim happens, and it happens here rather than in claim()
// because CSI ? 1049 h is not idempotent: it saves the cursor on the way in, so sending it
// twice overwrites the saved main-screen position with a position on the alternate screen
// and the restore at the end lands somewhere meaningless. Taking the buffer exactly once,
// at the one moment we know is the first, is the only way to be sure of that. Nothing is
// erased here — 1049h clears the alternate buffer itself, and paintScreen erases every row
// it addresses — which keeps "we never send CSI 2 J" true of every mode rather than of one.
//
// The mouse is left to the terminal in alt mode, and that is a reversal: tracking used to be
// claimed here so that a notch would arrive as a report we decode ourselves. It was claimed
// for a real reason and given up for a better one, and the trade is worth writing down because
// the sequences below are unreadable without it.
//
// Three things want the mouse and only two can have it. A terminal hands a program the mouse
// with tracking (CSI ? 1002 h, plus CSI ? 1006 h for coordinates past column 223), and from
// that moment a press belongs to the program: term.Decode names a notch "wheelup" and the
// arrow keys stay free for the input's history, but drag-to-select is gone and every terminal's
// way back to it is the same — hold shift. Without tracking the terminal keeps the press, so a
// drag selects and copies the way it does in every other program, and the wheel is whatever
// alternate scroll (CSI ? 1007) says: on, the terminal rewrites a notch into the arrow keys a
// reader would have pressed, before we see it and with no byte to tell a notch from a finger.
// That is not a wheel we can use, it is the input's history being walked by a mouse.
//
// So: the wheel is given up, because selecting text is the gesture a reader makes more often
// and the wheel has keyboard spellings that tracking cannot take away — ctrl+up and ctrl+down
// move a notch, alt+up and alt+down a row, pgup and pgdown a screen, ctrl+home and ctrl+end the
// ends. 1007 is switched off rather than left alone, which is the one sequence here that is not
// obvious: leaving it on would not give us a wheel, it would give us arrows nobody pressed.
// -mouse takes the old arrangement back for a reader who would rather have the wheel and hold
// shift, and it is a flag and not a config setting for the same reason -ascii is: it is a fact
// about the terminal in front of the person, not a taste.
//
// Every clause of that trade assumes a pointer, and on a phone none of them hold — which is
// why the caller, and not this file, decides: term.IsTermux is asked, and Mouse is turned on
// under Android before the first frame. A finger has no drag to lose, because Termux selects
// with a long press and its own handles and never asks a program's permission for it; and the
// 1007 above is a mode Termux does not implement, so an unclaimed swipe is not a notch that
// does nothing, it is arrow keys — the ones a reader would have pressed, byte for byte, so
// nothing here or in term.Decode can tell a swipe from a press of up, and the input's history
// walks under a thumb that asked to scroll. Tracking is the only fix that exists there, and it
// costs a phone nothing at all. -mouse=false is still the way to say otherwise.
//
// Button events and not any-motion (CSI ? 1003 h) when tracking is asked for, which would
// report every bare mouse move across the pane and buy nothing this needs.
//
// In inline mode it is almost nothing, which is the point: we have not taken the terminal
// away from the user, we are just about to print under their prompt. The wheel there is the
// terminal's own scrollback and moving it is the terminal's job, so neither mouse mode nor
// 1007 is touched at all — and 1007 has no effect on the main screen anyway.
//
// What is claimed in both modes is how input arrives, and that is the line the two halves of
// this function are drawn on: the screen and the mouse belong to a surface, while bracketed
// paste and the keyboard belong to the reader, who is the same person either way.
//
// The keyboard is asked to disambiguate, and that request is what makes shift+enter a second
// line in the input rather than a sent message. In the encoding every terminal has shipped
// with since VT100 it is 0x0d, the same byte as enter, because the modifier on a return key is
// dropped before anything leaves the terminal — so the chord every editor binds to "new line"
// cannot be told from the one that sends. ctrl+enter escapes that without any protocol on the
// terminals that fold it into the control range instead of dropping it, Windows Terminal and
// conhost among them: 0x0a, which is ctrl+j, which app.DefaultBindings binds. shift+enter has
// no byte of its own anywhere. CSI > 1 u pushes the first flag of the Kitty keyboard protocol,
// and a terminal that speaks it reports shift+enter as CSI 13;2u and ctrl+enter as CSI 13;5u,
// which term.Decode already names. One flag and not the whole set, on purpose: the second asks
// for key-release events too, and every binding would fire twice.
//
// A terminal that has never heard of it ignores an unknown private sequence, which is the
// entire cost of asking — six bytes, and shift+enter stays 0x0d there. What it is not is a
// mode, and that difference has teeth: this is a stack, so Exit has to take our entry back
// off, and popping one we never pushed would take the keyboard out from under whatever
// program launched us. e.pushed is what keeps the two symmetric.
func (e *Emitter) Enter() []byte {
	e.started = true
	var b strings.Builder
	if e.Mode == ModeAlt {
		b.WriteString(ansi.SetMode(ansi.ModeAltScreenSaveCursor))
		e.claimed = true
		if e.Mouse {
			b.WriteString(ansi.SetMode(ansi.ModeMouseButtonEvent))
			b.WriteString(ansi.SetMode(ansi.ModeMouseExtSgr))
		} else {
			b.WriteString(ansi.ResetMode(modeAlternateScroll))
		}
	}
	b.WriteString(ansi.SetMode(ansi.ModeBracketedPaste))
	b.WriteString(ansi.PushKittyKeyboard(ansi.KittyDisambiguateEscapeCodes))
	e.pushed = true
	return []byte(b.String())
}

// Exit undoes Enter. It runs from a deferred call and from the signal handler, so
// it must be safe to send twice and must never depend on the last frame having
// been well formed: whatever went wrong, the terminal has to come back usable.
func (e *Emitter) Exit() []byte {
	var b strings.Builder
	b.WriteString(ansi.ResetMode(ansi.ModeBracketedPaste))
	// The keyboard entry comes off before anything else, because a shell prompt drawn under a
	// keyboard still in CSI u is a shell whose every arrow key is a sequence it cannot read.
	// Guarded, unlike the modes around it: a pop is not an off switch but a step down a stack
	// we share with whoever launched us, and Exit runs from a defer and from a signal handler,
	// so an unguarded one would eventually take somebody else's entry.
	if e.pushed {
		b.WriteString(ansi.PopKittyKeyboard(1))
		e.pushed = false
	}
	b.WriteString(ansi.SetMode(ansi.ModeTextCursorEnable))
	// Auto-wrap is switched off for the length of an absolute paint and back on at the
	// end of it. Sending it again here costs four bytes and covers the one case that
	// order cannot: a signal, a panic or a short write between the two, which would
	// otherwise hand back a terminal where the shell's own long lines are clipped.
	b.WriteString(ansi.SetMode(ansi.ModeAutoWrap))
	b.WriteString("\x1b[m")
	if e.Mode == ModeAlt {
		if e.Mouse {
			// Tracking goes off in the reverse order it went on, and it goes off unconditionally.
			// Twelve bytes cover the one state a restore must not preserve — us and the terminal
			// disagreeing after a signal or a short write, which is a shell whose every click is
			// swallowed as a report — and no terminal has tracking on by default, so switching off a
			// mode nobody turned on takes nothing from whoever runs next.
			b.WriteString(ansi.ResetMode(ansi.ModeMouseExtSgr))
			b.WriteString(ansi.ResetMode(ansi.ModeMouseButtonEvent))
		} else {
			// Alternate scroll goes back on because Enter switched it off, and this is the one
			// sequence in Exit that is a guess rather than an undo: 1007 is a terminal-wide
			// preference with no way to read the old value back, so "on" is the answer chosen for
			// it — the default nearly every terminal ships, and the one that leaves a wheel in the
			// next reader's less and vim. The guess is bounded by the mode itself: 1007 only does
			// anything while a program has the alternate buffer up, so being wrong costs a reader
			// who had turned it off nothing outside those programs, and one setting to put back.
			b.WriteString(ansi.SetMode(modeAlternateScroll))
		}
		// Only while the buffer is still ours. A session that ended properly handed its
		// transcript over, and the handover is what left the alternate buffer — 1049l is as
		// non-idempotent as 1049h, and a second one restores the caret to the shell prompt
		// the session started from, which is above the transcript we just printed. The shell
		// would then draw its next prompt straight through it.
		if e.claimed {
			b.WriteString(ansi.ResetMode(ansi.ModeAltScreenSaveCursor))
		}
	} else if e.started {
		// A session that ended properly handed its transcript over, which un-claimed the
		// screen and left the caret under the last row of it. A session that did not — a
		// signal, a panic, a short write — leaves the caret parked wherever the input bar
		// was, in the middle of a screen we painted, and the shell prompt would come up
		// through the interface. Stepping to the bottom row first costs nothing and puts
		// the prompt under it instead of inside it.
		if e.claimed && e.rows > 0 {
			b.WriteString(ansi.CursorPosition(1, e.rows))
		}
		b.WriteString("\r\n")
	}
	e.claimed, e.started = false, false
	return []byte(b.String())
}

// Emit draws one frame.
//
// One algorithm for both surfaces, which is the point of the mode being a field and not a
// fork. The split between committed and live rows is the whole design: live rows are a
// claim we repaint whole every frame, and a committed row is one we have promised never to
// address again. Inside a session a frame declares nothing committed, so this is a repaint
// in place, every time, and a resize is nothing more than a repaint at a different width.
// The committed write happens once, on the frame that has no live region left to hold, and
// it is the same handover in both modes. What the mode decides is only what the claim is
// made of: the alternate buffer, or the visible screen.
//
// There used to be a second body for alt that walked rows with newlines and cut the
// transcript's head off to fit. It is gone. Every line of it was a different way of saying
// "the emitter decides what is on screen", and each one was a decision Render is the only
// thing with enough information to make — where the window into the transcript starts, how
// tall it is, what fills the rows nobody claimed.
//
// Nothing can scroll while the session runs, and that is a property rather than a detail.
// Each row is reached with CUP, which every terminal clamps to the screen, so a frame laid
// out for a geometry that changed a microsecond ago cannot push a row off the top;
// auto-wrap is off around the loop, so a row too wide for the terminal is clipped at the
// margin instead of wrapping and shoving the bottom row into history. A stale frame then
// costs one ugly repaint that the next frame overwrites, where a scrolled one costs a row
// we can never address again.
//
// So the user's history is written in exactly two places: the inline claim, which puts
// their own screen there once, and the handover, which puts the transcript there once.
// CSI 2 J, CSI 3 J and RIS are never sent, in either mode — erasing each row we are about
// to own reaches every cell those would and not one cell more, without taking history that
// is not ours.
func (e *Emitter) Emit(f Frame) []byte {
	// A frame with no height is a document rather than a screen — the last frame of a
	// session, a fold, a pipe — and a frame carrying rows we have not written yet has to
	// take the path that writes them. Both are the handover, which is the only thing that
	// gives the terminal something to keep.
	if f.Height <= 0 || len(f.Committed) > e.committed {
		return e.handOver(f)
	}

	var b strings.Builder
	b.WriteString(ansi.SetMode(ansi.ModeSynchronizedOutput))
	b.WriteString(ansi.ResetMode(ansi.ModeTextCursorEnable))
	if !e.claimed {
		e.claim(&b, f)
	}
	e.paintScreen(&b, f)
	b.WriteString(ansi.ResetMode(ansi.ModeSynchronizedOutput))
	return []byte(b.String())
}

// claim takes the surface, once, so that every row on it is ours to address.
//
// Inline, it is f.Height newlines and nothing else. From a shell prompt on row r, the first
// Height-r of them walk the caret to the bottom row and the remaining r scroll the screen
// by exactly r rows: the prompt and everything above it move into scrollback, intact,
// selectable, still in the user's history, and what is left is a blank screen. That is the
// difference between this and CSI 2 J, which erases those rows instead of keeping them, and
// CSI 3 J, which deletes the history behind them. A newline is the only way to put a row
// into scrollback, and this is the only place we want one.
//
// In alt mode the claim is the alternate buffer, and Enter has normally taken it already —
// this is the fallback for an Emit that never got an Enter, which is a test and a caller
// that skipped a step, not a session. Sending 1049h a second time would overwrite the saved
// main-screen cursor, so e.claimed being true is what keeps it from happening.
//
// It goes inside the synchronized block, with the paint that follows, so the terminal shows
// the switch and the interface as one update instead of a blank flash. There is no second
// claim in either mode: a terminal that grows hands us rows we never asked for, and the next
// frame paints them because it addresses rows 1 to Height unconditionally.
func (e *Emitter) claim(b *strings.Builder, f Frame) {
	if e.Mode == ModeAlt {
		b.WriteString(ansi.SetMode(ansi.ModeAltScreenSaveCursor))
	} else {
		b.WriteString(strings.Repeat("\n", f.Height))
	}
	e.claimed = true
}

// paintScreen draws the screen: every row from the first to the last placed at its own
// absolute position, erased, then painted from the frame, and the caret placed absolutely
// too.
//
// The loop runs to f.Height rather than to the end of the rows, which is what makes
// "every visible row is ours" a fact about the emitter instead of a promise Render has to
// keep. Render pads its live region out to the height, so ordinarily the two are the same
// number; when a caller hands over a shorter frame, the rows past it are erased rather
// than left holding whatever was on them. Rows past the bottom of the screen are dropped
// from the top of the frame for the same reason from the other side, so the caret is never
// parked in a row something else was painted over.
//
// The erase goes before the text and never after. A row that exactly fills the width
// leaves the caret on its last column under deferred wrap, so an erase-to-end sent
// afterwards deletes the cell that was just painted.
func (e *Emitter) paintScreen(b *strings.Builder, f Frame) {
	rows := f.Live
	row, col := clampCursor(f.Cursor, len(rows))
	if n := len(rows) - f.Height; n > 0 {
		rows = rows[n:]
		if row -= n; row < 0 {
			row = 0
		}
	}

	b.WriteString(ansi.ResetMode(ansi.ModeAutoWrap))
	for i := 0; i < f.Height; i++ {
		b.WriteString(ansi.CursorPosition(1, i+1))
		b.WriteString(ansi.EraseLine(0))
		if i < len(rows) {
			e.writeLine(b, rows[i])
		}
	}
	b.WriteString(ansi.SetMode(ansi.ModeAutoWrap))
	b.WriteString(ansi.CursorPosition(col+1, row+1))
	if !f.Cursor.Hidden {
		b.WriteString(ansi.SetMode(ansi.ModeTextCursorEnable))
	}
	// The height is the one number kept, and it is kept for Exit, which steps past the
	// interface when a session ends without a handover. Nothing is derived from it, which
	// is the point: every position this function writes came from the frame it was handed.
	e.rows = f.Height
}

// handOver prints the transcript and gives the terminal back. It runs once, on the frame
// that has no screen to hold: Render declares the whole transcript committed when it is
// asked for a document rather than a screen, and settle() is what asks.
//
// This is the only place a session's transcript reaches the terminal, and holding it back
// until here is the reason a drag can be repaired at all. Until this frame every row we
// have written is a row we can still address, so re-wrapping the conversation is a
// repaint. After it the rows belong to the terminal's history, wrapped to the width the
// session ended on, and we never touch them again.
//
// Releasing the claim is the one step that differs by mode, and both spellings end with the
// caret at the top of ground we are allowed to write on. Alt leaves the alternate buffer,
// and 1049l restores the cursor the switch saved — the shell's own position, one row under
// the command the user ran, which is exactly where output belongs. Inline has no buffer to
// leave, so it goes home to the top of the screen it claimed. Then one erase to the end of
// the display, which is every cell we ever addressed and not one cell of history, and the
// transcript is written from there with newlines, so the terminal scrolls it into the user's
// history the way it scrolls any program's output.
//
// When no surface was ever claimed there is nothing to release and this is a plain print: a
// fold, a pipe, a run that ended before its first frame. The high-water mark is still a
// mark and the guard still a >, because Emit has to survive being handed a second, shorter
// document — Exit calling in, a caller folding twice — without re-slicing past the end of
// it or printing the transcript twice.
func (e *Emitter) handOver(f Frame) []byte {
	var b strings.Builder
	b.WriteString(ansi.SetMode(ansi.ModeSynchronizedOutput))
	if e.claimed {
		if e.Mode == ModeAlt {
			b.WriteString(ansi.ResetMode(ansi.ModeAltScreenSaveCursor))
		} else {
			b.WriteString(ansi.CursorHomePosition)
		}
		b.WriteString(ansi.EraseDisplay(0))
		e.claimed = false
	}
	if n := len(f.Committed); n > e.committed {
		for _, l := range f.Committed[e.committed:] {
			e.writeLine(&b, l)
			b.WriteString("\r\n")
		}
		e.committed = n
	}
	// A document frame from settle() has no live region at all. A caller that asks for one
	// anyway — a fold that wants its input bar in the output — gets it printed under the
	// transcript, as text, with no claim on it and no caret arithmetic.
	for i, l := range f.Live {
		if i > 0 {
			b.WriteString("\r\n")
		}
		e.writeLine(&b, l)
	}
	// The caret comes back unconditionally. The interface is gone, the terminal is the
	// user's again, and a hidden cursor is the one thing a program must not leave behind;
	// Exit sends it too, but Exit is not guaranteed to follow a handover.
	b.WriteString(ansi.SetMode(ansi.ModeTextCursorEnable))
	b.WriteString(ansi.ResetMode(ansi.ModeSynchronizedOutput))
	e.rows = 0
	return []byte(b.String())
}

// clampCursor keeps a bad cursor from turning into a bad escape sequence. A
// negative column would emit CSI -3 C, which some terminals read as a huge
// positive number.
func clampCursor(c Cursor, live int) (row, col int) {
	row, col = c.Line, c.Col
	if row < 0 {
		row = 0
	}
	if live > 0 && row > live-1 {
		row = live - 1
	}
	if live == 0 {
		row = 0
	}
	if col < 0 {
		col = 0
	}
	return row, col
}

// writeLine paints one row. A span with no style is written bare, which is what
// makes an unthemed frame come out as plain text and what keeps the escape traffic
// down to the spans that actually carry colour.
//
// A span carrying a Fill resolves both keys and composes them, Style over Fill, so
// the row-wide decision (a diff's coloured band) and the token-wide one (a syntax
// colour) can be made independently by code that does not know about each other.
func (e *Emitter) writeLine(b *strings.Builder, l Line) {
	for _, sp := range l {
		if sp.Text == "" {
			continue
		}
		st := e.resolve(sp.Style)
		if sp.Fill != "" {
			st = st.Over(e.resolve(sp.Fill))
		}
		if st.IsZero() || (e.Profile == ProfileMono && st.Attrs == 0) {
			b.WriteString(sp.Text)
			continue
		}
		b.WriteString(e.sgr(st))
		b.WriteString(sp.Text)
		b.WriteString(ansi.ResetStyle)
	}
}

func (e *Emitter) resolve(key string) Style {
	if e.Theme == nil {
		return Style{}
	}
	return e.Theme.Resolve(key)
}

// sgr writes one style, and the following span closes it with a full reset rather
// than with the matching off-codes. That is not laziness: SGR 22 turns off bold
// and dim together, so "stop being dim, stay bold" cannot be expressed. One reset
// is three bytes and is always right.
func (e *Emitter) sgr(s Style) string {
	parts := make([]string, 0, 8)
	for _, a := range []struct {
		bit  Attr
		code string
	}{
		{AttrBold, "1"},
		{AttrDim, "2"},
		{AttrItalic, "3"},
		{AttrUnderline, "4"},
		{AttrReverse, "7"},
		{AttrStrike, "9"},
	} {
		if s.Has(a.bit) {
			parts = append(parts, a.code)
		}
	}
	parts = append(parts, e.colorParams(s.FG, false)...)
	parts = append(parts, e.colorParams(s.BG, true)...)
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

// colorParams turns one colour into SGR parameters under the current profile.
//
// The first sixteen indices go out as the 1979 forms (30-37, 90-97) rather than as
// 38;5;n. Every terminal that has ever existed understands them, they are shorter,
// and — this is the part that matters for a theme built out of them — they are the
// colours the user chose in their own terminal settings, so the default theme
// inherits their scheme instead of fighting it.
func (e *Emitter) colorParams(c Color, bg bool) []string {
	if e.Profile == ProfileMono || c.Kind == ColorNone {
		return nil
	}
	idx := -1
	switch c.Kind {
	case ColorIndex:
		idx = int(c.R)
		if e.Profile == ProfileANSI && idx > 15 {
			idx = idx256to16(idx)
		}
	case ColorRGB:
		switch e.Profile {
		case ProfileTrueColor:
			base := "38"
			if bg {
				base = "48"
			}
			return []string{base, "2", itoa(c.R), itoa(c.G), itoa(c.B)}
		case Profile256:
			idx = rgbTo256(c)
		default:
			idx = rgbTo16(c)
		}
	}
	switch {
	case idx < 0:
		return nil
	case idx < 8:
		if bg {
			return []string{strconv.Itoa(40 + idx)}
		}
		return []string{strconv.Itoa(30 + idx)}
	case idx < 16:
		if bg {
			return []string{strconv.Itoa(100 + idx - 8)}
		}
		return []string{strconv.Itoa(90 + idx - 8)}
	default:
		if bg {
			return []string{"48", "5", strconv.Itoa(idx)}
		}
		return []string{"38", "5", strconv.Itoa(idx)}
	}
}

func itoa(v uint8) string { return strconv.Itoa(int(v)) }

// basic16 is what the sixteen ANSI slots look like on a mid-range terminal. It is
// only ever used to answer "which of these is nearest", so being a stand-in for
// the user's real palette costs nothing: we are choosing a slot, and the terminal
// then paints it in whatever colour the user actually configured.
var basic16 = [16]Color{
	{Kind: ColorRGB, R: 0x00, G: 0x00, B: 0x00},
	{Kind: ColorRGB, R: 0xcd, G: 0x00, B: 0x00},
	{Kind: ColorRGB, R: 0x00, G: 0xcd, B: 0x00},
	{Kind: ColorRGB, R: 0xcd, G: 0xcd, B: 0x00},
	{Kind: ColorRGB, R: 0x00, G: 0x00, B: 0xee},
	{Kind: ColorRGB, R: 0xcd, G: 0x00, B: 0xcd},
	{Kind: ColorRGB, R: 0x00, G: 0xcd, B: 0xcd},
	{Kind: ColorRGB, R: 0xe5, G: 0xe5, B: 0xe5},
	{Kind: ColorRGB, R: 0x7f, G: 0x7f, B: 0x7f},
	{Kind: ColorRGB, R: 0xff, G: 0x00, B: 0x00},
	{Kind: ColorRGB, R: 0x00, G: 0xff, B: 0x00},
	{Kind: ColorRGB, R: 0xff, G: 0xff, B: 0x00},
	{Kind: ColorRGB, R: 0x5c, G: 0x5c, B: 0xff},
	{Kind: ColorRGB, R: 0xff, G: 0x00, B: 0xff},
	{Kind: ColorRGB, R: 0x00, G: 0xff, B: 0xff},
	{Kind: ColorRGB, R: 0xff, G: 0xff, B: 0xff},
}

// rgbTo256 maps into the 6x6x6 cube, or into the 24-step grey ramp when the three
// channels are close enough that the cube would tint it.
func rgbTo256(c Color) int {
	r, g, b := int(c.R), int(c.G), int(c.B)
	if abs(r-g) < 8 && abs(g-b) < 8 && abs(r-b) < 8 {
		switch {
		case r < 5:
			return 16
		case r > 246:
			return 231
		}
		step := (r - 8 + 5) / 10 // the ramp runs 8, 18, ... 238; round, do not floor
		if step > 23 {
			step = 23
		}
		return 232 + step
	}
	return 16 + 36*cube6(r) + 6*cube6(g) + cube6(b)
}

func cube6(v int) int {
	if v < 48 {
		return 0
	}
	if v < 115 {
		return 1
	}
	return (v - 35) / 40
}

// rgbTo16 picks the nearest of the sixteen slots. The green channel is weighted
// because the eye is: an unweighted distance sends warm greys to blue.
func rgbTo16(c Color) int {
	best, bestD := 0, 1<<30
	for i, p := range basic16 {
		dr, dg, db := int(c.R)-int(p.R), int(c.G)-int(p.G), int(c.B)-int(p.B)
		d := 2*dr*dr + 4*dg*dg + 3*db*db
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// idx256to16 collapses a 256-colour index for a terminal that has sixteen.
func idx256to16(idx int) int {
	switch {
	case idx < 16:
		return idx
	case idx < 232:
		n := idx - 16
		lv := [6]uint8{0, 95, 135, 175, 215, 255}
		return rgbTo16(Color{Kind: ColorRGB, R: lv[n/36], G: lv[n/6%6], B: lv[n%6]})
	default:
		v := uint8(8 + (idx-232)*10)
		return rgbTo16(Color{Kind: ColorRGB, R: v, G: v, B: v})
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
