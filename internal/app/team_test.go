package app

import (
	"bytes"
	"strings"
	"testing"

	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

func teamFixture() *state.State {
	return &state.State{RunID: "r-team", Members: []*state.Member{
		{Name: "scout", Role: "investigator", Model: "claude-opus-5", Advisory: true, Tools: []string{"read", "bash"}, Activation: "parallel", Stages: []string{"plan", "execute"}, Busy: true},
		{Name: "builder", Blocked: &state.Blocked{On: "approval", Ref: map[string]any{"inbox_id": "inbox-7"}}},
		{Name: "reviewer", Error: "focused test failed"},
		{Name: "scribe", Notified: "document the result", NotifiedTo: "scribe"},
	}}
}

func TestTeamFrameBuildsRosterStateAndRemedy(t *testing.T) {
	f := renderTeam(teamFixture(), ui.Viewport{Width: 80, Height: 18}, 0)
	got := f.Plain()
	for _, want := range []string{
		"Team  4 members", "scout  working", "role investigator", "model claude-opus-5",
		"builder  blocked on approval", "remedy: arxi inbox approve inbox-7",
		"reviewer  failed: focused test failed", "notified to scribe: document the result", "Esc back",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Team frame does not contain %q:\n%s", want, got)
		}
	}
	if len(f.Live) != 18 || !f.Cursor.Hidden || len(f.Committed) != 0 {
		t.Fatalf("frame geometry = live %d hidden %v committed %d", len(f.Live), f.Cursor.Hidden, len(f.Committed))
	}
}

func TestTeamFrameFitsNarrowAndShortSurfaces(t *testing.T) {
	for _, width := range []int{20, 31, 48, 72} {
		for _, height := range []int{1, 2, 3, 8} {
			f := renderTeam(teamFixture(), ui.Viewport{Width: width, Height: height}, 1000)
			if len(f.Live) != height {
				t.Errorf("%dx%d: live=%d", width, height, len(f.Live))
			}
			if bad := f.Overflow(); len(bad) != 0 {
				t.Errorf("%dx%d overflows at %v:\n%s", width, height, bad, f.Plain())
			}
			for _, row := range f.Live {
				if strings.HasSuffix(row.Text(), " ") {
					t.Errorf("%dx%d has trailing space in %q", width, height, row.Text())
				}
			}
		}
	}
}

func TestTeamFrameOmitsUnknownMetadata(t *testing.T) {
	for _, width := range []int{31, 80} {
		got := renderTeam(teamFixture(), ui.Viewport{Width: width, Height: 18}, 0).Plain()
		for _, placeholder := range []string{"role —", "model default", "model run default"} {
			if strings.Contains(got, placeholder) {
				t.Errorf("width %d renders placeholder %q:\n%s", width, placeholder, got)
			}
		}
		if !strings.Contains(got, "role investigator") || !strings.Contains(got, "claude-opus-5") {
			t.Errorf("width %d dropped real metadata:\n%s", width, got)
		}
	}
}

func TestTeamFrameHandlesNilStateAtEveryHeight(t *testing.T) {
	for height := 0; height <= 4; height++ {
		f := renderTeam(nil, ui.Viewport{Width: 40, Height: height}, 1000)
		if len(f.Live) != height {
			t.Errorf("height %d: live=%d", height, len(f.Live))
		}
		if bad := f.Overflow(); len(bad) != 0 {
			t.Errorf("height %d overflows at %v:\n%s", height, bad, f.Plain())
		}
	}
	if got := teamHeader(nil, 40).Text(); got != "Team" {
		t.Fatalf("nil header = %q", got)
	}
	if got := teamOneLine(nil, 40).Text(); !strings.Contains(got, "Team 0") {
		t.Fatalf("nil compact row = %q", got)
	}
}

func TestTeamFrameScrollAndLiveState(t *testing.T) {
	st := teamFixture()
	first := renderTeam(st, ui.Viewport{Width: 40, Height: 6}, 0)
	if first.Scroll.Below == 0 {
		t.Fatal("fixture is not tall enough to scroll")
	}
	last := renderTeam(st, ui.Viewport{Width: 40, Height: 6}, 1000)
	if last.Scroll.Below != 0 || last.Scroll.Above == 0 {
		t.Fatalf("last window = %+v", last.Scroll)
	}
	st.Members[0].Busy = false
	st.Members[0].Error = "new failure"
	live := renderTeam(st, ui.Viewport{Width: 80, Height: 10}, 0)
	if !strings.Contains(live.Plain(), "failed: new failure") {
		t.Fatal("Team frame retained a stale snapshot")
	}
}

func TestTeamDrawUsesCurrentFoldedState(t *testing.T) {
	var out bytes.Buffer
	em := &ui.Emitter{Theme: ui.DefaultTheme(), Mode: ui.ModeInline, Profile: ui.ProfileMono}
	a := New(Config{Width: 80, Height: 10, Out: &out, Emitter: em})
	a.st = teamFixture()
	a.openTeam()
	if err := a.draw(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "scout") || !strings.Contains(got, "builder") {
		t.Fatalf("draw did not emit current Team roster: %q", got)
	}
}

func TestTeamViewEntryKeyIsolationAndRestoration(t *testing.T) {
	if got := DefaultBindings()["ctrl+t"]; got != ActionTeam {
		t.Fatalf("ctrl+t = %q", got)
	}
	a := New(Config{Width: 40, Height: 8})
	a.st = teamFixture()
	a.ed.Insert("draft")
	a.scrolled, a.top = true, 7
	if !a.dispatch(ActionTeam, mustKey(t, "ctrl+t")) || a.view != viewTeam {
		t.Fatal("team action did not open dedicated view")
	}
	before := a.ed.Text()
	if a.dispatch(ActionSubmit, mustKey(t, "enter")) {
		t.Fatal("enter changed Team view")
	}
	a.dispatch(ActionNone, term.Key{Type: term.KeyRunes, Runes: []rune{'x'}})
	if a.ed.Text() != before {
		t.Fatalf("Team key mutated editor: %q", a.ed.Text())
	}
	a.dispatch(ActionPageDown, mustKey(t, "pgdown"))
	a.dispatch(ActionCancel, mustKey(t, "esc"))
	if a.view != viewConversation || !a.scrolled || a.top != 7 || a.ed.Text() != "draft" {
		t.Fatalf("conversation not restored: view=%v scroll=%v/%d text=%q", a.view, a.scrolled, a.top, a.ed.Text())
	}
}

func TestSlashAndKeyUseSameTeamPath(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.openTeam()
	a.viewTop = 9
	a.closeFullView()
	a.ed.Insert("/team")
	if !a.dispatch(ActionSubmit, mustKey(t, "enter")) || a.view != viewTeam || a.viewTop != 0 {
		t.Fatal("/team did not use fresh dedicated Team entry")
	}
}
