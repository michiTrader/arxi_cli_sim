// Command arxi-sim plays a recorded arxi run in a terminal, so that the interface can be
// built, watched and argued about before the orchestrator it belongs to is finished.
//
// This file is the impure half of the program and nothing else: ask the environment what
// the terminal can do, put it in raw mode, hand the player a channel and a writer, and
// put the terminal back however the run ends. Every decision it makes leaves here as a
// value in an app.Config, which is why the player has tests and this file has none.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"arxi.local/sim/internal/app"
	"arxi.local/sim/internal/config"
	"arxi.local/sim/internal/scenario"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

const usage = `arxi-sim plays a recorded arxi run.

usage:
  arxi-sim play [flags] <scenario.ndjson>
  arxi-sim keys
  arxi-sim check <scenario.ndjson|config.toml>...

play flags, which go before the file because flag parsing stops at the first
argument that is not one:
  -speed f    divide every recorded delay by f: 2 is twice as fast, 0.5 is half
  -instant    fold the whole log and print the last frame as text, no waiting
  -inline     draw under the shell prompt instead of on the alternate screen;
              scroll and resize are the terminal's problem, and a terminal that
              reflows will garble the conversation when the window is dragged
  -alt        take the alternate screen; this is the default, and the flag is
              still accepted so an old command line keeps working
  -ascii      no box drawing and no emoji: the fallback glyph for everything
  -no-color   emit no colour, whatever the terminal claims it can take
  -width n    columns for -instant (default 80); ignored when playing
  -scroll n   how far one notch of the wheel moves the conversation (default 3,
              and 1 on Termux, where a report is one row of a swipe rather than a
              notch); ctrl+up/down move by the same number, alt+up/down by one
  -mouse      let arxi-sim have the mouse, which is the only way a terminal will
              send it a wheel notch: the wheel then scrolls the conversation and
              selecting text needs shift held down. On by default, because a
              wheel that does nothing reads as a broken program; -mouse=false
              gives the mouse back to the terminal and a plain drag selects again
  -shine      sweep a highlight along the input while it is your turn, and along
              the word "working" while it is not. On by default; -shine=false
              stops it, and so does [anim] shine = false
  -title s    a word to let into the top border of the input box; empty by
              default, which draws an unbroken rule
  -config p   read settings from p instead of the file in the default place;
              -config "" reads none at all

An interactive run takes the alternate screen, which is the surface a resize
cannot corrupt: no terminal reflows it, so a drag is one repaint at the new
width. It is printed onto the main screen in one piece when the session ends —
nothing reaches your history before then, and nothing of your own is erased.

The keys are the ones a hand already knows. Enter sends the line and ctrl+enter
starts a second one inside the input; shift+enter does the same on a terminal
that can tell the two chords apart. Plain up and down are the input's history, as
in a shell, and the conversation moves under the keyboard: ctrl+up/down by three
lines, alt+up/down by one, pgup/pgdown a screen, ctrl+home/ctrl+end to the ends,
and shift+up/down jump between your own messages, which is the landmark a long
conversation is actually searched by.

Leaving is ctrl+d, once, which is what EOF has always meant. ctrl+c is the line's
key and not the door: it throws away what you were typing, the way it does in a
shell, and only a second press with nothing left to throw away leaves — while
that second press is live the row under the input says so. esc clears the line
too and never leaves.

The wheel scrolls the conversation, which costs a gesture: a terminal sends a
notch only to a program that has claimed the mouse, and a program holding the
mouse is also handed the drag that would otherwise have selected text — so
selecting inside a run needs shift held down, as it does in an editor or a pager.
-mouse=false trades the other way and hands the mouse back: a plain drag selects
with no modifier, and the wheel then does nothing, because there is no scrollback
on the alternate screen for the terminal to move either. The keys above move the
view under both arrangements and are the spelling nothing can take away. A
repaint can clear a highlight whichever way you run it, so a selection is
steadiest once the replay has caught up; and quitting leaves the whole
conversation on the main screen, in your terminal's own scrollback, where it
selects like any other output.

On Android the same claim buys more and costs nothing. Termux selects with a long
press and its own handles whether or not a program has the mouse, so there is no
drag to protect; and a swipe with the mouse unclaimed does not turn a wheel that
nothing is listening to — Termux sends the arrow keys instead, which walks the
input's history under your finger. There a swipe reports one row at a time rather
than in notches, so -scroll defaults to 1 and one row of swipe moves one row of
conversation, while a real wheel notch still moves three.

Settings live in a file where a flag would not be enough: six flat tables —
[keys], [glyphs], [styles], [input], [scroll] and [anim] — one key = "value" a
line, renaming a binding, redrawing a marker, recolouring a span or setting a
number. It is read from ~/.config/arxi-sim/config.toml, or
%AppData%\arxi-sim\config.toml on Windows, and not having one is the ordinary
case rather than an error. A flag you type beats the file and the file beats the
shipped default, so nothing you asked for on the command line is quietly
overridden by something you wrote last month.

keys prints the whole vocabulary such a file may name: bindings, actions,
glyphs, style keys and widget slots. check loads and validates without playing,
a recording or a config — for a config it prints how much of each table it read,
which is the cheapest answer to whether the file the program found is the file
you have been editing.

When stdin is not a terminal there is nobody to press a key, so the log is
folded and printed exactly as -instant would.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		// One line on stderr and a non-zero status: the transcript went to stdout and a
		// diagnostic mixed into it would end up in somebody's golden file.
		fmt.Fprintln(os.Stderr, "arxi-sim: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	switch args[0] {
	case "play":
		return play(args[1:])
	case "keys":
		return keys()
	case "check":
		return check(args[1:])
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q; try `arxi-sim help`", args[0])
}

// options are the flags play takes, in one place so that the two paths out of play — a
// real terminal and a pipe — are built from the same values.
//
// alt has no field of its own to read: the alternate screen is what an interactive run
// takes now, so the flag only says the default out loud. inline asks for the old surface
// and is the one that has to be stored.
type options struct {
	speed   float64
	instant bool
	inline  bool
	ascii   bool
	noColor bool
	mouse   bool
	shine   bool
	width   int
	scroll  int
	title   string
	config  string
	anim    ui.Shimmer
}

// band is the light the player sweeps, or the zero shimmer — no light anywhere — when the
// reader turned it off. It is the one place -shine and [anim] are spent, so the flag, the
// file and the default meet once instead of at each of the two bands.
//
// The three numbers are [anim]'s. The key is only a switch: which theme key each band is
// actually drawn in is app's answer, since ui declares exactly two of them and a run that
// could rename them would be a run whose [styles] entries stopped matching. What a reader
// does get to choose is the colour, and [styles] input.shine is where they choose it.
func (o options) band() ui.Shimmer {
	if !o.shine {
		return ui.Shimmer{}
	}
	b := o.anim
	b.Style = ui.InputShine
	return b
}

func play(args []string) error {
	var o options
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	fs.Float64Var(&o.speed, "speed", 1, "divide every recorded delay by this")
	fs.BoolVar(&o.instant, "instant", false, "fold the log and print the last frame")
	fs.BoolVar(&o.inline, "inline", false, "draw under the shell prompt instead of on the alternate screen")
	// -alt is parsed and thrown away. It used to be how you asked for the alternate
	// screen; that is now what an interactive run does, so the flag has nothing left to
	// turn on — but a command line that still names it has to run rather than die on an
	// unknown flag, and dropping it would break every script and shell history that has it.
	_ = fs.Bool("alt", false, "take the alternate screen; the default, and now a no-op")
	fs.BoolVar(&o.ascii, "ascii", false, "fallback glyphs only")
	fs.BoolVar(&o.noColor, "no-color", false, "emit no colour")
	// On by default, and this is the third time this trade has been decided. The wheel and a
	// plain unmodified drag cannot both work on the alternate screen: a terminal sends a notch
	// only to a program holding the mouse, and that same claim takes the drag. It was claimed,
	// then released to buy the drag, and released is what a reader read as a broken program —
	// a dead wheel gives no reason for being dead, while shift+drag is a habit every editor
	// and pager already taught. So the wheel wins by default and this flag is how to run it
	// back. A flag and a setting both, unlike -ascii: which gesture matters more is a taste,
	// and [scroll] mouse = false is how a reader makes that taste stick.
	fs.BoolVar(&o.mouse, "mouse", true, "claim the mouse so the wheel scrolls; selecting text then needs shift")
	// The shine, on by default so that it is seen at all — an animation nobody switches on is
	// an animation nobody reviews. It is the cheapest proof that the render seam is real: two
	// theme keys and four numbers, no new widget and no change to any drawing code.
	fs.BoolVar(&o.shine, "shine", true, "sweep a highlight along the input while it is your turn")
	fs.IntVar(&o.width, "width", 0, "columns for -instant")
	// Zero, not three: the default belongs to the player, so there is one place that
	// answers "how far is a notch" and -h does not have to be kept in step with it.
	fs.IntVar(&o.scroll, "scroll", 0, "lines one notch of the wheel moves")
	// The border's word, empty by default: the marker inside the box already says a prompt
	// goes here. [input] title in a config file sets the same thing, and this flag beats it
	// — the flag was typed for this run and the file was written for every run.
	fs.StringVar(&o.title, "title", "", "a word to let into the top border of the input box")
	// The file the other four tables come from. An empty string here is not the same as no
	// -config at all, which is the whole reason given() exists below: -config "" is the
	// escape hatch that reads nothing, for the reader whose own file is what they are
	// debugging and who needs to see the program without it.
	fs.StringVar(&o.config, "config", "", "read settings from this file instead of the default one")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("play takes exactly one scenario file")
	}

	// The config is read before the recording, because it is the file nobody named: a
	// mistake in the argument you typed is easy to find, and a mistake in a file you wrote
	// last month is the one that wants a line number in front of it.
	f, err := configFor(fs, o.config)
	if err != nil {
		return err
	}
	// A flag beats the file, for the two settings both can set. It has to be asked of the
	// FlagSet rather than of the field: -title "" and no -title at all leave the same empty
	// string behind, and reading the field alone would either lose the flag to the file or
	// lose the file to a default nobody typed.
	if !given(fs, "title") {
		o.title = f.InputTitle
	}
	if !given(fs, "scroll") {
		o.scroll = f.ScrollLines
	}
	// The two booleans a file may also hold. They are pointers there and not bools, because a
	// file that says nothing has to be told apart from a file that says false: a plain bool
	// would let every unconfigured run overwrite a default of true with the zero value.
	if !given(fs, "mouse") && f.Mouse != nil {
		o.mouse = *f.Mouse
	}
	if !given(fs, "shine") && f.Shine != nil {
		o.shine = *f.Shine
	}
	// The shine's numbers, straight from [anim]. A zero in any of them is "unset" and the
	// shimmer fills it with its own default, so there is nothing to check here.
	o.anim = f.Anim

	sc, err := load(fs.Arg(0))
	if err != nil {
		return err
	}
	// The three vocabularies, each built by the package that owns it from the table the file
	// held. A zero config answers with the shipped set for all three, which is why there is
	// one path here and not two — the run with no config file is not a special case of
	// anything, it is this one with empty maps.
	glyphs, err := f.GlyphSet(o.ascii)
	if err != nil {
		return err
	}
	theme, err := f.Theme()
	if err != nil {
		return err
	}
	km, err := f.Keymap()
	if err != nil {
		return err
	}
	// The surface, decided here and nowhere else. An interactive run takes the alternate
	// screen, because that is the only surface a resize cannot corrupt: a terminal reflows
	// the rows it has on the main screen and pushes whatever no longer fits above the top
	// edge into scrollback, where no erase of ours can reach it, and a slow drag does that
	// once per step until the conversation is shredded. Nothing reflows the alternate
	// buffer, so a drag there is one repaint at the new width. -inline keeps the old
	// surface for whoever wants the run under their prompt and can live with that.
	em := &ui.Emitter{Theme: theme, Mode: ui.ModeAlt, Profile: ui.ProfileMono, Mouse: o.mouse}
	if o.inline {
		em.Mode = ui.ModeInline
	}

	if o.instant {
		return fold(sc, em, &glyphs, o)
	}
	tty, err := term.Open()
	if errors.Is(err, term.ErrNotATerminal) {
		// A pipe, a CI job, a `| less`. Nobody is going to answer a question, so the
		// recording's own replies are the answers and the last frame is the whole story.
		return fold(sc, em, &glyphs, o)
	}
	if err != nil {
		return err
	}
	// One terminal wants a different number, and now that there is a terminal open this is
	// where it gets to. The mouse is claimed everywhere, so the reason Termux used to be a
	// special case is gone — but what arrives there is still not a notch. Termux answers a
	// finger with one report per row it has travelled, so three rows a report would scroll a
	// swipe three times too far; 1 makes the page track the hand, and a real wheel notch is
	// three reports there and so still moves three rows.
	//
	// This is a default and not a policy. -scroll is asked for by the flag or by the file —
	// `given` reads the flag, and a file that set it has already left a number here, since the
	// config refuses a zero. So the order that holds everywhere else holds here too: what you
	// typed, then the file, then this, then the shipped 3. A typed -scroll 0 or -2 is left
	// where it lands for the same reason it is on a desktop, which is that main validates none
	// of its numbers and this is not the place to start.
	if term.IsTermux() && !given(fs, "scroll") && o.scroll <= 0 {
		o.scroll = 1
	}
	controller, err := configControllerFor(fs, o, f)
	if err != nil {
		return err
	}
	return live(tty, sc, em, &glyphs, km, controller, o)
}

func configControllerFor(fs *flag.FlagSet, o options, f *config.File) (*config.Controller, error) {
	configPath := config.DefaultPath()
	configEnabled := configPath != ""
	if given(fs, "config") {
		configPath, configEnabled = o.config, o.config != ""
	}
	return config.NewController(config.ControllerOptions{
		Path: configPath, Enabled: configEnabled, File: f, ASCII: o.ascii,
		Runtime: runtimeLabel(o), Title: o.title, ScrollLines: o.scroll, Mouse: o.mouse,
		Shine: o.shine, Anim: o.anim,
		MaskTitle: given(fs, "title"), MaskScroll: given(fs, "scroll"),
		MaskMouse: given(fs, "mouse"), MaskShine: given(fs, "shine"),
	})
}

func runtimeLabel(o options) string {
	surface := "alternate screen"
	if o.inline {
		surface = "inline"
	}
	return surface + " · live preview"
}

// configFor answers with the config a run should use, which is three cases rather than one.
// A -config somebody typed is a request, so a file missing or broken there is an error: the
// alternative is a mistyped path running the program unconfigured and never saying so. No
// -config at all reads the file in the default place when there is one, and having none is
// the ordinary case and not a problem. And -config "" reads nothing, which is the only way
// to see the shipped look on a machine that has a config file.
func configFor(fs *flag.FlagSet, path string) (*config.File, error) {
	if !given(fs, "config") {
		return config.LoadDefault()
	}
	if path == "" {
		return &config.File{}, nil
	}
	return config.Load(path)
}

// given reports whether a flag was typed on this command line. A flag's value cannot answer
// that question — every default is also a value somebody may have typed — and the difference
// is exactly whether the config file gets to fill it in.
func given(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// live plays into a real terminal. The order at the end is the only subtle thing in it:
// the player stops, the emitter's Exit bytes put the cursor and the screen back, and only
// then does the terminal leave raw mode — the other order races the shell's prompt.
//
// The keymap arrives as an argument rather than in options because it is not a flag: it is
// the config's [keys] table already built by the package that owns it, and it comes here
// and not to fold for the same reason WheelLines does — a folded document has no keypress
// to look up.
func live(tty *term.TTY, sc *scenario.Scenario, em *ui.Emitter, g *ui.Glyphs, km *app.Keymap, controller app.ConfigController, o options) error {
	// A panic must not leave a terminal in raw mode with the cursor hidden. Close is
	// idempotent, so the explicit one below is still the one that reports a failure.
	defer tty.Close()
	if err := tty.Raw(); err != nil {
		return err
	}
	// Colour is asked of the environment exactly here. internal/ui may not read it, which
	// is what lets a test pass a Profile instead of setting TERM and hoping.
	if !o.noColor {
		em.Profile = term.DetectProfile()
	}
	w, h := tty.Size()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// ISIG is off in raw mode, so ctrl+c reaches the player as a keypress — where it clears
	// the input line and takes a second press to leave, App.interrupt's job — and a signal
	// arriving here is somebody else killing us rather than the reader asking to stop. The
	// player only ever leaves through an event, so that is what a cancelled context has to
	// become.
	evs := make(chan term.Event, 64)
	go forward(ctx, tty.Events(), evs)

	cfg := app.Config{
		Scenario: sc,
		Events:   evs,
		Out:      tty,
		Emitter:  em,
		Glyphs:   g,
		Keymap:   km,
		Width:    w,
		Height:   h,
		// The size is asked again before every frame, not only when a resize event
		// arrives. A drag re-wraps the terminal's buffer before it tells us, so during a
		// slow drag most frames would otherwise be laid out for a screen that is already
		// gone.
		Size:  tty.Size,
		Speed: o.speed,
		// Only here. A fold has no wheel to turn.
		WheelLines: o.scroll,
		InputTitle: o.title,
		Config:     controller,
		// Only here either, and for the same kind of reason: a fold has no clock to animate
		// against. A folded frame would draw one arbitrary tick of the sweep and call it the
		// document, which is worse than drawing none.
		Shine: o.band(),
	}
	// Still no variation on the keymap by surface, and none by -mouse either: one table
	// serves every run. It used to fork here, because the wheel and the arrows were the same
	// bytes under alternate scroll and only a tracked terminal could tell them apart. Now the
	// arrows are the input's history in every case, and what a notch arrives as is a question
	// about the terminal rather than about the table: with the mouse claimed it is a wheel
	// report, which is the default and the reason the wheel scrolls at all; with -mouse=false
	// it is whatever the terminal decides to send an unclaimed program, and this player then
	// reads it as the arrows it looks like — the history moves and the transcript does not.
	// That is the cost of giving the mouse back, and it is bought outside this table too.
	//
	// Termux is the terminal that shows what the rule is really about. It never consults 1007,
	// so an unclaimed swipe arrives as the arrows themselves and there is no switch to throw;
	// play answers it by claiming the mouse, which is the same answer as everywhere else. A
	// terminal changes whether a notch arrives, and how. It never changes what a key means.

	a := app.New(cfg)
	err := a.Run()
	if _, werr := tty.Write(em.Exit()); err == nil {
		err = werr
	}
	if cerr := tty.Close(); err == nil {
		err = cerr
	}
	return err
}

// forward carries the terminal's events to the player, and turns a cancelled context into
// the one event that ends a run. It never closes the channel it writes to: a closed
// channel is how a terminal says it is gone, and inventing that news would be a lie. The
// goroutine outlives a quit and dies with the process, which is a better trade than a
// second way out of the player.
func forward(ctx context.Context, in <-chan term.Event, out chan<- term.Event) {
	for {
		select {
		case ev := <-in:
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			out <- term.Event{Kind: term.EventClosed}
			return
		}
	}
}

// fold prints what the run ends up looking like. Plain text and not the emitter's bytes:
// this output is for a pipe, a diff or a snapshot, and SGR in a golden file is a diff
// nobody can read. Width 0 means the app's own default, which is eighty columns.
//
// The mode is forced back to inline, whatever the flags said. A fold is a document and not
// a screen — there is no surface to claim, no window to scroll and nothing to put back
// afterwards — and the mode still reaches the layout through the viewport's capabilities,
// so leaving it on alt would quietly give a piped run a different shape from the one it has
// always had.
//
// The title comes through because a fold is what a pipe and a golden file see: a config that
// set one and could not be seen setting it on the only path with text output would be a
// setting nobody could check.
func fold(sc *scenario.Scenario, em *ui.Emitter, g *ui.Glyphs, o options) error {
	em.Mode = ui.ModeInline
	a := app.New(app.Config{Scenario: sc, Emitter: em, Glyphs: g, Width: o.width, InputTitle: o.title})
	_, err := fmt.Println(a.Fold().Plain())
	return err
}

// load refuses to play a recording that does not validate, and reports every problem in
// it at once: a file with three mistakes should cost one run to fix, not three.
func load(path string) (*scenario.Scenario, error) {
	sc, err := scenario.Load(path)
	if err != nil {
		return nil, err
	}
	if errs := scenario.Validate(sc); len(errs) > 0 {
		return nil, fmt.Errorf("%s does not validate:\n%s", path, indent(errs))
	}
	return sc, nil
}

func indent(errs []error) string {
	lines := make([]string, len(errs))
	for i, e := range errs {
		lines[i] = "  " + e.Error()
	}
	return strings.Join(lines, "\n")
}

// keys prints every name a config is allowed to use. This is the point of declaring the
// tables instead of scattering literals: the program can hand the user the whole
// vocabulary, so customizing does not mean reading the source.
//
// The first two columns are measured rather than guessed. A hardcoded width is a bet on the
// longest name a table will ever hold, and that bet was already lost: shift+wheeldown and
// jump-next-message are both wider than the widths this used to pad to, so the rows carrying
// them shouldered the following column out of line and the table stopped being one. The other
// three tables still pad to a constant, because nothing in them is close to overflowing and a
// quoted glyph is not measured by len — when one of them does overflow, widen it the same way.
func keys() error {
	km := app.DefaultKeymap()
	binds := km.Bindings()
	keyW, actW := 0, 0
	for _, b := range binds {
		keyW, actW = max(keyW, len(b.Key)), max(actW, len(b.Action))
	}
	// The shipped table is what is printed, not the one a config file would build. This
	// command answers "what may I write", and a reader whose file is already in place asks
	// the other question — what did it do — with check.
	fmt.Println("bindings — [keys] in a config, key = action; shipped, before any file")

	for _, b := range binds {
		fmt.Printf("  %-*s %-*s %s\n", keyW, b.Key, actW, b.Action, b.Doc)
	}
	docs := app.ActionDocs()
	actW = 0
	for _, d := range docs {
		actW = max(actW, len(d.Action))
	}
	fmt.Println("\nactions — every name a binding may name; \"\" removes a default")
	for _, d := range docs {
		fmt.Printf("  %-*s %s\n", actW, d.Action, d.Doc)
	}
	fmt.Println("\nglyphs — [glyphs], shown as default / --ascii fallback")
	for _, g := range ui.GlyphDocs() {
		fmt.Printf("  %-18s %-6q %-6q %s\n", g.Key, g.Default, g.Fallback, g.Doc)
	}
	fmt.Println("\nstyles — [styles], one colour and attribute set each")
	for _, k := range ui.Keys {
		fmt.Printf("  %-24s %s\n", k.Key, k.Doc)
	}
	fmt.Println("\nslots — where a widget may ask to be drawn")
	for _, s := range ui.SlotKeys {
		fmt.Printf("  %-10s %s\n", s.Slot, s.Doc)
	}
	return nil
}

// check loads and validates without playing, which is what a recording gets edited under —
// and a config, which needs it more: a recording that is wrong shows you a wrong frame, and a
// config that is wrong shows you the frame you already had. A misspelt style key is a colour
// that never arrives, and nothing on the screen says why.
//
// A .toml is read as a config and everything else as a recording. The extension is the whole
// rule, and it is a rule rather than a sniff because this file is the impure half of the
// program: guessing at a format belongs in a reader, and both readers are one call away.
func check(paths []string) error {
	if len(paths) == 0 {
		return errors.New("check takes at least one scenario or config file")
	}
	bad := 0
	for _, path := range paths {
		var err error
		if strings.EqualFold(filepath.Ext(path), ".toml") {
			err = checkConfig(path)
		} else {
			err = checkScenario(path)
		}
		if err != nil {
			bad++
			fmt.Println(err)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d of %d files did not load", bad, len(paths))
	}
	return nil
}

func checkScenario(path string) error {
	sc, err := load(path)
	if err != nil {
		return err
	}
	fmt.Printf("%s: ok — %d steps, %s of recorded time\n", path, len(sc.Steps), sc.Duration())
	return nil
}

// checkConfig prints the counts and not the tables. The question a reader has in front of an
// editor is whether the file the program read is the file they have been editing, and one line
// of counts and settings answers it; the file itself is on their screen already. A table the
// file was silent about is missing from that line rather than printed as a zero, so what comes
// back is the shape of what they wrote.
//
// Its errors go out as they come, without the indent load() gives a recording's: every one of
// them already begins with path:line:, which is the shape an editor jumps to, and two spaces
// in front of it is exactly what stops it being one.
func checkConfig(path string) error {
	f, err := config.Load(path)
	if err != nil {
		return err
	}
	fmt.Printf("%s: ok — %s\n", path, f.Summary())
	return nil
}
