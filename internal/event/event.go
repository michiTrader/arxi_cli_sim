// Package event defines the run-log wire format accepted for playback: one JSON
// object per line and one immutable fact per object. Its core catalogue mirrors
// arxi, while types and fields documented as proposals are simulator replay
// extensions rather than claims about the current arxi event contract.
package event

import (
	"encoding/json"
	"fmt"
)

// Source is who caused the event. The catalogue is closed on purpose: a source
// the runtime cannot produce is a bug in the scenario, not a new feature.
type Source string

const (
	SourceHuman   Source = "human"
	SourceAgent   Source = "agent"
	SourceRuntime Source = "runtime"
	SourceTrigger Source = "trigger"
)

// Event is a single line of the log.
type Event struct {
	Seq     int             `json:"seq"`
	Type    string          `json:"type"`
	Scope   string          `json:"scope"`
	Source  Source          `json:"source"`
	Actor   string          `json:"actor,omitempty"`
	TS      string          `json:"ts,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// Decode unmarshals the payload into v.
func (e Event) Decode(v any) error {
	if len(e.Payload) == 0 {
		return fmt.Errorf("event %d (%s): empty payload", e.Seq, e.Type)
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("event %d (%s): %w", e.Seq, e.Type, err)
	}
	return nil
}

// Event types produced by the arxi runtime.
const (
	RunStarted   = "run.started"
	RunPrompt    = "run.prompt"
	RunPaused    = "run.paused"
	RunUnpaused  = "run.unpaused"
	RunCancelled = "run.cancelled"
	RunExpired   = "run.expired"
	RunQuiescent = "run.quiescent"
	RunResult    = "run.result"

	StageEntered   = "stage.entered"
	StageSubmitted = "stage.submitted"
	StageAdvanced  = "stage.advanced"
	StageTimeout   = "stage.timeout"

	AgentActivated = "agent.activated"
	AgentSteered   = "agent.steered"
	AgentNotified  = "agent.notified"
	AgentTurnDone  = "agent.turn_done"
	AgentBlocked   = "agent.blocked"
	AgentUnblocked = "agent.unblocked"
	AgentFailed    = "agent.failed"

	ToolCall          = "tool.call"
	ToolCallCompleted = "tool.call_completed"
	ToolCallDenied    = "tool.call_denied"

	LockAcquired     = "lock.acquired"
	LockReleased     = "lock.released"
	ResourceConflict = "resource.conflict"

	BudgetWarning  = "budget.warning"
	BudgetExceeded = "budget.exceeded"

	InboxCreated = "inbox.created"
	InboxReplied = "inbox.replied"
	InboxTimeout = "inbox.timeout"
	TimerTick    = "timer.tick"
)

// The llm.* family is a simulator replay extension that carries assistant prose.
// The current arxi log records that a turn happened and what it cost, but not what
// was said; a TUI cannot be replayed from that. These types close the gap and stay
// additive rather than claiming to be events observed from arxi.
const (
	LLMTurnStarted = "llm.turn_started"
	LLMPart        = "llm.part"
	LLMDelta       = "llm.delta"
	LLMPartDone    = "llm.part_done"
	LLMResponse    = "llm.response"
)

// The sim.task.* family is an experimental simulator extension. Arxi does not
// currently expose a Tasks contract; these names must not be read as upstream
// runtime events.
const (
	SimTaskCreated       = "sim.task.created"
	SimTaskUpdated       = "sim.task.updated"
	SimTaskStatusChanged = "sim.task.status_changed"
)

// Known is every event type the simulator accepts.
var Known = map[string]bool{
	RunStarted: true, RunPrompt: true, RunPaused: true, RunUnpaused: true,
	RunCancelled: true, RunExpired: true, RunQuiescent: true, RunResult: true,
	StageEntered: true, StageSubmitted: true, StageAdvanced: true, StageTimeout: true,
	AgentActivated: true, AgentSteered: true, AgentNotified: true, AgentTurnDone: true,
	AgentBlocked: true, AgentUnblocked: true, AgentFailed: true,
	ToolCall: true, ToolCallCompleted: true, ToolCallDenied: true,
	LockAcquired: true, LockReleased: true, ResourceConflict: true,
	BudgetWarning: true, BudgetExceeded: true,
	InboxCreated: true, InboxReplied: true, InboxTimeout: true, TimerTick: true,
	LLMTurnStarted: true, LLMPart: true, LLMDelta: true, LLMPartDone: true,
	LLMResponse:    true,
	SimTaskCreated: true, SimTaskUpdated: true, SimTaskStatusChanged: true,
}

// KnownSource reports whether s is in the closed source catalogue.
func KnownSource(s Source) bool {
	switch s {
	case SourceHuman, SourceAgent, SourceRuntime, SourceTrigger:
		return true
	}
	return false
}
