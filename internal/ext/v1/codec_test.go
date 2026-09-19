package v1_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"arxi.local/sim/internal/ext"
	"arxi.local/sim/internal/ext/v1"
)

func TestCodecRoundTrip(t *testing.T) {
	envelope, err := v1.Pack("handshake-1", v1.Ready{Protocol: v1.Protocol, Name: "clock", Version: "1", Capabilities: []ext.Capability{ext.EventsSubscribe}})
	if err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	if err := v1.NewEncoder(&wire).Encode(envelope); err != nil {
		t.Fatal(err)
	}
	decoded, err := v1.NewDecoder(&wire).Decode()
	if err != nil {
		t.Fatal(err)
	}
	message, err := v1.Unpack(decoded)
	if err != nil {
		t.Fatal(err)
	}
	ready := message.(*v1.Ready)
	if ready.Name != "clock" || decoded.ID != "handshake-1" {
		t.Fatalf("bad round trip: %#v %#v", decoded, ready)
	}
	if _, err := v1.NewDecoder(&wire).Decode(); !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want EOF", err)
	}
}

func TestDecoderRejectsMalformedLines(t *testing.T) {
	for _, wire := range []string{"\n", `{"type":"ready","extra":1}` + "\n", `{"type":"ready"} {}` + "\n", `{"payload":{}}` + "\n"} {
		if _, err := v1.NewDecoder(strings.NewReader(wire)).Decode(); !errors.Is(err, v1.ErrInvalidMessage) {
			t.Errorf("wire %q: got %v", wire, err)
		}
	}
}

func TestUnpackRejectsUnknownTypeAndPayloadFields(t *testing.T) {
	_, err := v1.Unpack(v1.Envelope{Type: "future.message", Payload: []byte(`{}`)})
	var protocolError *v1.ProtocolError
	if !errors.As(err, &protocolError) || protocolError.Code != "unknown_type" {
		t.Fatalf("got %v", err)
	}
	_, err = v1.Unpack(v1.Envelope{Type: v1.TypeSubscribe, Payload: []byte(`{"types":[],"future":true}`)})
	if !errors.Is(err, v1.ErrInvalidMessage) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateMessageFamilies(t *testing.T) {
	valid := []v1.Message{
		v1.Hello{Protocol: v1.Protocol, Capabilities: ext.Capabilities()},
		v1.Subscribe{Types: []string{"run.started"}},
		v1.Event{Seq: 1, Type: "run.started", Event: []byte(`{"seq":1}`)},
		v1.EmitEvent{Type: "run.notice", Scope: "run-1", Payload: []byte(`{"message":"hello"}`)},
		v1.EventsDropped{Count: 2, AfterSeq: 4},
		v1.InboxAnswer{InboxID: "in-1", Kind: "approve"},
		v1.InboxAnswer{InboxID: "in-2", Kind: "answer", Answer: []byte(`"yes"`)},
		v1.RegisterActions{Actions: []v1.Action{{Name: "refresh", Description: "Refresh data"}}},
		v1.InvokeAction{Action: "refresh", Args: "now"},
		v1.Error{Code: "not_granted", Message: "denied"},
	}
	for _, message := range valid {
		if err := v1.Validate(message); err != nil {
			t.Errorf("%T: %v", message, err)
		}
	}
	invalid := []v1.Message{
		v1.Hello{Protocol: "ext/v2"}, v1.Subscribe{After: -1}, v1.Event{Seq: 0},
		v1.EmitEvent{Type: "run.notice", Scope: "", Payload: []byte(`{}`)},
		v1.EventsDropped{Count: 0}, v1.InboxAnswer{InboxID: "x", Kind: "answer"},
		v1.RegisterActions{Actions: []v1.Action{{Name: "Bad", Description: "x"}}},
		v1.InvokeAction{Action: "Bad"},
		v1.Error{Code: "future", Message: "x"},
	}
	for _, message := range invalid {
		if err := v1.Validate(message); err == nil {
			t.Errorf("%T unexpectedly valid", message)
		}
	}
}

func TestEmitEventCodecExcludesHostAttribution(t *testing.T) {
	envelope, err := v1.Pack("emit-1", v1.EmitEvent{
		Type:    "run.notice",
		Scope:   "run-1",
		Payload: []byte(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(envelope.Payload); strings.Contains(got, `"seq"`) || strings.Contains(got, `"source"`) || strings.Contains(got, `"actor"`) {
		t.Fatalf("extension controls host attribution: %s", got)
	}
	message, err := v1.Unpack(envelope)
	if err != nil {
		t.Fatal(err)
	}
	emit := message.(*v1.EmitEvent)
	if emit.Type != "run.notice" || emit.Scope != "run-1" || string(emit.Payload) != `{"message":"hello"}` {
		t.Fatalf("unexpected emit request: %#v", emit)
	}
}

func TestEmitEventRejectsAttributionFields(t *testing.T) {
	_, err := v1.Unpack(v1.Envelope{Type: v1.TypeEmit, Payload: []byte(`{"type":"run.notice","scope":"run-1","payload":{},"actor":"forged"}`)})
	if !errors.Is(err, v1.ErrInvalidMessage) {
		t.Fatalf("got %v, want invalid message", err)
	}
}

func TestAuthorizeDistinguishesDeclarationAndGrant(t *testing.T) {
	declared := ext.NewCapabilitySet(ext.EventsSubscribe)
	granted := ext.NewCapabilitySet()
	for _, test := range []struct {
		cap               ext.Capability
		declared, granted ext.CapabilitySet
		code              string
	}{
		{ext.Capability("panel.render"), declared, granted, "not_declared"},
		{ext.EventsEmit, declared, granted, "not_declared"},
		{ext.EventsSubscribe, declared, granted, "not_granted"},
		{ext.EventsSubscribe, declared, ext.NewCapabilitySet(ext.EventsSubscribe), ""},
	} {
		err := v1.Authorize(test.cap, test.declared, test.granted)
		if test.code == "" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		var got *v1.ProtocolError
		if !errors.As(err, &got) || got.Code != test.code {
			t.Fatalf("got %v, want %s", err, test.code)
		}
	}
}
