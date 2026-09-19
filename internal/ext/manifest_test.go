package ext_test

import (
	"errors"
	"strings"
	"testing"

	"arxi.local/sim/internal/ext"
)

func TestParseManifest(t *testing.T) {
	manifest, err := ext.ParseManifest("clock/manifest.toml", strings.NewReader(`# clock
name = "clock"
version = "1.2.0"
protocol = "ext/v1"
executable = "clock.exe" # local
args = ["--zone", "UTC"]
capabilities = ["events.subscribe", "events.emit"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "clock" || len(manifest.Args) != 2 || len(manifest.Capabilities) != 2 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestManifestRejectsInvalidInput(t *testing.T) {
	tests := []struct{ name, text, want string }{
		{"unknown key", `name="x"
wat="x"`, "unknown key"},
		{"duplicate", `name="x"
name="y"`, "duplicate key"},
		{"unknown capability", validManifest(`["root.access"]`), "unknown capability"},
		{"duplicate capability", validManifest(`["events.emit", "events.emit"]`), "duplicate capability"},
		{"phase 3 capability", validManifest(`["panel.render"]`), "unknown capability"},
		{"wrong protocol", strings.Replace(validManifest(`[]`), `ext/v1`, `ext/v3`, 1), "protocol must be"},
		{"bad name", strings.Replace(validManifest(`[]`), `name="x"`, `name="X"`, 1), "name must match"},
		{"table", "[extension]", "tables are not supported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ext.ParseManifest("manifest.toml", strings.NewReader(test.text))
			if !errors.Is(err, ext.ErrInvalidManifest) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want invalid manifest containing %q", err, test.want)
			}
		})
	}
}

func validManifest(caps string) string {
	return "name=\"x\"\nversion=\"1\"\nprotocol=\"ext/v1\"\nexecutable=\"x\"\ncapabilities=" + caps
}

func TestV2ManifestVocabulary(t *testing.T) {
	text := strings.Replace(validManifest(`["panel.render"]`), `ext/v1`, `ext/v2`, 1)
	manifest, err := ext.ParseManifest("manifest.toml", strings.NewReader(text))
	if err != nil || manifest.Protocol != "ext/v2" {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	if !ext.KnownCapabilityFor("ext/v2", ext.PanelRender) || ext.KnownCapabilityFor("ext/v1", ext.PanelRender) {
		t.Fatal("protocol capability vocabularies overlap")
	}
}

func TestCapabilitiesAreClosedAndCopied(t *testing.T) {
	want := []ext.Capability{ext.ActionsRegister, ext.EventsEmit, ext.EventsSubscribe, ext.InboxAnswer}
	got := ext.Capabilities()
	if len(got) != len(want) {
		t.Fatalf("capabilities = %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("capabilities = %v, want exactly %v", got, want)
		}
	}
	got[0] = "changed"
	if ext.KnownCapability("changed") || ext.Capabilities()[0] != ext.ActionsRegister {
		t.Fatal("capability vocabulary was mutable")
	}
	if !ext.NewCapabilitySet(ext.EventsEmit).Has(ext.EventsEmit) {
		t.Fatal("set lost capability")
	}
	if ext.KnownCapability("panel.render") {
		t.Fatal("Phase 3 panel capability advertised by MVP")
	}
}
