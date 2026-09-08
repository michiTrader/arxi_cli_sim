package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"arxi.local/sim/internal/state"
)

// The elision of a long tool result, pinned here rather than in the corpus because
// what these tests are about is a boundary. 09-long-output.ndjson draws both sides of
// it once; a constant is worth a test that fails when somebody moves it by one.

// longResult is a failed result of n lines, each naming its own number so a test can
// say which lines survived and in which order.
func longResult(n int) state.Item {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("row-%d", i+1)
	}
	return state.Item{
		Kind: state.KindTool, ID: "tool-long", Tool: "bash",
		Args: map[string]any{"cmd": "go test ./..."}, Status: state.ToolFailed,
		Summary: strings.Join(rows, "\n"),
	}
}

// resultRows is the result half of a tool block. The indent is the width of the tool
// marker, which is what renderTool passes and the only thing the elbow is offset by.
func resultRows(it state.Item, width int, g Glyphs) []Line {
	return ItemBlock{It: it}.resultLines(width, 2, g)
}

// resultText is a row with its prefix taken off: the pad, and on the first row the
// elbow too, are layout, and every test here is about what the row says.
func resultText(l Line, g Glyphs) string {
	s := strings.TrimSpace(l.Text())
	return strings.TrimSpace(strings.TrimPrefix(s, strings.TrimRight(g.Get("tool.result"), " ")))
}

// TestALongResultKeepsBothEnds pins what survives and what the transcript admits to
// dropping. The head is where the command names itself, the tail is where the failure
// is, and the count between them is checkable: head + count + tail is the number of
// lines the reader would see if they ran the command themselves.
func TestALongResultKeepsBothEnds(t *testing.T) {
	const n = 40
	g := DefaultGlyphs()
	rows := resultRows(longResult(n), 72, g)
	got := texts(rows)

	want := []string{"row-1", "row-2", "row-3", g.Get("tool.result.gap") + " 29 more lines"}
	for i := n - resultTail + 1; i <= n; i++ {
		want = append(want, fmt.Sprintf("row-%d", i))
	}
	if len(got) != len(want) {
		t.Fatalf("a %d-line result drew %d rows, want %d:\n%s", n, len(got), len(want), strings.Join(got, "\n"))
	}
	for i, w := range want {
		if resultText(rows[i], g) != w {
			t.Errorf("row %d is %q, want %q", i, got[i], w)
		}
	}
}

// TestTheThresholdNeverSpendsARowToSaveOne is the boundary the two constants imply.
// At head+tail+1 lines the notice costs exactly what it saves, so the result is drawn
// whole; one line further the elision starts, and from there it always admits to two
// or more — which is why the wording has no singular anywhere to get wrong.
func TestTheThresholdNeverSpendsARowToSaveOne(t *testing.T) {
	g := DefaultGlyphs()
	marker := g.Get("tool.result.gap")
	whole := resultHead + resultTail + 1

	rows := resultRows(longResult(whole), 72, g)
	if len(rows) != whole {
		t.Errorf("a %d-line result drew %d rows; it fits and should be whole", whole, len(rows))
	}
	if all := strings.Join(texts(rows), "\n"); strings.Contains(all, marker) {
		t.Errorf("a %d-line result was elided to save nothing:\n%s", whole, all)
	}

	rows = resultRows(longResult(whole+1), 72, g)
	if len(rows) != resultHead+1+resultTail {
		t.Fatalf("a %d-line result drew %d rows, want %d:\n%s",
			whole+1, len(rows), resultHead+1+resultTail, strings.Join(texts(rows), "\n"))
	}
	if got := resultText(rows[resultHead], g); got != marker+" 2 more lines" {
		t.Errorf("the shortest result worth eliding says %q", got)
	}
}

// TestAnElidedResultWearsOneElbowAndKeepsTheGapItsOwn. The elbow introduces the
// result and not each of the three pieces it is assembled from, because three elbows
// read as three results. And the gap line is the interface speaking, not the command:
// it keeps its own style key even here, where the result failed and every line the
// command did write is drawn in red around it.
func TestAnElidedResultWearsOneElbowAndKeepsTheGapItsOwn(t *testing.T) {
	g := DefaultGlyphs()
	rows := resultRows(longResult(40), 72, g)
	elbow := strings.TrimRight(g.Get("tool.result"), " ")
	elbows := 0
	for i, l := range rows {
		if !strings.Contains(l.Text(), elbow) {
			continue
		}
		elbows++
		if i != 0 {
			t.Errorf("row %d of %d carries the elbow: %q", i, len(rows), l.Text())
		}
	}
	if elbows != 1 {
		t.Errorf("drew %d elbows for one result:\n%s", elbows, strings.Join(texts(rows), "\n"))
	}
	styleOf(t, "the gap line", rows[resultHead], "tool.result.gap")
	styleOf(t, "the last line the command wrote", rows[len(rows)-1], "tool.result.error")
}

// styleOf asserts every inked span of a row names one style key. The padding is
// skipped: it is a prefix join builds, and it carries no key by design.
func styleOf(t *testing.T, what string, l Line, want string) {
	t.Helper()
	for _, sp := range l {
		if strings.TrimSpace(sp.Text) == "" {
			continue
		}
		if sp.Style != want {
			t.Errorf("%s draws %q as %q, want %q", what, sp.Text, sp.Style, want)
		}
	}
}

// TestAnElidedResultFitsEveryWidth. The gap line is text like any other and goes
// through the same wrap as the result, so the widths worth checking are the narrow
// ones where "… 29 more lines" does not fit beside an elbow at all.
func TestAnElidedResultFitsEveryWidth(t *testing.T) {
	g := DefaultGlyphs()
	it := longResult(40)
	elided := false
	for width := 1; width <= 96; width++ {
		rows := resultRows(it, width, g)
		for i, l := range rows {
			if l.Width() > width {
				t.Fatalf("width %d: row %d is %d columns: %q", width, i, l.Width(), l.Text())
			}
			if txt := l.Text(); strings.HasSuffix(txt, " ") {
				t.Fatalf("width %d: row %d ends in a bare space: %q", width, i, txt)
			}
			if strings.Contains(l.Text(), g.Get("tool.result.gap")) {
				elided = true
			}
		}
	}
	if !elided {
		t.Fatal("no width elided anything, so this test proves nothing")
	}
}

// The hanging indent a wrapped result line gets, which is the other half of drawing a
// long result: eliding one decides which lines survive, and this decides what a line
// that survived looks like when it does not fit the pane.

// hangs are the indents a line of a result can arrive with, and the column each one is
// worth once the wrapper has had it. A tab is a stop rather than a column, so "\t" and
// "  \t" both land on four, exactly where four spaces land.
var hangs = []struct {
	prefix string
	col    int
}{
	{"", 0},
	{"    ", 4},
	{"\t", 4},
	{"  \t", 4},
	{"        ", 8},
}

// hangingResult is the shape a `go test` failure has: a header at the result's own left
// edge and details indented under it, every line long enough to wrap. Each word names
// the line it belongs to and its place on that line, which is what lets a test say
// where a row continues from without counting rows the width decides.
func hangingResult() state.Item {
	rows := make([]string, len(hangs))
	for i, h := range hangs {
		words := make([]string, 12)
		for w := range words {
			words[w] = fmt.Sprintf("l%d-w%02d", i, w)
		}
		rows[i] = h.prefix + strings.Join(words, " ")
	}
	return state.Item{
		Kind: state.KindTool, ID: "tool-hang", Tool: "bash",
		Args: map[string]any{"cmd": "go test ./..."}, Status: state.ToolOK,
		Summary: strings.Join(rows, "\n"),
	}
}

// resultBody is where a row's own text starts and what it says. The pad and the elbow
// are layout — the pad carries no style key and the elbow carries the marker's — so the
// row's own text is everything from the first span holding key onwards, and the width of
// what stands in front of it is the column a hanging indent is measured at.
func resultBody(l Line, key string) (col int, text string) {
	for i, sp := range l {
		if sp.Style != key {
			col += ansi.StringWidth(sp.Text)
			continue
		}
		var b strings.Builder
		for _, sp := range l[i:] {
			b.WriteString(sp.Text)
		}
		return col, b.String()
	}
	return -1, ""
}

// TestAWrappedResultLineHangsUnderItsOwnIndent is the rule, and it is about being read
// rather than about looking tidy. A continuation drawn at the result's left edge is drawn
// where a `--- FAIL:` header sits, so the tail of an indented detail arrives looking like
// a heading: the reader is told about a failure that is really the second half of a
// sentence, and one subtest ends up drawn at two depths.
//
// The indent is the line's own, which is the other half of it and why a line at column
// zero is in the table — nothing is indented here that did not indent itself. The widths
// are ones where no word has to be split, so every row still begins with a word that
// names which line it continues and how far into it.
func TestAWrappedResultLineHangsUnderItsOwnIndent(t *testing.T) {
	g := DefaultGlyphs()
	it := hangingResult()
	ew := 2 + ansi.StringWidth(g.Get("tool.result"))
	for _, width := range []int{34, 48, 72} {
		conts := 0
		for i, l := range resultRows(it, width, g) {
			col, text := resultBody(l, "tool.result.text")
			if col < 0 {
				t.Fatalf("width %d: row %d carries no result text: %q", width, i, l.Text())
			}
			var k, w int
			if n, err := fmt.Sscanf(text, "l%d-w%d", &k, &w); n != 2 || err != nil {
				t.Fatalf("width %d: row %d begins %q, which names no line", width, i, text)
			}
			if want := ew + hangs[k].col; col != want {
				t.Errorf("width %d: row %d starts at column %d, want %d: %q",
					width, i, col, want, l.Text())
			}
			if w > 0 {
				conts++
			}
		}
		if conts == 0 {
			t.Fatalf("width %d: nothing wrapped, so this test proves nothing", width)
		}
	}
}

// TestATrailingNewlineInAResultIsATerminator. Splitting the result into its own lines is
// what gives each one its own indent to hang under, and a string that ends in a break
// splits into a last piece that is empty — a blank row under the result the recording
// never wrote. tokenize drops a trailing break for the same reason; the split has to too.
func TestATrailingNewlineInAResultIsATerminator(t *testing.T) {
	g := DefaultGlyphs()
	it := hangingResult()
	want := plain(resultRows(it, 48, g))
	it.Summary += "\n"
	if got := plain(resultRows(it, 48, g)); got != want {
		t.Errorf("a result ending in a newline draws:\n%s\nwant:\n%s", got, want)
	}
}

// TestAnIndentWithNoRoomForAWordGivesWay is the boundary the hang has to respect, and it
// is the wrapper's own rule one layer down: WrapSpans drops an indent that leaves no room
// for the word after it, and listItem's marker gives way to its item for the same reason.
// Kept where it does not fit, a hang is worse than dropped — the body would be wrapped
// into whatever columns were left over, and a detail indented eight columns in a
// thirteen-column pane would come back a syllable to a row.
//
// So the pane is built to the column: room for the indent and the first word exactly, and
// then one column less. The two are told apart by where the row's text is measured from,
// because a hang is padding the layout added and carries no style, while an indent the
// wrapper kept is text the command wrote and carries the result's own key.
func TestAnIndentWithNoRoomForAWordGivesWay(t *testing.T) {
	g := DefaultGlyphs()
	const indent, word = 8, "detail" // six columns, and every word of the line the same
	it := state.Item{
		Kind: state.KindTool, ID: "tool-edge", Tool: "bash",
		Args: map[string]any{"cmd": "go test ./..."}, Status: state.ToolOK,
		Summary: strings.Repeat(" ", indent) + strings.TrimPrefix(strings.Repeat(" "+word, 8), " "),
	}
	ew := 2 + ansi.StringWidth(g.Get("tool.result"))
	for _, tc := range []struct {
		what  string
		width int
		col   int
	}{
		{"the first word fits beside the indent", ew + indent + ansi.StringWidth(word), ew + indent},
		{"the pane is one column short of it", ew + indent + ansi.StringWidth(word) - 1, ew},
	} {
		rows := resultRows(it, tc.width, g)
		if len(rows) < 2 {
			t.Fatalf("%s: width %d drew %d rows, so nothing continued", tc.what, tc.width, len(rows))
		}
		for i, l := range rows {
			if col, text := resultBody(l, "tool.result.text"); col != tc.col {
				t.Errorf("%s: width %d row %d begins at column %d, want %d: %q",
					tc.what, tc.width, i, col, tc.col, text)
			}
		}
	}
}

// TestABlankLineInAResultStaysABlankRow. A line of nothing but indentation is a line the
// command wrote, and a transcript that swallowed it would close up the gap `go test` puts
// between one package's report and the next. It is also the one line with nothing to hang:
// the body is empty, so it is left to wrap itself and comes back as the blank row it
// always was, rather than as a row made of padding or as no row at all.
func TestABlankLineInAResultStaysABlankRow(t *testing.T) {
	g := DefaultGlyphs()
	it := state.Item{
		Kind: state.KindTool, ID: "tool-blank", Tool: "bash",
		Args: map[string]any{"cmd": "go test ./..."}, Status: state.ToolOK,
		Summary: "FAIL\tarxi.local/sim/internal/ui\t0.412s\n    \nok  \tarxi.local/sim/internal/app\t0.004s",
	}
	for width := 8; width <= 64; width++ {
		rows := resultRows(it, width, g)
		blanks := 0
		for _, l := range rows {
			if l.Text() == "" {
				blanks++
			}
		}
		if blanks != 1 {
			t.Errorf("width %d: one blank line drew %d blank rows:\n%s", width, blanks, plain(rows))
		}
	}
}

// TestAHangingResultFitsEveryWidth is what every row owes the pane whatever its width,
// held against a result that is indented — which is a case no other test here has, since
// longResult's lines all begin at column zero and never reach the hang at all. The
// trailing blank is the one to watch: a hang is made of spaces, and a row that came out as
// nothing but its own indentation would end in every one of them.
func TestAHangingResultFitsEveryWidth(t *testing.T) {
	g := DefaultGlyphs()
	it := hangingResult()
	want := strings.Join(strings.Fields(it.Summary), "")
	drew := false
	for width := 1; width <= 96; width++ {
		rows := resultRows(it, width, g)
		if len(rows) == 0 {
			continue // narrower than the elbow, where resultLines draws nothing at all
		}
		drew = true
		var b strings.Builder
		for i, l := range rows {
			if l.Width() > width {
				t.Fatalf("width %d: row %d is %d columns: %q", width, i, l.Width(), l.Text())
			}
			if txt := l.Text(); strings.HasSuffix(txt, " ") {
				t.Fatalf("width %d: row %d ends in a bare space: %q", width, i, txt)
			}
			for _, sp := range l {
				if sp.Style == "tool.result.text" {
					b.WriteString(sp.Text)
				}
			}
		}
		if got := strings.Join(strings.Fields(b.String()), ""); got != want {
			t.Fatalf("width %d holds %q, want %q", width, got, want)
		}
	}
	if !drew {
		t.Fatal("no width drew the result, so this test proves nothing")
	}
}

// promptTexts are the prompts a band has to survive: one word, one that wraps several
// times, one of two-column runes, and one where the whole line is one word too long for
// any row, which is where a wrap that cannot break has to overflow rather than lose it.
var promptTexts = []string{
	"go",
	"run the tests and tell me which ones fail",
	"日本語のテキストを入力する、それも折り返して",
	"a prompt long enough to wrap on a narrow terminal, twice over and then some more",
	"ejecuta-el-comando-mas-largo-del-mundo-sin-un-solo-espacio-en-todo-el-nombre",
}

func promptRows(text string, width int, g Glyphs) []Line {
	return ItemBlock{It: state.Item{Kind: state.KindPrompt, ID: "prompt-1", Text: text}}.Render(width, g)
}

// TestAPromptIsBandedToTheRightEdge is the band's whole contract, and it is a
// measurement because a wash is invisible in a screenshot when it is one column short:
// the reader sees a ragged right edge and blames their terminal. Every row of a prompt
// runs the full width, every span of it carries the band — the marker and the padding
// included, or the rectangle has a notch cut out of its left end and a hole at its right
// — and no row is wider than the pane, which would wrap and cost the frame a row it
// believes it owns.
func TestAPromptIsBandedToTheRightEdge(t *testing.T) {
	g := DefaultGlyphs()
	drew := false
	for _, text := range promptTexts {
		for _, w := range boxWidths {
			rows := promptRows(text, w, g)
			for i, l := range rows {
				drew = true
				if l.Width() != w {
					t.Errorf("width %d %q: row %d is %d columns: %q", w, text, i, l.Width(), l.Text())
				}
				for j, sp := range l {
					if sp.Fill != "prompt.band" {
						t.Errorf("width %d %q: row %d span %d %q carries fill %q",
							w, text, i, j, sp.Text, sp.Fill)
					}
				}
			}
		}
	}
	if !drew {
		t.Fatal("no width drew a prompt at all, so this test proves nothing")
	}
}

// TestABandedPromptKeepsEveryRuneOfItsText is the rule the padding could break. The band
// is made of trailing spaces, which is exactly what every other row in the transcript has
// stripped from it, so the risk is a row padded to the width and then trimmed back — or
// worse, text cut to make room for a pad that was measured before the text was placed.
// Whatever the width, the prompt on screen is the prompt that was sent.
//
// The comparison drops every space on both sides, because a wrap legitimately eats the one
// at the break and putting it back would be this test guessing where the breaks were. What
// is left is the property that matters: the same runes, in the same order, all of them. The
// narrowest widths are excluded for the reason the input box has a threshold — the wrap cuts
// rather than splits, so an interior one column wide cannot place a two-column grapheme and
// places none of the line. That is what the transcript has always done there, band or no band.
func TestABandedPromptKeepsEveryRuneOfItsText(t *testing.T) {
	g := DefaultGlyphs()
	marker := ansi.StringWidth(g.Get("prompt.marker"))
	for _, text := range promptTexts {
		for _, w := range boxWidths {
			if w-marker < 2 {
				continue
			}
			var b strings.Builder
			for _, l := range promptRows(text, w, g) {
				for _, sp := range l {
					if sp.Style == "prompt.text" {
						b.WriteString(sp.Text)
					}
				}
			}
			got := strings.Join(strings.Fields(b.String()), "")
			want := strings.Join(strings.Fields(text), "")
			if got != want {
				t.Errorf("width %d: the band holds %q, want %q", w, got, want)
			}
		}
	}
}

// TestABandedPromptKeepsItsOwnColours is why the band is a Fill and not a Style. The two
// are merged at emit — Style over Fill — so a bold word inside a prompt keeps its
// attribute and gains the background, rather than the wash flattening the line into one
// colour. Held against the resolved style rather than the key, because the whole
// mechanism is that the two keys survive as far as the emitter.
func TestABandedPromptKeepsItsOwnColours(t *testing.T) {
	g, th := DefaultGlyphs(), DefaultTheme()
	band := th.Resolve("prompt.band")
	if band.BG.Kind == ColorNone {
		t.Fatal("the default prompt.band has no background, so this test proves nothing")
	}
	var strong int
	for _, l := range promptRows("run **every** test now", 40, g) {
		for _, sp := range l {
			st := th.Resolve(sp.Style).Over(th.Resolve(sp.Fill))
			if st.BG != band.BG {
				t.Errorf("span %q lost the band: %+v", sp.Text, st)
			}
			if sp.Style == "md.strong" {
				strong++
				if st.Attrs&AttrBold == 0 {
					t.Errorf("the bold word %q came out unbold on the band", sp.Text)
				}
			}
		}
	}
	if strong == 0 {
		t.Fatal("no span was bold, so the inline markdown never reached the band")
	}
}

// TestAFinishedThoughtHasNoMarker is a one-line rule with a reason: the summary is a
// sentence, and a glyph in front of a sentence makes it an entry in a list. The indent
// stays — the transcript's left edge is two columns everywhere and the streaming text
// hangs under them — so what is asserted is the pair: no glyph, and the two columns it
// used to occupy still there.
func TestAFinishedThoughtHasNoMarker(t *testing.T) {
	g := DefaultGlyphs()
	it := state.Item{Kind: state.KindThinking, ID: "think-1", EndedAt: 1500 * time.Millisecond, Effort: "high"}
	rows := ItemBlock{It: it}.Render(60, g)
	if len(rows) != 1 {
		t.Fatalf("a finished thought drew %d rows: %s", len(rows), plain(rows))
	}
	got := rows[0].Text()
	if !strings.HasPrefix(got, "  Thought for ") {
		t.Errorf("a finished thought reads %q, want two columns of indent and the sentence", got)
	}
	for _, r := range got {
		if r > 0x7e {
			t.Errorf("a finished thought carries the non-ascii %q: %q", r, got)
			break
		}
	}
}
