package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"arxi.local/sim/internal/event"
)

// These are the fold's two multi-member rules, tested here rather than only through
// a scenario: a scenario proves the transcript reads correctly, a unit proves the
// rule holds for a log nobody has recorded yet.

// line builds one log line. The payload is marshalled from the declared struct so a
// renamed field breaks the test instead of silently vanishing from the fixture.
func line(seq int, typ, actor string, payload any) *event.Event {
	b, err := json.Marshal(payload)
	if err != nil {
		panic(err) // a fixture that cannot be marshalled is a bug in the test
	}
	return &event.Event{
		Seq: seq, Type: typ, Scope: "run:t",
		Source: event.SourceRuntime, Actor: actor, Payload: b,
	}
}

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func completed(seq int, actor, tool string, exit *int, summary string) *event.Event {
	var p event.ToolResultPayload
	p.Tool, p.Result.ExitCode, p.Result.Summary = tool, exit, summary
	return line(seq, event.ToolCallCompleted, actor, p)
}

func notices(s *State) []string {
	var out []string
	for _, it := range s.Items {
		if it.Kind == KindNotice {
			out = append(out, it.Text)
		}
	}
	return out
}

// A four-member run has two `bash` calls open at once and the log carries no call
// id, so a result matched by tool name alone lands on the wrong call: scout's
// failure appeared under builder's build, which is worse than showing nothing.
func TestResultBindsToTheCallThatIssuedIt(t *testing.T) {
	s := New()
	s.Apply(line(1, event.ToolCall, "scout",
		event.ToolCallPayload{Tool: "bash", Args: map[string]any{"cmd": "go test ./internal/auth"}}), ms(10))
	s.Apply(line(2, event.ToolCall, "builder",
		event.ToolCallPayload{Tool: "bash", Args: map[string]any{"cmd": "go build ./..."}}), ms(20))

	fail := 1
	s.Apply(completed(3, "scout", "bash", &fail, "FAIL internal/auth"), ms(900))

	if scout := s.Items[0]; scout.Open || scout.Summary != "FAIL internal/auth" || scout.Status != ToolFailed {
		t.Fatalf("scout's call did not take scout's result: %+v", scout)
	}
	if builder := s.Items[1]; !builder.Open {
		t.Fatalf("scout's result sealed builder's call: %+v", builder)
	}

	ok := 0
	s.Apply(completed(4, "builder", "bash", &ok, "build ok"), ms(1200))
	if builder := s.Items[1]; builder.Open || builder.Summary != "build ok" || builder.Status != ToolOK {
		t.Fatalf("builder's call did not take builder's result: %+v", builder)
	}
}

// Two calls by one actor complete in the order they were issued, and a result the
// runtime reports with no actor has nothing else to go on.
func TestResultWithoutAnActorTakesTheOldestCall(t *testing.T) {
	s := New()
	s.Apply(line(1, event.ToolCall, "solo",
		event.ToolCallPayload{Tool: "read", Args: map[string]any{"path": "a.go"}}), ms(10))
	s.Apply(line(2, event.ToolCall, "solo",
		event.ToolCallPayload{Tool: "read", Args: map[string]any{"path": "b.go"}}), ms(20))
	s.Apply(completed(3, "", "read", nil, "read 10 lines"), ms(30))

	if first := s.Items[0]; first.Open || first.Summary != "read 10 lines" {
		t.Fatalf("the oldest open call did not take the result: %+v", first)
	}
	if second := s.Items[1]; !second.Open {
		t.Fatalf("the newer call was sealed instead: %+v", second)
	}
}

// A pause has to have a reason on screen, and only one.
func TestBlockedNoticeSpeaksOnlyWhenNothingElseHas(t *testing.T) {
	blocked := func(s *State, on string) {
		s.Apply(line(9, event.AgentBlocked, "tester", event.AgentBlockedPayload{BlockedOn: on}), ms(50))
	}

	// An approval draws its own question with the answer keys on it.
	s := New()
	blocked(s, "approval")
	if got := notices(s); len(got) != 0 {
		t.Fatalf("an approval added a line of its own: %v", got)
	}

	// A lock nothing announced is the case a reader would otherwise have to guess at.
	s = New()
	blocked(s, "lock")
	if got := notices(s); len(got) != 1 || got[0] != "tester is blocked on lock" {
		t.Fatalf("an unexplained lock: %v", got)
	}

	// The conflict one line up already said who waits for what.
	s = New()
	s.Apply(line(8, event.ResourceConflict, "tester",
		event.ConflictPayload{Resource: "file:a.go", Holder: "builder", Waiter: "tester", Wanted: "write"}), ms(40))
	blocked(s, "lock")
	if got := notices(s); len(got) != 1 || got[0] != "tester waits for file:a.go, held by builder" {
		t.Fatalf("a conflict said twice: %v", got)
	}
}

// The lock family draws nothing at all until this fold names it, which is why a run
// that serialized on one file used to look like a run that stalled for no reason.
func TestLockNoticesNameTheHolderAndTheMode(t *testing.T) {
	s := New()
	held := event.LockPayload{Resource: "file:a.go", Holder: "builder", Mode: "write"}
	s.Apply(line(1, event.LockAcquired, "builder", held), ms(10))
	s.Apply(line(2, event.LockReleased, "builder", held), ms(20))

	want := "builder holds file:a.go (write) | builder released file:a.go"
	if got := strings.Join(notices(s), " | "); got != want {
		t.Fatalf("lock notices read:\n  %s\nwant:\n  %s", got, want)
	}
}

func TestRecordedEnvironmentAndContextFoldExactly(t *testing.T) {
	s := New()
	s.Apply(line(1, event.RunStarted, "human", event.RunStartedPayload{
		RunID: "r-context", CWD: `D:\projects\arxi`, GitBranch: "main",
	}), 0)
	if s.CWD != `D:\projects\arxi` || s.GitBranch != "main" {
		t.Fatalf("recorded environment = %q %q", s.CWD, s.GitBranch)
	}

	used, capacity := 81_250, 200_000
	s.Apply(line(2, event.LLMResponse, "builder", event.ResponsePayload{
		Model: "claude-sonnet-5", TokensIn: 900, TokensOut: 120,
		ContextUsed: &used, ContextCapacity: &capacity,
	}), ms(10))
	if s.ContextUsed != used || s.ContextCapacity != capacity {
		t.Fatalf("context = %d/%d, want %d/%d", s.ContextUsed, s.ContextCapacity, used, capacity)
	}
	if s.TokensIn != 900 || s.TokensOut != 120 {
		t.Fatalf("response tokens = %d/%d", s.TokensIn, s.TokensOut)
	}

	s.Apply(line(3, event.LLMResponse, "builder", event.ResponsePayload{Model: "claude-sonnet-5"}), ms(20))
	if s.ContextUsed != 0 || s.ContextCapacity != 0 {
		t.Fatalf("omitted current context left stale values: %d/%d", s.ContextUsed, s.ContextCapacity)
	}
}

func TestMembersKeepBlueprintOrderAndAppendUnknownActors(t *testing.T) {
	s := New()
	s.Apply(line(1, event.RunStarted, "human", event.RunStartedPayload{
		RunID: "r-team", Actor: "team", Members: []event.MemberSpec{
			{Name: "scout", Role: "investigator", Tools: []string{"read"}, Stages: []string{"execute"}},
			{Name: "builder", Model: "claude-sonnet-5"},
			{Name: "scout", Role: "duplicate"},
			{Name: ""},
		},
	}), 0)
	if got := memberNames(s); strings.Join(got, ",") != "scout,builder" {
		t.Fatalf("seeded members = %v, want blueprint order without blanks or duplicates", got)
	}
	// The state owns its copy of slice fields rather than aliases into the decoded payload.
	if got := strings.Join(s.Members[0].Tools, ","); got != "read" {
		t.Fatalf("scout tools = %q, want read", got)
	}
	if scout := s.Members[0]; scout.Role != "investigator" || strings.Join(scout.Stages, ",") != "execute" {
		t.Fatalf("scout blueprint metadata = %+v", scout)
	}
	if builder := s.Members[1]; builder.Model != "claude-sonnet-5" {
		t.Fatalf("builder blueprint metadata = %+v", builder)
	}

	s.Apply(line(2, event.AgentActivated, "reviewer", struct{}{}), ms(10))
	s.Apply(line(3, event.AgentActivated, "reviewer", struct{}{}), ms(20))
	s.Apply(line(4, event.AgentActivated, "", struct{}{}), ms(30))
	if got := memberNames(s); strings.Join(got, ",") != "scout,builder,reviewer" {
		t.Fatalf("members after unknown actors = %v, want reviewer appended and no nameless row", got)
	}
	if !s.Members[2].Busy {
		t.Fatal("newly discovered reviewer is not busy after activation")
	}
}

func TestMemberLiveStateFoldsEveryAgentDetail(t *testing.T) {
	s := New()
	s.Apply(line(1, event.RunStarted, "human", event.RunStartedPayload{Members: []event.MemberSpec{{Name: "builder"}}}), 0)
	s.Apply(line(2, event.AgentSteered, "builder", event.AgentSteeredPayload{Text: "patch cache only", To: "builder"}), ms(10))
	s.Apply(line(3, event.AgentNotified, "builder", event.AgentNotifiedPayload{Text: "reviewer is waiting", To: "builder"}), ms(20))
	s.Apply(line(4, event.AgentActivated, "builder", struct{}{}), ms(30))
	m := s.Members[0]
	if !m.Busy || m.Steered != "patch cache only" || m.SteeredTo != "builder" || m.Notified != "reviewer is waiting" || m.NotifiedTo != "builder" {
		t.Fatalf("member after details and activation = %+v", m)
	}

	ref := map[string]any{"inbox_id": "inbox-1", "tool": "write"}
	s.Apply(line(5, event.AgentBlocked, "builder", event.AgentBlockedPayload{BlockedOn: "approval", BlockedRef: ref}), ms(40))
	if !m.Busy || m.Blocked == nil || m.Blocked.On != "approval" || m.Blocked.Ref["inbox_id"] != "inbox-1" {
		t.Fatalf("member after block = %+v", m)
	}
	s.Apply(line(6, event.AgentUnblocked, "builder", struct{}{}), ms(50))
	if !m.Busy || m.Blocked != nil {
		t.Fatalf("member after unblock = %+v, want resumed open turn", m)
	}
	s.Apply(line(7, event.AgentTurnDone, "builder", struct{}{}), ms(60))
	if m.Busy || m.Blocked != nil {
		t.Fatalf("member after turn_done = %+v", m)
	}

	s.Apply(line(8, event.AgentActivated, "builder", struct{}{}), ms(70))
	s.Apply(line(9, event.AgentFailed, "builder", event.AgentFailedPayload{Error: "focused test failed"}), ms(80))
	if m.Busy || m.Blocked != nil || m.Error != "focused test failed" {
		t.Fatalf("member after failure = %+v", m)
	}
	// A later activation starts a fresh attempt and clears the stale failure.
	s.Apply(line(10, event.AgentActivated, "builder", struct{}{}), ms(90))
	if !m.Busy || m.Error != "" {
		t.Fatalf("member after reactivation = %+v", m)
	}
}

func TestTasksReplayAtEveryPrefix(t *testing.T) {
	title := "Ship v2"
	empty := ""
	events := []*event.Event{
		line(1, event.SimTaskCreated, "", event.TaskCreatedPayload{TaskID: "t1", Title: "Ship", Detail: "notes", Owner: "builder"}),
		line(2, event.SimTaskCreated, "", event.TaskCreatedPayload{TaskID: "t2", Title: "Review", Status: taskStatus(event.TaskActive)}),
		line(3, event.SimTaskUpdated, "", event.TaskUpdatedPayload{TaskID: "t1", Title: &title, Detail: &empty, Owner: &empty}),
		line(4, event.SimTaskStatusChanged, "", event.TaskStatusChangedPayload{TaskID: "t1", Status: event.TaskActive}),
		line(5, event.SimTaskStatusChanged, "", event.TaskStatusChangedPayload{TaskID: "t1", Status: event.TaskCompleted}),
	}
	ats := []time.Duration{ms(10), ms(20), ms(30), ms(40), ms(50)}
	for prefix := 0; prefix <= len(events); prefix++ {
		s := New()
		for i := 0; i < prefix; i++ {
			s.Apply(events[i], ats[i])
		}
		if len(s.Items) != 0 {
			t.Fatalf("prefix %d: task events created %d transcript items", prefix, len(s.Items))
		}
		wantTasks := prefix
		if wantTasks > 2 {
			wantTasks = 2
		}
		if len(s.Tasks) != wantTasks {
			t.Fatalf("prefix %d: got %d tasks, want %d", prefix, len(s.Tasks), wantTasks)
		}
		if prefix >= 2 && (s.Tasks[0].ID != "t1" || s.Tasks[1].ID != "t2") {
			t.Fatalf("prefix %d: creation order changed: %+v", prefix, s.Tasks)
		}
	}

	s := New()
	for i, ev := range events {
		s.Apply(ev, ats[i])
	}
	first := s.Tasks[0]
	if first.Title != "Ship v2" || first.Detail != "" || first.Owner != "" || first.Status != event.TaskCompleted {
		t.Fatalf("final task = %+v", first)
	}
	if first.CreatedSeq != 1 || first.UpdatedSeq != 5 || first.CreatedAt != ms(10) || first.UpdatedAt != ms(50) {
		t.Fatalf("task metadata = %+v", first)
	}
	second := s.Tasks[1]
	if second.Status != event.TaskActive || second.CreatedSeq != 2 || second.UpdatedSeq != 2 || second.CreatedAt != ms(20) || second.UpdatedAt != ms(20) {
		t.Fatalf("explicit active task = %+v", second)
	}
}

func TestTaskReducerSafelyIgnoresInvalidEvents(t *testing.T) {
	empty, late := "", "late"
	s := New()
	s.Apply(line(1, event.SimTaskUpdated, "", event.TaskUpdatedPayload{TaskID: "missing", Detail: &late}), ms(1))
	s.Apply(line(2, event.SimTaskStatusChanged, "", event.TaskStatusChangedPayload{TaskID: "missing", Status: event.TaskActive}), ms(2))
	s.Apply(line(3, event.SimTaskCreated, "", event.TaskCreatedPayload{TaskID: "t1", Title: "Original"}), ms(3))
	s.Apply(line(4, event.SimTaskCreated, "", event.TaskCreatedPayload{TaskID: "t1", Title: "Duplicate"}), ms(4))
	s.Apply(line(5, event.SimTaskUpdated, "", event.TaskUpdatedPayload{TaskID: "t1", Title: &empty}), ms(5))
	s.Apply(line(6, event.SimTaskStatusChanged, "", event.TaskStatusChangedPayload{TaskID: "t1", Status: event.TaskStatus("failed")}), ms(6))
	s.Apply(line(7, event.SimTaskStatusChanged, "", event.TaskStatusChangedPayload{TaskID: "t1", Status: event.TaskCompleted}), ms(7))
	s.Apply(line(8, event.SimTaskUpdated, "", event.TaskUpdatedPayload{TaskID: "t1", Detail: &late}), ms(8))
	s.Apply(line(9, event.SimTaskStatusChanged, "", event.TaskStatusChangedPayload{TaskID: "t1", Status: event.TaskPending}), ms(9))
	if len(s.Tasks) != 1 {
		t.Fatalf("invalid events created tasks: %+v", s.Tasks)
	}
	task := s.Tasks[0]
	if task.Title != "Original" || task.Detail != "" || task.Status != event.TaskCompleted {
		t.Fatalf("invalid event mutated task: %+v", task)
	}
	if task.CreatedSeq != 3 || task.UpdatedSeq != 7 || task.CreatedAt != ms(3) || task.UpdatedAt != ms(7) {
		t.Fatalf("invalid event changed metadata: %+v", task)
	}
	if len(s.Items) != 0 {
		t.Fatalf("task events created transcript items: %+v", s.Items)
	}
}

func taskStatus(status event.TaskStatus) *event.TaskStatus { return &status }

func memberNames(s *State) []string {
	out := make([]string, len(s.Members))
	for i, m := range s.Members {
		out[i] = m.Name
	}
	return out
}
