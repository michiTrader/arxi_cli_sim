package app

import (
	"encoding/json"
	"strings"
	"testing"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/ext/viewmodel"
	"arxi.local/sim/internal/term"
)

type extensionProbe struct {
	pending   []ExtensionConsent
	granted   []string
	rejected  []string
	messages  chan ExtensionMessage
	published chan Event
	invoked   chan [3]string
	viewOps   chan string
	inputs    chan viewmodel.Input
}

func (p *extensionProbe) Pending() []ExtensionConsent {
	return append([]ExtensionConsent(nil), p.pending...)
}
func (p *extensionProbe) Grant(name string) error           { p.granted = append(p.granted, name); return nil }
func (p *extensionProbe) Reject(name string)                { p.rejected = append(p.rejected, name) }
func (p *extensionProbe) Messages() <-chan ExtensionMessage { return p.messages }
func (p *extensionProbe) Publish(e Event)                   { p.published <- e }
func (p *extensionProbe) Invoke(x, a, args string) error {
	p.invoked <- [3]string{x, a, args}
	return nil
}
func (p *extensionProbe) ViewResize(x, id string, w, h int) bool {
	p.viewOps <- "resize:" + x + ":" + id
	return true
}
func (p *extensionProbe) ViewFocus(x, id string) bool {
	p.viewOps <- "focus:" + x + ":" + id
	return true
}
func (p *extensionProbe) ViewBlur(x, id string) bool {
	p.viewOps <- "blur:" + x + ":" + id
	return true
}
func (p *extensionProbe) ViewInput(x, id string, in viewmodel.Input) bool {
	p.inputs <- in
	return true
}

func newExtensionProbe() *extensionProbe {
	return &extensionProbe{messages: make(chan ExtensionMessage, 8), published: make(chan Event, 8), invoked: make(chan [3]string, 8), viewOps: make(chan string, 16), inputs: make(chan viewmodel.Input, 16)}
}

func TestExtensionConsentGrantAndRejectBeforeStart(t *testing.T) {
	p := newExtensionProbe()
	p.pending = []ExtensionConsent{{Name: "one"}, {Name: "two"}}
	a := New(Config{Extensions: p})
	if a.view != viewConsent {
		t.Fatal("pending consent did not own full frame")
	}
	if !a.fullViewKey(ActionApprove) || len(p.granted) != 1 || p.granted[0] != "one" {
		t.Fatalf("grant=%v", p.granted)
	}
	if !a.fullViewKey(ActionDeny) || len(p.rejected) != 1 || p.rejected[0] != "two" || a.view != viewConversation {
		t.Fatalf("reject=%v view=%v", p.rejected, a.view)
	}
}

func TestAcceptedEventsPublishCompleteJSONAndSafeEmitIsAttributed(t *testing.T) {
	p := newExtensionProbe()
	a := New(Config{Extensions: p})
	payload, _ := json.Marshal(event.RunPromptPayload{Text: "from extension"})
	if err := a.acceptExtensionEvent("clock", ExtensionEvent{Type: event.RunPrompt, Scope: a.scope, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	published := <-p.published
	var got event.Event
	if err := json.Unmarshal(published.JSON, &got); err != nil {
		t.Fatal(err)
	}
	if got.Seq != published.Seq || got.Type != event.RunPrompt || got.Actor != "clock" || got.Source != event.SourceHuman {
		t.Fatalf("published=%+v event=%+v", published, got)
	}
}

func TestInvalidExtensionProposalDoesNotMutateState(t *testing.T) {
	a := New(Config{})
	beforeSeq, beforeItems := a.seq, len(a.st.Items)
	if err := a.acceptExtensionEvent("bad", ExtensionEvent{Type: event.ToolCall, Scope: a.scope, Payload: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("unsafe event accepted")
	}
	if a.seq != beforeSeq || len(a.st.Items) != beforeItems {
		t.Fatalf("invalid proposal mutated seq/items: %d/%d", a.seq, len(a.st.Items))
	}
}

func TestExtensionInboxAnswerChecksCurrentDecisionAndAttributes(t *testing.T) {
	p := newExtensionProbe()
	a := New(Config{Extensions: p})
	inboxOpen(t, a, "i-1", "approve?")
	answer, _ := json.Marshal("allow")
	if err := a.acceptExtensionAnswer("router", ExtensionAnswer{InboxID: "other", Kind: "answer", Answer: answer}); err == nil {
		t.Fatal("stale inbox accepted")
	}
	if a.st.Inboxes[0].Answered {
		t.Fatal("invalid answer mutated inbox")
	}
	if err := a.acceptExtensionAnswer("router", ExtensionAnswer{InboxID: "i-1", Kind: "approve"}); err != nil {
		t.Fatal(err)
	}
	var got event.Event
	json.Unmarshal((<-p.published).JSON, &got)
	if got.Actor != "router" || !a.st.Inboxes[0].Answered || a.st.Inboxes[0].Answer != "approve" {
		t.Fatalf("event=%+v inbox=%+v", got, a.st.Inboxes[0])
	}
}

func TestExtensionPanelLifecycleRoutingAndResize(t *testing.T) {
	p := newExtensionProbe()
	a := New(Config{Extensions: p, Width: 20, Height: 6})
	v := &viewmodel.View{ID: "main", Width: 10, Height: 4, Rows: []viewmodel.Row{{Spans: []viewmodel.Span{{Text: "hello", Role: viewmodel.RoleTitle}}}}}
	if !a.extensionMessage(ExtensionMessage{Extension: "clock", View: v}) || len(a.panelNames()) != 1 {
		t.Fatal("panel update not cached")
	}
	if !a.openPanel("clock:main") || a.view != viewExtension {
		t.Fatal("panel did not open")
	}
	if got := <-p.viewOps; got != "focus:clock:main" {
		t.Fatalf("first op=%q", got)
	}
	if got := <-p.viewOps; got != "resize:clock:main" {
		t.Fatalf("resize=%q", got)
	}
	a.sendPanelResize()
	select {
	case got := <-p.viewOps:
		t.Fatalf("duplicate resize %q", got)
	default:
	}
	f := a.renderPanel()
	if !f.Cursor.Hidden || len(f.Live) != 6 || !containsPlain(f, "stale") {
		t.Fatalf("stale frame=%q", f.Plain())
	}
	if !a.panelKey(ActionNone, term.Key{Type: term.KeyRunes, Runes: []rune("x")}) {
		t.Fatal("text not routed")
	}
	if got := <-p.inputs; got.Kind != "text" || got.Text != "x" || got.Width != 20 || got.Height != 6 {
		t.Fatalf("input=%+v", got)
	}
	a.extensionMessage(ExtensionMessage{Extension: "clock", Removed: true})
	if a.view != viewConversation || len(a.extPanels) != 0 {
		t.Fatal("disconnect did not revoke panel")
	}
}

func containsPlain(f interface{ Plain() string }, want string) bool {
	return strings.Contains(f.Plain(), want)
}

func TestExtensionPanelsAreDeterministicAndUnfocusedGetsNothing(t *testing.T) {
	p := newExtensionProbe()
	a := New(Config{Extensions: p})
	a.extensionMessage(ExtensionMessage{Extension: "z", View: &viewmodel.View{ID: "b"}})
	a.extensionMessage(ExtensionMessage{Extension: "a", View: &viewmodel.View{ID: "x"}})
	if got := a.panelNames(); len(got) != 2 || got[0] != "a:x" || got[1] != "z:b" {
		t.Fatalf("panels=%v", got)
	}
	if a.panelInput(viewmodel.Input{Kind: "key"}) {
		t.Fatal("unfocused panel received input")
	}
}

func TestExtensionActionRegistrationInvocationRemovalAndSlash(t *testing.T) {
	p := newExtensionProbe()
	a := New(Config{Extensions: p})
	a.extensionMessage(ExtensionMessage{Extension: "clock", Actions: []ExtensionAction{{Name: "tick", Description: "Tick"}}})
	id := Action("ext:clock:tick")
	if a.extActions[id] != "clock" {
		t.Fatal("action not registered")
	}
	a.dispatch(id, term.Key{})
	if got := <-p.invoked; got != [3]string{"clock", "tick", ""} {
		t.Fatalf("dynamic invoke=%v", got)
	}
	if !a.runSlash("ext:clock", "now") {
		t.Fatal("slash not routed")
	}
	if got := <-p.invoked; got != [3]string{"clock", "", "now"} {
		t.Fatalf("slash invoke=%v", got)
	}
	a.extensionMessage(ExtensionMessage{Extension: "clock", Removed: true})
	if _, ok := a.extActions[id]; ok {
		t.Fatal("action survived disconnect")
	}
}
