// Command panel-v2 is a minimal stdlib-only ext/v2 declarative panel.
package main

import (
	"bufio"
	"encoding/json"
	"os"
)

type envelope struct {
	Type string `json:"type"`
}

func main() {
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	enc := json.NewEncoder(os.Stdout)
	var hello envelope
	if dec.Decode(&hello) != nil || hello.Type != "hello" {
		return
	}
	_ = enc.Encode(map[string]any{"type": "ready", "payload": map[string]any{
		"protocol": "ext/v2", "name": "panel-v2", "version": "1.0.0",
		"capabilities": []string{"panel.render"},
	}})
	_ = enc.Encode(map[string]any{"type": "view.update", "payload": map[string]any{
		"id": "example", "width": 24, "height": 2,
		"rows": []any{
			map[string]any{"id": "title", "spans": []any{map[string]any{"text": "Example panel", "role": "title"}}},
			map[string]any{"id": "body", "spans": []any{map[string]any{"text": "ext/v2 is running", "role": "success"}}},
		},
	}})
	for {
		var message envelope
		if dec.Decode(&message) != nil || message.Type == "shutdown" {
			return
		}
	}
}
