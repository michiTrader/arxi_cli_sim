package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func source(t *testing.T, executable string) string {
	t.Helper()
	d := t.TempDir()
	manifest := "name = \"demo\"\nversion = \"1\"\nprotocol = \"ext/v1\"\nexecutable = \"" + executable + "\"\ncapabilities = []\n"
	write(t, filepath.Join(d, ManifestName), manifest, 0600)
	write(t, filepath.Join(d, "bin", "run"), "payload", 0755)
	return d
}

func write(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightAndCommit(t *testing.T) {
	src, root := source(t, "bin/run"), t.TempDir()
	plan, err := Preflight(src, Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if plan.FileCount() != 2 || plan.Bytes() == 0 || len(plan.Digest()) != 64 {
		t.Fatalf("bad plan: %#v", plan)
	}
	result, err := Commit(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.ManifestPath != filepath.Join(plan.Destination(), ManifestName) {
		t.Fatalf("bad result: %#v", result)
	}
	info, err := os.Stat(filepath.Join(plan.Destination(), "bin", "run"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	again, err := Commit(plan)
	if err != nil || again.Created {
		t.Fatalf("idempotent result=%#v err=%v", again, err)
	}
	if err := Rollback(again); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plan.Destination()); err != nil {
		t.Fatalf("non-created rollback removed generation: %v", err)
	}
	if err := Rollback(result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plan.Destination()); !os.IsNotExist(err) {
		t.Fatalf("created rollback: %v", err)
	}
}

func TestDigestChanges(t *testing.T) {
	src, root := source(t, "bin/run"), t.TempDir()
	a, err := Preflight(src, Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(src, "bin", "run"), "changed", 0755)
	b, err := Preflight(src, Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest() == b.Digest() {
		t.Fatal("content did not affect digest")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(src, "bin", "run"), 0644); err != nil {
			t.Fatal(err)
		}
		c, err := Preflight(src, Options{Root: root})
		if err != nil {
			t.Fatal(err)
		}
		if b.Digest() == c.Digest() {
			t.Fatal("executable mode did not affect digest")
		}
	}
}

func TestThreatsAndLimits(t *testing.T) {
	cases := []struct {
		name, executable string
		mutate           func(*testing.T, string)
		limits           Limits
	}{
		{"bare executable", "run", nil, Limits{}},
		{"absolute executable", filepath.Join(string(filepath.Separator), "bin", "run"), nil, Limits{}},
		{"reserved name", "bin/run", func(t *testing.T, d string) { write(t, filepath.Join(d, "CON"), "x", 0600) }, Limits{}},
		{"ads", "bin/run", func(t *testing.T, d string) {
			if runtime.GOOS == "windows" {
				t.Skip("directory enumeration does not expose alternate data streams")
			}
			write(t, filepath.Join(d, "bad:name"), "x", 0600)
		}, Limits{}},
		{"files", "bin/run", nil, Limits{MaxFiles: 1, MaxDepth: 9, MaxBytes: 9999}},
		{"bytes", "bin/run", nil, Limits{MaxFiles: 9, MaxDepth: 9, MaxBytes: 1}},
		{"depth", "bin/run", nil, Limits{MaxFiles: 9, MaxDepth: 1, MaxBytes: 9999}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := source(t, tc.executable)
			if tc.mutate != nil {
				tc.mutate(t, d)
			}
			_, err := Preflight(d, Options{Root: t.TempDir(), Limits: tc.limits})
			if err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestSymlinkAndCollision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege is environment-dependent")
	}
	for _, kind := range []string{"file", "root"} {
		t.Run(kind, func(t *testing.T) {
			src := source(t, "bin/run")
			root := t.TempDir()
			if kind == "file" {
				if err := os.Symlink(filepath.Join(src, "bin", "run"), filepath.Join(src, "link")); err != nil {
					t.Fatal(err)
				}
			} else {
				real := src + "-real"
				if err := os.Rename(src, real); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, src); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Preflight(src, Options{Root: root}); err == nil {
				t.Fatal("expected symlink rejection")
			}
		})
	}
	src := source(t, "bin/run")
	write(t, filepath.Join(src, "Foo"), "a", 0600)
	write(t, filepath.Join(src, "foo"), "b", 0600)
	if _, err := Preflight(src, Options{Root: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("collision error: %v", err)
	}
}
