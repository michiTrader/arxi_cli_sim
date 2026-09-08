package ui

import (
	"strings"
	"testing"

	"arxi.local/sim/internal/state"
)

// The diff view's own tests. Three of its claims are not visible in a plain-text
// frame at all — the band runs past the end of the text, the code keeps its syntax
// colour on top of the band, the sign is brighter than the row it sits on — so they
// are checked against the spans rather than against the rendered string, which is
// the whole reason Span carries two style keys instead of one merged style.

// diffPlain renders one diff and returns its rows with the band's trailing blanks
// removed, which is what a reader sees on a terminal that cannot colour anything.
func diffPlain(d *state.Diff, width int) string {
	var out []string
	for _, l := range renderDiff(d, width, DefaultGlyphs()) {
		out = append(out, strings.TrimRight(l.Text(), " "))
	}
	return strings.Join(out, "\n")
}

// TestDiffLayout pins the geometry: margin, right-aligned number, sign, code. It is
// a block literal rather than four assertions because the columns only mean anything
// relative to each other, and a diff that has drifted half a cell is obvious here
// and invisible in a message about one line.
func TestDiffLayout(t *testing.T) {
	want := strings.TrimPrefix(`
    140       for _, l := range st.Locks {
    141 -         locks = append(locks, map[string]any{"key": l.Key})
    141 +         lk := map[string]any{"key": l.Key}
    142 +         // Omitted rather than "" when there is no expiry.
    143 +         lk["ttl"] = 300
      …
    150 +         locks = append(locks, lk)
    151       }`, "\n")
	if got := diffPlain(editWithDiff().Diff, 72); got != want {
		t.Errorf("diff at 72 columns:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestChangedRowsAreBandedToTheEdge is the claim a plain frame cannot carry. A band
// that stopped at the end of the text would draw the length of every line instead of
// the shape of the change, so a changed row is padded to the full width and the pad
// is ink. A context row has no band, so it is trimmed and must not be padded.
func TestChangedRowsAreBandedToTheEdge(t *testing.T) {
	const width = 72
	changed, context := 0, 0
	for _, l := range renderDiff(editWithDiff().Diff, width, DefaultGlyphs()) {
		banded := false
		for _, sp := range l {
			if sp.Fill != "" {
				banded = true
			}
		}
		if !banded {
			context++
			if strings.HasSuffix(l.Text(), " ") {
				t.Errorf("an unbanded row was padded: %q", l.Text())
			}
			continue
		}
		changed++
		if l.Width() != width {
			t.Errorf("a banded row is %d columns wide, want %d: %q", l.Width(), width, l.Text())
		}
		if endsInBareSpace(l) {
			t.Errorf("a banded row ends in an unstyled space: %q", l.Text())
		}
	}
	if changed == 0 || context == 0 {
		t.Fatalf("the fixture drew %d banded and %d unbanded rows, so this proves nothing", changed, context)
	}
}

// TestBandComposesWithSyntaxColour is why Span has two keys. The comment inside the
// added block has to come out green *and* on the band: one merged style could only
// have been one or the other, and the version of this renderer that resolved a single
// key punched a hole in the wash wherever the highlighter had an opinion.
func TestBandComposesWithSyntaxColour(t *testing.T) {
	th := DefaultTheme()
	found := false
	for _, l := range renderDiff(editWithDiff().Diff, 72, DefaultGlyphs()) {
		for _, sp := range l {
			if sp.Style != codeComment {
				continue
			}
			found = true
			st := th.Resolve(sp.Style).Over(th.Resolve(sp.Fill))
			if st.FG != th.Resolve(codeComment).FG {
				t.Errorf("the comment lost its own colour on a banded row")
			}
			if st.BG != th.Resolve("diff.added").BG {
				t.Errorf("the comment punched a hole in the band")
			}
		}
	}
	if !found {
		t.Fatal("no comment was highlighted inside the diff, so this proves nothing")
	}
}

// TestTheWashCarriesNoForeground is the invariant that makes two keys enough. A band
// is a background and nothing else: the moment diff.added also set a colour, Over
// would hand it to every span the highlighter left unclassified, and the row would
// change colour rather than sit on one. The sign is the exception the layout wants —
// it is the one glyph that has to be read at a glance, so it is brighter than its row.
func TestTheWashCarriesNoForeground(t *testing.T) {
	th := DefaultTheme()
	for _, key := range []string{"diff.added", "diff.removed"} {
		st := th.Resolve(key)
		if st.BG.Kind == ColorNone {
			t.Errorf("%s has no background, so it cannot be a band", key)
		}
		if st.FG.Kind != ColorNone {
			t.Errorf("%s sets a foreground, which would repaint unclassified code", key)
		}
		if sign := th.Resolve(key + ".sign"); sign.FG.Kind == ColorNone || !sign.Has(AttrBold) {
			t.Errorf("%s.sign is not brighter than the row it sits on", key)
		}
	}
}

// TestTheBandsAreAGreenAndARedOfEqualWeight holds the four corrections a reader has made to
// this theme, as claims about the colour rather than about the hex it happens to be written as.
//
// Green, because that is what a diff is read as everywhere else. The added band was a dark
// blue for a while, on the argument that a green wash sits under a syntax highlighter's own
// greens, and the reader who has to read it asked for the green back and called the blue too
// strong. So this asserts dominance and not a value: however the channels are re-tuned, the
// added band has to be greener than it is anything else, which is the one thing a blue cannot
// be.
//
// No louder than the red it is compared against, which is the second correction — the first
// pair of bands won against the code lying on them. That is the ceiling, and it is there for a
// reason a diff cannot do without: a band is a background for somebody else's text, and a wash
// the foreground cannot be read on is not a band, it is a highlight.
//
// And not black, which is the third. Answering the ceiling by driving both bands down to a
// twelfth of full brightness produced a wash that on a dark terminal cannot be told from no
// wash at all, and the reader rejected it in exactly those terms: green or blue, and neither
// very dark nor very light. So the weight has a floor as well, and the two together are the
// whole claim — a middle band, seen as a colour, with legible code on it.
//
// And red rather than pink, which is the fourth. A band can be dominant in red and still be
// the wrong red: what the reader called pink was #7f2830, whose blue outran its green, and past
// red more blue than green is the road to magenta. So the removed band is held to a direction
// as well as to a dominance, and that is a claim about hue that no re-tune of the weight can
// quietly break.
func TestTheBandsAreAGreenAndARedOfEqualWeight(t *testing.T) {
	// The window, on the same 0-255 scale as bandWeight. The floor is above the band that was
	// rejected as black (26) and above the neutral wash under the reader's own turns (44), which
	// is deliberately quieter than a diff. The ceiling is where a #cccccc foreground stops
	// clearing 4.5:1 on a green of that weight, green being the worst case because it carries
	// most of both the luma and the luminance.
	const (
		floor   = 48
		ceiling = 80
		tol     = 4 // a shade, so "matched" is a claim and not a coincidence of rounding
	)
	th := DefaultTheme()
	added, removed := th.Resolve("diff.added").BG, th.Resolve("diff.removed").BG
	for key, c := range map[string]Color{"diff.added": added, "diff.removed": removed} {
		if c.Kind != ColorRGB {
			t.Fatalf("%s is not an RGB colour: no indexed colour is both a colour and a surface to put text on", key)
		}
		w := bandWeight(c)
		if w > ceiling {
			t.Errorf("%s weighs %d of 255, over %d: a band the terminal's own foreground cannot be read on is a highlight", key, w, ceiling)
		}
		if w < floor {
			t.Errorf("%s weighs %d of 255, under %d: a band that cannot be told from the terminal's background is not a band, and black was rejected", key, w, floor)
		}
	}
	if added.G <= added.R || added.G <= added.B {
		t.Errorf("the added band is rgb(%d,%d,%d), which is not a green: the blue was rejected by the reader who has to read it", added.R, added.G, added.B)
	}
	if removed.R <= removed.G || removed.R <= removed.B {
		t.Errorf("the removed band is rgb(%d,%d,%d), which is not a red", removed.R, removed.G, removed.B)
	}
	// Which way the red leans is the difference between the two words the reader used for it. A
	// red may lean towards orange; leaning the other way is how it stops being read as red.
	if removed.B > removed.G {
		t.Errorf("the removed band is rgb(%d,%d,%d): more blue than green tilts a red towards magenta, and the reader called that pink", removed.R, removed.G, removed.B)
	}
	// Matched in both directions, because "neither side of a change shouts over the other" is one
	// decision about a pair and not a ranking of the two.
	if a, r := bandWeight(added), bandWeight(removed); a-r > tol || r-a > tol {
		t.Errorf("the added band weighs %d against the removed band's %d, more than %d apart: neither side of a change gets to shout over the other", a, r, tol)
	}
	// And the sign is the whole fallback for a reader who cannot separate red from green, so the
	// two are not allowed to be the same colour.
	if a, r := th.Resolve("diff.added.sign").FG, th.Resolve("diff.removed.sign").FG; a == r {
		t.Error("both gutter signs are the same colour, and the sign is what tells the sides apart when the hue does not")
	}
}

// bandWeight is Rec. 601 luma in the same 0-255 range as one channel: green counts most and
// blue least, which is how an eye counts them. Integer arithmetic on purpose — nothing here
// needs a tenth of a shade, and a float would invite a tolerance nobody can justify.
func bandWeight(c Color) int {
	return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
}

// TestGapIsDrawnBetweenHunksOnly pins the marker's count and its place. Once per
// join, never leading or trailing: a gap before the first hunk claims the file starts
// somewhere it does not, and the number field is where it belongs because that is
// where the reader is already looking for the numbering to skip.
func TestGapIsDrawnBetweenHunksOnly(t *testing.T) {
	d := editWithDiff().Diff
	g := DefaultGlyphs()
	rows := renderDiff(d, 72, g)
	marker, gaps := g.Get("diff.gap"), 0
	for i, l := range rows {
		if !strings.Contains(l.Text(), marker) {
			continue
		}
		gaps++
		if i == 0 || i == len(rows)-1 {
			t.Errorf("the gap marker is row %d of %d", i, len(rows))
		}
		if l.Width() >= 72 {
			t.Errorf("the gap row was banded to the edge: %q", l.Text())
		}
	}
	if want := len(d.Hunks) - 1; gaps != want {
		t.Errorf("drew %d gap markers for %d hunks, want %d", gaps, len(d.Hunks), want)
	}
}

// diffFields picks the number and the sign out of a rendered row. A row's prefix is
// fixed by construction — margin, number, blank, sign, blank — so reading it by
// position is not fragility, it is the layout this test means to pin.
func diffFields(t *testing.T, l Line) (num, sign string) {
	t.Helper()
	if len(l) < 5 {
		t.Fatalf("row has %d spans, too few to carry a gutter: %q", len(l), l.Text())
	}
	return l[1].Text, l[3].Text
}

// TestAWrappedRowRepeatsItsSignAndBlanksItsNumber is what keeps a long insertion
// readable. The second physical row is the same line of the file: a number there
// would claim a line that does not exist, and a sign that stopped would leave the
// tail of an insertion looking like context.
func TestAWrappedRowRepeatsItsSignAndBlanksItsNumber(t *testing.T) {
	d := &state.Diff{
		Path: "cmd/arxi/runshow.go", Lang: "go", Added: 1,
		Hunks: []state.DiffHunk{{Rows: []state.DiffRow{{
			Op: state.DiffAdded, New: 141,
			Text: "\t\tlk := map[string]any{\"key\": l.Key, \"holder\": l.Holder, \"ttl\": l.TTL}",
		}}}},
	}
	rows := renderDiff(d, 40, DefaultGlyphs())
	if len(rows) < 2 {
		t.Fatalf("the row did not wrap at 40 columns, so this proves nothing")
	}
	for i, l := range rows {
		num, sign := diffFields(t, l)
		if sign != "+" {
			t.Errorf("physical row %d lost its sign: %q", i, sign)
		}
		want := "   "
		if i == 0 {
			want = "141"
		}
		if num != want {
			t.Errorf("physical row %d has the number %q, want %q", i, num, want)
		}
	}
}

// TestNarrowWidthsDropTheGutterThenTheDiff is the ladder that keeps the 20-column
// sweep legal, and it is the arithmetic the whole corpus depends on: for this fixture
// the numbers survive to 18 columns, the gutter goes at 17, and below 14 there is no
// code column left worth drawing. Nothing is the honest answer there — the summary
// above has already said what the edit did, so a phone held in portrait keeps the
// sentence and loses the detail instead of being handed six columns and a mess.
func TestNarrowWidthsDropTheGutterThenTheDiff(t *testing.T) {
	d, g := editWithDiff().Diff, DefaultGlyphs()
	for _, w := range []int{13, 14, 17, 18, 20, 24, 40} {
		rows, text := renderDiff(d, w, g), ""
		for _, l := range rows {
			if l.Width() > w {
				t.Errorf("width %d: a row is %d columns wide: %q", w, l.Width(), l.Text())
			}
			text += l.Text()
		}
		switch {
		case w < 14:
			if rows != nil {
				t.Errorf("width %d: drew %d rows, want nothing at all", w, len(rows))
			}
		case len(rows) == 0:
			t.Errorf("width %d: drew nothing, but there is room for a code column", w)
		case w >= 18 && !strings.Contains(text, "140"):
			t.Errorf("width %d: the numbering was dropped early", w)
		case w < 18 && strings.Contains(text, "140"):
			t.Errorf("width %d: the numbering survived past the width that fits it", w)
		}
	}
}
