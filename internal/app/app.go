// Package app is the player. It turns a recorded scenario into a conversation that
// unfolds at wall-clock speed, and it lets a human interrupt that conversation the way
// they will interrupt the real agent: by typing, and by answering the questions it asks.
//
// Two decisions carry the whole file. The first is that a human action becomes a real
// event and goes through state.Apply, never through a shortcut: only the Apply path
// clears a stale quiescent diagnosis and marks an inbox answered, so a submitted line
// that skipped it would leave the interface lying about the run. The second is that the
// player owns no clock of its own beyond the next step's delay — a barrier is a nil
// channel, which blocks forever, so simulated time simply does not pass while the human
// is thinking.
package app

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/ext/viewmodel"
	"arxi.local/sim/internal/scenario"
	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

// Config is everything the player needs from outside. Events and Out are interfaces
// rather than a *term.TTY on purpose: the whole app is testable against a channel and
// a bytes.Buffer, which is only possible because the emitter returns bytes instead of
// writing them.
type Config struct {
	Scenario *scenario.Scenario
	Events   <-chan term.Event
	Out      io.Writer
	Emitter  *ui.Emitter
	Keymap   *Keymap

	// Glyphs is the marker vocabulary. Nil means the shipped set, which is what a
	// caller that has not chosen wants; --ascii passes a set built from the fallbacks.
	// It is a pointer because the zero Glyphs has no map in it and would answer every
	// lookup with an empty string, and a frame drawn with no markers at all is worse
	// than one drawn with the wrong ones.
	Glyphs *ui.Glyphs

	Width  int
	Height int

	// Size asks the terminal how big it is right now, and is called immediately before
	// every frame. A resize event alone is not enough: the terminal re-wraps its buffer
	// when the user drags the window and the signal announcing it arrives afterwards, so
	// a frame drawn from a recorded step in that gap is laid out against a geometry the
	// screen no longer has — and a slow drag is dozens of those gaps. Nil means the
	// caller has no terminal to ask (a test, a pipe) and Width and Height stand.
	Size func() (int, int)

	// Now and After are the wall clock used by input gestures and visual deadlines.
	// They share one domain so a test can advance animation without sleeping. Nil
	// means time.Now and time.After respectively.
	//
	// Simulated time is emphatically not this. A recording's clock is divided by Speed and
	// stops dead at a barrier; a reader's hand does neither, and a wheel measured in scenario
	// time would accelerate differently under -speed 4 than under -speed 1.
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time

	// Speed divides every recorded delay: 2 is twice as fast, 0.5 is half. A test
	// passes something enormous and the recording plays out in a few microseconds.
	Speed float64

	// WheelLines is how far one notch of the wheel moves the conversation. It is a setting and
	// not a constant because it is a fact about a mouse and a hand: the same number is two
	// flicks on one machine and a fistful on another, and the only person who can judge it is
	// the one turning the wheel. Zero means defaultWheelLines.
	WheelLines int

	// InputTitle is the word let into the top border of the input box. Empty — the
	// default — draws an unbroken rule, because the marker inside the box already says
	// what the box is for. It is here rather than left to the caller to reach into the
	// editor because it is a setting a config file will want, and the editor is not
	// otherwise ours to touch.
	InputTitle string

	// Layout is the [layout] table as parsed: per-slot overrides of the composition's
	// own order, tiers included. Nil or empty is the composition as shipped, and
	// chrome() answers byte for byte what it always did — the zero-layout rule, the
	// same one every other setting here already keeps.
	Layout []LayoutOverride

	// Config is the optional full-frame configuration controller. Its interface lives here
	// so the config package may implement it without an import cycle.
	Config ConfigController

	// Extensions is the consumer-owned process integration seam. Nil preserves the
	// historical loop and output byte for byte.
	Extensions ExtensionManager

	// Shine is the shape of the band of light that crosses the input box while the turn is
	// the human's, and the word "working" while it is not. Zero is no shine anywhere: every
	// frame then draws exactly what it drew before the animation existed, and a stable
	// Frame reports no visual deadline, so the loop parks at zero CPU.
	//
	// One field for both bands, because they are one phenomenon and a reader who slows the
	// sweep down means both of them. Two of its fields are read differently from the rest.
	// Style is the switch and nothing else — which theme key each band is drawn in is this
	// package's business, since ui declares exactly two and [styles] is where their colour
	// comes from — and Phase is ignored, because App.phase is the only clock in the program
	// and a second one would drift against the spinner it is meant to move with.
	Shine ui.Shimmer
}

// defaultWheelLines is what one notch of the wheel moves the conversation.
//
// It is three because that is the number the reader asked for, and the unit is now exactly
// what it says: three rows a notch. That holds while a notch reaches us once, whole, as a
// single wheel report — which is what a terminal sends a program that has claimed tracking,
// and not what this number used to be multiplied by back when a notch arrived as however many
// arrow presses the OS thought it was worth, which is why it was four and then two while the
// multiplication was being understood.
//
// One terminal reports something else, and the caller sets the number to match rather than
// teaching this package about phones: Termux sends one wheel report per row a finger has
// travelled, so -scroll 1 is what makes a swipe track the hand — and 1 there is still three
// rows for a real notch, because Termux spends three reports of its own on one.
//
// -scroll is the knob for the hand that disagrees, and it moves ctrl+up and ctrl+down with it:
// the reader asked for the keyboard's step to be the wheel's step, so one number sets both.
// alt+up and alt+down are the single row, for the line you land one short of.
const defaultWheelLines = 3

// accelPeriod is how long a spin stays one spin. A wheel report arriving within this long of
// the last one is the same gesture continuing, and carries further than the report before it; a
// report arriving later is a hand placed on the wheel again, and carries a plain notch.
//
// The number sits in a gap that a hand puts there. Turning a wheel notch by notch, watching
// the rows go past, is three or four notches a second — 250 ms apart and up — while a flick is
// five to ten times that, 20 to 50 ms apart, because the wheel is freewheeling and the finger
// has already left it. 150 ms is between those two and close to neither, so it does not need
// to be tuned: what a spin is worth is -scroll, which is a fact about a hand, and all this
// decides is when two reports are one gesture, which is a fact about a wheel.
const accelPeriod = 150 * time.Millisecond

// armTimeout is how long the second ctrl+c stays armed. A reader who pressed ctrl+c to
// clear the line and then paused for more than this has moved on — the next ctrl+c clears
// the line again instead of leaving. 1.5 seconds is long enough that a deliberate double
// tap always lands and short enough that a reflex five seconds later does not.
const armTimeout = 1500 * time.Millisecond

// spinPeriod is how long one frame of the status spinner lasts. It is not divided by
// -speed: the spinner is a fact about the process being alive now, not about the run
// being replayed, and a -speed 8 spin fast enough to strobe would be a worse lie than a
// steady one. Ten frames at this period is a turn and a fifth per second, which reads as
// deliberate rather than frantic, and it is slow enough that the redraw it costs is
// invisible next to the one an event already causes.
const spinPeriod = 120 * time.Millisecond

// App is the running simulation.
type App struct {
	cfg Config
	st  *state.State
	ed  *ui.Input
	r   *ui.Renderer
	em  *ui.Emitter
	km  *Keymap
	vp  ui.Viewport

	// view owns the current full-frame surface. Team and Config share local
	// scroll geometry so either can open without moving the conversation.
	view       appView
	viewTop    int
	viewRows   int
	viewBelow  int
	configView *configViewState

	// streamed says the screen is currently holding the conversation's own rows, which
	// is what lets a scrollback surface commit the rows its window leaves behind: a
	// scroll carries whatever the screen's top is holding, and after a roster or a
	// config editor has painted, that is not the transcript. Every non-conversation
	// draw clears it and the next conversation draw answers with one seam frame — the
	// window parked at the row history ends on, repainting over the visitor — before
	// any further commit.
	streamed bool

	// tasksOpen is whether the Tasks panel above the input is dropped down. It
	// starts open: a run that ships tasks is telling the reader what is happening,
	// and the summary alone made them press a key to find out. The panel is fixed —
	// collapsed it is the one summary line the widget has always drawn, open it is
	// the bounded list, and the toggle keys flip between the two without anything
	// else on the screen moving.
	tasksOpen bool

	// overlay is the active floating window, or nil. When set, key routing
	// changes: arrow keys and enter go to the overlay instead of the editor.
	overlay   *ui.Overlay
	ovHandler OverlayHandler

	// effortSlider is the open inline effort bar, or nil. While open it is
	// installed as a widget in SlotAboveInput by chrome(), and keys are routed
	// to it before the editor.
	effortSlider *EffortSlider

	// smenu is the slash command suggestion dropdown, rebuilt after every edit.
	smenu slashMenu

	// toast is the transient the input's right border is holding up, and toastUntil
	// is when it comes back down. A level change is worth naming once — the
	// transcript just re-flowed under the reader, and the reason should be on
	// screen — and worth forgetting soon, which is why it is a deadline and not a
	// flag: draw takes it down when the clock has passed it, and scheduleFrame arms
	// the wake that makes that draw happen even in a session where nothing else
	// moves.
	toast      string
	toastUntil time.Time

	// Extension actions are process-scoped and disappear on disconnect. Consent is
	// kept as a full-frame view before the manager starts native code.
	extActions  map[Action]string
	consents    []ExtensionConsent
	extPanels   map[string]extensionPanel
	extPanel    string
	extCapture  string
	extResize   map[string][2]int
	extRedraw   bool
	extDeadline <-chan time.Time

	// layout is the parsed [layout] table, empty when the config named none — which
	// is the ordinary case and the byte-identical one. chrome() consults it per
	// frame, so a tier's width and height conditions are re-read against the
	// terminal as it is now, not as it was when the run started.
	layout []LayoutOverride

	steps []scenario.Step
	next  int           // index of the step that has not played yet
	clock time.Duration // simulated time, which only recorded steps advance

	// seq numbers the events the human causes. It starts above every recorded seq so
	// that a synthesized prompt can never take an id — prompt-<seq>, tool-<seq> — that
	// the recording already used, which the renderer's memo would then draw wrong.
	seq   int
	scope string

	// barrier is the await token the scenario is waiting on, empty when it is running.
	barrier string
	// released counts the answers the human gave before the recording reached the
	// barrier they satisfy. Without it the ordering of two goroutines would decide
	// whether the run resumes at all: the frame that shows a question is drawn a step
	// before the barrier that waits for it, so an answer typed in that gap would clear
	// nothing and the scenario would then park on it forever.
	released map[string]int
	// skip is the inbox the human just answered. The recording answers it too, and
	// applying both replies appends the approval notice twice.
	skip string

	// scrolled, top and rows are where the human is looking. They are here rather than in
	// the viewport because the viewport is rebuilt from the terminal's size on every frame
	// and a reading position must survive that.
	//
	// scrolled off means "follow the tail", which is the state a session spends almost all
	// of its life in and which costs no arithmetic at all: the renderer takes the last
	// window every frame, however many rows arrived. Once it is on, top is an offset from
	// the top of the transcript — not from the bottom — so rows arriving while the human
	// reads pile up below the window and the text on screen does not move.
	//
	// Both come back out of the frame after every render: top is re-read from Scroll.Above
	// so that the clamp the renderer applied is what we store, instead of an offset that
	// drifts a page further past the end with every keypress, and rows is the height of the
	// window, which is what a page key moves by. Neither can be computed here — the number
	// of rows the transcript wraps to at this width is the renderer's answer, not ours.
	scrolled bool
	top      int
	rows     int

	// spin is how many wheel reports have arrived in a row without the hand pausing, and
	// spinAt is when the last one came. They are the whole of what an accelerating wheel
	// remembers, and the pair is a timestamp rather than a counter some other keypress clears
	// because a hand coming off the wheel is what ends a spin, and nothing announces that: no
	// key is pressed, no report is sent, and the only evidence is the silence afterwards.
	//
	// spinDir is the way it was going, in rows and not in keys, so an inverted wheel spins on
	// the direction the reader sees. A reversal starts the ramp over rather than carrying a
	// flick's momentum into it, because a reversal is a correction — the reader went too far
	// and is coming back — and the row they are hunting for is one of the ones just passed.
	spin    int
	spinAt  time.Time
	spinDir int

	// dragging is whether the pointer has hold of the scrollbar's thumb, and grab is where on
	// the thumb it took hold, counted in rows from the thumb's top. The pair is the whole of a
	// drag, and it is here because a drag is the one gesture in this program that outlives an
	// event: the widget is rebuilt from scratch every frame, so nothing below this line can
	// remember that a press happened, and the release that ends it may never arrive at all.
	//
	// grab is what makes the pill follow the pointer instead of jumping its top edge to it. A
	// reader who takes hold of the middle of the thumb and moves down two rows expects the
	// thumb to move down two rows, which is only true if the row they grabbed is subtracted
	// back out of every report that follows.
	dragging bool
	grab     int

	// side is where the last frame put the column the scrollbar is drawn in, and below is the
	// third of that frame's three scroll numbers — top and rows above already hold the other
	// two. Together they are everything needed to ask the widget which row of the bar a pointer
	// landed on, and they are the renderer's answers rather than ours for the reason Frame.Side
	// gives: the wrap point and the rows of chrome above the transcript are its arithmetic, and
	// a second copy here would agree until either was touched.
	//
	// They describe the frame on the screen, which is the frame the reader is pointing at, so
	// reading them a report later is not staleness — it is the only correct thing to read.
	side  ui.Rect
	below int

	// phase is the spinner's frame counter, and the one piece of state here that a
	// recorded event never touches. It is counted in wall-clock ticks rather than in
	// simulated time because a spinner exists to say the program is still running, and
	// simulated time is exactly what stops passing when it is not: -speed 4 spins the
	// same, a barrier spins on while the clock stands still, and a 1400 ms tool call
	// spins for 1400 ms instead of jumping one frame when its result lands.
	// frameNext is the deadline reported by the frame on screen. phaseAt anchors
	// phase to wall time only while that frame can change; a stable frame pauses
	// the shared animation clock until another event arms it again.
	phase     int
	phaseAt   time.Time
	frameNext int
	animation <-chan time.Time

	quit bool

	// armed is "the last key was ActionInterrupt, and it did not leave". It is what makes
	// the second press of ctrl+c a way out instead of a second empty line-clear. Any other
	// keypress clears it, and so does 1.5 seconds of silence: a reader who pressed ctrl+c
	// to clear the line and then paused has moved on, and a door that opens five minutes
	// later on a reflex is not what they reached for.
	armed   bool
	armedAt time.Time
}

// New builds the simulation. Every zero value in Config becomes something usable, so a
// caller that only has a scenario can still run.
func New(cfg Config) *App {
	if cfg.Out == nil {
		cfg.Out = io.Discard
	}
	if cfg.Speed <= 0 {
		cfg.Speed = 1
	}
	if cfg.Width <= 0 {
		cfg.Width = 80
	}
	if cfg.Height <= 0 {
		cfg.Height = 24
	}
	if cfg.WheelLines <= 0 {
		cfg.WheelLines = defaultWheelLines
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	if cfg.Emitter == nil {
		cfg.Emitter = &ui.Emitter{Theme: ui.DefaultTheme(), Mode: ui.ModeInline, Profile: ui.ProfileTrueColor}
	}
	if cfg.Keymap == nil {
		cfg.Keymap = DefaultKeymap()
	}

	a := &App{
		cfg:        cfg,
		st:         state.New(),
		ed:         ui.NewInput(),
		r:          ui.NewRenderer(),
		em:         cfg.Emitter,
		km:         cfg.Keymap,
		released:   make(map[string]int),
		extActions: make(map[Action]string),
		extPanels:  make(map[string]extensionPanel),
		extResize:  make(map[string][2]int),
		tasksOpen:  true,
		layout:     cfg.Layout,
		streamed:   true,
	}
	if cfg.Extensions != nil {
		a.consents = cfg.Extensions.Pending()
		if len(a.consents) > 0 {
			a.openFullView(viewConsent)
		}
	}
	a.ed.Title = cfg.InputTitle
	// A session opens compact: the run's shape first, the machinery one ctrl+o away.
	a.r.Detail = ui.DetailCompact
	if cfg.Scenario != nil {
		a.steps = cfg.Scenario.Steps
	}
	if cfg.Glyphs != nil {
		a.r.Glyphs = *cfg.Glyphs
	}
	a.seq = maxSeq(a.steps) + 1
	a.scope = scopeOf(a.steps)
	a.resize(cfg.Width, cfg.Height)
	return a
}

// maxSeq is why the human's events cannot collide with the recording's.
func maxSeq(steps []scenario.Step) int {
	high := 0
	for _, s := range steps {
		if s.Event != nil && s.Event.Seq > high {
			high = s.Event.Seq
		}
	}
	return high
}

// scopeOf borrows the recording's scope so a synthesized event belongs to the same run.
// A scenario with no events at all gets a scope of its own rather than an empty string,
// because an event with no scope belongs to nothing.
func scopeOf(steps []scenario.Step) string {
	for _, s := range steps {
		if s.Event != nil && s.Event.Scope != "" {
			return s.Event.Scope
		}
	}
	return "run:local"
}

// resize turns a terminal size into the capabilities the renderer is allowed to use.
// This is the only place a mode becomes a capability: everything downstream asks "can
// this surface hold a fixed top row", never "am I in alt screen".
//
// The reading position is left alone. A resize re-wraps the transcript, so the row that was
// at the top of the window is not row a.top any more and the view drifts by however much the
// text above it changed height. Correcting for that would mean remembering which item the
// window started on and finding it again at the new width, which is a real feature and not
// this one; until then a little drift beats being thrown back to the tail mid-read, and the
// renderer's clamp keeps the offset inside the log however far the terminal grew.
func (a *App) resize(w, h int) {
	alt := a.em.Mode == ui.ModeAlt
	a.vp = ui.Viewport{
		Width:      w,
		Height:     h,
		SidePanels: alt && w >= 100,
		FixedTop:   alt,
		Scrollback: a.em.Scrollback,
	}
}

// Run plays the scenario until the log is exhausted and the human quits. It returns
// nil on a clean exit; the terminal is left as Enter found it by the caller, which owns
// the writer and therefore owns the restore.
func (a *App) Run() error {
	if err := a.write(a.em.Enter()); err != nil {
		return err
	}
	if err := a.draw(); err != nil {
		return err
	}

	// wake is the pending delay. It is deliberately not a Timer: a keypress must not
	// restart a delay that is already counting, and the only way to be sure of that is
	// to never touch it once armed.
	var wake <-chan time.Time
	var disarm <-chan time.Time
	for !a.quit {
		if wake == nil && a.barrier == "" && a.next >= len(a.steps) && !a.st.Finished {
			a.finish()
			if err := a.draw(); err != nil {
				return err
			}
		}
		if wake == nil {
			wake = a.arm()
		}
		if disarm == nil && a.armed {
			rem := armTimeout - a.cfg.Now().Sub(a.armedAt)
			if rem <= 0 {
				rem = time.Millisecond
			}
			disarm = a.cfg.After(rem)
		}
		select {
		case m, ok := <-extensionMessages(a.cfg.Extensions):
			if ok {
				visual := a.extensionMessage(m)
				if m.View != nil || m.ViewClosed != "" || m.Removed {
					a.extRedraw = a.extRedraw || visual
					if a.extDeadline == nil {
						a.extDeadline = a.cfg.After(spinPeriod)
					}
				} else if visual {
					if err := a.draw(); err != nil {
						return err
					}
				}
			}
		case <-a.extDeadline:
			a.extDeadline = nil
			if a.extRedraw {
				a.extRedraw = false
				if err := a.draw(); err != nil {
					return err
				}
			}
		case <-disarm:
			disarm = nil
			if a.armed {
				a.armed = false
				if err := a.draw(); err != nil {
					return err
				}
			}
		case fired, ok := <-a.animation:
			if !ok {
				a.animation = nil
				continue
			}
			now := a.cfg.Now()
			if now.After(fired) {
				fired = now
			}
			a.advancePhase(fired)
			a.animation = nil
			if err := a.draw(); err != nil {
				return err
			}
		case <-wake:
			wake = nil
			if a.advance() {
				if err := a.draw(); err != nil {
					return err
				}
			}
		case ev, ok := <-a.cfg.Events:
			if !ok {
				return a.settle()
			}
			dirty, err := a.handle(ev)
			if err != nil {
				return err
			}
			// Whatever is already queued is folded in before drawing. One drag of a
			// window corner is dozens of SIGWINCH and a held key is a burst of them,
			// and every intermediate frame is a full repaint of a size nobody ever
			// saw. This is not a frame limiter: there is no timer, no event is
			// dropped, and a lone keypress still draws immediately. It just refuses
			// to spend a frame on state that is already stale.
		drain:
			for !a.quit {
				select {
				case ev, ok := <-a.cfg.Events:
					if !ok {
						return a.settle()
					}
					d, err := a.handle(ev)
					if err != nil {
						return err
					}
					dirty = dirty || d
				default:
					break drain
				}
			}
			if dirty {
				if err := a.draw(); err != nil {
					return err
				}
			}
		}
	}
	return a.settle()
}

// arm returns the channel that fires when the next step is due, or nil when nothing is
// due at all. A nil channel blocks forever in a select, which is precisely what a
// barrier means: the recording stops here until the human does something, and the
// program burns nothing while it waits.
func (a *App) arm() <-chan time.Time {
	if a.barrier != "" || a.next >= len(a.steps) {
		return nil
	}
	d := a.steps[a.next].At - a.clock
	if d < 0 {
		d = 0
	}
	if a.cfg.Speed != 1 {
		d = time.Duration(float64(d) / a.cfg.Speed)
	}
	return a.cfg.After(d)
}

// advance plays one step and reports whether the frame changed. A barrier changes
// nothing: it is a note to the player, not something the transcript can show.
func (a *App) advance() bool {
	s := a.steps[a.next]
	a.next++
	a.clock = s.At
	if s.IsBarrier() {
		if a.released[s.Await] > 0 {
			// Answered already. It is crossed without waiting, and one answer crosses
			// exactly one barrier: two prompts do not license skipping a question.
			a.released[s.Await]--
			return false
		}
		a.barrier = s.Await
		return false
	}
	if a.skip != "" && repliesTo(s.Event, a.skip) {
		// The human already answered this question. Its recorded reply is dropped, and
		// only its reply: the agent.unblocked that follows is the recording's own, so
		// the run resumes exactly as it was recorded.
		a.skip = ""
		return false
	}
	a.accepted(s.Event, false)
	return true
}

func repliesTo(ev *event.Event, inboxID string) bool {
	if ev == nil || ev.Type != event.InboxReplied {
		return false
	}
	var p event.InboxRepliedPayload
	if ev.Decode(&p) != nil {
		return false
	}
	return p.InboxID == inboxID
}

// handle folds one thing that arrived from the terminal into the state, and reports
// whether the frame has to be drawn again.
func (a *App) handle(ev term.Event) (bool, error) {
	switch ev.Kind {
	case term.EventResize:
		a.resize(ev.Width, ev.Height)
		if a.view == viewExtension {
			a.sendPanelResize()
		}
		return true, nil
	case term.EventPaste:
		if a.view == viewExtension {
			return a.panelInput(viewmodel.Input{Kind: "paste", Text: ev.Text}), nil
		}
		if a.view == viewConfig {
			if a.configView != nil && a.configView.editID != "" {
				a.configView.editor.Insert(configClean(ev.Text))
				return true, nil
			}
			return false, nil
		}
		if a.view != viewConversation {
			return false, nil
		}
		// A paste is text, never keys, so nothing in it can trigger an action. Its newlines
		// are kept now that the editor holds them: a pasted command block arrives as the
		// block it was, which is what pasting one was for. Everything else a clipboard can
		// carry that a Line cannot is flattened or dropped by clean.
		a.ed.Insert(clean(ev.Text))
		return true, nil
	case term.EventKey:
		return a.key(ev.Key), nil
	case term.EventMouse:
		if a.view == viewExtension {
			return a.panelMouse(ev.Mouse), nil
		}
		// Its own case and not a detour through key, because key owns the interrupt's arm and
		// clears it on everything else it is handed. A pointer is not a keypress: taking the
		// door away because the reader reached for the scrollbar would withdraw a promise the
		// row under the input is still making.
		return a.mouse(ev.Mouse), nil
	case term.EventClosed:
		a.quit = true
		if ev.Err != nil && !errors.Is(ev.Err, io.EOF) {
			return false, ev.Err
		}
		return false, nil
	}
	return false, nil
}

// key resolves one keypress, and owns the one piece of state a keymap cannot hold: whether
// the last key was the interrupt. Two presses of it in a row leave, so every other key has
// to take that back — and "every other key" is why the disarm is here instead of in the
// thirty-odd returns of dispatch below. One arm left standing is a door that opens on a
// keypress the reader did not mean as one.
//
// The `|| disarmed` is a repaint that would otherwise be dropped. dispatch reports whether
// it changed anything, and a key that changed nothing while the arm was up has still changed
// the screen: the row promising that the next press leaves has to come off it.
func (a *App) key(k term.Key) bool {
	act := a.km.Lookup(k)
	if act == ActionQuit {
		a.quit = true
		return false
	}
	if a.view == viewExtension {
		return a.panelKey(act, k)
	}
	if a.view == viewConfig {
		return a.configKey(act, k)
	}
	if a.view != viewConversation {
		return a.fullViewKey(act)
	}
	if act == ActionInterrupt {
		return a.interrupt()
	}
	disarmed := a.armed
	a.armed = false
	dirty := a.dispatch(act, k) || disarmed
	// Rebuild the slash suggestion menu after every keypress that touches the
	// editor, so the dropdown tracks what has been typed.
	a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
	return dirty
}

// interrupt is ctrl+c: the line first, the door second. It is the behaviour of every shell
// and of Claude Code itself, and the reason it is not simply ActionQuit is the user's own
// report — a half-typed line and a reflex reached for to drop it took the whole program down
// with it.
//
// Nothing here interrupts anything, which is worth saying because the name promises it: a
// recording plays whether the reader watches or not, and there is no request in flight to
// cancel. What the name is honest about is the reflex, and readline has called that reflex
// interrupt for forty years.
//
// The second press has armTimeout (1.5 s) to land: long enough that a deliberate double
// tap always makes it, short enough that a ctrl+c five seconds later clears the line
// again instead of leaving.
func (a *App) interrupt() bool {
	if a.armed && a.cfg.Now().Sub(a.armedAt) <= armTimeout {
		a.quit = true
		return false
	}
	a.armed = true
	a.armedAt = a.cfg.Now()
	a.ed.KillLine()
	return true
}

// dispatch runs one action. Everything the keymap does not claim is text, which is why the
// switch has no case for a plain letter: adding one would mean deciding twice what a letter
// is.
func (a *App) dispatch(act Action, k term.Key) bool {
	if a.view == viewConfig {
		return a.configKey(act, k)
	}
	if a.view != viewConversation {
		return a.fullViewKey(act)
	}
	// First-party modal surfaces outrank extension-owned actions.
	if a.overlay != nil && a.ovHandler != nil {
		return a.overlayKey(act, k)
	}
	if owner, ok := a.extActions[act]; ok {
		parts := strings.SplitN(string(act), ":", 3)
		if len(parts) == 3 {
			go a.cfg.Extensions.Invoke(owner, parts[2], "")
		}
		return true
	}
	// If the inline effort slider is open, route keys to it.
	if a.effortSlider != nil {
		return a.effortKey(act, k)
	}
	// If the slash menu is active, intercept navigation keys. The scroll actions count as
	// movement here as they already do in the effort slider and the config view: on a phone
	// with the mouse released the plain arrows are bound to them, and a menu open on a phone
	// still has to move.
	if a.smenu.Active() {
		switch act {
		case ActionHistoryPrev, ActionScrollUp:
			a.smenu = a.smenu.moveUp()
			return true
		case ActionHistoryNext, ActionScrollDown:
			a.smenu = a.smenu.moveDown()
			return true
		case ActionSubmit:
			// Execute the selected command immediately.
			sel := a.smenu.Selected()
			a.ed.KillLine()
			a.smenu = slashMenu{}
			return a.runSlash(sel.Name, "")
		case ActionComplete:
			// Fill the command text without executing.
			sel := a.smenu.Selected()
			a.ed.SetText("/" + sel.Name + " ")
			a.smenu = slashMenu{}
			return true
		case ActionCancel:
			a.smenu = slashMenu{}
			return true
		}
	}
	switch act {
	case ActionTeam:
		a.openTeam()
		return true
	case ActionTasks:
		a.tasksOpen = !a.tasksOpen
		return true
	case ActionOutputLevel:
		a.cycleOutputLevel()
		return true
	case ActionQuit:
		// ctrl+d, and unconditional: a simulator has nothing unsaved, so there is nothing
		// for a confirmation to protect. The key that does ask twice is ActionInterrupt,
		// and it asks because a reader reaches for it to clear a line and not to leave.
		a.quit = true
		return false
	case ActionSubmit:
		return a.submit()
	case ActionNewline:
		// The one editing verb with no method of its own on the editor, because a break is
		// text and Insert is how text gets in. It is a whole action rather than a fall-through
		// so that a config can move it, and so that `arxi-sim keys` says the second line
		// exists — a multi-line input nobody can find the key for is a single-line input.
		a.ed.Insert("\n")
		return true
	case ActionCancel:
		// Clearing the line, and nothing else. An esc that could quit is an esc that
		// eventually will, on a keypress the user did not mean. That is the whole
		// difference from ActionInterrupt, which clears the same line and then offers the
		// door: esc is pressed to back out of something and ctrl+c is pressed to get out.
		if a.ed.Text() == "" {
			return false
		}
		a.ed.KillLine()
		return true

	case ActionBackspace:
		a.ed.Backspace()
		return true
	case ActionDelete:
		a.ed.Delete()
		return true
	case ActionKillWord:
		a.ed.KillWord()
		return true
	case ActionKillToEnd:
		a.ed.KillToEnd()
		return true
	case ActionKillLine:
		a.ed.KillLine()
		return true

	case ActionLeft:
		a.ed.Left()
		return true
	case ActionRight:
		a.ed.Right()
		return true
	case ActionWordLeft:
		a.ed.WordLeft()
		return true
	case ActionWordRight:
		a.ed.WordRight()
		return true
	case ActionHome:
		a.ed.Home()
		return true
	case ActionEnd:
		a.ed.End()
		return true

	case ActionHistoryPrev:
		// In a multiline input, move cursor up first; only walk history
		// when the cursor is on the first visual line.
		in := a.ed
		room := in.Room(a.vp.Width)
		if in.Lines(room) > 1 {
			if in.Up(room) {
				return true
			}
		}
		a.ed.Older()
		return true
	case ActionHistoryNext:
		in := a.ed
		room := in.Room(a.vp.Width)
		if in.Lines(room) > 1 {
			if in.Down(room) {
				return true
			}
		}
		a.ed.Newer()
		return true

	// Moving the view, which is the one thing here that changes what is on screen without
	// changing the run. There are three amounts — a line, a wheel notch, a screen — and none
	// of them depends on which key was pressed: the amount belongs to the action, so a user
	// who rebinds page-down to something else gets a page, and the keymap stays a map from
	// keys to meanings rather than a place behaviour hides.
	//
	// A notch is several lines rather than one. Folding a burst of events into one frame is
	// what keeps a fast spin cheap, but it does not make it travel: a spin is a handful of
	// notches, so at one line each a sixty-row conversation is out of reach of a hand. And a
	// notch from the wheel itself carries further the faster the wheel is turning, which is
	// the one place the device the key came from is worth knowing — see fast.
	case ActionScrollUp:
		return a.scroll(-1)
	case ActionScrollDown:
		return a.scroll(1)
	case ActionScrollUpFast:
		return a.scroll(-a.fast(k, -1))
	case ActionScrollDownFast:
		return a.scroll(a.fast(k, 1))
	case ActionPageUp:
		return a.scroll(-a.page())
	case ActionPageDown:
		return a.scroll(a.page())
	case ActionScrollTop:
		return a.toTop()
	case ActionScrollBottom:
		return a.follow()
	case ActionJumpPrevMessage:
		return a.jump(-1)
	case ActionJumpNextMessage:
		return a.jump(1)

	case ActionApprove, ActionDeny:
		// y and n are letters first. They answer only while a question is open and the
		// line is empty, so typing "yes" into a reply is never swallowed halfway.
		if in := a.st.OpenInbox(); in != nil && a.ed.Empty() {
			a.answer(in, answerFor(act))
			return true
		}
	}
	// Unclaimed and printable: insert it. Runes arrive as a run — a burst of typing or
	// a paste from a terminal without bracketed paste is one event — so this inserts a
	// string rather than a rune, and a fast typist costs one frame instead of ten.
	if k.Type == term.KeyRunes && k.Mod == 0 {
		a.ed.Insert(string(k.Runes))
		return true
	}
	return false
}

// answerFor is the text a shortcut stands for. The approval widget draws "y allow" and
// "n deny", so these two words are what it promises, and they are also what the
// recording's own reply says — which is what keeps the transcript identical whether the
// human answered or the log did.
func answerFor(a Action) string {
	if a == ActionDeny {
		return "deny"
	}
	return "allow"
}

// submit sends the line. With a question open the line is the answer, because an inbox
// reply is free text and y/n are only the shortcuts for the two common ones.
func (a *App) submit() bool {
	text, ok := a.ed.Submit()
	if !ok {
		return false
	}
	a.smenu = slashMenu{} // dismiss any open suggestions
	if cmd, args := slashCommand(text); cmd != "" {
		return a.runSlash(cmd, args)
	}
	if in := a.st.OpenInbox(); in != nil {
		a.answer(in, text)
		return true
	}
	a.human(event.RunPrompt, event.RunPromptPayload{Text: text})
	a.release("prompt")
	return true
}

// answer replies to an open question as the human.
func (a *App) answer(in *state.Inbox, text string) {
	a.human(event.InboxReplied, event.InboxRepliedPayload{InboxID: in.ID, Text: text})
	a.skip = in.ID
	a.release(in.ID)
}

// release lets the recording continue if it was waiting on exactly this, and remembers
// the answer when it is not waiting yet.
func (a *App) release(await string) {
	if a.barrier == await {
		a.barrier = ""
		return
	}
	a.released[await]++
}

// human turns something the person did into a real event and applies it.
//
// This is the load-bearing shortcut nobody gets to take: state has AppendPrompt, and
// using it here would put the prompt in the transcript while leaving the previous
// quiescent diagnosis standing, so the interface would show "the run stopped because…"
// under a line the user just sent. Only Apply clears it, and only Apply marks an inbox
// answered, so the human speaks the same language as the log.
//
// It also returns the view to the tail, and only here. Someone who scrolled up to read is
// left alone by everything the agent does — that is the whole point of an offset measured
// from the top — but sending a line is asking to see what happens next, and answering from
// row 40 of a 200-row log while the reply lands off screen is the interface losing the
// user. A recorded event never calls this.
//
// TS is left empty: state is told the time explicitly, nothing reads the string, and a
// timestamp that claims to be real while the clock is simulated is worse than no
// timestamp. The clock does not advance here either — thinking is free.
func (a *App) human(typ string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ev := &event.Event{
		Seq:     a.seq,
		Type:    typ,
		Scope:   a.scope,
		Source:  event.SourceHuman,
		Actor:   "human",
		Payload: b,
	}
	a.seq++
	a.accepted(ev, true)
}

// finish marks the log exhausted. The input bar belongs to the human from here on: the
// scenario has nothing left to say, and the notice is a widget rather than a transcript
// entry because it stops being true the moment they type.
func (a *App) finish() {
	a.st.Finished = true
	a.st.Active = false
	a.st.AppendNotice(state.NoticeEndOfLog, "end of scenario")
}

// outputToastTTL is how long the input's border keeps naming the output level after
// a change. Long enough to read a three-word notice twice, short enough that it is
// gone before the reader starts wondering whether it is a mode they are stuck in:
// the level is a setting that stays until it is changed again, and the border says
// so only once.
const outputToastTTL = 2 * time.Second

// cycleOutputLevel steps the transcript through its three detail levels and holds
// the notice up. The order is compact, standard, full, and back to compact: the
// levels are a ladder by amount, and a cycle walks a ladder by steps rather than
// by jumping between its ends.
func (a *App) cycleOutputLevel() {
	a.setOutputLevel(a.r.Detail.OrStandard()%ui.DetailFull + 1)
}

// setOutputLevel moves the transcript to l and tells the reader, once, where they
// landed. The toggle is honest about position the way a jump is: the change re-wraps
// the whole conversation — the same items are a different number of rows at a
// different level — so a row number saved here would name a different sentence
// after the change. What survives the re-wrap is an item and an offset inside it,
// and ItemSpans can solve that against either geometry, so the window's top row is
// re-anchored on the item the reader was reading rather than on where that item
// used to start.
//
// Following the tail — the state a session spends most of its life in — needs none
// of it: the tail is the tail at every level, and draw keeps the window there.
func (a *App) setOutputLevel(l ui.DetailLevel) {
	cur := a.r.Detail.OrStandard()
	if l == cur {
		return
	}
	anchor, offset := -1, 0
	if a.scrolled {
		// A reader away from the tail owns a row of the transcript, and this change
		// re-wraps the transcript under them. Following the tail needs none of what
		// follows: the tail is the tail at every level.
		starts, _ := a.r.ItemSpans(a.st, a.vp)
		anchor, offset = spanAt(starts, a.top)
	}
	a.r.Detail = l
	if anchor >= 0 {
		starts, heights := a.r.ItemSpans(a.st, a.vp)
		if s := starts[anchor]; s >= 0 {
			// The offset is capped at the item's own height, the one number the two
			// geometries disagree about: at the fuller level the item is taller and
			// the reader is exactly where they were, at the sparser one it is shorter
			// and the cap is what keeps the window from sliding into the item below.
			a.top = s + max(0, min(offset, heights[anchor]-1))
		} else {
			// The item they were reading draws nothing at this level — a thought the
			// compact level hides. The nearest visible thing below it is where they
			// were headed; with nothing below, the conversation past the window has
			// collapsed away entirely, and the honest answer is the tail again.
			a.top, a.scrolled = 0, false
			for j := anchor; j < len(starts); j++ {
				if starts[j] >= 0 {
					a.top, a.scrolled = starts[j], true
					break
				}
			}
		}
	}
	a.toast = "Output level " + strconv.Itoa(int(l)) + "/3"
	a.toastUntil = a.cfg.Now().Add(outputToastTTL)
}

// spanAt names the item a transcript row sits in: the last item that starts at or
// above the row, and how far into it the row has gone. A row on the blank between
// two items belongs to the item above it, the item transcript() charged the blank
// after. (-1, 0) means no visible item reaches that high, which is a window resting
// on row zero of a conversation whose first item draws nothing.
func spanAt(starts []int, row int) (int, int) {
	at, off := -1, 0
	for i, s := range starts {
		if s >= 0 && s <= row {
			at, off = i, row-s
		}
	}
	return at, off
}

// draw renders and writes. The renderer's chrome is rebuilt every frame because a widget
// here is a value over live state, not an object with a lifetime: an approval widget
// exists exactly while a question is open, and that is one comparison, not a callback. The
// input's band of light is armed the same way and for the same reason — it is a fact about
// what the program is doing this frame, so it is derived here rather than switched on and
// off by whoever changed the state.
//
// The reading position goes in and comes back out. It has to come back out because the
// renderer is the only thing that knows how many rows the transcript wraps to at this
// width, so it is the only thing that can clamp an offset to the end of the log — and if
// the clamped answer were thrown away, a page key held down near the end would push our
// idea of the offset a page further past the end on every press, and the view would then
// take that many presses to move at all on the way back.
//
// Below == 0 means the window is sitting on the tail, so following resumes: the transcript
// grew until it caught up with a scrolled view, or the terminal grew until the whole log
// fit on it. Dropping the offset there is what keeps a session that was scrolled to the
// bottom from freezing at that row while the agent keeps talking.
//
// Two more things come back out for the pointer's sake and are never put in: the third scroll
// number, which the scrollbar needs to place a thumb and which nothing here can derive, and the
// rectangle the side column ended up in. A press arrives as a bare row and column of the
// terminal, and these are what turn one into a row of the bar — see the fields, and Frame.Side.
func (a *App) draw() error {
	if a.frameNext > 0 && !a.phaseAt.IsZero() {
		a.advancePhase(a.cfg.Now())
	}
	a.sync()
	if a.view != viewConversation {
		a.streamed = false
		var f ui.Frame
		switch a.view {
		case viewTeam:
			f = renderTeam(a.st, a.vp, a.viewTop)
		case viewConfig:
			f = renderConfig(a.configSnapshot(), a.configView, a.vp)
		case viewConsent:
			if len(a.consents) > 0 {
				f = renderConsent(a.consents[0], a.vp)
			}
		case viewExtension:
			a.sendPanelResize()
			f = a.renderPanel()
		}
		a.viewTop, a.viewRows, a.viewBelow = f.Scroll.Above, f.Scroll.Rows, f.Scroll.Below
		a.scheduleFrame(f)
		return a.write(a.em.Emit(f))
	}
	// The working verb lives in the input's top border now. While the run works, the
	// border carries the spinner and the verb on the same shared phase the status row
	// always indexed, with the status band of light crossing the word; the moment the
	// turn is the human's, the border gives the title back to whatever the caller set.
	// The editor's history label outranks both in Render, so walking back through the
	// history still names the entry rather than the run — History is the older claim
	// on that border and keeps it.
	if ui.Working(a.st) {
		a.ed.Title = a.workTitle()
		a.ed.TitleShine = a.shine(ui.StatusShine)
	} else {
		a.ed.Title = a.cfg.InputTitle
		a.ed.TitleShine = ui.Shimmer{}
	}
	a.ed.Shine = a.inputShine()
	// The level notice is a transient. Past its moment it comes down whether or not
	// anything else has happened, which is why the expiry is checked here and not in
	// the keypress that raised it: a session where nothing moves would otherwise
	// hold the notice up forever, and a notice that never leaves is a title.
	if !a.toastUntil.IsZero() && !a.cfg.Now().Before(a.toastUntil) {
		a.toast, a.toastUntil = "", time.Time{}
	}
	a.ed.Hint = a.toast
	// Back on the conversation after something else painted the screen: the window's
	// rows have to be laid back down before any more of them are committed, because a
	// scroll carries whatever the screen's top is holding and right now that is the
	// visitor's rows. One seam frame — the window parked at the row history ends on,
	// painted over the visitor — puts them back; the frame below then scrolls the rows
	// the window has grown past into history as if the visit had never happened.
	if a.em.Scrollback && !a.streamed {
		seam := a.vp
		seam.Scrolled, seam.ScrollTop = true, a.top
		if err := a.write(a.em.Emit(a.r.Render(a.st, a.ed, seam))); err != nil {
			return err
		}
	}
	a.streamed = true
	a.r.Widgets = a.chrome()
	a.r.Overlay = a.overlay
	a.vp.Scrolled, a.vp.ScrollTop = a.scrolled, a.top
	f := a.r.Render(a.st, a.ed, a.vp)
	a.top, a.rows, a.below = f.Scroll.Above, f.Scroll.Rows, f.Scroll.Below
	a.side = f.Side
	if f.Scroll.Below == 0 {
		a.scrolled = false
	}
	a.scheduleFrame(f)
	return a.write(a.em.Emit(f))
}

func (a *App) advancePhase(at time.Time) {
	if a.phaseAt.IsZero() {
		return
	}
	if at.Before(a.phaseAt) {
		at = a.cfg.Now()
	}
	elapsed := at.Sub(a.phaseAt) / spinPeriod
	if elapsed < 1 {
		elapsed = 1
	}
	a.phase += int(elapsed)
	a.phaseAt = at
}

// scheduleFrame replaces the previous frame's visual deadline. A stable frame
// pauses the shared phase; a moving one is anchored now and gets one one-shot wake.
//
// The toast's own deadline is a visual deadline too: the frame that takes the
// notice down has to be drawn even though nothing on screen is moving, so its wake
// is armed beside the animation's and the sooner of the two fires. A wake that
// arrives early only redraws and re-arms; the deadline is re-read against the clock
// every schedule, so the notice comes down at its ttl rather than at a tick count
// taken when it went up.
func (a *App) scheduleFrame(f ui.Frame) {
	now := a.cfg.Now()
	a.frameNext = f.NextVisualChange
	a.animation = nil
	next := time.Duration(0)
	if a.frameNext > 0 {
		next = time.Duration(a.frameNext) * spinPeriod
		a.phaseAt = now
	} else {
		a.phaseAt = time.Time{}
	}
	if !a.toastUntil.IsZero() {
		if d := a.toastUntil.Sub(now); d > 0 && (next <= 0 || d < next) {
			next = d
		}
	}
	if next > 0 {
		a.animation = a.cfg.After(next)
	}
}

// scroll moves the window n rows, negative for up, and reports whether the frame has to be
// drawn again.
//
// Scrolling down while following is not a move: the view is already at the end and there is
// nothing below it, so pressing the key does nothing rather than nudging the offset into a
// state the next frame would immediately undo. Scrolling up while following is what starts
// scrolling, and from where the view already is — a.top is the tail offset the last frame
// reported, so the first press moves by one row rather than jumping to the top of the log.
//
// scroll moves the reading position by n rows and reports whether the frame has to be
// drawn again.
//
// The bottom is not clamped here on purpose. Asking for a row past the end is a page key
// near the tail, and the renderer answers with the last window and reports the offset it
// used; clamping it twice, once here against a row count we would have to recompute, is how
// the two clamps disagree.
//
// On a scrollback surface it answers no at any distance, and so do the movers below it.
// The reading position there is the terminal's own history, and the terminal takes no
// requests: there is no sequence that scrolls somebody else's view, and a window that
// moved back over committed rows would print them twice. The keys stay bound — the keymap
// does not fork for a surface — and the swipe is the scroll, which on the one surface
// these fire on is the scroll there is.
func (a *App) scroll(n int) bool {
	if a.em.Scrollback {
		return false
	}
	if !a.scrolled && (n >= 0 || a.top == 0) {
		return false
	}
	top := a.top + n
	if top < 0 {
		top = 0
	}
	if a.scrolled && top == a.top {
		return false
	}
	a.scrolled, a.top = true, top
	return true
}

// mouse folds one pointer report into the reading position and reports whether the frame has to
// be drawn again. It is the scrollbar's second half: the widget draws the thumb and can say where
// it put it, and this decides whether the pointer is on it.
//
// The left button only, and the strip the bar is drawn in only. Everything else a terminal sends
// while tracking is on has somewhere better to be — a right-click opens the emulator's own menu,
// a middle-click pastes, a drag across the prose selects it — and a program that swallows those
// has taken away gestures the terminal always provided. The wheel never reaches here at all: it
// is decoded as a key, so one binding covers the wheel and the arrow keys together.
//
// Both columns of the strip answer, the ink and the air beside it, because the prose has already
// given up both and a one-column target is a target nobody can hit twice in a row.
//
// The report's row is the terminal's and side's is Live's, and subtracting one from the other is
// exact rather than nearly so on the one surface where a report can arrive: the alternate screen
// keeps the whole frame live, so Live's first row is the screen's first row. The emitter does drop
// rows from the top of a frame taller than the screen, which would shift every row under them —
// but the renderer has trimmed to the height before it ever gets there, so that cannot fire, and
// a frame with no transcript window carries an empty Side and fails the bounds test on width
// alone.
func (a *App) mouse(m term.Mouse) bool {
	if m.Button != term.MouseLeft {
		return false
	}
	sb := ui.ScrollbarWidget{Scroll: ui.Scroll{Above: a.top, Rows: a.rows, Below: a.below}}
	row := m.Row - a.side.Y
	switch m.Action {
	case term.MouseRelease:
		// A release nobody was holding is not ours. Reporting a repaint for it would redraw the
		// frame on every click anywhere on the screen.
		if !a.dragging {
			return false
		}
		a.dragging = false
		return true
	case term.MousePress:
		// The columns are this half's question and the rows are the widget's. Grab is handed the
		// strip's height and refuses a row outside it, so testing the rows here would be the same
		// arithmetic written twice and only one of the copies could ever be wrong; the column the
		// bar is drawn in is the part no widget can know, and an empty strip fails it on width.
		if m.Col < a.side.X || m.Col >= a.side.X+a.side.W {
			return false
		}
		grab, onThumb, ok := sb.Grab(a.side.H, row)
		if !ok {
			return false
		}
		// A press on bare track and a motion end in the same call, which is what makes the click
		// work: Grab hands back the middle of the thumb, and moving at once puts the pill under the
		// pointer. It is also why a press that turns out to be a drag's first report does what the
		// drag does, instead of paging somewhere else and being dragged back from it.
		//
		// A press on the pill moves nothing. The reader is reaching for something already on the
		// screen, and one row of track can be worth many rows of transcript, so asking Offset where
		// the pointer is would answer with the nearest row of that coarse grid — a different row
		// than the thumb was drawn from — and the log would jump the instant it was touched.
		a.dragging, a.grab = true, grab
		if !onThumb {
			a.drag(sb, row)
		}
		// True whatever the window did, because the thumb is held now and held is a style.
		return true

	case term.MouseDrag:
		// The gate that keeps a drag begun on the transcript from taking the bar the moment it
		// crosses into the column. Motion is ours only if the press was.
		if !a.dragging {
			return false
		}
		return a.drag(sb, row)
	}
	return false
}

// drag puts the window where a thumb taken hold of at a.grab and dropped on row would put it,
// and reports whether that moved anything.
//
// Not ok is a frame with no bar in it at all, which a press cannot reach — Grab refused it first —
// and a drag can, because the rows the reader is holding the pointer over may all have arrived while
// they held it and left nothing hidden. Nothing to point at means nothing to move.
//
// The move goes through scroll rather than writing a.top, which is what puts the pointer under
// the same rules as the keys: no downward move while following the tail, a floor at the first
// row, no bottom clamp of its own — the renderer's, reported back — and following resumed by draw
// on the frame where the window lands on the end. Dragging the pill to the bottom therefore hands
// the conversation back, which is the gesture's whole point and not a side effect of it.
func (a *App) drag(sb ui.ScrollbarWidget, row int) bool {
	above, ok := sb.Offset(a.side.H, row, a.grab)
	if !ok {
		return false
	}
	return a.scroll(above - a.top)
}

// page is what a page key moves by: the window, less a row the reader keeps in common between
// the two screens so they can see where they were, and less whatever the pinned header could
// take out of the window at the far end.
//
// That second subtraction is the whole of this function's difficulty. The header's rows are
// spent out of the transcript window, so the window is not the same size at every reading
// position, and a step measured against a window with no header in it can land on a window
// that has one — two rows shorter than the step just travelled, with the rows in between
// belonging to neither screen. Nobody read them and nothing would say so.
//
// Paying for the header whether or not it is up is what closes that, and it costs less than it
// looks: a page is then exactly the same distance on every press, because the rows the header
// is taking now come back out of the correction. What varies is only how much of the far screen
// is already familiar — one row when a header appears there, three when none does — and
// re-reading a line is not the kind of mistake skipping one is.
func (a *App) page() int {
	n := a.rows - 1 - a.slack()
	if n > 1 {
		return n
	}
	return 1
}

// pinned is what the header costs the window right now, and slack is the most it could cost
// somewhere else. The widget itself is asked rather than reasoned about, because whether those
// rows exist is its rule and not this file's: no text, no lost rows, or a terminal too narrow
// for a marker, and there is nothing to pay for.
//
// Both are nothing at all on a surface with no fixed top. Place is what leaves the header out
// of an inline frame — the app installs it on the same terms everywhere, which is why chrome
// does not ask — so inline windows are all the same height and a page there is the plain one.
//
// The width is the transcript's and not the frame's, because that is the width the renderer will
// draw the header at: it is a quotation of a row inside the transcript and wraps where that row
// wrapped. Asking at the frame's width would undercount the rows on exactly the surface that
// reserves a side column, and a page key would then move by one row too many.
func (a *App) pinned() int {
	if !a.scrolled || !a.vp.FixedTop {
		return 0
	}
	t, n := a.r.PromptInside(a.st, a.vp, a.top)
	return len(ui.HeaderWidget{Text: t, Rows: n}.Render(ui.TranscriptWidth(a.vp), 0, a.r.Glyphs))
}

func (a *App) slack() int {
	if !a.vp.FixedTop {
		return 0
	}
	return ui.HeaderRows - a.pinned()
}

// notch is what one turn of the wheel moves by: Config.WheelLines, held between one line and
// a page.
//
// The ceiling is the interesting half. A notch worth more than a screen is a page key wearing
// the wrong name — it would skip text nobody had read, which is the one thing a scroll must
// not do — and the ceiling is a page rather than the window so that two notches, like two
// page keys, still leave a line in common. It also means the setting does not have to be
// chosen against the smallest terminal the user might drag the window down to: four lines on
// a window with three in it is three, and the same config is right on both.
func (a *App) notch() int {
	n := a.cfg.WheelLines
	if n < 1 {
		n = 1
	}
	if p := a.page(); n > p {
		n = p
	}
	return n
}

// fast is what one press of a fast-scroll key moves by, and the difference between its two
// callers is the device rather than the action: a notch sent by the wheel accelerates while the
// wheel is spinning, and ctrl+up never does.
//
// That is not the keymap forking. wheelup and ctrl+up still mean one action, either can be
// rebound to anything, and both still move a notch — what differs is what a hand can afford to
// send. A wheel report is one flick of one finger and there is no way to send fifty of them but
// to mean fifty; a key auto-repeats, so a hand merely resting on ctrl+up would accelerate by
// leaning, and the row it came to rest on would be the keyboard controller's decision rather
// than the reader's. The keyboard's ladder is already four rungs deep — a row, a notch, a
// screen, an end — and a hand that wants to go further reaches for the next rung.
//
// dir is the way the view is about to move rather than the key that asked for it, so a reader
// with an inverted wheel spins on the direction they can see.
func (a *App) fast(k term.Key, dir int) int {
	n := a.notch()
	if k.Type != term.KeyWheelUp && k.Type != term.KeyWheelDown {
		return n
	}
	// A notch worth one row is not a notch, and this is where the one terminal that reports
	// like a finger rather than like a wheel is left alone. Termux sends one report per row a
	// swipe has travelled and -scroll is 1 there, so the distance is already the hand's own
	// measurement of itself: multiplying it would take the text off the thumb that was dragging
	// it, which is the opposite of the request that made that default. It is also what a reader
	// who asked for the finest wheel there is asked for, and undoing that on their second
	// report would be answering some other question.
	if n <= 1 {
		return n
	}
	now := a.cfg.Now()
	// A gesture continues when it is going the same way and arrived soon enough. The first report
	// of a session needs no case of its own: spinDir is zero and a direction never is, so the
	// comparison is false before a wheel has ever turned and the spin starts at one.
	if dir == a.spinDir && now.Sub(a.spinAt) <= accelPeriod {
		a.spin++
	} else {
		a.spin = 1
	}
	a.spinAt, a.spinDir = now, dir
	// The ceiling a notch already has, for the reason it has it: a step longer than a page
	// skips rows nobody read, which is the one thing a scroll must not do however fast the
	// wheel is going. So a spin converges on the page key instead of outrunning it, and what
	// the ramp buys is the distance covered on the way there — seven reports at three rows a
	// notch carry 3+6+9+12+15+18+21 rows instead of 21, which is a long answer in one flick
	// rather than in a spin sustained through thirty notches.
	return min(n*a.spin, a.page())
}

// toTop jumps to the start of the conversation. top == 0 already means row zero is on
// screen, whether that is because the human scrolled there or because the whole transcript
// fits, and in both cases there is nothing above to move to.
func (a *App) toTop() bool {
	if a.em.Scrollback || a.top == 0 {
		return false
	}
	a.scrolled, a.top = true, 0
	return true
}

// follow returns to the tail and stays there. This is the state the session starts in, so
// it is also the way back from anywhere: one key, no arithmetic, and new rows resume
// arriving on screen.
func (a *App) follow() bool {
	if a.em.Scrollback || !a.scrolled {
		return false
	}
	a.scrolled = false
	return true
}

// jump puts one of the reader's own turns on the top row of the window: dir below zero for
// the previous one, above zero for the next.
//
// It is there because a long conversation is searched by landmark and this is the only
// landmark in it. Nobody scrolls back looking for a tool call; they scroll back looking for
// what they asked, and a page key is a poor way to find it — it moves by a screen, which is
// a distance that has nothing to do with where the turns are.
//
// Which rows the prompts are on is a question only the renderer can answer, because the
// answer depends on how the text wrapped, and it is asked again on every press rather than
// kept: the same conversation at a different width is at different rows, so a remembered row
// survives a resize as a wrong answer. The rows come back ascending, which is what lets one
// pass serve both directions.
//
// The comparison is strict, and that is what keeps the two keys honest. a.top is the offset
// the frame reported, following or not, so a landmark already sitting on the top row is not a
// candidate for the key that claims to go to the previous one — without the strictness the
// view would stay where it is and the reader would press again harder.
//
// A target past the end is left past the end, for scroll's reason: the renderer owns the last
// window and reports the offset it used, so a jump to a turn too near the tail to reach the
// top row lands as far down as the log goes and hands following back through f.Scroll.Below.
// Finding nothing does nothing — there is no next turn to go to — which leaves the frame
// alone instead of nudging the offset to a row the next frame would take away again.
func (a *App) jump(dir int) bool {
	if a.em.Scrollback {
		return false
	}
	target := -1
	for _, row := range a.r.PromptRows(a.st, a.vp) {
		switch {
		case dir < 0 && row < a.top:
			target = row
		case dir > 0 && row > a.top && target < 0:
			target = row
		}
	}
	if target < 0 {
		return false
	}
	a.scrolled, a.top = true, target
	return true
}

// sync takes the terminal's size from the terminal rather than from the last event that
// mentioned it. Cheap — one ioctl — and it is what makes every frame agree with the
// screen it is written to, including the frames a resize event has not caught up with
// yet. See Config.Size.
func (a *App) sync() {
	if a.cfg.Size == nil {
		return
	}
	w, h := a.cfg.Size()
	if w <= 0 || h <= 0 || (w == a.vp.Width && h == a.vp.Height) {
		return
	}
	a.resize(w, h)
}

// settle is the last frame of the session, and the only one that gives the terminal
// anything to keep.
//
// While the session runs, nothing is handed over at all. The interactive surface — the
// alternate buffer, or the whole screen inline — is repainted whole, every frame, which is
// what lets a resize re-lay-out the history instead of watching the terminal reflow rows we
// had declared frozen and could no longer reach. The price is that the transcript exists
// only inside our claim, and the rows on screen when the loop ends are rows we still own:
// walking out then would leave the user with an input bar that answers nothing, or with
// nothing at all, depending on the surface.
//
// So this renders with no input, no widgets and no height. No height is how Render is told
// the caller wants a document rather than a screen: it stops holding a live region and
// declares the whole transcript committed, so the terminal receives it in one piece, wrapped
// to the width the session ended on, and keeps it. The emitter releases the claim before
// printing — leaving the alternate buffer, or erasing the screen it took — so what the user
// is left with is the conversation and no trace of the interface, and whatever was on their
// screen before the session began is exactly where they left it.
//
// Both surfaces settle. Alt used to return here without writing anything, on the reasoning
// that nothing it drew had reached the terminal and the buffer switch was about to give the
// screen back; the consequence was that a session run with -alt ended by throwing the entire
// conversation away. Which surface the rows were drawn on is not a reason to keep them from
// the user — it is the reason this is the only place they are written.
func (a *App) settle() error {
	a.sync()
	a.r.Widgets = nil
	vp := a.vp
	vp.Height = 0
	f := a.r.Render(a.st, nil, vp)
	f.Cursor = ui.Cursor{Hidden: true}
	return a.write(a.em.Emit(f))
}

func (a *App) write(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	_, err := a.cfg.Out.Write(b)
	return err
}

// shine returns this frame's band in the given theme key, or no band at all when the
// caller never asked for one. The shape is the config's and the clock is ours: Phase is
// overwritten with this loop's counter, which is the same counter the spinner is indexed
// by, so the two bands and the spinner can never drift apart or need a second timer.
//
// Config.Shine.Style is read as the switch on the way in and replaced by the key on the way
// out. That is the whole of the asymmetry the Config field documents: a caller says whether
// there is a light, and this package says which of ui's two declared keys each one is drawn
// in.
func (a *App) shine(key string) ui.Shimmer {
	s := a.cfg.Shine
	if !s.On() {
		return ui.Shimmer{}
	}
	s.Style, s.Phase = key, a.phase
	return s
}

// inputShine is the band on the input box, armed exactly while the interface is waiting for
// something and nothing has been typed yet.
//
// Two conditions, and both of them are the animation's meaning rather than a detail of it.
// Not while the run is working, because what this light says is "it is your turn" — a box
// glinting through a tool call contradicts the status row one line below it, and ui.Working
// is exported so that the two cannot disagree about which state that is. And not once the
// line has something in it, because from then on the human is what is moving on the screen:
// a light sweeping under a half-typed sentence competes with their own cursor for the eye,
// and the thing it was there to announce has already happened.
//
// The input keeps the same visible pass as the status shimmer. Slower(2) creates
// its established cadence, then LongerRest(4) multiplies only that cadence's dark
// interval: eight visible ticks and 288 dark ticks with the shipped values.
func (a *App) inputShine() ui.Shimmer {
	if ui.Working(a.st) || !a.ed.Empty() {
		return ui.Shimmer{}
	}
	return a.shine(ui.InputShine).Slower(2).LongerRest(4)
}

// workTitle is what the input's top border says while the run works: the spinner's
// frame on the shared clock and the verb. The same vocabulary the status row used,
// moved rather than rewritten.
func (a *App) workTitle() string {
	return ui.SpinnerFrame(a.phase, a.r.Glyphs) + " working"
}

// workQuiet reports whether the working verb has somewhere else to be this frame.
// It has whenever the input's border exists and will actually draw the title —
// HostsTitle is the widget's own fit test, so a terminal too narrow for the word
// keeps the verb in the bottom row instead of dropping it between the two homes.
func (a *App) workQuiet() bool {
	return ui.Working(a.st) && a.ed.HostsTitle(a.workTitle(), a.vp.Width, a.r.Glyphs)
}

// A compositionStep is one row of the chrome: a name — the stable ID of the widget or
// widgets it installs, and the word a [layout] table will be allowed to say — the slot
// those widgets ask for, and a builder that answers what this frame wants from it. A
// builder that returns nothing is a row the frame has no use for this time; the gates
// that decide so live inside the builder, so a step is read as one piece rather than as
// a question about a function that lives somewhere else.
//
// Some builders act and not only ask: effort hands its slider the animation tick,
// and the armed notice retires an arm that has timed out. The list is walked in
// order and the builders run in order, which is what keeps those effects where the
// hand-written chrome had them — build order is behaviour, not an implementation
// detail the day a builder clears a field a later builder would have read. A
// [layout] table may reorder or veto what is installed, but the builders still all
// run, in this order, whatever it says.
//
// The slot is declared here rather than asked of the built widget so that the
// vocabulary a config may address — name and slot together — is data, readable
// without an App to build against. TestTheCompositionNamesItsWidgets pins the
// declaration against the widgets' own answers, so the two cannot drift.
type compositionStep struct {
	name  string
	slot  ui.Slot
	build func(*App) []ui.Widget
}

// defaultComposition is the widget list for a frame, in the order it is installed:
// what the state demands, then what the player adds on top. It is the data form of
// the composition chrome() used to write by hand, and its default order is today's
// order byte for byte — pinned by TestTheDefaultCompositionIsToday, which is the
// test any reordering has to argue with.
//
// The inventory this sits in, so nobody has to go find it again: the list is built
// at the two install sites, the draw loop and Fold (both `a.r.Widgets = a.chrome()`),
// and consumed at the renderer's two Place sites — slot(), which stacks every widget
// that resolved into a row slot, and side(), which hands the right column to the
// first widget that claims it and leaves any second one out.
//
// The names are the widget's own Name(), asserted against it by the same test, and
// the vocabulary is closed: every name is unique, which is what a [layout] row
// addresses. It was not always so — the end-of-scenario notice and the armed
// interrupt both used to be "notice" — and the split is a spec decision
// (spec/look.md): the end of the scenario keeps the generic name, and the armed
// second-press warning is "interrupt", its own row under the input.
var defaultComposition = []compositionStep{
	// What the state itself demands, which today is the one open question. ui.ChromeFor
	// stays the owner of that question — see its comment for why the status row is
	// deliberately not among its answers.
	{"approval", ui.SlotBelowInput, func(a *App) []ui.Widget { return ui.ChromeFor(a.st) }},
	// The Tasks panel, fixed above the input: the summary line collapsed, the
	// bounded list dropped down. Either state renders nothing at all when the run
	// has no tasks, so a recording without task events costs the frame nothing.
	{"tasks", ui.SlotAboveInput, func(a *App) []ui.Widget {
		if len(a.st.Tasks) == 0 {
			return nil
		}
		return []ui.Widget{ui.TasksWidget{St: a.st, Open: a.tasksOpen}}
	}},
	// The inline effort slider, above the input, while the human is picking a level.
	{"effort", ui.SlotBelowInput, func(a *App) []ui.Widget {
		if a.effortSlider == nil {
			return nil
		}
		a.effortSlider.Phase = a.phase
		return []ui.Widget{EffortWidget{Slider: a.effortSlider}}
	}},
	// The slash suggestion dropdown, above the input, while the human is typing
	// a command name.
	{"slashmenu", ui.SlotBelowInput, func(a *App) []ui.Widget {
		if !a.smenu.Active() {
			return nil
		}
		return []ui.Widget{SlashMenuWidget{Menu: a.smenu}}
	}},
	// The pinned header, and the one place the rule "only while scrolled" is written down. A
	// reader following the tail is looking at their own last message a few rows up; a reader
	// who has scrolled away is not, and the header exists for exactly that gap. So it is not
	// installed while the recording plays, and the screen stays clean when nobody is looking
	// for a landmark.
	//
	// Whether the row can be drawn at all is a separate question with a separate owner: the
	// widget asks for the top slot, and Place leaves it out on any surface that has no fixed
	// top. Gating here on the surface as well would be the same decision made twice, in the
	// one layer that is supposed to be free of modes.
	//
	// a.top is the offset the last frame actually used, which is also the offset this frame
	// will be built with — draw sets the pair from these fields and Render only disagrees when
	// it clamps, which the following frame has already corrected.
	{"header", ui.SlotTop, func(a *App) []ui.Widget {
		if !a.scrolled {
			return nil
		}
		if t, n := a.r.PromptInside(a.st, a.vp, a.top); n > 0 {
			return []ui.Widget{ui.HeaderWidget{Text: t, Rows: n}}
		}
		return nil
	}},
	// The scrollbar, on the same terms as the header and for the same reason, one layer down:
	// the widget asks for the right slot, and Place leaves it out wherever a side column cannot
	// be held — which inline mode cannot, because a column down the edge of scrollback would be
	// a column drawn again on every frame. So chrome does not ask about the surface.
	//
	// It is installed whether or not there is anything to scroll, which is the difference
	// between this and the header: the bar's own geometry answers that question — a transcript
	// that fits draws no thumb and therefore no bar at all — and the columns it would sit in
	// are reserved by the layout regardless, so an installed bar with nothing to say costs the
	// frame nothing. Gating it here on a row count would put the same test in two places, and
	// the one here would be reading the previous frame's scroll.
	//
	// Held is the one thing about the bar this layer knows and the widget cannot: a widget is
	// built fresh every frame and a drag spans many, so whoever is tracking the pointer is the
	// only thing that can say the thumb is under a finger right now. Its scroll numbers arrive
	// the other way about, from the renderer, for the reason ScrollReader gives.
	{"scrollbar", ui.SlotRight, func(a *App) []ui.Widget {
		return []ui.Widget{ui.ScrollbarWidget{Held: a.dragging}}
	}},
	// The end-of-scenario notice. The key is asked of the keymap rather than written out
	// here, so the sentence names whatever the reader's own [keys] table binds instead of
	// whatever we shipped. It can bind nothing at all — ActionNone takes a default away —
	// and then the notice stops after the half of it that is still true, because naming no
	// key beats naming a key that does not leave.
	{"notice", ui.SlotBelowInput, func(a *App) []ui.Widget {
		if !a.st.Finished {
			return nil
		}
		leave := "the input bar is yours"
		if k := a.km.KeyFor(ActionQuit); k != "" {
			leave += ", " + k + " to leave"
		}
		return []ui.Widget{ui.NoticeWidget{Text: "end of scenario — " + leave}}
	}},
	// The armed interrupt, said out loud, which is what keeps the second press from being
	// folklore: the reader has just watched their line vanish, and the row under the input
	// tells them what the same key does now. KeyFor cannot answer "" here — the arm is only
	// ever raised by a key that looked up to ActionInterrupt, so at least that binding
	// exists — and if a config binds several, any of them is a true answer, because the
	// second press is matched on the action and not on the key.
	//
	// The row is an InterruptWidget rather than the NoticeWidget it draws so that its
	// config name is its own: "interrupt" in a [layout] list, where the end-of-scenario
	// notice is "notice". Two rows that could not be told apart were two rows a layout
	// could not address, and spec/look.md settles the split here rather than in the
	// spec's silence.
	{"interrupt", ui.SlotBelowInput, func(a *App) []ui.Widget {
		if !a.armed {
			return nil
		}
		if a.cfg.Now().Sub(a.armedAt) > armTimeout {
			a.armed = false
			return nil
		}
		return []ui.Widget{InterruptWidget{NoticeWidget: ui.NoticeWidget{
			Text: "press " + a.km.KeyFor(ActionInterrupt) + " again to leave",
			Warn: true,
		}}}
	}},
	// The recap widget, shown above the input after a response finishes. It is
	// only installed when state.Recap is true and the agent is no longer active.
	{"recap", ui.SlotAboveInput, func(a *App) []ui.Widget {
		if !a.st.Recap || ui.Working(a.st) || len(a.st.Items) == 0 {
			return nil
		}
		return []ui.Widget{ui.RecapWidget{St: a.st}}
	}},
	// The status row is installed here and not by ChromeFor because its phase comes from
	// this loop's wall clock, which no state field carries — and so does its band of light,
	// which is the same argument twice. It is handed in unconditionally: the widget draws it
	// on the verb "working" and on nothing else, so a frame that is not working carries a
	// shimmer nobody applies rather than asking this function to know the ladder.
	//
	// Last in the list, which for a bottom-slot widget is only tidiness — but it is also the
	// order a config will be allowed to rewrite, and last is where a row that summarizes the
	// others belongs.
	//
	// Quiet because the working verb — its spinner and its band of light with it — moved
	// into the input's top border, and a bottom row repeating it would be the same news
	// twice on one screen. The other rungs of the ladder stay here: blocked, waiting, done
	// and idle are states the border says nothing about.
	{"status", ui.SlotBottom, func(a *App) []ui.Widget {
		return []ui.Widget{ui.StatusWidget{St: a.st, Phase: a.phase, Shine: a.shine(ui.StatusShine), Quiet: a.workQuiet()}}
	}},
}

// InterruptWidget is the armed-interrupt notice under its own name. It draws exactly
// what ui.NoticeWidget draws — it is one, embedded — and answers "interrupt" to the
// one question that matters here: what a config calls this row. The end-of-scenario
// notice keeps the generic name because it is the generic row; this one is a specific
// warning with a specific remedy, and a reader vetoing it wants this one and not the
// other.
type InterruptWidget struct {
	ui.NoticeWidget
}

func (InterruptWidget) Name() string { return "interrupt" }

// WidgetDecl is one row of the composition's vocabulary: the name a [layout] list
// writes, the slot the row asks for, and a line of documentation for `arxi-sim keys`.
type WidgetDecl struct {
	Name string
	Slot ui.Slot
	Doc  string
}

// CompositionWidgets is the widget vocabulary a [layout] table may name, in
// composition order. It is the same closed list the config parser checks against, so
// a name a file may write is a name this program draws, or the file is refused.
func CompositionWidgets() []WidgetDecl {
	out := make([]WidgetDecl, len(defaultComposition))
	for i, step := range defaultComposition {
		out[i] = WidgetDecl{Name: step.name, Slot: step.slot, Doc: widgetDocs[step.name]}
	}
	return out
}

// widgetDocs is the one-line answer to "what is this row", printed by `arxi-sim keys`
// beside each name. It lives beside the vocabulary it documents, and not on the step
// itself, because the composition is the interface's skeleton — order, slots, gates —
// while this is its label.
var widgetDocs = map[string]string{
	"approval":  "the pending approval question, under the input",
	"tasks":     "the task panel above the input, collapsed or dropped down",
	"effort":    "the inline effort slider while a level is being picked",
	"slashmenu": "the slash command dropdown while a command is being typed",
	"header":    "the pinned header, while the transcript is scrolled",
	"scrollbar": "the scrollbar beside the transcript",
	"notice":    "the end-of-scenario notice",
	"interrupt": "the armed warning that a second press leaves",
	"recap":     "the recap of the turn, after a response finishes",
	"status":    "the status row at the foot of the frame",
}

// LayoutOverride is one [layout] row as parsed: the slot it addresses, at most one
// condition under which it applies, and the ordered widget names. Width > 0 means the
// row applies only while the terminal is narrower than that many columns; Height > 0
// only while the frame is shorter than that many rows; both zero is the unconditional
// row. Names may be empty — that is a veto, the slot's rows left out entirely.
type LayoutOverride struct {
	Slot   ui.Slot
	Width  int
	Height int
	Names  []string
}

// layoutFor resolves the overrides against a frame's geometry: per slot, the winning
// row's names, or nil when no row addressed the slot and the composition's own order
// stands. The rules are spec/look.md's, and the one that is not obvious from the code
// is the tie: among matching rows of one kind the narrowest bracket wins, and a
// matching height row beats a matching width row, because height is the scarcer axis —
// the transcript scrolls and the chrome stacks.
//
// h <= 0 is a fold — a document with no screen — and no height row matches there; a
// width row still can, against the width the fold will be printed at.
func layoutFor(overrides []LayoutOverride, w, h int) map[ui.Slot][]string {
	var out map[ui.Slot][]string
	best := map[ui.Slot]LayoutOverride{}
	for _, o := range overrides {
		if o.Width > 0 && !(w > 0 && w < o.Width) {
			continue
		}
		if o.Height > 0 && !(h > 0 && h < o.Height) {
			continue
		}
		cur, ok := best[o.Slot]
		if !ok || tierRank(o) > tierRank(cur) {
			best[o.Slot] = o
		}
	}
	for slot, o := range best {
		if out == nil {
			out = map[ui.Slot][]string{}
		}
		out[slot] = o.Names
	}
	return out
}

// tierRank orders two matching rows: height beats width, and within a kind the
// narrower bracket beats the wider. A rank of 0 is the unconditional row, which any
// matching tier outranks.
func tierRank(o LayoutOverride) int {
	switch {
	case o.Height > 0:
		return (1 << 20) + o.Height
	case o.Width > 0:
		return o.Width
	}
	return 0
}

// chrome is the widget list for this frame: the composition, walked in order — and,
// when a [layout] table spoke, re-stacked per slot by what it said. The builders run
// in the default order either way, because build order is behaviour (effort's tick,
// the arm's expiry); a layout may only change which built widgets are installed, and
// in what order within their slot. With no layout the walk is the whole answer, which
// is the byte-identical default the composition goldens pin.
func (a *App) chrome() []ui.Widget {
	var out []ui.Widget
	if len(a.layout) == 0 {
		for _, step := range defaultComposition {
			out = append(out, step.build(a)...)
		}
		return out
	}
	built := map[string][]ui.Widget{}
	slots := []ui.Slot{} // first-appearance order, so unnamed slots stay deterministic
	for _, step := range defaultComposition {
		built[step.name] = step.build(a)
		if len(slots) == 0 || slots[len(slots)-1] != step.slot {
			slots = appendUniqueSlot(slots, step.slot)
		}
	}
	resolved := layoutFor(a.layout, a.vp.Width, a.vp.Height)
	for _, slot := range slots {
		names, ok := resolved[slot]
		if !ok {
			// No row addressed this slot, or none matched: the composition's own
			// order stands, restricted to this slot.
			for _, step := range defaultComposition {
				if step.slot == slot {
					out = append(out, built[step.name]...)
				}
			}
			continue
		}
		for _, name := range names {
			out = append(out, built[name]...)
		}
	}
	return out
}

func appendUniqueSlot(slots []ui.Slot, s ui.Slot) []ui.Slot {
	for _, have := range slots {
		if have == s {
			return slots
		}
	}
	return append(slots, s)
}

// Fold applies the whole scenario at once and returns the frame it produces. It is what
// --instant prints, and what a machine gets when stdout is not a terminal: barriers are
// crossed without waiting, because the reply the recording carries is the answer and
// nobody is there to type another one.
func (a *App) Fold() ui.Frame {
	for _, s := range a.steps {
		if s.IsBarrier() {
			continue
		}
		a.clock = s.At
		a.st.Apply(s.Event, s.At)
	}
	a.next = len(a.steps)
	a.finish()
	a.r.Widgets = a.chrome()
	// Height 0 on purpose: a fold is a document, not a screen. Rendering at the
	// viewport's height would commit by geometry and show only the tail, which is right
	// for a terminal and wrong for -instant, for a pipe and for a golden file, all three
	// of which want the whole transcript. Width still comes from the viewport, because a
	// wrap is a fact about the text and not about the surface.
	vp := a.vp
	vp.Height = 0
	return a.r.Render(a.st, a.ed, vp)
}

// State exposes what was folded, for a test and for a `--dump` that has to assert
// something more specific than the frame.
func (a *App) State() *state.State { return a.st }

func (a *App) openTeam() { a.openFullView(viewTeam) }

func (a *App) openFullView(view appView) {
	a.view = view
	a.viewTop = 0
	a.viewRows, a.viewBelow = 0, 0
}

func (a *App) closeFullView() { a.view = viewConversation }

func (a *App) fullViewKey(act Action) bool {
	if a.view == viewConsent && len(a.consents) > 0 {
		current := a.consents[0]
		switch act {
		case ActionApprove:
			if err := a.cfg.Extensions.Grant(current.Name); err != nil {
				a.st.AppendNotice(state.NoticeQuiescent, current.Name+": "+err.Error())
				return true
			}
		case ActionDeny, ActionCancel:
			a.cfg.Extensions.Reject(current.Name)
		default:
			return false
		}
		a.consents = a.consents[1:]
		if len(a.consents) == 0 {
			a.closeFullView()
		}
		return true
	}
	switch act {
	case ActionCancel, ActionInterrupt:
		a.closeFullView()
		return true
	case ActionScrollUp, ActionHistoryPrev:
		return a.fullViewScroll(-1)
	case ActionScrollDown, ActionHistoryNext:
		return a.fullViewScroll(1)
	case ActionScrollUpFast:
		return a.fullViewScroll(-max(1, a.cfg.WheelLines))
	case ActionScrollDownFast:
		return a.fullViewScroll(max(1, a.cfg.WheelLines))
	case ActionPageUp:
		return a.fullViewScroll(-max(1, a.viewRows-1))
	case ActionPageDown:
		return a.fullViewScroll(max(1, a.viewRows-1))
	case ActionHome, ActionScrollTop:
		if a.viewTop == 0 {
			return false
		}
		a.viewTop = 0
		return true
	case ActionEnd, ActionScrollBottom:
		if a.viewBelow == 0 {
			return false
		}
		a.viewTop += a.viewBelow
		return true
	case ActionSubmit:
		return false
	}
	return false
}

func (a *App) fullViewScroll(n int) bool {
	top := min(max(0, a.viewTop+n), a.viewTop+a.viewBelow)
	if top == a.viewTop {
		return false
	}
	a.viewTop = top
	return true
}

// overlayKey routes a keypress to the active overlay handler.
func (a *App) overlayKey(act Action, k term.Key) bool {
	if a.ovHandler == nil {
		return false
	}
	dirty, done := a.ovHandler.Handle(act, k)
	if done {
		a.closeOverlay()
	}
	if !done && dirty {
		a.overlay = a.ovHandler.Build()
	}
	return dirty
}

// openOverlay installs an overlay and its handler.
func (a *App) openOverlay(h OverlayHandler) {
	a.ovHandler = h
	a.overlay = h.Build()
}

// closeOverlay dismisses the overlay.
func (a *App) closeOverlay() {
	a.overlay = nil
	a.ovHandler = nil
}

// effortKey routes a keypress to the inline effort slider.
func (a *App) effortKey(act Action, k term.Key) bool {
	if a.effortSlider == nil {
		return false
	}
	dirty, done := a.effortSlider.Handle(act, k)
	if done {
		if lv := a.effortSlider.Level(); lv != "" {
			a.st.Effort = lv
		}
		a.effortSlider = nil
		return true
	}
	return dirty
}

// slashCommand returns the command name and arguments if text starts with '/', else "", "".
func slashCommand(text string) (string, string) {
	if len(text) == 0 || text[0] != '/' {
		return "", ""
	}
	raw := text[1:]
	for i, c := range raw {
		if c == ' ' || c == '\t' {
			return raw[:i], strings.TrimSpace(raw[i+1:])
		}
	}
	return raw, ""
}

// runSlash dispatches a slash command.
func (a *App) runSlash(cmd, args string) bool {
	switch cmd {
	case "effort":
		// /effort <level> sets directly without the slider.
		if args != "" {
			for _, lv := range effortLevels {
				if strings.EqualFold(lv, args) {
					a.st.Effort = lv
					return true
				}
			}
			return true // unknown level, consume but ignore
		}
		current := a.st.Effort
		if current == "" {
			current = "high"
		}
		a.effortSlider = NewEffortSlider(current, a.vp.Width)
		return true
	case "team":
		a.openTeam()
		return true
	case "tasks":
		a.tasksOpen = !a.tasksOpen
		return true
	case "config", "settings":
		a.openConfig()
		return true
	case "panels":
		names := a.panelNames()
		text := "No extension panels."
		if len(names) > 0 {
			text = "Extension panels: " + strings.Join(names, ", ")
		}
		a.st.AppendNotice(state.NoticeQuiescent, text)
		return true
	case "panel":
		return a.openPanel(parsePanelName(args))
	case "recap":
		a.st.Recap = !a.st.Recap
		return true
	}
	if strings.HasPrefix(cmd, "ext:") && a.cfg.Extensions != nil {
		name := strings.TrimPrefix(cmd, "ext:")
		go a.cfg.Extensions.Invoke(name, "", args)
		return true
	}
	return false
}

// clean is the difference between what a clipboard may hold and what the editor may. A
// newline survives, because the editor now has rows to put them on. A carriage return
// becomes one: the decoder folds CRLF and lone CR already, so this is the second line of
// defence and not the first, but a bare CR that got past it would move the cursor to the
// start of the row and overwrite what the human typed. A tab becomes a space, because the
// input is the one text in the frame nothing expands tabs for. Every other control byte is
// dropped rather than shown: an ESC in a paste is not content, and a clipboard holding one
// is either an accident or an attempt to write to the terminal through the input line.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\r':
			return '\n'
		case r == '\t':
			return ' '
		case r < ' ' || r == 0x7f:
			return -1
		}
		return r
	}, s)
}

func configClean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < ' ' || r == 0x7f:
			return -1
		}
		return r
	}, s)
}
