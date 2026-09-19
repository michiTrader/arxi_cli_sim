package v1_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"arxi.local/sim/internal/ext/v1"
)

func TestConformanceFixtures(t *testing.T) {
	root := filepath.Join("testdata", "conformance")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Valid    bool            `json:"valid"`
				Envelope json.RawMessage `json:"envelope"`
			}
			if err := json.Unmarshal(body, &fixture); err != nil {
				t.Fatal(err)
			}
			var env v1.Envelope
			err = json.Unmarshal(fixture.Envelope, &env)
			if err == nil {
				_, err = v1.Unpack(env)
			}
			if fixture.Valid != (err == nil) {
				t.Fatalf("valid=%v err=%v", fixture.Valid, err)
			}
		})
	}
}
