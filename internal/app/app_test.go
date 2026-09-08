package app

// The player is tested the way a terminal drives it: events in on a channel, bytes out
// to a writer, nothing else. Two decisions elsewhere are what make that possible — the
// emitter returns bytes instead of writing them, so a test's terminal is a buffer, and
// Speed divides every recorded delay, so a recording that plays for eleven seconds
// finishes here in microseconds.
//
// These tests synchronize on the output and never on a sleep. The frame is what the
// player promises, so the frame is the only thing a test is allowed to wait for; waiting
// on a duration is a test that passes on a fast machine and fails on a loaded one.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/scenario"
	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

// probe is the terminal a test pretends to be. It keeps every byte for an assertion and
// closes a channel the first time the output contains the string a test is waiting for.
type probe struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	want string
	hit  chan struct{}
}

func (p *probe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf.Write(b)
	p.check()
	return len(b), nil
}

// check fires the pending wait once its string has arrived. Called with the lock held.
func (p *probe) check() {
	if p.want == "" || !strings.Contains(plain(p.buf.String()), p.want) {
		return
	}
	p.want = ""
	close(p.hit)
}

// wait arms the probe and blocks until the string is written. Arming looks at what has
// already arrived, because the player is much faster than the test and has usually got
// there first; a wait that only watched future writes would hang on its own success.
func (p *probe) wait(t *testing.T, s string) {
	t.Helper()
	p.mu.Lock()
	p.want, p.hit = s, make(chan struct{})
	hit := p.hit
	p.check()
	p.mu.Unlock()
	select {
	case <-hit:
	case <-time.After(5 * time.Second):
		t.Fatalf("waited 5s for %q on screen; the output so far was:\n%s", s, plain(p.text()))
	}
}

func (p *probe) text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

// plain is what the terminal would have shown: the escape sequences applied instead of
// printed. Every span is emitted with its own SGR, so a styled question reaches the writer
// with a reset between each word and no assertion could ever match the sentence raw. The
// repaints stack up in here — this is a byte stream, not a screen — which is exactly why
// a wait is written as a substring and never as an equality.
func plain(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '[':
			// CSI: parameter and intermediate bytes, then one final byte at 0x40 or above.
			for i++; i < len(s) && s[i] < '@'; i++ {
			}
		case ']', 'P':
			// OSC and DCS carry a string, ended by BEL or by ST.
			for i++; i < len(s); i++ {
				if s[i] == 0x07 {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
			}
		}
		// Anything else is a two-byte escape, and i is already on its second byte.
	}
	return b.String()
}

const firstConversation = "../../testdata/scenarios/01-first-conversation.ndjson"

// play starts a recording at a speed that makes every recorded delay vanish, and hands
// back the four things a test drives it with. The App is read only after Run returns:
// while that goroutine is alive, everything inside it belongs to it.
func play(t *testing.T, path string) (*App, chan term.Event, *probe, <-chan error) {
	t.Helper()
	sc, err := scenario.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	if errs := scenario.Validate(sc); len(errs) > 0 {
		t.Fatalf("%s does not validate: %v", path, errs)
	}
	p := &probe{}
	evs := make(chan term.Event, 16)
	a := New(Config{
		Scenario: sc,
		Events:   evs,
		Out:      p,
		// Mono: an assertion reads better against text than against SGR, and the
		// styling has tests of its own.
		Emitter: &ui.Emitter{Theme: ui.DefaultTheme(), Mode: ui.ModeInline, Profile: ui.ProfileMono},
		Width:   80,
		Height:  24,
		Speed:   1e6,
	})
	errc := make(chan error, 1)
	go func() { errc <- a.Run() }()
	return a, evs, p, errc
}

func mustKey(t *testing.T, name string) term.Key {
	t.Helper()
	k, ok := term.ParseKey(name)
	if !ok {
		t.Fatalf("%q is not a key name", name)
	}
	return k
}

// press sends one keypress under the name a user would put in a config, which is also
// the name the decoder produces. A test that built a Key literal instead would agree
// with the keymap by construction and prove nothing about either.
func press(t *testing.T, evs chan term.Event, name string) {
	t.Helper()
	evs <- term.Event{Kind: term.EventKey, Key: mustKey(t, name)}
}

// typeLine types a line as one burst and submits it, which is how a terminal delivers a
// line somebody typed quickly.
func typeLine(evs chan term.Event, s string) {
	evs <- term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyRunes, Runes: []rune(s)}}
	evs <- term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyEnter}}
}

// The whole point of the simulator in one test: the recording stops on a question, the
// human answers it with the key the widget advertises, the run resumes on the log's own
// events, and a line typed at the end arrives in the transcript as a real prompt.
func TestTheHumanAnswersAndSpeaks(t *testing.T) {
	a, evs, p, errc := play(t, firstConversation)

	// Parked on inbox-1. The question is on screen and no amount of waiting moves it.
	p.wait(t, "approve bash for solo?")
	press(t, evs, "y")

	// The recording resumed on its own events and ran out of things to say.
	p.wait(t, "no pending effects")
	typeLine(evs, "apply it")
	p.wait(t, "end of scenario")
	// ctrl+d and not ctrl+c: one press of it leaves, which is the whole of what this test
	// needs from the door. What ctrl+c does instead is two presses and its own tests below.
	press(t, evs, "ctrl+d")

	if err := <-errc; err != nil {
		t.Fatalf("Run: %v", err)
	}
	st := a.State()
	if !st.Finished {
		t.Error("the log ran out and Finished is still false")
	}
	// One notice, not two: the recording replies to inbox-1 as well, and applying both
	// replies is the regression this stands against.
	if n := countNotices(st, state.NoticeApproval); n != 1 {
		t.Errorf("%d approval notices, want exactly 1", n)
	}
	if in := st.OpenInbox(); in != nil {
		t.Errorf("inbox %s is still open after the human answered it", in.ID)
	}
	if got := st.Inboxes[0].Answer; got != "allow" {
		t.Errorf("inbox answered %q, want %q: y stands for the word the widget draws", got, "allow")
	}
	// Only the Apply path clears the diagnosis, which is the reason a submitted line is
	// a synthesized event rather than a call to AppendPrompt.
	if st.Quiescent != "" {
		t.Errorf("a prompt left the diagnosis %q standing underneath it", st.Quiescent)
	}
	if st.Blocked != nil {
		t.Errorf("still blocked on %q: the recording's own agent.unblocked was dropped", st.Blocked.On)
	}
	it := lastPrompt(st)
	if it == nil {
		t.Fatal("the human typed a line and the transcript has no prompt in it")
	}
	if it.Text != "apply it" {
		t.Errorf("last prompt is %q, want %q", it.Text, "apply it")
	}
	// The id the renderer memoizes on is prompt-<seq>, so a synthesized event reusing a
	// recorded seq would be drawn as a line the recording already committed.
	if high := maxSeq(a.steps); it.Seq <= high {
		t.Errorf("the human's prompt took seq %d; the recording already used up to %d", it.Seq, high)
	}
}

// countNotices counts the notices of one kind, which is how a test says "exactly once"
// about something the state only exposes as transcript items.
func countNotices(st *state.State, kind state.NoticeKind) int {
	n := 0
	for _, it := range st.Items {
		if it.Kind == state.KindNotice && it.Notice == kind {
			n++
		}
	}
	return n
}

func lastPrompt(st *state.State) *state.Item {
	for i := len(st.Items) - 1; i >= 0; i-- {
		if st.Items[i].Kind == state.KindPrompt {
			return &st.Items[i]
		}
	}
	return nil
}

// A human can answer before the recording asks. The frame that shows a question is drawn
// when inbox.created is applied, two steps before the barrier that waits on it, so at any
// real speed there is a window in which the answer lands first. It has to let the run
// through: the alternative is a scenario parked forever on a question already answered.
func TestAnAnswerAheadOfItsBarrierStillReleasesIt(t *testing.T) {
	steps := []scenario.Step{{Await: scenario.AwaitPrompt}}
	a := New(Config{Scenario: &scenario.Scenario{Steps: steps}})

	a.release(scenario.AwaitPrompt)
	if a.advance() {
		t.Error("crossing a barrier asked for a frame; a barrier is a note to the player, not something to draw")
	}
	if a.barrier != "" {
		t.Fatalf("parked on %q after an answer that satisfies it", a.barrier)
	}
	// And exactly one barrier: the credit is spent, so the same token waits again. Two
	// prompts do not license skipping a question.
	a.next = 0
	a.advance()
	if a.barrier != scenario.AwaitPrompt {
		t.Error("one answer crossed two barriers")
	}
}

// A paste is content, and now that the editor holds rows it keeps the rows it arrived with:
// a pasted command block is the block it was, which is what pasting one was for. Everything
// else a clipboard can carry that a row cannot is folded or dropped, and that is worth
// asserting arm by arm, because a paste is the only path by which text nobody typed reaches
// the input line — and so the only path by which an escape sequence could.
//
// The CR arm is the second line of defence and not the first: the decoder folds CRLF and
// lone CR into LF before an event is built, so a CR reaching here has got past that. It
// still has to become a break rather than survive, because a bare CR in a drawn row moves
// the terminal's cursor to column 0 and overwrites what the human typed.
//
// Dropping an ESC leaves the bytes that followed it, and they are ordinary text from there
// on: "\x1b[31m" pasted into the line is four printable characters and no longer a sequence
// the terminal can obey. That is the property being asserted — not that a paste is tidied
// up, but that nothing in it can address the terminal.
func TestAPasteKeepsItsLinesAndNothingElse(t *testing.T) {
	cases := map[string]struct{ text, want string }{
		"a break is a row":      {"one\ntwo", "one\ntwo"},
		"a bare CR becomes one": {"one\rtwo", "one\ntwo"},
		"a tab is a space":      {"go\ttest", "go test"},
		"an escape is dropped":  {"go \x1b[31mtest", "go [31mtest"},
		"a delete is dropped":   {"go\x7ftest", "gotest"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a := New(Config{})
			dirty, err := a.handle(term.Event{Kind: term.EventPaste, Text: c.text})
			if err != nil {
				t.Fatalf("handle paste: %v", err)
			}
			if !dirty {
				t.Error("a paste changed the input line and the frame was not redrawn")
			}
			if got := a.ed.Text(); got != c.want {
				t.Errorf("pasting %q left %q on the line, want %q", c.text, got, c.want)
			}
		})
	}
}

// Multi-line input is two bindings over one editor, and the pair only means anything
// together: shift+enter has to make a row without sending, and enter has to keep sending
// whatever rows are there. One binding doing both is a prompt nobody can finish typing; one
// doing neither is a prompt nobody can send.
//
// The keys are shift+enter, ctrl+enter and ctrl+j, and the reason there are three is that a
// return key's modifier is dropped before the byte leaves. shift+enter and ctrl+enter are both
// 0x0d until the keyboard has been asked to disambiguate — ui.Emitter.Enter asks, and keyTable
// in the term package is where CSI 13;2u and CSI 13;5u are held to their names. ctrl+j is the
// one that needs no asking: Windows Terminal and conhost send 0x0a for ctrl+enter, so binding
// ctrl+j is what makes that chord insert a row on a terminal shipped before CSI u. Here the
// three names are only asked to resolve and to fire; the bytes are the term package's business.
//
// alt+enter is checked for absence in the same test, because it is not an omission. Windows
// Terminal and conhost bind it to fullscreen and never pass it on, so a default that used it
// would be a feature that works everywhere except the machine this is written on.
func TestTheSecondLineIsAKeyAndTheFirstStillSubmits(t *testing.T) {
	for _, name := range []string{"shift+enter", "ctrl+enter", "ctrl+j"} {
		t.Run(name, func(t *testing.T) {
			a := New(Config{})
			a.ed.Insert("first")
			if !a.key(mustKey(t, name)) {
				t.Error("the input grew a row and no frame was asked for")
			}
			a.ed.Insert("second")
			if got, want := a.ed.Text(), "first\nsecond"; got != want {
				t.Fatalf("the line holds %q, want %q", got, want)
			}
			if lastPrompt(a.st) != nil {
				t.Fatalf("%s sent the line instead of continuing it", name)
			}

			if !a.key(mustKey(t, "enter")) {
				t.Error("sending the line asked for no frame")
			}
			if got := a.ed.Text(); got != "" {
				t.Errorf("the line still holds %q after being sent", got)
			}
			it := lastPrompt(a.st)
			if it == nil {
				t.Fatal("enter sent nothing: a prompt with a break in it is still a prompt")
			}
			if want := "first\nsecond"; it.Text != want {
				t.Errorf("the transcript holds %q, want %q: the rows are the prompt", it.Text, want)
			}
		})
	}
	if got, ok := DefaultBindings()["alt+enter"]; ok {
		t.Errorf("alt+enter is bound to %q by default: the terminal this runs on toggles fullscreen with it and never delivers it, so the binding could only ever look broken", got)
	}
}

// ctrl+d is the door, and it takes one press: a simulator has nothing unsaved, so there is
// nothing for a confirmation to protect, and ISIG is off in raw mode so a binding is the only
// way out that exists at all. It draws nothing either — the caller is about to write Exit over
// the frame.
func TestQuitIsOnePressOfCtrlD(t *testing.T) {
	a := New(Config{})
	if a.key(mustKey(t, "ctrl+d")) {
		t.Error("quitting asked for one more frame")
	}
	if !a.quit {
		t.Error("ctrl+d did not quit, and raw mode has no signal to fall back on")
	}
}

// The reported bug, in the words it was reported in: ctrl+c on a half-typed line took the
// whole program down, where every other tool would have dropped the line. So ctrl+c is the
// line's key now, and only a second press in a row is the door.
func TestInterruptClearsTheLineAndOnlyLeavesOnTheSecondPress(t *testing.T) {
	a := New(Config{})
	a.ed.Insert("half a thought")

	if !a.key(mustKey(t, "ctrl+c")) {
		t.Error("the first ctrl+c asked for no frame, having emptied the line and armed a hint")
	}
	if got := a.ed.Text(); got != "" {
		t.Errorf("the first ctrl+c left %q on the line, want it cleared", got)
	}
	if a.quit {
		t.Fatal("one ctrl+c left: that is the reported bug, and clearing the line is what the key is for")
	}
	if !a.armed {
		t.Fatal("the first ctrl+c did not arm the second, so there is no way out of an empty line")
	}

	if a.key(mustKey(t, "ctrl+c")) {
		t.Error("the second ctrl+c asked for one more frame")
	}
	if !a.quit {
		t.Error("ctrl+c twice in a row did not leave")
	}
}

// Twice in a row, and nothing weaker. Every other key takes the arm back, including the keys
// that change nothing else — which is why the disarm lives in key() rather than in the thirty
// returns of dispatch(), and why a disarming press still asks for a frame: the row promising
// the door has to come off the screen even when the keypress did nothing to the line.
func TestAnyOtherKeyDisarmsTheInterrupt(t *testing.T) {
	for _, name := range []string{"a", "left", "ctrl+u", "esc", "enter"} {
		t.Run(name, func(t *testing.T) {
			a := New(Config{})
			if !a.key(mustKey(t, "ctrl+c")) {
				t.Fatal("the first ctrl+c asked for no frame")
			}
			if !a.key(mustKey(t, name)) {
				t.Errorf("%s dropped the arm and asked for no frame, leaving a hint on screen that is no longer true", name)
			}
			if a.armed {
				t.Fatalf("%s left the interrupt armed, so the next ctrl+c leaves on a keypress nobody meant as a second one", name)
			}
			if a.quit {
				t.Fatalf("%s quit", name)
			}
			// And the count starts over rather than resuming: the press after a disarm is a
			// first press, so it clears and arms instead of leaving.
			a.ed.Insert("and another")
			if !a.key(mustKey(t, "ctrl+c")) || a.quit {
				t.Errorf("after %s, ctrl+c left instead of clearing the line", name)
			}
			if got := a.ed.Text(); got != "" {
				t.Errorf("after %s, ctrl+c left %q on the line", name, got)
			}
		})
	}
	// esc and enter on an empty line are the two that prove the point: dispatch has nothing
	// to do for either, so the frame the loop redraws can only come from the disarm.
	a := New(Config{})
	if a.dispatch(ActionCancel, mustKey(t, "esc")) {
		t.Error("esc on an empty line asks for a frame on its own, so the disarm's own repaint is untested here")
	}
	if a.dispatch(ActionSubmit, mustKey(t, "enter")) {
		t.Error("enter on an empty line asks for a frame on its own, so the disarm's own repaint is untested here")
	}
}

// The second press is discoverable or it is folklore. A reader who has just watched their
// line vanish is told, on the row under the input, what the key they pressed does next — and
// told it in the spelling their own [keys] table binds, not the one we shipped.
func TestTheArmedInterruptSaysSoOnScreen(t *testing.T) {
	a, p := scrollable(t, ui.ModeAlt)
	if got := p.frame(t, a); strings.Contains(got, "again to leave") {
		t.Fatalf("the hint is on screen before ctrl+c was ever pressed:\n%s", got)
	}
	if !a.key(mustKey(t, "ctrl+c")) {
		t.Fatal("the first ctrl+c asked for no frame")
	}
	got := p.frame(t, a)
	if want := "press ctrl+c again to leave"; !strings.Contains(got, want) {
		t.Errorf("the interrupt is armed and the screen does not say %q:\n%s", want, got)
	}
	// The recording is folded, so the end-of-scenario notice is up too: two notices are two
	// rows, and nothing dedupes a widget by name.
	if !strings.Contains(got, "end of scenario") {
		t.Errorf("the armed hint displaced the end-of-scenario notice:\n%s", got)
	}
	// And the row is gone the moment it stops being true.
	if !a.key(mustKey(t, "left")) {
		t.Fatal("the disarming key asked for no frame")
	}
	if got := p.frame(t, a); strings.Contains(got, "again to leave") {
		t.Errorf("the arm was dropped and the hint is still on screen:\n%s", got)
	}
}

// Both notices name the key the keymap binds, so a config that moves the door moves the
// sentence with it — and a config that removes the door says nothing rather than naming a key
// that no longer leaves. ActionNone is the only way to take a default away, which makes the
// empty case reachable and therefore worth a branch.
func TestTheNoticesNameWhateverTheConfigBinds(t *testing.T) {
	moved, err := NewKeymap(map[string]Action{
		"ctrl+c": ActionNone,
		"ctrl+d": ActionNone,
		"ctrl+g": ActionInterrupt,
		"f10":    ActionQuit,
	})
	if err != nil {
		t.Fatalf("NewKeymap: %v", err)
	}
	a, p := scrollable(t, ui.ModeAlt)
	a.km = moved
	if !a.key(mustKey(t, "ctrl+g")) {
		t.Fatal("the moved interrupt asked for no frame")
	}
	got := p.frame(t, a)
	for _, want := range []string{"press ctrl+g again to leave", "f10 to leave"} {
		if !strings.Contains(got, want) {
			t.Errorf("the screen does not say %q, so a notice is naming a key the config moved:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ctrl+c") || strings.Contains(got, "ctrl+d") {
		t.Errorf("a notice still names a key this config unbound:\n%s", got)
	}

	// Nothing bound to quit at all: the sentence stops after the half of it that is true.
	none, err := NewKeymap(map[string]Action{"ctrl+c": ActionNone, "ctrl+d": ActionNone})
	if err != nil {
		t.Fatalf("NewKeymap: %v", err)
	}
	b, q := scrollable(t, ui.ModeAlt)
	b.km = none
	if got := q.frame(t, b); !strings.Contains(got, "the input bar is yours") || strings.Contains(got, "to leave") {
		t.Errorf("with no quit binding the notice still promises a way out:\n%s", got)
	}
}

// inboxOpen asks a question the way the runtime does, through a real event, so the test
// drives the same path the recording drives.
func inboxOpen(t *testing.T, a *App, id, question string) {
	t.Helper()
	b, err := json.Marshal(event.InboxCreatedPayload{
		InboxID: id, Kind: "tool_approval", Question: question, Agent: "solo",
	})
	if err != nil {
		t.Fatalf("marshal inbox: %v", err)
	}
	a.st.Apply(&event.Event{
		Seq: 1, Type: event.InboxCreated, Scope: "run:local",
		Source: event.SourceRuntime, Actor: "runtime", Payload: b,
	}, 0)
	if a.st.OpenInbox() == nil {
		t.Fatal("inbox.created left no open question")
	}
}

// y and n are letters first. They answer only while a question is open and the line is
// empty, which is what lets somebody type "yes, do it" as a reply without the y being
// swallowed as a shortcut halfway through the word.
func TestApproveAnswersOnlyWhileAQuestionIsOpenAndTheLineIsEmpty(t *testing.T) {
	a := New(Config{})
	a.key(mustKey(t, "y"))
	if got := a.ed.Text(); got != "y" {
		t.Errorf("with nothing pending, y left %q on the line, want %q", got, "y")
	}
	a.ed.KillLine()

	inboxOpen(t, a, "inbox-9", "approve bash for solo?")
	a.key(mustKey(t, "y"))
	if got := a.ed.Text(); got != "" {
		t.Errorf("y answered the question and still typed %q", got)
	}
	if in := a.st.Inboxes[0]; !in.Answered || in.Answer != "allow" {
		t.Errorf("inbox answered=%v %q, want true %q", in.Answered, in.Answer, "allow")
	}

	// A question open and text on the line: a letter again, so a typed answer survives.
	inboxOpen(t, a, "inbox-10", "approve rm for solo?")
	a.ed.Insert("no")
	a.key(mustKey(t, "n"))
	if got := a.ed.Text(); got != "non" {
		t.Errorf("n with text on the line left %q, want %q", got, "non")
	}
	if a.st.OpenInbox() == nil {
		t.Error("a letter typed into a reply answered the question")
	}
}

// The shipped keymap is written as the strings a user would type, so every one of them
// has to be a name the decoder actually produces, in the spelling Key.String returns:
// Lookup keys on that string, and a binding spelled any other way never fires.
func TestDefaultBindingsAreCanonicalAndReachable(t *testing.T) {
	km := DefaultKeymap() // panics on a bad default, which is a build bug, not an error
	for name, act := range DefaultBindings() {
		k, ok := term.ParseKey(name)
		if !ok {
			t.Errorf("%q is bound to %q and is not a key name", name, act)
			continue
		}
		if canon := k.String(); canon != name {
			t.Errorf("%q is spelled %q by the decoder, so Lookup never sees it", name, canon)
		}
		if got := km.Lookup(k); got != act {
			t.Errorf("%q resolves to %q, want %q", name, got, act)
		}
	}
	// Every declared action is reachable and documented: an action nothing can press is
	// a row in `keys` that lies, and a binding with no doc is a hole in that table.
	bound := make(map[Action]bool)
	for _, b := range km.Bindings() {
		bound[b.Action] = true
		if b.Doc == "" {
			t.Errorf("binding %q -> %q has no doc", b.Key, b.Action)
		}
	}
	for _, d := range ActionKeys {
		if !bound[d.Action] {
			t.Errorf("action %q is declared and no default key reaches it", d.Action)
		}
	}
}

// A config goes through the same door as the defaults: parsed by the decoder that will
// later produce the name, canonicalized, and checked against the declared actions.
func TestKeymapOverridesAndReportsEveryProblem(t *testing.T) {
	km, err := NewKeymap(map[string]Action{
		"ctrl+d":     ActionNone, // the only way to take a default away
		"ctrl+g":     ActionQuit,
		"alt+ctrl+x": ActionHome, // a spelling the decoder never produces
	})
	if err != nil {
		t.Fatalf("NewKeymap: %v", err)
	}
	if got := km.Lookup(mustKey(t, "ctrl+d")); got != ActionNone {
		t.Errorf("ctrl+d still does %q after being unbound", got)
	}
	if got := km.Lookup(mustKey(t, "ctrl+g")); got != ActionQuit {
		t.Errorf("ctrl+g does %q, want %q", got, ActionQuit)
	}
	if got := km.Lookup(mustKey(t, "ctrl+alt+x")); got != ActionHome {
		t.Errorf("ctrl+alt+x does %q, want %q: an override is canonicalized on the way in", got, ActionHome)
	}

	// A config with two mistakes costs one run to fix, not two.
	_, err = NewKeymap(map[string]Action{"Ctrl+A": ActionHome, "ctrl+g": Action("teleport")})
	if err == nil {
		t.Fatal("a config naming an unknown key and an undeclared action was accepted")
	}
	for _, want := range []string{`"Ctrl+A"`, "teleport"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error never mentions %s:\n%v", want, err)
		}
	}
}

// Moving the view. Everything below this line is about the half of the interface the user
// found missing — "no puedo hacer scroll hacia arriba, de la conversacion" — and about the
// other half they never saw at all, which is that the conversation has to survive the end
// of the session on whichever surface it was drawn.

// linesChanged is the recording the user actually plays, and it is named here for the one
// thing the first conversation cannot give: it folds to more rows than a screen holds, so
// there is a window to move. Scrolling a conversation that already fits is a no-op by
// design, and a suite that only ever saw that case would pass against a player which cannot
// scroll at all — which is exactly the bug these tests exist for.
const linesChanged = "../../testdata/scenarios/06-lines-changed.ndjson"

// conversationHead is the first row of that recording's transcript, the line the human
// opened with. It is the marker for "the top of the conversation is on screen", and it comes
// from the transcript rather than from the chrome on purpose: the end-of-scenario widget is
// drawn on every frame, scrolled or not, so it says nothing about where the window is.
const conversationHead = "runshow prints ttl:0"

// longSession is the recording with more than one turn of the reader's own in it, which is
// the one thing linesChanged cannot give: it holds a single prompt, and that prompt is the
// first thing in the transcript, so it sits on row zero where ctrl+home already goes. A jump
// between the reader's messages needs messages to jump between and a distance to cover.
const longSession = "../../testdata/scenarios/08-long-session.ndjson"

// scrollable parks a player on a full screen with the whole conversation already behind it.
//
// It deliberately does not use play. That one runs the log on a goroutine, so which rows are
// on screen when a key arrives depends on timing, and "the window moved up by one row" is
// not a claim anybody can make about a moving target. Here the log is folded first and every
// frame afterwards is drawn by the test, through the same draw() the loop calls. With no Run
// goroutine the App's own fields are also safe to read, which is what lets these tests assert
// that the view moved rather than that some bytes changed.
func scrollable(t *testing.T, mode ui.Mode) (*App, *probe) {
	t.Helper()
	return scrollableWheel(t, mode, 0)
}

// scrollableWheel is scrollable with a wheel setting on it. Zero asks for the shipped one,
// which is what every test that is not about the wheel wants: those assert against a.notch()
// rather than against a number, so the default can be re-tuned without editing them.
func scrollableWheel(t *testing.T, mode ui.Mode, wheel int) (*App, *probe) {
	t.Helper()
	return scrollableFile(t, linesChanged, mode, wheel)
}

// scrollableFile is the one place a folded player is built, so that a test which needs a
// different recording says which and inherits every check below rather than growing a second
// copy of them that drifts.
func scrollableFile(t *testing.T, path string, mode ui.Mode, wheel int) (*App, *probe) {
	t.Helper()
	return scrollableSize(t, path, mode, wheel, 80, 24)
}

// scrollableSize is that same player with the terminal's size spelled out, for the tests that need
// a screen wide enough to hold a column beside the transcript. Eighty columns is what every other
// test wants and is therefore the default above; the bar is drawn in a column resize only reserves
// past a hundred, so a pointer test on that screen would be pointing at nothing.
func scrollableSize(t *testing.T, path string, mode ui.Mode, wheel, width, height int) (*App, *probe) {
	t.Helper()
	sc, err := scenario.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	if errs := scenario.Validate(sc); len(errs) > 0 {
		t.Fatalf("%s does not validate: %v", path, errs)
	}
	p := &probe{}
	a := New(Config{
		Scenario:   sc,
		Out:        p,
		Emitter:    &ui.Emitter{Theme: ui.DefaultTheme(), Mode: mode, Profile: ui.ProfileMono},
		Width:      width,
		Height:     height,
		WheelLines: wheel,
	})
	// Every wheel report here is its own press of the wheel. A report landing within accelPeriod
	// of the last one is the same flick continuing and carries further than the one before it, and
	// two presses written two lines apart in Go land microseconds apart — so without this a test
	// asserting "a notch moved a notch" would be measuring a spin. A hand pressing the wheel twice
	// while it reads is what all of those tests are about, and a clock that steps past the window
	// on every reading is that hand. The one test that is about a spin installs its own clock over
	// this one, which is the whole reason Config.Now is a function.
	//
	// The clock New defaulted is checked first, because this line is the last chance anything has
	// to see it: nothing outside a test ever sets Config.Now, so a session's wheel is measured
	// against whatever New put there, and a nil is a panic on the first notch while a stopped clock
	// is a spin that never ends.
	if now := a.cfg.Now; now == nil || now().IsZero() {
		t.Fatal("New left Config.Now unset or stopped, and a real session has no other clock")
	}
	clock := time.Now()
	a.cfg.Now = func() time.Time {
		clock = clock.Add(2 * accelPeriod)
		return clock
	}
	// Fold walks the whole log into the state. Its own frame is a document — no height — and
	// it renders on a copy of the viewport, so the 24-row screen asked for above is still
	// there for the draw that follows.
	a.Fold()
	if err := a.draw(); err != nil {
		t.Fatalf("first frame: %v", err)
	}
	if a.rows <= 0 {
		t.Fatalf("the window is %d rows on a %d-row screen: the chrome left nothing to scroll", a.rows, a.vp.Height)
	}
	if a.top <= 0 {
		t.Fatalf("the whole conversation fits in a %d-row window, so nothing here proves anything", a.rows)
	}
	return a, p
}

// frame draws once and returns what that one frame put on the screen. probe is a cumulative
// stream and every other assertion in this file is a substring of the whole run, which is
// exactly the wrong question here: a row that has scrolled away was on screen ten frames ago
// and is still in the buffer. Resetting first is what makes the answer the screen rather
// than the history.
func (p *probe) frame(t *testing.T, a *App) string {
	t.Helper()
	return plain(p.frameBytes(t, a))
}

// frameBytes is that same one frame with its escapes still in it, for the assertions that are
// about what the terminal was told rather than about what it would show. A mode set is nothing
// but escapes, so plain would leave an empty string where the whole claim was.
func (p *probe) frameBytes(t *testing.T, a *App) string {
	t.Helper()
	p.mu.Lock()
	p.buf.Reset()
	p.mu.Unlock()
	if err := a.draw(); err != nil {
		t.Fatalf("draw: %v", err)
	}
	return p.text()
}

// The reported bug, in the shape it was reported: a conversation of 68 rows on a 24-row
// screen, and no way to reach the 44 that are above the window. The transcript is drawn on a
// surface with no scrollback of its own, so the only thing that can move is the player's own
// offset, and until these actions existed there was nothing to move it.
//
// Every key is pressed by name through the keymap, so the default bindings are covered too:
// a binding spelled any other way would never fire.
func TestScrollingReachesTheTopOfTheConversationAndComesBack(t *testing.T) {
	a, p := scrollable(t, ui.ModeAlt)
	tail := p.frame(t, a)
	if strings.Contains(tail, conversationHead) {
		t.Fatalf("the first row of the conversation is already on the tail frame, so reaching it proves nothing:\n%s", tail)
	}
	bottom, page := a.top, a.page()

	// Down, while the view is already following, is not a move: there is nothing below the
	// tail, and nudging the offset would ask for a frame identical to the one on screen.
	if a.key(mustKey(t, "wheeldown")) {
		t.Error("scrolling down on the tail asked for a frame")
	}
	// Up by one row, from where the view already is. A first press that jumped to the top of
	// the log would be a pager nobody could read with. alt+up is the one-row key: the wheel and
	// ctrl+up are both worth a notch, and this is what a reader reaches for when a notch lands
	// past the line they meant.
	if !a.key(mustKey(t, "alt+up")) {
		t.Fatal("alt+up on a conversation taller than the screen changed nothing")
	}
	if !a.scrolled || a.top != bottom-1 {
		t.Fatalf("one row up put the window at %d (scrolled=%v), want %d", a.top, a.scrolled, bottom-1)
	}
	if p.frame(t, a); a.top != bottom-1 {
		t.Fatalf("drawing moved the window to %d: the renderer clamped an offset that was legal", a.top)
	}

	// A page is the window less one row, so two consecutive screens share a line and nothing
	// is read past.
	if !a.key(mustKey(t, "pgup")) {
		t.Fatal("pgup changed nothing")
	}
	want := bottom - 1 - page
	if want < 0 {
		want = 0
	}
	if a.top != want {
		t.Fatalf("a page up from %d landed on %d, want %d (a %d-row window)", bottom-1, a.top, want, a.rows)
	}
	// All the way up. This is the row the user could not reach.
	if !a.key(mustKey(t, "ctrl+home")) {
		t.Fatal("ctrl+home changed nothing")
	}
	head := p.frame(t, a)
	if a.top != 0 {
		t.Fatalf("the top of the conversation came out at offset %d, want 0", a.top)
	}
	if !strings.Contains(head, conversationHead) {
		t.Fatalf("scrolled to the top and the conversation's first row is not on screen:\n%s", head)
	}
	// Nothing above row zero, so nothing up there asks for a frame either. A player that
	// redrew here would repaint the same screen for as long as a key was held down. Every key
	// that moves the view upwards is in the list, jumping included: there is no turn of the
	// reader's own above row zero any more than there is a row.
	for _, name := range []string{"ctrl+home", "ctrl+up", "alt+up", "shift+up", "pgup", "wheelup"} {
		if a.key(mustKey(t, name)) {
			t.Errorf("%s at the top of the conversation asked for a frame", name)
		}
	}

	// Back to the tail, and the tail is the screen it was before any of this happened: the
	// same state at the same size with the same offset draws the same rows.
	if !a.key(mustKey(t, "ctrl+end")) {
		t.Fatal("ctrl+end changed nothing")
	}
	if a.scrolled {
		t.Error("ctrl+end left the view scrolled, so every new row would arrive off screen")
	}
	if got := p.frame(t, a); got != tail {
		t.Errorf("the tail came back as a different screen:\n--- now ---\n%s\n--- before ---\n%s", got, tail)
	}
	if a.top != bottom {
		t.Errorf("following again leaves %d rows above the window, want %d", a.top, bottom)
	}
}

// A page key from every row of the log, because the window is not the same height at every row
// of it. The pinned header is drawn out of the transcript window, so a page measured where no
// header is up can land where one is, and the destination is then shorter than the distance just
// travelled: the rows in between were on neither screen, nobody read them, and nothing on the
// screen would say so. Skipping a row is the one thing a scroll must not do, and this is the only
// way it can still happen once the window can change size under the reader.
//
// So the claim is the one page keys have always made, stated against the two windows either side
// of a press rather than against the arithmetic that produced them: the screen the reader lands
// on reaches back to the screen they left, by at least the row they were told they would keep.
// A test written the other way — assert the step is the window less one — would restate the bug.
//
// The two counters are what stop this from passing by being too easy. A sweep where the window
// never changed height across a press would hold for the wrong reason, and a sweep whose closest
// two screens shared more than one row never reached a destination pinning its full two, which is
// the case with nothing left over.
//
// Inline is the other half of the same claim and the stricter one. There the header is left out by
// Place, so the window is one height at every row of the log and a page keeps exactly the row it
// promises and no more — which is what says the correction is paid where a header can appear and
// nowhere else. A surface that cannot hold the row must not be charged for it: three familiar rows
// per press is a third of a screen the reader is made to read twice, for nothing.
func TestAPageKeyCannotStepOverARowNobodyRead(t *testing.T) {
	for _, surface := range []struct {
		name string
		mode ui.Mode
		pins bool
	}{
		{"alt", ui.ModeAlt, true},
		{"inline", ui.ModeInline, false},
	} {
		t.Run(surface.name, func(t *testing.T) { pageKeepsARow(t, surface.mode, surface.pins) })
	}
}

func pageKeepsARow(t *testing.T, mode ui.Mode, pins bool) {
	a, _ := scrollableFile(t, longSession, mode, 0)
	tail := a.top

	presses, shrank, exact, narrow := 0, 0, 0, -1
	for start := tail; start >= 0; start-- {
		// Put the window on the row and let the frame say where that actually is: the offset comes
		// back clamped and the window comes back measured, which is the pair a page key works from.
		a.scrolled, a.top = start < tail, start
		if err := a.draw(); err != nil {
			t.Fatalf("draw at row %d: %v", start, err)
		}
		t0, k0 := a.top, a.rows
		if !a.key(mustKey(t, "pgup")) {
			if t0 == 0 {
				continue // nothing above row zero to page into
			}
			t.Fatalf("a page up from row %d asked for no frame", t0)
		}
		if err := a.draw(); err != nil {
			t.Fatalf("draw after a page up from row %d: %v", t0, err)
		}
		t1, k1 := a.top, a.rows
		if t1 >= t0 {
			t.Fatalf("a page up from row %d landed on %d, which is not up", t0, t1)
		}
		presses++
		if over := t1 + k1 - t0; over < 1 {
			t.Fatalf("a page up from row %d over a %d-row window landed on row %d with a %d-row window, which stops at row %d: the screen the reader left starts at %d, so the two have no row in common and %d rows were on neither", t0, k0, t1, k1, t1+k1-1, t0, t0-(t1+k1))
		} else if narrow < 0 || over < narrow {
			narrow = over
		}
		if k1 < k0 {
			shrank++
		}
		// A press that ran into the top of the log stopped short of a page, so it says nothing about
		// what a page is worth; every other one on a surface with no pin is a whole page, and the
		// window it lands in is the window it left.
		if !pins && t1 > 0 {
			exact++
			if over := t1 + k1 - t0; over != 1 || k1 != k0 {
				t.Fatalf("a page up from row %d over a %d-row window landed on row %d with a %d-row window, so the two screens share %d rows: nothing on this surface can shorten the window, and a page here is it less the one row the reader keeps", t0, k0, t1, k1, over)
			}
		}
	}
	if !pins {
		if exact == 0 {
			t.Fatalf("%d page keys and not one of them was a whole page: this recording is too short to say what a page is worth here", presses)
		}
		return
	}
	if shrank == 0 || narrow != 1 {
		t.Fatalf("%d page keys of %d landed on a shorter window and the two closest screens shared %d rows: this recording never pages onto a full pin, so nothing here is testing what a page costs", shrank, presses, narrow)
	}
}

// The wheel arrives as a mouse report and has to leave as a scroll, so this one starts from
// the bytes a terminal actually writes. Nothing else in the tree walks that whole path —
// decode, keymap, action, offset — and both encodings are here because a terminal answers our
// request for SGR coordinates with whichever one it happens to know.
func TestTheWheelScrollsThroughTheWholePath(t *testing.T) {
	for _, enc := range []struct{ name, up, down string }{
		{"SGR", "\x1b[<64;10;5M", "\x1b[<65;10;5M"},
		{"X10", "\x1b[M`!!", "\x1b[Ma!!"},
	} {
		a, p := scrollable(t, ui.ModeAlt)
		bottom, notch := a.top, a.notch()
		// Three notches. A spin is not a page and a notch is not a keypress: the size of one
		// is a.notch(), asserted against on its own below, and what makes a fast spin cheap
		// rather than far is the loop folding a burst of them into one frame.
		for i := 0; i < 3; i++ {
			evs, rest := term.Decode([]byte(enc.up))
			if len(evs) != 1 || len(rest) != 0 {
				t.Fatalf("%s: %q decoded to %d events with %q left over", enc.name, enc.up, len(evs), rest)
			}
			dirty, err := a.handle(evs[0])
			if err != nil {
				t.Fatalf("%s: handle: %v", enc.name, err)
			}
			if !dirty {
				t.Fatalf("%s: notch %d up moved nothing", enc.name, i+1)
			}
		}
		if p.frame(t, a); a.top != bottom-3*notch {
			t.Errorf("%s: three notches up moved the window from %d to %d, want %d (%d lines a notch)", enc.name, bottom, a.top, bottom-3*notch, notch)
		}
		evs, rest := term.Decode([]byte(enc.down))
		if len(evs) != 1 || len(rest) != 0 {
			t.Fatalf("%s: %q decoded to %d events with %q left over", enc.name, enc.down, len(evs), rest)
		}
		if _, err := a.handle(evs[0]); err != nil {
			t.Fatalf("%s: handle: %v", enc.name, err)
		}
		if a.top != bottom-2*notch {
			t.Errorf("%s: a notch back down left the window at %d, want %d", enc.name, a.top, bottom-2*notch)
		}
	}
}

// How far one notch goes, which is a complaint in its own right: once the conversation could
// be scrolled at all, the wheel turned out to take too long to climb it. A wheel does not
// report a notch per row travelled — it reports a handful per flick of a finger — so a notch
// worth one line puts a sixty-row conversation out of reach of a hand, however fast the spin
// and however cheaply the loop folds it.
//
// Both halves are asserted here because they are why there are two actions instead of one
// number. ctrl+up and ctrl+down are the same notch as the wheel now — the reader asked for the
// keyboard's step to be the wheel's step — so what keeps a single row reachable is the second
// action, on alt+up: coarsening scroll-up itself would leave no key that moves one line.
func TestOneWheelNotchIsWorthSeveralLinesAndSoIsTheCtrlArrow(t *testing.T) {
	a, _ := scrollable(t, ui.ModeAlt)
	bottom, notch := a.top, a.notch()
	if notch < 2 {
		t.Fatalf("a notch is %d line(s) with the shipped setting, which is where the complaint started", notch)
	}
	if !a.key(mustKey(t, "wheelup")) {
		t.Fatal("wheelup on a conversation taller than the screen changed nothing")
	}
	if a.top != bottom-notch {
		t.Fatalf("one notch up moved the window from %d to %d, want %d", bottom, a.top, bottom-notch)
	}
	if !a.key(mustKey(t, "ctrl+up")) {
		t.Fatal("ctrl+up changed nothing")
	}
	if a.top != bottom-2*notch {
		t.Fatalf("ctrl+up moved %d lines, want the wheel's %d: the arrow is the keyboard's spelling of a notch", bottom-notch-a.top, notch)
	}
	if !a.key(mustKey(t, "alt+up")) {
		t.Fatal("alt+up changed nothing")
	}
	if a.top != bottom-2*notch-1 {
		t.Fatalf("alt+up moved %d lines, want exactly 1: it is the key left for a line", bottom-2*notch-a.top)
	}
	// Down the same way, so the wheel is symmetric and a spin back is one spin and not four.
	if !a.key(mustKey(t, "wheeldown")) {
		t.Fatal("wheeldown while scrolled changed nothing")
	}
	if a.top != bottom-notch-1 {
		t.Fatalf("a notch back down left the window at %d, want %d", a.top, bottom-notch-1)
	}
}

// The amount is a setting, because how far a notch should go is a fact about a mouse and a
// hand and not one this program can know. What it can know is the two ends: never less than a
// line, and never more than a page.
//
// The ceiling is the one worth a test. A notch that outran the window would jump over text
// nobody had read, which is the one thing scrolling must not do, and it would do it silently
// on whatever terminal happened to be smaller than the number in the config.
func TestTheWheelsAmountIsASettingHeldBetweenALineAndAPage(t *testing.T) {
	// The shipped amount, named. Every case below is symbolic on purpose — they compare against
	// a.page() and defaultWheelLines so the default can be re-tuned without editing them — and
	// that is exactly why the number itself needs a line of its own: it stopped being a tuning
	// the day a reader asked for it out loud, in lines, after spinning through a conversation
	// two at a time. Three is per notch, whole: the emitter claims mouse tracking, so a flick of
	// a finger arrives once as a wheel report rather than as however many arrow presses the OS
	// thinks a notch is worth, and there is nothing left multiplying this number behind our back.
	if defaultWheelLines != 3 {
		t.Errorf("a notch is %d lines, want 3: the reader asked for three and a suite that only checks the constant against itself would not notice", defaultWheelLines)
	}
	for _, c := range []struct {
		name  string
		set   int
		lines func(a *App) int
	}{
		{"as given", 9, func(*App) int { return 9 }},
		{"1 is the old wheel back", 1, func(*App) int { return 1 }},
		{"a nonsense setting is a page", 500, func(a *App) int { return a.page() }},
		{"negative is the default", -3, func(*App) int { return defaultWheelLines }},
	} {
		a, _ := scrollableWheel(t, ui.ModeAlt, c.set)
		bottom, want := a.top, c.lines(a)
		if want < 1 || want >= bottom {
			t.Fatalf("%s: %d lines is not a move this %d-row conversation can prove", c.name, want, bottom)
		}
		if !a.key(mustKey(t, "wheelup")) {
			t.Fatalf("%s: wheelup changed nothing", c.name)
		}
		if got := bottom - a.top; got != want {
			t.Errorf("%s: -scroll %d moved %d lines a notch, want %d", c.name, c.set, got, want)
		}
	}

	// And the setting reaches the wheel through the action, not through the key. A config
	// that puts wheelup back on scroll-up gets one line out of it whatever -scroll says,
	// which is the property that keeps the keymap a map from keys to meanings.
	a, _ := scrollableWheel(t, ui.ModeAlt, 9)
	km, err := NewKeymap(map[string]Action{"wheelup": ActionScrollUp})
	if err != nil {
		t.Fatalf("NewKeymap: %v", err)
	}
	a.km = km
	bottom := a.top
	if !a.key(mustKey(t, "wheelup")) {
		t.Fatal("a rebound wheelup changed nothing")
	}
	if got := bottom - a.top; got != 1 {
		t.Errorf("wheelup rebound to scroll-up moved %d lines, want 1: the amount belongs to the action", got)
	}
}

// And how far a notch goes depends on how fast the wheel is turning, which is the second half of
// the same complaint: a notch is three rows, a spin is a handful of notches, and twenty-one rows
// is not the length of an answer. A hand that wants to cross a long one either spins for thirty
// notches or gives up on the wheel.
//
// Nothing in a wheel report says whether it belongs to a finger resting on the wheel or to a
// flick, and the report is identical either way. The only difference is the gap before it: a hand
// stepping through rows sends three or four reports a second, a flick sends five to ten times
// that because the wheel is freewheeling and the finger has already left it. So the wheel is
// measured rather than counted, and the whole of what it remembers is when the last report came
// and which way it went.
//
// This is the test that holds the ramp to the two things it must not turn into. It must not
// outrun the reader's eye, which is why every step is still capped at a page. And it must not
// keep a flick's momentum after the hand has gone, which is why the silence resets it — a wheel
// that stayed fast would answer the reader's careful last notch by throwing the page away.
//
// The clock is the config's, frozen and stepped by hand. A test that slept for its gaps would be
// asserting 150 ms of wall time per report and would fail on a loaded machine; a test that spun
// the wheel as fast as Go can call a method would be measuring the machine.
func TestTheWheelAcceleratesOnlyWhileItIsSpinning(t *testing.T) {
	a, _ := scrollableFile(t, longSession, ui.ModeAlt, 0)
	clock := time.Now()
	a.cfg.Now = func() time.Time { return clock }
	notch, page := a.notch(), a.page()
	if notch < 2 || notch >= page {
		t.Fatalf("a notch is %d row(s) of a %d-row page: there is no room between them for a ramp to be seen", notch, page)
	}

	// Parked away from both ends, because a step is only measured where it was the ramp that moved
	// the view and not the clamp that stopped it. The walk below reaches nine notches above this
	// spot and never goes below it.
	if !a.scroll(-a.top / 2) {
		t.Fatalf("the view will not leave row %d", a.top)
	}
	if f := screen(t, a); f.Scroll.Above < 10*notch {
		t.Fatalf("parked with %d rows above on a %d-row notch: this recording is too short to spin through", f.Scroll.Above, notch)
	}

	// step waits d since the last report, presses one key, and answers how far the view went —
	// upward positive, so every claim below reads in the direction the reader is looking.
	step := func(name string, d time.Duration) int {
		t.Helper()
		clock = clock.Add(d)
		at := a.top
		if !a.key(mustKey(t, name)) {
			t.Fatalf("%s asked for no frame", name)
		}
		return at - a.top
	}
	for _, c := range []struct {
		what string
		name string
		gap  time.Duration
		want int
	}{
		{"the first report of a spin is a plain notch", "wheelup", 10 * time.Second, notch},
		{"a report at the far edge of the window is still the same spin", "wheelup", accelPeriod, 2 * notch},
		{"and the one after it carries further again", "wheelup", accelPeriod / 3, 3 * notch},
		{"one millisecond past the window is a hand placed on the wheel again", "wheelup", accelPeriod + time.Millisecond, notch},
		{"shift rides in the report's own byte, so it spins with the rest", "shift+wheelup", time.Millisecond, 2 * notch},
		{"a reversal starts over: the row being hunted for is one of the ones just passed", "wheeldown", time.Millisecond, -notch},
		{"and then ramps the other way like any other spin", "wheeldown", time.Millisecond, -2 * notch},
		{"ctrl+up is a notch on the keyboard, and a held key must not accelerate by leaning", "ctrl+up", time.Millisecond, notch},
		{"however fast it is pressed", "ctrl+up", time.Millisecond, notch},
		{"and the keyboard is not in the wheel's spin: it neither feeds it nor ends it", "wheeldown", time.Millisecond, -3 * notch},
	} {
		if got := step(c.name, c.gap); got != c.want {
			t.Errorf("%s: %s at a gap of %v moved %d rows, want %d", c.what, c.name, c.gap, got, c.want)
		}
	}

	// The ceiling, asserted on fast itself rather than through the view: through the view the end
	// of the log would be what stopped the ramp and the claim would be about how long the
	// recording is. A step longer than a page steps over rows nobody read, which is the one thing
	// a scroll must not do however fast the wheel is going — so a spin converges on the page key
	// instead of outrunning it, and what the ramp buys is the ground covered on the way there.
	k := mustKey(t, "wheelup")
	a.spin, a.spinAt, a.spinDir = 0, time.Time{}, 0
	last, hit := 0, false
	for i := 1; i <= 4*page; i++ {
		clock = clock.Add(time.Millisecond)
		got := a.fast(k, -1)
		if got > page {
			t.Fatalf("report %d of one spin moves %d rows on a %d-row page: that is a jump over rows nobody read", i, got, page)
		}
		if got < last {
			t.Fatalf("report %d of one spin moves %d rows after the one before it moved %d: a spin does not slow down while it is spinning", i, got, last)
		}
		last, hit = got, hit || got == page
	}
	if !hit {
		t.Errorf("%d reports of one spin never reached the %d-row page, stopping at %d: the ramp goes nowhere", 4*page, page, last)
	}

	// And the one setting that is a finger rather than a wheel. A terminal reporting once per row
	// a swipe has travelled — Termux, where -scroll defaults to 1 — is already sending the hand's
	// own measurement of itself, and multiplying it would take the text off the thumb dragging it.
	// The same line answers a reader who asked for the finest wheel there is: they asked for one
	// row, and giving them two on the second report would be answering some other question.
	//
	// Its clock is frozen over the stepping one for the same reason as above: these five reports
	// are one flick, and against the default clock they would be five separate presses, which is
	// the case this arm is not about.
	fine, _ := scrollableFile(t, longSession, ui.ModeAlt, 1)
	fine.cfg.Now = func() time.Time { return clock }
	if n := fine.notch(); n != 1 {
		t.Fatalf("-scroll 1 makes a notch %d rows, so nothing below is about the finest wheel", n)
	}
	const flick = 5
	if fine.top < flick {
		t.Fatalf("the view is %d rows from the top of the log, so %d reports cannot be told from the clamp", fine.top, flick)
	}
	for i := 1; i <= flick; i++ {
		clock = clock.Add(time.Millisecond)
		at := fine.top
		if !fine.key(k) {
			t.Fatalf("report %d with -scroll 1 asked for no frame", i)
		}
		if got := at - fine.top; got != 1 {
			t.Errorf("report %d of a spin with -scroll 1 moved %d rows, want 1: a swipe reporting once per row is already the hand's own distance", i, got)
		}
	}
}

// Who is allowed to move the view. A reader who has scrolled up is reading, and the agent
// saying one more thing is not a reason to yank them back to the bottom — the offset is
// measured from the top of the transcript, so a row arriving at the end moves nothing that is
// on screen. A line the human sends is the opposite: they asked to see what happens next, so
// it returns to the tail. follow() is called from exactly one place for exactly this reason.
func TestARecordedEventLeavesAScrolledReaderWhereTheyAre(t *testing.T) {
	sc, err := scenario.Load(linesChanged)
	if err != nil {
		t.Fatalf("load %s: %v", linesChanged, err)
	}
	// The last step that says anything. Everything before it is played first, so the single
	// event this test is about is the one that arrives while somebody is reading.
	last := -1
	for i, s := range sc.Steps {
		if !s.IsBarrier() {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("%s has no events in it", linesChanged)
	}
	p := &probe{}
	a := New(Config{
		Scenario: sc,
		Out:      p,
		Emitter:  &ui.Emitter{Theme: ui.DefaultTheme(), Mode: ui.ModeAlt, Profile: ui.ProfileMono},
		Width:    80,
		Height:   24,
	})
	for a.next < last {
		a.advance()
	}
	if err := a.draw(); err != nil {
		t.Fatalf("first frame: %v", err)
	}
	if a.top <= 0 {
		t.Fatalf("the conversation fits in a %d-row window before its last event, so nothing here proves anything", a.rows)
	}
	if !a.key(mustKey(t, "ctrl+home")) {
		t.Fatal("ctrl+home changed nothing")
	}
	if before := p.frame(t, a); !strings.Contains(before, conversationHead) {
		t.Fatalf("scrolled to the top and the conversation's first row is not on screen:\n%s", before)
	}
	// The recording says its last thing.
	if !a.advance() {
		t.Fatal("the last recorded step asked for no frame, so it changed nothing and this test proves nothing")
	}
	after := p.frame(t, a)
	if !a.scrolled || a.top != 0 {
		t.Errorf("a recorded event moved the view to %d (scrolled=%v); a reader gets to keep reading", a.top, a.scrolled)
	}
	if !strings.Contains(after, conversationHead) {
		t.Errorf("a recorded event scrolled the conversation's first row off a screen somebody was reading:\n%s", after)
	}

	// Now the human. A submitted line goes through the same door a recorded one does, and the
	// only difference is that this one is theirs.
	a.ed.Insert("apply it")
	if !a.key(mustKey(t, "enter")) {
		t.Fatal("submitting a line changed nothing")
	}
	if a.scrolled {
		t.Error("a line sent from the top of the conversation left the view there, so the reply would land off screen")
	}
	tail := p.frame(t, a)
	if a.top <= 0 {
		t.Errorf("following again leaves %d rows above the window, want the tail of a conversation that does not fit", a.top)
	}
	if strings.Contains(tail, conversationHead) {
		t.Errorf("the view is following and the conversation's first row is still on screen:\n%s", tail)
	}
}

// The end of a session is the only moment the terminal is handed anything to keep, and on the
// alternate screen it used to be handed nothing at all: Emit dispatched to an alt-only path,
// the document branch was unreachable from it, and a run played there ended by throwing the
// whole conversation away. Both surfaces settle now, and what they hand over is the document —
// every row of it, not the screenful that happened to be visible when the user quit.
func TestSettleHandsOverTheWholeConversationOnEitherSurface(t *testing.T) {
	for _, s := range []struct {
		name string
		mode ui.Mode
	}{{"inline", ui.ModeInline}, {"alt", ui.ModeAlt}} {
		a, p := scrollable(t, s.mode)
		if screen := p.frame(t, a); strings.Contains(screen, conversationHead) {
			t.Fatalf("%s: the whole conversation is on screen, so handing it over proves nothing", s.name)
		}
		p.mu.Lock()
		p.buf.Reset()
		p.mu.Unlock()
		if err := a.settle(); err != nil {
			t.Fatalf("%s: settle: %v", s.name, err)
		}
		// CRLF because a terminal in raw mode needs the carriage return; the document is
		// written with LF, and that is the only difference between them.
		got := strings.ReplaceAll(plain(p.text()), "\r\n", "\n")
		if !strings.Contains(got, conversationHead) {
			t.Errorf("%s: the session ended and the conversation's first row was never printed:\n%s", s.name, got)
		}
		if !strings.Contains(got, "end of scenario") {
			t.Errorf("%s: the session ended and the conversation's last row was never printed:\n%s", s.name, got)
		}
		// And it is the document, row for row: settle renders the same state with no height,
		// which is what a fold and a pipe and a golden file all get.
		vp := a.vp
		vp.Height = 0
		if doc := a.r.Render(a.st, nil, vp).Plain(); got != doc {
			t.Errorf("%s: what settle handed over is not the document — %s", s.name, firstDiff(doc, got))
		}
	}
}

// The mouse, which is the other half of the same report: the conversation scrolled at last, and the
// keys it scrolled with were the wrong ones. The bare arrows were the wheel, on the argument that the
// alternate buffer turns a notch into arrow bytes (CSI ? 1007 h) and that no honest timing tells a
// notch from a finger. The reader rejected that and named the counter-examples — Claude Code, every
// TUI editor, ishakat — and they were right: 1007's translation only applies while no program has
// claimed the mouse, so the arrows are the app's to bind either way. What changed after that is which
// way round it is paid for. Claiming the mouse also takes drag-to-select away, and the reader asked
// for the plain drag back, so the default releases the mouse and Emitter.Enter switches 1007 off
// instead: a notch arrives as nothing at all rather than as an arrow nobody pressed, and -mouse is
// the flag that turns it back into a report. Both halves are asserted in internal/ui.
//
// Which puts the three meanings on three keys instead of on one, and makes the keyboard the whole of
// scrolling in a default run. The wheel is the wheel wherever there is one, the bare arrows are the
// input's history, shift walks the reader's own turns, ctrl is the wheel's own notch spelled on the
// keyboard and alt is the single row. This is the test that holds the player to that whole layout —
// and to the thing that fell out of it, that no frame carries a mouse mode, because the mouse is
// settled once, when the surface is claimed, and never reconsidered by a repaint.
func TestTheArrowsAreTheHistoryAndTheWheelIsTheWheel(t *testing.T) {
	a, p := scrollable(t, ui.ModeAlt)
	bottom, notch := a.top, a.notch()

	// The wheel, in both spellings a terminal sends it, and under -mouse it is the only way these
	// two names arrive at all. They stay bound unconditionally anyway: a binding for a report
	// nobody sends costs one map entry, and a keymap that forked on a flag would cost every reader
	// of it a question. Shift rides in the report's own button byte, and a reader holding it to
	// scroll is not asking for something different, so both spellings land on the same action
	// rather than one of them falling through to nothing.
	for _, name := range []string{"wheelup", "shift+wheelup"} {
		at := a.top
		if !a.key(mustKey(t, name)) {
			t.Fatalf("%s on a conversation taller than the screen moved nothing", name)
		}
		if a.top != at-notch {
			t.Fatalf("%s moved the window from %d to %d, want %d: a notch is a notch, modifier or not", name, at, a.top, at-notch)
		}
	}
	for _, name := range []string{"shift+wheeldown", "wheeldown"} {
		at := a.top
		if !a.key(mustKey(t, name)) {
			t.Fatalf("%s while scrolled moved nothing", name)
		}
		if a.top != at+notch {
			t.Fatalf("%s moved the window from %d to %d, want %d", name, at, a.top, at+notch)
		}
	}
	if a.top != bottom {
		t.Fatalf("two notches up and two back down left the window at %d, want %d, at the tail", a.top, bottom)
	}
	if txt := a.ed.Text(); txt != "" {
		t.Fatalf("the wheel put %q on the input line", txt)
	}

	// History, on the four keys it lives on, and the two bare arrows among them are the whole point
	// of the change: the reader asked to walk their own lines the way a shell does, and that is where
	// a hand reaches first. The lines are recorded through Input.Submit rather than by pressing
	// enter: the player's own submit puts a prompt into the log and waits on the loop, and there is
	// no loop here — the whole point of scrollable is that the test draws every frame itself, which
	// is what makes "the window did not move" a claim at all.
	a.ed.Insert("first")
	a.ed.Submit()
	a.ed.Insert("second")
	a.ed.Submit()
	at := a.top
	for _, c := range []struct{ key, want string }{
		{"up", "second"},
		{"up", "first"},
		{"down", "second"},
		// And readline's own names for the same two steps, still bound beside them because a habit
		// is worth a map entry. It ends on "second" so the two modified pairs below have a line to
		// leave alone; and it never presses past "first", because the far end of the history is a
		// separate claim from this one.
		{"ctrl+p", "first"},
		{"ctrl+n", "second"},
	} {
		if !a.key(mustKey(t, c.key)) {
			t.Fatalf("%s asked for no frame", c.key)
		}
		if got := a.ed.Text(); got != c.want {
			t.Fatalf("%s recalled %q, want %q: the arrows are the history and the wheel is elsewhere", c.key, got, c.want)
		}
		if a.top != at {
			t.Fatalf("%s moved the window to %d, want %d: walking the history is not scrolling", c.key, a.top, at)
		}
	}

	// The three modified pairs, which is where moving the view went. ctrl is the wheel's notch on the
	// keyboard, alt is the single row — the key to reach for when a notch lands past what you meant —
	// and shift is one of the reader's own turns, walked landing by landing in
	// TestJumpingWalksTheReadersOwnTurns. None of them may touch the line the history walk left behind.
	line := a.ed.Text()
	if !a.key(mustKey(t, "ctrl+up")) {
		t.Fatal("ctrl+up moved nothing, and it is a notch")
	}
	if a.top != at-notch {
		t.Fatalf("ctrl+up moved the window from %d to %d, want %d: the same notch the wheel is worth", at, a.top, at-notch)
	}
	if !a.key(mustKey(t, "alt+up")) {
		t.Fatal("alt+up moved nothing, and it is the one-row key")
	}
	if a.top != at-notch-1 {
		t.Fatalf("alt+up moved the window from %d to %d, want %d", at-notch, a.top, at-notch-1)
	}
	if !a.key(mustKey(t, "shift+up")) {
		t.Fatal("shift+up moved nothing, with a turn of the reader's own above the window")
	}
	if rows := a.r.PromptRows(a.st, a.vp); !slices.Contains(rows, a.top) {
		t.Fatalf("shift+up left the window at row %d, which is not one of the reader's turns %v", a.top, rows)
	}
	if got := a.ed.Text(); got != line {
		t.Fatalf("a modified arrow rewrote the input line as %q, want %q: only the bare pair is the history", got, line)
	}

	// The toggle is gone, and gone means unbound rather than quietly inert: neither key it had
	// reaches an action, so a hand that still remembers alt+m types an m instead of asking for a
	// mouse mode nothing in the interface mentions any more. There is nothing left to toggle — who
	// has the mouse is settled by -mouse on the way in and handed back on the way out.
	for _, name := range []string{"alt+m", "f2"} {
		if a.key(mustKey(t, name)) {
			t.Fatalf("%s still claims a key: the toggle was removed, not hidden", name)
		}
	}

	// No frame carries a mouse mode, on either surface. The modes leave exactly once — in
	// Emitter.Enter, which no draw calls — and that is what makes who has the mouse a thing said
	// once in the help text rather than a thing a repaint keeps re-deciding. A frame that set
	// tracking again would also reset it on the next Exit path that runs, and a shell whose clicks
	// are swallowed is the worst thing this can hand back. 1007 is in the list for the same reason
	// from the other side: a frame that switched it off would leave the next program without a wheel.
	b, q := scrollable(t, ui.ModeInline)
	for _, c := range []struct {
		name string
		a    *App
		p    *probe
	}{
		{"alt", a, p},
		{"inline", b, q},
	} {
		out := c.p.frameBytes(t, c.a)
		for _, mode := range []string{"?1000", "?1002", "?1003", "?1006", "?1007"} {
			if strings.Contains(out, mode) {
				t.Fatalf("%s mode put %s on a frame: the mouse is settled when the surface is claimed, not once a repaint", c.name, mode)
			}
		}
		if got := plain(out); strings.Contains(got, "drag") {
			t.Fatalf("%s mode offers a mouse no key can hand back:\n%s", c.name, got)
		}
	}

	// And the arrows are the history only because the shipped keymap says so, which is the property
	// worth more than the layout itself: whoever preferred the arrangement this replaced can have it
	// back out of a config file, and no other line of the player is consulted about it. An override
	// replaces a meaning rather than adding to one, so the rebound key scrolls and recalls nothing.
	old, _ := scrollable(t, ui.ModeAlt)
	km, err := NewKeymap(map[string]Action{"up": ActionScrollUpFast, "down": ActionScrollDownFast})
	if err != nil {
		t.Fatalf("NewKeymap: %v", err)
	}
	old.km = km
	tail, step := old.top, old.notch()
	if !old.key(mustKey(t, "up")) {
		t.Fatal("the rebound up did nothing")
	}
	if old.top != tail-step {
		t.Fatalf("up rebound to scroll-up-fast moved the window from %d to %d, want %d", tail, old.top, tail-step)
	}
	if got := old.ed.Text(); got != "" {
		t.Fatalf("the rebound up recalled %q as well: an override replaces the meaning of a key, it does not add to it", got)
	}
	if !old.key(mustKey(t, "down")) {
		t.Fatal("the rebound down did nothing")
	}
	if old.top != tail {
		t.Fatalf("the rebound down left the window at %d, want %d: the amount belongs to the action, and so does the meaning", old.top, tail)
	}
}

// head is as much of one of the reader's turns as certainly fits on its first drawn row:
// enough of it to tell the turns of a long session apart, and short enough that no wrap can
// cut it in half. Runes rather than bytes, because a prompt is prose and a cut through a
// multi-byte rune leaves a string that matches nothing.
func head(s string) string {
	r := []rune(s)
	if len(r) > 24 {
		r = r[:24]
	}
	return string(r)
}

// The third meaning of the same two arrows, and the one a long conversation is actually
// searched by: bare is the input's history, ctrl is a single row, shift is a whole turn of the
// reader's own. Nobody scrolls back looking for a tool call.
//
// It is on shift and not on ctrl+shift because ctrl+shift+up never arrived for the reader who
// asked for this: that combination is claimed by the terminal or the desktop on enough setups
// to be worth nothing, whereas shift+up is CSI 1;2A and reaches the decoder.
//
// A landing is asserted against the reader's turns and never against a row number typed into
// this file. PromptRows is asked where the turns are, at the width the test is drawing, and
// that answer is worth nothing at any other width — the same conversation re-wrapped is a
// different set of rows, which is why it is measured here instead of remembered.
//
// The rows are read out of the document rather than out of the frame. Plain writes Committed
// first, and in a document the whole transcript is committed, so doc[i] is transcript row i
// however much chrome the top slot grows later — and the next widget on the plan goes exactly
// there.
func TestJumpingWalksTheReadersOwnTurns(t *testing.T) {
	a, _ := scrollableFile(t, longSession, ui.ModeAlt, 0)

	// The turns and their text, in one order: the rows from the renderer, the text from the
	// state, filtered the way PromptRows filters. The fatal is the assumption written down —
	// if the two ever disagree, every landing below is being named after the wrong turn.
	rows := a.r.PromptRows(a.st, a.vp)
	var texts []string
	for _, it := range a.st.Items {
		if it.Kind == state.KindPrompt {
			texts = append(texts, it.Text)
		}
	}
	if len(rows) != len(texts) || len(rows) < 3 {
		t.Fatalf("%s draws %d of the reader's turns and holds %d: this test needs several of them to walk between", longSession, len(rows), len(texts))
	}
	bottom := a.top
	if last := rows[len(rows)-1]; last >= bottom {
		t.Fatalf("the last turn is at row %d and the tail window opens at %d, so it is on screen already: a walk down would clamp instead of landing", last, bottom)
	}
	// The document, measured once: the walk moves the window, and moving the window does not
	// change which row a turn is on.
	vp := a.vp
	vp.Height = 0
	doc := strings.Split(a.r.Render(a.st, nil, vp).Plain(), "\n")

	// Up, one turn per press, from a reader who was following the tail. Every landing is exact
	// and none of them can be the clamp's work: a target is strictly above the offset the window
	// already has, and the tail offset is the largest one there is.
	var up []int
	for {
		at := a.top
		if !a.key(mustKey(t, "shift+up")) {
			// The top of the walk, and that it is the top is the claim: no turn above, no frame
			// asked for, and the window left exactly where it stood.
			if a.top != at {
				t.Fatalf("shift+up asked for no frame and moved the window from %d to %d anyway", at, a.top)
			}
			break
		}
		if a.top >= at {
			t.Fatalf("shift+up left the window at %d, from %d: a jump up goes up", a.top, at)
		}
		if !a.scrolled {
			t.Fatal("shift+up moved the window and left the player following the tail, where the next event will yank it back")
		}
		i := slices.Index(rows, a.top)
		if i < 0 {
			t.Fatalf("shift+up landed the window on row %d, which is not one of the reader's turns %v", a.top, rows)
		}
		if want := head(texts[i]); !strings.Contains(doc[a.top], want) {
			t.Fatalf("row %d is %q, and turn %d begins %q: the landing row is the turn's own first row", a.top, doc[a.top], i, want)
		}
		if a.top > 0 && strings.TrimSpace(doc[a.top-1]) != "" {
			t.Fatalf("row %d, just above the landing, is %q and should be blank: the rule between two turns belongs above the turn, not on top of the window", a.top-1, doc[a.top-1])
		}
		if up = append(up, a.top); len(up) > len(rows) {
			t.Fatalf("shift+up has landed %d times in a conversation with %d turns in it", len(up), len(rows))
		}
	}
	if len(up) == 0 {
		t.Fatalf("shift+up from the tail moved nothing, with %d turns above the window", len(rows))
	}
	if got := up[len(up)-1]; got != rows[0] {
		t.Fatalf("the walk up stopped at row %d, want %d: the first thing the reader said is the last landmark above them", got, rows[0])
	}

	// And down the same steps in reverse, which is a stronger claim than "it went down": every
	// row on the way back is a row the walk up reached, so the two directions have to agree
	// landing for landing. Nothing is clamped here either — each of those rows is above the tail
	// offset, so coming back down to one lands on it exactly.
	for i := len(up) - 2; i >= 0; i-- {
		at := a.top
		if !a.key(mustKey(t, "shift+down")) {
			t.Fatalf("shift+down asked for no frame at row %d, with the turn at %d still below it", at, up[i])
		}
		if a.top != up[i] {
			t.Fatalf("shift+down from row %d landed on %d, want %d: the way down is the way up in reverse", at, a.top, up[i])
		}
	}
	// The far end, which is the reader's last turn and not the tail: the turns are the landmarks
	// and there is no turn below this one, so the key says so rather than drifting to the bottom.
	// ctrl+end is what going to the bottom is for.
	at := a.top
	if a.key(mustKey(t, "shift+down")) {
		t.Fatalf("shift+down below the reader's last turn moved the window from %d to %d", at, a.top)
	}
	if a.top != at {
		t.Fatalf("shift+down asked for no frame and moved the window from %d to %d anyway", at, a.top)
	}
	if at != rows[len(rows)-1] {
		t.Fatalf("the walk down ended at row %d, want %d: the last thing the reader said is the last landmark below them", at, rows[len(rows)-1])
	}
}

// header is the pinned row this frame installed, if it installed one. It reads the widget list
// and not the screen, because those are two different claims and the difference between them is
// the whole of the inline case: what the app decided to draw is this list, and where that
// decision ended up is the frame. A row that is installed and then left out by the surface would
// be indistinguishable, from the screen alone, from a row nobody asked for.
func header(t *testing.T, a *App) (ui.HeaderWidget, bool) {
	t.Helper()
	var out ui.HeaderWidget
	found := false
	for _, wd := range a.chrome() {
		h, ok := wd.(ui.HeaderWidget)
		if !ok {
			continue
		}
		if found {
			t.Fatalf("the frame installed two pinned rows: %q and %q", out.Text, h.Text)
		}
		out, found = h, true
	}
	return out, found
}

// screen draws the frame the loop would have drawn and hands back the Frame, which is where the
// rows are: on the alternate buffer every row is placed with an absolute CUP and no newline
// between them, so the emitted bytes are one long line and "the top row of the screen" is not a
// question they can answer.
//
// Rendering a second time is free of consequence — Render is pure, and the widget list draw
// installed is still on the renderer — and the offsets are re-stated exactly as draw states them,
// so this is the frame that was emitted rather than a corrected one.
func screen(t *testing.T, a *App) ui.Frame {
	t.Helper()
	if err := a.draw(); err != nil {
		t.Fatalf("draw: %v", err)
	}
	a.vp.Scrolled, a.vp.ScrollTop = a.scrolled, a.top
	return a.r.Render(a.st, a.ed, a.vp)
}

// onScreen counts the rows of a frame that read exactly like l. One is a row the reader can see;
// two is the pin and the transcript printing the same words a line apart, which is the fault the
// count of lost rows exists to make impossible.
func onScreen(f ui.Frame, l ui.Line) int {
	want, n := strings.TrimRight(l.Text(), " "), 0
	for _, got := range f.Live {
		if strings.TrimRight(got.Text(), " ") == want {
			n++
		}
	}
	return n
}

// The reader's own message, held above the transcript while they are scrolled away from it. The
// rule as it was given is half of what this pins — only while scrolled, never while the recording
// is playing, so the screen stays clean when nobody is looking for a landmark — and the other half
// is that the row is paid for, in the only currency the window has.
//
// Every claim is made against the reader's turns as PromptRows reports them at this width and
// against the document those rows index into, and never against a row number written into this
// file: the same conversation at another width is a different set of rows.
func TestThePinnedHeaderNamesTheTurnTheReaderIsInside(t *testing.T) {
	a, _ := scrollableFile(t, longSession, ui.ModeAlt, 0)

	turns := a.r.PromptRows(a.st, a.vp)
	var texts []string
	for _, it := range a.st.Items {
		if it.Kind == state.KindPrompt {
			texts = append(texts, it.Text)
		}
	}
	if len(turns) != len(texts) || len(turns) < 3 {
		t.Fatalf("%s draws %d of the reader's turns and holds %d: this test needs several of them", longSession, len(turns), len(texts))
	}
	// The document, and out of it how tall each turn stands: a block is a run of rows with
	// something on them, which is what the blank between two items makes true. That height is the
	// ceiling on how much of a turn can have scrolled off — above it the lost rows belong to the
	// answer and not to the question.
	vp := a.vp
	vp.Height = 0
	doc := strings.Split(a.r.Render(a.st, nil, vp).Plain(), "\n")
	tall := make([]int, len(turns))
	for i, at := range turns {
		for at+tall[i] < len(doc) && strings.TrimSpace(doc[at+tall[i]]) != "" {
			tall[i]++
		}
	}

	// The tail, where the reader is following the conversation and their last message is a few rows
	// up on the screen already: nothing to pin, and the row it would cost goes to the transcript.
	if h, ok := header(t, a); ok {
		t.Fatalf("a reader following the tail was handed a pinned %q", h.Text)
	}
	full := a.rows

	// Then every offset from the tail to the top of the log, one row at a time. The rule is about
	// where the top edge of the window falls, and there is no substitute for standing on each row
	// of each turn in turn: the interesting positions are the two either side of a turn's first
	// row, and which rows those are is a thing this recording decides, not this test.
	drew := map[int]int{}
	for a.scroll(-1) {
		f := screen(t, a)
		if !a.scrolled {
			t.Fatalf("the window at row %d reports that it is following the tail", a.top)
		}
		i := -1
		for j, at := range turns {
			if at <= a.top {
				i = j
			}
		}
		h, ok := header(t, a)
		if i < 0 {
			if ok {
				t.Fatalf("row %d is above the reader's first turn at %d, and %q was pinned", a.top, turns[0], head(h.Text))
			}
			continue
		}
		hidden := min(a.top-turns[i], tall[i])
		// A window whose first row is the first row of a turn has that turn's question on screen,
		// which is where a jump lands: the landmark is already there to be read, so pinning a copy
		// of it above itself would be the one thing this row must never do.
		if hidden == 0 {
			if ok {
				t.Fatalf("row %d is the first row of turn %d and %d rows of it were pinned over the top of it", a.top, i, h.Rows)
			}
			continue
		}
		if !ok {
			t.Fatalf("row %d is %d rows into turn %d (%q) and nothing was pinned", a.top, hidden, i, head(texts[i]))
		}
		if h.Text != texts[i] || h.Rows != hidden {
			t.Fatalf("row %d pinned %d rows of %q, want %d of %q", a.top, h.Rows, head(h.Text), hidden, head(texts[i]))
		}
		drawn := h.Render(a.vp.Width, 0, a.r.Glyphs)
		if len(drawn) == 0 || len(drawn) > hidden {
			t.Fatalf("row %d: %d rows of turn %d have scrolled off and the pin drew %d of them", a.top, hidden, i, len(drawn))
		}
		// Where the row ended up, which is the top of the screen: above the transcript, above the
		// input, above everything, because a landmark below the rows it names is not one.
		for j, l := range drawn {
			if got, want := f.Live[j].Text(), l.Text(); got != want {
				t.Fatalf("row %d: the screen opens with\n\t%q\nand row %d of the pin is\n\t%q", a.top, got, j, want)
			}
		}
		// And what it cost, which is the rows it drew and not one more: the window is the screen
		// less the chrome, and the tail measurement above is the same chrome without the pin in it.
		if a.rows != full-len(drawn) {
			t.Fatalf("row %d: the window is %d rows under a %d-row pin, and %d rows with no pin at all", a.top, a.rows, len(drawn), full)
		}
		// Said once. A pinned line that is also in the transcript directly underneath it is the same
		// words twice, one row apart, and the count of lost rows is what bounds that away.
		for _, l := range drawn {
			if strings.TrimSpace(l.Text()) == "" {
				continue
			}
			if n := onScreen(f, l); n != 1 {
				t.Fatalf("row %d: %q is on the screen %d times", a.top, strings.TrimRight(l.Text(), " "), n)
			}
		}
		drew[len(drawn)]++
	}
	// Both shapes have to have happened, or the sweep proved less than it looks: one row lost is
	// the ordinary case and two is the ceiling, and a recording whose every turn is one row tall
	// would satisfy every assertion above without once reaching it.
	if drew[1] == 0 || drew[2] == 0 {
		t.Fatalf("the sweep drew %v: it needs a turn the reader can scroll two rows into", drew)
	}

	// And back to the tail, which is the other half of "only while scrolled": not merely absent
	// before the first scroll, but gone again after the last, with the row it borrowed handed back
	// to the transcript.
	if !a.key(mustKey(t, "ctrl+end")) {
		t.Fatal("ctrl+end from the top of the log asked for no frame")
	}
	screen(t, a)
	if h, ok := header(t, a); ok {
		t.Fatalf("the reader is following the tail again and %q is still pinned", head(h.Text))
	}
	if a.rows != full {
		t.Fatalf("the window came back to %d rows, want the %d it had before the scroll", a.rows, full)
	}
}

// Inline mode, where the pin is asked for and left out. The top slot wants a surface that does not
// slide out from under it: there every row above the input belongs to the terminal's history the
// moment it is printed, so a row "pinned" into history is the same message printed again on every
// frame, marching down the screen.
//
// Which makes this a test of a seam rather than of a widget. The app installs the header on the
// same terms as on the alternate screen — it knows nothing about surfaces, and gating it here as
// well would be the same decision made twice in the layer that is meant to be free of modes — and
// Place is what leaves it out. So the claim has two halves that must both hold at once: it is in
// the widget list, and it is not on the screen, and the window is not charged for it.
func TestThePinnedHeaderStaysOffASurfaceThatCannotHoldIt(t *testing.T) {
	a, _ := scrollableFile(t, longSession, ui.ModeInline, 0)
	if h, ok := header(t, a); ok {
		t.Fatalf("a reader following the tail was handed a pinned %q", h.Text)
	}
	full := a.rows

	asked, rows := 0, 0
	for a.scroll(-1) {
		f := screen(t, a)
		h, ok := header(t, a)
		if !ok {
			continue
		}
		asked++
		for _, l := range h.Render(a.vp.Width, 0, a.r.Glyphs) {
			if strings.TrimSpace(l.Text()) == "" {
				continue
			}
			rows++
			if n := onScreen(f, l); n != 0 {
				t.Fatalf("row %d: %q is on the screen %d times, on a surface that cannot keep it there", a.top, strings.TrimRight(l.Text(), " "), n)
			}
		}
		if a.rows != full {
			t.Fatalf("row %d: the window is %d rows with a pin nobody drew, and %d rows at the tail", a.top, a.rows, full)
		}
	}
	if asked == 0 || rows == 0 {
		t.Fatalf("the sweep installed %d pins drawing %d rows between them, so nothing here was left out of anything", asked, rows)
	}
}

// The scrollbar reaches a screen wide enough to hold it, which is the only half of the bar this
// package owns: internal/ui proves the thumb is the right length in the right place and that the
// column spends exactly the two reserved cells, and not one of those cells is drawn unless chrome
// installs the widget. Nothing else in this file would notice if that line went. The reservation
// belongs to the layout, so the transcript wraps two columns early either way, and every other
// assertion here would keep passing against a screen with a blank stripe down the edge of it.
//
// The window is widened by hand rather than built wide, because a hundred columns is what turns
// the side panels on and the folded player is eighty: resize is the same call the SIGWINCH handler
// makes, so this is a reader dragging the window out and then reading on.
//
// Both the tail and a screen scrolled away from it, because the tail is the half a gate would
// quietly break. The pinned row above the transcript is only up while the reader has scrolled
// away, and that rule is right for a landmark and wrong for a bar: a reader following the tail
// still has the whole conversation above them, and how much of it there is is the one thing the
// column says before they have touched a key.
//
// The count is the whole window rather than "a thumb somewhere". A bar drawn down part of the
// track is a bar with gaps in it, and the height side() is handed is the window's own — so the
// rows the frame says it is showing are exactly the rows that have to carry a cell.
func TestTheScrollbarReachesAScrolledScreen(t *testing.T) {
	a, _ := scrollable(t, ui.ModeAlt)
	if a.vp.SidePanels {
		t.Fatalf("an %d-column screen already holds side panels, so widening it proves nothing", a.vp.Width)
	}
	const width = 100
	a.resize(width, a.vp.Height)
	for i, where := range []string{"at the tail", "scrolled away"} {
		if i > 0 && !a.scroll(-1) {
			t.Fatalf("the view will not move up from row %d", a.top)
		}
		f := screen(t, a)
		if f.Width != width {
			t.Fatalf("%s: the frame reports width %d after a resize to %d", where, f.Width, width)
		}
		if f.Scroll.Above+f.Scroll.Below == 0 {
			t.Fatalf("%s: the whole conversation fits in the %d rows on screen at width %d, so there is no bar to find", where, f.Scroll.Rows, width)
		}
		track, thumb := 0, 0
		for _, l := range f.Live {
			if len(l) == 0 {
				continue
			}
			switch l[len(l)-1].Style {
			case "scroll.track":
				track++
			case "scroll.thumb":
				thumb++
			}
		}
		if thumb == 0 {
			t.Errorf("%s: %d rows are off screen at width %d and no row of the frame ends in a thumb cell:\n%s", where, f.Scroll.Above+f.Scroll.Below, width, f.Plain())
		}
		if got := track + thumb; got != f.Scroll.Rows {
			t.Errorf("%s: the frame shows %d rows of the transcript and %d of them end in a bar cell", where, f.Scroll.Rows, got)
		}
	}
}

// both ends: an unbroken rule when nobody asked for a word, and exactly the caller's word
// when somebody did. It is asserted against a drawn frame rather than against the editor's
// field, because the field being assigned is not the claim — the claim is that a reader
// sees it, and the assignment happens in New, where a later refactor could drop it and no
// other test would notice. The default is the half worth pinning: it is the kind of default
// an edit restores by accident, and "prompt" in the border was the frame describing itself.
func TestTheInputTitleComesFromTheConfig(t *testing.T) {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	tl := ui.DefaultGlyphs().Get("frame.tl")
	for _, title := range []string{"", "branch: main"} {
		a := New(Config{Width: 72, InputTitle: title})
		var top string
		for _, l := range strings.Split(a.Fold().Plain(), "\n") {
			if strings.HasPrefix(l, tl) {
				top = l
			}
		}
		if top == "" {
			t.Fatalf("%q: the fold drew no top border to look in", title)
		}
		if title == "" {
			if strings.ContainsAny(top, letters) {
				t.Errorf("the default border carries a title: %q", top)
			}
			continue
		}
		if !strings.Contains(top, " "+title+" ") {
			t.Errorf("the border %q does not hold %q let into the rule", top, title)
		}
	}
}

// TestTheBandsShapeIsTheConfigsAndTheClockIsOurs is the plumbing between a config file and
// two bands of light, and the asymmetry in it is the part a reader can be surprised by.
// Config.Shine.Style is a switch and nothing else: a caller says whether the interface glints
// at all, and this package says which of ui's two declared keys each band is drawn in — so the
// key that goes in is thrown away, and one field of the value that comes back is never the
// field that was set. Phase is thrown away for a harder reason: App.phase is the only clock in
// the program, the same counter the spinner is indexed by, and a band running on a number a
// config wrote would drift away from the spinner beside it with no second timer to blame.
//
// Everything else survives untouched, which is what makes [anim] period mean something. The
// case below is built out of numbers nothing ships so that a band rebuilt from the defaults
// instead of from the config fails here rather than looking right.
func TestTheBandsShapeIsTheConfigsAndTheClockIsOurs(t *testing.T) {
	cfg := Config{Width: 72, Shine: ui.Shimmer{
		Style: ui.StatusShine, Phase: 99, Period: 30, Travel: 6, Width: 9,
	}}
	a := New(cfg)
	a.phase = 7
	// The verb's band, which the chrome hands over on every frame regardless of what the row
	// will say: the ladder in ui decides which rung is drawn, and a shine wired per-state here
	// would be that ladder written a second time in a package that cannot see it.
	want := ui.Shimmer{Style: ui.StatusShine, Phase: 7, Period: 30, Travel: 6, Width: 9}
	if got := statusBand(t, a); got != want {
		t.Errorf("the status band is %+v, want %+v", got, want)
	}
	// The box's band is the same shape at half the rate: Slower doubles the rest between passes
	// and touches neither the pass nor the width, so the light crosses the box at exactly the
	// speed it crosses the verb and simply comes round half as often. That is the one place the
	// two bands differ and it is one multiplication, at the call, where a reader can find it.
	base := a.shine(ui.InputShine)
	in := a.inputShine()
	if in.Style != ui.InputShine {
		t.Errorf("the box glints in %q, want %q", in.Style, ui.InputShine)
	}
	if in.Phase != a.phase {
		t.Errorf("the box's band is on phase %d, want the loop's %d", in.Phase, a.phase)
	}
	if in.Travel != base.Travel || in.Width != base.Width {
		t.Errorf("the box's pass is %d ticks over %d columns, want the verb's %d over %d",
			in.Travel, in.Width, base.Travel, base.Width)
	}
	if in.Period != 2*base.Period {
		t.Errorf("the box waits %d ticks between passes, want twice the verb's %d",
			in.Period, base.Period)
	}
	// And a config that never asked for light gets none, which is the zero value doing the
	// switching: this is the state every golden file and both corpus sweeps render in.
	off := New(Config{Width: 72})
	off.phase = 7
	if got := off.inputShine(); got.On() {
		t.Errorf("an unconfigured player glints on the box: %+v", got)
	}
	if got := statusBand(t, off); got.On() {
		t.Errorf("an unconfigured player glints on the verb: %+v", got)
	}
}

// TestTheBoxGlintsOnlyWhileItIsTheHumansTurn is the condition the whole animation hangs on,
// and it is a condition about whose turn it is rather than about what the interface is doing.
// A light travelling the input frame says "this is yours now"; the same light while the model
// is mid-answer says nothing and competes with the text arriving above it for the one thing
// the reader has to spend. So the box glints while the interface is waiting on a person and is
// dark otherwise — and "waiting on a person" is not the same predicate as "not working": an
// approval is blocked on an answer with Active still true, and that is a turn like any other.
//
// A box with text in it is dark for a different reason from a box that is busy. The reader is
// looking at their own half-written line, the cursor is already saying where they are, and a
// band crossing the words underneath it is a distraction from the thing it would be pointing at.
//
// The frames are drawn rather than the field read, because the derivation being right is not
// the claim: draw hands the band to the editor in one line, and a refactor that drops that line
// leaves every value assertion above green and the box dark for good.
func TestTheBoxGlintsOnlyWhileItIsTheHumansTurn(t *testing.T) {
	// A pass of two ticks in a cycle of six, so a sweep of one cycle finds both halves of it
	// quickly and no assertion below has to know which phase is which.
	shine := ui.Shimmer{Style: ui.InputShine, Period: 6, Travel: 2, Width: 4}
	for _, tc := range []struct {
		name string
		lit  bool
		set  func(*App)
	}{
		{"an empty prompt", true, func(*App) {}},
		{"an approval waiting on a key", true, func(a *App) {
			a.st.Active, a.st.Quiescent = true, "approval"
		}},
		{"the model mid-answer", false, func(a *App) { a.st.Active = true }},
		{"a line already typed", false, func(a *App) { a.ed.SetText("go test ./...") }},
	} {
		a := New(Config{Width: 72, Shine: shine})
		tc.set(a)
		seen := 0
		for phase := 0; phase < 2*shine.Period; phase++ {
			a.phase = phase
			if err := a.draw(); err != nil {
				t.Fatalf("%s: phase %d: %v", tc.name, phase, err)
			}
			if band, want := a.ed.Shine, a.inputShine(); band != want {
				t.Fatalf("%s: the frame drew %+v, want the derived %+v", tc.name, band, want)
			}
			if litSpans(t, a) > 0 {
				seen++
			}
		}
		switch {
		case tc.lit && seen == 0:
			t.Errorf("%s: no phase of two whole cycles put light on the box", tc.name)
		case !tc.lit && seen > 0:
			t.Errorf("%s: the box glinted on %d of %d phases", tc.name, seen, 2*shine.Period)
		}
	}
}

// litSpans counts the spans of the input box drawn in any rung of the box's ladder. It matches
// on the prefix rather than on the four keys by name because the ladder lives in ui, where the
// falloff is decided and where a rung can be added to it — a list here would be that decision
// copied into a package that has no say in it, and would go stale the day it changed. Spans and
// not columns because the question is only whether there is light on the frame at all; which
// columns it lands on is settled in ui, against a Line, by a test that can see the arithmetic.
func litSpans(t *testing.T, a *App) int {
	t.Helper()
	rows, _ := a.ed.Render(72, ui.DefaultGlyphs())
	n := 0
	for _, l := range rows {
		for _, sp := range l {
			if strings.HasPrefix(sp.Style, ui.InputShine) {
				n++
			}
		}
	}
	return n
}

// statusBand digs the verb's band out of the widget list the chrome builds, rather than reading
// it off a field, because the list is what the renderer is handed: a band the status row never
// receives is a band nobody draws, however well it was derived.
func statusBand(t *testing.T, a *App) ui.Shimmer {
	t.Helper()
	for _, w := range a.chrome() {
		if sw, ok := w.(ui.StatusWidget); ok {
			return sw.Shine
		}
	}
	t.Fatal("the chrome handed over no status row to carry a band")
	return ui.Shimmer{}
}

// firstDiff says where two documents stop agreeing. The thing it reports on is a seventy-row
// conversation, and printing two of those side by side is a failure nobody reads.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) && i < len(g); i++ {
		if w[i] != g[i] {
			return fmt.Sprintf("row %d:\n want %q\n  got %q", i, w[i], g[i])
		}
	}
	return fmt.Sprintf("%d rows handed over, want %d", len(g), len(w))
}

// draggable parks a folded player on a screen wide enough to hold the column the bar is drawn in,
// at a reading position with hidden text on both sides of the window and a row pinned above it.
// That is the state every gesture below is about: a pill with bare track above it and bare track
// below it, in a strip whose first row is not the screen's.
//
// The position is searched for rather than written down. Where the pinned row comes up and how long
// the pill is depend on what the transcript wrapped to at this width, so a row number here would be
// a number the next recording invalidates in silence — and two of the shapes matter. A strip
// starting on row zero would leave the one subtraction the pointer does unexercised by every test
// here, and a pill one row long would hide the grab: with nothing to take hold of but the middle,
// a drag that forgot where the reader took hold of the pill would look exactly like one that
// remembered.
func draggable(t *testing.T) (*App, *probe) {
	t.Helper()
	a, p := scrollableSize(t, linesChanged, ui.ModeAlt, 0, 120, 24)
	if a.side.W != ui.SideCols {
		t.Fatalf("a %d-column alternate screen left %+v beside the transcript, so there is no bar here to point at", a.vp.Width, a.side)
	}
	tail, shape := a.top, ""
	for top := tail / 2; top > 0; top-- {
		park(t, a, top)
		if a.top != top || a.below == 0 || a.side.Y == 0 {
			shape = fmt.Sprintf("row %d came back as %d with %d below it and the strip on row %d", top, a.top, a.below, a.side.Y)
			continue
		}
		pill, n, _ := pillOf(t, screen(t, a))
		if pill > 0 && pill+n < a.side.H && n >= 3 {
			return a, p
		}
		shape = fmt.Sprintf("row %d drew a %d-row pill on rows %d..%d of a %d-row strip", top, n, pill, pill+n-1, a.side.H)
	}
	t.Fatalf("no reading position in %s puts a pill of three rows or more between two pieces of bare track under a pinned row: %s", linesChanged, shape)
	return nil, nil
}

// park puts the window on a row and draws it, which is the pair every pointer report works from:
// the offset comes back clamped and the strip comes back measured.
func park(t *testing.T, a *App, top int) {
	t.Helper()
	a.scrolled, a.top = true, top
	if err := a.draw(); err != nil {
		t.Fatalf("draw at row %d: %v", top, err)
	}
}

// pointerAt is the report a terminal writes for one gesture, in the terminal's own coordinates.
func pointerAt(act term.MouseAction, btn term.MouseButton, col, row int) term.Event {
	return term.Event{Kind: term.EventMouse, Mouse: term.Mouse{Action: act, Button: btn, Col: col, Row: row}}
}

// pressOn, dragTo and letGo are the three left-button reports on a row of the strip as the last
// frame drew it, which is where the reader's finger is: they are following the bar, not a row of
// the screen. The addition here is the subtraction mouse does, from the other side.
func pressOn(a *App, row int) term.Event {
	return pointerAt(term.MousePress, term.MouseLeft, a.side.X, a.side.Y+row)
}

func dragTo(a *App, row int) term.Event {
	return pointerAt(term.MouseDrag, term.MouseLeft, a.side.X, a.side.Y+row)
}

func letGo(a *App, row int) term.Event {
	return pointerAt(term.MouseRelease, term.MouseLeft, a.side.X, a.side.Y+row)
}

// gesture hands one report to the app the way the loop does — through handle, so what is under test
// is the path a session takes rather than a method nothing calls — and reports whether the frame has
// to be drawn again.
func gesture(t *testing.T, a *App, ev term.Event) bool {
	t.Helper()
	dirty, err := a.handle(ev)
	if err != nil {
		t.Fatalf("handle %+v: %v", ev.Mouse, err)
	}
	return dirty
}

// pillOf reads the pill off a drawn frame: which row of the strip it starts on, how long it is, and
// whether it is drawn held.
//
// Off the ink and not out of the widget list, because a press has to be answered where the reader is
// looking. Held is a style and nothing else, so a highlight nobody draws is not a highlight; and a
// pill in two pieces, or half of it held, would satisfy every count while being something no reader
// could aim at.
func pillOf(t *testing.T, f ui.Frame) (top, n int, held bool) {
	t.Helper()
	top = -1
	for i, l := range f.Live {
		if len(l) == 0 {
			continue
		}
		s := l[len(l)-1].Style
		if s != "scroll.thumb" && s != "scroll.thumb.held" {
			continue
		}
		row := i - f.Side.Y
		switch {
		case top < 0:
			top, held = row, s == "scroll.thumb.held"
		case row != top+n:
			t.Fatalf("the pill is in two pieces: rows %d..%d of the strip and again at %d", top, top+n-1, row)
		case held != (s == "scroll.thumb.held"):
			t.Fatalf("the pill is half held: row %d of the strip is %q and row %d was not", row, s, top)
		}
		n++
	}
	if top < 0 {
		t.Fatalf("no row of this frame is drawn in the thumb's style, so there is no pill to point at\n%s", f.Plain())
	}
	return top, n, held
}

// A press on the pill takes hold of it and moves nothing.
//
// One row of track can be worth many rows of transcript, so a press that asked where the pointer was
// would answer with the nearest row of that coarse grid — a different row than the pill was drawn
// from — and the conversation would jump the instant the reader touched the thing they were reaching
// for. The pill is on the screen already: the gesture has not asked for anything yet, and what it
// has to show for itself is the highlight.
//
// Every row of the pill, because the grab that comes back is measured from wherever the press landed,
// and a pill with an end and a middle is the only kind that can tell those apart.
func TestAPressOnThePillTakesHoldAndMovesNothing(t *testing.T) {
	a, _ := draggable(t)
	top, n, held := pillOf(t, screen(t, a))
	if held {
		t.Fatal("the pill is drawn held before anything has touched it")
	}
	at := a.top
	for row := top; row < top+n; row++ {
		if !gesture(t, a, pressOn(a, row)) {
			t.Errorf("a press on row %d of the pill asked for no frame, so the highlight would not arrive", row)
		}
		if !a.dragging {
			t.Fatalf("a press on row %d of the pill did not take hold of it", row)
		}
		if a.top != at {
			t.Fatalf("a press on row %d of the pill moved the window from %d to %d", row, at, a.top)
		}
		switch gotTop, gotN, held := pillOf(t, screen(t, a)); {
		case gotTop != top || gotN != n:
			t.Errorf("a press on row %d of the pill left it on rows %d..%d, want the %d..%d it was pressed on", row, gotTop, gotTop+gotN-1, top, top+n-1)
		case !held:
			t.Errorf("a press on row %d of the pill did not draw it held", row)
		}
		if !gesture(t, a, letGo(a, row)) {
			t.Errorf("the release on row %d asked for no frame, so the highlight would stay on", row)
		}
		if a.dragging {
			t.Fatalf("the release on row %d left the pill held", row)
		}
		if _, _, held := pillOf(t, screen(t, a)); held {
			t.Errorf("the release on row %d left the pill drawn held", row)
		}
		if a.top != at {
			t.Fatalf("the release on row %d moved the window from %d to %d", row, at, a.top)
		}
	}
}

// A press on bare track jumps the window, and puts the pill under the pointer.
//
// The arithmetic is ui's: Offset answers with the nearest window a pill can be drawn from, and ui
// asserts against the drawn pill that the answer leaves it under the finger. What is left for here is
// that the app asks about the row the reader pressed — the strip's first row is not the screen's, and
// a hit test taking one for the other would draw the pill beside the pointer while satisfying every
// assertion about the number — and that it hands the answer to the same scroll the keys go through,
// which is what makes both ends reachable and the bottom hand the conversation back.
func TestAPressOnBareTrackPutsThePillUnderThePointer(t *testing.T) {
	a, _ := draggable(t)
	home, strip := a.top, a.side
	top, n, _ := pillOf(t, screen(t, a))
	jumped, sized := 0, 0
	for row := 0; row < strip.H; row++ {
		if row >= top && row < top+n {
			continue
		}
		park(t, a, home)
		if a.side != strip {
			t.Fatalf("parking back on row %d drew the strip at %+v, want the %+v every press below is aimed at", home, a.side, strip)
		}
		if !gesture(t, a, pressOn(a, row)) {
			t.Fatalf("a press on row %d of the track asked for no frame", row)
		}
		if !a.dragging {
			t.Fatalf("a press on row %d of the track did not take hold of the pill", row)
		}
		f := screen(t, a)
		moved, length, held := pillOf(t, f)
		if !held {
			t.Errorf("a press on row %d of the track did not draw the pill held", row)
		}
		if a.top != home {
			jumped++
		}
		switch row {
		case 0:
			if a.top != 0 {
				t.Errorf("a press on the first row of the track left %d rows above the window, want the top of the log", a.top)
			}
		case strip.H - 1:
			if a.below != 0 || a.scrolled {
				t.Errorf("a press on the last row of the track left %d rows below the window (scrolled=%v), want the tail, and the conversation handed back with it", a.below, a.scrolled)
			}
		}
		// Where the pill landed is asked in the screen's rows rather than the strip's, because the strip
		// is allowed to move: the pinned row is up only while the reader is inside a turn, so a jump to
		// the top of the log takes it off and hands the bar the row it was using. The pointer never moved
		// through any of that, and it is under the pointer that the pill has to be — so the two rows are
		// compared where the pointer's is spoken. Its length is a claim only when the strip kept its own,
		// since a taller strip is drawn a longer pill by the same arithmetic ui already asserts.
		want := strip.Y + row
		if at := f.Side.Y + moved; want < at || want >= at+length {
			t.Errorf("a press on screen row %d left the pill on screen rows %d..%d, out from under the pointer", want, at, at+length-1)
		}
		if f.Side == strip {
			if length != n {
				t.Errorf("a press on row %d of the track drew a pill %d rows long, want the %d it had: a jump is not a resize", row, length, n)
			}
			sized++
		}
		gesture(t, a, letGo(a, row))
	}
	if jumped == 0 {
		t.Errorf("not one press on the %d rows of bare track moved the window", strip.H-n)
	}
	if sized == 0 {
		t.Errorf("every one of the %d presses moved the strip as well as the pill, so the pill's length was never a claim", strip.H-n)
	}
}

// A drag carries the window, and the bottom of the track hands the conversation back.
//
// The grab is what this is really about. A press takes hold of the pill at the row it landed on, and
// every report after it has to keep that offset: drag the pill's last row up by one and the window
// goes up, while a hold that forgot where it was taken would treat the pointer as the pill's first row
// and send the window the other way. Both readings put the pointer inside the pill, and both draw a
// pill of the right length, so what is asserted here is the direction — a walk up the track never
// moves the window down, a walk back down never moves it up, and the two ends are the log's own.
func TestADragCarriesTheWindowAndTheBottomHandsItBack(t *testing.T) {
	a, _ := draggable(t)
	top, n, _ := pillOf(t, screen(t, a))
	home := a.top

	if gesture(t, a, dragTo(a, top)) {
		t.Error("a motion nobody pressed for asked for a frame")
	}
	if a.dragging || a.top != home {
		t.Fatalf("a motion nobody pressed for took hold (%v) or moved the window to %d from %d", a.dragging, a.top, home)
	}
	if _, _, held := pillOf(t, screen(t, a)); held {
		t.Error("a motion nobody pressed for drew the pill held")
	}

	if !gesture(t, a, pressOn(a, top+n-1)) {
		t.Fatal("the press that starts the drag asked for no frame")
	}
	if a.top != home {
		t.Fatalf("the press on the pill's last row moved the window to %d from %d before a single motion", a.top, home)
	}
	for row := top + n - 2; row >= 0; row-- {
		was, at := a.top, a.side
		gesture(t, a, dragTo(a, row))
		if a.top > was {
			t.Fatalf("a drag up to row %d moved the window down, from %d to %d: the pill was let go of where it was taken", row, was, a.top)
		}
		pointerInPill(t, a, at, row)
	}
	if a.top != 0 {
		t.Errorf("a drag up to the first row of the track left %d rows above the window, want the top of the log", a.top)
	}
	if !a.dragging {
		t.Fatal("the pill was let go of somewhere on the way up")
	}
	for row := 1; row < a.side.H; row++ {
		was, at := a.top, a.side
		gesture(t, a, dragTo(a, row))
		if a.top < was {
			t.Fatalf("a drag down to row %d moved the window up, from %d to %d", row, was, a.top)
		}
		pointerInPill(t, a, at, row)
	}
	if a.below != 0 || a.scrolled {
		t.Errorf("a drag down to the last row of the track left %d rows below the window (scrolled=%v), want the tail, and the conversation followed again", a.below, a.scrolled)
	}
	if a.top <= 0 {
		t.Errorf("the walk back down left the window on row %d, so the two ends of the track are the same place", a.top)
	}
	if !gesture(t, a, letGo(a, a.side.H-1)) {
		t.Error("the release that ends the drag asked for no frame")
	}
	if a.dragging {
		t.Fatal("the release did not let go of the pill")
	}
	if _, _, held := pillOf(t, screen(t, a)); held {
		t.Error("the pill is still drawn held after the release")
	}
}

// pointerInPill says the pill is drawn under the pointer, and declines to say it when the strip moved
// out from under the pointer instead: at is the bar the report was aimed at, and a taller or shorter
// one means the pinned row came or went with the window, leaving the finger a row off a bar that has
// stopped moving. The next report corrects that, and nothing the hit test could have done would have.
func pointerInPill(t *testing.T, a *App, at ui.Rect, row int) {
	t.Helper()
	f := screen(t, a)
	pill, n, held := pillOf(t, f)
	if !held {
		t.Fatalf("the pill is not drawn held while a drag to row %d is being carried", row)
	}
	if f.Side != at {
		return
	}
	if row < pill || row >= pill+n {
		t.Errorf("a drag to row %d of the strip left the pill on rows %d..%d, out from under the pointer", row, pill, pill+n-1)
	}
}

// A pointer the bar was not given changes nothing.
//
// The bar owns two columns of one strip and the left button, and every report outside that belongs to
// whatever the terminal is doing with the rest of the screen — a middle click pasting, a right click
// opening a menu, a drag selecting a line of the transcript. Refusing them is not politeness: the app
// answers a report it handled with a frame, so a bar that swallowed a text selection would redraw over
// the terminal's own highlight and take hold of a pill the reader never touched. The last case is the
// control that keeps the rest honest — the bar is two columns wide and only one of them is inked, so a
// press on the blank one has to be taken, or every refusal above could be a bar that answers nothing.
func TestAPointerTheBarWasNotGivenChangesNothing(t *testing.T) {
	a, _ := draggable(t)
	strip, home := a.side, a.top
	top, _, _ := pillOf(t, screen(t, a))
	row := strip.Y + top
	for _, c := range []struct {
		what string
		ev   term.Event
	}{
		{"a release nobody was holding", pointerAt(term.MouseRelease, term.MouseLeft, strip.X, row)},
		{"a motion nobody pressed for", pointerAt(term.MouseDrag, term.MouseLeft, strip.X, row)},
		{"a middle click on the pill", pointerAt(term.MousePress, term.MouseMiddle, strip.X, row)},
		{"a right click on the pill", pointerAt(term.MousePress, term.MouseRight, strip.X, row)},
		{"a middle drag along the pill", pointerAt(term.MouseDrag, term.MouseMiddle, strip.X, row)},
		{"a press in the transcript's last column", pointerAt(term.MousePress, term.MouseLeft, strip.X-1, row)},
		{"a press past the bar's last column", pointerAt(term.MousePress, term.MouseLeft, strip.X+strip.W, row)},
		{"a press on the row above the strip", pointerAt(term.MousePress, term.MouseLeft, strip.X, strip.Y-1)},
		{"a press on the row below the strip", pointerAt(term.MousePress, term.MouseLeft, strip.X, strip.Y+strip.H)},
	} {
		if gesture(t, a, c.ev) {
			t.Errorf("%s asked for a frame", c.what)
		}
		if a.dragging {
			t.Fatalf("%s took hold of the pill", c.what)
		}
		if a.top != home || a.side != strip {
			t.Fatalf("%s moved the window to %d or the strip to %+v", c.what, a.top, a.side)
		}
		if _, _, held := pillOf(t, screen(t, a)); held {
			t.Errorf("%s drew the pill held", c.what)
		}
	}
	if !gesture(t, a, pointerAt(term.MousePress, term.MouseLeft, strip.X+strip.W-1, row)) {
		t.Fatalf("a press on column %d, the bar's last, asked for no frame: the strip is %d columns wide and the reader can press either", strip.X+strip.W-1, strip.W)
	}
	if !a.dragging {
		t.Error("a press on the bar's last column did not take hold of the pill")
	}
}

// A surface with no column beside it has no bar to press.
//
// Inline mode prints into the terminal's own scrollback and claims no mouse at all, and the alternate
// screen only reserves a column beside the transcript once there is width to spare. Both leave Side
// empty, and an empty strip has to refuse every report rather than let the arithmetic run on a zero
// height — which is also what keeps a pointer over the transcript from being read as a bar the reader
// cannot see.
func TestASurfaceWithNoColumnBesideItHasNoBarToPress(t *testing.T) {
	for _, c := range []struct {
		what  string
		mode  ui.Mode
		width int
	}{
		{"an eighty-column alternate screen", ui.ModeAlt, 80},
		{"a hundred-and-twenty-column inline session", ui.ModeInline, 120},
	} {
		a, _ := scrollableSize(t, linesChanged, c.mode, 0, c.width, 24)
		if a.side != (ui.Rect{}) {
			t.Fatalf("%s reserved %+v beside the transcript, and this test is about the screens that reserve nothing", c.what, a.side)
		}
		home := a.top
		for _, act := range []term.MouseAction{term.MousePress, term.MouseDrag, term.MouseRelease} {
			for _, col := range []int{0, c.width - 2, c.width - 1} {
				for _, row := range []int{0, 12, 23} {
					ev := pointerAt(act, term.MouseLeft, col, row)
					if gesture(t, a, ev) {
						t.Errorf("%s answered %+v with a frame", c.what, ev.Mouse)
					}
					if a.dragging || a.top != home {
						t.Fatalf("%s let %+v take hold (%v) or move the window to %d from %d", c.what, ev.Mouse, a.dragging, a.top, home)
					}
				}
			}
		}
	}
}
