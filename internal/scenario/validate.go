package scenario

import (
	"encoding/json"
	"fmt"
	"sort"

	"arxi.local/sim/internal/event"
)

// Validate reports everything wrong with a loaded scenario. It is the executable
// form of the event contract: a scenario that passes here is one the real runtime
// could have produced, so the TUI is never tuned against impossible input.
func Validate(sc *Scenario) []error {
	var problems []error
	bad := func(s Step, format string, args ...any) {
		problems = append(problems, fmt.Errorf("line %d: "+format, append([]any{s.Line}, args...)...))
	}

	wantSeq := 1
	openParts := map[string]event.PartKind{}
	seenParts := map[string]bool{}
	openInbox := map[string]bool{}
	tasks := map[string]event.TaskStatus{}

	for _, st := range sc.Steps {
		if st.IsBarrier() {
			if st.Await != AwaitPrompt && !openInbox[st.Await] {
				bad(st, "awaits %q, which no open inbox provides", st.Await)
			}
			continue
		}
		ev := st.Event
		if ev.Seq != wantSeq {
			bad(st, "seq is %d, expected %d (must be contiguous from 1)", ev.Seq, wantSeq)
		}
		wantSeq = ev.Seq + 1
		if ev.Type == "" {
			bad(st, "event without a type")
			continue
		}
		if !event.Known[ev.Type] {
			bad(st, "unknown event type %q", ev.Type)
		}
		if ev.Scope == "" {
			bad(st, "%s without a scope", ev.Type)
		}
		if !event.KnownSource(ev.Source) {
			bad(st, "%s has source %q, which is not in the catalogue", ev.Type, ev.Source)
		}
		if len(ev.Payload) == 0 {
			bad(st, "%s without a payload (use {} when there is nothing to say)", ev.Type)
			continue
		}

		switch ev.Type {
		case event.AgentBlocked:
			var p event.AgentBlockedPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
			} else if len(p.BlockedRef) == 0 {
				// Without blocked_ref a stuck run cannot be explained, which is the
				// one thing arxi promises to always be able to do.
				bad(st, "agent.blocked without blocked_ref")
			}
		case event.RunQuiescent:
			var p event.RunQuiescentPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
			} else if p.Diagnosis == "" {
				bad(st, "run.quiescent without a diagnosis")
			}
		case event.LLMResponse:
			var p event.ResponsePayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
				continue
			}
			if (p.ContextUsed == nil) != (p.ContextCapacity == nil) {
				bad(st, "llm.response must carry context_used and context_capacity together")
				continue
			}
			if p.ContextUsed != nil {
				if *p.ContextUsed < 0 {
					bad(st, "llm.response has negative context_used")
				}
				if *p.ContextCapacity <= 0 {
					bad(st, "llm.response has non-positive context_capacity")
				} else if *p.ContextUsed > *p.ContextCapacity {
					bad(st, "llm.response context_used exceeds context_capacity")
				}
			}
		case event.LLMPart:
			var p event.PartPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
				continue
			}
			if p.PartID == "" {
				bad(st, "llm.part without a part_id")
				continue
			}
			if seenParts[p.PartID] {
				bad(st, "part %q declared twice", p.PartID)
				continue
			}
			if p.Kind != event.PartThinking && p.Kind != event.PartText {
				bad(st, "part %q has unknown kind %q", p.PartID, p.Kind)
			}
			seenParts[p.PartID] = true
			openParts[p.PartID] = p.Kind
		case event.LLMDelta:
			var p event.DeltaPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
				continue
			}
			if _, open := openParts[p.PartID]; !open {
				bad(st, "delta for part %q, which is not open", p.PartID)
			}
		case event.LLMPartDone:
			var p event.DeltaPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
				continue
			}
			if _, open := openParts[p.PartID]; !open {
				bad(st, "part_done for part %q, which is not open", p.PartID)
				continue
			}
			delete(openParts, p.PartID)
		case event.InboxCreated:
			var p event.InboxCreatedPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
				continue
			}
			if p.InboxID == "" {
				bad(st, "inbox.created without an inbox_id")
				continue
			}
			openInbox[p.InboxID] = true
		case event.InboxReplied, event.InboxTimeout:
			var p event.InboxRepliedPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
				continue
			}
			if !openInbox[p.InboxID] {
				bad(st, "%s for inbox %q, which is not open", ev.Type, p.InboxID)
				continue
			}
			delete(openInbox, p.InboxID)
		case event.SimTaskCreated:
			var p event.TaskCreatedPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
				continue
			}
			if p.TaskID == "" {
				bad(st, "sim.task.created without a task_id")
				continue
			}
			if p.Title == "" {
				bad(st, "sim.task.created for %q without a title", p.TaskID)
				continue
			}
			if _, exists := tasks[p.TaskID]; exists {
				bad(st, "task %q created twice", p.TaskID)
				continue
			}
			status, ok := event.InitialTaskStatus(p.Status)
			if !ok {
				bad(st, "task %q created with invalid initial status %q", p.TaskID, *p.Status)
				continue
			}
			tasks[p.TaskID] = status
		case event.SimTaskUpdated:
			var p event.TaskUpdatedPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
				continue
			}
			status, exists := tasks[p.TaskID]
			if p.TaskID == "" {
				bad(st, "sim.task.updated without a task_id")
			} else if !exists {
				bad(st, "sim.task.updated for task %q, which does not exist", p.TaskID)
			} else if status == event.TaskCompleted {
				bad(st, "sim.task.updated for completed task %q", p.TaskID)
			} else if p.Title != nil && *p.Title == "" {
				bad(st, "sim.task.updated clears title for task %q", p.TaskID)
			}
		case event.SimTaskStatusChanged:
			var p event.TaskStatusChangedPayload
			if err := ev.Decode(&p); err != nil {
				bad(st, "%v", err)
				continue
			}
			status, exists := tasks[p.TaskID]
			if p.TaskID == "" {
				bad(st, "sim.task.status_changed without a task_id")
			} else if !exists {
				bad(st, "sim.task.status_changed for task %q, which does not exist", p.TaskID)
			} else if !event.CanChangeTaskStatus(status, p.Status) {
				bad(st, "task %q cannot change status from %q to %q", p.TaskID, status, p.Status)
			} else {
				tasks[p.TaskID] = p.Status
			}
		default:
			// Every other type only has to carry a JSON object, which it does.
			var raw map[string]json.RawMessage
			if err := ev.Decode(&raw); err != nil {
				bad(st, "%v", err)
			}
		}
	}
	unclosed := make([]string, 0, len(openParts))
	for id := range openParts {
		unclosed = append(unclosed, id)
	}
	sort.Strings(unclosed)
	for _, id := range unclosed {
		problems = append(problems, fmt.Errorf("part %q is never closed by llm.part_done", id))
	}
	return problems
}
