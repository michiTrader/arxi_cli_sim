package app

import (
	"bytes"
	"strings"
	"testing"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

func tasksFixture() *state.State {
	return &state.State{Tasks: []*state.Task{
		{ID: "task-1", Title: "Trace stale session cache", Detail: "Find where the old token survives rotation.", Owner: "scout", Status: event.TaskCompleted},
		{ID: "task-2", Title: "Invalidate the cache entry", Detail: "Delete the process cache key after storage succeeds.", Owner: "builder", Status: event.TaskActive},
		{ID: "task-3", Title: "Run the focused regression test", Detail: "Verify rotation reads the new token.", Owner: "reviewer", Status: event.TaskPending},
	}}
}

func TestTasksFrameBuildsLiveTaskList(t *testing.T) {
	st := tasksFixture()
	f := renderTasks(st, ui.Viewport{Width: 80, Height: 14}, 0, ui.DefaultGlyphs())
	got := f.Plain()
	for _, want := range []string{
		"Tasks  1/3 completed · 1 active · 1 pending", "completed  Trace stale session cache", "owner scout",
		"active  Invalidate the cache entry", "pending  Run the focused regression test", "Esc back",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Tasks frame does not contain %q:\n%s", want, got)
		}
	}
	if len(f.Live) != 14 || !f.Cursor.Hidden || len(f.Committed) != 0 {
		t.Fatalf("frame geometry = live %d hidden %v committed %d", len(f.Live), f.Cursor.Hidden, len(f.Committed))
	}
	st.Tasks[1].Status = event.TaskCompleted
	st.Tasks[1].Detail = "Changed after the first render."
	live := renderTasks(st, ui.Viewport{Width: 80, Height: 14}, 0, ui.DefaultGlyphs()).Plain()
	if !strings.Contains(live, "2/3 completed") || !strings.Contains(live, "Changed after the first render.") {
		t.Fatalf("Tasks frame retained a stale snapshot:\n%s", live)
	}
}

func TestTasksFrameFitsResponsiveSurfaces(t *testing.T) {
	for _, width := range []int{20, 31, 48, 72} {
		for _, height := range []int{1, 2, 3, 8} {
			f := renderTasks(tasksFixture(), ui.Viewport{Width: width, Height: height}, 1000, ui.DefaultGlyphs())
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

func TestTasksFrameHandlesNoTasksAtEveryHeight(t *testing.T) {
	for _, st := range []*state.State{nil, state.New()} {
		for height := 0; height <= 4; height++ {
			f := renderTasks(st, ui.Viewport{Width: 40, Height: height}, 1000, ui.DefaultGlyphs())
			if len(f.Live) != height {
				t.Errorf("state %p height %d: live=%d", st, height, len(f.Live))
			}
			if bad := f.Overflow(); len(bad) != 0 {
				t.Errorf("state %p height %d overflows at %v:\n%s", st, height, bad, f.Plain())
			}
		}
	}
	if got := renderTasks(state.New(), ui.Viewport{Width: 40, Height: 4}, 0, ui.DefaultGlyphs()).Plain(); !strings.Contains(got, "No tasks in this run.") {
		t.Fatalf("empty Tasks view = %q", got)
	}
}

func TestTasksFrameScrollsCreationOrder(t *testing.T) {
	st := tasksFixture()
	first := renderTasks(st, ui.Viewport{Width: 31, Height: 5}, 0, ui.DefaultGlyphs())
	if first.Scroll.Below == 0 || !strings.Contains(first.Plain(), "Trace stale") {
		t.Fatalf("first window = %+v\n%s", first.Scroll, first.Plain())
	}
	last := renderTasks(st, ui.Viewport{Width: 31, Height: 5}, 1000, ui.DefaultGlyphs())
	if last.Scroll.Below != 0 || last.Scroll.Above == 0 || !strings.Contains(last.Plain(), "owner reviewer") {
		t.Fatalf("last window = %+v\n%s", last.Scroll, last.Plain())
	}
}

func TestTasksDrawUsesCurrentFoldedState(t *testing.T) {
	var out bytes.Buffer
	em := &ui.Emitter{Theme: ui.DefaultTheme(), Mode: ui.ModeInline, Profile: ui.ProfileMono}
	a := New(Config{Width: 80, Height: 10, Out: &out, Emitter: em})
	a.st = tasksFixture()
	a.openTasks()
	if err := a.draw(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "Trace") || !strings.Contains(got, "Invalidate") {
		t.Fatalf("draw did not emit current Tasks list: %q", got)
	}
}

func TestTasksViewKeyIsolationAndConversationRestoration(t *testing.T) {
	a := New(Config{Width: 31, Height: 5})
	a.st = tasksFixture()
	a.ed.Insert("draft")
	a.scrolled, a.top = true, 7
	a.openTasks()
	if a.view != viewTasks {
		t.Fatal("Tasks view did not open")
	}
	if err := a.draw(); err != nil {
		t.Fatal(err)
	}
	before := a.ed.Text()
	if a.dispatch(ActionSubmit, mustKey(t, "enter")) {
		t.Fatal("enter changed Tasks view")
	}
	a.dispatch(ActionNone, term.Key{Type: term.KeyRunes, Runes: []rune{'x'}})
	if a.ed.Text() != before {
		t.Fatalf("Tasks key mutated editor: %q", a.ed.Text())
	}
	if !a.dispatch(ActionEnd, mustKey(t, "end")) || a.viewTop == 0 {
		t.Fatalf("end did not move local Tasks scroll: top=%d below=%d", a.viewTop, a.viewBelow)
	}
	if !a.dispatch(ActionHome, mustKey(t, "home")) || a.viewTop != 0 {
		t.Fatalf("home did not restore local Tasks top: %d", a.viewTop)
	}
	if !a.dispatch(ActionPageDown, mustKey(t, "pgdown")) || a.viewTop == 0 {
		t.Fatalf("page down did not move local Tasks scroll: %d", a.viewTop)
	}
	a.dispatch(ActionCancel, mustKey(t, "esc"))
	if a.view != viewConversation || !a.scrolled || a.top != 7 || a.ed.Text() != "draft" {
		t.Fatalf("conversation not restored: view=%v scroll=%v/%d text=%q", a.view, a.scrolled, a.top, a.ed.Text())
	}
}

func TestTasksViewInterruptClosesWithoutArmingQuit(t *testing.T) {
	a := New(Config{Width: 40, Height: 8})
	a.openTasks()
	if !a.dispatch(ActionInterrupt, mustKey(t, "ctrl+c")) {
		t.Fatal("ctrl+c did not close Tasks view")
	}
	if a.view != viewConversation || a.armed || a.quit {
		t.Fatalf("interrupt left view=%v armed=%v quit=%v", a.view, a.armed, a.quit)
	}
}

func TestSlashTasksUsesFreshDedicatedView(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.openTasks()
	a.viewTop = 9
	a.closeFullView()
	a.ed.Insert("/tasks")
	if !a.dispatch(ActionSubmit, mustKey(t, "enter")) || a.view != viewTasks || a.viewTop != 0 {
		t.Fatal("/tasks did not use fresh dedicated Tasks entry")
	}
	if _, ok := DefaultBindings()["ctrl+shift+t"]; ok {
		t.Fatal("Tasks acquired an unrequested default shortcut")
	}
}

func TestTasksGlyphsAreDrawnAndDeclared(t *testing.T) {
	g := ui.DefaultGlyphs()
	g.Track = true
	_ = renderTasks(tasksFixture(), ui.Viewport{Width: 80, Height: 14}, 0, g)
	got := strings.Join(g.Used(), ",")
	for _, key := range []string{"tasks.active", "tasks.completed", "tasks.pending"} {
		if !strings.Contains(got, key) {
			t.Errorf("Tasks view did not draw glyph %q; used %v", key, g.Used())
		}
	}
	if unknown := g.Unknown(); len(unknown) != 0 {
		t.Fatalf("Tasks view requested unknown glyphs: %v", unknown)
	}
}
