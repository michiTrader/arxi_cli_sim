package scenario_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/scenario"
)

const firstConversation = "../../testdata/scenarios/01-first-conversation.ndjson"

// TestEveryScenarioIsWellFormed is the gate every recorded conversation passes
// before the TUI is allowed to render it.
func TestEveryScenarioIsWellFormed(t *testing.T) {
	paths, err := filepath.Glob("../../testdata/scenarios/*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no scenarios found under testdata/scenarios")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := scenario.Load(path); err != nil {
				t.Fatalf("scenario is not well formed:\n%v", err)
			}
		})
	}
}

func TestFirstConversationShape(t *testing.T) {
	sc, err := scenario.Load(firstConversation)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var events, barriers int
	for _, st := range sc.Steps {
		if st.IsBarrier() {
			barriers++
		} else {
			events++
		}
	}
	if events != 34 || barriers != 2 {
		t.Errorf("got %d events and %d barriers, want 34 and 2", events, barriers)
	}
	if got := sc.Duration().Milliseconds(); got != 4930 {
		t.Errorf("simulated duration is %d ms, want 4930", got)
	}
	if got := sc.Steps[len(sc.Steps)-1].Await; got != scenario.AwaitPrompt {
		t.Errorf("the file ends awaiting %q, want %q", got, scenario.AwaitPrompt)
	}
}

// TestTimestampsAreDerived proves the loader, not the file, owns ts: a scenario
// carries relative delays so it can be replayed at any speed, and the absolute
// clock is reconstructed from a fixed epoch.
func TestTimestampsAreDerived(t *testing.T) {
	sc, err := scenario.Load(firstConversation)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	first := sc.Steps[0].Event
	if first.TS != "2026-01-01T00:00:00.000Z" {
		t.Errorf("first event ts is %q, want the epoch", first.TS)
	}
	var last *event.Event
	for i := range sc.Steps {
		if sc.Steps[i].Event != nil {
			last = sc.Steps[i].Event
		}
	}
	if last.TS != "2026-01-01T00:00:04.930Z" {
		t.Errorf("last event ts is %q, want epoch+4930ms", last.TS)
	}
}

// TestUnterminatedFenceIsReachable pins the reason scenario 01 exists: one delta
// opens a ```go fence and a later delta closes it, so any renderer that needs a
// complete document before it can draw anything fails here.
func TestUnterminatedFenceIsReachable(t *testing.T) {
	sc, err := scenario.Load(firstConversation)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var text strings.Builder
	sawOddFenceMidStream := false
	for _, st := range sc.Steps {
		if st.Event == nil || st.Event.Type != event.LLMDelta {
			continue
		}
		var p event.DeltaPayload
		if err := st.Event.Decode(&p); err != nil {
			t.Fatal(err)
		}
		if p.PartID != "p3" {
			continue
		}
		text.WriteString(p.Text)
		if strings.Count(text.String(), "```")%2 == 1 {
			sawOddFenceMidStream = true
		}
	}
	if !sawOddFenceMidStream {
		t.Error("no delta leaves a fence open; the incremental-markdown case is untested")
	}
	if n := strings.Count(text.String(), "```"); n%2 != 0 {
		t.Errorf("part p3 ends with %d fence markers, want an even count", n)
	}
}

func TestTaskContractValidatesLifecycleAndClearFields(t *testing.T) {
	body := strings.Join([]string{
		`{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"Ship","detail":"notes","owner":"builder"}}}`,
		`{"after_ms":1,"event":{"seq":2,"type":"sim.task.updated","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","detail":"","owner":""}}}`,
		`{"after_ms":1,"event":{"seq":3,"type":"sim.task.status_changed","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","status":"active"}}}`,
		`{"after_ms":1,"event":{"seq":4,"type":"sim.task.status_changed","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","status":"pending"}}}`,
		`{"after_ms":1,"event":{"seq":5,"type":"sim.task.status_changed","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","status":"completed"}}}`,
	}, "\n") + "\n"
	path := filepath.Join(t.TempDir(), "tasks.ndjson")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.Load(path); err != nil {
		t.Fatalf("valid task lifecycle rejected: %v", err)
	}
}

func TestLoadRejectsMalformedLines(t *testing.T) {
	cases := map[string]string{
		"unknown wrapper field":   `{"after_ms":0,"evnet":{}}`,
		"barrier with a delay":    `{"after_ms":10,"await":"prompt"}`,
		"event without delay":     `{"event":{"seq":1,"type":"run.prompt","scope":"run:r1","source":"human","payload":{}}}`,
		"negative delay":          `{"after_ms":-1,"event":{"seq":1,"type":"run.prompt","scope":"run:r1","source":"human","payload":{}}}`,
		"unknown event type":      `{"after_ms":0,"event":{"seq":1,"type":"run.explodes","scope":"run:r1","source":"human","payload":{}}}`,
		"unknown source":          `{"after_ms":0,"event":{"seq":1,"type":"run.prompt","scope":"run:r1","source":"cosmos","payload":{}}}`,
		"seq starts at two":       `{"after_ms":0,"event":{"seq":2,"type":"run.prompt","scope":"run:r1","source":"human","payload":{}}}`,
		"quiescent without why":   `{"after_ms":0,"event":{"seq":1,"type":"run.quiescent","scope":"run:r1","source":"runtime","payload":{"stage":"execute"}}}`,
		"blocked without ref":     `{"after_ms":0,"event":{"seq":1,"type":"agent.blocked","scope":"run:r1","source":"runtime","payload":{"blocked_on":"approval"}}}`,
		"await unknown inbox":     `{"await":"inbox-9"}`,
		"delta before part":       `{"after_ms":0,"event":{"seq":1,"type":"llm.delta","scope":"run:r1","source":"agent","payload":{"part_id":"p1","text":"hi"}}}`,
		"half context pair":       `{"after_ms":0,"event":{"seq":1,"type":"llm.response","scope":"run:r1","source":"runtime","payload":{"context_used":10}}}`,
		"negative context used":   `{"after_ms":0,"event":{"seq":1,"type":"llm.response","scope":"run:r1","source":"runtime","payload":{"context_used":-1,"context_capacity":100}}}`,
		"zero context capacity":   `{"after_ms":0,"event":{"seq":1,"type":"llm.response","scope":"run:r1","source":"runtime","payload":{"context_used":0,"context_capacity":0}}}`,
		"context above capacity":  `{"after_ms":0,"event":{"seq":1,"type":"llm.response","scope":"run:r1","source":"runtime","payload":{"context_used":101,"context_capacity":100}}}`,
		"part left open":          `{"after_ms":0,"event":{"seq":1,"type":"llm.part","scope":"run:r1","source":"agent","payload":{"part_id":"p1","kind":"text","index":0}}}`,
		"task create empty id":    `{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"title":"Ship"}}}`,
		"task create empty title": `{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":""}}}`,
		"task create completed":   `{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"Ship","status":"completed"}}}`,
		"task create unknown":     `{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"Ship","status":"failed"}}}`,
		"task orphan update":      `{"after_ms":0,"event":{"seq":1,"type":"sim.task.updated","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","detail":"notes"}}}`,
		"task orphan status":      `{"after_ms":0,"event":{"seq":1,"type":"sim.task.status_changed","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","status":"active"}}}`,
		"task duplicate": strings.Join([]string{
			`{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"One"}}}`,
			`{"after_ms":0,"event":{"seq":2,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"Two"}}}`,
		}, "\n"),
		"task update empty title": strings.Join([]string{
			`{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"One"}}}`,
			`{"after_ms":0,"event":{"seq":2,"type":"sim.task.updated","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":""}}}`,
		}, "\n"),
		"task same status": strings.Join([]string{
			`{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"One"}}}`,
			`{"after_ms":0,"event":{"seq":2,"type":"sim.task.status_changed","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","status":"pending"}}}`,
		}, "\n"),
		"task invalid transition": strings.Join([]string{
			`{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"One"}}}`,
			`{"after_ms":0,"event":{"seq":2,"type":"sim.task.status_changed","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","status":"failed"}}}`,
		}, "\n"),
		"task terminal update": strings.Join([]string{
			`{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"One"}}}`,
			`{"after_ms":0,"event":{"seq":2,"type":"sim.task.status_changed","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","status":"completed"}}}`,
			`{"after_ms":0,"event":{"seq":3,"type":"sim.task.updated","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","detail":"late"}}}`,
		}, "\n"),
		"task terminal status": strings.Join([]string{
			`{"after_ms":0,"event":{"seq":1,"type":"sim.task.created","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","title":"One","status":"active"}}}`,
			`{"after_ms":0,"event":{"seq":2,"type":"sim.task.status_changed","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","status":"completed"}}}`,
			`{"after_ms":0,"event":{"seq":3,"type":"sim.task.status_changed","scope":"run:r1","source":"runtime","payload":{"task_id":"t1","status":"pending"}}}`,
		}, "\n"),
	}
	dir := t.TempDir()
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, "case.ndjson")
			if err := os.WriteFile(path, []byte(body+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := scenario.Load(path); err == nil {
				t.Fatalf("loaded a scenario that should have been rejected: %s", body)
			}
		})
	}
}
