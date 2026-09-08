package app

import (
	"errors"
	"sort"
	"strconv"

	"arxi.local/sim/internal/term"
)

// The keymap is a table keyed by name, and that is the whole design. term.Decode
// turns bytes into a name a human can type — "ctrl+left", "alt+backspace" — so a
// binding is a string that fits in a config file, gets printed by a subcommand and
// reads correctly in a diff. Rebinding costs one map entry and no code.
//
// Actions are declared the way ui.Keys and ui.GlyphKeys are: a closed table with a
// doc line each, an index that panics on a duplicate, and an override naming
// something nothing declares is an error rather than a silent no-op. The table is
// short on purpose. Every action in it is one the editor already implements, because
// a binding the user is invited to set and that then does nothing is worse than no
// binding at all.

// Action is what a key does. It is a string because a config file has to name it.
type Action string

// The declared actions. ActionNone is not one of them: it is what Lookup returns for
// a key nothing claims, and what an override sets to take a default binding away.
const (
	ActionNone Action = ""

	ActionQuit      Action = "quit"
	ActionInterrupt Action = "interrupt"
	ActionSubmit    Action = "submit"
	ActionNewline   Action = "newline"
	ActionCancel    Action = "cancel"

	ActionBackspace Action = "backspace"
	ActionDelete    Action = "delete"
	ActionKillWord  Action = "kill-word"
	ActionKillToEnd Action = "kill-to-end"
	ActionKillLine  Action = "kill-line"

	ActionLeft      Action = "left"
	ActionRight     Action = "right"
	ActionWordLeft  Action = "word-left"
	ActionWordRight Action = "word-right"
	ActionHome      Action = "home"
	ActionEnd       Action = "end"

	ActionHistoryPrev Action = "history-prev"
	ActionHistoryNext Action = "history-next"

	ActionScrollUp       Action = "scroll-up"
	ActionScrollDown     Action = "scroll-down"
	ActionScrollUpFast   Action = "scroll-up-fast"
	ActionScrollDownFast Action = "scroll-down-fast"
	ActionPageUp         Action = "page-up"
	ActionPageDown       Action = "page-down"
	ActionScrollTop      Action = "scroll-top"
	ActionScrollBottom   Action = "scroll-bottom"

	ActionJumpPrevMessage Action = "jump-prev-message"
	ActionJumpNextMessage Action = "jump-next-message"

	ActionApprove Action = "approve"
	ActionDeny    Action = "deny"

	ActionComplete Action = "complete"
	ActionTeam     Action = "team"
)

// ActionDecl is one declared action and the line that explains it. The doc is not a
// comment: `arxi-sim keys` prints it, so it is written for whoever is editing a config
// and not for whoever is reading this file.
type ActionDecl struct {
	Action Action
	Doc    string
}

// ActionKeys is every action a binding may name.
//
// There is no redraw, deliberately: the emitter tracks what the terminal has already
// been given in counters it does not expose a reset for, and the sequence that would
// clear scrollback (CSI 3 J) is banned by a test, so "redraw" could only be a lie. Every
// frame is a full repaint of the surface anyway, so there is nothing for it to fix.
//
// Scrolling is here, and it used to be absent for a reason that turned out to be wrong.
// The claim was that the transcript is the terminal's own scrollback and scrolling is
// therefore the terminal's job. It is not, in either mode: alt-screen scrollback does not
// exist, and inline mode holds the transcript inside a region it repaints — the rows only
// reach the terminal's history when the session ends. In both cases the conversation above
// the window is ours to show or to hide, so moving the view has to be an action, and the
// window it moves through is Frame.Scroll.
var ActionKeys = []ActionDecl{
	{ActionQuit, "leave the simulation and restore the terminal"},
	{ActionInterrupt, "clear the input; pressed twice in a row, leaves"},
	{ActionSubmit, "send the typed line, or answer an open approval with it"},
	{ActionNewline, "start another line in the input instead of sending it"},
	{ActionCancel, "clear the input line; never quits"},

	{ActionBackspace, "delete the rune before the cursor"},
	{ActionDelete, "delete the rune under the cursor"},
	{ActionKillWord, "delete the word before the cursor"},
	{ActionKillToEnd, "delete from the cursor to the end of the line"},
	{ActionKillLine, "delete the whole line"},

	{ActionLeft, "move one rune left"},
	{ActionRight, "move one rune right"},
	{ActionWordLeft, "move one word left"},
	{ActionWordRight, "move one word right"},
	{ActionHome, "move to the start of the line"},
	{ActionEnd, "move to the end of the line"},

	{ActionHistoryPrev, "recall the previous line, stashing the half-typed one"},
	{ActionHistoryNext, "walk back towards the line you were typing"},

	{ActionScrollUp, "scroll the conversation up one line"},
	{ActionScrollDown, "scroll the conversation down one line"},
	{ActionScrollUpFast, "scroll up by one wheel notch: several lines, set by -scroll"},
	{ActionScrollDownFast, "scroll down by one wheel notch: several lines, set by -scroll"},
	{ActionPageUp, "scroll up by one screen, keeping a line of overlap"},
	{ActionPageDown, "scroll down by one screen, keeping a line of overlap"},
	{ActionScrollTop, "jump to the start of the conversation"},
	{ActionScrollBottom, "jump to the end and follow it again as it grows"},

	{ActionJumpPrevMessage, "put the previous turn of your own at the top of the window"},
	{ActionJumpNextMessage, "put the next turn of your own at the top of the window"},

	{ActionApprove, "answer an open approval with allow; types the key when nothing is pending"},
	{ActionDeny, "answer an open approval with deny; types the key when nothing is pending"},

	{ActionComplete, "in the slash menu, fill the command text without running it"},
	{ActionTeam, "open the team roster and live member state"},
}

// actionIndex fails the build's first test rather than the user's first keypress: a
// duplicated declaration means two docs for one action and nothing to choose between.
var actionIndex = func() map[Action]string {
	m := make(map[Action]string, len(ActionKeys))
	for _, d := range ActionKeys {
		if _, dup := m[d.Action]; dup {
			panic("app: action declared twice: " + string(d.Action))
		}
		m[d.Action] = d.Doc
	}
	return m
}()

// UndeclaredActionError is a config naming an action that does not exist. It is an
// error and not a warning because the alternative is a key that silently does nothing
// and a user who blames the terminal.
type UndeclaredActionError struct{ Action Action }

func (e *UndeclaredActionError) Error() string {
	return "app: undeclared action: " + string(e.Action)
}

// UnknownKeyError is a binding whose key name the decoder can never produce. The name
// is quoted because the usual cause is invisible: a trailing space, or "Ctrl+A" when
// the decoder only ever says "ctrl+a".
type UnknownKeyError struct{ Key string }

func (e *UnknownKeyError) Error() string {
	return "app: not a key name: " + strconv.Quote(e.Key)
}

// DefaultBindings is the keymap the program ships with, written as the strings a user
// would type. Every name here round-trips through term.ParseKey, which a test asserts.
//
// ctrl+h, ctrl+i and ctrl+m are absent and unbindable in practice: the bytes they send are
// 0x08, 0x09 and 0x0d, which the decoder reports as backspace, tab and enter, so binding
// them would produce a table entry no keypress can ever reach. ctrl+j used to be listed
// beside them and is the exception that pays for the rule — its 0x0a was being folded into
// enter too, and stopping that is what gave the second line a chord that works today.
func DefaultBindings() map[string]Action {
	return map[string]Action{
		// ISIG is off in raw mode, so ctrl+c arrives as a keypress like any other and there
		// is no signal to fall back on: leaving is a binding or it is nothing.
		//
		// The two keys divide the work the way a shell does, and that division is the whole
		// of the user's report — ctrl+c closed the program where every other tool would have
		// dropped the half-typed line. ctrl+c is the line's key: it throws away what you were
		// typing, and pressed again with nothing left to throw away it leaves, because a
		// reader hitting it on a clear line is asking for the door and knows no other way to
		// ask. ctrl+d is the door itself, once, which is what EOF has always meant.
		//
		// Which press leaves is not decided here. This table says what each key means and
		// App.interrupt counts, because "twice in a row" is a fact about the last keypress
		// and a map has no memory.
		"ctrl+c": ActionInterrupt,
		"ctrl+d": ActionQuit,

		// Submit stays on plain enter, and the second line goes on the chords every editor puts
		// it on. The obstacle is that a return key's modifier is dropped before the byte leaves,
		// so shift+enter and ctrl+enter are both 0x0d — until something distinguishes them, and
		// there are two somethings, one per row below.
		//
		// ctrl+j is the one that works on a terminal shipped before CSI u. Ctrl folds the key
		// into the control range instead of dropping: Windows Terminal and conhost send 0x0a for
		// ctrl+enter, which is ctrl+j's own byte, and the decoder now names it rather than
		// filing it under enter (term.decodeText). Binding it is why ctrl+enter inserts a row
		// here today, and it is what ishakat and Claude Code bind for the same reason. Anyone
		// who really means ctrl+j on its own gets a newline, which is what ctrl+j has meant
		// since the teletype.
		//
		// shift+enter has no such byte and needs the keyboard asked: ui.Emitter.Enter pushes
		// CSI > 1 u, the first Kitty progressive-enhancement flag, and a terminal that speaks it
		// reports shift+enter as CSI 13;2u and ctrl+enter as CSI 13;5u, both of which the decoder
		// already names. That is the row that starts working on a terminal newer than the one
		// this was written on rather than the row that works now, and both are bound because the
		// two hands that reach for them are different and the action is the same.
		//
		// alt+enter is deliberately not here. It is ESC 0x0d, which needs no protocol at all and
		// which the decoder has always read, and it is still the wrong key: Windows Terminal and
		// conhost both use it to toggle fullscreen, so on the machine this simulator is written
		// on the program is never handed the keypress. A binding that cannot fire is worse than
		// no binding, and on a terminal that neither eats it nor speaks CSI u it is one config
		// line away: "alt+enter" = newline under [keys].
		"enter":       ActionSubmit,
		"shift+enter": ActionNewline,
		"ctrl+enter":  ActionNewline,
		"ctrl+j":      ActionNewline,
		"esc":         ActionCancel,

		"backspace":     ActionBackspace,
		"alt+backspace": ActionKillWord,
		"ctrl+w":        ActionKillWord,
		"delete":        ActionDelete,
		"ctrl+k":        ActionKillToEnd,
		"ctrl+u":        ActionKillLine,

		"left":       ActionLeft,
		"right":      ActionRight,
		"ctrl+b":     ActionLeft,
		"ctrl+f":     ActionRight,
		"alt+left":   ActionWordLeft,
		"alt+right":  ActionWordRight,
		"ctrl+left":  ActionWordLeft,
		"ctrl+right": ActionWordRight,
		"home":       ActionHome,
		"end":        ActionEnd,
		"ctrl+a":     ActionHome,
		"ctrl+e":     ActionEnd,

		// The input's history is on the plain arrow keys, which is where a shell keeps it and
		// where a hand reaches first. Nothing can be allowed to send those two names except a
		// finger, and that is a claim about the mouse rather than about this table: a terminal
		// with alternate scroll on (CSI ? 1007) rewrites a wheel notch into exactly these arrows
		// before the program sees it, and no byte distinguishes the two. ui.Emitter.Enter is where
		// that is made safe, both ways round — claiming tracking turns the translation off, and so
		// does switching 1007 off when tracking is not claimed. ctrl+p and ctrl+n stay bound
		// beside them: readline's own names for the same two steps, and a habit is worth an entry.
		"up":     ActionHistoryPrev,
		"down":   ActionHistoryNext,
		"ctrl+p": ActionHistoryPrev,
		"ctrl+n": ActionHistoryNext,

		// Moving the view. The wheel is the wheel, with or without shift — a terminal sends the
		// modifier straight through in the report's button byte, and a reader holding shift to
		// scroll is not asking for something different, so both spellings land on the same
		// action rather than one of them falling through to nothing.
		//
		// Those two names only ever arrive under -mouse now, and they stay bound unconditionally
		// because a binding for a report nobody sends costs one map entry and a fork in this
		// table would cost every reader of it a question. What the default run has instead is the
		// keyboard, which is why the rows below are not a convenience but the whole of scrolling.
		//
		// A notch is worth several rows and not one, for the reason the amount makes plain: a
		// notch is a flick of a finger and a spin is a handful of them, and one line each meant
		// more spins to cross a conversation than anybody will make, which is what the user
		// reported. How many rows is -scroll, because it is a fact about a mouse and a hand and
		// not something this file can know.
		//
		// ctrl+up and ctrl+down (CSI 1;5A, CSI 1;5B) are worth the same notch, because the reader
		// asked for it in those words: they are the keyboard's spelling of the wheel and not a
		// finer version of it, so a hand that has learnt the wheel's step keeps that step when it
		// leaves the mouse alone. -scroll sets both, from one number, for the same reason.
		//
		// The one-row pair is on alt+up and alt+down, which is the key for the line you land one
		// short of. Alt is where the other fine moves already are — alt+left and alt+right are
		// the word steps — and it is free, whereas ctrl+shift+arrow was tried and never arrived.
		"wheelup":         ActionScrollUpFast,
		"wheeldown":       ActionScrollDownFast,
		"shift+wheelup":   ActionScrollUpFast,
		"shift+wheeldown": ActionScrollDownFast,
		"ctrl+up":         ActionScrollUpFast,
		"ctrl+down":       ActionScrollDownFast,
		"alt+up":          ActionScrollUp,
		"alt+down":        ActionScrollDown,
		"pgup":            ActionPageUp,
		"pgdown":          ActionPageDown,
		"ctrl+home":       ActionScrollTop,
		"ctrl+end":        ActionScrollBottom,

		// And shift on the same two arrows walks the reader's own turns, which is the landmark a
		// long conversation is searched by: nobody scrolls back looking for a tool call, they
		// scroll back looking for what they asked. It is here and not on ctrl+shift because
		// ctrl+shift+up never reached the app for the reader who asked for it — that combination
		// is claimed by the terminal or the desktop on enough setups to be worth nothing —
		// whereas shift+up is CSI 1;2A, which arrives.
		"shift+up":   ActionJumpPrevMessage,
		"shift+down": ActionJumpNextMessage,

		// The approval widget draws "y allow  n deny", so these two are what it
		// promises. They are plain letters: the app honours them only while an
		// approval is open and the line is empty, and types them otherwise.
		"y": ActionApprove,
		"n": ActionDeny,

		"tab":    ActionComplete,
		"ctrl+t": ActionTeam,
	}
}

// Keymap resolves a decoded key to an action. It is a map and a method, because the
// lookup happens once per keypress and anything cleverer would be a lie about cost.
type Keymap struct{ binds map[string]Action }

// NewKeymap layers overrides on the defaults. Both go through the same door, so a
// config cannot reach a state the defaults could not: every name is parsed by the same
// decoder that will later produce it, and every action must be declared.
//
// An override to ActionNone removes a binding. That is the only way to take a default
// away, and it is why ActionNone is not in ActionKeys — it is an instruction to the
// keymap, not something a key can do.
//
// Every problem is reported, not just the first: a config with three typos should cost
// one run to fix, not three.
func NewKeymap(overrides map[string]Action) (*Keymap, error) {
	defaults := DefaultBindings()
	km := &Keymap{binds: make(map[string]Action, len(defaults)+len(overrides))}
	problems := make(map[string]error)

	add := func(name string, a Action) {
		k, ok := term.ParseKey(name)
		if !ok {
			problems[name] = &UnknownKeyError{Key: name}
			return
		}
		if _, declared := actionIndex[a]; !declared && a != ActionNone {
			problems[name] = &UndeclaredActionError{Action: a}
			return
		}
		// Canonical form: "alt+ctrl+x" and "ctrl+alt+x" are one binding, and the
		// spelling the decoder will hand Lookup is the one Key.String produces.
		name = k.String()
		if a == ActionNone {
			delete(km.binds, name)
			return
		}
		km.binds[name] = a
	}

	for name, a := range defaults {
		add(name, a)
	}
	// Overrides are applied in name order. Map iteration is random, and two spellings
	// of one key in one config must not resolve differently from one run to the next.
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		add(name, overrides[name])
	}

	if len(problems) > 0 {
		bad := make([]string, 0, len(problems))
		for name := range problems {
			bad = append(bad, name)
		}
		sort.Strings(bad)
		errs := make([]error, 0, len(bad))
		for _, name := range bad {
			errs = append(errs, problems[name])
		}
		return nil, errors.Join(errs...)
	}
	return km, nil
}

// DefaultKeymap is the shipped keymap. It panics rather than returning an error: the
// only way it can fail is a mistake compiled into DefaultBindings, and that is a build
// bug the first test run has to expose, not something a caller can handle.
func DefaultKeymap() *Keymap {
	km, err := NewKeymap(nil)
	if err != nil {
		panic("app: default bindings are invalid: " + err.Error())
	}
	return km
}

// Lookup answers with ActionNone for a key nothing claims, which is most of them: a
// letter is text, and the app is expected to insert it.
func (km *Keymap) Lookup(k term.Key) Action {
	return km.binds[k.String()]
}

// KeyFor is one key that reaches an action, for a message that has to tell the user which
// key to press. It reads the keymap and not DefaultBindings, so what a hint names is the
// key the user's own config binds rather than the one we shipped.
//
// Several keys usually reach one action, and this returns the smallest by name rather than
// the first the map yields: map order is random, and a hint that named a different key on
// every frame would be unreadable. Nothing is sorted — one pass and a comparison is what a
// per-frame call should cost.
//
// It answers "" when nothing is bound at all, which a caller has to handle, because
// ActionNone in a config can take every default away and a hint naming no key is worse
// than no hint.
func (km *Keymap) KeyFor(a Action) string {
	best := ""
	for k, act := range km.binds {
		if act == a && (best == "" || k < best) {
			best = k
		}
	}
	return best
}

// Binding is one row of the keymap as a human reads it, doc included so that `keys`
// prints the table without joining anything by hand.
type Binding struct {
	Key    string
	Action Action
	Doc    string
}

// Bindings returns the whole keymap sorted by action and then by key, which groups the
// aliases — ctrl+b next to left — instead of scattering them alphabetically.
func (km *Keymap) Bindings() []Binding {
	out := make([]Binding, 0, len(km.binds))
	for k, a := range km.binds {
		out = append(out, Binding{Key: k, Action: a, Doc: actionIndex[a]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Action != out[j].Action {
			return out[i].Action < out[j].Action
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// ActionDocs returns the declared actions sorted by name. It is a copy: the table is
// package state and a caller sorting it in place would reorder it for everyone.
func ActionDocs() []ActionDecl {
	out := make([]ActionDecl, len(ActionKeys))
	copy(out, ActionKeys)
	sort.Slice(out, func(i, j int) bool { return out[i].Action < out[j].Action })
	return out
}
