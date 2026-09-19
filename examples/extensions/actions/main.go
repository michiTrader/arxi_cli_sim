// Command actions is a portable ext/v1 example that registers an action.
package main

import (
	"bufio"
	"encoding/json"
	"os"
)

type envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

func main() {
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	enc := json.NewEncoder(os.Stdout)
	var hello envelope
	if err := dec.Decode(&hello); err != nil || hello.Type != "hello" {
		return
	}
	_ = enc.Encode(map[string]any{"type": "ready", "payload": map[string]any{"protocol": "ext/v1", "name": "actions", "version": "1.0.0", "capabilities": []string{"actions.register"}}})
	_ = enc.Encode(map[string]any{"type": "actions.register", "payload": map[string]any{"actions": []map[string]string{{"name": "ping", "description": "Emit a ping notice"}}}})
	for {
		var message envelope
		if err := dec.Decode(&message); err != nil || message.Type == "shutdown" {
			return
		}
		// actions.invoke is deliberately handled without emitting an event because
		// this example requested only actions.register.
	}
}
