package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/ext/viewmodel"
	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/ui"
)

// ExtensionConsent is the complete identity shown before native code is started.
type ExtensionConsent struct {
	Name, Version, Executable, Identity string
	Args, Capabilities                  []string
}

// ExtensionMessage is an app-owned notification from the process host.
type ExtensionMessage struct {
	Extension  string
	Emit       *ExtensionEvent
	Answer     *ExtensionAnswer
	Actions    []ExtensionAction
	View       *viewmodel.View
	ViewClosed string
	Removed    bool
	Notice     string
}

type ExtensionEvent struct {
	Type, Scope string
	Payload     json.RawMessage
}
type ExtensionAnswer struct {
	InboxID, Kind string
	Answer        json.RawMessage
}
type ExtensionAction struct{ Name, Description string }

// ExtensionManager is implemented by composition code; app owns no process API.
type ExtensionManager interface {
	Pending() []ExtensionConsent
	Grant(name string) error
	Reject(name string)
	Messages() <-chan ExtensionMessage
	Publish(Event)
	Invoke(extension, action, args string) error
	ViewResize(extension, panel string, width, height int) bool
	ViewFocus(extension, panel string) bool
	ViewBlur(extension, panel string) bool
	ViewInput(extension, panel string, input viewmodel.Input) bool
}

func extensionMessages(m ExtensionManager) <-chan ExtensionMessage {
	if m == nil {
		return nil
	}
	return m.Messages()
}

// Event is the complete accepted event delivered to subscribers.
type Event struct {
	Seq  int
	Type string
	JSON json.RawMessage
}

func eventForHost(ev *event.Event) Event {
	body, _ := json.Marshal(ev)
	return Event{Seq: ev.Seq, Type: ev.Type, JSON: body}
}

func (a *App) accepted(ev *event.Event, follow bool) {
	if follow {
		a.follow()
	}
	a.st.Apply(ev, a.clock)
	if a.cfg.Extensions != nil {
		a.cfg.Extensions.Publish(eventForHost(ev))
	}
}

func (a *App) extensionMessage(m ExtensionMessage) bool {
	panelChanged := a.panelMessage(m)
	if m.Removed {
		for id, owner := range a.extActions {
			if owner == m.Extension {
				delete(a.extActions, id)
			}
		}
	}
	if m.Notice != "" {
		a.st.AppendNotice(state.NoticeQuiescent, m.Extension+": "+m.Notice)
	}
	if m.Emit != nil {
		if err := a.acceptExtensionEvent(m.Extension, *m.Emit); err != nil {
			a.st.AppendNotice(state.NoticeQuiescent, m.Extension+": "+err.Error())
		}
	}
	if m.Answer != nil {
		if err := a.acceptExtensionAnswer(m.Extension, *m.Answer); err != nil {
			a.st.AppendNotice(state.NoticeQuiescent, m.Extension+": "+err.Error())
		}
	}
	for _, action := range m.Actions {
		if action.Name == "" || action.Description == "" || strings.Contains(action.Name, ":") {
			continue
		}
		id := Action("ext:" + m.Extension + ":" + action.Name)
		if _, core := actionIndex[id]; core {
			continue
		}
		if owner, exists := a.extActions[id]; !exists || owner == m.Extension {
			a.extActions[id] = m.Extension
		}
	}
	return panelChanged || m.Notice != "" || m.Emit != nil || m.Answer != nil || len(m.Actions) > 0 || m.Removed
}

func (a *App) acceptExtensionEvent(name string, p ExtensionEvent) error {
	if p.Scope == "" || (p.Scope != a.scope && p.Scope != "run:local") {
		return errors.New("invalid event scope")
	}
	var payload any
	switch p.Type {
	case event.RunPrompt:
		payload = &event.RunPromptPayload{}
	case event.AgentSteered:
		payload = &event.AgentSteeredPayload{}
	case event.AgentNotified:
		payload = &event.AgentNotifiedPayload{}
	default:
		return fmt.Errorf("event type %q is not safe for extensions", p.Type)
	}
	if len(p.Payload) == 0 || json.Unmarshal(p.Payload, payload) != nil {
		return errors.New("invalid event payload")
	}
	ev := &event.Event{Seq: a.seq, Type: p.Type, Scope: a.scope, Source: event.SourceHuman, Actor: name, Payload: append(json.RawMessage(nil), p.Payload...)}
	a.seq++
	a.accepted(ev, true)
	return nil
}

func (a *App) acceptExtensionAnswer(name string, p ExtensionAnswer) error {
	in := a.st.OpenInbox()
	if in == nil || in.ID != p.InboxID {
		return errors.New("inbox is not current")
	}
	var text string
	switch p.Kind {
	case "answer":
		if err := json.Unmarshal(p.Answer, &text); err != nil || strings.TrimSpace(text) == "" {
			return errors.New("invalid inbox answer")
		}
	case "approve", "reject":
		if len(p.Answer) != 0 {
			return errors.New("decision must not include an answer")
		}
		text = p.Kind
	default:
		return errors.New("invalid inbox decision kind")
	}
	payload, _ := json.Marshal(event.InboxRepliedPayload{InboxID: in.ID, Text: text})
	ev := &event.Event{Seq: a.seq, Type: event.InboxReplied, Scope: a.scope, Source: event.SourceHuman, Actor: name, Payload: payload}
	a.seq++
	a.accepted(ev, true)
	a.skip = in.ID
	a.release(in.ID)
	return nil
}

func renderConsent(c ExtensionConsent, vp ui.Viewport) ui.Frame {
	f := ui.Frame{Width: vp.Width, Height: vp.Height, Cursor: ui.Cursor{Hidden: true}}
	rows := []ui.Line{
		{{Text: "Extension consent", Style: "team.name"}},
		{{Text: c.Name + " " + c.Version, Style: "team.state"}},
		{{Text: "Executable  " + c.Executable, Style: "tasks.meta"}},
		{{Text: "Arguments   " + strings.Join(c.Args, " "), Style: "tasks.meta"}},
		{{Text: "Capabilities", Style: "team.name"}},
	}
	for _, capability := range c.Capabilities {
		rows = append(rows, ui.Line{{Text: "  " + capability, Style: "team.state"}})
	}
	rows = append(rows, ui.Line{{Text: "Y grant all  ·  N reject for this session", Style: "team.meta"}})
	if vp.Height > 0 && len(rows) > vp.Height {
		rows = rows[:vp.Height]
	}
	f.Live = rows
	return f
}
