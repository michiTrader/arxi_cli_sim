package app

import (
	"io"
	"strings"
	"testing"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/ui"
)

func tasksFixture() *state.State {
	return &state.State{Tasks: []*state.Task{
		{ID: "task-1", Title: "Trace stale session cache", Owner: "scout", Status: event.TaskCompleted},
		{ID: "task-2", Title: "Invalidate the cache entry", Owner: "builder", Status: event.TaskActive},
		{ID: "task-3", Title: "Run the focused regression test", Owner: "reviewer", Status: event.TaskPending},
	}}
}

func tasksEmitter() *ui.Emitter {
	return &ui.Emitter{Theme: ui.DefaultTheme(), Mode: ui.ModeInline, Profile: ui.ProfileMono}
}

// tasksFrameText renders the conversation the way draw would and returns it as text.
// The assertions below are about which rows the panel contributes and in what order,
// which the frame answers directly; reading them out of emitted bytes would make every
// claim hostage to the escape sequences the emitter folds between spans.
func tasksFrameText(t *testing.T, a *App) string {
	t.Helper()
	a.r.Widgets = a.chrome()
	a.ed.Shine = a.inputShine()
	a.vp.Scrolled, a.vp.ScrollTop = a.scrolled, a.top
	f := a.r.Render(a.st, a.ed, a.vp)
	return f.Plain()
}

// TestTasksToggleViaSlashAndKey is the whole contract of the two entry points: both
// flip the same fixed panel between the summary and the dropped list, and neither
// opens a second surface the other does not know about.
func TestTasksToggleViaSlashAndKey(t *testing.T) {
	a := New(Config{Width: 80, Height: 24})
	a.st = tasksFixture()
	a.tasksOpen = false // the default-open panel is pinned in its own test below
	a.ed.Insert("/tasks")
	if !a.dispatch(ActionSubmit, mustKey(t, "enter")) || !a.tasksOpen {
		t.Fatal("/tasks did not drop the panel down")
	}
	a.ed.Insert("/tasks")
	a.dispatch(ActionSubmit, mustKey(t, "enter"))
	if a.tasksOpen {
		t.Fatal("/tasks did not collapse the panel back to the summary")
	}
	if got := DefaultBindings()["ctrl+t"]; got != ActionTasks {
		t.Fatalf("ctrl+t = %q, want the tasks action", got)
	}
	if _, taken := DefaultBindings()["ctrl+shift+t"]; taken {
		t.Fatal("Tasks acquired an unrequested default shortcut")
	}
	if !a.dispatch(ActionTasks, mustKey(t, "ctrl+t")) || !a.tasksOpen {
		t.Fatal("the tasks action did not drop the panel down")
	}
	if !a.dispatch(ActionTasks, mustKey(t, "ctrl+t")) || a.tasksOpen {
		t.Fatal("the tasks action did not collapse the panel")
	}
}

// TestTasksPanelOpenByDefault pins the starting state: a run that ships tasks opens
// with the panel dropped down, because the summary alone made the reader press a key
// to find out what the run is doing. It opens over nothing when there are no tasks —
// the widget draws nil for an empty list — so the default costs the empty run nothing.
func TestTasksPanelOpenByDefault(t *testing.T) {
	a := New(Config{Width: 80, Height: 24, Out: io.Discard, Emitter: tasksEmitter()})
	if !a.tasksOpen {
		t.Fatal("the panel does not start open")
	}
	a.st = tasksFixture()
	if got := tasksFrameText(t, a); !strings.Contains(got, "◼ Invalidate the cache entry") {
		t.Fatalf("a fresh run does not show the dropped list:\n%s", got)
	}
}

// TestTasksPanelDrawsAndCollapses asserts both states against the rendered frame,
// because the widget being installed is not the claim — the claim is what a reader
// sees. Collapsed, the panel is the summary line and nothing else; open, every task
// is on the screen above the input and the ordering is the panel's own: active first,
// then pending, then completed, whatever order the events arrived in.
func TestTasksPanelDrawsAndCollapses(t *testing.T) {
	a := New(Config{Width: 80, Height: 24, Out: io.Discard, Emitter: tasksEmitter()})
	a.st = tasksFixture()
	a.tasksOpen = false
	collapsed := tasksFrameText(t, a)
	if !strings.Contains(collapsed, "Tasks 3 (1 done, 1 in progress, 1 open)") {
		t.Fatalf("the collapsed panel does not carry the summary:\n%s", collapsed)
	}
	if strings.Contains(collapsed, "Invalidate the cache entry") {
		t.Fatal("the collapsed panel leaked the task list")
	}
	a.tasksOpen = true
	open := tasksFrameText(t, a)
	for _, want := range []string{
		"Invalidate the cache entry", "Run the focused regression test", "Trace stale session cache",
	} {
		if !strings.Contains(open, want) {
			t.Errorf("the open panel is missing %q:\n%s", want, open)
		}
	}
	active := strings.Index(open, "Invalidate")
	pending := strings.Index(open, "Run the focused")
	completed := strings.Index(open, "Trace stale")
	if active > pending || pending > completed {
		t.Errorf("panel order is active %d, pending %d, completed %d — the active task must lead", active, pending, completed)
	}
}

// TestTasksPanelCountsWhatDoesNotFit is the "… +N" row: the panel is bounded, and a
// list longer than the bound is counted rather than truncated in silence.
func TestTasksPanelCountsWhatDoesNotFit(t *testing.T) {
	st := &state.State{}
	for i := 0; i < 8; i++ {
		st.Tasks = append(st.Tasks, &state.Task{Title: "task", Status: event.TaskPending})
	}
	a := New(Config{Width: 80, Height: 24, Out: io.Discard, Emitter: tasksEmitter()})
	a.st = st
	a.tasksOpen = true
	if got := tasksFrameText(t, a); !strings.Contains(got, "… +3") {
		t.Fatalf("the open panel does not count the tasks it left out:\n%s", got)
	}
}

// TestTasksPanelGlyphsAreDrawnAndDeclared keeps the panel on the same glyph
// vocabulary as the task blocks in the transcript: the same three status glyphs,
// none invented and none missing.
func TestTasksPanelGlyphsAreDrawnAndDeclared(t *testing.T) {
	g := ui.DefaultGlyphs()
	g.Track = true
	a := New(Config{Width: 80, Height: 24, Out: io.Discard, Emitter: tasksEmitter(), Glyphs: &g})
	a.st = tasksFixture()
	a.tasksOpen = true
	if got := tasksFrameText(t, a); !strings.Contains(got, "Invalidate") {
		t.Fatalf("the open panel drew no tasks:\n%s", got)
	}
	used := strings.Join(g.Used(), ",")
	for _, key := range []string{"tasks.active", "tasks.completed", "tasks.pending"} {
		if !strings.Contains(used, key) {
			t.Errorf("the panel did not draw glyph %q; used %v", key, used)
		}
	}
	if unknown := g.Unknown(); len(unknown) != 0 {
		t.Fatalf("the panel requested unknown glyphs: %v", unknown)
	}
}

// TestTasksWithoutTasksStaysHidden pins the cost of the panel on a run with no task
// events: nothing. Neither state may draw a frame of chrome for an empty list.
func TestTasksWithoutTasksStaysHidden(t *testing.T) {
	a := New(Config{Width: 80, Height: 24, Out: io.Discard, Emitter: tasksEmitter()})
	a.tasksOpen = true
	if got := tasksFrameText(t, a); strings.Contains(got, "Tasks") {
		t.Fatalf("an empty run drew a Tasks panel:\n%s", got)
	}
}

// TestTasksKeyMutatesNothingButThePanel keeps ctrl+t out of the editor's way: the
// toggle is a view action, and a half-typed line survives it untouched.
func TestTasksKeyMutatesNothingButThePanel(t *testing.T) {
	a := New(Config{Width: 80, Height: 24})
	a.st = tasksFixture()
	a.tasksOpen = false
	a.ed.Insert("draft")
	if !a.dispatch(ActionTasks, mustKey(t, "ctrl+t")) || !a.tasksOpen {
		t.Fatal("ctrl+t did not drop the panel")
	}
	if a.ed.Text() != "draft" {
		t.Fatalf("ctrl+t mutated the editor: %q", a.ed.Text())
	}
}
