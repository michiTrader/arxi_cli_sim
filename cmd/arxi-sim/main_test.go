package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"arxi.local/sim/internal/app"
	"arxi.local/sim/internal/config"
	"arxi.local/sim/internal/ui"
)

func controllerTestFlags(t *testing.T, args ...string) (*flag.FlagSet, options) {
	t.Helper()
	var o options
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.StringVar(&o.config, "config", "", "")
	fs.StringVar(&o.title, "title", "", "")
	fs.IntVar(&o.scroll, "scroll", 0, "")
	fs.BoolVar(&o.mouse, "mouse", true, "")
	fs.BoolVar(&o.shine, "shine", true, "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs, o
}

func commandRow(t *testing.T, s app.ConfigSnapshot, id app.ConfigID) app.ConfigRow {
	t.Helper()
	for _, category := range s.Categories {
		for _, row := range category.Rows {
			if row.ID == id {
				return row
			}
		}
	}
	t.Fatalf("missing row %q", id)
	return app.ConfigRow{}
}

func TestConfigControllerForCarriesExplicitFlagProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	fs, o := controllerTestFlags(t, "-config", path, "-title", "CLI", "-scroll", "9", "-mouse=false", "-shine=false")
	o.anim = ui.Shimmer{Period: 40, Travel: 8, Width: 16}
	f := &config.File{InputTitle: "file", ScrollLines: 4}
	c, err := configControllerFor(fs, o, f)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []app.ConfigID{app.ConfigInputTitle, app.ConfigScrollLines, app.ConfigMouse, app.ConfigShine} {
		row := commandRow(t, c.Snapshot(), id)
		if !row.Masked || row.Source != "CLI" {
			t.Errorf("%s masked=%v source=%q", id, row.Masked, row.Source)
		}
	}
	if row := commandRow(t, c.Snapshot(), app.ConfigInputTitle); row.Persisted != "file" || row.Effective != "CLI" {
		t.Fatalf("title layers=%+v", row)
	}
}

func TestConfigControllerForUsesDefaultPathAndExplicitDisable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // linux
	t.Setenv("AppData", dir)         // windows
	if path := config.DefaultPath(); !filepath.HasPrefix(path, dir) {
		t.Skipf("os.UserConfigDir ignores the environment here: %s", path)
	}
	fs, o := controllerTestFlags(t)
	c, err := configControllerFor(fs, o, &config.File{})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "arxi-sim", "config.toml")
	if s := c.Snapshot(); !s.SaveEnabled || s.Path != want {
		t.Fatalf("default snapshot=%+v want path %q", s, want)
	}

	fs, o = controllerTestFlags(t, "-config", "")
	c, err = configControllerFor(fs, o, &config.File{})
	if err != nil {
		t.Fatal(err)
	}
	if s := c.Snapshot(); s.SaveEnabled || s.Path != "" {
		t.Fatalf("disabled snapshot=%+v", s)
	}
	if err := c.Save(); err == nil {
		t.Fatal("Save succeeded with explicit -config empty")
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("disabled mode created default config: %v", err)
	}
}
