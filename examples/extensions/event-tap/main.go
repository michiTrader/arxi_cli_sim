// Command event-tap is a portable ext/v1 example that subscribes to all events.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func main() {
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	enc := json.NewEncoder(os.Stdout)
	var hello envelope
	if err := dec.Decode(&hello); err != nil || hello.Type != "hello" {
		return
	}
	_ = enc.Encode(map[string]any{"type": "ready", "payload": map[string]any{"protocol": "ext/v1", "name": "event-tap", "version": "1.0.0", "capabilities": []string{"events.subscribe"}}})
	_ = enc.Encode(map[string]any{"type": "events.subscribe", "payload": map[string]any{}})
	for {
		var message envelope
		if err := dec.Decode(&message); err != nil || message.Type == "shutdown" {
			return
		}
		if message.Type == "event" {
			fmt.Fprintln(os.Stderr, string(message.Payload))
		}
	}
}
