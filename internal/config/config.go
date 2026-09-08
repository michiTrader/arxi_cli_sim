// Package config reads the file the program has been advertising: six flat tables that
// rename a key, redraw a glyph, recolour a span, set a number or turn an animation off.
//
// It is not a TOML implementation, and the .toml name is a promise about the shape rather
// than the grammar: [section] headers, one `key = "value"` per line, # comments, and
// nothing else — no arrays, no nesting, no dates, no multi-line strings. A file this reader
// accepts is one a real TOML parser would accept too, with a single exception kept on
// purpose: a bare key on the left. `ctrl+up = "scroll-up-fast"` reads better than
// `"ctrl+up" = "scroll-up-fast"`, and + is not a character TOML allows in a bare key, so
// the quoted spelling is the one that stays valid if this file ever meets a real parser and
// both are accepted here.
//
// The vocabularies are not in this package. Every name a config may write is declared by
// whoever owns it — app.ActionKeys, ui.GlyphKeys, ui.Keys, term.ParseKey — and this reader
// checks membership against those tables and then hands the maps to their real
// constructors, which keep the last word. The check is duplicated so that an error can name
// a line number. The lists never are.
//
// Every problem in a file is reported rather than the first, which is the rule
// internal/scenario keeps and for the same reason: a config with three typos in it should
// cost one run to fix, not three.
package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arxi.local/sim/internal/app"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

// SectionDecl is one table a config may hold, and the line `arxi-sim keys` prints for it.
type SectionDecl struct {
	Name string
	Doc  string
}

// Sections is every header this reader understands. A header outside it is an error and not
// something skipped: a section nobody reads is a page of settings that never took effect,
// and silence is how the reader fails to find that out.
var Sections = []SectionDecl{
	{"keys", `a binding each: key = action, and key = "" to take a default away`},
	{"glyphs", `a marker each: glyph = the string to draw, quoted so a trailing space survives`},
	{"styles", "a colour each: style key = " + ui.StyleSyntax()},
	{"input", `title = a word for the input box's top border`},
	{"scroll", "lines = how far one wheel notch moves the conversation; mouse = whether to claim it"},
	{"anim", "shine = whether the highlight sweeps at all, then period, travel and width in ticks"},
}

func sectionList() string {
	out := make([]string, len(Sections))
	for i, s := range Sections {
		out[i] = "[" + s.Name + "]"
	}
	return strings.Join(out, ", ")
}

// File is a config as read: the three vocabulary tables, the settings the other three
// sections hold, and where they came from.
//
// A zero File is a valid config that changes nothing. That is the point of it — the program
// builds its keymap, glyphs and theme through the three methods below whether a file was
// found or not, so there is one path through main and not two.
type File struct {
	Path string // "" when nothing was read

	Keys   map[string]app.Action // canonical key name -> action; "" removes a binding
	Glyphs map[string]string     // glyph key -> the string to draw
	Styles map[string]ui.Style   // style key -> the whole style for it

	// InputTitle is both "unset" and "no title" when empty, which costs nothing because
	// they draw the same border. ScrollLines is 0 when unset, and the player's own
	// defaultWheelLines stands.
	InputTitle  string
	ScrollLines int

	// The two settings whose shipped default is true, and therefore the two that cannot be
	// plain bools: a file that says nothing has to be told apart from a file that says
	// false, and a bool answers "false" to both. A nil here means main keeps whatever the
	// flag left, which is how `a flag beats the file beats the default` stays one rule
	// rather than three special cases.
	Mouse *bool
	Shine *bool

	// Anim is the shine's shape — period, travel and width — with a zero in any field
	// meaning "unset", which is the same word ui.Shimmer already uses for it. Style is
	// deliberately not read from the file: which theme key the band is drawn in is the
	// program's business, and the colour is what [styles] input.shine is for.
	Anim ui.Shimmer
}

// Keymap is the shipped bindings with this file's laid on top.
func (f *File) Keymap() (*app.Keymap, error) { return app.NewKeymap(f.Keys) }

// GlyphSet is the glyphs a run draws. ascii stays a flag rather than becoming a setting: it
// is a fact about the terminal in front of the user and not about their taste. An override
// in [glyphs] wins over it either way, which is what lets somebody who needs -ascii still
// ask for the one glyph they know their terminal can draw.
func (f *File) GlyphSet(ascii bool) (ui.Glyphs, error) { return ui.NewGlyphs(f.Glyphs, ascii) }

// Theme is the shipped theme with [styles] replacing the keys it names.
func (f *File) Theme() (*ui.Theme, error) { return ui.DefaultThemeWith(f.Styles) }

// Summary is what `arxi-sim check` prints for a config that loads. Counts for the three
// vocabulary tables, because the question a reader actually has is whether the file the
// program read is the file they have been editing, and a "[keys] 0" answers it. Literal
// values for everything else, because those are one number or one word each and a reader
// can compare them with what they typed at a glance.
//
// A section that said nothing contributes nothing, which is what makes the line worth
// reading at all: every setting on it came out of the file.
func (f *File) Summary() string {
	out := fmt.Sprintf("[keys] %d, [glyphs] %d, [styles] %d",
		len(f.Keys), len(f.Glyphs), len(f.Styles))
	if f.InputTitle != "" {
		out += fmt.Sprintf(", [input] title=%q", f.InputTitle)
	}
	var scroll []string
	if f.ScrollLines != 0 {
		scroll = append(scroll, fmt.Sprintf("lines=%d", f.ScrollLines))
	}
	if f.Mouse != nil {
		scroll = append(scroll, fmt.Sprintf("mouse=%t", *f.Mouse))
	}
	var anim []string
	if f.Shine != nil {
		anim = append(anim, fmt.Sprintf("shine=%t", *f.Shine))
	}
	// In the order [anim] declares them rather than in the order the struct holds them, so
	// that the line reads like the section it is reporting on.
	for _, n := range []struct {
		name string
		val  int
	}{{"period", f.Anim.Period}, {"travel", f.Anim.Travel}, {"width", f.Anim.Width}} {
		if n.val != 0 {
			anim = append(anim, fmt.Sprintf("%s=%d", n.name, n.val))
		}
	}
	return out + table("scroll", scroll) + table("anim", anim)
}

// table writes one section's worth of settings, or nothing at all when the file set none of
// them. The header is written once and the settings joined after it, because that is what
// the file looks like: `[scroll] lines=1, mouse=false`, not `[scroll] lines=1, [scroll]
// mouse=false`.
func table(name string, settings []string) string {
	if len(settings) == 0 {
		return ""
	}
	return ", [" + name + "] " + strings.Join(settings, ", ")
}

// build runs the three constructors and keeps none of what they return. It is the difference
// between a file that parses and a config the program can be handed: the parse knows what
// the tables declare, the constructors are what will really be built, and a mistake only
// they can see must not wait for a keypress to appear. It is also what makes `check` worth
// running on a config at all.
func (f *File) build() error {
	var problems []error
	if _, err := f.Keymap(); err != nil {
		problems = append(problems, err)
	}
	// Both fallback sets validate the same keys, so one pass answers for -ascii too.
	if _, err := f.GlyphSet(false); err != nil {
		problems = append(problems, err)
	}
	if _, err := f.Theme(); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

// DefaultPath is where the file lives when nobody names one: the config directory the
// operating system already has, plus arxi-sim/config.toml. On Linux and macOS that is
// $XDG_CONFIG_HOME or ~/.config — the path the plan names — and on Windows it is %AppData%,
// which is where a Windows program's settings belong even though the rest of this program's
// habits are POSIX.
//
// It answers "" when there is no home directory to ask about, which LoadDefault reads as "no
// config": the right answer for an empty environment, and not an error.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "arxi-sim", "config.toml")
}

// LoadDefault reads the file in the default place, and answers with an empty config when
// there is none. Not having one is the normal case and cannot be an error. A file that does
// exist and does not load is an error, because the alternative is a config the program
// silently ignores and a reader who never learns why their colour never arrived.
func LoadDefault() (*File, error) {
	path := DefaultPath()
	if path == "" {
		return &File{}, nil
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return &File{}, nil
	}
	return Load(path)
}

// Load reads a config and reports everything wrong with it at once. A missing file is an
// error here; LoadDefault is the door for "there may not be one".
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, data)
}

// Parse validates config bytes already in memory. path is only their name for diagnostics and
// the returned File; Load remains the filesystem API and delegates here so candidate documents
// are checked by exactly the same parser and constructors as files read at startup.
func Parse(path string, data []byte) (*File, error) {
	p := &parser{
		path: path,
		f: &File{
			Path:   path,
			Keys:   map[string]app.Action{},
			Glyphs: map[string]string{},
			Styles: map[string]ui.Style{},
		},
		seen: map[string]int{},
	}
	sn := bufio.NewScanner(bytes.NewReader(data))
	for p.line = 1; sn.Scan(); p.line++ {
		p.feed(sn.Text())
	}
	if err := sn.Err(); err != nil {
		return nil, err
	}
	if len(p.problems) > 0 {
		return nil, errors.Join(p.problems...)
	}
	if err := p.f.build(); err != nil {
		return nil, err
	}
	return p.f, nil
}

// parser is the reader's state: which section the lines are landing in, which line is being
// read, and where each key was first set. It is a type rather than closures over locals
// because bad and dup both need the line number and every section needs both of them.
type parser struct {
	path     string
	f        *File
	problems []error
	line     int
	section  string
	skip     bool           // inside a section that was itself the error: its lines are noise
	seen     map[string]int // "section.key" -> the line that set it
}

func (p *parser) bad(format string, a ...any) {
	p.problems = append(p.problems,
		fmt.Errorf("%s:%d: %s", p.path, p.line, fmt.Sprintf(format, a...)))
}

// dup reports a key set twice and remembers it otherwise. It takes the canonical name and
// not the one written in the file, so that ctrl+alt+x and alt+ctrl+x — one binding under two
// spellings — is caught here instead of being settled silently by whichever line came last.
func (p *parser) dup(name string) bool {
	at := p.section + "." + name
	if prev, ok := p.seen[at]; ok {
		p.bad("%s is set twice, here and on line %d", name, prev)
		return true
	}
	p.seen[at] = p.line
	return false
}

// feed reads one line. Blank lines are allowed, unlike the scenario loader's, because a
// config is written by a person and grouping is how it stays readable.
func (p *parser) feed(raw string) {
	text := strings.TrimSpace(raw)
	if text == "" || strings.HasPrefix(text, "#") {
		return
	}
	if strings.HasPrefix(text, "[") {
		p.header(text)
		return
	}
	if p.skip {
		return
	}
	// The first = is the separator, which is what lets a style value hold one of its own:
	// `prompt.text = fg=red` splits into the key and fg=red and not into three pieces.
	name, rest, ok := strings.Cut(text, "=")
	if !ok {
		p.bad(`%s is not a setting; want key = "value"`, strconv.Quote(text))
		return
	}
	key := strings.Trim(strings.TrimSpace(name), `"`)
	if key == "" {
		p.bad("a setting with no name")
		return
	}
	if p.section == "" {
		// One error and then silence. A file with no header at all is a single mistake, and
		// repeating it once per line buries the line that says so.
		p.bad("%s comes before any [section] header", strconv.Quote(key))
		p.skip = true
		return
	}
	val, err := value(rest)
	if err != nil {
		p.bad("%s: %v", key, err)
		return
	}
	p.set(key, val)
}

// header changes section, or refuses to. An unknown one skips the lines under it rather than
// reporting each: they are almost certainly fine, and the header is the one thing to fix.
func (p *parser) header(text string) {
	head := strings.TrimSpace(cutComment(text))
	name := ""
	if strings.HasSuffix(head, "]") {
		name = strings.TrimSpace(head[1 : len(head)-1])
	}
	p.section, p.skip = "", true
	if name == "" {
		p.bad("%s is not a section header; want one of %s", strconv.Quote(text), sectionList())
		return
	}
	for _, s := range Sections {
		if s.Name == name {
			p.section, p.skip = name, false
			return
		}
	}
	p.bad("[%s] is not a section this file has; there are %s", name, sectionList())
}

// set files one setting under the section it was written in. Each of the six checks the
// name against the table that declares it, so that the error carries a line number, and
// stores the value for the constructor that has the final say over it.
func (p *parser) set(key, val string) {
	switch p.section {
	case "keys":
		k, ok := term.ParseKey(key)
		if !ok {
			p.bad("%s is not a key name; `arxi-sim keys` prints the ones there are",
				strconv.Quote(key))
			return
		}
		a := app.Action(val)
		if a != app.ActionNone && !declaredAction(a) {
			p.bad(`%s is not an action; `+"`arxi-sim keys`"+` prints them, and "" removes a binding`,
				strconv.Quote(val))
			return
		}
		if !p.dup(k.String()) {
			p.f.Keys[k.String()] = a
		}
	case "glyphs":
		if !declaredGlyph(key) {
			p.bad("%s is not a glyph; `arxi-sim keys` prints them", strconv.Quote(key))
			return
		}
		if !p.dup(key) {
			p.f.Glyphs[key] = val
		}
	case "styles":
		if !ui.Declared(key) {
			p.bad("%s is not a style key; `arxi-sim keys` prints them", strconv.Quote(key))
			return
		}
		st, err := ui.ParseStyle(val)
		if err != nil {
			p.bad("%s: %v", key, err)
			return
		}
		if !p.dup(key) {
			p.f.Styles[key] = st
		}
	case "input":
		if key != "title" {
			p.bad("[input] has no %s; it has title", strconv.Quote(key))
			return
		}
		if !p.dup(key) {
			p.f.InputTitle = val
		}
	case "scroll":
		switch key {
		case "lines":
			n, ok := p.count(key, val, "rows")
			if !ok {
				return
			}
			if !p.dup(key) {
				p.f.ScrollLines = n
			}
		case "mouse":
			b, ok := p.flag(key, val)
			if !ok {
				return
			}
			if !p.dup(key) {
				p.f.Mouse = &b
			}
		default:
			p.bad("[scroll] has no %s; it has lines and mouse", strconv.Quote(key))
		}
	case "anim":
		if key == "shine" {
			b, ok := p.flag(key, val)
			if !ok {
				return
			}
			if !p.dup(key) {
				p.f.Shine = &b
			}
			return
		}
		// The three numbers, each named with what it is counted in so that a typo's error says
		// what the number would have meant. Ticks and not milliseconds: the clock belongs to
		// the player, and a file naming a duration would be quietly wrong the day the tick
		// changes length.
		var dst *int
		unit := "ticks"
		switch key {
		case "period":
			dst = &p.f.Anim.Period
		case "travel":
			dst = &p.f.Anim.Travel
		case "width":
			dst, unit = &p.f.Anim.Width, "columns"
		default:
			p.bad("[anim] has no %s; it has shine, period, travel and width", strconv.Quote(key))
			return
		}
		n, ok := p.count(key, val, unit)
		if !ok {
			return
		}
		if !p.dup(key) {
			*dst = n
		}
	}
}

// count reads one positive whole number, which is the only kind of number any section here
// takes. Zero is refused rather than stored: it is the word every one of these fields already
// uses for "unset", so a file asking for it would be asking for the shipped default and would
// then look like a setting that had silently failed.
//
// The unit is the caller's because the error is the whole point of this being a method: rows,
// ticks and columns are three different mistakes, and "want a whole number" alone does not
// tell a reader which one they made.
func (p *parser) count(key, val, unit string) (int, bool) {
	n, err := strconv.Atoi(val)
	if err != nil || n < 1 {
		p.bad("%s %s: want a whole number of %s, 1 or more", key, strconv.Quote(val), unit)
		return 0, false
	}
	return n, true
}

// flag reads a bool for a setting whose shipped default is already true, which is why the
// field behind it is a pointer: this answers "what did the file say", and not saying anything
// is a third answer that main needs to be able to see.
//
// true and false and nothing else. A config is written by a person, and accepting yes/on/1 as
// well would be three more spellings to keep working forever in exchange for nothing — while
// refusing them costs one error message that names the two that work.
func (p *parser) flag(key, val string) (bool, bool) {
	switch val {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	p.bad("%s %s: want true or false", key, strconv.Quote(val))
	return false, false
}

// The two membership questions the owning packages do not answer as a predicate. ui.Declared
// is the model for both — a style key has one — and these ask the same thing of the tables
// that do not. Walking a short slice per line is the right cost for a file read once.
func declaredAction(a app.Action) bool {
	for _, d := range app.ActionKeys {
		if d.Action == a {
			return true
		}
	}
	return false
}

func declaredGlyph(key string) bool {
	for _, d := range ui.GlyphKeys {
		if d.Key == key {
			return true
		}
	}
	return false
}

// value reads the right-hand side of a setting: either a quoted string, taken exactly as it
// stands between the quotes, or the bare text up to a comment.
//
// Quotes are what a value with a space at either end needs, and glyphs are full of them —
// "❯ " is a marker and "❯" is a different one, one column narrower than the transcript's own
// left edge. Inside them \\ and \" are the only escapes, which is enough for the ASCII
// spinner (|/-\) and for a quote in a title, and anything else is an error rather than a
// backslash that quietly stayed.
//
// A quoted value may be empty, and two of them mean something: `up = ""` takes a binding
// away and `prompt.band = ""` takes a wash away. That is why only the bare branch refuses an
// empty value.
func value(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New(`no value; want key = "value"`)
	}
	if raw[0] != '"' {
		v := strings.TrimSpace(cutComment(raw))
		if v == "" {
			return "", errors.New("no value before the comment")
		}
		return v, nil
	}
	var b strings.Builder
	for i := 1; i < len(raw); i++ {
		switch raw[i] {
		case '"':
			if rest := strings.TrimSpace(cutComment(raw[i+1:])); rest != "" {
				return "", fmt.Errorf("%s after the closing quote", strconv.Quote(rest))
			}
			return b.String(), nil
		case '\\':
			if i++; i == len(raw) {
				return "", errors.New("a backslash at the end of the line")
			}
			if raw[i] != '\\' && raw[i] != '"' {
				return "", fmt.Errorf(`unknown escape \%c; there are \\ and \"`, raw[i])
			}
			b.WriteByte(raw[i])
		default:
			b.WriteByte(raw[i])
		}
	}
	return "", errors.New("unclosed quote")
}

// cutComment drops a # and everything after it, but only where the # starts a word: at the
// beginning of the line or after a space. TOML's rule is that any # outside a string opens a
// comment, and this file cannot afford that one — `bg=#2c2c31` is the most ordinary value in
// the whole format and it carries a # in the middle of a word. So a colour needs no quotes and
// `lines = 3 # three rows` still works. The casualties are a bare value that begins with a #
// or holds a space and then a #, and the quoted form is what those are for.
func cutComment(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			return s[:i]
		}
	}
	return s
}
