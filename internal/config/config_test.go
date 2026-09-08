package config_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"arxi.local/sim/internal/app"
	"arxi.local/sim/internal/config"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

// A config file is read once, by hand, from a text a person wrote in an editor with no
// checker in it. So the tests here are mostly about the second half of that sentence: what
// the reader does with a file that is wrong, and whether the line it names is the line the
// mistake is on. The one accepted fixture below carries every section at once, because a
// config that only ever gets tested a section at a time is a config nobody has read whole.

// whole is a plausible file rather than a minimal one: comments in three positions, blank
// lines between the tables, both spellings of a key on the left, a removal, an escape, a
// # inside a quoted value and another in a bare one, and one name written in two sections —
// status.spinner is a glyph and a style, and it is two settings and not a key set twice.
//
// The last two tables are the settings whose shipped answer is yes, written here as the no
// and the slower yes: they are the only two in the format that need a File field to tell
// "unset" from "false", so a fixture that left them out would be a fixture that never
// distinguished them.
const whole = `# arxi-sim, as one reader has it.

[keys]
ctrl+up      = scroll-up-fast      # the wheel's notch, without the wheel
alt+ctrl+x   = kill-line           # written the other way round on purpose
"shift+down" = jump-next-message   # the quoted spelling a real parser would want
esc          = ""                  # stop cancelling the line

[glyphs]                           # the third place a comment goes: after a header
prompt.marker   = "» "             # the trailing space is why quotes are here
thinking.marker = "✻ "             # the star, back again
status.spinner  = "|/-\\"          # the ascii cycle, one escape in it

[styles]
prompt.text    = fg=red
md.code        = fg=#c8d3f5 bold      # a # in the middle of a word is not a comment
prompt.band    = ""                   # no wash behind my own turns
status.spinner = fg=bright-black dim  # a glyph key as well, and a different setting

[input]
title = "prompt # 1"               # and inside quotes a # is a #

[scroll]
lines = 5
mouse = false                      # let the terminal keep the wheel and its own selection

[anim]
shine  = true                      # said aloud, which is what a reader who turned it off does
period = 48                        # the sweep, slower and further apart than we ship it
travel = 24
width  = 12
`

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadReadsEverySection is the acceptance half, and it checks the tables rather than the
// objects built from them: what a reader gets wrong about this format is which characters
// survive the trip, and a keymap answers that question one binding at a time.
func TestLoadReadsEverySection(t *testing.T) {
	f, err := config.Load(write(t, whole))
	if err != nil {
		t.Fatal(err)
	}
	// alt+ctrl+x is filed under ctrl+alt+x: the key is stored the way term prints it, which
	// is the only spelling app.NewKeymap will look up.
	wantKeys := map[string]app.Action{
		"ctrl+up":    app.ActionScrollUpFast,
		"ctrl+alt+x": app.ActionKillLine,
		"shift+down": app.ActionJumpNextMessage,
		"esc":        app.ActionNone,
	}
	if !maps.Equal(f.Keys, wantKeys) {
		t.Errorf("[keys] = %v, want %v", f.Keys, wantKeys)
	}
	// The spinner is the reason the escape exists: |/-\ ends in a backslash, so the file has
	// to write \\ and get one back.
	wantGlyphs := map[string]string{
		"prompt.marker":   "» ",
		"thinking.marker": "✻ ",
		"status.spinner":  `|/-\`,
	}
	if !maps.Equal(f.Glyphs, wantGlyphs) {
		t.Errorf("[glyphs] = %q, want %q", f.Glyphs, wantGlyphs)
	}
	// md.code carries a # in the middle of a word, which is a colour and not a comment,
	// prompt.band is the empty style — the spelling ui.Keys documents for taking a wash off —
	// and status.spinner is the name [glyphs] also used, which is why the reader remembers a
	// key under the section it was written in.
	wantStyles := map[string]ui.Style{
		"prompt.text":    {FG: ui.Idx(ui.Red)},
		"md.code":        {FG: ui.MustHex("#c8d3f5"), Attrs: ui.AttrBold},
		"prompt.band":    {},
		"status.spinner": {FG: ui.Idx(ui.Bright + ui.Black), Attrs: ui.AttrDim},
	}
	if !maps.Equal(f.Styles, wantStyles) {
		t.Errorf("[styles] = %v, want %v", f.Styles, wantStyles)
	}
	if got, want := f.InputTitle, "prompt # 1"; got != want {
		t.Errorf("[input] title = %q, want %q", got, want)
	}
	if got, want := f.ScrollLines, 5; got != want {
		t.Errorf("[scroll] lines = %d, want %d", got, want)
	}
	// The two pointers, read as three states and not two: nil is the file saying nothing, and
	// main keeps whatever the flag left. A plain bool would answer "false" to both, which is
	// how a file that never mentioned the mouse would take it away.
	if f.Mouse == nil || *f.Mouse {
		t.Errorf("[scroll] mouse = %v, want a false the file set", f.Mouse)
	}
	if f.Shine == nil || !*f.Shine {
		t.Errorf("[anim] shine = %v, want a true the file set", f.Shine)
	}
	// Style is absent on purpose: which key the band is drawn in is app's answer, so a file
	// that could name one would be a file that could stop [styles] input.shine from matching.
	if got, want := f.Anim, (ui.Shimmer{Period: 48, Travel: 24, Width: 12}); got != want {
		t.Errorf("[anim] = %+v, want %+v", got, want)
	}
	if f.Path == "" {
		t.Error("a loaded config has no Path")
	}
}

// TestFileBuildsTheThreeObjects is the other half. The tables above are only worth checking
// because something is built from them, and what has to be true of that something is narrow:
// the keys the file named changed, and nothing else did.
func TestFileBuildsTheThreeObjects(t *testing.T) {
	f, err := config.Load(write(t, whole))
	if err != nil {
		t.Fatal(err)
	}
	km, err := f.Keymap()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		key  string
		want app.Action
	}{
		// Two additions, and the second of them proves the canonical spelling is what got
		// stored: the file wrote alt+ctrl+x and the lookup here asks both ways round.
		{"ctrl+alt+x", app.ActionKillLine},
		{"alt+ctrl+x", app.ActionKillLine},
		{"shift+down", app.ActionJumpNextMessage},
		// A line that agrees with the default. It has to still hold afterwards, which is the
		// difference between overrides laid on top of the shipped table and overrides that
		// replace it.
		{"ctrl+up", app.ActionScrollUpFast},
		// The removal. esc is cancel out of the box, so "" is visible here or nowhere.
		{"esc", app.ActionNone},
		// And a binding the file never mentioned.
		{"ctrl+c", app.ActionInterrupt},
	} {
		k, ok := term.ParseKey(c.key)
		if !ok {
			t.Fatalf("%s is not a key name", c.key)
		}
		if got := km.Lookup(k); got != c.want {
			t.Errorf("%s = %q, want %q", c.key, got, c.want)
		}
	}
	g, err := f.GlyphSet(false)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := g.Get("status.spinner"), `|/-\`; got != want {
		t.Errorf("status.spinner = %q, want %q", got, want)
	}
	def := ui.DefaultGlyphs()
	for _, d := range ui.GlyphKeys {
		if _, named := f.Glyphs[d.Key]; named {
			continue
		}
		if got, want := g.Get(d.Key), def.Get(d.Key); got != want {
			t.Errorf("%s = %q, want the shipped %q", d.Key, got, want)
		}
	}
	// -ascii is a fact about the terminal and an override is a request, so the request wins
	// for the three keys it names and the narrow fallbacks stand for the other nineteen.
	// That is what lets somebody who needs -ascii still ask for the one glyph they know
	// their font has.
	a, err := f.GlyphSet(true)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := ui.NewGlyphs(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := a.Get("prompt.marker"), "» "; got != want {
		t.Errorf("with -ascii, prompt.marker = %q, want the file's %q", got, want)
	}
	for _, d := range ui.GlyphKeys {
		if _, named := f.Glyphs[d.Key]; named {
			continue
		}
		if got, want := a.Get(d.Key), plain.Get(d.Key); got != want {
			t.Errorf("with -ascii, %s = %q, want the fallback %q", d.Key, got, want)
		}
	}
	th, err := f.Theme()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := th.Resolve("prompt.text"), (ui.Style{FG: ui.Idx(ui.Red)}); got != want {
		t.Errorf("prompt.text = %+v, want %+v", got, want)
	}
	if got := th.Resolve("prompt.band"); !got.IsZero() {
		t.Errorf(`prompt.band = %+v, want the zero style after ""`, got)
	}
	shipped := ui.DefaultTheme()
	for _, d := range ui.Keys {
		if _, named := f.Styles[d.Key]; named {
			continue
		}
		if got, want := th.Resolve(d.Key), shipped.Resolve(d.Key); got != want {
			t.Errorf("%s = %+v, want the shipped %+v", d.Key, got, want)
		}
	}
}

// TestZeroFileChangesNothing is the sentence main rests on. There is one path through startup
// whether a file was found or not, and the arm with no file has to produce exactly the
// program we ship — otherwise every user without a config is running a fourth configuration
// nobody tests.
func TestZeroFileChangesNothing(t *testing.T) {
	var f config.File
	km, err := f.Keymap()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := km.Bindings(), app.DefaultKeymap().Bindings(); !slices.Equal(got, want) {
		t.Errorf("the zero config's keymap is not the shipped one:\n got %v\nwant %v", got, want)
	}
	// Both arms, because the fallbacks are a whole second glyph set and the zero config is
	// the one -ascii is usually run with.
	for _, ascii := range []bool{false, true} {
		g, err := f.GlyphSet(ascii)
		if err != nil {
			t.Fatal(err)
		}
		want, err := ui.NewGlyphs(nil, ascii)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range ui.GlyphKeys {
			if got, want := g.Get(d.Key), want.Get(d.Key); got != want {
				t.Errorf("ascii=%v: %s = %q, want %q", ascii, d.Key, got, want)
			}
		}
	}
	th, err := f.Theme()
	if err != nil {
		t.Fatal(err)
	}
	shipped := ui.DefaultTheme()
	for _, d := range ui.Keys {
		if got, want := th.Resolve(d.Key), shipped.Resolve(d.Key); got != want {
			t.Errorf("%s = %+v, want the shipped %+v", d.Key, got, want)
		}
	}
	if got, want := f.Summary(), "[keys] 0, [glyphs] 0, [styles] 0"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

// TestSummaryIsWhatCheckPrints pins the single line `arxi-sim check` gives a reader. The
// question behind that line is whether the file the program read is the file they have been
// editing, and three counts and the settings' own values are the cheapest thing that can
// answer no.
//
// The order is the format's and not the struct's, and it is worth pinning: a reader compares
// this line against the file in their editor from the top down, so the tables have to come out
// in the order they wrote them.
func TestSummaryIsWhatCheckPrints(t *testing.T) {
	f, err := config.Load(write(t, whole))
	if err != nil {
		t.Fatal(err)
	}
	want := `[keys] 4, [glyphs] 3, [styles] 4, [input] title="prompt # 1", ` +
		`[scroll] lines=5, mouse=false, [anim] shine=true, period=48, travel=24, width=12`
	if got := f.Summary(); got != want {
		t.Errorf("Summary() = %q\n              want %q", got, want)
	}
	// A section the file was silent about contributes nothing at all, which is the property that
	// makes the line worth reading: every setting on it came out of the file. One number is
	// enough to show it, and [anim] is the section with four ways to be partly set.
	f, err = config.Load(write(t, "[anim]\ntravel = 24\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.Summary(), "[keys] 0, [glyphs] 0, [styles] 0, [anim] travel=24"; got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

// TestValueSpellings is the quoting rules as a person meets them. [input] title carries them
// because it is the one setting whose value is an arbitrary string: a glyph has to be a
// declared name and a style has to parse, and neither is what these lines are about.
func TestValueSpellings(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{`title = prompt`, "prompt"},
		{`title = "prompt"`, "prompt"},
		{`title = "prompt "`, "prompt "}, // a trailing space is the whole reason for quotes
		{`title = two words`, "two words"},
		{`title = prompt   # a comment`, "prompt"},
		{`title = "prompt"  # or after a quoted one`, "prompt"},
		{`title = "a # b"`, "a # b"}, // and inside quotes a # is a #
		{`title = "say \"go\""`, `say "go"`},
		{`title = "back\\slash"`, `back\slash`},
		{"title =\tprompt\t", "prompt"}, // a tab is a space
		{`title="prompt"`, "prompt"},    // and no space at all is still a setting
		{`"title" = prompt`, "prompt"},  // the quoted spelling on the left
	} {
		f, err := config.Load(write(t, "[input]\n"+c.line+"\n"))
		if err != nil {
			t.Errorf("%s: %v", c.line, err)
			continue
		}
		if f.InputTitle != c.want {
			t.Errorf("%s gave %q, want %q", c.line, f.InputTitle, c.want)
		}
	}
}

// TestLoadRejects is the breadth. Every one of these is a file somebody will write, and the
// only thing asserted is that none of them loads: a config that is silently half-applied is
// the failure this whole package exists to prevent. What the messages say is pinned by the two
// tests after this one, which is where a line number is worth checking.
func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"a setting before any header":     "up = quit",
		"a section nobody reads":          "[keyz]\nup = quit",
		"a header that never closes":      "[keys\nup = quit",
		"a header with a word after it":   "[keys] extra\nup = quit",
		"a header with no name":           "[]\nup = quit",
		"a line that is not a setting":    "[keys]\nup quit",
		"a setting with no name":          "[keys]\n = quit",
		"a key that is not a key":         "[keys]\nnope+up = quit",
		"an action that does not exist":   "[keys]\nup = fly",
		"one key set twice":               "[keys]\nup = quit\nup = cancel",
		"one key under two spellings":     "[keys]\nctrl+alt+x = quit\nalt+ctrl+x = cancel",
		"a glyph nobody draws":            "[glyphs]\nprompt.marker.big = \">\"",
		"a style key with a typo in it":   "[styles]\nprompt.txt = fg=red",
		"two foregrounds for one span":    "[styles]\nprompt.text = fg=red fg=blue",
		"a colour with no palette entry":  "[styles]\nprompt.text = fg=orange",
		"an attribute that is a prefix":   "[styles]\nprompt.text = bol",
		"a setting [input] does not have": "[input]\nname = \"prompt\"",
		"a setting [scroll] lacks":        "[scroll]\nrows = 3",
		"a scroll of no rows at all":      "[scroll]\nlines = 0",
		"a scroll that goes backwards":    "[scroll]\nlines = -2",
		"a scroll measured in words":      "[scroll]\nlines = many",
		"a mouse that is a number":        "[scroll]\nmouse = 1",
		"a setting [anim] lacks":          "[anim]\nspeed = 3",
		"a shine that is yes":             "[anim]\nshine = yes",
		"a sweep of no ticks at all":      "[anim]\nperiod = 0",
		"a band of negative width":        "[anim]\nwidth = -4",
		"a travel measured in seconds":    "[anim]\ntravel = 2s",
		"a setting with no value":         "[scroll]\nlines =",
		"a value that is all comment":     "[input]\ntitle = # nothing",
		"a quote that never closes":       "[input]\ntitle = \"prompt",
		"a word after the closing quote":  "[input]\ntitle = \"prompt\" and more",
		"an escape that means nothing":    "[input]\ntitle = \"a\\nb\"",
		"a backslash at the end":          "[input]\ntitle = \"a\\",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if f, err := config.Load(write(t, body+"\n")); err == nil {
				t.Fatalf("accepted as %s", f.Summary())
			}
		})
	}
}

// TestErrorNamesTheLineAndTheFile is why the reader repeats a membership check that three
// constructors already do. A config is edited in a text editor, and "prompt.txt is not a style
// key" with no line on it sends a reader looking through a file for a word they cannot see.
func TestErrorNamesTheLineAndTheFile(t *testing.T) {
	for _, c := range []struct {
		body string
		line int
	}{
		{"[keys]\nup = fly\n", 2},
		// After a comment and two blank lines, because a reader counts lines the way their
		// editor does and all of those are lines.
		{"# a note\n\n[keys]\n\nup = fly\n", 5},
		// And in the third table down, which is where the number stops being guessable.
		{"[keys]\nup = quit\n\n[glyphs]\nprompt.marker = \">\"\n\n[scroll]\nlines = 0\n", 8},
		// One case per check the reader repeats, because the line number is the whole of what
		// repeating it buys. Every name below would be refused a second time by the constructor
		// build() runs — and that refusal arrives with no line on it, which is a message about a
		// word in a file the reader then has to search for. Dropping any one of these checks
		// leaves the file rejected either way, so this table is the only thing that would notice.
		{"[keys]\nnope+up = quit\n", 2},
		{"[glyphs]\nprompt.marker.big = \">\"\n", 2},
		{"[styles]\nprompt.txt = fg=red\n", 2},
		// The style's value and not its name: ui.ParseStyle is the reader's, and a line number is
		// the only reason it runs here instead of inside DefaultThemeWith.
		{"[styles]\nprompt.text = fg=red fg=blue\n", 2},
		// [input] has no constructor behind it at all, so this line is refused here or nowhere.
		{"[input]\nname = \"prompt\"\n", 2},
		// A header is a whole table's worth of settings rather than one, and the line to fix is
		// the header's.
		{"# a note\n\n[keyz]\nup = quit\n", 3},
		// And the second of two lines that set one binding under two spellings, which is the
		// line a reader deletes.
		{"[keys]\nup = quit\n\nctrl+alt+x = quit\nalt+ctrl+x = cancel\n", 5},
	} {
		path := write(t, c.body)
		_, err := config.Load(path)
		if err == nil {
			t.Errorf("%q was accepted", c.body)
			continue
		}
		if want := fmt.Sprintf("%s:%d:", path, c.line); !strings.Contains(err.Error(), want) {
			t.Errorf("%q\n gave %v\nwant a problem at %s", c.body, err, want)
		}
	}
}

// TestLoadReportsEveryProblemAtOnce is internal/scenario's rule kept here and for its reason:
// a file with three typos in it should cost one run to fix, not three.
func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	path := write(t, "[keys]\nup = fly\nnope = quit\n\n[scroll]\nlines = 0\n")
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("a file with three mistakes in it was accepted")
	}
	for _, line := range []int{2, 3, 6} {
		if want := fmt.Sprintf("%s:%d:", path, line); !strings.Contains(err.Error(), want) {
			t.Errorf("nothing reported at %s:\n%v", want, err)
		}
	}
}

// TestOneMistakeIsReportedOnce is the same rule read the other way, and it is why the reader
// has a skip field. A file whose [keys] header was deleted is one mistake and a page of
// settings that were fine, and complaining once per line buries the sentence that says so.
func TestOneMistakeIsReportedOnce(t *testing.T) {
	for name, body := range map[string]string{
		"no header at all":      "up = quit\ndown = cancel\nesc = quit\n",
		"a header nobody reads": "[keyz]\nup = quit\ndown = cancel\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Load(write(t, body))
			if err == nil {
				t.Fatal("accepted")
			}
			joined, ok := err.(interface{ Unwrap() []error })
			if !ok {
				return // one problem, not worth joining: the shape this test wants
			}
			if n := len(joined.Unwrap()); n != 1 {
				t.Errorf("reported %d problems, want 1:\n%v", n, err)
			}
		})
	}
}

// TestLoadDefaultCoversTheThreeStatesMainCanStartIn moves the config directory rather than
// trusting the real one, and skips rather than lying when the platform puts that directory
// somewhere t.Setenv cannot reach — on darwin it is Application Support and neither variable
// below is read.
func TestLoadDefaultCoversTheThreeStatesMainCanStartIn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // linux
	t.Setenv("AppData", dir)         // windows
	path := config.DefaultPath()
	if !strings.HasPrefix(path, dir) {
		t.Skipf("os.UserConfigDir ignores the environment here: %s", path)
	}
	// No file. The normal case, and it cannot be an error.
	f, err := config.LoadDefault()
	if err != nil {
		t.Fatalf("having no config file is not an error: %v", err)
	}
	if f.Path != "" || f.Summary() != "[keys] 0, [glyphs] 0, [styles] 0" {
		t.Errorf("with no file, LoadDefault gave %s from %q", f.Summary(), f.Path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// A file, in the place the program promises to look.
	if err := os.WriteFile(path, []byte(whole), 0o600); err != nil {
		t.Fatal(err)
	}
	if f, err = config.LoadDefault(); err != nil {
		t.Fatal(err)
	}
	if f.Path != path || f.ScrollLines != 5 {
		t.Errorf("LoadDefault read %q with lines=%d, want %q with 5", f.Path, f.ScrollLines, path)
	}
	// And a file that does not load, which has to be an error rather than a config the program
	// steps over: the alternative is a reader whose colour never arrived and no sentence
	// anywhere saying why.
	if err := os.WriteFile(path, []byte("[keys]\nup = fly\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadDefault(); err == nil {
		t.Error("a broken config in the default place was ignored")
	}
}

// Load is the other door, and a path a reader named is a request. A missing file is an error
// here, because the alternative is a mistyped -config that runs the program unconfigured.
func TestLoadOfAMissingFileIsAnError(t *testing.T) {
	if _, err := config.Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("a path that does not exist was accepted")
	}
}
