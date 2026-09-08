// Package state folds a run log into everything the TUI needs to draw. It is the
// simulator's half of arxi's own shape, Decide(State, Event) -> State': one pure
// step per event, no reaching back into the log, no I/O.
package state

import (
	"strconv"
	"time"

	"arxi.local/sim/internal/event"
)

// Member is one blueprint member plus the live facts needed by the team status
// and monitor. Members remain in blueprint order; a runtime actor absent from
// that blueprint is appended with only Name populated.
type Member struct {
	Name       string
	Role       string
	Model      string
	Advisory   bool
	Tools      []string
	Activation string
	Stages     []string

	Busy       bool
	Blocked    *Blocked
	Error      string
	Steered    string
	SteeredTo  string
	Notified   string
	NotifiedTo string
}

// Task is the normalized replay state of one experimental sim.task.* task.
// Tasks remain in creation order and are updated in place by TaskID.
type Task struct {
	ID         string
	Title      string
	Detail     string
	Owner      string
	Status     event.TaskStatus
	CreatedSeq int
	UpdatedSeq int
	CreatedAt  time.Duration
	UpdatedAt  time.Duration
}

// State is the whole world the renderer is allowed to look at.
type State struct {
	RunID        string
	BlueprintSHA string
	CWD          string
	GitBranch    string
	Stage        string
	StageIndex   int
	Actor        string // the agent this simulation follows

	BudgetUSD       float64
	SpentUSD        float64
	TokensIn        int
	TokensOut       int
	ContextUsed     int
	ContextCapacity int
	Model           string
	Turn            int

	Items   []Item
	Inboxes []*Inbox // open first-to-last; answered ones stay for the transcript
	Tasks   []*Task  // creation order; task events never add transcript items
	Blocked *Blocked
	Members []*Member // blueprint order, followed by actors first seen in the log

	Active    bool   // an agent is working
	Quiescent string // non-empty means the run stopped and this is why
	Finished  bool   // the log is exhausted and the input bar belongs to the human
	Effort    string // thinking effort level: low, medium, high, xhigh, max, ultracode
	Recap     bool   // whether to show a recap line after responses

	Now time.Duration // simulated clock, advanced by the player
}

// New returns the state of a run that has not started.
func New() *State { return &State{} }

// OpenInbox returns the first unanswered question, or nil.
func (s *State) OpenInbox() *Inbox {
	for _, in := range s.Inboxes {
		if !in.Answered {
			return in
		}
	}
	return nil
}

// appendItem adds an item to the transcript and guarantees it has an identity.
// The identity is not decoration: the renderer memoizes a sealed block under its
// id, so two items sharing one means the second is drawn with the first's lines.
// A caller that has a meaningful id (a part id from the log, a tool's sequence
// number) keeps it, because those ids also correlate later events; everything else
// is named after its position, which is unique and stable because this list is
// append-only and nothing is ever removed from it.
func (s *State) appendItem(it Item) {
	if it.ID == "" {
		it.ID = "item-" + strconv.Itoa(len(s.Items))
	}
	s.Items = append(s.Items, it)
}

// AppendPrompt records a prompt the human typed into the simulator itself, which
// is indistinguishable from one that arrived as run.prompt.
func (s *State) AppendPrompt(text string) {
	s.appendItem(Item{Kind: KindPrompt, Text: text, StartedAt: s.Now, EndedAt: s.Now})
}

// AppendNotice records a runtime remark.
func (s *State) AppendNotice(kind NoticeKind, text string) {
	s.appendItem(Item{Kind: KindNotice, Notice: kind, Text: text, StartedAt: s.Now, EndedAt: s.Now})
}

// Apply folds one event in. at is the simulated offset the event happens at.
// Unknown or uninteresting events are ignored on purpose: a TUI that crashes on
// an event it has no opinion about is a TUI that breaks on every runtime release.
func (s *State) Apply(ev *event.Event, at time.Duration) {
	s.Now = at
	switch ev.Type {
	case event.RunStarted:
		var p event.RunStartedPayload
		if ev.Decode(&p) == nil {
			s.RunID, s.Actor, s.BudgetUSD, s.BlueprintSHA = p.RunID, p.Actor, p.BudgetUSD, p.BlueprintSHA
			s.CWD, s.GitBranch = p.CWD, p.GitBranch
			s.seedMembers(p.Members)
		}
	case event.StageEntered:
		var p event.StageEnteredPayload
		if ev.Decode(&p) == nil {
			s.Stage, s.StageIndex = p.Stage, p.Index
		}
		s.Quiescent = ""
	case event.RunPrompt:
		var p event.RunPromptPayload
		if ev.Decode(&p) == nil {
			// Keyed by seq, not by timestamp: two prompts can share a
			// millisecond, and then one would inherit the other's lines.
			s.appendItem(Item{
				Kind: KindPrompt, Seq: ev.Seq, ID: "prompt-" + strconv.Itoa(ev.Seq),
				Text: p.Text, StartedAt: at, EndedAt: at,
			})
		}
		s.Quiescent = ""
	case event.AgentActivated:
		s.Active, s.Quiescent = true, ""
		if m := s.member(ev.Actor); m != nil {
			m.Busy, m.Blocked, m.Error = true, nil, ""
		}
	case event.AgentTurnDone:
		s.Active = false
		if m := s.member(ev.Actor); m != nil {
			m.Busy, m.Blocked = false, nil
		}
	case event.AgentFailed:
		s.Active = false
		if m := s.member(ev.Actor); m != nil {
			var p event.AgentFailedPayload
			_ = ev.Decode(&p)
			m.Busy, m.Blocked, m.Error = false, nil, p.Error
		}
	case event.LLMTurnStarted:
		var p event.TurnStartedPayload
		if ev.Decode(&p) == nil {
			s.Turn = p.Turn
		}
	case event.LLMPart:
		var p event.PartPayload
		if ev.Decode(&p) != nil {
			return
		}
		kind := KindText
		if p.Kind == event.PartThinking {
			kind = KindThinking
		}
		s.appendItem(Item{
			Kind: kind, Seq: ev.Seq, ID: p.PartID, Actor: ev.Actor,
			Effort: p.Effort, Open: true, StartedAt: at,
		})
	case event.LLMDelta:
		var p event.DeltaPayload
		if ev.Decode(&p) != nil {
			return
		}
		if it := s.findPart(p.PartID); it != nil {
			it.Text += p.Text
		}
	case event.LLMPartDone:
		var p event.DeltaPayload
		if ev.Decode(&p) != nil {
			return
		}
		if it := s.findPart(p.PartID); it != nil {
			it.Open, it.EndedAt = false, at
		}
	case event.LLMResponse:
		var p event.ResponsePayload
		if ev.Decode(&p) == nil {
			s.SpentUSD += p.CostUSD
			s.TokensIn, s.TokensOut, s.Model = p.TokensIn, p.TokensOut, p.Model
			s.ContextUsed, s.ContextCapacity = 0, 0
			if p.ContextUsed != nil && p.ContextCapacity != nil {
				s.ContextUsed, s.ContextCapacity = *p.ContextUsed, *p.ContextCapacity
			}
		}
	case event.SimTaskCreated:
		s.applyTaskCreated(ev, at)
	case event.SimTaskUpdated:
		s.applyTaskUpdated(ev, at)
	case event.SimTaskStatusChanged:
		s.applyTaskStatusChanged(ev, at)
	default:
		s.applyRest(ev, at)
	}
}

func (s *State) findTask(id string) *Task {
	for _, task := range s.Tasks {
		if task.ID == id {
			return task
		}
	}
	return nil
}

func (s *State) applyTaskCreated(ev *event.Event, at time.Duration) {
	var p event.TaskCreatedPayload
	if ev.Decode(&p) != nil || p.TaskID == "" || p.Title == "" || s.findTask(p.TaskID) != nil {
		return
	}
	status, ok := event.InitialTaskStatus(p.Status)
	if !ok {
		return
	}
	s.Tasks = append(s.Tasks, &Task{
		ID: p.TaskID, Title: p.Title, Detail: p.Detail, Owner: p.Owner, Status: status,
		CreatedSeq: ev.Seq, UpdatedSeq: ev.Seq, CreatedAt: at, UpdatedAt: at,
	})
}

func (s *State) applyTaskUpdated(ev *event.Event, at time.Duration) {
	var p event.TaskUpdatedPayload
	if ev.Decode(&p) != nil || p.TaskID == "" {
		return
	}
	task := s.findTask(p.TaskID)
	if task == nil || task.Status == event.TaskCompleted || p.Title != nil && *p.Title == "" {
		return
	}
	if p.Title != nil {
		task.Title = *p.Title
	}
	if p.Detail != nil {
		task.Detail = *p.Detail
	}
	if p.Owner != nil {
		task.Owner = *p.Owner
	}
	task.UpdatedSeq, task.UpdatedAt = ev.Seq, at
}

func (s *State) applyTaskStatusChanged(ev *event.Event, at time.Duration) {
	var p event.TaskStatusChangedPayload
	if ev.Decode(&p) != nil || p.TaskID == "" {
		return
	}
	task := s.findTask(p.TaskID)
	if task == nil || !event.CanChangeTaskStatus(task.Status, p.Status) {
		return
	}
	task.Status, task.UpdatedSeq, task.UpdatedAt = p.Status, ev.Seq, at
}

func (s *State) seedMembers(specs []event.MemberSpec) {
	s.Members = nil
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if spec.Name == "" || seen[spec.Name] {
			continue
		}
		seen[spec.Name] = true
		s.Members = append(s.Members, &Member{
			Name: spec.Name, Role: spec.Role, Model: spec.Model,
			Advisory: spec.Advisory, Tools: append([]string(nil), spec.Tools...),
			Activation: spec.Activation, Stages: append([]string(nil), spec.Stages...),
		})
	}
}

func (s *State) findMember(name string) *Member {
	for _, m := range s.Members {
		if m.Name == name {
			return m
		}
	}
	return nil
}

// member returns the named member, appending an actor the blueprint omitted.
// Empty actors describe the followed run rather than a team member and do not
// create a nameless row in the status or overlay.
func (s *State) member(name string) *Member {
	if name == "" {
		return nil
	}
	if m := s.findMember(name); m != nil {
		return m
	}
	m := &Member{Name: name}
	s.Members = append(s.Members, m)
	return m
}

func (s *State) findPart(id string) *Item {
	for i := len(s.Items) - 1; i >= 0; i-- {
		if s.Items[i].ID == id {
			return &s.Items[i]
		}
	}
	return nil
}
