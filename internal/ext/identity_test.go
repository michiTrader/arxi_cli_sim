package ext_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"arxi.local/sim/internal/ext"
)

func TestIdentityIncludesProtocolAndPackageContent(t *testing.T) {
	m := ext.Manifest{Name: "x", Version: "1", Protocol: "ext/v2", Executable: "x", Capabilities: []ext.Capability{ext.PanelRender}}
	if ext.Identity(m, "one") == ext.Identity(m, "two") {
		t.Fatal("package digest did not change identity")
	}
	m.Protocol = "ext/v1"
	if ext.Identity(m, "one") == ext.Identity(ext.Manifest{Name: "x", Version: "1", Protocol: "ext/v2", Executable: "x", Capabilities: []ext.Capability{ext.PanelRender}}, "one") {
		t.Fatal("protocol did not change identity")
	}
}

func TestTreeDigestContentModeAndUnsafeEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "x")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	one, err := ext.TreeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	two, err := ext.TreeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if one == two {
		t.Fatal("content did not change digest")
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(path, filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
		if _, err := ext.TreeDigest(dir); err == nil {
			t.Fatal("accepted symlink")
		}
	}
}
