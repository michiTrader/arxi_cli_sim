package app

import (
	"testing"

	"arxi.local/sim/internal/ui"
)

func TestSlashMenuAppearsOnSlash(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/")
	a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
	if !a.smenu.Active() {
		t.Fatal("menu should be active after typing /")
	}
	if len(a.smenu.items) != len(slashRegistry) {
		t.Fatalf("got %d items, want %d", len(a.smenu.items), len(slashRegistry))
	}
}

func TestSlashMenuFilters(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/eff")
	a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
	if !a.smenu.Active() {
		t.Fatal("menu should be active for /eff")
	}
	for _, e := range a.smenu.items {
		if e.Name != "effort" {
			t.Fatalf("unexpected item %q for /eff", e.Name)
		}
	}
}

func TestSlashMenuRegistersTasks(t *testing.T) {
	m := slashMenuFor("/tasks", slashMenu{})
	if !m.Active() || len(m.items) != 1 || m.Selected().Name != "tasks" {
		t.Fatalf("/tasks menu = %#v", m.items)
	}
	if m.Selected().Desc == "" {
		t.Fatal("/tasks has no menu description")
	}
}

func TestSlashMenuDismissedOnSpace(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/effort high")
	a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
	if a.smenu.Active() {
		t.Fatal("menu should be dismissed once a space is present")
	}
}

func TestSlashMenuDismissedNoMatch(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/zzz")
	a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
	if a.smenu.Active() {
		t.Fatal("menu should be dismissed when nothing matches")
	}
}

func TestSlashMenuUpDownWraps(t *testing.T) {
	m := slashMenuFor("/", slashMenu{})
	if !m.Active() {
		t.Skip("no registry items")
	}
	// Move up from the top should wrap to the last item.
	m2 := m.moveUp()
	if m2.sel != len(m2.items)-1 {
		t.Fatalf("moveUp from 0: sel=%d, want %d", m2.sel, len(m2.items)-1)
	}
	// And down from there wraps back to 0.
	m3 := m2.moveDown()
	if m3.sel != 0 {
		t.Fatalf("moveDown from last: sel=%d, want 0", m3.sel)
	}
}

func TestSlashMenuSubmitExecutesCommand(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/")
	a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
	if !a.smenu.Active() {
		t.Fatal("menu should be active")
	}

	a.dispatch(ActionSubmit, mustKey(t, "enter"))

	// Enter should execute the command (effort opens the slider) and clear the input.
	if a.ed.Text() != "" {
		t.Fatalf("after submit, input=%q, want empty (command executed)", a.ed.Text())
	}
	if a.smenu.Active() {
		t.Fatal("menu should be dismissed after submit")
	}
	if a.effortSlider == nil {
		t.Fatal("effort slider should have opened from menu submit")
	}
}

func TestSlashMenuTabFillsInput(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/")
	a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
	if !a.smenu.Active() {
		t.Fatal("menu should be active")
	}
	sel := a.smenu.Selected()
	expected := "/" + sel.Name + " "

	a.dispatch(ActionComplete, mustKey(t, "tab"))

	if a.ed.Text() != expected {
		t.Fatalf("after tab, input=%q, want %q", a.ed.Text(), expected)
	}
	if a.smenu.Active() {
		t.Fatal("menu should be dismissed after tab")
	}
}

func TestSlashMenuWidgetRender(t *testing.T) {
	m := slashMenuFor("/", slashMenu{})
	if !m.Active() {
		t.Skip("no registry items")
	}
	wd := SlashMenuWidget{Menu: m}
	if wd.Name() != "slashmenu" {
		t.Fatalf("name=%q", wd.Name())
	}
	lines := wd.Render(60, 0, defaultGlyphs(t))
	if len(lines) == 0 {
		t.Fatal("widget rendered nothing")
	}
}

func TestSlashMenuCancelDismisses(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/")
	a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
	if !a.smenu.Active() {
		t.Fatal("menu should be active")
	}
	a.dispatch(ActionCancel, mustKey(t, "esc"))
	if a.smenu.Active() {
		t.Fatal("menu should be dismissed after cancel")
	}
}

// defaultGlyphs returns a Glyphs for testing.
func defaultGlyphs(t *testing.T) ui.Glyphs {
	t.Helper()
	return ui.DefaultGlyphs()
}
