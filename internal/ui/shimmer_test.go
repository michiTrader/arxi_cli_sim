package ui

import (
	"math"
	"strings"
	"testing"
)

// The shimmer is the one thing in this package that moves on its own, and what it moves over is
// every other thing in it: a diff row, a prompt band, a frame rule, a half-typed line. So most
// of what is pinned here is what it must not do — text it may not touch, columns it may not
// skip, ticks it has to stay dark for — and only then the light itself.

// keysOf spreads a Line into one style key per column, which is the shape most of these tests
// want: a band is a claim about columns and a Line is a list of runs. Ascii only, so that a rune
// is a column and the two measurements cannot disagree.
func keysOf(t *testing.T, l Line) []string {
	t.Helper()
	var out []string
	for _, sp := range l {
		for range sp.Text {
			out = append(out, sp.Style)
		}
	}
	if len(out) != l.Width() {
		t.Fatalf("the row measures %d columns and spreads into %d", l.Width(), len(out))
	}
	return out
}

// row is w columns of one span in one key: the plainest thing a band can cross, and the one that
// makes every cut visible, since a column the light touched then differs from the column beside
// it and a column it missed still says what it always said.
func row(w int, key string) Line {
	return Line{{Text: strings.Repeat("x", w), Style: key}}
}

// rungOf places a key on its ladder, and fails on a key that is not on it at all — which is how
// a band drawn in an undeclared or misspelled step is caught here rather than in the theme test,
// where it would arrive as a colour that silently resolves to nothing.
func rungOf(t *testing.T, ramp []string, key string) int {
	t.Helper()
	for i, k := range ramp {
		if k == key {
			return i
		}
	}
	t.Fatalf("%q is not a rung of %v", key, ramp)
	return 0
}

// TestZeroShimmerIsOff is the one this file exists for most. Every golden file in the package,
// both corpus sweeps and every widget test builds its rows without arming a band, so if the zero
// value ever drew anything the animation would have quietly rewritten the whole suite's idea of
// what a frame looks like — and it would have done it in a way that still passed, because those
// tests were regenerated after it.
func TestZeroShimmerIsOff(t *testing.T) {
	var s Shimmer
	if s.On() {
		t.Error("the zero shimmer says it is on")
	}
	for _, phase := range []int{0, 1, 5, 12, 37, 40, 1000} {
		s.Phase = phase
		if from, to, ok := s.Band(72); ok {
			t.Errorf("phase %d of a shimmer with no key lit %d..%d", phase, from, to)
		}
		in := row(72, "input.text")
		got := s.Apply(in, 72)
		if len(got) != len(in) || got[0] != in[0] {
			t.Errorf("phase %d of a shimmer with no key rewrote the row", phase)
		}
	}
}

// TestTheBandIsBrightestInTheMiddle is the request that brought the ladder in: a band drawn in
// one key has two vertical edges and reads as a block, and what a reflection has instead is a
// falloff. Four things make it one — every rung is used, the core is in the middle, the steps
// only ever go one at a time, and the two halves match — and all four are checked, because three
// of them can hold while the fourth is what a reader would call ungraded.
func TestTheBandIsBrightestInTheMiddle(t *testing.T) {
	s := Shimmer{Style: InputShine, Phase: 5}
	from, to, ok := s.Band(72)
	if !ok {
		t.Fatal("phase 5 of 40 lights nothing: the shipped constants moved")
	}
	if from <= 0 || to >= 72 {
		t.Fatalf("the band is %d..%d, which is not wholly inside a 72-column row", from, to)
	}
	got := keysOf(t, s.Apply(row(72, "input.text"), 72))
	ramp := ramps[InputShine]
	var rungs []int
	for c := from; c < to; c++ {
		rungs = append(rungs, rungOf(t, ramp, got[c]))
	}
	for c := 0; c < 72; c++ {
		if (c < from || c >= to) && got[c] != "input.text" {
			t.Fatalf("column %d is outside the band and drawn in %q", c, got[c])
		}
	}
	// The outermost rung has to appear, and it is the one a bug loses: a ladder measured against
	// the visible part of the band instead of the whole of it puts the core wherever the band was
	// clipped, and the far step then only ever shows on one side.
	if rungs[0] != len(ramp)-1 || rungs[len(rungs)-1] != len(ramp)-1 {
		t.Errorf("the band starts on rung %d and ends on %d, want %d at both ends",
			rungs[0], rungs[len(rungs)-1], len(ramp)-1)
	}
	if mid := rungs[len(rungs)/2]; mid != 0 {
		t.Errorf("the middle column of the band is on rung %d, want the brightest", mid)
	}
	for i := 1; i < len(rungs); i++ {
		if d := rungs[i] - rungs[i-1]; d < -1 || d > 1 {
			t.Errorf("the ladder jumps from rung %d to %d between columns %d and %d",
				rungs[i-1], rungs[i], from+i-1, from+i)
		}
	}
	for i, j := 0, len(rungs)-1; i < j; i, j = i+1, j-1 {
		if rungs[i] != rungs[j] {
			t.Errorf("column %d is on rung %d and its mirror %d is on %d: the light has a side",
				from+i, rungs[i], from+j, rungs[j])
		}
	}
	// And the steps are even, which is what "more progressive" asked for: sixteen columns over
	// four rungs is four columns a rung, so no rung may be a single column while another is six.
	wide := map[int]int{}
	for _, r := range rungs {
		wide[r]++
	}
	if len(wide) != len(ramp) {
		t.Fatalf("the band uses %d of the ladder's %d rungs", len(wide), len(ramp))
	}
	for r, n := range wide {
		if n < 2 {
			t.Errorf("rung %d is %d column(s) wide, which is a stripe rather than a step", r, n)
		}
	}
}

// TestTheBandLeavesTheTextAlone is the property that lets Apply run last, after wrapping and
// padding and the overflow check, without being able to invalidate any of them. It is also the
// one a gradient could plausibly break: the light is seven passes over the same Line now, each
// cutting the spans the last one left, and a cut that dropped or duplicated a byte would be
// invisible in a colour test and obvious on a terminal.
func TestTheBandLeavesTheTextAlone(t *testing.T) {
	// Two spans and a wide rune, because the cut has to land somewhere awkward: a straddle, a
	// boundary between spans, and a grapheme cutCols is allowed to refuse to split.
	in := Line{
		{Text: "the quick brown fox ", Style: "md.text"},
		{Text: "→ jumps over it", Style: "md.em"},
	}
	want, wantW := in.Text(), in.Width()
	for _, key := range []string{InputShine, StatusShine, "md.code"} {
		for _, w := range []int{1, 2, 3, 7, 8, 35, 36, 72, 200} {
			for phase := -3; phase < 45; phase++ {
				s := Shimmer{Style: key, Phase: phase}
				got := s.Apply(in, w)
				if got.Text() != want {
					t.Fatalf("%s at width %d phase %d rewrote the row to %q",
						key, w, phase, got.Text())
				}
				if got.Width() != wantW {
					t.Fatalf("%s at width %d phase %d remeasured the row as %d, want %d",
						key, w, phase, got.Width(), wantW)
				}
			}
		}
	}
}

// TestTheLightGoesUnderTheBandsItCrosses is the composition seam, and the reason a Span carries
// two keys. A shine that simply overwrote Style would take the prompt's wash and a diff row's
// background off every column it touched, which on a diff is a white stripe sliding through the
// change the reader is trying to read.
func TestTheLightGoesUnderTheBandsItCrosses(t *testing.T) {
	s := Shimmer{Style: InputShine, Phase: 5}
	from, to, ok := s.Band(72)
	if !ok {
		t.Fatal("phase 5 of 40 lights nothing: the shipped constants moved")
	}
	// A row carrying a background already, the way a diff row and a prompt turn both do.
	in := Line{{Text: strings.Repeat("y", 72), Style: "diff.context", Fill: "diff.added"}}
	for _, sp := range s.Apply(in, 72) {
		if sp.Fill != "diff.added" {
			t.Fatalf("a lit span kept %q as its fill, want the band it was crossing", sp.Fill)
		}
	}
	// And a row carrying only a foreground: a lit span's own key moves down to Fill rather than
	// being dropped, so whatever it said that the shine does not say is still said — while a
	// column the light never reached keeps both of its keys exactly as they arrived. Stated by
	// column and not by span because that is the half a bug gets wrong: dropping the key on the
	// way down leaves an unlit row looking right and a lit one unstyled underneath the glint.
	col := 0
	for _, sp := range s.Apply(row(72, "input.placeholder"), 72) {
		for range sp.Text {
			switch inside := col >= from && col < to; {
			case inside && (sp.Fill != "input.placeholder" || sp.Style == "input.placeholder"):
				t.Fatalf("lit column %d is %q over %q, want the shine over the row's own key",
					col, sp.Style, sp.Fill)
			case !inside && (sp.Style != "input.placeholder" || sp.Fill != ""):
				t.Fatalf("unlit column %d is %q over %q, want the row's own key and nothing else",
					col, sp.Style, sp.Fill)
			}
			col++
		}
	}
}

// TestTheLightCrossesTheWholeRowWithoutTearing is the sweep itself, written as the four things a
// reader would notice going wrong: the band only ever moves right, it never opens a gap between
// one frame and the next, it passes over every column before the pass ends, and it never grows
// into a wash. Geometry does not consult the ladder, so one key states it for all of them.
//
// The wide widths are the ones that used to fail and the reason reach widens a band now. Travel
// is ticks and not columns, so the jump per tick grows with the row while a configured sixteen
// columns does not, and past 192 the light was landing rather than sliding: eight columns of a
// 200-column row were never lit at all, and what a reader sees is a dotted trail. 192 and 193 are
// both here because that is the boundary, and 1000 because the fix has to hold at any width and
// not merely at the next one somebody tries.
func TestTheLightCrossesTheWholeRowWithoutTearing(t *testing.T) {
	for _, w := range []int{1, 2, 3, 7, 8, 40, 72, 120, 192, 193, 200, 300, 1000} {
		s := Shimmer{Style: InputShine}
		lit := make([]bool, w)
		prevFrom, prevTo, seen := 0, 0, false
		for phase := 0; phase < shinePeriod; phase++ {
			s.Phase = phase
			from, to, ok := s.Band(w)
			if !ok {
				continue
			}
			if to-from > max(1, w/2) {
				t.Fatalf("on %d columns the band is %d..%d, which is more than half the row",
					w, from, to)
			}
			switch {
			case !seen && from != 0:
				t.Errorf("on %d columns the light appears at column %d, want it entering from the left edge",
					w, from)
			case seen && (from < prevFrom || to < prevTo):
				t.Fatalf("on %d columns the band went from %d..%d back to %d..%d",
					w, prevFrom, prevTo, from, to)
			case seen && from > prevTo:
				t.Fatalf("on %d columns %d..%d is followed by %d..%d, which skips column %d",
					w, prevFrom, prevTo, from, to, prevTo)
			}
			for c := from; c < to; c++ {
				lit[c] = true
			}
			prevFrom, prevTo, seen = from, to, true
		}
		if !seen {
			t.Fatalf("on %d columns nothing is ever lit", w)
		}
		if prevTo != w {
			t.Errorf("on %d columns the light stops at column %d, want it leaving past the right edge",
				w, prevTo)
		}
		for c, on := range lit {
			if !on {
				t.Fatalf("on %d columns column %d is never lit by a whole pass", w, c)
			}
		}
	}
}

// TestApplyLightsExactlyTheColumnsBandNames ties the exported question to what is actually drawn.
// Band is the only part of this type a caller outside the package can ask anything of, and no
// widget in here uses it — both of them call Apply — so nothing else would notice if the two
// disagreed, and a caller sizing a frame rule to Band would be sizing it to a guess.
func TestApplyLightsExactlyTheColumnsBandNames(t *testing.T) {
	for _, key := range []string{InputShine, StatusShine, "md.code"} {
		for _, w := range []int{7, 40, 72, 200} {
			for phase := 0; phase < shinePeriod; phase++ {
				s := Shimmer{Style: key, Phase: phase}
				from, to, ok := s.Band(w)
				for c, k := range keysOf(t, s.Apply(row(w, "input.text"), w)) {
					if inside := ok && c >= from && c < to; inside == (k == "input.text") {
						t.Fatalf("%s at width %d phase %d lights %d..%d (%v) and draws column %d in %q",
							key, w, phase, from, to, ok, c, k)
					}
				}
			}
		}
	}
}

// TestTheFalloffSurvivesBeingClipped is the gradient at every other tick of the pass, and the
// ticks that matter are the two ends: a band still entering from the left has its core off the
// screen, so what is visible is one flank of the light and nothing else. A ladder measured against
// the visible part instead of the whole band would put the brightest rung on column zero there,
// which is a white edge flashing as the glint arrives — and it is the bug the reach/Band split
// exists to prevent, so it needs a test that fails when the two are collapsed back together.
func TestTheFalloffSurvivesBeingClipped(t *testing.T) {
	ramp := ramps[InputShine]
	for _, w := range []int{40, 72, 120} {
		for phase := 0; phase < shinePeriod; phase++ {
			s := Shimmer{Style: InputShine, Phase: phase}
			from, to, ok := s.Band(w)
			if !ok {
				continue
			}
			keys := keysOf(t, s.Apply(row(w, "input.text"), w))
			rungs := make([]int, 0, to-from)
			for c := from; c < to; c++ {
				rungs = append(rungs, rungOf(t, ramp, keys[c]))
			}
			peak := 0
			for i, r := range rungs {
				if r < rungs[peak] {
					peak = i
				}
			}
			for i := 1; i < len(rungs); i++ {
				d := rungs[i] - rungs[i-1]
				switch {
				case d < -1 || d > 1:
					t.Fatalf("width %d phase %d: the ladder jumps from rung %d to %d at column %d",
						w, phase, rungs[i-1], rungs[i], from+i)
				case i <= peak && d > 0:
					t.Fatalf("width %d phase %d: %v dims before its brightest column",
						w, phase, rungs)
				case i > peak && d < 0:
					t.Fatalf("width %d phase %d: %v brightens again after column %d",
						w, phase, rungs, from+peak)
				}
			}
		}
	}
}

// TestThePassIsFollowedByRest is the half of the cycle nothing is drawn in, and it is the half
// the reader asked for by name: a glint that came round twice as often would be a strobe at the
// foot of a window they are trying to read. Stated through reach rather than Band because Band
// is allowed to be dark during the pass — on a one-column row the light is off the left edge for
// half of it — and what is being pinned here is the clock, not the clipping.
func TestThePassIsFollowedByRest(t *testing.T) {
	for _, s := range []Shimmer{
		{Style: InputShine},
		Shimmer{Style: InputShine}.Slower(2),
		{Style: StatusShine, Travel: 3, Period: 7},
		{Style: StatusShine, Travel: 5}, // a configured pass on the shipped period
		// A pass as long as the shipped period, which is the case norm's floor is written for and
		// the only one that can see it: a Period floored at shinePeriod alone would come back as
		// forty ticks of travel in a forty-tick cycle, every tick of it lit, and the band would
		// wrap to the left edge on the tick after it left the right one. Travel+1 is the answer,
		// and one dark tick in forty-one is little enough that the assertion below — lit exactly
		// while phase%Period < Travel — is the only place it shows.
		{Style: StatusShine, Travel: shinePeriod},
	} {
		n := s.norm()
		if n.Period <= n.Travel {
			t.Fatalf("%+v normalizes to no dark half at all: a pass would wrap into the next", s)
		}
		for phase := 0; phase < 3*n.Period; phase++ {
			s.Phase = phase
			if _, _, ok := s.reach(72); ok != (phase%n.Period < n.Travel) {
				t.Fatalf("travel %d of period %d: phase %d is lit=%v", n.Travel, n.Period, phase, ok)
			}
		}
	}
	// The shipped cadence itself, as the one relation between the two numbers that is not a
	// matter of taste. They have moved together once already — twelve ticks and forty where it
	// was eighteen and thirty-six, which is a faster pass coming round less often, both of which
	// the reader asked for in the same sentence — so what is pinned is that most of the cycle
	// stays dark rather than the pair of numbers that happen to satisfy it today.
	if 2*shineTravel >= shinePeriod {
		t.Errorf("the shipped pass is %d ticks of %d, so the row is lit for most of the cycle",
			shineTravel, shinePeriod)
	}
}

// TestAPhaseOutsideOnePeriodStillShines covers the modulo, and the reason it is doubled. Phase is
// whatever counter the caller already runs — in the player the same one the spinner is indexed by,
// which only ever counts up, but the type does not get to assume that. Go's % keeps the sign of
// its left operand, so a single one would hand a negative tick to the comparison against Travel
// and the shine would simply never come back for the rest of the process.
func TestAPhaseOutsideOnePeriodStillShines(t *testing.T) {
	base := Shimmer{Style: InputShine}
	for phase := -3*shinePeriod - 1; phase <= 3*shinePeriod; phase++ {
		s, wrapped := base, base
		s.Phase = phase
		wrapped.Phase = ((phase % shinePeriod) + shinePeriod) % shinePeriod
		from, to, ok := s.Band(72)
		wantFrom, wantTo, wantOK := wrapped.Band(72)
		if from != wantFrom || to != wantTo || ok != wantOK {
			t.Fatalf("phase %d lights %d..%d (%v), want what phase %d does: %d..%d (%v)",
				phase, from, to, ok, wrapped.Phase, wantFrom, wantTo, wantOK)
		}
	}
	// One negative phase that has to shine, spelled out, because the loop above would also pass
	// on an implementation that was dark for every phase below zero — it only compares the two.
	// -35 is 5 of 40, which is the tick the rest of this file and theme_test.go both stand on.
	s := base
	s.Phase = -shinePeriod + 5
	if _, _, ok := s.Band(72); !ok {
		t.Errorf("phase %d lights nothing, and phase 5 is the same tick", s.Phase)
	}
	// And the two ends of the int range, where a caller counting down or a counter that has
	// wrapped arrives: MaxInt is 7 of 40 and lit, MinInt is 32 and dark, and neither may panic
	// or come back with a band that is not inside the row. Adding Period to a MinInt remainder is
	// safe because the first modulo has already brought it inside (-Period, Period).
	for _, phase := range []int{math.MinInt, math.MinInt + 1, math.MaxInt - 1, math.MaxInt} {
		s.Phase = phase
		if from, to, ok := s.Band(72); ok && (from < 0 || to > 72 || from >= to) {
			t.Errorf("phase %d lights %d..%d", phase, from, to)
		}
	}
}

// TestSlowerLengthensTheRestAndNothingElse is the asymmetry the reader asked for: the input frame
// gets one pass in nine or ten seconds where the status verb keeps five, and it has to be the same
// light at two rates rather than a second animation. So the pass is compared tick for tick and not
// merely measured — a Slower that divided Travel as well would still cross in the same total time
// and would read as a different, slower glint, which is the thing this rules out.
func TestSlowerLengthensTheRestAndNothingElse(t *testing.T) {
	look := func(s Shimmer) string {
		return strings.Join(keysOf(t, s.Apply(row(72, "input.text"), 72)), "|")
	}
	base := Shimmer{Style: InputShine}
	fast, slow := base.norm(), base.Slower(2)
	switch {
	case slow.Travel != fast.Travel || slow.Width != fast.Width:
		t.Fatalf("Slower(2) is %d ticks over %d columns, want the %d over %d it was given",
			slow.Travel, slow.Width, fast.Travel, fast.Width)
	case slow.Period != 2*fast.Period:
		t.Fatalf("Slower(2) has a period of %d, want twice %d", slow.Period, fast.Period)
	}
	for phase := 0; phase < fast.Travel; phase++ {
		f, s := fast, slow
		f.Phase, s.Phase = phase, phase
		if got, want := look(s), look(f); got != want {
			t.Fatalf("at phase %d the slowed pass draws\n\t%s\nand the shipped one draws\n\t%s",
				phase, got, want)
		}
	}
	for phase := slow.Travel; phase < slow.Period; phase++ {
		s := slow
		s.Phase = phase
		if _, _, ok := s.reach(72); ok {
			t.Fatalf("the slowed shimmer is lit at phase %d of %d", phase, slow.Period)
		}
	}
	// n at or below one is identity on the value it was given, unnormalized. A zero field means
	// "whatever the shipped number is" everywhere else in this type, so a Slower(1) that filled
	// them in would freeze a value that was tracking the default.
	for _, n := range []int{-1, 0, 1} {
		if got := base.Slower(n); got != base {
			t.Errorf("Slower(%d) returned %+v, want the %+v it was given", n, got, base)
		}
	}
	// And a shimmer with no key stays off however slow it is asked to be, which is the guard on
	// norm: without it the zero value would come back carrying real numbers and reading as armed.
	var off Shimmer
	if got := off.Slower(4); got != off || got.On() {
		t.Errorf("Slower(4) turned the zero shimmer into %+v", got)
	}
}

// TestAnUnknownKeyIsOneHardEdgedBand is the seam kept open rather than an oversight. A program
// embedding this package can shine in a key of its own — one it declared and themed itself — and
// what it gets is a band with two edges, because there is no ladder to fall off along. The
// alternative would be a lookup failure drawn as nothing at all, which is the one outcome a
// caller cannot debug from a theme file.
func TestAnUnknownKeyIsOneHardEdgedBand(t *testing.T) {
	if _, ok := ramps["md.code"]; ok {
		t.Fatal("md.code has a ladder now: this test proves nothing")
	}
	s := Shimmer{Style: "md.code", Phase: 5}
	from, to, ok := s.Band(72)
	if !ok {
		t.Fatal("phase 5 of 40 lights nothing: the shipped constants moved")
	}
	got := s.steps(72)
	if len(got) != 1 || got[0] != (shineStep{from, to, "md.code"}) {
		t.Fatalf("an unladdered key draws %v, want the single run %d..%d", got, from, to)
	}
}

// TestEveryRungOfEveryLadderIsReachable is what theme_test.go's empty notYetDrawn map rests on at
// the far end: six shine keys are declared and documented as things a reader can retune, so every
// one of them has to be a key some frame actually draws. The widths are the ones the player uses —
// an input box on a normal window, and the seven letters of "working" — because a rung is only
// reachable at the width its band is allowed to be: this is exactly why the status ladder has two
// rungs and not four, and a fifth key added to either list fails here rather than in a theme.
func TestEveryRungOfEveryLadderIsReachable(t *testing.T) {
	for _, tc := range []struct {
		key string
		w   int
	}{
		{InputShine, 72},
		{StatusShine, len("working")},
	} {
		seen := map[string]bool{}
		for phase := 0; phase < shinePeriod; phase++ {
			s := Shimmer{Style: tc.key, Phase: phase}
			for _, st := range s.steps(tc.w) {
				seen[st.key] = true
			}
		}
		for _, k := range ramps[tc.key] {
			if !seen[k] {
				t.Errorf("%s is a rung of %s and no tick of a %d-column row draws it",
					k, tc.key, tc.w)
			}
			delete(seen, k)
		}
		if len(seen) > 0 {
			t.Errorf("%s at %d columns drew %v, which its ladder does not name",
				tc.key, tc.w, sortedSet(seen))
		}
	}
}

// TestApplyOnNothingIsNothing is the degenerate row, and it is not hypothetical: a resize arrives
// as a width before it arrives as a layout, and a fold can hand a widget an empty line. Apply runs
// last, after everything else has already agreed on the frame, so the only acceptable behaviour
// here is to hand back what it was given.
func TestApplyOnNothingIsNothing(t *testing.T) {
	s := Shimmer{Style: InputShine, Phase: 5}
	for _, l := range []Line{nil, {}, {{Text: "", Style: "input.text"}}} {
		if got := s.Apply(l, 72); got.Text() != "" || got.Width() != 0 {
			t.Errorf("a %d-span empty row became %q at %d columns", len(l), got.Text(), got.Width())
		}
	}
	// A row with no columns to cross is dark rather than divided into: Travel+1 is never zero, but
	// a negative width would put the band's right edge left of its left one and hand paint a range
	// that runs backwards.
	for _, w := range []int{-1, 0} {
		if from, to, ok := s.Band(w); ok {
			t.Errorf("a %d-column row lights %d..%d", w, from, to)
		}
		in := row(8, "input.text")
		if got := s.Apply(in, w); len(got) != len(in) || got[0] != in[0] {
			t.Errorf("width %d rewrote the row to %v", w, got)
		}
	}
}

// TestNoBandIsMoreThanHalfTheRow is the invariant that makes a shine a shine. Light crossing a
// row is only visible as movement if there is unlit row for it to move across: a band covering
// two thirds of the input box is the box brightening and going out again, which is a flash, and
// the reader asked for a reflection in as many words. So the width is capped at half the row no
// matter what was configured or computed, and the cap is a property of every width rather than
// of the ones a terminal happens to be.
//
// The narrow end is where a configured band meets it — sixteen columns of light on the seven
// letters of "working" would be the whole word — and the wide end is where the row's own
// arithmetic does. reach widens a band that would tear on a wide row, and at a Travel of one
// that asks for the entire width; the cap is written second so that it also holds the widening
// down, and this test is what says so. At a thousand columns and a one-tick pass the shipped
// code answers five hundred, dropping the cap answers a thousand, and re-applying the widening
// after it answers a thousand as well: three different numbers for two mistakes that look like
// reorderings of the same two lines.
//
// One column is the exception the arithmetic makes and it is the honest one: half of one column
// is nothing, so max(1, w/2) lights it. A single cell cannot show a band moving anyway.
func TestNoBandIsMoreThanHalfTheRow(t *testing.T) {
	// Travel 1 and 2 are below anything the player runs and are the two that make the widening
	// demand more than the whole row; 12 is the shipped pass and 40 is a slow one, where the
	// widening never fires and the cap is the only thing that can be wrong.
	for _, travel := range []int{1, 2, 3, 6, 12, 40} {
		for _, w := range []int{1, 2, 3, 7, 8, 40, 72, 200, 1000} {
			s := Shimmer{Style: InputShine, Travel: travel}
			half := max(1, w/2)
			for phase := 0; phase < s.norm().Period; phase++ {
				s.Phase = phase
				// Through reach, because reach is where the two clamps are and its answer is the
				// band's own extent: a cap undone by a later widening is invisible in a clipped
				// range on every width where the band happens to hang off an edge.
				if lo, hi, ok := s.reach(w); ok && hi-lo > half {
					t.Fatalf("travel %d width %d phase %d: the band is %d..%d, %d of %d columns",
						travel, w, phase, lo, hi, hi-lo, w)
				}
				// And through Band, which is what a caller outside this file can see. It cannot be
				// wider than reach, so this is the weaker of the two and it is here because it is
				// the one an embedding program is entitled to rely on.
				if from, to, ok := s.Band(w); ok && to-from > half {
					t.Fatalf("travel %d width %d phase %d: Band lights %d..%d of %d columns",
						travel, w, phase, from, to, w)
				}
			}
		}
	}
}
