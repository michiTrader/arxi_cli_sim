package ui

import (
	"strings"
	"testing"
)

// The inline markdown an assistant emits, tested where the corpus cannot reach it. A
// recording proves the whole frame draws; these tests ask the smaller and harder
// question of which characters are markers and which are text, and that question is
// where a markdown renderer earns its keep: every rule here exists because the naive
// reading of the marker turns something an assistant wrote into something it did not.

// sampleProse is one document holding every block the renderer knows about except a
// table, and theme_test.go draws it to reach the five style keys the corpus leaves
// unproven. The link's url differs from its text deliberately: when they agree the url
// is suppressed, so a fixture whose link is bare would leave md.link.url undrawn and
// the declared-key test would be right to complain. The fenced code carries none of the
// inline markers, because the marker test below reads the whole fixture and a backtick
// or an asterisk inside the fence would be indistinguishable from one that leaked. The
// first list item is wrapped in the source, because the width sweep is the only test
// that asks what a continuation row does at a width narrower than the indent it wants.
const sampleProse = "# What changed\n" +
	"\n" +
	"The loader is *strict* now, so a **malformed line** is an error rather than a\n" +
	"warning. See [the event contract](https://arxi.dev/events) for the shapes it\n" +
	"accepts.\n" +
	"\n" +
	"> A recording is a fact about a run.\n" +
	"> > Which is why nothing rewrites one.\n" +
	"\n" +
	"1. Read the file with `LoadScenario`, which reports every malformed line\n" +
	"   at once.\n" +
	"2. Fold it into a state.\n" +
	"3. Render the state.\n" +
	"\n" +
	"```go\n" +
	"if err := LoadScenario(path); err != nil {\n" +
	"\treturn err\n" +
	"}\n" +
	"```\n"

// proseWords is the letters of each line of sampleProse, with the spaces taken out: one
// entry per block, in the order the document holds them. The sweep below asks for these
// rather than for the lines themselves, because at a narrow width the renderer is
// entitled to break a line anywhere it likes — and not entitled to lose a word.
var proseWords = []string{
	"Whatchanged",
	"Theloaderisstrictnow,soamalformedlineisanerrorratherthana",
	"warning.Seetheeventcontract(https://arxi.dev/events)fortheshapesit",
	"accepts.",
	"Arecordingisafactaboutarun.",
	"Whichiswhynothingrewritesone.",
	"ReadthefilewithLoadScenario,whichreportseverymalformedline",
	"atonce.",
	"Folditintoastate.",
	"Renderthestate.",
	"iferr:=LoadScenario(path);err!=nil{",
	"returnerr",
}

// styled is the text of every run drawn in one style, in the order it was drawn. A
// test that only looked at the characters would pass on a renderer that styled the
// markers and left the words plain.
//
// Adjacent spans of the style are joined, because the wrapper is a tokenizer: it
// re-emits every word and every gap between them as its own span, so an emphasised
// phrase reaches a Line as three spans that were one run in the source. Joining stops
// at the end of a row, so a run the width split is reported as the two rows it became.
func styled(lines []Line, style string) []string {
	var out []string
	for _, l := range lines {
		run, open := "", false
		for _, sp := range l {
			if sp.Style == style {
				run, open = run+sp.Text, true
				continue
			}
			if open {
				out = append(out, run)
				run, open = "", false
			}
		}
		if open {
			out = append(out, run)
		}
	}
	return out
}

func inline(s string) []Line { return mdLines(s, 72) }

// TestAnAsteriskIsUsuallyJustAnAsterisk is the table the emphasis rule exists for. An
// assistant writing about code writes asterisks it does not mean: a glob, a
// multiplication, a pointer, a Python signature. Italicising from the first one to the
// next swallows the words between them and, worse, deletes the asterisks themselves —
// so `rm *.go *_test.go` would come out as one italic word and a reader copying the
// command would run the wrong thing. The rule that buys all of these rows is that the
// emphasised text may not start or end with a space.
func TestAnAsteriskIsUsuallyJustAnAsterisk(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string // the runs that should come out italic, if any
	}{
		{"the loader is *strict* now", []string{"strict"}},
		{"it is *strict*", []string{"strict"}},
		{"*two* of *them*", []string{"two", "them"}},
		{"2 * 3 * 4 is 24", nil},
		{"rm *.go *_test.go", nil},
		{"./**/*.go finds them all", nil},
		{"*args and *kwargs", nil},
		{"the buffer is n * h*w bytes", nil},
		{"a lone * asterisk", nil},
		{"trailing asterisk *", nil},
	} {
		t.Run(tc.text, func(t *testing.T) {
			got := styled(inline(tc.text), "md.em")
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("%q italicised %q, want %q", tc.text, got, tc.want)
			}
			// Whatever was not a marker has to survive as text, which the styles alone do
			// not prove: a rule that rejected the run by eating it would pass the check above.
			if tc.want == nil && !strings.Contains(show(inline(tc.text)), tc.text) {
				t.Errorf("%q should have stayed literal, got %q", tc.text, show(inline(tc.text)))
			}
		})
	}
}

// TestStrongAndEmphasisStayApart: the two markers differ by one character and share
// their first, so the scan has to try the pair before the single. Read the other way
// round, "**bold**" is an italic asterisk wrapped around another one, and the theme's
// bold would be unreachable from prose.
func TestStrongAndEmphasisStayApart(t *testing.T) {
	lines := inline("a **bold** and an *italic* word")
	if got := styled(lines, "md.strong"); strings.Join(got, "|") != "bold" {
		t.Errorf("strong drew %q, want [bold]", got)
	}
	if got := styled(lines, "md.em"); strings.Join(got, "|") != "italic" {
		t.Errorf("emphasis drew %q, want [italic]", got)
	}
	if text := show(lines); strings.Contains(text, "*") {
		t.Errorf("the asterisks are markers, not text: %q", text)
	}
}

// TestAnUnterminatedMarkerStaysText is what a delta looks like halfway through. The
// text arrives in pieces and a marker's other half may still be in flight, so every
// case here is a real frame the transcript draws for a few milliseconds. Emphasising
// the rest of the paragraph while waiting makes the whole reply flicker as it streams;
// leaving the marker as text costs one character that a later frame corrects.
//
// The "**bold* " row is the one that needs the scan to step over both asterisks of an
// unterminated pair. Stepping over one leaves the second free to open an emphasis with
// whatever asterisk turns up next, which is how a half-arrived bold word becomes an
// italic one that never existed.
func TestAnUnterminatedMarkerStaysText(t *testing.T) {
	for _, s := range []string{
		"a **bold word",
		"an *italic word",
		"a `code span",
		"a [link](https://example.com",
		"a [link] without a target",
		"**bold* and then some",
	} {
		t.Run(s, func(t *testing.T) {
			lines := inline(s)
			if text := show(lines); text != s {
				t.Errorf("drew %q, want the text as written", text)
			}
			for _, style := range []string{"md.strong", "md.em", "md.code", "md.link"} {
				if got := styled(lines, style); len(got) > 0 {
					t.Errorf("%s drew %q from an unterminated marker", style, got)
				}
			}
		})
	}
}

// TestALinkShowsWhereItGoes. A terminal is not a browser: the only reliable way to
// give somebody a url is to write it out, so the text carries the link's style and the
// target follows it as an aside. Hiding the url behind the words, as a hyperlink
// escape would, means a reader on a terminal that ignores the escape sees a sentence
// with no way to reach what it points at.
func TestALinkShowsWhereItGoes(t *testing.T) {
	lines := inline("See [the contract](https://arxi.dev/events) for the shapes.")
	if got := styled(lines, "md.link"); strings.Join(got, "|") != "the contract" {
		t.Errorf("the link text is %q, want [the contract]", got)
	}
	if got := styled(lines, "md.link.url"); strings.Join(got, "|") != " (https://arxi.dev/events)" {
		t.Errorf("the url is %q, want it drawn after the text", got)
	}
	if text := show(lines); strings.ContainsAny(text, "[]") {
		t.Errorf("the brackets are markers, not text: %q", text)
	}
	// A url written as its own text is the common case in a changelog, and drawing it
	// twice is noise the reader has to check character by character to dismiss.
	bare := inline("See https://arxi.dev/events — or [https://arxi.dev/events](https://arxi.dev/events).")
	if got := styled(bare, "md.link.url"); len(got) > 0 {
		t.Errorf("a link to its own text should not repeat it, got %q", got)
	}
}

// TestABracketIsNotAlwaysALink guards the two things markdown puts in square brackets
// that are not links. A task list is what an assistant writes when it plans, and a
// footnote marker is what it writes when it cites; a renderer that read either as a
// link would delete the brackets and leave the reader a line that has lost its meaning.
func TestABracketIsNotAlwaysALink(t *testing.T) {
	for _, s := range []string{
		"- [ ] not started",
		"- [x] done",
		"as reported in [1] and [2]",
		"an empty target [text]()",
		"an empty text [](https://arxi.dev)",
	} {
		t.Run(s, func(t *testing.T) {
			lines := inline(s)
			if got := styled(lines, "md.link"); len(got) > 0 {
				t.Errorf("drew a link %q", got)
			}
			if text := show(lines); !strings.Contains(text, "[") {
				t.Errorf("the brackets are text here, got %q", text)
			}
		})
	}
}

// TestAQuoteCarriesItsBarOnEveryRow is the difference between a quote and a list. A
// list's marker introduces an item and the lines under it are indented; a quote's bar
// says the row is still somebody else's words, so it has to be redrawn on every row the
// quote wraps onto. Dropped after the first, the rest of the quote reads as the
// assistant's own prose, which in a transcript of an agent's reasoning is the one
// confusion worth spending two columns a line to avoid.
func TestAQuoteCarriesItsBarOnEveryRow(t *testing.T) {
	bar := DefaultGlyphs().Get("quote.bar")
	lines := mdLines("> "+strings.Repeat("quoted words ", 6), 30)
	if len(lines) < 3 {
		t.Fatalf("thirty columns should wrap this quote onto several rows, got:\n%s", show(lines))
	}
	for i, l := range lines {
		if got := styled([]Line{l}, "md.quote.bar"); len(got) != 1 || got[0] != bar {
			t.Errorf("row %d draws %q for its bar, want one %q:\n%s", i, got, bar, show(lines))
		}
		if body := styled([]Line{l}, "md.quote"); len(body) == 0 {
			t.Errorf("row %d has a bar and no words: %q", i, l.Text())
		}
	}
	// Nesting counts markers rather than characters, and one space after each marker
	// belongs to it: read the other way, "> >" is one level quoting the text "> ".
	nested := mdLines("> > twice removed\n", 40)
	for i, l := range nested {
		if got := styled([]Line{l}, "md.quote.bar"); len(got) != 1 || got[0] != bar+bar {
			t.Errorf("nested row %d draws %q, want the bar twice: %q", i, got, bar+bar)
		}
	}
	if got := styled(nested, "md.quote"); strings.Join(got, "|") != "twice removed" {
		t.Errorf("the quoted text is %q, want [twice removed]", got)
	}
	// A blank row inside a quote is a bar and nothing else. The trailing space of the
	// glyph has to go, because a row whose last cell is filled is how a terminal is
	// talked into wrapping a row the frame thought it owned.
	blank := mdLines("> one\n>\n> two\n", 40)
	if len(blank) != 3 {
		t.Fatalf("a quote with a blank row is three rows, got:\n%s", show(blank))
	}
	if blank[1].Text() != strings.TrimRight(DefaultGlyphs().Get("quote.bar"), " ") {
		t.Errorf("the blank row of a quote is %q, want a bare bar", blank[1].Text())
	}
}

// TestAnOrderedListKeepsTheAuthorsNumbers. The numbers are drawn as written rather
// than counted here, because this renderer is a state machine over a document that may
// still be arriving: a list that renumbered itself would have to remember what came
// before, and the first frame of "3." — arriving before its own list — would be wrong
// in a way the reader can see.
func TestAnOrderedListKeepsTheAuthorsNumbers(t *testing.T) {
	lines := mdLines("1. read the file\n2. fold it into a state\n7) then this\n", 72)
	if got := styled(lines, "md.bullet"); strings.Join(got, "|") != "1. |2. |7) " {
		t.Errorf("the markers are %q, want the numbers and delimiters as written", got)
	}
	if text := show(lines); !strings.Contains(text, "1. read the file") {
		t.Errorf("the item text follows its marker, got:\n%s", text)
	}
	// The second line of an item lines up with the first word, not with the number: a
	// continuation under the marker reads as a new item whose number went missing.
	wrapped := mdLines("10. "+strings.Repeat("word ", 12), 24)
	if len(wrapped) < 2 {
		t.Fatalf("twenty-four columns should wrap this item, got:\n%s", show(wrapped))
	}
	if !strings.HasPrefix(wrapped[0].Text(), "10. word") {
		t.Errorf("the first row is %q, want the marker then the text", wrapped[0].Text())
	}
	for i, l := range wrapped[1:] {
		if !strings.HasPrefix(l.Text(), "    word") {
			t.Errorf("continuation %d is %q, want it indented under the text", i, l.Text())
		}
	}
}

// TestAWrappedItemStaysOneItem is the shape an assistant emits whenever an item runs
// past the width it was writing to: the text arrives already broken, indented under the
// words it continues rather than under the marker. Read as a paragraph of its own, the
// second row loses that indent the moment the screen is narrower than the source was,
// and the item comes out as an item followed by a sentence that fell out of the list.
func TestAWrappedItemStaysOneItem(t *testing.T) {
	bullet := DefaultGlyphs().Get("bullet")
	pad := strings.Repeat(" ", Line{{Text: bullet}}.Width())
	for _, tc := range []struct {
		md   string
		want []string
	}{
		// The indent the author wrote is structure rather than text, so whatever it measures
		// the row is redrawn at the column the item's own words start at: an assistant that
		// counted two spaces under a three-column marker still lines up, and so does one
		// that counted four under a bullet.
		{"1. one\n   two\n", []string{"1. one", "   two"}},
		{"1. one\n  two\n", []string{"1. one", "   two"}},
		{"1. one\n     two\n", []string{"1. one", "   two"}},
		{"10. one\n  two\n", []string{"10. one", "    two"}},
		{"- one\n  two\n", []string{bullet + "one", pad + "two"}},
		{"- one\n    two\n", []string{bullet + "one", pad + "two"}},
		// Every row of a run of them, not only the first — and at the item's column rather
		// than the source's on every one of them, which the run above cannot show because
		// there the two happen to agree.
		{"1. one\n   two\n   three\n", []string{"1. one", "   two", "   three"}},
		{"1. one\n  two\n  three\n", []string{"1. one", "   two", "   three"}},
		// A nested item is an item: it brought its own marker, and reading it as the text of
		// the item above would delete that marker and the nesting with it.
		{"- one\n  - nested\n", []string{bullet + "one", pad + bullet + "nested"}},
		// The paragraph after a list is not part of the last item, with or without a blank
		// line between them. Reading an unindented row as a lazy continuation, which is what
		// CommonMark does with it, swallows the rest of the reply into the list.
		{"1. one\nprose\n", []string{"1. one", "prose"}},
		{"1. one\n\nprose\n", []string{"1. one", "", "prose"}},
		// A blank line closes the item, so the indented block an assistant writes after a
		// list — a stanza of output, a shell transcript — keeps the spaces it was given
		// instead of being pulled into an item three rows above it.
		{"1. one\n\n  indented\n", []string{"1. one", "", "  indented"}},
		// One space is not a continuation. In front of a sentence it is a typo far more
		// often than it is structure, and a typo that reindented the row would be a change
		// the reader has no way to account for.
		{"1. one\n two\n", []string{"1. one", " two"}},
		// An indented row with nothing open above it has nothing to continue.
		{"  indented prose\n", []string{"  indented prose"}},
	} {
		t.Run(tc.md, func(t *testing.T) {
			lines := mdLines(tc.md, 72)
			got := make([]string, len(lines))
			for i, l := range lines {
				got[i] = l.Text()
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("drew %q, want %q", got, tc.want)
			}
		})
	}
	// The marker is drawn once however many rows the item takes, which the text above does
	// not prove: a continuation that redrew the head would put a second "1. " on the screen
	// in exactly the columns the spaces occupy.
	if got := styled(mdLines("1. one\n   two\n   three\n", 72), "md.bullet"); strings.Join(got, "|") != "1. " {
		t.Errorf("the markers are %q, want only the one the author wrote", got)
	}
}

// TestANumberIsNotAlwaysAList is the other half of the rule, and the reason the space
// after the delimiter is required. An assistant writes version numbers and measurements
// in prose all day; read as list markers they would each start a new line with a
// hanging indent, and the sentence they belonged to would be shredded.
func TestANumberIsNotAlwaysAList(t *testing.T) {
	for _, s := range []string{
		"1.5x faster than the loader it replaces",
		"go 1.26 dropped it",
		"the run cost 2.40 in tokens",
		"see step 3.",
	} {
		t.Run(s, func(t *testing.T) {
			lines := mdLines(s, 72)
			if got := styled(lines, "md.bullet"); len(got) > 0 {
				t.Errorf("drew a list marker %q", got)
			}
			if text := show(lines); text != s {
				t.Errorf("drew %q, want the sentence as written", text)
			}
		})
	}
}

// TestProseNeverOverflowsAnyWidth is the property every frame rests on, asked of the
// whole fixture at every width a terminal could be and several no terminal is. A line
// wider than the screen wraps where the renderer did not choose to and every row
// counted after it is off by one; a line ending in a space fills the last cell, which
// does the same thing; a control character moves the cursor while scoring zero columns,
// so nothing downstream can see it at all. The narrow end matters most here, because
// that is where a bar, a marker, a gutter and a url all compete for a screen that
// cannot hold them.
//
// The second half of the sweep is the half the first cannot see: every structure that
// gives way to make room has the option of dropping the row instead, and a row that was
// never drawn is neither too wide nor ended in a space. So the words are counted too.
func TestProseNeverOverflowsAnyWidth(t *testing.T) {
	for w := 1; w <= 100; w++ {
		lines := mdLines(sampleProse, w)
		for i, l := range lines {
			text := l.Text()
			if l.Width() > w {
				t.Fatalf("width %d: line %d is %d columns: %q", w, i, l.Width(), text)
			}
			if strings.HasSuffix(text, " ") {
				t.Fatalf("width %d: line %d ends in a space: %q", w, i, text)
			}
			if strings.ContainsFunc(text, isControl) {
				t.Fatalf("width %d: line %d holds a control character: %q", w, i, text)
			}
		}
		// The glyphs come out trimmed, because a row whose only text was a space is a bare
		// bar by the time join has been over it, so the trailing space of the glyph cannot
		// be relied on to be there.
		letters := show(lines)
		g := DefaultGlyphs()
		for _, cut := range []string{
			strings.TrimRight(g.Get("quote.bar"), " "),
			strings.TrimRight(g.Get("code.gutter"), " "),
			" ", "\n",
		} {
			letters = strings.ReplaceAll(letters, cut, "")
		}
		for _, want := range proseWords {
			if !strings.Contains(letters, want) {
				t.Fatalf("width %d lost %q:\n%s", w, want, show(lines))
			}
		}
	}
}

// TestProseKeepsItsMarkersOutOfTheText is the fixture read end to end: whatever the
// blocks and runs turn out to be, none of the characters that asked for them should
// reach the screen. This is the check that catches a construct nobody thought to test —
// a marker left in the text is the visible half of a rule that did not fire.
func TestProseKeepsItsMarkersOutOfTheText(t *testing.T) {
	text := show(mdLines(sampleProse, 72))
	for _, marker := range []string{"#", "*", "[", "]", "`", ">"} {
		if strings.Contains(text, marker) {
			t.Errorf("%q survived into the text:\n%s", marker, text)
		}
	}
	for _, want := range []string{"What changed", "strict", "malformed line", "the event contract",
		"https://arxi.dev/events", "A recording is a fact about a run.", "LoadScenario"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q should be on screen:\n%s", want, text)
		}
	}
}
