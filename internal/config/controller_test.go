package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arxi.local/sim/internal/app"
	"arxi.local/sim/internal/ui"
)

func controllerFile() *File {
	mouse, shine := false, true
	return &File{
		Keys: map[string]app.Action{}, Glyphs: map[string]string{}, Styles: map[string]ui.Style{},
		InputTitle: "file title", ScrollLines: 5, Mouse: &mouse, Shine: &shine,
		Anim: ui.Shimmer{Period: 21, Travel: 7, Width: 11},
	}
}

func rowByID(t *testing.T, s app.ConfigSnapshot, id app.ConfigID) app.ConfigRow {
	t.Helper()
	for _, category := range s.Categories {
		for _, row := range category.Rows {
			if row.ID == id {
				return row
			}
		}
	}
	t.Fatalf("snapshot has no row %q", id)
	return app.ConfigRow{}
}

func newControllerAt(t *testing.T, body string, mutate func(*ControllerOptions)) (*Controller, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := controllerFile()
	o := ControllerOptions{Path: path, Enabled: true, File: f, Title: f.InputTitle, ScrollLines: f.ScrollLines, Mouse: *f.Mouse, Shine: *f.Shine, Anim: f.Anim}
	if mutate != nil {
		mutate(&o)
	}
	c, err := NewController(o)
	if err != nil {
		t.Fatal(err)
	}
	return c, path
}

func TestControllerDraftEffectiveAndCLIMasking(t *testing.T) {
	c, _ := newControllerAt(t, "", func(o *ControllerOptions) {
		o.Title, o.ScrollLines, o.Shine = "CLI title", 9, true
		o.MaskTitle, o.MaskScroll, o.MaskShine = true, true, true
	})
	for id, value := range map[app.ConfigID]string{
		app.ConfigInputTitle: "draft title", app.ConfigScrollLines: "6", app.ConfigShine: "false", app.ConfigPeriod: "33",
	} {
		if err := c.Set(id, value); err != nil {
			t.Fatal(err)
		}
	}
	s := c.Snapshot()
	checks := []struct {
		id                               app.ConfigID
		value, persisted, effective, src string
		masked                           bool
	}{
		{app.ConfigInputTitle, "draft title", "file title", "CLI title", "CLI", true},
		{app.ConfigScrollLines, "6", "5", "9", "CLI", true},
		{app.ConfigShine, "false", "true", "true", "CLI", true},
		{app.ConfigPeriod, "33", "21", "33", "file", false},
	}
	for _, want := range checks {
		got := rowByID(t, s, want.id)
		if got.Value != want.value || got.Persisted != want.persisted || got.Effective != want.effective || got.Source != want.src || got.Masked != want.masked || !got.Dirty {
			t.Errorf("%s = %+v, want value=%q persisted=%q effective=%q source=%q masked=%v dirty", want.id, got, want.value, want.persisted, want.effective, want.src, want.masked)
		}
	}
	mouse := rowByID(t, s, app.ConfigMouse)
	if mouse.Apply != "next launch" || mouse.Effective != "false" {
		t.Fatalf("mouse row = %+v", mouse)
	}
}

func TestControllerDefaultsAndValidation(t *testing.T) {
	c, err := NewController(ControllerOptions{File: &File{}, Mouse: true, Shine: true})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[app.ConfigID]string{
		app.ConfigScrollLines: "3", app.ConfigPeriod: "40", app.ConfigTravel: "8", app.ConfigWidth: "16",
	} {
		if got := rowByID(t, c.Snapshot(), id); got.Value != want || got.Effective != want || got.Source != "default" {
			t.Errorf("%s = %+v, want default %s", id, got, want)
		}
	}
	for id, value := range map[app.ConfigID]string{
		app.ConfigScrollLines: "0", app.ConfigPeriod: "no", app.ConfigMouse: "maybe", "session.effort": "high",
	} {
		if err := c.Set(id, value); err == nil {
			t.Errorf("Set(%s, %q) succeeded", id, value)
		}
	}
}

func TestControllerReloadDirtyConflictAndDisabledMode(t *testing.T) {
	c, path := newControllerAt(t, "[input]\ntitle = \"file title\"\n", nil)
	if err := c.Set(app.ConfigInputTitle, "draft"); err != nil {
		t.Fatal(err)
	}
	if err := c.Reload(false); !errors.Is(err, app.ErrConfigDirty) {
		t.Fatalf("dirty Reload = %v, want ErrConfigDirty", err)
	}
	if err := os.WriteFile(path, []byte("[input]\ntitle = \"external\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); !errors.Is(err, ErrConflict) {
		t.Fatalf("Save after external edit = %v, want ErrConflict", err)
	}
	if !c.Snapshot().Dirty {
		t.Fatal("conflicted Save discarded the dirty draft")
	}
	if err := c.Reload(true); err != nil {
		t.Fatal(err)
	}
	got := rowByID(t, c.Snapshot(), app.ConfigInputTitle)
	if got.Value != "external" || got.Persisted != "external" || got.Effective != "external" || got.Dirty {
		t.Fatalf("forced Reload = %+v", got)
	}

	disabledPath := filepath.Join(t.TempDir(), "must-not-exist.toml")
	disabled, err := NewController(ControllerOptions{Path: disabledPath, Enabled: false, File: &File{}, Mouse: true, Shine: true})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Snapshot().SaveEnabled || !strings.Contains(disabled.Snapshot().Categories[0].Rows[0].Value, "disabled") {
		t.Fatalf("disabled snapshot = %+v", disabled.Snapshot())
	}
	if err := disabled.Set(app.ConfigInputTitle, "still editable"); err != nil {
		t.Fatal(err)
	}
	if err := disabled.Save(); err == nil {
		t.Fatal("disabled Save succeeded")
	}
	if err := disabled.Reload(true); err == nil {
		t.Fatal("disabled Reload succeeded")
	}
	if _, err := os.Stat(disabledPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled controller created %s: %v", disabledPath, err)
	}
}

func TestControllerInspectorsExposeAllLayers(t *testing.T) {
	f := controllerFile()
	f.Keys["ctrl+x"] = app.ActionCancel
	f.Glyphs["bullet"] = "* "
	f.Styles["prompt.text"] = ui.Style{Attrs: ui.AttrBold}
	c, err := NewController(ControllerOptions{File: f, Mouse: false, Shine: true, Anim: f.Anim})
	if err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	for _, category := range []string{"Overview", "Input", "Scrolling", "Animation", "Session", "Keys", "Glyphs", "Styles"} {
		found := false
		for _, got := range s.Categories {
			if got.Name == category {
				found = true
				if (category == "Keys" || category == "Glyphs" || category == "Styles") && len(got.Rows) == 0 {
					t.Errorf("%s inspector is empty", category)
				}
				for _, row := range got.Rows {
					if category == "Keys" || category == "Glyphs" || category == "Styles" {
						if row.Kind != app.ConfigReadOnly || !strings.Contains(row.Detail, "default ") || !strings.Contains(row.Detail, "configured ") || !strings.Contains(row.Detail, "effective ") {
							t.Errorf("%s row lacks layers: %+v", category, row)
						}
					}
				}
			}
		}
		if !found {
			t.Errorf("snapshot lacks %s category", category)
		}
	}
}

func TestControllerSaveAdvancesBaselineAndCancelRollsBack(t *testing.T) {
	c, path := newControllerAt(t, "", nil)
	if err := c.Set(app.ConfigInputTitle, "saved"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(app.ConfigScrollLines, "8"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Dirty {
		t.Fatal("successful Save left the controller dirty")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"title = \"saved\"", "lines = 8", "mouse = false", "shine = true", "period = 21", "travel = 7", "width = 11"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("saved document lacks %q:\n%s", want, body)
		}
	}
	if err := c.Set(app.ConfigInputTitle, "discard me"); err != nil {
		t.Fatal(err)
	}
	c.Cancel()
	got := rowByID(t, c.Snapshot(), app.ConfigInputTitle)
	if got.Value != "saved" || got.Persisted != "saved" || got.Dirty || c.Snapshot().Dirty {
		t.Fatalf("Cancel after Save = %+v", got)
	}
}
