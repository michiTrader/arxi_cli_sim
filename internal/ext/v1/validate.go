package v1

import (
	"fmt"
	"regexp"
	"strings"

	"arxi.local/sim/internal/ext"
)

var actionName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// ProtocolError is a refusal suitable for an ext/v1 error response.
type ProtocolError struct {
	Code    string
	Message string
	ID      string
}

func (e *ProtocolError) Error() string        { return e.Code + ": " + e.Message }
func (e *ProtocolError) Is(target error) bool { return target == ErrInvalidMessage }

// Validate checks a typed message without session-specific authorization.
func Validate(message Message) error {
	if message == nil {
		return invalid("invalid_message", "message is nil")
	}
	switch value := message.(type) {
	case Hello:
		return validateHello(value)
	case *Hello:
		return validateHello(*value)
	case Ready:
		return validateReady(value)
	case *Ready:
		return validateReady(*value)
	case Subscribe, *Subscribe:
		var request Subscribe
		if p, ok := value.(*Subscribe); ok {
			request = *p
		} else {
			request = value.(Subscribe)
		}
		if request.After < 0 {
			return invalid("invalid_message", "after must not be negative")
		}
		return uniqueStrings("event type", request.Types)
	case Event, *Event:
		var event Event
		if p, ok := value.(*Event); ok {
			event = *p
		} else {
			event = value.(Event)
		}
		if event.Seq <= 0 || event.Type == "" || len(event.Event) == 0 {
			return invalid("invalid_message", "event requires positive seq, type, and event")
		}
	case EmitEvent, *EmitEvent:
		var event EmitEvent
		if p, ok := value.(*EmitEvent); ok {
			event = *p
		} else {
			event = value.(EmitEvent)
		}
		if strings.TrimSpace(event.Type) == "" || strings.TrimSpace(event.Scope) == "" || len(event.Payload) == 0 {
			return invalid("invalid_message", "events.emit requires type, scope, and payload")
		}
	case EventsDropped, *EventsDropped:
		var dropped EventsDropped
		if p, ok := value.(*EventsDropped); ok {
			dropped = *p
		} else {
			dropped = value.(EventsDropped)
		}
		if dropped.Count <= 0 || dropped.AfterSeq < 0 {
			return invalid("invalid_message", "events.dropped requires positive count and non-negative after_seq")
		}
	case InboxAnswer, *InboxAnswer:
		var answer InboxAnswer
		if p, ok := value.(*InboxAnswer); ok {
			answer = *p
		} else {
			answer = value.(InboxAnswer)
		}
		if answer.InboxID == "" {
			return invalid("invalid_message", "inbox_id is required")
		}
		switch answer.Kind {
		case "answer":
			if len(answer.Answer) == 0 {
				return invalid("invalid_message", "answer kind requires answer")
			}
		case "approve", "reject":
			if len(answer.Answer) != 0 {
				return invalid("invalid_message", answer.Kind+" must not include answer")
			}
		default:
			return invalid("invalid_message", "kind must be answer, approve, or reject")
		}
	case RegisterActions, *RegisterActions:
		var register RegisterActions
		if p, ok := value.(*RegisterActions); ok {
			register = *p
		} else {
			register = value.(RegisterActions)
		}
		if len(register.Actions) == 0 {
			return invalid("invalid_message", "actions must not be empty")
		}
		seen := map[string]bool{}
		for _, action := range register.Actions {
			if !actionName.MatchString(action.Name) || strings.TrimSpace(action.Description) == "" {
				return invalid("invalid_message", "each action requires a valid name and description")
			}
			if seen[action.Name] {
				return invalid("invalid_message", fmt.Sprintf("duplicate action %q", action.Name))
			}
			seen[action.Name] = true
		}
	case InvokeAction, *InvokeAction:
		var invoke InvokeAction
		if p, ok := value.(*InvokeAction); ok {
			invoke = *p
		} else {
			invoke = value.(InvokeAction)
		}
		if !actionName.MatchString(invoke.Action) {
			return invalid("invalid_message", "actions.invoke requires a valid local action name")
		}
	case Shutdown, *Shutdown:
		return nil
	case Error, *Error:
		var protocolError Error
		if p, ok := value.(*Error); ok {
			protocolError = *p
		} else {
			protocolError = value.(Error)
		}
		switch protocolError.Code {
		case "unknown_type", "not_declared", "not_granted", "invalid_message", "protocol_mismatch":
		default:
			return invalid("invalid_message", fmt.Sprintf("unknown error code %q", protocolError.Code))
		}
		if strings.TrimSpace(protocolError.Message) == "" {
			return invalid("invalid_message", "error message is required")
		}
	default:
		return invalid("unknown_type", fmt.Sprintf("unsupported message %T", message))
	}
	return nil
}

func validateHello(hello Hello) error {
	if hello.Protocol != Protocol {
		return invalid("protocol_mismatch", fmt.Sprintf("protocol must be %q", Protocol))
	}
	return validateCapabilities(hello.Capabilities)
}
func validateReady(ready Ready) error {
	if ready.Protocol != Protocol {
		return invalid("protocol_mismatch", fmt.Sprintf("protocol must be %q", Protocol))
	}
	if !actionName.MatchString(ready.Name) || strings.TrimSpace(ready.Version) == "" {
		return invalid("invalid_message", "ready requires a valid name and version")
	}
	return validateCapabilities(ready.Capabilities)
}
func validateCapabilities(values []ext.Capability) error {
	seen := map[ext.Capability]bool{}
	for _, capability := range values {
		if !ext.KnownCapability(capability) {
			return invalid("invalid_message", fmt.Sprintf("unknown capability %q", capability))
		}
		if seen[capability] {
			return invalid("invalid_message", fmt.Sprintf("duplicate capability %q", capability))
		}
		seen[capability] = true
	}
	return nil
}
func uniqueStrings(label string, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return invalid("invalid_message", label+" must not be empty")
		}
		if seen[value] {
			return invalid("invalid_message", fmt.Sprintf("duplicate %s %q", label, value))
		}
		seen[value] = true
	}
	return nil
}
func invalid(code, message string) error { return &ProtocolError{Code: code, Message: message} }

// Authorize verifies that an operation was both declared in the manifest and
// granted by the user. It intentionally has no application dependencies.
func Authorize(capability ext.Capability, declared, granted ext.CapabilitySet) error {
	if !ext.KnownCapability(capability) {
		return &ProtocolError{Code: "not_declared", Message: fmt.Sprintf("unknown capability %q", capability)}
	}
	if !declared.Has(capability) {
		return &ProtocolError{Code: "not_declared", Message: fmt.Sprintf("capability %q was not declared", capability)}
	}
	if !granted.Has(capability) {
		return &ProtocolError{Code: "not_granted", Message: fmt.Sprintf("capability %q was not granted", capability)}
	}
	return nil
}
