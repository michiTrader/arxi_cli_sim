// Package ext defines the narrow, transport-independent extension host surface.
// Process lifecycle and application integration build on this package; protocol
// values live in the v1 subpackage.
package ext

import "sort"

// Capability names an operation an extension may request. The vocabulary is
// closed so manifests and handshakes cannot silently grant future authority.
type Capability string

const (
	EventsSubscribe Capability = "events.subscribe"
	EventsEmit      Capability = "events.emit"
	InboxAnswer     Capability = "inbox.answer"
	ActionsRegister Capability = "actions.register"
	PanelRender     Capability = "panel.render"
)

var capabilities = []Capability{
	ActionsRegister,
	EventsEmit,
	EventsSubscribe,
	InboxAnswer,
}

// Capabilities returns the protocol capability vocabulary in lexical order.
// The returned slice is independent and may be modified by the caller.
func Capabilities() []Capability {
	out := append([]Capability(nil), capabilities...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// KnownCapability reports whether capability belongs to this protocol version.
func KnownCapability(capability Capability) bool {
	switch capability {
	case EventsSubscribe, EventsEmit, InboxAnswer, ActionsRegister:
		return true
	default:
		return false
	}
}

// CapabilitiesFor returns the closed vocabulary for a protocol revision.
func CapabilitiesFor(protocol string) []Capability {
	out := Capabilities()
	if protocol == "ext/v2" {
		out = append(out, PanelRender)
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	}
	return out
}

// KnownCapabilityFor reports whether capability belongs to protocol.
func KnownCapabilityFor(protocol string, capability Capability) bool {
	return KnownCapability(capability) || protocol == "ext/v2" && capability == PanelRender
}

// CapabilitySet is an immutable-by-convention set used at the consent seam.
type CapabilitySet map[Capability]struct{}

// NewCapabilitySet copies capabilities into a set.
func NewCapabilitySet(capabilities ...Capability) CapabilitySet {
	set := make(CapabilitySet, len(capabilities))
	for _, capability := range capabilities {
		set[capability] = struct{}{}
	}
	return set
}

// Has reports whether capability is in the set.
func (set CapabilitySet) Has(capability Capability) bool {
	_, ok := set[capability]
	return ok
}

// Equal reports exact set equality.
func (set CapabilitySet) Equal(other CapabilitySet) bool {
	if len(set) != len(other) {
		return false
	}
	for capability := range set {
		if !other.Has(capability) {
			return false
		}
	}
	return true
}
