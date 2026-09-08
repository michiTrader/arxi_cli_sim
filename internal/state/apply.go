package state

import (
	"fmt"
	"time"

	"arxi.local/sim/internal/event"
)

// applyRest handles tools, inboxes, blocking and budget. Split from Apply only to
// keep either half readable.
func (s *State) applyRest(ev *event.Event, at time.Duration) {
	switch ev.Type {
	case event.ToolCall:
		var p event.ToolCallPayload
		if ev.Decode(&p) != nil {
			return
		}
		s.appendItem(Item{
			Kind: KindTool, Seq: ev.Seq, ID: fmt.Sprintf("tool-%d", ev.Seq), Actor: ev.Actor,
			Tool: p.Tool, Args: p.Args, Status: ToolPending, Open: true, StartedAt: at,
		})
	case event.ToolCallCompleted:
		var p event.ToolResultPayload
		if ev.Decode(&p) != nil {
			return
		}
		if it := s.findOpenTool(p.Tool, ev.Actor); it != nil {
			it.Open, it.EndedAt, it.Summary = false, at, p.Result.Summary
			it.Status = ToolOK
			if p.Result.ExitCode != nil && *p.Result.ExitCode != 0 {
				it.Status = ToolFailed
			}
			// An edit that reports its rows gets to describe itself. The wording is
			// derived only when the log stayed silent, so a runtime that has something
			// better to say than "Added 3 lines" keeps saying it.
			if it.Diff = buildDiff(p.Result.Diff); it.Diff != nil && it.Summary == "" {
				it.Summary = it.Diff.Summary()
			}
		}
	case event.ToolCallDenied:
		var p event.ToolResultPayload
		if ev.Decode(&p) != nil {
			return
		}
		if it := s.findOpenTool(p.Tool, ev.Actor); it != nil {
			it.Open, it.EndedAt, it.Status, it.Policy = false, at, ToolDenied, p.Policy
			it.Summary = "denied by policy " + p.Policy
		}
	case event.InboxCreated:
		var p event.InboxCreatedPayload
		if ev.Decode(&p) != nil {
			return
		}
		s.Inboxes = append(s.Inboxes, &Inbox{
			ID: p.InboxID, Kind: p.Kind, Question: p.Question,
			Agent: p.Agent, OnTimeout: p.OnTimeout, CreatedAt: at,
		})
	case event.InboxReplied, event.InboxTimeout:
		var p event.InboxRepliedPayload
		if ev.Decode(&p) != nil {
			return
		}
		if in := s.findInbox(p.InboxID); in != nil {
			in.Answered, in.Answer = true, p.Text
			if ev.Type == event.InboxTimeout {
				in.Answer = in.OnTimeout + " (timed out)"
			}
			s.AppendNotice(NoticeApproval, in.Question+" "+in.Answer)
		}
	case event.AgentBlocked:
		var p event.AgentBlockedPayload
		if ev.Decode(&p) != nil {
			return
		}
		s.Blocked = &Blocked{Agent: ev.Actor, On: p.BlockedOn, Ref: p.BlockedRef}
		if m := s.member(ev.Actor); m != nil {
			m.Busy = true
			m.Blocked = &Blocked{Agent: ev.Actor, On: p.BlockedOn, Ref: p.BlockedRef}
		}
		// A pause needs a reason on screen, and exactly one. An approval draws its
		// own question with the answer keys on it; a conflict or a spent budget has
		// just written the detail one line up. What is left is the case nothing
		// explained, where the run would otherwise sit still with no reason given.
		if p.BlockedOn != "" && p.BlockedOn != "approval" && !s.lastNoticeExplains() {
			s.AppendNotice(NoticeBlocked, who(ev.Actor)+" is blocked on "+p.BlockedOn)
		}
	case event.LockAcquired, event.LockReleased:
		var p event.LockPayload
		if ev.Decode(&p) != nil || p.Resource == "" {
			return
		}
		holder := p.Holder
		if holder == "" {
			holder = who(ev.Actor)
		}
		verb := "holds"
		if ev.Type == event.LockReleased {
			verb = "released"
		}
		text := holder + " " + verb + " " + p.Resource
		if p.Mode != "" && ev.Type == event.LockAcquired {
			text += " (" + p.Mode + ")"
		}
		s.AppendNotice(NoticeLock, text)
	case event.ResourceConflict:
		var p event.ConflictPayload
		if ev.Decode(&p) != nil || p.Resource == "" {
			return
		}
		waiter := p.Waiter
		if waiter == "" {
			waiter = who(ev.Actor)
		}
		text := waiter + " waits for " + p.Resource
		if p.Holder != "" {
			text += ", held by " + p.Holder
		}
		s.AppendNotice(NoticeConflict, text)
	case event.AgentUnblocked:
		s.Blocked = nil
		if m := s.member(ev.Actor); m != nil {
			m.Blocked = nil
			m.Busy = true
		}
	case event.AgentSteered:
		var p event.AgentSteeredPayload
		if ev.Decode(&p) == nil {
			if m := s.member(ev.Actor); m != nil {
				m.Steered, m.SteeredTo = p.Text, p.To
			}
		}
	case event.AgentNotified:
		var p event.AgentNotifiedPayload
		if ev.Decode(&p) == nil {
			if m := s.member(ev.Actor); m != nil {
				m.Notified, m.NotifiedTo = p.Text, p.To
			}
		}
	case event.RunQuiescent:
		var p event.RunQuiescentPayload
		if ev.Decode(&p) != nil {
			return
		}
		s.Quiescent = p.Diagnosis
		s.Active = false
		s.AppendNotice(NoticeQuiescent, p.Diagnosis)
	case event.BudgetWarning, event.BudgetExceeded:
		var p event.BudgetPayload
		if ev.Decode(&p) != nil {
			return
		}
		if p.LimitUSD > 0 {
			s.BudgetUSD = p.LimitUSD
		}
		if p.SpentUSD > 0 {
			s.SpentUSD = p.SpentUSD
		}
		verb := "warning"
		if ev.Type == event.BudgetExceeded {
			verb = "exceeded"
		}
		s.AppendNotice(NoticeBudget, fmt.Sprintf("budget %s: $%.4f of $%.2f", verb, s.SpentUSD, s.BudgetUSD))
	}
}

// findOpenTool matches a result to the call it belongs to. arxi's log carries no
// call id, so the only keys on the wire are the tool name, the actor, and the order
// events arrive in — and with a four-member run the name alone is not a key: scout's
// `bash` test and builder's `bash` build are open at the same time, and matching the
// latest by name alone binds scout's failure to builder's build. Two calls by one
// actor still complete in the order they were issued, so oldest-first within the
// actor resolves every case a call id would. The name-only fallback is for a result
// the runtime reports without an actor: guessing leaves the block visibly wrong,
// while dropping it leaves a spinner running forever.
func (s *State) findOpenTool(tool, actor string) *Item {
	var fallback *Item
	for i := range s.Items {
		it := &s.Items[i]
		if it.Kind != KindTool || !it.Open || it.Tool != tool {
			continue
		}
		if actor != "" && it.Actor == actor {
			return it
		}
		if fallback == nil {
			fallback = it
		}
	}
	return fallback
}

func (s *State) findInbox(id string) *Inbox {
	for _, in := range s.Inboxes {
		if in.ID == id {
			return in
		}
	}
	return nil
}

// who names an actor for a line of prose. A runtime event can arrive with no actor
// at all, and a line that opens "is blocked on lock" reads like a bug in the TUI
// rather than a gap in the log.
func who(actor string) string {
	if actor == "" {
		return "an agent"
	}
	return actor
}

// lastNoticeExplains reports whether the line already on screen says why the run is
// about to stop.
func (s *State) lastNoticeExplains() bool {
	if len(s.Items) == 0 {
		return false
	}
	it := s.Items[len(s.Items)-1]
	if it.Kind != KindNotice {
		return false
	}
	return it.Notice == NoticeConflict || it.Notice == NoticeBudget
}
