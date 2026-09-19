// Package config reads the file the program has been advertising: flat tables that
// rename a key, redraw a glyph, recolour a span, set a number, turn an animation off,
// pick a theme pack or reorder the chrome.
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
// whoever owns it — app.ActionKeys, ui.GlyphKeys, ui.Keys, app.CompositionWidgets,
// term.ParseKey — and this reader checks membership against those tables and then hands
// the maps to their real constructors, which keep the last word. The check is duplicated
// so that an error can name a line number. The lists never are.
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
	"arxi.local/sim/internal/ext"
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
	{"ui", `theme = the name of a theme pack to wear, from the themes dir`},
	{"layout", "one row each: slot = ordered widget names, and slot = \"\" to leave the slot empty"},
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
type Extension struct {
	Name          string
	Manifest      string
	Enabled       bool
	Allow         ext.CapabilitySet
	Identity      string
	PackageDigest string
	Generation    int
}

type File struct {
	Path string // "" when nothing was read

	// Extensions is keyed by canonical manifest name. Manifest paths are absolute,
	// resolved relative to this config file when written relatively.
	Extensions map[string]Extension

	Keys   map[string]app.Action // canonical key name -> action; "" removes a binding
	Glyphs map[string]string     // glyph key -> the string to draw
	Styles map[string]ui.Style   // style key -> the whole style for it

	// InputTitle is both "unset" and "no title" when empty, which costs nothing because
	// they draw the same border. ScrollLines is 0 when unset, and the player's own
	// defaultWheelLines stands.
	InputTitle  string
	ScrollLines int

	// These settings cannot be plain bools: a file that says nothing has to be told apart
	// from a file that says false, and a bool answers "false" to both. A nil here means main
	// keeps the runtime default, which is how `a flag beats the file beats the default` stays
	// one rule rather than three special cases.
	Mouse *bool
	Shine *bool

	// Anim is the shine's shape — period, travel and width — with a zero in any field
	// meaning "unset", which is the same word ui.Shimmer already uses for it. Style is
	// deliberately not read from the file: which theme key the band is drawn in is the
	// program's business, and the colour is what [styles] input.shine is for.
	Anim ui.Shimmer

	// ThemeName is the theme pack [ui] asked to wear, or "" when it asked for none.
	// The pack itself is not read here — a theme is resolved by whoever runs the
	// program, because resolution (flag over file over nothing, and the file that
	// must exist when a name is given) is a startup decision and not a document's.
	ThemeName string

	// Layout is [layout] as parsed: one override per row, tiers included, validated
	// against the composition's vocabulary with line numbers. Nil when the section
	// is absent, which is the ordinary case and the byte-identical one.
	Layout []app.LayoutOverride
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
	out = out + table("scroll", scroll) + table("anim", anim)
	if f.ThemeName != "" {
		out += fmt.Sprintf(", [ui] theme=%s", strconv.Quote(f.ThemeName))
	}
	if len(f.Layout) > 0 {
		out += fmt.Sprintf(", [layout] %d", len(f.Layout))
	}
	return out
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

// ThemesDir is where theme packs live when nobody names a path: the config directory
// the operating system already has, plus arxi-sim/themes — the same portable rule
// DefaultPath resolves by, one level down, so a reader who found their config file
// finds their themes beside it. It answers "" when there is no home directory to ask
// about, which LoadTheme reads as "nowhere to look".
func ThemesDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "arxi-sim", "themes")
}

// LoadTheme reads the named pack from the themes dir and validates it as a theme. A
// name is a file name without .toml and nothing more — the dir is the whole address
// space, which is what keeps a theme from being a path someone typed into a config
// file. A pack that is missing or invalid is an error and never a silent fall-back to
// the shipped look: whatever named it meant it.
func LoadTheme(name string) (*File, error) {
	dir := ThemesDir()
	if dir == "" {
		return nil, fmt.Errorf("no user config directory, so nowhere to look for theme %s", strconv.Quote(name))
	}
	path := filepath.Join(dir, name+".toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("theme %s: %w", strconv.Quote(name), err)
	}
	f, err := ParseTheme(path, data)
	if err != nil {
		return nil, fmt.Errorf("theme %s: %w", strconv.Quote(name), err)
	}
	return f, nil
}

// ApplyTheme loads the named pack and layers it under f. It is the whole of theme
// resolution on the file side; which name to apply — a flag's, the config's, or
// nobody's — stays with the caller, because that ladder is a startup decision.
func ApplyTheme(f *File, name string) (*File, error) {
	t, err := LoadTheme(name)
	if err != nil {
		return nil, err
	}
	return f.WithTheme(t), nil
}

// WithTheme layers t under f and answers the dressed result: per key of [styles] and
// [glyphs], and per field of [anim], f's own word stands and t fills in only what f
// left unsaid. That order is spec/look.md's, and the reason is the config view: /config
// edits write to the user's own file, so a theme that outranked it would make every
// edit to a themed key a lie on screen. Partial themes are thereby legal — a pack that
// names six keys retunes six keys.
//
// The result is a copy; neither input moves. Only the three tables a theme may hold
// are merged — the role check at load keeps everything else in t zero, and this
// function does not trust that by accident but by not looking.
func (f *File) WithTheme(t *File) *File {
	out := *f
	out.Glyphs = make(map[string]string, len(f.Glyphs)+len(t.Glyphs))
	for k, v := range t.Glyphs {
		out.Glyphs[k] = v
	}
	for k, v := range f.Glyphs {
		out.Glyphs[k] = v
	}
	out.Styles = make(map[string]ui.Style, len(f.Styles)+len(t.Styles))
	for k, v := range t.Styles {
		out.Styles[k] = v
	}
	for k, v := range f.Styles {
		out.Styles[k] = v
	}
	if out.Anim.Period == 0 {
		out.Anim.Period = t.Anim.Period
	}
	if out.Anim.Travel == 0 {
		out.Anim.Travel = t.Anim.Travel
	}
	if out.Anim.Width == 0 {
		out.Anim.Width = t.Anim.Width
	}
	if out.Shine == nil {
		out.Shine = t.Shine
	}
	return &out
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

// LoadForListing reads config syntax and extension metadata without requiring each referenced
// manifest to load. It exists for the read-only extensions list command, which must report an
// invalid entry rather than making one broken manifest hide every other extension.
func LoadForListing(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(path, data, false, false)
}

// Parse validates config bytes already in memory. path is only their name for diagnostics and
// the returned File; Load remains the filesystem API and delegates here so candidate documents
// are checked by exactly the same parser and constructors as files read at startup.
func Parse(path string, data []byte) (*File, error) { return parse(path, data, false, true) }

// ParseTheme validates bytes already in memory as a theme pack: the same grammar and the
// same vocabularies, restricted to the three sections a theme may hold. A theme is not a
// second format — it is a role the same reader already knows, which is what lets `check`
// treat a pack as an ordinary document and this function treat it as a theme without the
// two ever disagreeing about what a line means.
func ParseTheme(path string, data []byte) (*File, error) { return parse(path, data, true, true) }

func parse(path string, data []byte, theme, validateExtensionManifests bool) (*File, error) {
	p := &parser{
		path:  path,
		theme: theme,
		f: &File{
			Path:   path,
			Keys:   map[string]app.Action{},
			Glyphs: map[string]string{},
			Styles: map[string]ui.Style{},
		},
		seen:          map[string]int{},
		extensionLine: map[string]int{},
	}
	sn := bufio.NewScanner(bytes.NewReader(data))
	for p.line = 1; sn.Scan(); p.line++ {
		p.feed(sn.Text())
	}
	if err := sn.Err(); err != nil {
		return nil, err
	}
	if len(p.problems) == 0 && validateExtensionManifests {
		p.validateExtensions()
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
// read, where each key was first set, and whether the file is being read as a theme — the
// one role a reader has besides "config". It is a type rather than closures over locals
// because bad and dup both need the line number and every section needs both of them.
type parser struct {
	path          string
	f             *File
	problems      []error
	line          int
	section       string
	skip          bool           // inside a section that was itself the error: its lines are noise
	seen          map[string]int // "section.key" -> the line that set it
	extensionLine map[string]int
	theme         bool // reading as a theme pack: only taste sections are allowed
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
	if strings.HasPrefix(name, "extensions.") && !p.theme {
		extName := strings.TrimPrefix(name, "extensions.")
		if !validExtensionName(extName) {
			p.bad("[%s] has an invalid extension name; want [extensions.<name>] with [a-z][a-z0-9-]{0,62}", name)
			return
		}
		if _, exists := p.f.Extensions[extName]; exists {
			p.bad("[extensions.%s] is declared twice", extName)
			return
		}
		if p.f.Extensions == nil {
			p.f.Extensions = make(map[string]Extension)
		}
		p.f.Extensions[extName] = Extension{Name: extName, Enabled: true, Allow: ext.NewCapabilitySet()}
		p.extensionLine[extName] = p.line
		p.section, p.skip = name, false
		return
	}
	for _, s := range Sections {
		if s.Name == name {
			if p.theme && !themeHolds(name) {
				p.bad("[%s] is not a section a theme may hold; a theme is [glyphs], [styles] and [anim], and taste is all it carries", name)
				return
			}
			p.section, p.skip = name, false
			return
		}
	}
	p.bad("[%s] is not a section this file has; there are %s", name, sectionList())
}

// themeHolds answers whether a theme pack may hold the section. [keys] is behaviour, [ui]
// is a selection, [input] and [scroll] are settings — none of them is taste, and a theme
// that carried any would be a config wearing a theme's name.
func themeHolds(name string) bool {
	switch name {
	case "glyphs", "styles", "anim":
		return true
	}
	return false
}

// set files one setting under the section it was written in. Each of the six checks the
// name against the table that declares it, so that the error carries a line number, and
// stores the value for the constructor that has the final say over it.
func (p *parser) set(key, val string) {
	if strings.HasPrefix(p.section, "extensions.") {
		p.setExtension(strings.TrimPrefix(p.section, "extensions."), key, val)
		return
	}
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
	case "ui":
		if key != "theme" {
			p.bad("[ui] has no %s; it has theme", strconv.Quote(key))
			return
		}
		if val == "" {
			p.bad(`theme: want the name of a theme pack — the file's name in the themes dir, without .toml`)
			return
		}
		if strings.ContainsAny(val, `/\`) || val == "." || val == ".." {
			p.bad("theme %s: want a name, not a path; the themes dir is where packs live", strconv.Quote(val))
			return
		}
		if !p.dup(key) {
			p.f.ThemeName = val
		}
	case "layout":
		o, ok := p.layoutOverride(key, val)
		if !ok {
			return
		}
		if !p.dup(key) {
			p.f.Layout = append(p.f.Layout, o)
		}
	}
}

func (p *parser) validateExtensions() {
	for name, x := range p.f.Extensions {
		line := p.extensionLine[name]
		if x.Manifest == "" {
			p.problems = append(p.problems, fmt.Errorf("%s:%d: [extensions.%s] requires manifest", p.path, line, name))
			continue
		}
		manifest, err := ext.LoadManifest(x.Manifest)
		if err != nil {
			p.problems = append(p.problems, fmt.Errorf("%s:%d: extension %s manifest: %w", p.path, line, strconv.Quote(name), err))
			continue
		}
		if manifest.Name != name {
			p.problems = append(p.problems, fmt.Errorf("%s:%d: extension section name %q does not match manifest name %q", p.path, line, name, manifest.Name))
		}
		declared := ext.NewCapabilitySet(manifest.Capabilities...)
		for capability := range x.Allow {
			if !declared.Has(capability) {
				p.problems = append(p.problems, fmt.Errorf("%s:%d: extension %q allows %q, which its manifest does not declare", p.path, line, name, capability))
			}
		}
	}
}

func validExtensionName(name string) bool {
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, r := range name[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func stringArray(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		return nil, errors.New(`want an array of quoted capabilities, such as ["events.subscribe"]`)
	}
	raw = strings.TrimSpace(raw[1 : len(raw)-1])
	if raw == "" {
		return []string{}, nil
	}
	var out []string
	for raw != "" {
		if raw[0] != '"' {
			return nil, errors.New("array items must be quoted")
		}
		end, escaped := -1, false
		for i := 1; i < len(raw); i++ {
			if escaped {
				escaped = false
				continue
			}
			if raw[i] == '\\' {
				escaped = true
				continue
			}
			if raw[i] == '"' {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, errors.New("unclosed quote in array")
		}
		item, err := strconv.Unquote(raw[:end+1])
		if err != nil {
			return nil, err
		}
		out = append(out, item)
		raw = strings.TrimSpace(raw[end+1:])
		if raw == "" {
			break
		}
		if raw[0] != ',' {
			return nil, errors.New("want a comma between array items")
		}
		raw = strings.TrimSpace(raw[1:])
		if raw == "" {
			return nil, errors.New("trailing comma is not supported")
		}
	}
	return out, nil
}

func (p *parser) setExtension(name, key, val string) {
	x := p.f.Extensions[name]
	if p.dup(key) {
		return
	}
	switch key {
	case "manifest":
		if val == "" {
			p.bad("manifest must not be empty")
			return
		}
		if filepath.IsAbs(val) {
			x.Manifest = filepath.Clean(val)
		} else {
			x.Manifest = filepath.Clean(filepath.Join(filepath.Dir(p.path), val))
		}
	case "enabled":
		b, ok := p.flag(key, val)
		if !ok {
			return
		}
		x.Enabled = b
	case "allow":
		items, err := stringArray(val)
		if err != nil {
			p.bad("allow: %v", err)
			return
		}
		set := ext.NewCapabilitySet()
		for _, item := range items {
			capability := ext.Capability(item)
			// Parse the union here; validate against the loaded manifest's protocol below.
			if !ext.KnownCapabilityFor("ext/v2", capability) {
				p.bad("allow contains unknown capability %q", capability)
				continue
			}
			if set.Has(capability) {
				p.bad("allow contains duplicate capability %q", capability)
				continue
			}
			set[capability] = struct{}{}
		}
		x.Allow = set
	case "identity":
		// Empty is the installer state before consent has been granted.
		x.Identity = val
	case "package_digest":
		x.PackageDigest = val
	case "generation":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			p.bad("generation must be a non-negative integer")
			return
		}
		x.Generation = n
	default:
		p.bad("[extensions.%s] has no %s; it has manifest, enabled, allow, identity, package_digest and generation", name, strconv.Quote(key))
		return
	}
	p.f.Extensions[name] = x
}

// The composition's vocabulary, indexed once for the [layout] checks. The names and
// slots come from app, which owns them; this index exists so that every row of a file
// can be checked against the same closed list the parser reports from — an invented
// name or a slot nobody draws into is an error with a line number here, not a silent
// no-op in the frame.
var (
	layoutVocabulary = func() map[string]ui.Slot {
		m := make(map[string]ui.Slot)
		for _, w := range app.CompositionWidgets() {
			m[w.Name] = w.Slot
		}
		return m
	}()
	layoutSlots = func() []ui.Slot {
		var out []ui.Slot
		seen := map[ui.Slot]bool{}
		for _, w := range app.CompositionWidgets() {
			if !seen[w.Slot] {
				seen[w.Slot] = true
				out = append(out, w.Slot)
			}
		}
		return out
	}()
)

// layoutOverride parses one [layout] row: `slot`, `slot@width<N` or `slot@height<N` on
// the left, an ordered list of widget names on the right — or "" for a veto, the slot
// left empty. Every name is checked against the composition's vocabulary together with
// the slot it belongs to, because a layout row may reorder and veto inside its own slot
// but never move a widget between slots: the slot is the widget's own answer, and Place
// is the one that resolves it.
func (p *parser) layoutOverride(key, val string) (app.LayoutOverride, bool) {
	var o app.LayoutOverride
	slot, cond := key, ""
	if at := strings.IndexByte(key, '@'); at >= 0 {
		slot, cond = key[:at], key[at+1:]
	}
	s := ui.Slot(slot)
	if !s.Declared() {
		p.bad("%s is not a slot; `arxi-sim keys` prints the ones there are", strconv.Quote(slot))
		return o, false
	}
	if !containsSlot(layoutSlots, s) {
		p.bad("no widget asks for %s; the composition draws into %s", slot, joinSlots(layoutSlots))
		return o, false
	}
	if cond != "" {
		kind, nStr, has := strings.Cut(cond, "<")
		if !has || (kind != "width" && kind != "height") {
			p.bad("%s: after the @ want width<N or height<N, the row's condition", strconv.Quote(key))
			return o, false
		}
		n, err := strconv.Atoi(nStr)
		unit := "columns"
		if kind == "height" {
			unit = "rows"
		}
		if err != nil || n < 1 {
			p.bad("%s: want a whole number of %s after the <, 1 or more", strconv.Quote(key), unit)
			return o, false
		}
		if kind == "width" {
			o.Width = n
		} else {
			o.Height = n
		}
	}
	var names []string
	if val != "" {
		seen := map[string]bool{}
		for _, part := range strings.Split(val, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				p.bad("an empty name in the list; want widget names, comma-separated")
				return o, false
			}
			home, ok := layoutVocabulary[name]
			if !ok {
				p.bad("%s is not a widget; `arxi-sim keys` prints the ones there are", strconv.Quote(name))
				return o, false
			}
			if home != s {
				p.bad("%s is drawn in %s and not %s; a layout row reorders its own slot", strconv.Quote(name), home, s)
				return o, false
			}
			if seen[name] {
				p.bad("%s is listed twice", strconv.Quote(name))
				return o, false
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	o.Slot = s
	o.Names = names
	return o, true
}

func containsSlot(slots []ui.Slot, s ui.Slot) bool {
	for _, have := range slots {
		if have == s {
			return true
		}
	}
	return false
}

func joinSlots(slots []ui.Slot) string {
	out := make([]string, len(slots))
	for i, s := range slots {
		out[i] = string(s)
	}
	return strings.Join(out, ", ")
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
