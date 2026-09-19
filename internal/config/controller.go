package config

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"arxi.local/sim/internal/app"
	"arxi.local/sim/internal/ext"
	"arxi.local/sim/internal/ui"
)

// ControllerOptions captures startup provenance. Mask fields mean an explicit CLI flag owns
// the effective value even while a different file value is edited and saved.
type ControllerOptions struct {
	Path                                        string
	Enabled                                     bool
	File                                        *File
	ASCII                                       bool
	Runtime                                     string
	Title                                       string
	ScrollLines                                 int
	Mouse                                       bool
	Shine                                       bool
	Anim                                        ui.Shimmer
	MaskTitle, MaskScroll, MaskMouse, MaskShine bool

	// ThemeName is the pack the run wears, after the flag-over-file ladder, and Theme
	// is that pack as loaded — nil when none is worn. The controller never edits a
	// theme: it reads the pack only to attribute what the user's own file was silent
	// about, so the inspector says "worn" where it would otherwise say "default".
	ThemeName string
	Theme     *File
}

type scalarValues struct {
	title                 string
	scroll                int
	mouse                 bool
	shine                 bool
	period, travel, width int
}

type Controller struct {
	opts     ControllerOptions
	doc      *Document
	baseline scalarValues
	draft    scalarValues
	// dressed is the file with the pack layered under it — the tables the run actually
	// reads. The draft and baseline stay raw, because /config edits and saves the
	// user's own file, and baking theme values into it on a save nobody meant would be
	// the theme writing over its reader. Effective columns read dressed; Value,
	// Persisted and Dirty read raw.
	dressed       *File
	dressedValues scalarValues
	file          *File
}

func NewController(o ControllerOptions) (*Controller, error) {
	if o.File == nil {
		o.File = &File{}
	}
	// Options are the values the running app received, after file and CLI precedence but
	// before app.New fills its own zero defaults. Normalize those defaults here so a CLI mask
	// never reports zero while the runtime is actually using the shipped value.
	if o.ScrollLines <= 0 {
		o.ScrollLines = 3
	}
	if o.Anim.Period <= 0 {
		o.Anim.Period = 40
	}
	if o.Anim.Travel <= 0 {
		o.Anim.Travel = 8
	}
	if o.Anim.Width <= 0 {
		o.Anim.Width = 16
	}
	c := &Controller{opts: o, file: o.File}
	c.baseline = valuesFromFile(o.File, o)
	c.draft = c.baseline
	c.dressed = o.File
	if o.Theme != nil {
		c.dressed = o.File.WithTheme(o.Theme)
	}
	c.dressedValues = valuesFromFile(c.dressed, o)
	if o.Enabled {
		d, err := LoadDocument(o.Path)
		if err != nil {
			return nil, err
		}
		c.doc = d
	}
	return c, nil
}

func valuesFromFile(f *File, o ControllerOptions) scalarValues {
	v := scalarValues{title: f.InputTitle, scroll: f.ScrollLines, mouse: o.Mouse, shine: o.Shine, period: f.Anim.Period, travel: f.Anim.Travel, width: f.Anim.Width}
	if v.scroll <= 0 {
		v.scroll = 3
	}
	if f.Mouse != nil {
		v.mouse = *f.Mouse
	}
	if f.Shine != nil {
		v.shine = *f.Shine
	}
	if v.period <= 0 {
		v.period = 40
	}
	if v.travel <= 0 {
		v.travel = 8
	}
	if v.width <= 0 {
		v.width = 16
	}
	return v
}

func (c *Controller) dirty() bool { return c.draft != c.baseline }

// effectiveAnim gives a live draft the last word, then the worn pack, then the
// shipped default already normalized into dressedValues. A pack only fills a field
// the user's file left unset; once the reader edits that field in /config, the draft
// is the user's own word and must be visible immediately rather than hidden behind
// what the pack supplied at startup.
func (c *Controller) effectiveAnim(id app.ConfigID) int {
	switch id {
	case app.ConfigPeriod:
		if c.draft.period != c.baseline.period || c.file.Anim.Period != 0 {
			return c.draft.period
		}
		return c.dressedValues.period
	case app.ConfigTravel:
		if c.draft.travel != c.baseline.travel || c.file.Anim.Travel != 0 {
			return c.draft.travel
		}
		return c.dressedValues.travel
	case app.ConfigWidth:
		if c.draft.width != c.baseline.width || c.file.Anim.Width != 0 {
			return c.draft.width
		}
		return c.dressedValues.width
	}
	return 0
}

func source(configured, masked bool) string {
	if masked {
		return "CLI"
	}
	if configured {
		return "file"
	}
	return "default"
}
func boolString(v bool) string { return strconv.FormatBool(v) }

func (c *Controller) Snapshot() app.ConfigSnapshot {
	s := app.ConfigSnapshot{Path: c.opts.Path, Runtime: c.opts.Runtime, SaveEnabled: c.opts.Enabled, Dirty: c.dirty()}
	s.Categories = []app.ConfigCategory{
		{Name: "Overview", Rows: []app.ConfigRow{
			{Label: "Config path", Value: displayPath(c.opts.Path, c.opts.Enabled), Effective: displayPath(c.opts.Path, c.opts.Enabled), Source: "runtime", Apply: "read-only", Kind: app.ConfigReadOnly},
			{Label: "Runtime", Value: c.opts.Runtime, Effective: c.opts.Runtime, Source: "runtime", Apply: "read-only", Kind: app.ConfigReadOnly},
			c.themeRow(),
		}},
		{Name: "Input", Rows: []app.ConfigRow{c.textRow(app.ConfigInputTitle, "Title", c.draft.title, c.baseline.title, effectiveString(c.draft.title, c.opts.Title, c.opts.MaskTitle), c.file.InputTitle != "", c.opts.MaskTitle, "live")}},
		{Name: "Scrolling", Rows: []app.ConfigRow{
			c.intRow(app.ConfigScrollLines, "Lines", c.draft.scroll, c.baseline.scroll, effectiveInt(c.draft.scroll, c.opts.ScrollLines, c.opts.MaskScroll), c.file.ScrollLines != 0, c.opts.MaskScroll, "live"),
			c.boolRow(app.ConfigMouse, "Mouse", c.draft.mouse, c.baseline.mouse, c.opts.Mouse, c.file.Mouse != nil, c.opts.MaskMouse, "next launch"),
		}},
		{Name: "Animation", Rows: []app.ConfigRow{
			c.boolRow(app.ConfigShine, "Shine", c.draft.shine, c.baseline.shine, effectiveBool(c.draft.shine, c.opts.Shine, c.opts.MaskShine), c.file.Shine != nil, c.opts.MaskShine, "live"),
			// The three numbers read their effective side from the dressed file, because a
			// theme pack may set them where the user's file was silent. The draft and the
			// dirty flag stay raw: editing starts from what the file would say, and a save
			// writes what was edited, never what the pack wore.
			c.intRow(app.ConfigPeriod, "Period", c.draft.period, c.baseline.period, c.effectiveAnim(app.ConfigPeriod), c.file.Anim.Period != 0, false, "live"),
			c.intRow(app.ConfigTravel, "Travel", c.draft.travel, c.baseline.travel, c.effectiveAnim(app.ConfigTravel), c.file.Anim.Travel != 0, false, "live"),
			c.intRow(app.ConfigWidth, "Width", c.draft.width, c.baseline.width, c.effectiveAnim(app.ConfigWidth), c.file.Anim.Width != 0, false, "live"),
		}},
		{Name: "Session", Rows: []app.ConfigRow{
			{ID: "session.effort", Label: "Effort", Value: "", Effective: "", Source: "session", Apply: "immediate · not saved", Kind: app.ConfigText},
			{ID: "session.recap", Label: "Recap", Value: "false", Effective: "false", Source: "session", Apply: "immediate · not saved", Kind: app.ConfigBool},
		}},
		{Name: "Layout", Rows: c.layoutRows()},
		{Name: "Keys", Rows: c.keyRows()},
		{Name: "Glyphs", Rows: c.glyphRows()},
		{Name: "Styles", Rows: c.styleRows()},
	}
	return s
}

// themeRow is the Overview's answer to "what is this run wearing": the pack's name,
// or the dash when none, with the themes dir beside it so the next question — where
// would I put one — is answered on the same row.
func (c *Controller) themeRow() app.ConfigRow {
	name := c.opts.ThemeName
	if name == "" {
		name = "—"
	}
	return app.ConfigRow{
		Label: "Theme", Value: name, Persisted: name, Effective: name,
		Source: "runtime", Apply: "read-only", Kind: app.ConfigReadOnly,
		Detail: "packs live in " + ThemesDir() + " · -theme beats [ui] theme · a pack layers under your own file, per key",
	}
}

// layoutRows are the Layout category: one row per slot the composition draws into,
// the file's word beside the composition's default. The effective column — what this
// frame actually stacked, tiers resolved — is filled in by the app, which is the only
// thing that knows how wide the terminal is; until then the row shows what was
// written, which is the honest answer a file inspector can give on its own.
func (c *Controller) layoutRows() []app.ConfigRow {
	bySlot := map[ui.Slot][]string{}
	var order []ui.Slot
	for _, w := range app.CompositionWidgets() {
		if _, seen := bySlot[w.Slot]; !seen {
			order = append(order, w.Slot)
		}
		bySlot[w.Slot] = append(bySlot[w.Slot], w.Name)
	}
	written := map[ui.Slot][]app.LayoutOverride{}
	for _, o := range c.file.Layout {
		written[o.Slot] = append(written[o.Slot], o)
	}
	rows := make([]app.ConfigRow, 0, len(order))
	for _, slot := range order {
		def := strings.Join(bySlot[slot], ", ")
		id := app.ConfigID("layout." + string(slot))
		ws, has := written[slot]
		if !has {
			rows = append(rows, app.ConfigRow{ID: id, Label: string(slot), Value: def, Persisted: def, Effective: def, Source: "default", Apply: "read-only", Kind: app.ConfigReadOnly, Detail: "the composition's own order"})
			continue
		}
		for _, o := range ws {
			label := string(slot)
			if o.Width > 0 {
				label += fmt.Sprintf(" @width<%d", o.Width)
			}
			if o.Height > 0 {
				label += fmt.Sprintf(" @height<%d", o.Height)
			}
			v := strings.Join(o.Names, ", ")
			if v == "" {
				v = "(empty)"
			}
			rows = append(rows, app.ConfigRow{ID: id, Label: label, Value: v, Persisted: v, Effective: v, Source: "file", Apply: "read-only", Kind: app.ConfigReadOnly, Detail: "default " + def})
		}
	}
	return rows
}

func displayPath(path string, enabled bool) string {
	if !enabled {
		return "disabled (-config \"\")"
	}
	if path == "" {
		return "unavailable"
	}
	return path
}
func effectiveString(draft, cli string, masked bool) string {
	if masked {
		return cli
	}
	return draft
}
func effectiveInt(draft, cli int, masked bool) int {
	if masked {
		return cli
	}
	return draft
}
func effectiveBool(draft, cli, masked bool) bool {
	if masked {
		return cli
	}
	return draft
}

func baseRow(id app.ConfigID, label, value, persisted, effective string, kind app.ConfigValueKind, configured, masked, dirty bool, apply string) app.ConfigRow {
	return app.ConfigRow{ID: id, Label: label, Value: value, Persisted: persisted, Effective: effective, Source: source(configured, masked), Apply: apply, Kind: kind, Dirty: dirty, Masked: masked}
}
func (c *Controller) textRow(id app.ConfigID, label, value, persisted, effective string, configured, masked bool, apply string) app.ConfigRow {
	return baseRow(id, label, value, persisted, effective, app.ConfigText, configured, masked, value != persisted, apply)
}
func (c *Controller) intRow(id app.ConfigID, label string, value, persisted, effective int, configured, masked bool, apply string) app.ConfigRow {
	return baseRow(id, label, strconv.Itoa(value), strconv.Itoa(persisted), strconv.Itoa(effective), app.ConfigInt, configured, masked, value != persisted, apply)
}
func (c *Controller) boolRow(id app.ConfigID, label string, value, persisted, effective bool, configured, masked bool, apply string) app.ConfigRow {
	return baseRow(id, label, boolString(value), boolString(persisted), boolString(effective), app.ConfigBool, configured, masked, value != persisted, apply)
}

func (c *Controller) Set(id app.ConfigID, raw string) error {
	switch id {
	case app.ConfigInputTitle:
		c.draft.title = raw
	case app.ConfigScrollLines, app.ConfigPeriod, app.ConfigTravel, app.ConfigWidth:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return fmt.Errorf("%s must be a positive integer", id)
		}
		switch id {
		case app.ConfigScrollLines:
			c.draft.scroll = n
		case app.ConfigPeriod:
			c.draft.period = n
		case app.ConfigTravel:
			c.draft.travel = n
		case app.ConfigWidth:
			c.draft.width = n
		}
	case app.ConfigMouse, app.ConfigShine:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("%s must be true or false", id)
		}
		if id == app.ConfigMouse {
			c.draft.mouse = v
		} else {
			c.draft.shine = v
		}
	default:
		return fmt.Errorf("%s is read-only", id)
	}
	return nil
}

func (c *Controller) applyDraft(d *Document) error {
	for _, x := range []struct {
		section, key string
		value        int
	}{{"scroll", "lines", c.draft.scroll}, {"anim", "period", c.draft.period}, {"anim", "travel", c.draft.travel}, {"anim", "width", c.draft.width}} {
		if err := d.SetPositiveInt(x.section, x.key, x.value); err != nil {
			return err
		}
	}
	if err := d.SetString("input", "title", c.draft.title); err != nil {
		return err
	}
	if err := d.SetBool("scroll", "mouse", c.draft.mouse); err != nil {
		return err
	}
	return d.SetBool("anim", "shine", c.draft.shine)
}

func (c *Controller) Save() error {
	if !c.opts.Enabled || c.doc == nil {
		return errors.New("save is disabled for this run")
	}
	if err := c.applyDraft(c.doc); err != nil {
		return err
	}
	if err := c.doc.Save(); err != nil {
		return err
	}
	f, err := Parse(c.opts.Path, c.doc.Bytes())
	if err != nil {
		return err
	}
	c.file = f
	c.dressed = f
	if c.opts.Theme != nil {
		c.dressed = f.WithTheme(c.opts.Theme)
	}
	c.dressedValues = valuesFromFile(c.dressed, c.opts)
	c.baseline = c.draft
	return nil
}
func (c *Controller) Cancel() { c.draft = c.baseline }
func (c *Controller) Reload(discard bool) error {
	if c.dirty() && !discard {
		return app.ErrConfigDirty
	}
	if !c.opts.Enabled {
		return errors.New("reload is disabled for this run")
	}
	d, err := LoadDocument(c.opts.Path)
	if err != nil {
		return err
	}
	f, err := Parse(c.opts.Path, d.Bytes())
	if err != nil {
		return err
	}
	c.doc = d
	c.file = f
	c.dressed = f
	if c.opts.Theme != nil {
		c.dressed = f.WithTheme(c.opts.Theme)
	}
	c.dressedValues = valuesFromFile(c.dressed, c.opts)
	c.baseline = valuesFromFile(f, c.opts)
	c.draft = c.baseline
	return nil
}

// Extensions returns an independent snapshot consumable by startup integration.
func (c *Controller) Extensions() map[string]Extension {
	out := make(map[string]Extension, len(c.file.Extensions))
	for name, value := range c.file.Extensions {
		value.Allow = ext.NewCapabilitySet(capabilities(value.Allow)...)
		out[name] = value
	}
	return out
}

func capabilities(set ext.CapabilitySet) []ext.Capability {
	out := make([]ext.Capability, 0, len(set))
	for capability := range set {
		out = append(out, capability)
	}
	return out
}

// SetExtensionConsent persists an exact grant and the identity it applies to.
func (c *Controller) SetExtensionConsent(name string, allow ext.CapabilitySet, identity string) error {
	if !c.opts.Enabled || c.doc == nil {
		return errors.New("save is disabled for this run")
	}
	if _, ok := c.file.Extensions[name]; !ok {
		return fmt.Errorf("extension %q is not configured", name)
	}
	if err := c.doc.SetExtensionConsent(name, allow, identity); err != nil {
		return err
	}
	if err := c.doc.Save(); err != nil {
		return err
	}
	f, err := Parse(c.opts.Path, c.doc.Bytes())
	if err != nil {
		return err
	}
	c.file = f
	return nil
}

// SetExtensionAllow persists an exact grant set immediately, with the same
// optimistic-concurrency protection as other controller saves.
func (c *Controller) SetExtensionAllow(name string, allow ext.CapabilitySet) error {
	if !c.opts.Enabled || c.doc == nil {
		return errors.New("save is disabled for this run")
	}
	if _, ok := c.file.Extensions[name]; !ok {
		return fmt.Errorf("extension %q is not configured", name)
	}
	if err := c.doc.SetExtensionAllow(name, allow); err != nil {
		return err
	}
	if err := c.doc.Save(); err != nil {
		return err
	}
	f, err := Parse(c.opts.Path, c.doc.Bytes())
	if err != nil {
		return err
	}
	c.file = f
	return nil
}

func (c *Controller) keyRows() []app.ConfigRow {
	defaults := app.DefaultBindings()
	effective, _ := c.file.Keymap()
	em := map[string]app.Action{}
	for _, b := range effective.Bindings() {
		em[b.Key] = b.Action
	}
	keys := map[string]bool{}
	for k := range defaults {
		keys[k] = true
	}
	for k := range c.file.Keys {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	rows := make([]app.ConfigRow, 0, len(names))
	for _, k := range names {
		configured := "—"
		src := "default"
		if v, ok := c.file.Keys[k]; ok {
			configured = string(v)
			if v == "" {
				configured = "removed"
			}
			src = "file"
		}
		def := string(defaults[k])
		if def == "" {
			def = "—"
		}
		eff := string(em[k])
		if eff == "" {
			eff = "removed"
		}
		rows = append(rows, app.ConfigRow{Label: k, Value: eff, Persisted: configured, Effective: eff, Source: src, Apply: "read-only", Kind: app.ConfigReadOnly, Detail: "default " + def + " · configured " + configured + " · effective " + eff})
	}
	return rows
}

// packGlyphs and packStyles answer the worn pack's tables, or nil when none is worn.
// A read on a nil map is the empty answer, which is the right one here; the guard is
// for the nil *File the map would have to be reached through.
func (c *Controller) packGlyphs() map[string]string {
	if c.opts.Theme != nil {
		return c.opts.Theme.Glyphs
	}
	return nil
}

func (c *Controller) packStyles() map[string]ui.Style {
	if c.opts.Theme != nil {
		return c.opts.Theme.Styles
	}
	return nil
}

func (c *Controller) glyphRows() []app.ConfigRow {
	eff, _ := c.dressed.GlyphSet(c.opts.ASCII)
	rows := make([]app.ConfigRow, 0, len(ui.GlyphKeys))
	for _, d := range ui.GlyphDocs() {
		configured := "—"
		src := "default"
		detail := ""
		if v, ok := c.file.Glyphs[d.Key]; ok {
			configured = strconv.Quote(v)
			src = "file"
		} else if v, ok := c.packGlyphs()[d.Key]; ok {
			// The file was silent and the worn pack speaks, so the key is worn rather
			// than default. The pack's word is named in the detail line and never in
			// the configured column, which is only ever what the user's own file says.
			src = "theme " + c.opts.ThemeName
			detail = "the pack wears " + strconv.Quote(v)
		}
		def := d.Default
		if c.opts.ASCII {
			def = d.Fallback
		}
		ev := eff.Value(d.Key)
		rows = append(rows, app.ConfigRow{Label: d.Key, Value: strconv.Quote(ev), Persisted: configured, Effective: strconv.Quote(ev), Source: src, Apply: "read-only", Kind: app.ConfigReadOnly, Detail: strings.TrimSpace("default " + strconv.Quote(def) + " · configured " + configured + " · effective " + strconv.Quote(ev) + " · " + detail)})
	}
	return rows
}

func (c *Controller) styleRows() []app.ConfigRow {
	def := ui.DefaultTheme()
	eff, _ := c.dressed.Theme()
	rows := make([]app.ConfigRow, 0, len(ui.Keys))
	for _, d := range ui.Keys {
		configured := "—"
		src := "default"
		detail := ""
		if v, ok := c.file.Styles[d.Key]; ok {
			configured = v.String()
			if configured == "" {
				configured = "empty"
			}
			src = "file"
		} else if v, ok := c.packStyles()[d.Key]; ok {
			src = "theme " + c.opts.ThemeName
			detail = "the pack wears " + v.String()
		}
		dv := def.Resolve(d.Key).String()
		if dv == "" {
			dv = "empty"
		}
		ev := eff.Resolve(d.Key).String()
		if ev == "" {
			ev = "empty"
		}
		rows = append(rows, app.ConfigRow{Label: d.Key, Value: ev, Persisted: configured, Effective: ev, Source: src, Apply: "read-only", Kind: app.ConfigReadOnly, Detail: strings.TrimSpace("default " + dv + " · configured " + configured + " · effective " + ev + " · " + detail)})
	}
	return rows
}
