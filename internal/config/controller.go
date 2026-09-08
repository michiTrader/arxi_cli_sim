package config

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"arxi.local/sim/internal/app"
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
	file     *File
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
		}},
		{Name: "Input", Rows: []app.ConfigRow{c.textRow(app.ConfigInputTitle, "Title", c.draft.title, c.baseline.title, effectiveString(c.draft.title, c.opts.Title, c.opts.MaskTitle), c.file.InputTitle != "", c.opts.MaskTitle, "live")}},
		{Name: "Scrolling", Rows: []app.ConfigRow{
			c.intRow(app.ConfigScrollLines, "Lines", c.draft.scroll, c.baseline.scroll, effectiveInt(c.draft.scroll, c.opts.ScrollLines, c.opts.MaskScroll), c.file.ScrollLines != 0, c.opts.MaskScroll, "live"),
			c.boolRow(app.ConfigMouse, "Mouse", c.draft.mouse, c.baseline.mouse, c.opts.Mouse, c.file.Mouse != nil, c.opts.MaskMouse, "next launch"),
		}},
		{Name: "Animation", Rows: []app.ConfigRow{
			c.boolRow(app.ConfigShine, "Shine", c.draft.shine, c.baseline.shine, effectiveBool(c.draft.shine, c.opts.Shine, c.opts.MaskShine), c.file.Shine != nil, c.opts.MaskShine, "live"),
			c.intRow(app.ConfigPeriod, "Period", c.draft.period, c.baseline.period, c.draft.period, c.file.Anim.Period != 0, false, "live"),
			c.intRow(app.ConfigTravel, "Travel", c.draft.travel, c.baseline.travel, c.draft.travel, c.file.Anim.Travel != 0, false, "live"),
			c.intRow(app.ConfigWidth, "Width", c.draft.width, c.baseline.width, c.draft.width, c.file.Anim.Width != 0, false, "live"),
		}},
		{Name: "Session", Rows: []app.ConfigRow{
			{ID: "session.effort", Label: "Effort", Value: "", Effective: "", Source: "session", Apply: "immediate · not saved", Kind: app.ConfigText},
			{ID: "session.recap", Label: "Recap", Value: "false", Effective: "false", Source: "session", Apply: "immediate · not saved", Kind: app.ConfigBool},
		}},
		{Name: "Keys", Rows: c.keyRows()},
		{Name: "Glyphs", Rows: c.glyphRows()},
		{Name: "Styles", Rows: c.styleRows()},
	}
	return s
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
	c.baseline = valuesFromFile(f, c.opts)
	c.draft = c.baseline
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

func (c *Controller) glyphRows() []app.ConfigRow {
	eff, _ := c.file.GlyphSet(c.opts.ASCII)
	rows := make([]app.ConfigRow, 0, len(ui.GlyphKeys))
	for _, d := range ui.GlyphDocs() {
		configured := "—"
		src := "default"
		if v, ok := c.file.Glyphs[d.Key]; ok {
			configured = strconv.Quote(v)
			src = "file"
		}
		def := d.Default
		if c.opts.ASCII {
			def = d.Fallback
		}
		ev := eff.Value(d.Key)
		rows = append(rows, app.ConfigRow{Label: d.Key, Value: strconv.Quote(ev), Persisted: configured, Effective: strconv.Quote(ev), Source: src, Apply: "read-only", Kind: app.ConfigReadOnly, Detail: "default " + strconv.Quote(def) + " · configured " + configured + " · effective " + strconv.Quote(ev)})
	}
	return rows
}

func (c *Controller) styleRows() []app.ConfigRow {
	def := ui.DefaultTheme()
	eff, _ := c.file.Theme()
	rows := make([]app.ConfigRow, 0, len(ui.Keys))
	for _, d := range ui.Keys {
		configured := "—"
		src := "default"
		if v, ok := c.file.Styles[d.Key]; ok {
			configured = v.String()
			if configured == "" {
				configured = "empty"
			}
			src = "file"
		}
		dv := def.Resolve(d.Key).String()
		if dv == "" {
			dv = "empty"
		}
		ev := eff.Resolve(d.Key).String()
		if ev == "" {
			ev = "empty"
		}
		rows = append(rows, app.ConfigRow{Label: d.Key, Value: ev, Persisted: configured, Effective: ev, Source: src, Apply: "read-only", Kind: app.ConfigReadOnly, Detail: "default " + dv + " · configured " + configured + " · effective " + ev})
	}
	return rows
}
