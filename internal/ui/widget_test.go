package ui

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/state"
)

// The chrome is the part of the interface no other test in this package can see. It
// never reaches scrollback, it has no golden file, and both corpus sweeps fold their
// scenarios at Height 0 — where the transcript is the document and the status row is
// one line of footer. So the rules the transcript is held to have to be restated here
// for the two widgets that own a rectangle: every row fits its width, no row ends in a
// bare space, and the one row carrying a cursor puts it on the character the human is
// editing rather than a column away from it.

// boxWidths spans both sides of the width where the border stops being worth its four
// columns: 6 is exactly the marker plus the box with nothing left over, 72 is the
// golden's width, and 120 is a wide pane. Above the threshold they come in pairs, because
// a two-column border glyph divides an even interior and not an odd one — a run built by
// repetition is right at every even width and one column short at every odd one, and a
// list of round numbers is a list of even ones.
var boxWidths = []int{1, 2, 3, 5, 6, 7, 8, 9, 12, 13, 20, 21, 24, 25, 40, 41, 60, 72, 73, 100, 120}

// boxTexts are the lines that break a box: nothing typed at all, a line that fills the
// room exactly, one that wraps several times, and one made of two-column runes, which
// is where a border built by repetition rather than by measurement goes wrong.
//
// Then every one of those questions again with the break the editor now holds, because a
// row that ends without being full is a different row: two lines, an empty line between
// two, a break and nothing else, three of them in a row, a break at the very end, a break
// between two-column runes, and a break inside text long enough that both of its halves
// wrap as well. The last one is the only case where "a row per line" and "a row per wrap"
// are both in play, and confusing the two is what puts the cursor a row above the
// character somebody is editing.
var boxTexts = []string{
	"",
	"go",
	"go test ./internal/...",
	"日本語のテキストを入力する",
	"a prompt long enough to wrap on a narrow terminal, twice over and then some more",
	"go\ntest",
	"a\n\nb",
	"\n",
	"\n\n\n",
	"go\n",
	"日本\n語のテキスト",
	"a prompt long enough to wrap on a narrow terminal\nand a second line that wraps as well",
}

// TestInputBoxIsExactlyTheWidth is the box's whole contract. A row one column short
// leaves the closing border off the last cell, and a row one column long makes the
// terminal wrap a row the frame believes it owns — after which every cursor position
// below it is wrong. Both are invisible in a screenshot and obvious in a measurement.
func TestInputBoxIsExactlyTheWidth(t *testing.T) {
	g := DefaultGlyphs()
	for _, text := range boxTexts {
		for _, w := range boxWidths {
			in := NewInput()
			in.SetText(text)
			rows, cur := in.Render(w, g)
			checkBox(t, fmt.Sprintf("width %d %q", w, text), rows, cur, w, g)
		}
	}
}

// checkBox holds one render to every rule a drawn box has, and holds an undrawn one to
// the only rules that still apply. It is a helper rather than a loop body because the
// glyph-override test has to be held to exactly the same contract as the default one:
// two copies of these assertions would drift, and the copy that drifted would be the one
// covering the overridden glyphs nobody looks at.
//
// The no-trailing-space rule is a border rule and stops at the border. An unboxed row ends
// in the human's own character, and when they type a space at a wrap boundary that
// character is a space — trimming it would move every column after it, so the bare marker
// row is held to its width, and to the one thing a row with no border to its right cannot
// do: put the cursor in a column the terminal does not have.
func checkBox(t *testing.T, label string, rows []Line, cur Cursor, w int, g Glyphs) {
	t.Helper()
	if !boxedRows(rows, g) {
		for i, l := range rows {
			if l.Width() > w {
				t.Fatalf("%s: unboxed row %d is %d columns: %q", label, i, l.Width(), l.Text())
			}
		}
		// The marker row spends every column it has on text, so the column just past the last
		// character of a full row is off the right edge — and a cursor on a break is the one
		// thing that reaches it. A terminal asked for a column it does not own puts the cursor
		// wherever it likes, usually the first cell of the row below, which is a different
		// character. Boxed, the border absorbs that column; here nothing does, so the answer
		// has to be clamped and this is what says it was.
		if !cur.Hidden && cur.Col >= w {
			t.Errorf("%s: unboxed cursor at column %d of %d columns", label, cur.Col, w)
		}
		return
	}
	if len(rows) < 3 {
		t.Fatalf("%s: a box needs two rules and a row, got %d", label, len(rows))
	}
	for i, l := range rows {
		if l.Width() != w {
			t.Errorf("%s: row %d is %d columns: %q", label, i, l.Width(), l.Text())
		}
		if endsInBareSpace(l) {
			t.Errorf("%s: row %d ends in a space: %q", label, i, l.Text())
		}
	}
	if cur.Hidden {
		t.Errorf("%s: the cursor is hidden inside a drawn box", label)
	}
	// Never on either rule: the top one is the title's, and a cursor on the bottom one is a
	// character the human cannot see themselves typing.
	if cur.Line < 1 || cur.Line > len(rows)-2 {
		t.Errorf("%s: cursor on row %d of %d rows", label, cur.Line, len(rows))
	}
}

// TestInputCursorSitsOnTheCharacter is why the input reports a cursor at all. A real
// terminal cursor is the one part of this interface we do not draw, so the only way it
// can be wrong is by being at the wrong coordinates — and a cursor a column off is the
// most obvious tell that a prompt is a simulation. Every position of every line is
// checked against the cell the box actually put there.
func TestInputCursorSitsOnTheCharacter(t *testing.T) {
	g := DefaultGlyphs()
	for _, text := range boxTexts[1:] {
		runes := []rune(text)
		// 3, 5 and 6 are below the box's threshold, which is the mode with no spare column to
		// the right of the text. 7 is one column below the threshold and 8 is the narrowest box
		// there is: at 8 the interior is two columns, which is one wide grapheme, and every
		// off-by-one in the wrap arithmetic shows up there or nowhere. 9 is the same box with
		// an odd interior, where a two-column rune cannot divide the room evenly.
		for _, w := range []int{3, 5, 6, 7, 8, 9, 12, 20, 40, 72} {
			for i := 0; i <= len(runes); i++ {
				in := NewInput()
				in.SetText(text)
				in.cursor = i
				rows, cur := in.Render(w, g)
				if cur.Hidden {
					continue
				}
				if !boxedRows(rows, g) {
					// Unboxed the answer for a cursor on a break is clamped, because the column it
					// wants is off the edge of the terminal. A clamped column is deliberately not
					// the one the cell test below would ask for — it is one short of the truth in
					// the mode that is already a border short of the interface — so the bound is
					// the whole assertion here.
					if cur.Col >= w {
						t.Fatalf("width %d %q cursor %d: unboxed cursor at column %d of %d\n%s",
							w, text, i, cur.Col, w, plain(rows))
					}
					continue
				}
				got := cellAt(rows[cur.Line], cur.Col)
				// A break is not drawn anywhere, so a cursor on one has no cell of its own: it
				// sits at the end of the row the break closes, which inside a box is a padding
				// cell. That is the honest place for it, because what gets typed there joins the
				// row above the break and not the row below, and it is the one position in this
				// sweep that is a blank on purpose rather than by running out of text.
				want := " "
				if i < len(runes) && runes[i] != '\n' {
					want = string(runes[i])
				}
				if got != want {
					t.Fatalf("width %d %q cursor %d: at row %d col %d the box has %q, want %q\n%s",
						w, text, i, cur.Line, cur.Col, got, want, plain(rows))
				}
			}
		}
	}
}

// boxedRows reports whether Render drew a border, by looking for the corner it would
// have started with. Asked rather than recomputed on purpose: a test that derived the
// threshold from the same arithmetic Render uses would agree with a broken Render.
func boxedRows(rows []Line, g Glyphs) bool {
	return len(rows) > 0 && strings.HasPrefix(rows[0].Text(), g.Get("frame.tl"))
}

// TestBoxKeepsEveryRuneOfTheLine is the threshold's reason for existing, and it is a
// property rather than a width: whatever the box is, the text inside it is all of the
// text. HardWrap cuts a row to the room it is given and cannot split a grapheme, so an
// interior one column wide has no way to place a two-column character — it places none of
// the line, and the human types into a box that stays blank. That is why the border needs
// two columns to survive rather than one, and this is the assertion that keeps it: below
// the threshold the box is gone and the same text wraps into the four columns the border
// was spending, which is worse-looking and not lossy.
//
// A break is the one rune the box keeps by not drawing it: it is a row boundary and has no
// cell, so it cannot appear in the text read back out of the spans. Dropping it from the
// comparison is what makes this test about the characters and nothing else, and
// TestBoxGrowsARowPerLine is what proves the break was honoured rather than eaten.
func TestBoxKeepsEveryRuneOfTheLine(t *testing.T) {
	g := DefaultGlyphs()
	for _, text := range boxTexts[1:] {
		for _, w := range boxWidths {
			in := NewInput()
			in.SetText(text)
			rows, _ := in.Render(w, g)
			if !boxedRows(rows, g) {
				continue
			}
			var b strings.Builder
			for _, l := range rows {
				for _, sp := range l {
					if sp.Style == "input.text" {
						b.WriteString(sp.Text)
					}
				}
			}
			if got, want := b.String(), strings.ReplaceAll(text, "\n", ""); got != want {
				t.Errorf("width %d: the box holds %q, want %q\n%s", w, got, want, plain(rows))
			}
		}
	}
}

// TestBoxGrowsARowPerLine is the second half of the multi-line editor, and it is the half
// the wrap tests cannot see: they hold every row to the width and the cursor to its
// character, and a Render that dropped a break entirely would satisfy both by drawing one
// long wrapped row. So the rows are read back as text and rejoined with the breaks they
// came from — a break the layout ate, or a row it invented, is a difference in this string.
//
// The row count is asserted as well as the joined text, and it is not redundant: a layout
// that split on some other byte puts the whole text on one row with the break still in it,
// and rejoining one row is the text again. The count is what says how many rows there were.
//
// The width is wide and the texts are short so that no segment wraps: with wrapping in play
// a row is not a line, and this count would be a count of both. A break inside text long
// enough to wrap is covered by the sweeps, which run boxTexts at every width there is.
func TestBoxGrowsARowPerLine(t *testing.T) {
	g := DefaultGlyphs()
	for _, text := range []string{"", "go", "go\ntest", "a\n\nb", "\n", "\n\n\n", "go\n", "日本\n語"} {
		in := NewInput()
		in.SetText(text)
		rows, _ := in.Render(72, g)
		if !boxedRows(rows, g) {
			t.Fatalf("%q: no box at 72 columns", text)
		}
		got := make([]string, 0, len(rows)-2)
		for _, l := range rows[1 : len(rows)-1] {
			var b strings.Builder
			for _, sp := range l {
				if sp.Style == "input.text" {
					b.WriteString(sp.Text)
				}
			}
			got = append(got, b.String())
		}
		if want := strings.Count(text, "\n") + 1; len(got) != want {
			t.Errorf("%q is %d rows in the box, want %d: one per line", text, len(got), want)
		}
		if joined := strings.Join(got, "\n"); joined != text {
			t.Errorf("the box holds %q, want %q\n%s", joined, text, plain(rows))
		}
	}
}

// TestBoxTitleIsWholeOrAbsent is the rule for a word let into a border. Half a title reads
// as a rendering fault rather than as a narrow terminal, and a title is the one thing in
// the frame a reader could mistake for content — so it is drawn with a space on each side
// or not drawn at all. Whether one was drawn is decided by looking for a letter, because
// the frame has none: any letter in the top rule is title, and a rule holding some of them
// but not the whole word is the failure this exists to catch. The title's own runes cannot
// be the test — every title here contains a space, and so does a rule whose glyph does not
// divide its run.
//
// A drawn title also keeps a whole horizontal on each side of it. That is what makes it a
// word let into a rule rather than a label leaning on the corner, and it is why the fit
// test asks for two horizontals more than the title needs. With a double-width glyph a
// single column is not enough to draw one in, so the lead-in is as wide as the glyph and
// the rule is checked under an override that proves it.
func TestBoxTitleIsWholeOrAbsent(t *testing.T) {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	for _, ov := range []map[string]string{nil, {"frame.h": "一"}, {"frame.h": "──"}} {
		g, err := NewGlyphs(ov, false)
		if err != nil {
			t.Fatal(err)
		}
		h := g.Get("frame.h")
		for _, title := range []string{"prompt", "a much longer title than any border needs"} {
			for _, w := range boxWidths {
				in := NewInput()
				in.Title = title
				rows, _ := in.Render(w, g)
				if !boxedRows(rows, g) {
					continue
				}
				top := rows[0].Text()
				switch {
				case !strings.ContainsAny(top, letters):
				case !strings.Contains(top, h+" "+title+" "+h):
					t.Errorf("%v width %d: the rule %q does not hold %q whole and let in", ov, w, top, title)
				}
				if rows[0].Width() != w {
					t.Errorf("%v width %d: the rule is %d columns: %q", ov, w, rows[0].Width(), top)
				}
			}
		}
	}
}

// TestBoxSurvivesAWideBorderGlyph is the one case a border built by repetition gets wrong.
// A double-width horizontal repeated to an odd number of columns comes back one column
// short, and the fix — make the remainder up in spaces — is invisible at every even width
// and load-bearing at every odd one, which is why the widths above the threshold come in
// pairs. A double-width vertical moves the interior and the threshold as well, and
// double-width corners move where the title can start, so all of them are overridden here
// and every render is held to the same contract as the default glyphs.
//
// The first set is the trap: "──" looks wide and is not. Both of its runes are one column,
// so the string divides every width exactly and every off-by-one this test exists for is
// unreachable through it. Only a genuinely East-Asian-Wide grapheme — one rune, two
// columns, indivisible — reaches them.
func TestBoxSurvivesAWideBorderGlyph(t *testing.T) {
	for _, ov := range []map[string]string{
		{"frame.h": "──"},
		{"frame.h": "一"},
		{"frame.v": "｜"},
		{"frame.h": "一", "frame.v": "｜", "frame.tl": "一", "frame.tr": "｜"},
	} {
		g, err := NewGlyphs(ov, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range boxTexts {
			for _, w := range boxWidths {
				in := NewInput()
				in.SetText(text)
				rows, cur := in.Render(w, g)
				checkBox(t, fmt.Sprintf("%v width %d %q", ov, w, text), rows, cur, w, g)
			}
		}
	}
}

// TestTheBorderHasNoTitleByDefault is the default stated as a test, because it is the
// kind of default a later edit restores by accident. The marker inside the box already
// says a prompt goes there, so the word "prompt" in the border was the frame describing
// itself; an unbroken rule is what a box with nothing to declare looks like. The field
// stays, and TestBoxTitleIsWholeOrAbsent sets it — what is asserted here is only that
// nothing is written into the rule unless a caller asks for it.
func TestTheBorderHasNoTitleByDefault(t *testing.T) {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	g := DefaultGlyphs()
	for _, w := range boxWidths {
		rows, _ := NewInput().Render(w, g)
		if !boxedRows(rows, g) {
			continue
		}
		if top := rows[0].Text(); strings.ContainsAny(top, letters) {
			t.Errorf("width %d: the default border carries a title: %q", w, top)
		}
	}
}

// cellAt returns the cell starting at column col, which is what a terminal would put
// the cursor on. It walks by width rather than by rune because a two-column character
// occupies a column the cursor can never be at, and a rune index would call that
// column the next character.
func cellAt(l Line, col int) string {
	c := 0
	for _, r := range l.Text() {
		w := ansi.StringWidth(string(r))
		if c == col {
			return string(r)
		}
		if c > col {
			break
		}
		c += w
	}
	return " " // past the last cell of the row, which is where a new character goes
}

func plain(rows []Line) string {
	var b strings.Builder
	for _, l := range rows {
		b.WriteString(l.Text())
		b.WriteByte('\n')
	}
	return b.String()
}

// statusStates are the rungs of the verb ladder plus the fields that make the row wide.
// The blocked one carries a finished flag too, because that combination is the one the
// ladder deliberately answers with the run's state rather than the player's.
func statusStates() map[string]*state.State {
	full := func() *state.State {
		return &state.State{
			Actor: "builder", Model: "claude-sonnet-4", Turn: 3,
			SpentUSD: 0.08, BudgetUSD: 0.5, TokensIn: 12400, TokensOut: 3100,
		}
	}
	working, waiting, blocked, done, idle := full(), full(), full(), full(), full()
	working.Active = true
	waiting.Active, waiting.Quiescent = true, "approval"
	blocked.Active, blocked.Finished = true, true
	blocked.Blocked = &state.Blocked{Agent: "builder", On: "budget_raise"}
	done.Finished = true
	return map[string]*state.State{
		"working": working, "waiting": waiting, "blocked": blocked,
		"done": done, "idle": idle, "bare": state.New(),
	}
}

func TestTasksWidgetSummarizesLiveCounts(t *testing.T) {
	st := &state.State{Tasks: []*state.Task{
		{Status: event.TaskCompleted}, {Status: event.TaskCompleted}, {Status: event.TaskActive},
		{Status: event.TaskPending}, {Status: event.TaskPending},
	}}
	wd := TasksWidget{St: st}
	rows := wd.Render(80, 0, DefaultGlyphs())
	if len(rows) != 1 || rows[0].Text() != "Tasks 2/5 · 1 active · 2 pending · /tasks" {
		t.Fatalf("wide Tasks summary = %q", plain(rows))
	}
	st.Tasks[2].Status = event.TaskCompleted
	if got := wd.Render(80, 0, DefaultGlyphs())[0].Text(); got != "Tasks 3/5 · 0 active · 2 pending · /tasks" {
		t.Fatalf("live Tasks summary = %q", got)
	}
	if wd.Name() != "tasks" || wd.Slot() != SlotAboveInput || wd.Fallback() != SlotBelowInput || wd.Animated() {
		t.Fatalf("Tasks widget contract = name %q slot %q fallback %q animated %v", wd.Name(), wd.Slot(), wd.Fallback(), wd.Animated())
	}
	if s, ok := Place(wd, Viewport{Width: 80, Height: 20, FixedTop: true}); !ok || s != SlotAboveInput {
		t.Fatalf("Tasks preferred placement = %q, %v", s, ok)
	}
}

func TestTasksWidgetDegradesWithoutOverflow(t *testing.T) {
	st := &state.State{Tasks: []*state.Task{{Status: event.TaskCompleted}, {Status: event.TaskActive}, {Status: event.TaskPending}}}
	wd := TasksWidget{St: st}
	wide := wd.Render(200, 0, DefaultGlyphs())[0].Text()
	seen := map[string]bool{}
	for w := 1; w <= len(wide)+4; w++ {
		rows := wd.Render(w, 0, DefaultGlyphs())
		if len(rows) > 1 {
			t.Fatalf("width %d rendered %d rows", w, len(rows))
		}
		if len(rows) == 0 {
			continue
		}
		row := rows[0]
		seen[row.Text()] = true
		if row.Width() > w {
			t.Errorf("width %d rendered %d columns: %q", w, row.Width(), row.Text())
		}
		if endsInBareSpace(row) {
			t.Errorf("width %d ends in a space: %q", w, row.Text())
		}
	}
	for _, want := range []string{"Tasks 1/3 · 1 active · 1 pending · /tasks", "Tasks 1/3 · 1 active · /tasks", "Tasks 1/3 · /tasks", "Tasks 1/3", "Tasks"} {
		if !seen[want] {
			t.Errorf("responsive ladder never drew %q", want)
		}
	}
}

func TestTasksWidgetHidesWithoutTasks(t *testing.T) {
	for _, wd := range []TasksWidget{{}, {St: state.New()}} {
		if rows := wd.Render(80, 0, DefaultGlyphs()); rows != nil {
			t.Errorf("empty Tasks widget drew %q", plain(rows))
		}
	}
	if rows := (TasksWidget{St: &state.State{Tasks: []*state.Task{{Status: event.TaskPending}}}}).Render(0, 0, DefaultGlyphs()); rows != nil {
		t.Errorf("zero-width Tasks widget drew %q", plain(rows))
	}
}

// TestStatusRowFits is the same width discipline the transcript gets, at every width a
// pane can be dragged to. The row is chrome, so nothing else in the package measures
// it: the corpus sweeps fold at Height 0 and the app installs this widget, not the
// renderer the sweeps drive.
func TestStatusRowFits(t *testing.T) {
	g := DefaultGlyphs()
	for name, st := range statusStates() {
		for w := 1; w <= 130; w++ {
			rows := (StatusWidget{St: st, Phase: 4}).Render(w, 0, g)
			if len(rows) > 1 {
				t.Fatalf("%s at width %d: the status line is %d rows", name, w, len(rows))
			}
			if len(rows) == 0 {
				continue
			}
			if got := rows[0].Width(); got > w {
				t.Errorf("%s at width %d: row is %d columns: %q", name, w, got, rows[0].Text())
			}
			if endsInBareSpace(rows[0]) {
				t.Errorf("%s at width %d: row ends in a space: %q", name, w, rows[0].Text())
			}
		}
	}
}

// TestStatusDropsWholeSegmentsFromTheRight is the fit rule stated as a property rather
// than as an expected string: whatever a narrow row is, it is the wide row cut at a
// separator. That catches the two ways this can rot — a segment truncated mid-word, and
// a separator left dangling where the segment after it was dropped — without pinning the
// wording of any segment, which is the part that is allowed to change.
func TestStatusDropsWholeSegmentsFromTheRight(t *testing.T) {
	g := DefaultGlyphs()
	for name, st := range statusStates() {
		wd := StatusWidget{St: st, Phase: 4}
		full := wd.Render(400, 0, g)
		if len(full) == 0 {
			t.Fatalf("%s: no row at 400 columns", name)
		}
		want := full[0].Text()
		for w := 1; w <= 130; w++ {
			rows := wd.Render(w, 0, g)
			if len(rows) == 0 {
				continue
			}
			if got := rows[0].Text(); !strings.HasPrefix(want, got) {
				t.Errorf("%s at width %d: %q is not the head of %q", name, w, got, want)
			}
		}
		// And nothing is given up while it still fits. A prefix test alone is satisfied by a
		// row that drops everything, so the exact width of the whole row is checked too: it
		// is the one width where an off-by-one in the fit arithmetic costs a segment, and the
		// only place a reader would see the row lose a field it had room for.
		exact := wd.Render(full[0].Width(), 0, g)
		if len(exact) == 0 || exact[0].Text() != want {
			t.Errorf("%s at its own width %d: %q, want %q", name, full[0].Width(), plain(exact), want)
		}
	}
}

func TestStatusMemberClusterShowsEveryStateInBlueprintOrder(t *testing.T) {
	g := DefaultGlyphs()
	st := &state.State{Active: true, Members: []*state.Member{
		{Name: "scout", Busy: true},
		{Name: "builder", Blocked: &state.Blocked{On: "approval"}},
		{Name: "reviewer", Error: "tests failed"},
		{Name: "scribe"},
	}}
	phase := 3
	wd := StatusWidget{St: st, Phase: phase}
	cluster := wd.memberCluster(g)
	busy := string([]rune(g.Get("status.spinner"))[phase])
	want := busy + "scout " + g.Get("status.member.blocked") + "builder " + g.Get("status.member.failed") + "reviewer " + g.Get("status.member.idle") + "scribe"
	if got := cluster.Text(); got != want {
		t.Fatalf("cluster = %q, want %q", got, want)
	}
	wantStyles := []string{
		"status.member.busy", "status.member.name", "",
		"status.member.blocked", "status.member.name", "",
		"status.member.failed", "status.member.name", "",
		"status.member.idle", "status.member.name",
	}
	if len(cluster) != len(wantStyles) {
		t.Fatalf("cluster has %d spans, want %d: %#v", len(cluster), len(wantStyles), cluster)
	}
	for i, style := range wantStyles {
		if cluster[i].Style != style {
			t.Errorf("span %d style = %q, want %q", i, cluster[i].Style, style)
		}
	}
	// The run spinner and every busy member are driven by the same frame method.
	head := wd.head(g).Text()
	if !strings.HasPrefix(head, busy+" ") {
		t.Errorf("run head = %q, want the member's %q frame", head, busy)
	}
}

func TestStatusMemberClusterIsOneIndivisibleSegment(t *testing.T) {
	g := DefaultGlyphs()
	st := &state.State{Active: true, Members: []*state.Member{
		{Name: "scout", Busy: true}, {Name: "builder", Busy: true}, {Name: "reviewer"},
	}}
	wd := StatusWidget{St: st, Phase: 2}
	head := wd.head(g)
	cluster := wd.memberCluster(g)
	sepW := ansi.StringWidth(" " + g.Get("status.sep") + " ")
	justShort := bottomMargin + head.Width() + sepW + cluster.Width() - 1
	rows := statusRow([]Line{head, cluster}, justShort, g)
	if len(rows) == 0 {
		t.Fatalf("width %d rendered no row", justShort)
	}
	if got, want := unmargin(t, rows.Text()), head.Text(); got != want {
		t.Fatalf("narrow row = %q, want only complete head %q", got, want)
	}
	fits := statusRow([]Line{head, cluster}, justShort+1, g)
	if len(fits) == 0 || !strings.Contains(fits.Text(), "scout") || !strings.Contains(fits.Text(), "reviewer") {
		t.Fatalf("cluster did not appear whole at its exact fit: %q", fits.Text())
	}
}

func TestSingleMemberStatusKeepsLegacyActorSegment(t *testing.T) {
	g := DefaultGlyphs()
	st := &state.State{Actor: "solo", Active: true, Members: []*state.Member{{Name: "solo", Busy: true}}}
	segments := (StatusWidget{St: st, Phase: 1}).segments(g)
	if len(segments) != 2 || segments[1].Text() != "solo" || segments[1][0].Style != "status.text" {
		t.Fatalf("single-member segments = %#v, want the legacy actor segment", segments)
	}
}

func TestApprovalChoicesAreVertical(t *testing.T) {
	rows := (ApprovalWidget{Inbox: &state.Inbox{
		Question: "approve write?", OnTimeout: "deny",
	}}).Render(40, 12, DefaultGlyphs())
	if len(rows) != 4 {
		t.Fatalf("approval rendered %d rows, want question, two choices, and timeout: %q", len(rows), plain(rows))
	}
	want := []string{"approve write?", "y  allow", "n  deny", "timeout  deny"}
	for i, text := range want {
		if !strings.Contains(rows[i].Text(), text) {
			t.Errorf("row %d = %q, want %q", i, rows[i].Text(), text)
		}
	}
	if strings.Contains(rows[1].Text(), "deny") || strings.Contains(rows[2].Text(), "allow") {
		t.Fatalf("approval choices share a row: %q", plain(rows))
	}
}

func TestStatusResponsiveInformationRows(t *testing.T) {
	g := DefaultGlyphs()
	st := statusStates()["working"]
	st.CWD = `D:\projects\arxi\a-very-long-recorded-directory`
	st.GitBranch = "main"
	st.Effort = "xhigh"
	st.ContextUsed, st.ContextCapacity = 81250, 200000
	wd := StatusWidget{St: st, Phase: 4}

	cases := []struct {
		height int
		rows   int
		want   []string
		not    []string
	}{
		{8, 1, []string{"working", "turn 3"}, []string{"main", "context"}},
		{9, 2, []string{"working", "claude-sonnet-4", "context 81.2k/200k (40%)", "effort xhigh"}, []string{`D:\projects`}},
		{14, 3, []string{"working", `D:\projects`, "⑂ main", "claude-sonnet-4", "context 81.2k/200k (40%)"}, nil},
	}
	for _, tc := range cases {
		rows := wd.Render(120, tc.height, g)
		if len(rows) != tc.rows {
			t.Fatalf("height %d rendered %d rows, want %d: %q", tc.height, len(rows), tc.rows, plain(rows))
		}
		text := plain(rows)
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Errorf("height %d does not contain %q: %q", tc.height, want, text)
			}
		}
		for _, unwanted := range tc.not {
			if strings.Contains(text, unwanted) {
				t.Errorf("height %d unexpectedly contains %q: %q", tc.height, unwanted, text)
			}
		}
	}
}

func TestStatusNarrowRowsRetainBranchAndWholeValues(t *testing.T) {
	g := DefaultGlyphs()
	st := &state.State{
		Active: true, CWD: `D:\projects\arxi\deep\nested\workspace`, GitBranch: "feature/mobile",
		Model: "claude-sonnet-5", ContextUsed: 81250, ContextCapacity: 200000, Effort: "high",
		Members: []*state.Member{{Name: "scout"}, {Name: "builder"}, {Name: "reviewer"}},
	}
	wd := StatusWidget{St: st}
	for _, w := range []int{20, 23, 31, 47, 72, 96} {
		rows := wd.Render(w, 14, g)
		for i, row := range rows {
			if row.Width() > w {
				t.Errorf("width %d row %d is %d columns: %q", w, i, row.Width(), row.Text())
			}
			if endsInBareSpace(row) {
				t.Errorf("width %d row %d ends in space: %q", w, i, row.Text())
			}
		}
		text := plain(rows)
		if w >= 31 && !strings.Contains(text, "feature/mobile") {
			t.Errorf("width %d dropped branch: %q", w, text)
		}
		if strings.Contains(text, "81.2k/") && !strings.Contains(text, "81.2k/200k") {
			t.Errorf("width %d split context value: %q", w, text)
		}
	}
	row := wd.provenanceRow(31).Text()
	if !strings.Contains(row, "…") || !strings.Contains(row, "feature/mobile") {
		t.Fatalf("narrow provenance did not middle-ellipsize path and retain branch: %q", row)
	}
}

// TestStatusSaysNothingRatherThanNothingness covers the two ways the row can have
// nothing to say. A nil state is the app before its first event; a width narrower than
// the verb is a pane dragged to a sliver. Both must return no row at all — a widget that
// returns one empty line costs a row of screen and draws a blank in it.
func TestStatusSaysNothingRatherThanNothingness(t *testing.T) {
	g := DefaultGlyphs()
	if rows := (StatusWidget{}).Render(80, 0, g); rows != nil {
		t.Errorf("a nil state drew %d rows", len(rows))
	}
	for _, w := range []int{-1, 0, 1, 2} {
		if rows := (StatusWidget{St: state.New()}).Render(w, 0, g); rows != nil {
			t.Errorf("width %d drew %q", w, rows[0].Text())
		}
	}
}

// TestStatusVerbLadder pins the one thing in the row a reader acts on. The order is the
// argument: a finished run that is blocked reports what it is blocked on, because the
// notice one row up has already said the recording ended and the two rows agreeing is a
// waste of the widest line on the screen.
func TestStatusVerbLadder(t *testing.T) {
	g := DefaultGlyphs()
	want := map[string]string{
		"working": "working",
		"waiting": "waiting",
		"blocked": "blocked on budget_raise",
		"done":    "done",
		"idle":    "idle",
		"bare":    "idle",
	}
	states := statusStates()
	frame0 := string([]rune(g.Get("status.spinner"))[0]) + " "
	for name, verb := range want {
		wd := StatusWidget{St: states[name], Phase: 0}
		row := unmargin(t, wd.Render(400, 0, g)[0].Text())
		body := strings.TrimPrefix(row, frame0)
		// Animated is the timer the app arms from, so it has to mean exactly "there is a
		// spinner in this row" — a frame that says "waiting" asking for a 120 ms wake-up
		// would keep the process busy for a spin nobody can see, and a frame that spins
		// without asking would show the same frame until the next event landed.
		if spun := body != row; spun != wd.Animated() {
			t.Errorf("%s: Animated is %v but the row is %q", name, wd.Animated(), row)
		}
		// And the spinner belongs to one rung of the ladder and no other. Held against
		// Animated alone that is invisible, because both answers come from the same
		// predicate: one that forgot an open approval or a block would flip the row and the
		// timer together and the two would still agree with each other. The verb is the
		// independent witness — something moves only while the row says work is being done,
		// and a run stopped on a question the human has to answer is not working.
		if spun, wantSpin := body != row, verb == "working"; spun != wantSpin {
			t.Errorf("%s: the row is %q, want a spinner: %v", name, row, wantSpin)
		}
		if !strings.HasPrefix(body, verb) {
			t.Errorf("%s: the row opens %q, want the verb %q", name, row, verb)
		}
	}
}

// TestStatusSpinnerIsIndexedByRune is the whole reason one glyph holds ten frames. Every
// frame of the default is a three-byte braille cell, so indexing the cycle by byte would
// cut one in thirds and leave a replacement character turning in the corner of the
// screen. The phase is a counter the app increments forever and never reduces, so it is
// checked past the end of the cycle and, for a caller that decrements one, below zero.
func TestStatusSpinnerIsIndexedByRune(t *testing.T) {
	g := DefaultGlyphs()
	frames := []rune(g.Get("status.spinner"))
	if len(frames) < 2 {
		t.Fatalf("the default spinner has %d frames", len(frames))
	}
	st := statusStates()["working"]
	for p := -len(frames) - 3; p <= 2*len(frames)+3; p++ {
		row := unmargin(t, (StatusWidget{St: st, Phase: p}).Render(400, 0, g)[0].Text())
		want := string(frames[((p%len(frames))+len(frames))%len(frames)])
		if got := string([]rune(row)[0]); got != want {
			t.Errorf("phase %d drew %q, want %q", p, got, want)
		}
	}
}

// TestStatusSpinnerCanBeTurnedOff is the override with nothing in it. Every other glyph
// answers an empty value by drawing nothing, and a cycle of no frames has to mean the
// same rather than panic on a zero modulus — and it has to leave no indent behind
// either, or the verb sits a column right of where every other row starts.
func TestStatusSpinnerCanBeTurnedOff(t *testing.T) {
	g, err := NewGlyphs(map[string]string{"status.spinner": ""}, false)
	if err != nil {
		t.Fatal(err)
	}
	row := unmargin(t, (StatusWidget{St: statusStates()["working"], Phase: 3}).Render(400, 0, g)[0].Text())
	if !strings.HasPrefix(row, "working") {
		t.Errorf("an emptied spinner drew %q", row)
	}
}

// unmargin takes the one column of air off the front of a row that sits at the foot of the
// frame, and fails if it is not there. A check and not a strings.TrimPrefix: trimming a
// prefix that has gone missing succeeds quietly, so the three tests below it would keep
// passing on the day the margin was deleted — which is how a test outlives the behaviour it
// was rewritten for.
func unmargin(t *testing.T, row string) string {
	t.Helper()
	if !strings.HasPrefix(row, " ") || strings.HasPrefix(row, "  ") {
		t.Fatalf("the row is %q, want exactly one column of left margin", row)
	}
	return row[1:]
}

// TestBottomRowsKeepTheirMargin is the reader's own request, held as a property of both rows
// at once: the notice and the status line are the only text in the interface welded to the
// first cell of the terminal, since every other row is either inside the input's border or
// indented by a marker of its own, and one column of air is what takes them off the edge.
//
// Both are checked together because they are one edge of the frame and a margin on one of
// them would read as a misalignment rather than as a margin. The right-hand end is checked
// too: the column is taken off the width before the text is laid out, not added to the row
// afterwards, so a row that reaches the right edge still reaches it and neither row may end
// in a blank — a trailing space in the last cell is how a terminal is talked into wrapping a
// row we thought we owned.
func TestBottomRowsKeepTheirMargin(t *testing.T) {
	g := DefaultGlyphs()
	rows := map[string][]Line{
		"notice": (NoticeWidget{Text: "end of scenario — the input bar is yours"}).Render(60, 0, g),
		"status": (StatusWidget{St: statusStates()["working"]}).Render(60, 0, g),
	}
	for name, got := range rows {
		if len(got) == 0 {
			t.Fatalf("%s drew nothing at 60 columns", name)
		}
		for i, l := range got {
			text := l.Text()
			if !strings.HasPrefix(text, " ") || strings.HasPrefix(text, "  ") {
				t.Errorf("%s row %d is %q, want exactly one column of left margin", name, i, text)
			}
			if strings.HasSuffix(text, " ") {
				t.Errorf("%s row %d is %q, and a trailing blank is how a row wraps", name, i, text)
			}
			if w := l.Width(); w > 60 {
				t.Errorf("%s row %d is %d columns wide at a width of 60", name, i, w)
			}
		}
	}
	// The margin is spent before the fitting, not after it, so a terminal too narrow for the
	// text still gets no row at all rather than a row holding one space — which would be a
	// blank line the reader cannot account for, and a trailing blank besides.
	for _, w := range []int{0, 1} {
		if got := (NoticeWidget{Text: "x"}).Render(w, 0, g); got != nil {
			t.Errorf("the notice drew %q at width %d", got[0].Text(), w)
		}
	}
}

// TestTheHeaderQuotesWhatScrolledOffAndNoMore is the pinned row's whole contract, and every
// clause of it follows from one decision: the header is a quotation, not a summary. So it is
// checked against the transcript's own drawing of the same message rather than against a
// literal — a header that drifted from the block it quotes would still be a plausible row, and
// the reader would be asked to recognise words they never saw in that shape.
//
// Three things then have to hold at once. The rows are the message's own leading rows, in
// order, so the pin is the top of the turn and not some other part of it. There are never more
// than the reader has lost, and never more than the cap, which is what stops a pasted essay
// from taking the screen away from the conversation it labels. And when there is more of the
// message than was pinned, the row says so with the glyph a clipped tool result already uses —
// because leaving it silent is how a reader comes to believe their paragraph was one line long.
//
// The band is the fourth clause and the one most easily lost: banded stamps its wash on every
// span it makes, and the mark and the padding are appended *after* it has run, so a span
// without a fill would punch a visible hole in the right-hand end of the row. The rows are
// swept span by span for exactly that.
func TestTheHeaderQuotesWhatScrolledOffAndNoMore(t *testing.T) {
	g := DefaultGlyphs()
	gap := g.Get("tool.result.gap")
	cut := 0
	for _, text := range boxTexts {
		for _, w := range boxWidths {
			// One row lost, the cap exactly, and more than the cap — which is the count a reader
			// scrolled well past a long question hands in, and the only one that exercises the
			// ceiling rather than the count.
			for _, n := range []int{1, HeaderRows, HeaderRows + 3} {
				got := (HeaderWidget{Text: text, Rows: n}).Render(w, 0, g)
				if text == "" {
					if got != nil {
						t.Errorf("width %d: a message with no words in it drew %q", w, got[0].Text())
					}
					continue
				}
				want := BlockFor(state.Item{Kind: state.KindPrompt, Text: text}).Render(w, g)
				pin := min(n, HeaderRows)
				if len(want) < pin {
					pin = len(want)
				}
				if len(got) != pin {
					t.Fatalf("width %d, %d rows lost of %q: the header drew %d rows, want %d of the %d the transcript draws",
						w, n, text, len(got), pin, len(want))
				}
				for i, l := range got {
					if l.Width() != w {
						t.Errorf("width %d, %d rows lost of %q: row %d is %d columns", w, n, text, i, l.Width())
					}
					if endsInBareSpace(l) {
						t.Errorf("width %d, %d rows lost of %q: row %d ends in a bare space: %q", w, n, text, i, l.Text())
					}
					for _, sp := range l {
						if sp.Fill != "prompt.band" {
							t.Errorf("width %d, %d rows lost of %q: row %d has a span %q filled with %q, and the wash has a hole in it",
								w, n, text, i, sp.Text, sp.Fill)
						}
					}
					// Every row but the last is the transcript's row, character for character. The last one
					// is too when nothing was cut; when something was, it is that row with a mark on the
					// end, which the block below states precisely.
					if i < len(got)-1 || len(want) <= pin {
						if l.Text() != want[i].Text() {
							t.Errorf("width %d, %d rows lost of %q: row %d is\n\t%q\nand the transcript draws\n\t%q", w, n, text, i, l.Text(), want[i].Text())
						}
					}
				}
				if len(want) <= pin {
					continue
				}
				cut++
				last := strings.TrimRight(got[pin-1].Text(), " ")
				if !strings.HasSuffix(last, gap) {
					t.Errorf("width %d: %d of %d rows of %q are pinned and the last of them is %q, which does not say there is more",
						w, pin, len(want), text, last)
					continue
				}
				// And the mark is on the end of that row's own words rather than out at the right edge,
				// where a lone glyph in the last column reads as a scrollbar. What precedes it is a
				// prefix of the row the transcript drew — a prefix and not the whole of it, because the
				// mark has to be paid for in columns when the row was already full.
				if body := strings.TrimSuffix(last, gap); !strings.HasPrefix(strings.TrimRight(want[pin-1].Text(), " "), body) {
					t.Errorf("width %d: the pinned row reads\n\t%q\nwhich is not how the transcript's row begins:\n\t%q", w, body, want[pin-1].Text())
				}
			}
		}
	}
	if cut == 0 {
		t.Fatal("no message here was ever longer than the cap, so the mark and the cut are untested")
	}
	// A count of nothing is no header. The rule about when a header is wanted lives in the
	// caller, so the widget's side of it is this: handed a turn the reader has not scrolled past,
	// it draws no row rather than a row they can already see.
	for _, n := range []int{0, -1} {
		if got := (HeaderWidget{Text: "a question", Rows: n}).Render(72, 0, g); got != nil {
			t.Errorf("%d rows lost and the header drew %q", n, got[0].Text())
		}
	}
	// A terminal too narrow for the marker draws no header either, for banded's reason: a marker
	// with nothing after it is not a landmark, and a row of one is worse than none.
	for _, w := range []int{0, 1, 2} {
		if got := (HeaderWidget{Text: "a question", Rows: 1}).Render(w, 0, g); got != nil {
			t.Errorf("width %d: the header drew %q", w, got[0].Text())
		}
	}
	// And where it goes, which is the last thing that has to be true for the row to cost what it
	// costs. It asks for the top slot and names no fallback, so a surface with no fixed top leaves
	// it out entirely instead of dropping it somewhere it would be reprinted into the terminal's
	// history on every frame.
	if s, ok := Place(HeaderWidget{Text: "a question", Rows: 1}, Viewport{Width: 72, Height: 40, FixedTop: true}); !ok || s != SlotTop {
		t.Errorf("a surface with a fixed top placed the header in %q (drawn: %v)", s, ok)
	}
	if s, ok := Place(HeaderWidget{Text: "a question", Rows: 1}, Viewport{Width: 72, Height: 40}); ok {
		t.Errorf("a surface with no fixed top placed the header in %q, where a pinned row cannot stay pinned", s)
	}
}

// TestTheStatusBandCrossesTheVerbAndNothingElse pins the seam between the animation and the row
// it animates, which is one line of head and gets two things right at once.
//
// The band is handed in unconditionally — the app has no way to know which rung of the ladder
// this row will pick, and a shine wired per-rung in the caller would be the ladder written
// twice — so it is head that decides light means work. A glint on "waiting" would say something
// is moving while the run sits on a question nobody has answered, which is exactly what the
// spinner beside it is there to deny; on "done" it would animate a recording that has ended.
//
// And it is measured against the word rather than against the terminal. Apply is given
// verb.Width() and not the row's width, so seven columns are crossed in the pass's twelve
// ticks. Handed the row's width instead, a four-hundred-column window would put the band over
// the word for one tick of the forty and leave it dark for the other thirty-nine — still a
// shine, still green under every other test here, and not a thing a reader would ever see. What
// the assertion below states is the sharp version of that: the lit columns of the verb are the
// ones Band names for a seven-column row, at every phase of a whole cycle.
func TestTheStatusBandCrossesTheVerbAndNothingElse(t *testing.T) {
	g := DefaultGlyphs()
	rung := map[string]bool{}
	for _, k := range ramps[StatusShine] {
		rung[k] = true
	}
	const verb = "working"
	states := statusStates()
	lit := 0
	// The rungs by name rather than by ranging the map, so that a failure reports the same one
	// first on every run and so that a rung added to statusStates without being considered here
	// fails rather than going unchecked.
	for _, name := range []string{"working", "waiting", "blocked", "done", "idle", "bare"} {
		st, ok := states[name]
		if !ok {
			t.Fatalf("statusStates has no %q", name)
		}
		for phase := 0; phase < shinePeriod; phase++ {
			wd := StatusWidget{St: st, Phase: 2, Shine: Shimmer{Style: StatusShine, Phase: phase}}
			rows := wd.Render(400, 0, g)
			if len(rows) == 0 {
				t.Fatalf("%s drew no row at 400 columns", name)
			}
			// The verb's own columns are the ones carrying its key in either slot, because a lit
			// span keeps status.verb in Fill — that is the composition the whole type is built on,
			// and a test reading only Style would report the word as having gone missing.
			at, start, end := 0, -1, -1
			shone := map[int]bool{}
			for _, sp := range rows[0] {
				w := ansi.StringWidth(sp.Text)
				if sp.Style == "status.verb" || sp.Fill == "status.verb" {
					if start < 0 {
						start = at
					}
					end = at + w
				}
				if rung[sp.Style] {
					if sp.Fill != "status.verb" {
						t.Fatalf("%s phase %d: a lit span %q fills with %q, so the row's own style is gone",
							name, phase, sp.Text, sp.Fill)
					}
					for c := at; c < at+w; c++ {
						shone[c] = true
					}
				}
				at += w
			}
			if start < 0 {
				t.Fatalf("%s phase %d: no verb in %q", name, phase, rows[0].Text())
			}
			if name != "working" {
				if len(shone) > 0 {
					t.Errorf("%s phase %d is lit at %v: %q", name, phase, sortedCols(shone), rows[0].Text())
				}
				continue
			}
			if got := end - start; got != len(verb) {
				t.Fatalf("phase %d: the verb is %d columns, want the %d of %q", phase, got, len(verb), verb)
			}
			from, to, on := Shimmer{Style: StatusShine, Phase: phase}.Band(len(verb))
			want := map[int]bool{}
			for c := from; on && c < to; c++ {
				want[start+c] = true
			}
			if len(want) != len(shone) {
				t.Fatalf("phase %d: the row lights %v, the word's own band is %d..%d of %d columns",
					phase, sortedCols(shone), from, to, len(verb))
			}
			for c := range want {
				if !shone[c] {
					t.Fatalf("phase %d: column %d is in the word's band %d..%d and is not lit: %v",
						phase, c-start, from, to, sortedCols(shone))
				}
			}
			lit += len(want)
			// The text is what it was before the light crossed it, which Apply promises and this
			// row is the only widget that can check at the seam: a band that cut a span wrongly
			// would show up here as a verb one column short of its own word.
			plainWd := StatusWidget{St: st, Phase: 2}
			if got, want := rows[0].Text(), plainWd.Render(400, 0, g)[0].Text(); got != want {
				t.Fatalf("phase %d rewrote the row:\n got %q\nwant %q", phase, got, want)
			}
		}
	}
	// And the light does arrive, on a row this wide, within one cycle. Without this the whole
	// test above passes on a shine that never draws anything — an empty band equals an empty
	// expectation at every phase, which is the shape a mutant that measures the pass against the
	// terminal takes at a width no terminal has.
	if lit == 0 {
		t.Error("no phase of a whole cycle put any light on the verb")
	}
}

// sortedCols is the columns of a set, in order, for an error message. A map printed directly
// comes out in a different order on every run, which is how a failure that is one column off
// reads as a different failure each time it is looked at.
func sortedCols(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for c := range m {
		out = append(out, c)
	}
	sort.Ints(out)
	return out
}

// thumbOf reads a drawn column back as a run of thumb cells: where it starts and how long it is.
func thumbOf(t *testing.T, rows []Line) (top, n int) {
	t.Helper()
	top, n, _ = thumbAndStyleOf(t, rows)
	return top, n
}

// thumbAndStyleOf is thumbOf plus the style the run was drawn in, which is how the held pill is
// asserted: held is not a glyph and not a position, so the only way to see it is to read the key.
//
// It is read off the drawing rather than taken from bar, which is the private function under
// test, so the assertions below are made against what a reader would see. A gap in the middle
// would be two bars, and a widget that drew one would still satisfy every count. Two styles in one
// run would be a pill half held, which is not a state this widget has.
func thumbAndStyleOf(t *testing.T, rows []Line) (top, n int, style string) {
	t.Helper()
	top = -1
	for i, l := range rows {
		if len(l) == 0 {
			continue
		}
		s := l[len(l)-1].Style
		if s != "scroll.thumb" && s != "scroll.thumb.held" {
			continue
		}
		if top < 0 {
			top, style = i, s
		} else if i != top+n {
			t.Fatalf("the thumb is broken: rows %d..%d and again at %d", top, top+n-1, i)
		} else if s != style {
			t.Fatalf("the thumb changes style at row %d: %q after %q", i, s, style)
		}
		n++
	}
	if top < 0 {
		top = 0
	}
	return top, n, style
}

// TestTheBarSaysHowMuchAndWhere is the pill's whole contract, and it is written as a sweep over
// geometries rather than as a handful of pictures because a scrollbar is arithmetic: every
// number it draws is a ratio, and a ratio is exactly the kind of thing that is right at the
// example somebody checked and wrong two rows either side of it.
//
// Five claims, and each of them is something a reader acts on.
//
// The length is the window over the whole log, so a bar that fills most of its track means most of
// the conversation is on screen. It is monotone in the log's own length — hold the window still,
// feed it a longer transcript, and the bar may not grow — which is the property that catches an
// inverted ratio, a thing no single case can see.
//
// The position is where the window is, and it is bounded by two claims that have to be exact
// rather than approximately right: the thumb touches the top only when there is nothing above it,
// and the bottom only when there is nothing below. Rounding is what makes those interesting — on
// a long log the arithmetic reaches an end a row early, and a bar resting at the bottom while a
// hundred rows are still under it is the one error that costs the reader a keypress to discover.
//
// An off-screen row always leaves an empty cell, which is the same claim from the other side and
// the reason a cell is held back at each occupied end before the thumb is measured.
//
// A transcript that fits draws nothing at all. A bar as long as its own track is not information,
// and drawing it would spend a column saying so in the one case where the reader can already see
// the whole log.
//
// And a track with no room for the held-back cells draws nothing either, which is what the two
// exact position claims cost at the shortest heights. The sweep asserts that price rather than
// tolerating it — whether a bar exists at all is a function of the height and the occupied ends,
// nothing else — so a column that quietly starts lying at h=2 fails here.
func TestTheBarSaysHowMuchAndWhere(t *testing.T) {
	g := DefaultGlyphs()
	// Heights a terminal really has, plus the three degenerate ones. 1 and 2 are what is left of a
	// screen whose chrome has taken all but a row or two, and 3 is the shortest track that can hold
	// a thumb with a cell spare at either end — so it is the first height at which a window in the
	// middle of a log can be drawn at all.
	for _, h := range []int{1, 2, 3, 12, 40, 200} {
		for _, above := range []int{0, 1, 7, 40, 5000} {
			for _, below := range []int{0, 1, 7, 40, 5000} {
				sc := Scroll{Above: above, Rows: h, Below: below}
				rows := (ScrollbarWidget{Scroll: sc}).Render(SideCols, h, g)
				where := fmt.Sprintf("h=%d above=%d below=%d", h, above, below)
				// One cell per occupied end and one for the thumb is the whole of when a bar
				// exists. It is spelled out here from the ends rather than read back off the
				// drawing, so the drawing cannot define its own precondition.
				ends := 0
				if above > 0 {
					ends++
				}
				if below > 0 {
					ends++
				}
				if ends == 0 || h < ends+1 {
					if rows != nil {
						t.Errorf("%s: %d hidden rows in a %d-row track have no honest bar, and %d rows were drawn", where, above+below, h, len(rows))
					}
					continue
				}
				if len(rows) != h {
					t.Fatalf("%s: the column is %d rows tall and the bar drew %d", where, h, len(rows))
				}
				top, n := thumbOf(t, rows)
				if n < 1 {
					t.Fatalf("%s: %d rows off screen and no thumb was drawn", where, above+below)
				}
				if top+n > h {
					t.Fatalf("%s: the thumb runs from %d for %d rows and the column is %d tall", where, top, n, h)
				}
				if n > h-ends {
					t.Errorf("%s: the thumb is %d of %d rows, leaving no empty cell for %d occupied end(s)", where, n, h, ends)
				}
				// The two honesty guards, stated in both directions so that neither a bar welded to
				// an end nor one that never reaches an end can pass.
				if (top == 0) != (above == 0) {
					t.Errorf("%s: the thumb starts at row %d, and it may touch the top only when nothing is above it", where, top)
				}
				if (top+n == h) != (below == 0) {
					t.Errorf("%s: the thumb ends at row %d of %d, and it may touch the bottom only when nothing is below it", where, top+n, h)
				}
			}
		}
	}
	// Monotonicity is the one claim the sweep above cannot see: it varies the two ends
	// independently, so it never holds a shape still and grows the log underneath it. Here the
	// window and both occupied ends stay put, only the amount hidden grows, and the thumb has to
	// shrink or hold. An inverted ratio passes every case above and fails on the second step here.
	for _, h := range []int{3, 12, 40} {
		last := h + 1
		for _, hidden := range []int{2, 4, 10, 50, 400, 9000} {
			sc := Scroll{Above: hidden / 2, Rows: h, Below: hidden - hidden/2}
			_, n := thumbOf(t, (ScrollbarWidget{Scroll: sc}).Render(SideCols, h, g))
			if n > last {
				t.Errorf("h=%d: %d rows hidden drew a %d-row thumb, longer than the %d rows drawn when less was hidden", h, hidden, n, last)
			}
			last = n
		}
	}
}

// TestTheBarSurvivesItsOwnGlyphs is the column's answer to a theme file, which is the one input
// here a person writes by hand. A glyph is a string, and the two strings that break a fixed-width
// column are the empty one and one wider than the column is — so an override of either kind draws
// nothing for that cell rather than a row of the wrong width. A wrong-width row down the side of
// the transcript is not a cosmetic problem: it is a Frame.Overflow, and past that a conversation
// that rewraps on every frame.
//
// Nothing for the cell and not nothing for the bar: the column is still h rows tall, because the
// rows are what holds the alignment. Dropping one would slide every row under it up by one and
// leave the pill pointing at the wrong part of the log, which is worse than a gap.
func TestTheBarSurvivesItsOwnGlyphs(t *testing.T) {
	const h = 12
	sc := Scroll{Above: 40, Rows: h, Below: 90}
	top, n := thumbOf(t, (ScrollbarWidget{Scroll: sc}).Render(SideCols, h, DefaultGlyphs()))
	if n < 1 || n >= h {
		t.Fatalf("the shipped glyphs drew a %d-row thumb in a %d-row column, so this test can tell a blanked track from a blanked thumb no better than it can tell either from nothing", n, h)
	}
	for _, key := range []string{"scroll.track", "scroll.thumb"} {
		for _, bad := range []string{"", "###", "  |  "} {
			g, err := NewGlyphs(map[string]string{key: bad}, false)
			if err != nil {
				t.Fatalf("%s=%q: %v", key, bad, err)
			}
			rows := (ScrollbarWidget{Scroll: sc}).Render(SideCols, h, g)
			if len(rows) != h {
				t.Fatalf("%s=%q: the column is %d rows tall and the bar drew %d, so the rows under it have moved", key, bad, h, len(rows))
			}
			for i, l := range rows {
				// The rows the override owns are the ones that must be blank, and the rest must be
				// untouched — a widget that blanked the whole column on one bad key would pass a
				// test that only looked for gaps.
				thumb := i >= top && i < top+n
				if (key == "scroll.thumb") == thumb {
					if len(l) != 0 {
						t.Errorf("%s=%q: row %d drew %d cells for a glyph the column has no room for", key, bad, i, len(l))
					}
					continue
				}
				if l.Width() != SideCols {
					t.Errorf("%s=%q: row %d is %d cells wide in a %d-cell column, and the other glyph was the one overridden", key, bad, i, l.Width(), SideCols)
				}
			}
		}
	}
}

// TestTheBarIsPlacedOnlyWhereAColumnCanBeHeld is the inline-mode contract, and it belongs to Place
// rather than to the caller. The app installs the bar on every surface on purpose: a chrome that
// asked about the surface first would be a second copy of this rule, kept a layer away from the
// slot table that already holds it and free to disagree with it.
//
// The fallback is the empty slot rather than a full-width row, which is where this widget parts
// company with the pinned header. A bar is a position in a column, and there is no honest way to
// state a position in a column across a row. Inline mode already answers "is there more?" with the
// terminal's own scrollback, which has a bar of its own.
func TestTheBarIsPlacedOnlyWhereAColumnCanBeHeld(t *testing.T) {
	w := ScrollbarWidget{Scroll: Scroll{Above: 40, Rows: 12, Below: 90}}
	if s, ok := Place(w, Viewport{Width: 100, Height: 40, SidePanels: true}); !ok || s != SlotRight {
		t.Errorf("on a screen holding side panels the bar was placed in %q (placed=%v), want %q", s, ok, SlotRight)
	}
	// Every shape of viewport that cannot hold a column, including the two that hold the other
	// optional region — a fixed top is not a side panel, and a widget reading either flag for the
	// other would pass on the plain screen and fail on the one arxi actually draws.
	for _, vp := range []Viewport{
		{Width: 100, Height: 40},
		{Width: 100, Height: 40, FixedTop: true},
		{Width: 100, Height: 40, Scrolled: true, ScrollTop: 3},
		{Width: 100},
	} {
		if s, ok := Place(w, vp); ok {
			t.Errorf("viewport %+v can hold no side column and the bar was placed in %q", vp, s)
		}
	}
}

// pill renders a geometry and reads the thumb back out of it, so the drag tests below can speak in
// the rows a reader sees rather than in the arithmetic under test. n of 0 means no bar was drawn.
func pill(t *testing.T, g Glyphs, h, above, rows, below int) (sb ScrollbarWidget, top, n int) {
	t.Helper()
	sb = ScrollbarWidget{Scroll: Scroll{Above: above, Rows: rows, Below: below}}
	top, n = thumbOf(t, sb.Render(SideCols, h, g))
	return sb, top, n
}

// TestTheHeldThumbIsAStyleAndNothingElse pins the one thing holding the pill may change. A column
// that grew a cell, moved a row, or restyled its track under the finger would read as the bar having
// jumped rather than been grabbed — and a widened cell is a frame two columns too wide, which is the
// failure that rewraps the whole conversation.
func TestTheHeldThumbIsAStyleAndNothingElse(t *testing.T) {
	const h = 12
	g := DefaultGlyphs()
	sc := Scroll{Above: 40, Rows: h, Below: 90}
	loose := (ScrollbarWidget{Scroll: sc}).Render(SideCols, h, g)
	held := (ScrollbarWidget{Scroll: sc, Held: true}).Render(SideCols, h, g)
	top, n, style := thumbAndStyleOf(t, loose)
	if n < 1 || style != "scroll.thumb" {
		t.Fatalf("an unheld thumb of %d rows drew style %q, want some rows of %q", n, style, "scroll.thumb")
	}
	if top2, n2, style2 := thumbAndStyleOf(t, held); top2 != top || n2 != n || style2 != "scroll.thumb.held" {
		t.Errorf("held drew %d rows from %d in %q, want %d from %d in %q", n2, top2, style2, n, top, "scroll.thumb.held")
	}
	for i := range loose {
		if a, b := loose[i].Text(), held[i].Text(); a != b {
			t.Errorf("row %d reads %q held and %q loose, and holding the pill is a style and not a glyph", i, b, a)
		}
	}
}

// TestGrabSaysWhatAPressLandedOn is the hit test, swept for the same reason the bar's own contract
// is: which rows the pill covers is a ratio, and the row just above and just below it are the two a
// reader keeps hitting.
//
// Three claims, and the app acts on all of them. A press on the pill hands back how far into it the
// pointer went, which is what keeps that spot of the pill under the finger for the rest of the drag
// instead of snapping its middle there. A press on bare track hands back the middle, so the jump that
// follows puts the pill under the pointer. And the flag between them is the widget's answer rather
// than the caller's: the app moves the window on one and not on the other, and a caller working the
// rows out for itself would be a second copy of bar's arithmetic, free to drift by a row.
//
// Not ok is a row outside the column, or a frame with no bar in it, and nothing else — every row of a
// column that has a bar can be taken hold of, including the cells held back at the ends.
func TestGrabSaysWhatAPressLandedOn(t *testing.T) {
	g := DefaultGlyphs()
	for _, h := range []int{3, 4, 5, 12, 40} {
		for _, total := range []int{h + 1, h + 2, h + 5, h + 40, h + 9000} {
			d := total - h
			for above := 0; above <= d; above += max(1, d/13) {
				sb, top, n := pill(t, g, h, above, h, d-above)
				if n == 0 {
					continue
				}
				where := fmt.Sprintf("h=%d above=%d below=%d", h, above, d-above)
				for _, row := range []int{-1, h, h + 1} {
					if _, _, ok := sb.Grab(h, row); ok {
						t.Errorf("%s: row %d is outside the column and was grabbed", where, row)
					}
				}
				grabTheRows(t, sb, where, h, top, n)
			}
		}
	}
	// A frame with nothing hidden draws no bar, so there is nothing to take hold of anywhere in it.
	for _, sc := range []Scroll{{}, {Rows: 12}, {Above: 0, Rows: 12, Below: 0}} {
		for _, row := range []int{0, 5, 11} {
			if _, _, ok := (ScrollbarWidget{Scroll: sc}).Grab(12, row); ok {
				t.Errorf("scroll %+v draws no bar and row %d was grabbed anyway", sc, row)
			}
		}
	}
}

// grabTheRows presses every row of one column and checks the three answers against the pill that was
// actually drawn there.
func grabTheRows(t *testing.T, sb ScrollbarWidget, where string, h, top, n int) {
	t.Helper()
	for row := 0; row < h; row++ {
		grab, onThumb, ok := sb.Grab(h, row)
		if !ok {
			t.Fatalf("%s: row %d of a column with a bar in it could not be grabbed", where, row)
		}
		drawn := row >= top && row < top+n
		if onThumb != drawn {
			t.Errorf("%s: a press on row %d says onThumb=%v and the pill is drawn on rows %d..%d", where, row, onThumb, top, top+n-1)
		}
		want := n / 2
		if drawn {
			want = row - top
		}
		if grab != want {
			t.Errorf("%s: a press on row %d took hold at %d of a %d-row pill starting at %d, want %d", where, row, grab, n, top, want)
		}
	}
}

// TestADragLandsTheThumbUnderThePointer is the drag's contract, and it decides whether the gesture is
// a gesture or an approximation of one. Every claim in it is read off two drawings: the column the
// press landed in, and the column the offset it handed back draws.
//
// The pointer ends up on the pill. That is the whole feel of dragging a scrollbar — the thing follows
// the finger — and it is why the arithmetic must not be inverted in the frame it started from. A cell
// is held back only at an occupied end, so the track changes shape the moment the window reaches the
// top or the bottom of the log, and a drag measured against the old shape sticks a row behind the
// finger at exactly the two positions a reader aims for. It is swept over every height and every
// offset because that is where it went wrong.
//
// The pill does not change length. Its length is the window over the whole log and a drag changes
// neither, so a pill that grew while it was dragged would be the surest sign the offset had been fed
// back into the wrong denominator.
//
// Both ends are reachable, exactly. The top row of the track means the first row of the log and the
// bottom row the last, which is what lets a drag hand the conversation back — the window landing on
// the end is what resumes following — and a rounding that stopped a row short would leave the reader
// holding a pill they cannot get home with.
//
// And a drag never runs backwards: pressing further down the track never scrolls the log up. That is
// the claim no single case can see, and the one a reader would describe as the bar fighting them.
func TestADragLandsTheThumbUnderThePointer(t *testing.T) {
	g := DefaultGlyphs()
	for _, h := range []int{3, 4, 5, 6, 12, 40, 200} {
		for _, total := range []int{h + 1, h + 2, h + 3, h + 7, h + 40, h + 400, h + 9000, 2 * h, 3*h + 1} {
			d := total - h
			for above := 0; above <= d; above += max(1, d/13) {
				sb, _, n := pill(t, g, h, above, h, d-above)
				if n == 0 {
					continue
				}
				dragTheRows(t, g, sb, h, d, n)
			}
		}
	}
}

// dragTheRows presses every row of one column's track and reads the column each press asks for.
//
// The rows the pill is drawn on are skipped rather than asserted about: taking hold of the pill is a
// different gesture, it moves nothing, and Grab's own test is where the flag that says so is checked.
// Which leaves the bare track, where every press is a request to go somewhere, and the last offset a
// press asked for is carried along so that the sweep can see the one property a single case cannot.
func dragTheRows(t *testing.T, g Glyphs, sb ScrollbarWidget, h, d, n int) {
	t.Helper()
	where := fmt.Sprintf("h=%d, %d hidden with %d above", h, d, sb.Scroll.Above)
	last := -1
	for row := 0; row < h; row++ {
		grab, onThumb, ok := sb.Grab(h, row)
		if !ok {
			t.Fatalf("%s: row %d of a column with a bar in it could not be grabbed", where, row)
		}
		if onThumb {
			continue
		}
		above, ok := sb.Offset(h, row, grab)
		if !ok {
			t.Fatalf("%s: a column with a bar in it refused a drag to row %d", where, row)
		}
		if above < 0 || above > d {
			t.Fatalf("%s: a drag to row %d asks to hide %d rows of a log with %d to hide", where, row, above, d)
		}
		if above < last {
			t.Errorf("%s: a drag to row %d asks for %d above, back up from the %d the row before asked for", where, row, above, last)
		}
		last = above
		if row == 0 && above != 0 {
			t.Errorf("%s: a drag to the top row asks for %d above, and the top of the track is the top of the log", where, above)
		}
		if row == h-1 && above != d {
			t.Errorf("%s: a drag to the last row asks for %d of %d above, and the end of the track is the end of the log", where, above, d)
		}
		_, moved, length := pill(t, g, h, above, h, d-above)
		if length != n {
			t.Errorf("%s: a drag to row %d turned a %d-row pill into a %d-row one", where, row, n, length)
		}
		if row < moved || row >= moved+length {
			t.Errorf("%s: a drag to row %d left the pill on rows %d..%d, out from under the pointer", where, row, moved, moved+length-1)
		}
	}
}

// TestADragLandsAsCloseAsAnyOffsetCould is the claim a long pill would hide. The transcript moves in
// whole rows and so does the track, and a log with more hidden rows than the track has cells cannot
// name every window — so the honest claim is not that the pill lands exactly where the finger asked,
// but that no other offset would have landed it closer. That is what makes a drag feel like it tracks
// the finger rather than leading or lagging it, and it is the claim that catches a rounding bias: half
// a cell of consistent error is invisible in any one case and is the difference between a pill that
// sits under the pointer and one that sits just above it the whole way down.
//
// It sweeps small logs only, and every offset in them. A long log's coarseness is the same arithmetic
// with a longer step, and the whole family has to be drawn to know what the nearest of them was, so
// the sweep buys its certainty with the size of the log rather than the height of the track.
func TestADragLandsAsCloseAsAnyOffsetCould(t *testing.T) {
	g := DefaultGlyphs()
	for _, h := range []int{3, 4, 5, 6, 7, 12, 40} {
		for _, d := range []int{1, 2, 3, 5, 8, 13, h, 2*h + 1} {
			// Where every offset in the family draws its pill, so that nearest is a fact about the
			// family and not about the one the arithmetic picked. An offset that draws no pill is not a
			// candidate to be near anything, and -1 is how it says so.
			tops := make([]int, 0, d+1)
			for above := 0; above <= d; above++ {
				_, top, n := pill(t, g, h, above, h, d-above)
				if n == 0 {
					top = -1
				}
				tops = append(tops, top)
			}
			for above := 0; above <= d; above++ {
				sb, _, n := pill(t, g, h, above, h, d-above)
				if n == 0 {
					continue
				}
				where := fmt.Sprintf("h=%d, %d hidden with %d above", h, d, above)
				for row := 0; row < h; row++ {
					grab, onThumb, ok := sb.Grab(h, row)
					if !ok {
						t.Fatalf("%s: row %d of a column with a bar in it could not be grabbed", where, row)
					}
					if onThumb {
						continue
					}
					got, ok := sb.Offset(h, row, grab)
					if !ok {
						t.Fatalf("%s: a column with a bar in it refused a drag to row %d", where, row)
					}
					if tops[got] < 0 {
						t.Fatalf("%s: a drag to row %d asks for %d above, which draws no pill at all", where, row, got)
					}
					// What the press asked for: the row the pill would start on if the track were as
					// fine as the finger. Every offset is measured against that, including the one the
					// arithmetic chose.
					target := min(max(row-grab, 0), h-n)
					if best := closestTop(tops, target); abs(tops[got]-target) > best {
						t.Errorf("%s: a drag to row %d asks the pill to start at %d and gets %d with %d above, when some offset starts it within %d", where, row, target, tops[got], got, best)
					}
				}
			}
		}
	}
}

// closestTop is how near the best offset in a family gets to the row the press asked for. The tops
// that draw nothing are skipped, and a family with none of them is not one this can be asked about —
// the caller found a pill before it got here.
func closestTop(tops []int, target int) int {
	best := -1
	for _, top := range tops {
		if top < 0 {
			continue
		}
		if d := abs(top - target); best < 0 || d < best {
			best = d
		}
	}
	return best
}
