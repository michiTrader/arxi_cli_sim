package app

import (
	"errors"
	"strings"
	"testing"

	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

type configControllerStub struct {
	snapshot            ConfigSnapshot
	saved, cancelled    int
	reloaded, discarded int
	setErr, saveErr     error
	reloadErr           error
}

func (c *configControllerStub) Snapshot() ConfigSnapshot { return c.snapshot }
func (c *configControllerStub) Set(id ConfigID, raw string) error {
	if c.setErr != nil {
		return c.setErr
	}
	for i := range c.snapshot.Categories {
		for j := range c.snapshot.Categories[i].Rows {
			r := &c.snapshot.Categories[i].Rows[j]
			if r.ID == id {
				r.Value, r.Effective, r.Dirty = raw, raw, raw != r.Persisted
				c.snapshot.Dirty = c.snapshot.Dirty || r.Dirty
				return nil
			}
		}
	}
	return errors.New("unknown setting")
}
func (c *configControllerStub) Save() error {
	c.saved++
	if c.saveErr != nil {
		return c.saveErr
	}
	c.snapshot.Dirty = false
	for i := range c.snapshot.Categories {
		for j := range c.snapshot.Categories[i].Rows {
			r := &c.snapshot.Categories[i].Rows[j]
			r.Persisted, r.Dirty = r.Value, false
		}
	}
	return nil
}
func (c *configControllerStub) Cancel() {
	c.cancelled++
	c.snapshot.Dirty = false
	for i := range c.snapshot.Categories {
		for j := range c.snapshot.Categories[i].Rows {
			r := &c.snapshot.Categories[i].Rows[j]
			if r.Persisted != "" {
				r.Value, r.Effective = r.Persisted, r.Persisted
			}
			r.Dirty = false
		}
	}
}
func (c *configControllerStub) Reload(discard bool) error {
	c.reloaded++
	if discard {
		c.discarded++
	}
	return c.reloadErr
}

func TestConfigFrameFitsResponsiveSurfaces(t *testing.T) {
	for _, width := range []int{1, 12, 31, 72, 88, 120} {
		for _, height := range []int{0, 1, 2, 3, 8, 20} {
			v := newConfigViewState()
			v.category, v.detail = 3, true
			f := renderConfig(configTestSnapshot(), v, ui.Viewport{Width: width, Height: height})
			if len(f.Live) != height {
				t.Errorf("%dx%d: live=%d", width, height, len(f.Live))
			}
			if bad := f.Overflow(); len(bad) != 0 {
				t.Errorf("%dx%d overflows at %v:\n%s", width, height, bad, f.Plain())
			}
			for _, row := range f.Live {
				if strings.HasSuffix(row.Text(), " ") {
					t.Errorf("%dx%d has trailing space in %q", width, height, row.Text())
				}
			}
		}
	}
}

func TestConfigUsesWideMasterDetailAndNarrowStages(t *testing.T) {
	s := configTestSnapshot()
	wide := renderConfig(s, newConfigViewState(), ui.Viewport{Width: 100, Height: 20}).Plain()
	if !strings.Contains(wide, "Overview") || !strings.Contains(wide, "Config path") || !strings.Contains(wide, "│") {
		t.Fatalf("wide Config is not master/detail:\n%s", wide)
	}
	v := newConfigViewState()
	narrow := renderConfig(s, v, ui.Viewport{Width: 40, Height: 12}).Plain()
	if !strings.Contains(narrow, "Animation") || strings.Contains(narrow, "Config path") {
		t.Fatalf("narrow Config did not start at categories:\n%s", narrow)
	}
	v.detail = true
	detail := renderConfig(s, v, ui.Viewport{Width: 40, Height: 12}).Plain()
	if !strings.Contains(detail, "Config path") || strings.Contains(detail, "Animation\n") {
		t.Fatalf("narrow Config detail is not staged:\n%s", detail)
	}
	one := renderConfig(s, v, ui.Viewport{Width: 40, Height: 1}).Plain()
	if one != "Config · clean · Esc" {
		t.Fatalf("one-row Config = %q", one)
	}
}

func TestConfigNavigationKeepsWideCategoriesAndShortSelectionsReachable(t *testing.T) {
	c := &configControllerStub{snapshot: configTestSnapshot()}
	a := New(Config{Width: 100, Height: 12, Config: c})
	a.openConfig()
	if !a.configKey(ActionComplete, mustKey(t, "tab")) || a.configView.category != 1 || a.configView.row != 0 {
		t.Fatalf("wide Tab category=%d row=%d", a.configView.category, a.configView.row)
	}
	if !a.configKey(ActionComplete, term.Key{Type: term.KeyTab, Mod: term.ModShift}) || a.configView.category != 0 {
		t.Fatalf("wide Shift+Tab category=%d", a.configView.category)
	}

	v := newConfigViewState()
	v.category = len(c.snapshot.Categories) - 1
	f := renderConfig(c.snapshot, v, ui.Viewport{Width: 40, Height: 3})
	if !strings.Contains(f.Plain(), "Styles") || f.Scroll.Above == 0 {
		t.Fatalf("short category list did not reveal selection: scroll=%+v\n%s", f.Scroll, f.Plain())
	}

	v.category, v.row, v.detail, v.editID = 7, 0, true, ""
	f = renderConfig(c.snapshot, v, ui.Viewport{Width: 14, Height: 3})
	if !strings.Contains(f.Plain(), "prompt.text") {
		t.Fatalf("tall selected row hid its actionable first line:\n%s", f.Plain())
	}
}

func TestConfigEditorIsScalarAndCursorIsVisibleOnlyWhileEditing(t *testing.T) {
	s := configTestSnapshot()
	v := newConfigViewState()
	v.category, v.row, v.detail, v.editID = 1, 0, true, ConfigInputTitle
	v.editor.SetText("a long scalar value")
	f := renderConfig(s, v, ui.Viewport{Width: 24, Height: 8})
	if f.Cursor.Hidden || strings.Contains(f.Plain(), "ask anything") || strings.Contains(f.Plain(), "╭") {
		t.Fatalf("Config borrowed conversation editor chrome:\n%s\ncursor=%+v", f.Plain(), f.Cursor)
	}
	v.editID = ""
	if f := renderConfig(s, v, ui.Viewport{Width: 24, Height: 8}); !f.Cursor.Hidden {
		t.Fatalf("non-editing Config exposes cursor: %+v", f.Cursor)
	}
}

func TestConfigViewPreviewsPersistentValuesAndRollsBackOnClose(t *testing.T) {
	c := &configControllerStub{snapshot: configTestSnapshot()}
	a := New(Config{Width: 100, Height: 20, Config: c, InputTitle: "base", WheelLines: 3, Shine: ui.Shimmer{Style: ui.InputShine, Period: 40, Travel: 8, Width: 16}})
	a.ed.Insert("conversation draft")
	a.scrolled, a.top = true, 7
	a.openConfig()
	if err := c.Set(ConfigInputTitle, "preview"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ConfigScrollLines, "9"); err != nil {
		t.Fatal(err)
	}
	a.applyConfigSnapshot(c.Snapshot())
	if a.ed.Title != "preview" || a.cfg.WheelLines != 9 || a.ed.Text() != "conversation draft" {
		t.Fatalf("preview title=%q lines=%d draft=%q", a.ed.Title, a.cfg.WheelLines, a.ed.Text())
	}
	a.closeConfig()
	if c.cancelled != 1 || a.ed.Title != "base" || a.cfg.WheelLines != 3 || a.view != viewConversation || !a.scrolled || a.top != 7 || a.ed.Text() != "conversation draft" {
		t.Fatalf("close did not roll back and restore: cancel=%d title=%q lines=%d view=%v scroll=%v/%d draft=%q", c.cancelled, a.ed.Title, a.cfg.WheelLines, a.view, a.scrolled, a.top, a.ed.Text())
	}
}

func TestConfigPreviewHonorsMasksAndAllLiveAnimationFields(t *testing.T) {
	c := &configControllerStub{snapshot: configTestSnapshot()}
	for i := range c.snapshot.Categories {
		for j := range c.snapshot.Categories[i].Rows {
			r := &c.snapshot.Categories[i].Rows[j]
			switch r.ID {
			case ConfigInputTitle:
				r.Value, r.Effective, r.Masked = "save this title", "CLI title", true
			case ConfigScrollLines:
				r.Value, r.Effective, r.Masked = "12", "9", true
			case ConfigMouse:
				r.Value, r.Effective = "false", "true"
			case ConfigShine:
				r.Value, r.Effective = "false", "false"
			case ConfigPeriod:
				r.Value, r.Effective = "23", "23"
			case ConfigTravel:
				r.Value, r.Effective = "6", "6"
			case ConfigWidth:
				r.Value, r.Effective = "10", "10"
			}
		}
	}
	a := New(Config{Width: 100, Height: 20, Config: c, Emitter: &ui.Emitter{Mouse: true}, InputTitle: "runtime title", WheelLines: 7, Shine: ui.Shimmer{Style: ui.InputShine, Period: 40, Travel: 8, Width: 16}})
	a.applyConfigSnapshot(c.Snapshot())
	if a.ed.Title != "runtime title" || a.cfg.WheelLines != 7 {
		t.Fatalf("masked values previewed: title=%q lines=%d", a.ed.Title, a.cfg.WheelLines)
	}
	if a.cfg.Shine.Style != "" || a.cfg.Shine.Period != 23 || a.cfg.Shine.Travel != 6 || a.cfg.Shine.Width != 10 {
		t.Fatalf("animation preview=%+v", a.cfg.Shine)
	}
	if !a.cfg.Emitter.Mouse {
		t.Fatal("next-launch mouse changed the current emitter")
	}
}

func TestConfigInvalidEditStaysOpenWithError(t *testing.T) {
	c := &configControllerStub{snapshot: configTestSnapshot(), setErr: errors.New("must be a positive integer")}
	a := New(Config{Width: 40, Height: 8, Config: c})
	a.openConfig()
	a.configView.category, a.configView.row, a.configView.detail = 2, 0, true
	if !a.configActivate(a.configSnapshot()) || a.configView.editID != ConfigScrollLines {
		t.Fatal("scroll lines did not enter editor")
	}
	if !a.configEditKey(ActionSubmit, mustKey(t, "enter")) || a.configView.editID != ConfigScrollLines || !strings.Contains(a.configView.errorText, "positive integer") {
		t.Fatalf("invalid edit id=%q error=%q", a.configView.editID, a.configView.errorText)
	}
}

func TestConfigInteractionIsolationSessionControlsAndErrors(t *testing.T) {
	c := &configControllerStub{snapshot: configTestSnapshot()}
	a := New(Config{Width: 100, Height: 12, Config: c})
	a.ed.Insert("draft")
	a.openConfig()
	before := a.ed.Text()
	if dirty, err := a.handle(term.Event{Kind: term.EventPaste, Text: "ignored"}); err != nil || dirty {
		t.Fatalf("paste outside Config edit = %v, %v", dirty, err)
	}
	if a.ed.Text() != before {
		t.Fatalf("Config paste reached conversation editor: %q", a.ed.Text())
	}

	a.configView.category, a.configView.row = 1, 0
	if !a.configActivate(a.configSnapshot()) || a.configView.editID != ConfigInputTitle {
		t.Fatal("title did not enter the local editor")
	}
	if dirty, err := a.handle(term.Event{Kind: term.EventPaste, Text: " next\nline"}); err != nil || !dirty {
		t.Fatalf("paste into Config edit = %v, %v", dirty, err)
	}
	if strings.Contains(a.configView.editor.Text(), "\n") || a.ed.Text() != before {
		t.Fatalf("scalar paste=%q conversation=%q", a.configView.editor.Text(), a.ed.Text())
	}
	if !a.configEditKey(ActionSubmit, mustKey(t, "enter")) || a.configView.editID != "" || a.ed.Title != "base nextline" {
		t.Fatalf("edit did not commit a live preview: id=%q title=%q", a.configView.editID, a.ed.Title)
	}

	a.configView.category, a.configView.row = 4, 0
	if !a.configActivate(a.configSnapshot()) || a.st.Effort != "low" {
		t.Fatalf("session effort = %q", a.st.Effort)
	}
	a.configView.row = 1
	if !a.configActivate(a.configSnapshot()) || !a.st.Recap {
		t.Fatal("session recap did not toggle immediately")
	}

	c.saveErr = errors.New("save conflict stays here")
	if !a.configKey(ActionNone, term.Key{Type: term.KeyRunes, Runes: []rune{'s'}}) || !strings.Contains(a.configView.errorText, "conflict") {
		t.Fatalf("save error not retained: %q", a.configView.errorText)
	}
	f := renderConfig(a.configSnapshot(), a.configView, ui.Viewport{Width: 100, Height: 12})
	if !strings.Contains(f.Plain(), "save conflict stays here") {
		t.Fatalf("save error absent from frame:\n%s", f.Plain())
	}
}

func TestConfigReloadNeedsExplicitDirtyConfirmation(t *testing.T) {
	c := &configControllerStub{snapshot: configTestSnapshot(), reloadErr: ErrConfigDirty}
	a := New(Config{Width: 100, Height: 12, Config: c})
	a.openConfig()
	if !a.configKey(ActionNone, term.Key{Type: term.KeyRunes, Runes: []rune{'r'}}) || !a.configView.confirmReload {
		t.Fatal("dirty reload did not ask for confirmation")
	}
	c.reloadErr = nil
	if !a.configKey(ActionNone, term.Key{Type: term.KeyRunes, Runes: []rune{'y'}}) || c.discarded != 1 || a.configView.confirmReload {
		t.Fatalf("confirmed reload discarded=%d confirm=%v", c.discarded, a.configView.confirmReload)
	}
}

func TestSlashConfigAndSettingsOpenFreshDedicatedView(t *testing.T) {
	for _, command := range []string{"/config", "/settings"} {
		t.Run(command, func(t *testing.T) {
			c := &configControllerStub{snapshot: configTestSnapshot()}
			a := New(Config{Width: 72, Height: 24, Config: c})
			a.ed.Insert(command)
			if !a.dispatch(ActionSubmit, mustKey(t, "enter")) || a.view != viewConfig || a.configView == nil {
				t.Fatalf("%s did not open Config", command)
			}
		})
	}
	m := slashMenuFor("/config", slashMenu{})
	if !m.Active() || m.Selected().Name != "config" || m.Selected().Desc == "" {
		t.Fatalf("/config menu = %#v", m.items)
	}
	if m := slashMenuFor("/settings", slashMenu{}); m.Active() {
		t.Fatal("/settings alias should stay out of canonical suggestions")
	}
}

func configTestSnapshot() ConfigSnapshot {
	return ConfigSnapshot{Path: "/tmp/config.toml", Runtime: "alternate screen · live preview", SaveEnabled: true, Categories: []ConfigCategory{
		{Name: "Overview", Rows: []ConfigRow{{Label: "Config path", Value: "/tmp/config.toml", Effective: "/tmp/config.toml", Source: "runtime", Apply: "read-only", Kind: ConfigReadOnly}}},
		{Name: "Input", Rows: []ConfigRow{{ID: ConfigInputTitle, Label: "Title", Value: "base", Persisted: "base", Effective: "base", Source: "file", Apply: "live", Kind: ConfigText}}},
		{Name: "Scrolling", Rows: []ConfigRow{{ID: ConfigScrollLines, Label: "Lines", Value: "3", Persisted: "3", Effective: "3", Source: "default", Apply: "live", Kind: ConfigInt}, {ID: ConfigMouse, Label: "Mouse", Value: "true", Persisted: "true", Effective: "true", Source: "default", Apply: "next launch", Kind: ConfigBool}}},
		{Name: "Animation", Rows: []ConfigRow{{ID: ConfigShine, Label: "Shine", Value: "true", Persisted: "true", Effective: "true", Source: "default", Apply: "live", Kind: ConfigBool}, {ID: ConfigPeriod, Label: "Period", Value: "40", Persisted: "40", Effective: "40", Source: "default", Apply: "live", Kind: ConfigInt}, {ID: ConfigTravel, Label: "Travel", Value: "8", Persisted: "8", Effective: "8", Source: "default", Apply: "live", Kind: ConfigInt}, {ID: ConfigWidth, Label: "Width", Value: "16", Persisted: "16", Effective: "16", Source: "default", Apply: "live", Kind: ConfigInt}}},
		{Name: "Session", Rows: []ConfigRow{{ID: "session.effort", Label: "Effort", Source: "session", Apply: "immediate · not saved", Kind: ConfigText}, {ID: "session.recap", Label: "Recap", Source: "session", Apply: "immediate · not saved", Kind: ConfigBool}}},
		{Name: "Keys", Rows: []ConfigRow{{Label: "enter", Value: "submit", Effective: "submit", Source: "default", Apply: "read-only", Kind: ConfigReadOnly, Detail: "default submit · configured — · effective submit"}}},
		{Name: "Glyphs", Rows: []ConfigRow{{Label: "bullet", Value: "• ", Effective: "• ", Source: "default", Apply: "read-only", Kind: ConfigReadOnly}}},
		{Name: "Styles", Rows: []ConfigRow{{Label: "prompt.text", Value: "fg=7", Effective: "fg=7", Source: "default", Apply: "read-only", Kind: ConfigReadOnly}}},
	}}
}
