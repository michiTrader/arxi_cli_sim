// Package v2 defines the ext/v2 NDJSON wire protocol. It contains values and
// validation only; transport lifecycle belongs to the parent ext package.
package v2

import (
	"encoding/json"

	"arxi.local/sim/internal/ext"
)

const Protocol = "ext/v2"

const (
	TypeHello        = "hello"
	TypeReady        = "ready"
	TypeEvent        = "event"
	TypeSubscribe    = "events.subscribe"
	TypeEmit         = "events.emit"
	TypeInboxAnswer  = "inbox.answer"
	TypeActionAdd    = "actions.register"
	TypeActionInvoke = "actions.invoke"
	TypeError        = "error"
	TypeDropped      = "events.dropped"
	TypeShutdown     = "shutdown"
	TypeViewUpdate   = "view.update"
	TypeViewClose    = "view.close"
	TypeViewResize   = "view.resize"
	TypeViewFocus    = "view.focus"
	TypeViewBlur     = "view.blur"
	TypeViewInput    = "view.input"
)

// Envelope is one NDJSON object. Payload is decoded according to Type.
type Envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Message is implemented by all typed ext/v2 messages.
type Message interface {
	MessageType() string
}

// Hello starts every host session.
type Hello struct {
	Protocol     string           `json:"protocol"`
	Capabilities []ext.Capability `json:"capabilities"`
}

func (Hello) MessageType() string { return TypeHello }

// Ready completes the handshake and requests manifest-declared capabilities.
type Ready struct {
	Protocol     string           `json:"protocol"`
	Name         string           `json:"name"`
	Version      string           `json:"version"`
	Capabilities []ext.Capability `json:"capabilities"`
}

func (Ready) MessageType() string { return TypeReady }

// Subscribe requests events by exact type. Empty Types means all events.
type Subscribe struct {
	Types []string `json:"types,omitempty"`
	After int      `json:"after,omitempty"`
}

func (Subscribe) MessageType() string { return TypeSubscribe }

// Event is the stable event tap: sequence, type, and opaque event bytes.
type Event struct {
	Seq   int             `json:"seq"`
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event"`
}

func (Event) MessageType() string { return TypeEvent }

// EmitEvent proposes an attributed event. The host assigns seq, source, and
// actor so extensions cannot forge ordering or attribution.
type EmitEvent struct {
	Type    string          `json:"type"`
	Scope   string          `json:"scope"`
	Payload json.RawMessage `json:"payload"`
}

func (EmitEvent) MessageType() string { return TypeEmit }

// EventsDropped notifies an extension that the host discarded events rather
// than block its fold.
type EventsDropped struct {
	Count    int `json:"count"`
	AfterSeq int `json:"after_seq"`
}

func (EventsDropped) MessageType() string { return TypeDropped }

// InboxAnswer proposes a host/v1-shaped inbox decision. The host binds and
// attributes it; an extension cannot choose source or actor.
type InboxAnswer struct {
	InboxID string          `json:"inbox_id"`
	Kind    string          `json:"kind"`
	Answer  json.RawMessage `json:"answer,omitempty"`
}

func (InboxAnswer) MessageType() string { return TypeInboxAnswer }

// Action describes one extension-owned action. The host constructs its full
// ext:<extension>:<name> identifier.
type Action struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type RegisterActions struct {
	Actions []Action `json:"actions"`
}

func (RegisterActions) MessageType() string { return TypeActionAdd }

// InvokeAction asks the extension to execute one action registered by this
// process. Action is the local registered name; Args is the raw slash argument.
type InvokeAction struct {
	Action string `json:"action"`
	Args   string `json:"args,omitempty"`
}

func (InvokeAction) MessageType() string { return TypeActionInvoke }

// Error is a machine-readable refusal. Codes are a closed vocabulary.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
}

func (Error) MessageType() string { return TypeError }

// Shutdown asks an extension to stop cleanly before the host terminates it.
type Shutdown struct{}

func (Shutdown) MessageType() string { return TypeShutdown }

// Role is a closed semantic hint; styling remains host-owned.
type Role string

const (
	RoleText     Role = "text"
	RoleMuted    Role = "muted"
	RoleEmphasis Role = "emphasis"
	RoleSuccess  Role = "success"
	RoleWarning  Role = "warning"
	RoleError    Role = "error"
	RoleTitle    Role = "title"
	RoleCode     Role = "code"
)

type Span struct {
	Text string `json:"text"`
	Role Role   `json:"role"`
}

type Row struct {
	ID    string `json:"id"`
	Spans []Span `json:"spans"`
}

// ViewUpdate replaces the complete cached panel representation for ID.
type ViewUpdate struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Rows   []Row  `json:"rows"`
}

func (ViewUpdate) MessageType() string { return TypeViewUpdate }

type ViewClose struct {
	ID string `json:"id"`
}

func (ViewClose) MessageType() string { return TypeViewClose }

type ViewResize struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (ViewResize) MessageType() string { return TypeViewResize }

type ViewFocus struct {
	ID string `json:"id"`
}

func (ViewFocus) MessageType() string { return TypeViewFocus }

type ViewBlur struct {
	ID string `json:"id"`
}

func (ViewBlur) MessageType() string { return TypeViewBlur }

// Input is one normalized panel-local input event.
type Input struct {
	Kind   string `json:"kind"`
	Key    string `json:"key,omitempty"`
	Action string `json:"action,omitempty"`
	Text   string `json:"text,omitempty"`
	X      int    `json:"x,omitempty"`
	Y      int    `json:"y,omitempty"`
	DX     int    `json:"dx,omitempty"`
	DY     int    `json:"dy,omitempty"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}
type ViewInput struct {
	ID    string `json:"id"`
	Input Input  `json:"input"`
}

func (ViewInput) MessageType() string { return TypeViewInput }
