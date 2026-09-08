package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"arxi.local/sim/internal/config"
)

func loadDocument(t *testing.T, body string) (*config.Document, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := config.LoadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	return d, path
}

func TestDocumentRoundTripIsByteIdentical(t *testing.T) {
	body := "# mixed endings\r\n[input]\n\ttitle\t =\t\"old\"  # keep\r\n\r\n[scroll]\r\nlines = 3"
	d, _ := loadDocument(t, body)
	if got := string(d.Bytes()); got != body {
		t.Fatalf("Bytes() changed an untouched document:\n got %q\nwant %q", got, body)
	}
	if got, err := d.Render(); err != nil || string(got) != body {
		t.Fatalf("Render() = %q, %v; want the original bytes", got, err)
	}
	if !d.Fingerprint().Exists {
		t.Fatal("an existing document has a missing-file fingerprint")
	}
}

func TestDocumentReplacesOnlyTheValue(t *testing.T) {
	body := "[input]\r\n\ttitle\t =\t\"old\"  # keep this\r\n[scroll]\r\nlines  =  3\t # rows\r\n"
	d, _ := loadDocument(t, body)
	if err := d.SetString("input", "title", ` # " \\ `); err != nil {
		t.Fatal(err)
	}
	if err := d.SetPositiveInt("scroll", "lines", 12); err != nil {
		t.Fatal(err)
	}
	want := "[input]\r\n\ttitle\t =\t" + `" # \" \\\\ "` + "  # keep this\r\n" +
		"[scroll]\r\nlines  =  12\t # rows\r\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("replacement changed surrounding text:\n got %q\nwant %q", got, want)
	}
	f, err := config.Parse(d.Path(), d.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.InputTitle, ` # " \\ `; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestDocumentInsertsKeysAndSections(t *testing.T) {
	d, _ := loadDocument(t, "[scroll]\nlines = 3\n\n[styles]\nprompt.text = fg=red\n")
	if err := d.SetBool("scroll", "mouse", false); err != nil {
		t.Fatal(err)
	}
	if err := d.SetPositiveInt("anim", "width", 7); err != nil {
		t.Fatal(err)
	}
	want := "[scroll]\nlines = 3\nmouse = false\n\n[styles]\nprompt.text = fg=red\n\n[anim]\nwidth = 7\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("insertions:\n%s\nwant:\n%s", got, want)
	}
	if _, err := d.Render(); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentCreatesCRLFSettings(t *testing.T) {
	d, _ := loadDocument(t, "[input]\r\ntitle = \"prompt\"\r\n")
	if err := d.SetPositiveInt("scroll", "lines", 4); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(string(d.Bytes()), "\r\n", ""), "\n") {
		t.Fatalf("inserted a bare LF into %q", d.Bytes())
	}
}

func TestDocumentQuotesEveryStringSafely(t *testing.T) {
	for _, want := range []string{"", "#", `say "go"`, `back\slash`, "trail "} {
		t.Run(want, func(t *testing.T) {
			d, path := loadDocument(t, "[input]\ntitle = \"old\"\n")
			if err := d.SetString("input", "title", want); err != nil {
				t.Fatal(err)
			}
			f, err := config.Parse(path, d.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if f.InputTitle != want {
				t.Errorf("round trip = %q, want %q", f.InputTitle, want)
			}
		})
	}
}

func TestDocumentRejectsInvalidInputAndEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[future]\nanswer = 42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadDocument(path); !errors.Is(err, config.ErrValidation) {
		t.Fatalf("LoadDocument error = %v, want ErrValidation", err)
	}
	d, _ := loadDocument(t, "[scroll]\nlines = 3\n")
	for _, err := range []error{
		d.SetPositiveInt("scroll", "lines", 0),
		d.SetBool("scroll", "lines", true),
		d.SetString("input", "name", "x"),
	} {
		if !errors.Is(err, config.ErrValidation) {
			t.Errorf("edit error = %v, want ErrValidation", err)
		}
	}
}

func TestDocumentSaveCreatesSecureParentsAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one", "two", "config.toml")
	d, err := config.LoadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Fingerprint().Exists {
		t.Fatal("a missing document has an existing-file fingerprint")
	}
	if err := d.SetString("input", "title", "safe # title"); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "[input]\ntitle = \"safe # title\"\n" {
		t.Errorf("saved %q", got)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("new file mode = %v, %v; want 0600", info.Mode().Perm(), err)
		}
		for _, dir := range []string{filepath.Dir(path), filepath.Dir(filepath.Dir(path))} {
			if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
				t.Errorf("new directory mode = %v, %v; want 0700", info.Mode().Perm(), err)
			}
		}
	}
}

func TestDocumentSavePreservesExistingPermissions(t *testing.T) {
	d, path := loadDocument(t, "[scroll]\nlines = 3\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := d.SetPositiveInt("scroll", "lines", 8); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o640 {
			t.Errorf("mode = %o, want 640", got)
		}
	}
}

func TestDocumentSaveDetectsEveryConflict(t *testing.T) {
	t.Run("missing appeared", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		d, err := config.LoadDocument(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.SetBool("anim", "shine", true); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("[anim]\nshine = false\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertConflict(t, d.Save())
	})
	t.Run("existing removed", func(t *testing.T) {
		d, path := loadDocument(t, "[anim]\nshine = true\n")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		assertConflict(t, d.Save())
	})
	t.Run("contents changed", func(t *testing.T) {
		d, path := loadDocument(t, "[anim]\nshine = true\n")
		if err := os.WriteFile(path, []byte("[anim]\nshine = false\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertConflict(t, d.Save())
	})
}

func assertConflict(t *testing.T, err error) {
	t.Helper()
	var conflict *config.ConflictError
	if !errors.Is(err, config.ErrConflict) || !errors.As(err, &conflict) {
		t.Fatalf("Save error = %v, want ConflictError", err)
	}
}

func TestDocumentSaveAllowsMetadataOnlyChangeAndRefreshesFingerprint(t *testing.T) {
	d, path := loadDocument(t, "[scroll]\nlines = 3\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := d.SetPositiveInt("scroll", "lines", 5); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	first := d.Fingerprint()
	if err := d.SetPositiveInt("scroll", "lines", 6); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(); err != nil {
		t.Fatalf("second Save after fingerprint refresh: %v", err)
	}
	if first == d.Fingerprint() {
		t.Fatal("Save did not refresh the content fingerprint")
	}
}

func TestDocumentSaveRejectsUnsafeDestinations(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadDocument(path); err == nil {
			t.Fatal("LoadDocument accepted a directory")
		}
	})
	t.Run("destination symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation is not generally available")
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		target := filepath.Join(dir, "target.toml")
		d, err := config.LoadDocument(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.SetBool("anim", "shine", false); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("[anim]\nshine = true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if err := d.Save(); !errors.Is(err, config.ErrUnsafePath) {
			t.Fatalf("Save error = %v, want ErrUnsafePath", err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "[anim]\nshine = true\n" {
			t.Fatal("Save followed and changed a destination symlink")
		}
	})
	t.Run("parent symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation is not generally available")
		}
		root := t.TempDir()
		realParent := filepath.Join(root, "real")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatal(err)
		}
		linkedParent := filepath.Join(root, "linked")
		if err := os.Symlink(realParent, linkedParent); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(linkedParent, "config.toml")
		d, err := config.LoadDocument(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.SetBool("anim", "shine", true); err != nil {
			t.Fatal(err)
		}
		if err := d.Save(); !errors.Is(err, config.ErrUnsafePath) {
			t.Fatalf("Save error = %v, want ErrUnsafePath", err)
		}
		if _, err := os.Stat(filepath.Join(realParent, "config.toml")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Save wrote through parent symlink: %v", err)
		}
	})
	t.Run("parent is file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "parent")
		if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := config.LoadDocument(filepath.Join(dir, "config.toml"))
		if err == nil {
			t.Fatal("LoadDocument accepted a destination below a regular file")
		}
	})
}

func TestDocumentSaveLeavesNoTemporaryFile(t *testing.T) {
	d, path := loadDocument(t, "[scroll]\nlines = 3\n")
	if err := d.SetPositiveInt("scroll", "lines", 4); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".arxi-config-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("temporary files left after Save: %v", matches)
	}
}
