package ui

// The output levels are one transcript drawn three ways, and the tests here hold each
// way to its promise: compact keeps the run's shape and nothing else, standard is byte
// for byte the drawing that existed before there were levels, and full draws what
// state kept. The renderer-side claims — the cache cannot serve one level's rows to
// another, and ItemSpans answers in both geometries — are here too, because they are
// the parts a keypress relies on and a transcript screenshot never shows.

import (
	"strings"
	"testing"
	"time"

	"arxi.local/sim/internal/state"
)

// The thought every level is tested against: long enough to tell apart from its
// summary, finished, and stamped so the standard level has something to say.
var thoughtItem = state.Item{
	Kind: state.KindThinking, ID: "think-1",
	Text:      "Refresh means the session cookie is re-read before the token is rotated.",
	StartedAt: 0, EndedAt: 1500 * time.Millisecond, Effort: "high",
}

// editItem is a tool call whose outcome has two shapes at once: a summary the
// standard level folds and the counts the compact one shows instead of the diff.
var editItem = state.Item{
	Kind: state.KindTool, ID: "tool-edit", Tool: "update",
	Args: map[string]any{"path": "cmd/arxi/log.go"}, Status: state.ToolOK,
	Summary: "Updated cmd/arxi/log.go",
	Diff: &state.Diff{
		Path: "cmd/arxi/log.go", Added: 27, Removed: 6,
		Hunks: []state.DiffHunk{{Rows: []state.DiffRow{
			{Op: state.DiffRemoved, Old: 10, Text: "import \"os\""},
			{Op: state.DiffAdded, New: 10, Text: "import \"log\""},
		}}},
	},
}

// bashItem is a call whose result is a wall of text, which is what the elision is for.
func bashItem() state.Item {
	return state.Item{
		Kind: state.KindTool, ID: "tool-bash", Tool: "bash",
		Args: map[string]any{"cmd": "go test ./..."}, Status: state.ToolOK,
		Summary: strings.TrimSuffix(strings.Repeat("ok  arxi.local/sim/internal/event  0.004s\n", 20), "\n"),
	}
}

// spansOf flattens a row's spans into "text|style" pairs, so an assertion can hold the
// colour to the word it colours and not just to the row that carries them both.
func spansOf(rows []Line) []string {
	var out []string
	for _, l := range rows {
		for _, sp := range l {
			out = append(out, sp.Text+"|"+sp.Style)
		}
	}
	return out
}

func hasSpan(rows []Line, text, style string) bool {
	for _, s := range spansOf(rows) {
		if s == text+"|"+style {
			return true
		}
	}
	return false
}

// TestCompactHidesAThoughtWhole covers both halves of the promise: a finished thought
// draws nothing at the compact level — not its text, not its summary — while an open
// one draws the one-line marquee, the same line the standard level draws, whose
// scrolling tail is the sign of life. Standard keeps the finished thought at the
// one-line summary, and full is the level the text was kept for.
func TestCompactHidesAThoughtWhole(t *testing.T) {
	g := DefaultGlyphs()
	if rows := (ItemBlock{It: thoughtItem, Detail: DetailCompact}).Render(60, g); len(rows) != 0 {
		t.Errorf("the compact level drew a finished thought: %s", plain(rows))
	}
	open := thoughtItem
	open.Open = true
	if rows := (ItemBlock{It: open, Detail: DetailCompact}).Render(60, g); len(rows) != 1 || !strings.HasPrefix(plain(rows), "Thinking · ") {
		t.Errorf("the compact level did not draw the open thought's marquee line: %s", plain(rows))
	}
	if rows := (ItemBlock{It: thoughtItem, Detail: DetailStandard}).Render(60, g); !strings.Contains(plain(rows), "Thought · a few seconds (high effort)") {
		t.Errorf("the standard level lost the finished thought's summary: %s", plain(rows))
	}
	if rows := (ItemBlock{It: thoughtItem, Detail: DetailFull}).Render(60, g); !strings.Contains(plain(rows), "re-read before") {
		t.Errorf("the full level never drew what state kept of the thought: %s", plain(rows))
	}
}

// TestCompactReducesAToolCallToItsOutcome walks the outcomes the level names. The
// counts come off the diff it is not drawing and take the diff's own colours; the two
// statuses that have no diff to count take a word; a success with nothing to count
// draws the call and stops, and so does one still running.
func TestCompactReducesAToolCallToItsOutcome(t *testing.T) {
	g := DefaultGlyphs()
	rows := ItemBlock{It: editItem, Detail: DetailCompact}.Render(80, g)
	if len(rows) != 1 {
		t.Fatalf("a compact edit drew %d rows: %s", len(rows), plain(rows))
	}
	got := plain(rows)
	for _, want := range []string{"Update(cmd/arxi/log.go)", "+27", "-6"} {
		if !strings.Contains(got, want) {
			t.Errorf("a compact edit never said %q: %s", want, got)
		}
	}
	for _, banned := range []string{"Updated cmd/arxi/log.go", "import"} {
		if strings.Contains(got, banned) {
			t.Errorf("a compact edit leaked the result it exists to hide (%q): %s", banned, got)
		}
	}
	// The wrapper keeps a leading space a span of its own, so the colours are held to
	// the word and not to the space before it.
	if !hasSpan(rows, "+27", "tool.count.add") || !hasSpan(rows, "-6", "tool.count.del") {
		t.Errorf("the counts did not arrive in their own colours: %v", spansOf(rows))
	}

	failed := editItem
	failed.ID = "tool-failed"
	failed.Status = state.ToolFailed
	failed.Summary = "--- FAIL: TestSince"
	rows = ItemBlock{It: failed, Detail: DetailCompact}.Render(80, g)
	if !strings.Contains(plain(rows), "Failed") || !hasSpan(rows, "Failed", "tool.status.error") {
		t.Errorf("a failed call is not named in red: %v", spansOf(rows))
	}
	if strings.Contains(plain(rows), "FAIL: TestSince") {
		t.Errorf("a compact failure leaked its result: %s", plain(rows))
	}

	denied := editItem
	denied.ID = "tool-denied"
	denied.Status = state.ToolDenied
	denied.Diff = nil
	denied.Summary = "denied by policy ask"
	rows = ItemBlock{It: denied, Detail: DetailCompact}.Render(80, g)
	if !hasSpan(rows, "Denied", "tool.status.denied") {
		t.Errorf("a refused call is not named: %v", spansOf(rows))
	}

	quiet := editItem
	quiet.ID = "tool-quiet"
	quiet.Diff = nil
	quiet.Summary = "read 214 lines"
	rows = ItemBlock{It: quiet, Detail: DetailCompact}.Render(80, g)
	if len(rows) != 1 || strings.Contains(plain(rows), "read 214 lines") {
		t.Errorf("a success with nothing to count did not stop at the call: %s", plain(rows))
	}

	pending := editItem
	pending.ID = "tool-pending"
	pending.Open = true
	pending.Status = state.ToolPending
	pending.Diff = nil
	pending.Summary = ""
	rows = ItemBlock{It: pending, Detail: DetailCompact}.Render(80, g)
	if len(rows) != 1 || !hasSpan(rows, "Update", "tool.name") {
		t.Errorf("a running call lost its name in the compact level: %v", spansOf(rows))
	}
}

// TestStandardIsTheUnsetLevel pins the back-compat contract the whole design rests
// on: a block nobody told about the levels — Detail's zero value, which is what
// BlockFor hands back — draws exactly what DetailStandard draws, so every caller
// that predates the levels keeps its look.
func TestStandardIsTheUnsetLevel(t *testing.T) {
	g := DefaultGlyphs()
	for _, it := range []state.Item{thoughtItem, editItem, bashItem()} {
		zero := ItemBlock{It: it}.Render(72, g)
		std := ItemBlock{It: it, Detail: DetailStandard}.Render(72, g)
		if plain(zero) != plain(std) {
			t.Errorf("%v: the unset level is not the standard one:\nzero:     %s\nstandard: %s", it.Kind, plain(zero), plain(std))
		}
	}
}

// TestFullDrawsWhatStateKept is the elision's exception: a result too long for the
// standard level is folded there, and drawn whole at full — the summary was never
// cut, only put away, which is the difference between a level and a loss.
func TestFullDrawsWhatStateKept(t *testing.T) {
	g := DefaultGlyphs()
	it := bashItem()
	std := plain(ItemBlock{It: it, Detail: DetailStandard}.Render(72, g))
	full := plain(ItemBlock{It: it, Detail: DetailFull}.Render(72, g))
	if !strings.Contains(std, "more lines") {
		t.Fatalf("the standard level stopped eliding a twenty-line result: %s", std)
	}
	if strings.Contains(full, "more lines") {
		t.Errorf("the full level kept the gap line: %s", full)
	}
	if n := strings.Count(full, "ok  arxi.local/sim/internal/event"); n != 20 {
		t.Errorf("the full level drew %d of the result's 20 lines", n)
	}
}

// TestCompactDegradesWithoutOverflow runs the compact drawing through the width
// sweep everything else in the transcript survives: a level whose one row cannot
// narrow is a level that breaks the frame's one invariant, which is that no row
// asks the terminal for a column it does not have.
func TestCompactDegradesWithoutOverflow(t *testing.T) {
	g := DefaultGlyphs()
	items := []state.Item{thoughtItem, editItem, bashItem()}
	for _, it := range items {
		for w := 8; w <= 80; w++ {
			rows := ItemBlock{It: it, Detail: DetailCompact}.Render(w, g)
			for _, l := range rows {
				if l.Width() > w {
					t.Errorf("%v at width %d: a compact row is %d columns: %q", it.Kind, w, l.Width(), l.Text())
					break
				}
			}
		}
	}
}

// TestTheBlockCacheCannotServeAnotherLevel is the regression the memo asked for:
// the cache is keyed on the item's id, and an id's rows are now one answer per
// level. Toggling has to re-draw, and toggling back has to find the old drawing
// still there and still right.
func TestTheBlockCacheCannotServeAnotherLevel(t *testing.T) {
	st := &state.State{Items: []state.Item{editItem, bashItem()}}
	r := NewRenderer()
	vp := Viewport{Width: 72, Height: 40}

	if f := r.Render(st, nil, vp).Plain(); !strings.Contains(f, "Updated cmd/arxi/log.go") || strings.Contains(f, "+27") {
		t.Fatalf("the first frame is not the standard level:\n%s", f)
	}
	r.Detail = DetailCompact
	if f := r.Render(st, nil, vp).Plain(); !strings.Contains(f, "+27") || strings.Contains(f, "Updated cmd/arxi/log.go") {
		t.Errorf("toggling to compact kept drawing the standard level:\n%s", f)
	}
	r.Detail = DetailStandard
	if f := r.Render(st, nil, vp).Plain(); !strings.Contains(f, "Updated cmd/arxi/log.go") || strings.Contains(f, "+27") {
		t.Errorf("toggling back to standard kept drawing the compact level:\n%s", f)
	}
}

// TestItemSpansAnswersBothGeometries is the anchor's source of truth: the same
// transcript measured at two levels gives two different shapes, and the span of the
// item a level hides has to come back as nothing rather than as the next item's
// rows, which is what keeps a collapsed view from pinning itself to a thought that
// is no longer there.
func TestItemSpansAnswersBothGeometries(t *testing.T) {
	st := &state.State{Items: []state.Item{
		{Kind: state.KindPrompt, ID: "p1", Text: "the login flow breaks on refresh"},
		thoughtItem,
		editItem,
		{Kind: state.KindText, ID: "p2", Text: "Tests pass with the calls swapped."},
	}}
	r := NewRenderer()
	vp := Viewport{Width: 72, Height: 40}
	starts, heights := r.ItemSpans(st, vp)
	for i := range st.Items {
		if starts[i] < 0 || heights[i] <= 0 {
			t.Fatalf("item %d drew nothing at the standard level (start %d, height %d)", i, starts[i], heights[i])
		}
	}
	// Ascending and non-overlapping, or the arithmetic an anchor runs on is a guess.
	for i := 1; i < len(starts); i++ {
		if starts[i] <= starts[i-1] {
			t.Fatalf("item %d starts at %d, at or before item %d at %d", i, starts[i], i-1, starts[i-1])
		}
		if want := starts[i-1] + heights[i-1] + 1; starts[i] != want {
			t.Fatalf("item %d starts at %d, want %d: the blank between items belongs to the layout", i, starts[i], want)
		}
	}
	r.Detail = DetailCompact
	starts, heights = r.ItemSpans(st, vp)
	if starts[1] != -1 || heights[1] != 0 {
		t.Errorf("a hidden thought is item 1 at (start %d, height %d), want (-1, 0)", starts[1], heights[1])
	}
	// A hidden item spends no rows, and not even the blank after it: the separator
	// belongs to whatever follows, and item 2 starts directly off item 0's span.
	if starts[2] != starts[0]+heights[0]+1 {
		t.Errorf("a hidden thought still spent the row between items: item 2 starts at %d, want %d", starts[2], starts[0]+heights[0]+1)
	}
}
