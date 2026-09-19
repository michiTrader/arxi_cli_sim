package v2_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"arxi.local/sim/internal/ext/v2"
)

func TestConformanceFixtures(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "conformance"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", "conformance", entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Valid     bool            `json:"valid"`
				Direction string          `json:"direction"`
				Envelope  json.RawMessage `json:"envelope"`
			}
			if err = json.Unmarshal(body, &fixture); err != nil {
				t.Fatal(err)
			}
			var env v2.Envelope
			err = json.Unmarshal(fixture.Envelope, &env)
			var message v2.Message
			if err == nil {
				message, err = v2.Unpack(env)
			}
			if err == nil && fixture.Direction == "extension" && !v2.FromExtension(message) {
				err = v2.ErrInvalidMessage
			}
			if err == nil && fixture.Direction == "host" && !v2.FromHost(message) {
				err = v2.ErrInvalidMessage
			}
			if fixture.Valid != (err == nil) {
				t.Fatalf("valid=%v err=%v", fixture.Valid, err)
			}
		})
	}
}
