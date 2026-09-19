package v2_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"arxi.local/sim/internal/ext"
	"arxi.local/sim/internal/ext/v2"
)

func validView() v2.ViewUpdate {
	return v2.ViewUpdate{ID: "main", Width: 10, Height: 2, Rows: []v2.Row{{ID: "r1", Spans: []v2.Span{{Text: "界e\u0301", Role: v2.RoleText}}}}}
}

func TestCapabilitiesAndMessages(t *testing.T) {
	if !ext.KnownCapabilityFor(v2.Protocol, ext.PanelRender) || ext.KnownCapability(ext.PanelRender) {
		t.Fatal("versioned vocabulary broken")
	}
	messages := []v2.Message{v2.Hello{Protocol: v2.Protocol, Capabilities: ext.CapabilitiesFor(v2.Protocol)}, v2.Ready{Protocol: v2.Protocol, Name: "x", Version: "1", Capabilities: []ext.Capability{ext.PanelRender}}, validView(), v2.ViewClose{ID: "main"}, v2.ViewResize{ID: "main", Width: 80, Height: 24}, v2.ViewFocus{ID: "main"}, v2.ViewBlur{ID: "main"}, v2.ViewInput{ID: "main", Input: v2.Input{Kind: "click", X: 0, Y: 0, Width: 10, Height: 2}}}
	for _, message := range messages {
		env, err := v2.Pack("id", message)
		if err != nil {
			t.Fatalf("%T: %v", message, err)
		}
		got, err := v2.Unpack(env)
		if err != nil || got.MessageType() != message.MessageType() {
			t.Fatalf("%T round trip: %v", message, err)
		}
	}
}

func TestDirection(t *testing.T) {
	if !v2.FromExtension(validView()) || v2.FromHost(validView()) {
		t.Fatal("view.update direction")
	}
	resize := v2.ViewResize{ID: "main", Width: 1, Height: 1}
	if !v2.FromHost(resize) || v2.FromExtension(resize) {
		t.Fatal("view.resize direction")
	}
}

func TestViewLimitsAndText(t *testing.T) {
	if v2.CellWidth("Ae\u0301界") != 4 {
		t.Fatalf("width=%d", v2.CellWidth("Ae\u0301界"))
	}
	cases := []v2.ViewUpdate{validView(), validView(), validView(), validView()}
	cases[0].Rows[0].Spans[0].Text = "\x1b[31mred"
	cases[1].Rows[0].Spans[0].Text = "a\nb"
	cases[2].Rows[0].Spans[0].Text = strings.Repeat("界", 6)
	cases[3].Rows[0].Spans[0].Role = "future"
	for i, view := range cases {
		if err := v2.Validate(view); !errors.Is(err, v2.ErrInvalidMessage) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func TestStructuredInput(t *testing.T) {
	valid := []v2.Input{{Kind: "key", Key: "enter", Width: 1, Height: 1}, {Kind: "action", Action: "refresh", Width: 1, Height: 1}, {Kind: "text", Text: "x", Width: 1, Height: 1}, {Kind: "paste", Text: "xy", Width: 1, Height: 1}, {Kind: "click", X: 1, Y: 1, Width: 2, Height: 2}, {Kind: "drag", Width: 1, Height: 1}, {Kind: "release", Width: 1, Height: 1}, {Kind: "wheel", DY: -1, Width: 1, Height: 1}}
	for _, in := range valid {
		if err := v2.Validate(v2.ViewInput{ID: "p", Input: in}); err != nil {
			t.Errorf("%s: %v", in.Kind, err)
		}
	}
	invalid := []v2.Input{{Kind: "key", Key: "quit", Width: 1, Height: 1}, {Kind: "action", Action: "interrupt", Width: 1, Height: 1}, {Kind: "click", X: 1, Width: 1, Height: 1}, {Kind: "wheel", Width: 1, Height: 1}, {Kind: "text", Text: "\x00", Width: 1, Height: 1}}
	for _, in := range invalid {
		if err := v2.Validate(v2.ViewInput{ID: "p", Input: in}); err == nil {
			t.Errorf("valid: %#v", in)
		}
	}
}

func TestStrictCodecAndFraming(t *testing.T) {
	_, err := v2.Unpack(v2.Envelope{Type: v2.TypeViewClose, Payload: []byte(`{"id":"p","future":1}`)})
	if !errors.Is(err, v2.ErrInvalidMessage) {
		t.Fatal(err)
	}
	wire := bytes.Repeat([]byte{'x'}, v2.DefaultMaxLineBytes+1)
	wire = append(wire, '\n')
	if _, err = v2.NewDecoder(bytes.NewReader(wire)).Decode(); !errors.Is(err, v2.ErrLineTooLong) {
		t.Fatalf("%v", err)
	}
}
