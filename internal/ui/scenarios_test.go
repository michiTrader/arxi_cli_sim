package ui

import (
	"path/filepath"
	"strings"
	"testing"
)

// The scenario directory is the case catalogue, so it is also the test corpus: a
// file dropped into it becomes coverage the moment it lands, at every width a
// phone or a split pane might hand us. Without this the sweep only ever saw the
// first conversation, and every new case was a case nobody checked.

func scenarioFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("../../testdata/scenarios/*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no scenarios found, so the sweep below proves nothing")
	}
	return paths
}

// TestEveryScenarioFitsEveryWidth is TestFrameNeverOverflows and
// TestNoTranscriptLineEndsInSpace applied to the whole corpus, plus the one rule
// neither of them can state: a line holds no control characters. All three failures
// are the same failure seen three ways — a row we thought we owned is not one row —
// and the third is the one that hid the longest, because a newline is zero columns
// wide and so is invisible to a width check and to a golden file alike.
//
// Rendered as a document, because a frame with a height holds a screenful and the rest of
// the conversation is above it, unexamined: a row that overflows two hundred rows up
// corrupts the terminal exactly as thoroughly as one in the tail, and a sweep with a height
// was quietly not looking at it.
func TestEveryScenarioFitsEveryWidth(t *testing.T) {
	widths := []int{20, 24, 40, 60, 72, 100, 120}
	for _, path := range scenarioFiles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			_, sc := play(t, path, 0)
			for _, w := range widths {
				for n := 0; n <= len(sc.Steps); n++ {
					st, _ := play(t, path, n)
					r := NewRenderer()
					f := render(r, st, NewInput(), docViewport(w))
					if bad := f.Overflow(); len(bad) > 0 {
						t.Fatalf("width %d after %d steps: lines %v overflow:\n%s", w, n, bad, f.Plain())
					}
					if f.Width != w {
						t.Fatalf("width %d: frame reports %d", w, f.Width)
					}
					for _, l := range append(append([]Line{}, f.Committed...), f.Live...) {
						if endsInBareSpace(l) {
							t.Fatalf("width %d after %d steps: line ends in a space: %q", w, n, l.Text())
						}
						if i := strings.IndexFunc(l.Text(), isControl); i >= 0 {
							t.Fatalf("width %d after %d steps: line carries a control character at byte %d: %q", w, n, i, l.Text())
						}
					}
				}
			}
		})
	}
}

// TestNoScenarioHandsOverMidSession is TestNothingIsHandedOverMidSession applied to the
// whole corpus, and the corpus is where the old version of it earned its keep: a run with
// four members seals its items out of order, and a renderer that commits as blocks seal
// would hand over a later one while an earlier one was still open. Under the rule as it now
// stands there is no ordering left to get wrong, because mid-session nothing is handed over
// at all — see TestNothingIsHandedOverMidSession for why the promise is no longer made, and
// for why the height is an equality rather than a cap.
//
// The screen is short and the input swings between one row and five, because at a height the
// scenario fits on there is no tail to trim, and trimming is the half of the rule a corpus
// can actually get wrong: padding is one loop with no content in it, while a trim has to pick
// which rows survive.
func TestNoScenarioHandsOverMidSession(t *testing.T) {
	trimmed, padded := 0, 0
	for _, path := range scenarioFiles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			_, sc := play(t, path, 0)
			r := NewRenderer()
			for n := 0; n <= len(sc.Steps); n++ {
				st, _ := play(t, path, n)
				in := NewInput()
				if n%4 == 0 {
					in.SetText(strings.Repeat("a prompt long enough to wrap on a narrow terminal, twice over ", 5))
				}
				vp := shortViewport(72)
				f := render(r, st, in, vp)
				if len(f.Committed) != 0 {
					t.Fatalf("after %d steps a frame with a height handed over %d rows, and a handed-over row is one a drag reflows out of reach", n, len(f.Committed))
				}
				if len(f.Live) != vp.Height {
					t.Fatalf("after %d steps the frame is %d rows on a %d-row screen: short of it leaves rows no repaint reaches, past it scrolls its own top rows into history", n, len(f.Live), vp.Height)
				}
				if f.Cursor.Line < 0 || f.Cursor.Line >= len(f.Live) {
					t.Fatalf("after %d steps the cursor is on row %d of a %d-row region", n, f.Cursor.Line, len(f.Live))
				}
				// The same document comparison as TestNothingIsHandedOverMidSession: at no
				// height the transcript is all committed and the chrome is all that is live,
				// so the two together are how tall this state wanted to be.
				doc := render(r, st, in, docViewport(vp.Width))
				switch total := len(doc.Committed) + len(doc.Live); {
				case total > vp.Height:
					trimmed++
				case total < vp.Height:
					padded++
				}
			}
		})
	}
	// Per file this would be too strong — a two-step scenario is allowed to be shorter than
	// the screen from beginning to end — but if no frame in the whole corpus ever outgrew it
	// then the trim above was never once tested. Subtests without t.Parallel run to completion
	// before t.Run returns, so both counts are already final here.
	if trimmed == 0 {
		t.Fatal("no scenario ever outgrew the screen, so the trim above proves nothing")
	}
	if padded == 0 {
		t.Fatal("every scenario outgrew the screen from its first frame, so the padding above proves nothing")
	}
}
