package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// topRule returns the frame's top border row, which is where a title lives.
func topRule(t *testing.T, rows []Line) Line {
	t.Helper()
	tl := DefaultGlyphs().Get("frame.tl")
	for _, l := range rows {
		if strings.HasPrefix(l.Text(), tl) {
			return l
		}
	}
	t.Fatalf("no top border in %d rows", len(rows))
	return nil
}

// TestInputTitleShineLightsOnlyTheTitle pins the band the player arms on the working
// verb in the border: it is lit across the title and measured against the title, so
// the border still measures exactly the width it measured and nothing outside the
// word changes style. It is also the reason the field exists at all — the box's own
// shine means the opposite of what this one says, and one field cannot carry both.
func TestInputTitleShineLightsOnlyTheTitle(t *testing.T) {
	g := DefaultGlyphs()
	in := &Input{Title: "working", TitleShine: Shimmer{Style: StatusShine, Phase: 0}}
	rows, _ := in.Render(80, g)
	rule := topRule(t, rows)
	if got := rule.Width(); got != 80 {
		t.Fatalf("the top border is %d columns at a width of 80", got)
	}
	lit := 0
	for _, sp := range rule {
		if strings.HasPrefix(sp.Style, StatusShine) {
			lit += ansi.StringWidth(sp.Text)
		}
	}
	if lit == 0 {
		t.Fatalf("no column of the title is lit:\n%s", rule.Text())
	}
	// The band is measured against the nine-column title, so it can never reach the
	// corners of an eighty-column rule.
	if lit > 20 {
		t.Errorf("%d columns are lit, want a band fitted to the title", lit)
	}
	// And a plain title stays plain: the zero value is the switch.
	plain := &Input{Title: "branch: main"}
	rows, _ = plain.Render(80, g)
	for _, sp := range topRule(t, rows) {
		if strings.HasPrefix(sp.Style, StatusShine) {
			t.Fatalf("an unshined title drew %q in %q", sp.Text, sp.Style)
		}
	}
}

// TestHistoryLabelOutranksTheWorkingTitle is the priority the border's two tenants
// settle by seniority: History was written into it first and keeps it. While the
// reader walks their own turns, the border names the entry; the run's verb returns
// the moment the label is gone.
func TestHistoryLabelOutranksTheWorkingTitle(t *testing.T) {
	g := DefaultGlyphs()
	in := NewInput()
	in.Insert("earlier turn")
	in.Submit()
	in.Insert("draft")
	in.Older() // now showing "earlier turn" under the History label
	in.Title = "◼ working"
	rows, _ := in.Render(80, g)
	rule := topRule(t, rows).Text()
	if !strings.Contains(rule, " History 1/1 ") {
		t.Fatalf("the border is %q, want the history label over the working title", rule)
	}
	if strings.Contains(rule, "working") {
		t.Fatalf("the border is %q, and the working title leaked through the label", rule)
	}
}

// TestInputAnimatedCoversBothShims is the timer contract: the editor asks for ticks
// while either band is armed, and its precise answer is the sooner of the two — so
// a title shim in its dark rest still parks the loop even while the box's own band
// would have kept ticking, and the reverse holds too.
func TestInputAnimatedCoversBothShims(t *testing.T) {
	in := &Input{}
	if in.Animated() || in.NextVisualChange() != 0 {
		t.Fatal("an editor with no bands armed asked for ticks")
	}
	in.TitleShine = Shimmer{Style: StatusShine, Phase: 3, Period: 40, Travel: 8}
	if !in.Animated() {
		t.Fatal("a title shine armed did not arm the editor")
	}
	if got := in.NextVisualChange(); got != 1 {
		t.Errorf("a moving title band reports %d ticks, want 1", got)
	}
	in.TitleShine.Phase = 20 // inside the dark rest
	if got := in.NextVisualChange(); got != 20 {
		t.Errorf("a resting title band reports %d ticks, want the 20 left in its rest", got)
	}
	in.Shine = Shimmer{Style: InputShine, Phase: 0}
	if got := in.NextVisualChange(); got != 1 {
		t.Errorf("with both armed the answer is %d, want the box band's 1", got)
	}
}
