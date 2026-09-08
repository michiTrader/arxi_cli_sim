package event

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The team and recorded-environment payload is additive, so decoding an older
// run with neither must keep working while a recorded blueprint retains every
// field and its order.
func TestRunStartedPayloadDecodesMembers(t *testing.T) {
	raw := json.RawMessage(`{"run_id":"r-team","actor":"team","budget_usd":12,"blueprint_sha":"abc","cwd":"D:\\projects\\arxi","git_branch":"main","members":[{"name":"scout","role":"investigator","model":"claude-opus-5","advisory":true,"tools":["read","bash"],"activation":"parallel","stages":["plan","execute"]},{"name":"builder"}]}`)
	ev := Event{Seq: 1, Type: RunStarted, Payload: raw}
	var got RunStartedPayload
	if err := ev.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.CWD != `D:\projects\arxi` || got.GitBranch != "main" {
		t.Fatalf("recorded environment = %q %q", got.CWD, got.GitBranch)
	}
	want := []MemberSpec{
		{Name: "scout", Role: "investigator", Model: "claude-opus-5", Advisory: true, Tools: []string{"read", "bash"}, Activation: "parallel", Stages: []string{"plan", "execute"}},
		{Name: "builder"},
	}
	if !reflect.DeepEqual(got.Members, want) {
		t.Fatalf("members = %#v, want %#v", got.Members, want)
	}

	ev.Payload = json.RawMessage(`{"run_id":"r-solo","actor":"solo","budget_usd":5,"blueprint_sha":"def"}`)
	got = RunStartedPayload{}
	if err := ev.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Members) != 0 {
		t.Fatalf("older run decoded %d members, want none", len(got.Members))
	}
}

func TestResponsePayloadPreservesOptionalExactContext(t *testing.T) {
	ev := Event{Seq: 2, Type: LLMResponse, Payload: json.RawMessage(
		`{"tokens_in":900,"context_used":81250,"context_capacity":200000}`,
	)}
	var got ResponsePayload
	if err := ev.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ContextUsed == nil || got.ContextCapacity == nil ||
		*got.ContextUsed != 81250 || *got.ContextCapacity != 200000 {
		t.Fatalf("context pointers = %v/%v", got.ContextUsed, got.ContextCapacity)
	}
	if got.TokensIn != 900 {
		t.Fatalf("tokens_in = %d, want independent latest-response count", got.TokensIn)
	}

	ev.Payload = json.RawMessage(`{"tokens_in":901}`)
	got = ResponsePayload{}
	if err := ev.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ContextUsed != nil || got.ContextCapacity != nil {
		t.Fatalf("omitted context decoded as %v/%v", got.ContextUsed, got.ContextCapacity)
	}
}

func TestTaskPayloadsPreserveOptionalFields(t *testing.T) {
	ev := Event{Seq: 1, Type: SimTaskCreated, Payload: json.RawMessage(`{"task_id":"t1","title":"Ship","detail":"notes","owner":"builder"}`)}
	var created TaskCreatedPayload
	if err := ev.Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Status != nil {
		t.Fatalf("omitted status decoded as %v", created.Status)
	}
	if status, ok := InitialTaskStatus(created.Status); !ok || status != TaskPending {
		t.Fatalf("default status = %q, %v; want pending, true", status, ok)
	}

	ev.Payload = json.RawMessage(`{"task_id":"t1","title":"","detail":"","owner":""}`)
	var updated TaskUpdatedPayload
	if err := ev.Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Title == nil || updated.Detail == nil || updated.Owner == nil {
		t.Fatalf("explicit empty fields lost presence: %+v", updated)
	}
	if *updated.Title != "" || *updated.Detail != "" || *updated.Owner != "" {
		t.Fatalf("empty fields changed on decode: %+v", updated)
	}

	ev.Payload = json.RawMessage(`{"task_id":"t1"}`)
	updated = TaskUpdatedPayload{}
	if err := ev.Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Title != nil || updated.Detail != nil || updated.Owner != nil {
		t.Fatalf("omitted fields decoded as present: %+v", updated)
	}
}

func TestTaskLifecycleHelpers(t *testing.T) {
	active, completed, unknown := TaskActive, TaskCompleted, TaskStatus("failed")
	for _, tc := range []struct {
		name string
		from TaskStatus
		to   TaskStatus
		ok   bool
	}{
		{"pending-active", TaskPending, TaskActive, true},
		{"pending-completed", TaskPending, TaskCompleted, true},
		{"active-pending", TaskActive, TaskPending, true},
		{"active-completed", TaskActive, TaskCompleted, true},
		{"same", TaskPending, TaskPending, false},
		{"terminal", TaskCompleted, TaskActive, false},
		{"unknown target", TaskActive, unknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanChangeTaskStatus(tc.from, tc.to); got != tc.ok {
				t.Fatalf("CanChangeTaskStatus(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.ok)
			}
		})
	}
	if got, ok := InitialTaskStatus(&active); !ok || got != TaskActive {
		t.Fatalf("active initial status = %q, %v", got, ok)
	}
	if _, ok := InitialTaskStatus(&completed); ok {
		t.Fatal("completed accepted as an initial status")
	}
	if _, ok := InitialTaskStatus(&unknown); ok {
		t.Fatal("unknown initial status accepted")
	}
}

func TestAgentDetailPayloadsDecodeTheirWireNames(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		out  any
		want any
	}{
		{"steered", `{"text":"inspect only","to":"scout"}`, &AgentSteeredPayload{}, AgentSteeredPayload{Text: "inspect only", To: "scout"}},
		{"notified", `{"text":"patch landed","to":"reviewer"}`, &AgentNotifiedPayload{}, AgentNotifiedPayload{Text: "patch landed", To: "reviewer"}},
		{"failed", `{"error":"focused test failed"}`, &AgentFailedPayload{}, AgentFailedPayload{Error: "focused test failed"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := Event{Seq: 9, Type: "agent." + tc.name, Payload: json.RawMessage(tc.raw)}
			if err := ev.Decode(tc.out); err != nil {
				t.Fatal(err)
			}
			got := reflect.ValueOf(tc.out).Elem().Interface()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decoded %#v, want %#v", got, tc.want)
			}
		})
	}
}
