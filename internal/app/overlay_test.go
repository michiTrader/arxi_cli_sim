package app

import (
	"strings"
	"testing"

	"arxi.local/sim/internal/state"
)

func TestSlashEffortOpensSlider(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/effort")
	if !a.dispatch(ActionSubmit, mustKey(t, "enter")) {
		t.Fatal("/effort did not change the frame")
	}
	if a.effortSlider == nil {
		t.Fatal("effort slider not opened after /effort")
	}
	if a.overlay != nil {
		t.Fatal("overlay was opened — effort should use the inline widget")
	}
}

func TestSlashEffortWithArgSetsDirectly(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/effort max")
	a.dispatch(ActionSubmit, mustKey(t, "enter"))
	if a.effortSlider != nil {
		t.Fatal("/effort max should not open the slider")
	}
	if a.st.Effort != "max" {
		t.Fatalf("effort=%q, want \"max\"", a.st.Effort)
	}
}

func TestEffortSliderUpDownAndConfirm(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/effort")
	a.dispatch(ActionSubmit, mustKey(t, "enter"))
	if a.effortSlider == nil {
		t.Fatal("slider not opened")
	}
	if !a.dispatch(ActionHistoryNext, mustKey(t, "down")) || a.effortSlider.Selected != 3 {
		t.Fatalf("down selected %d, want 3", a.effortSlider.Selected)
	}
	if !a.dispatch(ActionHistoryPrev, mustKey(t, "up")) || a.effortSlider.Selected != 2 {
		t.Fatalf("up selected %d, want 2", a.effortSlider.Selected)
	}
}

func TestEffortSliderArrowsAndConfirm(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/effort")
	a.dispatch(ActionSubmit, mustKey(t, "enter"))
	if a.effortSlider == nil {
		t.Fatal("slider not opened")
	}

	// Default selection is "high" (index 2). Move right to "xhigh".
	if !a.dispatch(ActionRight, mustKey(t, "right")) {
		t.Fatal("right arrow in slider did nothing")
	}
	if a.effortSlider.Selected != 3 {
		t.Fatalf("after right, selected=%d, want 3", a.effortSlider.Selected)
	}

	// Move left back to "high".
	a.dispatch(ActionLeft, mustKey(t, "left"))
	if a.effortSlider.Selected != 2 {
		t.Fatalf("after left, selected=%d, want 2", a.effortSlider.Selected)
	}

	// Confirm with enter.
	if !a.dispatch(ActionSubmit, mustKey(t, "enter")) {
		t.Fatal("enter in slider did nothing")
	}
	if a.effortSlider != nil {
		t.Fatal("slider still open after enter")
	}
	if a.st.Effort != "high" {
		t.Fatalf("effort=%q after confirm, want \"high\"", a.st.Effort)
	}
}

func TestEffortSliderCancel(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.st.Effort = "medium"
	a.ed.Insert("/effort")
	a.dispatch(ActionSubmit, mustKey(t, "enter"))

	// Move to a different level.
	a.dispatch(ActionRight, mustKey(t, "right"))

	// Cancel with esc.
	a.dispatch(ActionCancel, mustKey(t, "esc"))
	if a.effortSlider != nil {
		t.Fatal("slider still open after cancel")
	}
	// Effort should remain unchanged.
	if a.st.Effort != "medium" {
		t.Fatalf("effort=%q after cancel, want \"medium\" (unchanged)", a.st.Effort)
	}
}

func TestEffortSliderBoundsClamp(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/effort")
	a.dispatch(ActionSubmit, mustKey(t, "enter"))
	es := a.effortSlider

	// Move left past the beginning.
	for i := 0; i < 10; i++ {
		a.dispatch(ActionLeft, mustKey(t, "left"))
	}
	if es.Selected != 0 {
		t.Fatalf("after many lefts, selected=%d, want 0", es.Selected)
	}

	// Move right past the end.
	for i := 0; i < 20; i++ {
		a.dispatch(ActionRight, mustKey(t, "right"))
	}
	if es.Selected != len(effortLevels)-1 {
		t.Fatalf("after many rights, selected=%d, want %d", es.Selected, len(effortLevels)-1)
	}
}

func TestSlashUnknownCommandIsIgnored(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/foo")
	changed := a.dispatch(ActionSubmit, mustKey(t, "enter"))
	if changed {
		t.Fatal("/foo should not change the frame")
	}
	if a.effortSlider != nil {
		t.Fatal("unknown slash opened a slider")
	}
	if a.overlay != nil {
		t.Fatal("unknown slash opened an overlay")
	}
}

func TestSlashEffortCaseInsensitive(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.ed.Insert("/effort HIGH")
	a.dispatch(ActionSubmit, mustKey(t, "enter"))
	if a.st.Effort != "high" {
		t.Fatalf("effort=%q, want \"high\" (case-insensitive)", a.st.Effort)
	}
}

func TestSlashEffortUnknownLevelIgnored(t *testing.T) {
	a := New(Config{Width: 72, Height: 24})
	a.st.Effort = "medium"
	a.ed.Insert("/effort banana")
	a.dispatch(ActionSubmit, mustKey(t, "enter"))
	if a.st.Effort != "medium" {
		t.Fatalf("effort=%q, want \"medium\" (unchanged after unknown level)", a.st.Effort)
	}
}

func TestEffortSliderBuildReturnsNil(t *testing.T) {
	es := NewEffortSlider("high", 50)
	o := es.Build()
	if o != nil {
		t.Fatal("Build should return nil — effort is no longer an overlay")
	}
}

func TestEffortWidgetRendersVerticalOrder(t *testing.T) {
	es := NewEffortSlider("high", 72)
	ew := EffortWidget{Slider: es}
	if ew.Name() != "effort" {
		t.Fatalf("name=%q, want \"effort\"", ew.Name())
	}
	rows := ew.Render(72, 12, defaultGlyphs(t))
	if len(rows) != 9 {
		t.Fatalf("EffortWidget rendered %d rows, want 9", len(rows))
	}
	text := make([]string, len(rows))
	for i, row := range rows {
		text[i] = row.Text()
	}
	joined := strings.Join(text, "\n")
	last := -1
	for _, level := range effortLevels {
		at := strings.Index(joined, level)
		if at < 0 || at <= last {
			t.Fatalf("level %q missing or out of order:\n%s", level, joined)
		}
		last = at
	}
	if !strings.Contains(joined, "● high") || !strings.Contains(joined, "↑/↓ move") {
		t.Fatalf("vertical selector lacks marker or key hint:\n%s", joined)
	}
}

func TestEffortWidgetShortWindowKeepsSelectionVisible(t *testing.T) {
	for selected, level := range effortLevels {
		es := NewEffortSlider(level, 32)
		rows := (EffortWidget{Slider: es}).Render(32, 3, defaultGlyphs(t))
		if len(rows) != 3 {
			t.Fatalf("%s rendered %d rows at height 3", level, len(rows))
		}
		joined := ""
		for _, row := range rows {
			if row.Width() > 32 {
				t.Fatalf("%s overflowed: %q", level, row.Text())
			}
			joined += row.Text() + "\n"
		}
		if !strings.Contains(joined, "● "+effortLevels[selected]) {
			t.Fatalf("selection %q is outside short window:\n%s", level, joined)
		}
	}
}

func TestEffortWidgetBelow24UsesOnlyOptions(t *testing.T) {
	rows := (EffortWidget{Slider: NewEffortSlider("max", 20)}).Render(20, 4, defaultGlyphs(t))
	if len(rows) != 4 {
		t.Fatalf("rendered %d compact rows, want 4", len(rows))
	}
	for _, row := range rows {
		if strings.Contains(row.Text(), "Thinking") || strings.Contains(row.Text(), "confirm") || strings.Contains(row.Text(), "Maximum") {
			t.Fatalf("compact row contains chrome or description: %q", row.Text())
		}
	}
}

func TestEffortWidgetAnimatedOnMax(t *testing.T) {
	es := NewEffortSlider("max", 72)
	ew := EffortWidget{Slider: es}
	if !ew.Animated() {
		t.Fatal("EffortWidget should be animated on max")
	}

	es2 := NewEffortSlider("high", 72)
	ew2 := EffortWidget{Slider: es2}
	if ew2.Animated() {
		t.Fatal("EffortWidget should not be animated on high")
	}
}

func TestTeamOverlayRemediesCoverEveryStructuredBlock(t *testing.T) {
	cases := []struct {
		on   string
		ref  map[string]any
		want string
	}{
		{"approval", map[string]any{"inbox_id": "in-1"}, "remedy: arxi inbox approve in-1"},
		{"lock", map[string]any{"key": "file:auth.go"}, "remedy: arxi state unlock run-9 file:auth.go"},
		{"budget", nil, "remedy: arxi run unpause run-9 --budget <higher>"},
		{"workspace", nil, "remedy: arxi run show run-9 --workspace"},
		{"peer", map[string]any{"peer": "scout"}, "waiting for peer scout"},
		{"timer", map[string]any{"timer_id": "timer-2"}, "waiting for timer timer-2"},
		{"tool", map[string]any{"tool": "bash"}, "waiting for tool bash"},
		{"other", map[string]any{"z": 2, "a": "first"}, "blocked reference: a=first, z=2"},
		{"other", nil, "blocked reference: schema violation: missing structured reference"},
	}
	for _, tc := range cases {
		t.Run(tc.on+tc.want, func(t *testing.T) {
			if got := memberRemedy("run-9", &state.Blocked{On: tc.on, Ref: tc.ref}); got != tc.want {
				t.Fatalf("remedy = %q, want %q", got, tc.want)
			}
		})
	}
}
