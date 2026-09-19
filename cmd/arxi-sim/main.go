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
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"arxi.local/sim/internal/app"
	"arxi.local/sim/internal/config"
	"arxi.local/sim/internal/ext"
	"arxi.local/sim/internal/ext/install"
	"arxi.local/sim/internal/ext/orchestrator"
	"arxi.local/sim/internal/scenario"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

const usage = `arxi-sim plays a recorded arxi run.

usage:
  arxi-sim play [flags] <scenario.ndjson>
  arxi-sim keys
  arxi-sim check <scenario.ndjson|config.toml>...
  arxi-sim extensions list [--config <config.toml>]
  arxi-sim extensions install <directory> [--yes] [--config <config.toml>]

extensions list reads configuration without starting extension processes. install
preflights and copies an immutable local package, then registers it enabled but
with no runtime capability grants; the first run asks for consent.

play flags, which go before the file because flag parsing stops at the first
argument that is not one:
  -speed f    divide every recorded delay by f: 2 is twice as fast, 0.5 is half
  -instant    fold the whole log and print the last frame as text, no waiting
  -inline     draw under the shell prompt instead of on the alternate screen;
              scroll and resize are the terminal's problem, and a terminal that
              reflows will garble the conversation when the window is dragged.
              On Termux it also streams the conversation into the terminal's
              own history as it goes: see On Android below
  -alt        take the alternate screen, which is the default everywhere. The
              flag is accepted so an old command line keeps working
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
              gives the mouse back to the terminal and a plain drag selects
              again. Off by default on Termux, where the claim would take the
              tap that reopens the keyboard; see On Android below
  -shine      sweep a highlight along the input while it is your turn, and along
              the word "working" while it is not. On by default; -shine=false
              stops it, and so does [anim] shine = false
  -title s    a word to let into the top border of the input box; empty by
              default, which draws an unbroken rule
  -config p   read settings from p instead of the file in the default place;
              -config "" reads none at all
  -theme s    wear the theme pack named s, from the themes dir beside the
              config file; -theme "" wears none, whatever [ui] says. A pack
              carries taste only — [glyphs], [styles] and [anim] — and layers
              under your own file: per key, what your config says wins and the
              pack fills in the rest, so partial packs are legal. A pack that
              is missing or broken is an error and never a silent return to
              the shipped look

An interactive run takes the alternate screen, which is the surface a resize
cannot corrupt: no terminal reflows it, so a drag is one repaint at the new
width. It is printed onto the main screen in one piece when the session ends —
nothing reaches your history before then, and nothing of your own is erased.
Termux runs the same surface, with the mouse left to the terminal so a tap
still reaches the keyboard. See On Android below.

The keys are the ones a hand already knows. Enter sends the line and ctrl+enter
starts a second one inside the input; shift+enter does the same on a terminal
that can tell the two chords apart. Plain up and down are the input's history, as
in a shell — on a phone with the mouse released they scroll the conversation
instead, one row a report, and ctrl+p/ctrl+n keep the history — and the
conversation moves under the keyboard: ctrl+up/down by three
lines, alt+up/down by one, pgup/pgdown a screen, ctrl+home/ctrl+end to the ends,
and shift+up/down jump between your own messages, which is the landmark a long
conversation is actually searched by. They work on a phone too, where the
extra-keys row's arrows and a released swipe both scroll; see On Android below.

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

On Android the mouse is decided the other way, and the surface used to be
with it. With tracking claimed, Termux hands a tap to the program as a mouse
report instead of using it to show the keyboard, and no sequence can ask
Android for the keyboard back — so a Termux run releases the mouse by default
on either surface, and a tap is a tap. The default surface is the alternate
screen like anywhere else, because the main screen was the phone's default
and lost it to ctrl+o: the key re-wraps the whole transcript, rows already
written to history cannot be re-drawn to match, and the screen stood split —
the old level above the window, the new one below — while the rewrite also
showed in pieces, the frame's atomicity being synchronized output (?2026),
which Termux does not honour. The alternate screen commits nothing before
the handover and repaints every row in place, so the same keypress draws
clean there. What it gives up it takes back on the keys: an unclaimed swipe on
a terminal with no 1007 arrives as the plain arrows, and while the mouse is
released those arrows scroll the conversation, one row a report, with the
input history kept on ctrl+p and ctrl+n. -mouse takes the swipe as a tracked
wheel at the tap's price and hands the arrows back to the history. -inline
still takes the main screen, where the conversation streams
into history as it goes and the swipe is the scroll — an arrangement that
holds while the width holds still, because turning the phone re-wraps what
history already holds, and history is not repairable. -scroll defaults to 1
either way, where a tracked swipe reports one row at a time rather than in
notches.

Settings live in a file where a flag would not be enough: eight flat tables —
[keys], [glyphs], [styles], [input], [scroll], [anim], [ui] and [layout] — one
key = "value" a line, renaming a binding, redrawing a marker, recolouring a
span, setting a number, naming a theme pack or reordering the chrome. It is
read from ~/.config/arxi-sim/config.toml, or %AppData%\arxi-sim\config.toml on
Windows, and not having one is the ordinary case rather than an error. A flag
you type beats the file and the file beats the shipped default, so nothing you
asked for on the command line is quietly overridden by something you wrote
last month.

Theme packs live in the themes dir beside the config file — the same portable
path, one level down — and a pack is named by its file's name without .toml.
[layout] rewrites the chrome per slot: above_input = "tasks, recap" orders
those rows above the input, "" leaves a slot empty, and slot@width<60 tiers a
row to narrow terminals. Both vocabularies — widget names and the slots they
ask for — print under the keys command.

keys prints the whole vocabulary such a file may name: bindings, actions,
glyphs, style keys, widget slots and the widget names a [layout] row may list.
check loads and validates without playing, a recording or a config — and a
theme pack, which is a config wearing a role. For a config it prints how much
of each table it read, and if the config names a theme, that pack is resolved
and validated with it, so one check reports both files.

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
	case "extensions":
		return extensionsCommand(args[1:], commandIO{in: os.Stdin, out: os.Stdout, err: os.Stderr})
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q; try `arxi-sim help`", args[0])
}

type commandIO struct {
	in                    io.Reader
	out, err              io.Writer
	configPath, storeRoot string
}

func extensionsCommand(args []string, streams commandIO) error {
	if len(args) == 0 {
		return errors.New("extensions takes list or install")
	}
	switch args[0] {
	case "list":
		return extensionsList(args[1:], streams)
	case "install":
		return extensionsInstall(args[1:], streams)
	default:
		return fmt.Errorf("unknown extensions command %q; want list or install", args[0])
	}
}

func extensionFlags(command string, args []string) (*flag.FlagSet, string, bool, error) {
	fs := flag.NewFlagSet("extensions "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var path string
	var yes bool
	fs.StringVar(&path, "config", "", "config file")
	fs.BoolVar(&yes, "yes", false, "trust and install without prompting")
	if command == "list" {
		fs.BoolVar(&yes, "unused-yes", false, "")
	}
	// Permit the conventional `install directory --yes` spelling while retaining flag's parser.
	ordered := append([]string(nil), args...)
	if command == "install" && len(args) > 1 && !strings.HasPrefix(args[0], "-") {
		ordered = append(append([]string(nil), args[1:]...), args[0])
	}
	if err := fs.Parse(ordered); err != nil {
		return nil, "", false, err
	}
	return fs, path, yes, nil
}

func commandConfigPath(streams commandIO, explicit string, given bool) string {
	if given {
		return explicit
	}
	if streams.configPath != "" {
		return streams.configPath
	}
	return config.DefaultPath()
}

func extensionsList(args []string, streams commandIO) error {
	fs, path, _, err := extensionFlags("list", args)
	if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("extensions list takes no arguments")
	}
	path = commandConfigPath(streams, path, given(fs, "config"))
	if path == "" {
		fmt.Fprintln(streams.out, "No config file; no extensions configured.")
		return nil
	}
	f, err := config.LoadForListing(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(streams.out, "No config file at %s; no extensions configured.\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	if len(f.Extensions) == 0 {
		fmt.Fprintf(streams.out, "No extensions configured in %s.\n", path)
		return nil
	}
	names := make([]string, 0, len(f.Extensions))
	for name := range f.Extensions {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(streams.out, "NAME\tVERSION\tPROTOCOL\tSTATE\tCAPABILITIES\tMANAGEMENT\tMANIFEST")
	for _, name := range names {
		x := f.Extensions[name]
		manifest, loadErr := ext.LoadManifest(x.Manifest)
		state, version, protocol, caps := "invalid", "-", "-", "-"
		if loadErr == nil && manifest.Name == name {
			version, protocol = manifest.Version, manifest.Protocol
			capNames := make([]string, len(manifest.Capabilities))
			for i, capability := range manifest.Capabilities {
				capNames[i] = string(capability)
			}
			sort.Strings(capNames)
			caps = strings.Join(capNames, ",")
			state = extensionState(x, manifest)
		}
		managed := "unmanaged"
		if x.PackageDigest != "" {
			managed = "managed"
		}
		fmt.Fprintf(streams.out, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", name, version, protocol, state, caps, managed, x.Manifest)
	}
	return nil
}

func extensionState(x config.Extension, manifest ext.Manifest) string {
	if !x.Enabled {
		return "disabled"
	}
	packageDigest := ""
	if x.PackageDigest != "" {
		var err error
		packageDigest, err = ext.TreeDigest(filepath.Dir(x.Manifest))
		if err != nil || packageDigest != x.PackageDigest {
			return "content-changed"
		}
	}
	declared := ext.NewCapabilitySet(manifest.Capabilities...)
	if x.Identity != ext.Identity(manifest, packageDigest) || !x.Allow.Equal(declared) {
		return "consent-required"
	}
	return "ready"
}

func extensionsInstall(args []string, streams commandIO) (err error) {
	fs, path, yes, err := extensionFlags("install", args)
	if err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("extensions install takes exactly one directory")
	}
	root := streams.storeRoot
	planOptions := []install.Options(nil)
	if root != "" {
		planOptions = append(planOptions, install.Options{Root: root})
	}
	plan, err := install.Preflight(fs.Arg(0), planOptions...)
	if err != nil {
		return err
	}
	m := plan.Manifest()
	caps := make([]string, len(m.Capabilities))
	for i, c := range m.Capabilities {
		caps[i] = string(c)
	}
	sort.Strings(caps)
	fmt.Fprintf(streams.out, "Source: %s\nDestination: %s\nDigest: %s\nName: %s\nVersion: %s\nProtocol: %s\nExecutable: %s\nCapabilities: %s\n", plan.Source(), plan.Destination(), plan.Digest(), m.Name, m.Version, m.Protocol, m.Executable, strings.Join(caps, ", "))
	fmt.Fprintln(streams.err, "WARNING: This installs native code running as your user. There is NO filesystem or network sandbox. Install only code you trust.")
	if !yes {
		fmt.Fprint(streams.out, "Install this extension? [y/N] ")
		var answer string
		if _, scanErr := fmt.Fscanln(streams.in, &answer); scanErr != nil || (strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes") {
			fmt.Fprintln(streams.out, "Installation declined.")
			return nil
		}
	}
	path = commandConfigPath(streams, path, given(fs, "config"))
	if path == "" {
		return errors.New("no user config path is available")
	}
	if existing, loadErr := config.LoadForListing(path); loadErr == nil {
		if _, collision := existing.Extensions[m.Name]; collision {
			return fmt.Errorf("extension %q is already configured", m.Name)
		}
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return loadErr
	}
	result, err := install.Commit(plan)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, install.Rollback(result))
		}
	}()
	doc, err := config.LoadDocument(path)
	if err != nil {
		return err
	}
	manifestPath := result.ManifestPath
	if rel, relErr := filepath.Rel(filepath.Dir(path), manifestPath); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		manifestPath = rel
	}
	if err = doc.RegisterExtension(m.Name, config.ManagedExtension{Manifest: manifestPath, Enabled: true, PackageDigest: plan.Digest(), Generation: 1}); err != nil {
		return err
	}
	if err = doc.Save(); err != nil {
		return err
	}
	fmt.Fprintf(streams.out, "Installed %s %s. Runtime capabilities remain ungranted until first-run consent.\n", m.Name, m.Version)
	return nil
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
	theme   string
	anim    ui.Shimmer
	layout  []app.LayoutOverride
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
	// -alt used to be how you asked for the alternate screen; then interactive runs took it
	// everywhere and the flag became a no-op kept alive because dropping it would break every
	// script and shell history that names it. Termux gave it work again on 2026-09 — there
	// the default went to the main screen and -alt was the way back — and on 2026-09-12 it
	// became a no-op a second time, because the main screen tore at ctrl+o. inlineFor below
	// is the rule; the reversal and its reasons are in docs/PLAN.md.
	_ = fs.Bool("alt", false, "take the alternate screen; it is the default everywhere and the flag survives for old command lines")
	fs.BoolVar(&o.ascii, "ascii", false, "fallback glyphs only")
	fs.BoolVar(&o.noColor, "no-color", false, "emit no colour")
	// On by default on a desktop, where a dead wheel gives no clue why it is dead and
	// shift+drag is a habit every editor and pager already taught. A phone answers no on
	// every surface — mouseFor below holds the rule, and the reason — because tracking
	// takes the tap Android needs to show its keyboard again. An explicit flag or [scroll]
	// setting wins on either platform.
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
	// The theme pack for this run. Like -config, an empty string is a value and not the
	// absence of one: -theme "" wears none, and is how a reader sees the shipped look
	// without editing the file that names a pack.
	fs.StringVar(&o.theme, "theme", "", "wear this theme pack from the themes dir")
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
	// The platform layers two defaults under the flags and the file, and on a phone they are
	// one trade. With tracking claimed, a tap arrives as a mouse report instead of reaching
	// Termux, and Android's keyboard becomes unreachable — no escape sequence asks for it
	// back. The surface is not the platform's to choose any more. The main screen was the
	// phone default and lost it to ctrl+o, which re-wraps the whole transcript: rows already
	// committed to history kept the level they were drawn at, the screen stood split above
	// and below the window, and the un-atomic repaint — Termux honours no synchronized
	// output — tore while it happened. The alternate screen commits nothing and repaints in
	// place, so it draws clean, and the swipe it gives up arrives as the plain arrows —
	// which platformKeys below then gives to the scroll. These four defaults — surface,
	// mouse, arrows, notch — only hold as a set; "The phone arrangement" in docs/PLAN.md is
	// the checklist, and TestTheTermuxArrangement is the tripwire.
	isTermux := term.IsTermux()
	o.inline = inlineFor(fs, o.inline)
	o.mouse = mouseFor(fs, o.mouse, f.Mouse, isTermux)
	if !given(fs, "shine") && f.Shine != nil {
		o.shine = *f.Shine
	}
	// The theme ladder, spec/look.md's: a flag beats [ui] beats wearing none. -theme ""
	// typed is the second answer and not the first — given() tells them apart, which is
	// why the field alone cannot. A named pack that is missing or broken stops the run
	// here rather than dressing the program in whatever it did not ask for. The dressed
	// file is what the rest of play reads: its [anim], its [layout], its glyph and style
	// tables all answer with the pack's word where the file was silent.
	themeName := f.ThemeName
	if given(fs, "theme") {
		themeName = o.theme
	}
	var pack *config.File
	// raw is the file as it was written, before the pack layered under it. The
	// controller edits and saves that file, so it reads raw and is handed the pack
	// only to attribute what the file was silent about.
	raw := f
	if themeName != "" {
		pack, err = config.LoadTheme(themeName)
		if err != nil {
			return err
		}
		f = f.WithTheme(pack)
	}
	// The shine's numbers and the chrome's layout, straight from the dressed file. A zero
	// in a shimmer field is "unset" and the shimmer fills it with its own default, so
	// there is nothing to check here. The layout has no flag: a composition is a thing
	// you write down and keep, not a thing you type per run.
	o.anim = f.Anim
	o.layout = f.Layout

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
	km, err := keymapFor(f, isTermux, o.mouse)
	if err != nil {
		return err
	}
	// What a run buys with the alternate screen is the surface a resize cannot shred: a
	// terminal reflows the rows it has on the main screen and pushes whatever no longer fits
	// above the top edge into scrollback, where no erase of ours can reach it, so a slow drag
	// shreds the conversation one step at a time, and nothing reflows the alternate buffer.
	// It is also why the phone runs here now: a level change re-wraps rows the main screen
	// has already committed, and history cannot be re-drawn to match. -inline asks for that
	// surface on purpose, knowing the price.
	// The scrollback stream rides on it, on the phone only. On that surface the reader
	// scrolls with a finger, the conversation is written into the terminal's history as the
	// window leaves it behind, and the swipe is the conversation moving. It costs the
	// in-app viewport its freedom — the window stays on the tail, because a row history
	// holds cannot be re-shown — and it stands or falls with the width holding still,
	// which a phone's does unless the device is turned. A desktop -inline keeps the old
	// contract: nothing reaches history until the handover, because a dragged window
	// re-wraps committed rows and history is not repairable.
	em := &ui.Emitter{Theme: theme, Mode: ui.ModeAlt, Profile: ui.ProfileMono, Mouse: o.mouse, Scrollback: o.inline && isTermux}
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
	// A tracked swipe in Termux produces one report per row travelled, so when tracking was
	// explicitly enabled one row per report keeps the page under the hand. The same default is
	// harmless when tracking is off and documents the value that will apply if it is enabled at
	// runtime through the config view.
	//
	// This is a default and not a policy. -scroll is asked for by the flag or by the file —
	// `given` reads the flag, and a file that set it has already left a number here, since the
	// config refuses a zero.
	o.scroll = scrollFor(fs, o.scroll, isTermux)
	controller, err := configControllerFor(fs, o, raw, pack, themeName)
	if err != nil {
		return err
	}
	return live(tty, sc, em, &glyphs, km, controller, o)
}

func configControllerFor(fs *flag.FlagSet, o options, f *config.File, pack *config.File, themeName string) (*config.Controller, error) {
	configPath := config.DefaultPath()
	configEnabled := configPath != ""
	if given(fs, "config") {
		configPath, configEnabled = o.config, o.config != ""
	}
	return config.NewController(config.ControllerOptions{
		Path: configPath, Enabled: configEnabled, File: f, ASCII: o.ascii,
		ThemeName: themeName, Theme: pack,
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

// inlineFor answers "main screen?" from the flags alone. It used to end on the platform: a
// phone took the main screen unasked, because only there does a swipe scroll the
// conversation and a tap reach the keyboard — until ctrl+o showed what that surface costs.
// A level change re-wraps the transcript, the rows history has taken cannot be re-drawn to
// match, and Termux, honouring no synchronized output, displayed the rewrite in pieces; so
// on 2026-09-12 the default went back to the alternate screen everywhere. -inline asks for
// the main screen anywhere, -alt names the default it already is and survives only in the
// flag set, and with neither typed no run takes the main screen.
func inlineFor(fs *flag.FlagSet, flagValue bool) bool {
	if given(fs, "inline") {
		return flagValue
	}
	return false
}

// mouseFor layers the mouse decision the same way: what you typed, then the file, then the
// platform. The desktop answer is the wheel — on the alternate screen a notch exists only
// for a program holding the mouse. The phone answers no on every surface: a claimed tap is
// a report, and a report is a keyboard that never comes back — the alternate screen is
// where the phone first learnt that, back when claiming was its default. What a released
// swipe costs — it arrives as the plain arrows on a terminal with no 1007 — the view keys
// cover, and -mouse says otherwise for a phone with a mouse attached.
func mouseFor(fs *flag.FlagSet, flagValue bool, configured *bool, termux bool) bool {
	if given(fs, "mouse") {
		return flagValue
	}
	if configured != nil {
		return *configured
	}
	return !termux
}

// keymapFor builds the run's keymap the way every other setting is layered: the platform's
// key defaults underneath, the config's [keys] table over them, the shipped table under
// everything. The table in app stays one table for every run — what a platform adds is a
// default here, not a fork there.
func keymapFor(f *config.File, termux, mouse bool) (*app.Keymap, error) {
	platform := platformKeys(termux, mouse)
	overrides := make(map[string]app.Action, len(f.Keys)+len(platform))
	for name, act := range f.Keys {
		overrides[name] = act
	}
	for name, act := range platform {
		if _, set := overrides[name]; !set {
			overrides[name] = act
		}
	}
	return app.NewKeymap(overrides)
}

// platformKeys answers the key defaults a platform adds, and nil where it has none. A phone
// with the mouse released is the one that has any, and it is also the one that needs them:
// Termux synthesizes the plain arrow keys for an unclaimed swipe on the alternate buffer and
// implements no 1007 to switch the synthesis off, so the released swipe arrives as up and
// down — and if those mean "history", as they do everywhere else, the conversation has no
// scroll a finger can reach. Bound to the one-row scroll actions the swipe is the scroll
// again, at the grain the tracked wheel had, and the history keeps ctrl+p and ctrl+n.
// Claiming the mouse with -mouse turns the swipe into wheel reports and hands the arrows
// back to the history. This default is one leg of the phone arrangement — surface, mouse,
// arrows, notch — that only holds as a set; see "The phone arrangement" in docs/PLAN.md,
// and TestTheTermuxArrangement in main_test.go, which fails when the legs drift apart.
func platformKeys(termux, mouse bool) map[string]app.Action {
	if !termux || mouse {
		return nil
	}
	return map[string]app.Action{
		"up":   app.ActionScrollUp,
		"down": app.ActionScrollDown,
	}
}

// scrollFor layers the notch width the way the other two layer their decisions: what you
// typed, then the file, then the platform. A phone's tracked swipe reports one row at a
// time, so one row per notch keeps the page under the hand, and a synthesized arrow arrives
// one at a time the same way; the shipped 3 stands on a desktop. A zero here is not a
// value — the player fills it with its own default.
func scrollFor(fs *flag.FlagSet, scroll int, termux bool) int {
	if given(fs, "scroll") || scroll > 0 {
		return scroll
	}
	if termux {
		return 1
	}
	return scroll
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
func live(tty *term.TTY, sc *scenario.Scenario, em *ui.Emitter, g *ui.Glyphs, km *app.Keymap, controller *config.Controller, o options) error {
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
	manager, err := orchestrator.New(ctx, controller, orchestrator.MinimalEnvironment())
	if err != nil {
		return err
	}
	// Processes must be gone before emitter Exit bytes and raw-mode teardown.
	defer manager.Close()
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
		// The [layout] table, parsed and validated by the config reader. A fold has
		// it too, for the same reason it has the title: a config that reorders the
		// chrome and could not be seen doing it on the only path with text output
		// would be a setting nobody could check.
		Layout:     o.layout,
		Config:     controller,
		Extensions: manager,
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
	// play answers it by releasing the mouse, which keeps the tap and lets the swipe read as
	// what it looks like. A terminal changes whether a notch arrives, and how. It never
	// changes what a key means.

	a := app.New(cfg)
	err = a.Run()
	// Extension shutdown is part of the live surface: complete it before terminal
	// Exit bytes restore the cursor/screen and before raw mode is released.
	if cerr := manager.Close(); err == nil {
		err = cerr
	}
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
	a := app.New(app.Config{Scenario: sc, Emitter: em, Glyphs: g, Width: o.width, InputTitle: o.title, Layout: o.layout})
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
	fmt.Println("\nwidgets — [layout], the names a slot's list may hold, in the order they stack by default")
	for _, w := range app.CompositionWidgets() {
		fmt.Printf("  %-12s %-12s %s\n", w.Name, string(w.Slot), w.Doc)
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
// The file's directory decides its role: a .toml inside the themes dir is read as a theme
// pack — [glyphs], [styles] and [anim] and nothing else — and any other .toml as an ordinary
// config. A config that names a theme gets that pack resolved and validated with it, so one
// check reports both files; a pack that is missing or broken fails the check of the config
// that named it, which is where the reader will be looking when it matters.
//
// Its errors go out as they come, without the indent load() gives a recording's: every one of
// them already begins with path:line:, which is the shape an editor jumps to, and two spaces
// in front of it is exactly what stops it being one.
func checkConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var f *config.File
	if inThemesDir(path) {
		f, err = config.ParseTheme(path, data)
	} else {
		f, err = config.Parse(path, data)
	}
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s: ok — %s", path, f.Summary())
	if !inThemesDir(path) && f.ThemeName != "" {
		pack, err := config.LoadTheme(f.ThemeName)
		if err != nil {
			return err
		}
		line += fmt.Sprintf(" — theme %s: ok — %s", strconv.Quote(f.ThemeName), pack.Summary())
	}
	fmt.Println(line)
	return nil
}

// inThemesDir answers whether a path is a theme pack's home: its directory, resolved,
// is the themes dir. It is a location question and nothing else — the same file
// elsewhere is an ordinary config, because a role follows the address and not the
// content, which is what keeps `check` from guessing at formats.
func inThemesDir(path string) bool {
	dir := config.ThemesDir()
	if dir == "" {
		return false
	}
	abs, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return strings.EqualFold(abs, absDir)
}
