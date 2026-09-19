package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arxi.local/sim/internal/app"
	"arxi.local/sim/internal/config"
	"arxi.local/sim/internal/ext"
	"arxi.local/sim/internal/term"
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
	fs.BoolVar(&o.inline, "inline", false, "")
	_ = fs.Bool("alt", false, "")
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

func TestMouseForHonoursFlagConfigThenPlatform(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name       string
		args       []string
		configured *bool
		termux     bool
		want       bool
	}{
		{name: "Termux automatic, every surface", termux: true, want: false},
		{name: "Termux CLI enable", args: []string{"-mouse=true"}, termux: true, want: true},
		{name: "Termux CLI disable", args: []string{"-mouse=false"}, termux: true, want: false},
		{name: "Termux config enable", configured: &on, termux: true, want: true},
		{name: "Termux config disable", configured: &off, termux: true, want: false},
		{name: "CLI beats config", args: []string{"-mouse=false"}, configured: &on, termux: true, want: false},
		{name: "desktop automatic", want: true},
		{name: "desktop inline keeps the desktop answer", want: true},
		{name: "SSH session uses desktop policy", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, o := controllerTestFlags(t, tc.args...)
			if got := mouseFor(fs, o.mouse, tc.configured, tc.termux); got != tc.want {
				t.Fatalf("mouseFor() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInlineForAnswersFromTheFlagsAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "no flag takes the alternate screen"},
		{name: "-inline", args: []string{"-inline"}, want: true},
		{name: "-inline=false", args: []string{"-inline=false"}},
		{name: "-alt names the default it already is"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, o := controllerTestFlags(t, tc.args...)
			if got := inlineFor(fs, o.inline); got != tc.want {
				t.Fatalf("inlineFor() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPlatformKeysGiveTheReleasedSwipeTheConversation(t *testing.T) {
	history := map[string]app.Action{"up": app.ActionHistoryPrev, "down": app.ActionHistoryNext}
	for _, tc := range []struct {
		name   string
		termux bool
		mouse  bool
		keys   map[string]app.Action
		upWant app.Action
	}{
		{name: "desktop keeps the history", upWant: app.ActionHistoryPrev},
		{name: "phone with the mouse released scrolls", termux: true, upWant: app.ActionScrollUp},
		{name: "phone with the mouse claimed keeps the history", termux: true, mouse: true, upWant: app.ActionHistoryPrev},
		{name: "the file beats the platform", termux: true, keys: history, upWant: app.ActionHistoryPrev},
	} {
		t.Run(tc.name, func(t *testing.T) {
			km, err := keymapFor(&config.File{Keys: tc.keys}, tc.termux, tc.mouse)
			if err != nil {
				t.Fatal(err)
			}
			wantDown := app.ActionHistoryNext
			if tc.upWant == app.ActionScrollUp {
				wantDown = app.ActionScrollDown
			}
			for name, want := range map[string]app.Action{"up": tc.upWant, "down": wantDown} {
				k, ok := term.ParseKey(name)
				if !ok {
					t.Fatalf("%q is not a key name", name)
				}
				if got := km.Lookup(k); got != want {
					t.Fatalf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// TestTheTermuxArrangement pins the phone's four defaults as one unit, because they only
// hold as one: the surface, the mouse, the plain arrows and the notch have broken each
// other twice — the swipe, then the tap, then ctrl+o — because each fix moved a leg of the
// set and the other legs answered. The checklist and the on-device oracle are "The phone
// arrangement" in docs/PLAN.md.
func TestTheTermuxArrangement(t *testing.T) {
	fs, o := controllerTestFlags(t)
	if inlineFor(fs, o.inline) {
		t.Fatal("surface: the phone runs on the alternate screen, where a level change repaints in place and commits nothing")
	}
	if mouseFor(fs, o.mouse, nil, true) {
		t.Fatal("mouse: claimed tracking is the tap the soft keyboard never comes back from")
	}
	if scrollFor(fs, 0, true) != 1 {
		t.Fatal("scroll: a report, or a synthesized arrow, is the row it stands for")
	}
	km, err := keymapFor(&config.File{}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		want app.Action
	}{
		{"up", app.ActionScrollUp},
		{"down", app.ActionScrollDown},
		{"ctrl+p", app.ActionHistoryPrev},
		{"ctrl+n", app.ActionHistoryNext},
		{"alt+up", app.ActionScrollUp},
		{"ctrl+up", app.ActionScrollUpFast},
	} {
		k, ok := term.ParseKey(tc.name)
		if !ok {
			t.Fatalf("%q is not a key name", tc.name)
		}
		if got := km.Lookup(k); got != tc.want {
			t.Fatalf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestConfigControllerForCarriesExplicitFlagProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	fs, o := controllerTestFlags(t, "-config", path, "-title", "CLI", "-scroll", "9", "-mouse=false", "-shine=false")
	o.anim = ui.Shimmer{Period: 40, Travel: 8, Width: 16}
	f := &config.File{InputTitle: "file", ScrollLines: 4}
	c, err := configControllerFor(fs, o, f, nil, "")
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

func TestExtensionsInstallConfirmationAndRegistration(t *testing.T) {
	src := extensionPackage(t, "demo")
	for _, tc := range []struct {
		name, answer string
		installed    bool
	}{{"no", "no\n", false}, {"EOF", "", false}, {"yes", " YES \n", true}, {"flag", "", true}} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			configPath, root := filepath.Join(d, "cfg", "config.toml"), filepath.Join(d, "store")
			var out, stderr bytes.Buffer
			args := []string{"install", src}
			if tc.name == "flag" {
				args = append(args, "--yes")
			}
			err := extensionsCommand(args, commandIO{in: strings.NewReader(tc.answer), out: &out, err: &stderr, configPath: configPath, storeRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(configPath)
			if tc.installed != (statErr == nil) {
				t.Fatalf("installed=%v stat=%v output=%s", tc.installed, statErr, out.String())
			}
			if !strings.Contains(stderr.String(), "NO filesystem or network sandbox") {
				t.Fatalf("missing warning: %s", stderr.String())
			}
			if tc.installed {
				f, err := config.Load(configPath)
				if err != nil {
					t.Fatal(err)
				}
				x := f.Extensions["demo"]
				if !x.Enabled || x.Identity != "" || len(x.Allow) != 0 || x.PackageDigest == "" {
					t.Fatalf("registration=%+v", x)
				}
				if !strings.Contains(out.String(), "Runtime capabilities remain ungranted") {
					t.Fatalf("missing consent message: %s", out.String())
				}
			}
		})
	}
}

func TestExtensionsInstallCollisionRollsBackNewGeneration(t *testing.T) {
	src := extensionPackage(t, "demo")
	d := t.TempDir()
	configPath, root := filepath.Join(d, "config.toml"), filepath.Join(d, "store")
	if err := os.WriteFile(configPath, []byte("[extensions.demo]\nmanifest = \"missing.toml\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := extensionsCommand([]string{"install", src, "--yes"}, commandIO{in: strings.NewReader(""), out: &out, err: io.Discard, configPath: configPath, storeRoot: root})
	if err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Fatalf("error=%v", err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr == nil && len(entries) != 0 {
		t.Fatalf("store changed: %v", entries)
	}
}

func TestExtensionsListOrdersAndReportsStates(t *testing.T) {
	d := t.TempDir()
	readyDir := filepath.Join(d, "ready")
	makeManifest(t, readyDir, "z-ready")
	digest, err := ext.TreeDigest(readyDir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ext.LoadManifest(filepath.Join(readyDir, "manifest.toml"))
	if err != nil {
		t.Fatal(err)
	}
	identity := ext.Identity(m, digest)
	configPath := filepath.Join(d, "config.toml")
	text := fmt.Sprintf("[extensions.z-ready]\nmanifest = %q\nenabled = true\nallow = []\nidentity = %q\npackage_digest = %q\ngeneration = 1\n\n[extensions.a-broken]\nmanifest = %q\n", filepath.Join(readyDir, "manifest.toml"), identity, digest, filepath.Join(d, "missing.toml"))
	if err := os.WriteFile(configPath, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := extensionsCommand([]string{"list", "--config", configPath}, commandIO{out: &out, err: io.Discard}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Index(s, "a-broken") > strings.Index(s, "z-ready") || !strings.Contains(s, "a-broken\t-\t-\tinvalid") || !strings.Contains(s, "z-ready\t1.0.0\text/v1\tconsent-required") {
		t.Fatalf("list:\n%s", s)
	}
}

func extensionPackage(t *testing.T, name string) string {
	t.Helper()
	d := t.TempDir()
	makeManifest(t, d, name)
	return d
}
func makeManifest(t *testing.T, d, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(d, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("name = %q\nversion = \"1.0.0\"\nprotocol = \"ext/v1\"\nexecutable = \"bin/run\"\ncapabilities = [\"events.subscribe\"]\n", name)
	if err := os.WriteFile(filepath.Join(d, "manifest.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "bin", "run"), []byte("binary"), 0700); err != nil {
		t.Fatal(err)
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
	c, err := configControllerFor(fs, o, &config.File{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "arxi-sim", "config.toml")
	if s := c.Snapshot(); !s.SaveEnabled || s.Path != want {
		t.Fatalf("default snapshot=%+v want path %q", s, want)
	}

	fs, o = controllerTestFlags(t, "-config", "")
	c, err = configControllerFor(fs, o, &config.File{}, nil, "")
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
