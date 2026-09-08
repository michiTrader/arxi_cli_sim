package ui

import (
	"sort"
	"testing"

	"arxi.local/sim/internal/state"
)

// The registry only means something if a test enforces it. Two directions matter,
// and they fail for different reasons: a key the renderer uses but nobody declared
// is invisible to the user configuring the interface, and a key we declare but
// never draw is a promise in the documentation that the code does not keep.

// notYetDrawn is the honest list of declared keys that no frame produces yet, with
// the milestone that will. It is not an excuse: the test below fails if a key on
// this list starts being used, so the list cannot rot into a permanent exemption.
//
// It is empty, and that is the interesting state: every key the theme declares is on
// the screen somewhere, so `theme keys` is a list of things a user can actually change
// rather than a mix of those and things that are still coming. Adding a key with a
// milestone note is still the right move when the code that draws it is a window away —
// what the empty map means is that the tests are now the only reason to trust that, and
// they are.
var notYetDrawn = map[string]string{}

// drawnKeys renders everything the simulator can currently draw and reports which
// style keys came out, the way emit would resolve them.
func drawnKeys(t *testing.T) (used, unknown map[string]bool) {
	t.Helper()
	th := DefaultTheme()
	th.Track = true
	used, unknown = map[string]bool{}, map[string]bool{}

	// Every prefix of every named recording, at two widths. The prefixes are the point:
	// a key can be on the screen for one step and gone by the next — tool.marker.pending
	// exists only while a call is open — so folding just the finished run would report it
	// as never drawn. 07 is here because it is the only recording whose tool calls fail,
	// and a failing call is the same glyph as a passing one with a different style key.
	// 09 is here because it is the only one whose result is long enough to be elided. 10
	// is here for emphasis, a link, its url and a nested quote's bars, which used to be
	// proven by handing a string to RenderMarkdown — that says the markdown renderer can
	// emit a key, not that a transcript ever shows it.
	for _, path := range []string{firstConversation, toolFails, longOutput, markdownProse} {
		_, sc := play(t, path, 0)
		for _, w := range []int{40, 72} {
			for n := 0; n <= len(sc.Steps); n++ {
				st, _ := play(t, path, n)
				r := NewRenderer()
				in := NewInput()
				if n%2 == 0 {
					in.SetText("go test ./internal/...")
				}
				f := render(r, st, in, Viewport{Width: w, Height: 40})
				for _, l := range append(append([]Line{}, f.Committed...), f.Live...) {
					for _, sp := range l {
						// Fill is a style key like any other: emit resolves it and merges
						// it under Style. A test that read only Style would report a diff
						// band as undrawn while it was on the screen.
						for _, key := range [2]string{sp.Style, sp.Fill} {
							if key == "" {
								continue
							}
							th.Resolve(key)
							if Declared(key) {
								used[key] = true
							} else {
								unknown[key] = true
							}
						}
					}
				}
			}
		}
	}
	// Markdown constructs the scenario happens not to contain are still part of
	// the interface, so they are drawn here rather than left unproven.
	collect := func(lines []Line) {
		for _, l := range lines {
			for _, sp := range l {
				if sp.Style != "" {
					used[sp.Style] = true
				}
				if sp.Fill != "" {
					used[sp.Fill] = true
				}
			}
		}
	}
	g := DefaultGlyphs()
	collect(RenderMarkdown("- a bullet\n- another one\n", 72, g))
	// Both of a table's forms, because they do not draw the same keys: the grid has a
	// frame and the narrow form deliberately has none, so playing only one of them would
	// leave a declared key unproven or an undeclared one unnoticed. The corpus does have
	// tables — 02 and 06 — but neither of those is a recording drawnKeys plays.
	collect(RenderMarkdown(sampleTable, 72, g))
	collect(RenderMarkdown(sampleTable, 20, g))
	collect((NoticeWidget{Text: "budget at 80%", Warn: true}).Render(72, 0, g))
	collect(BlockFor(editWithDiff()).Render(72, g))
	// The status row, which no recording draws: the app installs it, and the renderer this
	// test drives installs ChromeFor's widgets only. It is built from a literal rather than
	// from a played step because it is the one widget whose keys depend on which of its
	// segments fit, and a literal carries every field at once, which no single step of any
	// recording does. 120 columns is wide enough for all of them, which is what proves
	// status.text and status.dim; 24 exercises the cut, which is what proves the row
	// survives losing them. 72 is deliberately not here — the full segment list is 74
	// columns, so a reworded verb would silently move which keys this test covers.
	spinning := &state.State{
		Actor: "builder", Model: "claude-sonnet-4", Turn: 3,
		SpentUSD: 0.08, BudgetUSD: 0.5, TokensIn: 12400, TokensOut: 3100, Active: true,
		Members: []*state.Member{
			{Name: "scout", Busy: true},
			{Name: "builder", Blocked: &state.Blocked{On: "approval"}},
			{Name: "critic", Error: "review failed"},
			{Name: "mender"},
		},
	}
	for _, w := range []int{120, 24} {
		collect((StatusWidget{St: spinning, Phase: 2}).Render(w, 0, g))
	}
	// A titled input, for the same reason and with a sharper edge: the border's word is
	// empty by default now, so the sweep above — which builds its editors with NewInput —
	// draws every other key of the box and never this one. The key is not undrawn, it is
	// unasked-for: `-title` and Config.InputTitle reach it, and a config file will. Two
	// widths because a title that does not fit is dropped rather than truncated, and a
	// single width would make this collect a coin toss on the wording of a default.
	for _, w := range []int{72, 40} {
		in := NewInput()
		in.Title = "prompt"
		rows, _ := in.Render(w, g)
		collect(rows)
	}
	// The two bands of light, which nothing above can reach: a shimmer's zero value is off, so
	// every editor and every status row built anywhere else in this function draws the frame
	// they drew before the animation existed. Only a caller that arms one gets one, and in the
	// program that caller is the app.
	//
	// They are collected as extra rows rather than by arming the widgets above, because a lit
	// span keeps its own key in Fill and the point of the sweeps above is the unlit frame. The
	// phase is chosen so that the whole band, both edges and every rung of the ladder, is on the
	// screen at once — 5 of 40 puts it at columns 24..40 of a 72-column box and at 1..4 of the
	// seven-letter verb, and a band with nothing clipped off it is the only frame that draws its
	// outermost rung and proves a key survives being cut rather than merely applied to a span.
	//
	// It is arithmetic and not a guess, which is why it moved when the sweep did: the band is
	// Width wide and its right edge is at (w+Width)*(t+1)/(Travel+1), so on 72 columns it is
	// wholly inside the row only for t in 2..9, and on the verb — where Band halves a band it
	// cannot fit in — only for t in 3..8 with both rungs showing from t=2. Five is in both.
	shining := NewInput()
	shining.Shine = Shimmer{Style: InputShine, Phase: 5}
	litRows, _ := shining.Render(72, g)
	collect(litRows)
	collect((StatusWidget{
		St: spinning, Phase: 2,
		Shine: Shimmer{Style: StatusShine, Phase: 5},
	}).Render(120, 0, g))
	// The scrollbar, which the sweeps above cannot reach for two reasons at once: the viewport
	// they render at holds no side column, and even one that did would draw no bar, because a
	// recording played from the start is a transcript that fits. So it is built from a literal
	// with rows both above and below the window — the only arrangement in which the track and
	// the thumb are on the screen together, and therefore the only one that proves both keys.
	//
	// Then the same bar again with the pointer on it, which is a third key and cannot come out of
	// the first call: held is a style and not a glyph, so the only way to see it is to hold it.
	collect((ScrollbarWidget{Scroll: Scroll{Above: 40, Rows: 12, Below: 90}}).Render(SideCols, 12, g))
	collect((ScrollbarWidget{Scroll: Scroll{Above: 40, Rows: 12, Below: 90}, Held: true}).Render(SideCols, 12, g))
	// The overlay, which nothing above draws: the renderer composites it only when
	// Renderer.Overlay is set, and the sweeps above leave it nil. Build one with a
	// highlighted row so every overlay key is proven.
	collect(Composite(
		[]Line{
			{pad(72)}, {pad(72)}, {pad(72)}, {pad(72)}, {pad(72)},
		},
		&Overlay{
			Title: "Effort",
			Width: 40,
			Body: []Line{
				{{Text: "normal text", Style: "overlay.text"}},
				{{Text: "selected", Style: "overlay.highlight"}},
			},
		},
		72, g,
	))
	// Team and Tasks full views live in internal/app, so prove their public style
	// vocabularies with the same literal-span boundary used for the effort widget below.
	collect([]Line{
		{{Text: "scout", Style: "team.name"}},
		{{Text: "role lead · model sonnet", Style: "team.meta"}},
		{{Text: "working", Style: "team.state"}},
		{{Text: "remedy: arxi inbox approve inbox-1", Style: "team.remedy"}},
		{{Text: "Tasks 1/3", Style: "tasks.summary"}},
		{{Text: "/tasks", Style: "tasks.action"}},
		{{Text: "Patch cache", Style: "tasks.title"}},
		{{Text: "owner builder", Style: "tasks.meta"}},
		{{Text: "Invalidate the stale entry", Style: "tasks.detail"}},
		{{Text: "pending", Style: "tasks.pending"}},
		{{Text: "active", Style: "tasks.active"}},
		{{Text: "completed", Style: "tasks.completed"}},
	})
	// The effort slider lives in internal/app and renders via EffortWidget, which
	// this package cannot import. Its keys are proven by collecting literal spans
	// that name every declared effort.* key.
	collect([]Line{
		{{Text: "Faster", Style: "effort.label"}, {Text: "─", Style: "effort.track"}, {Text: "●", Style: "effort.marker"}},
		{{Text: "low", Style: "effort.level"}, {Text: "high", Style: "effort.selected"}},
		{{Text: "m", Style: "effort.fill.medium"}, {Text: "h", Style: "effort.fill.high"}, {Text: "x", Style: "effort.fill.xhigh"}},
		{{Text: "Balanced thinking.", Style: "effort.desc"}},
		{{Text: "move", Style: "effort.hint"}},
		{{Text: "r", Style: "effort.rainbow.r"}, {Text: "o", Style: "effort.rainbow.ro"}, {Text: "o", Style: "effort.rainbow.o"}, {Text: "y", Style: "effort.rainbow.oy"}, {Text: "y", Style: "effort.rainbow.y"}, {Text: "g", Style: "effort.rainbow.yg"}, {Text: "g", Style: "effort.rainbow.g"}, {Text: "b", Style: "effort.rainbow.gb"}, {Text: "b", Style: "effort.rainbow.b"}, {Text: "p", Style: "effort.rainbow.bp"}, {Text: "p", Style: "effort.rainbow.p"}, {Text: "r", Style: "effort.rainbow.pr"}},
		{{Text: "d", Style: "effort.ultra.deep"}, {Text: "m", Style: "effort.ultra.deepMid"}, {Text: "m", Style: "effort.ultra.mid"}, {Text: "b", Style: "effort.ultra.midBright"}, {Text: "b", Style: "effort.ultra.bright"}},
	})
	return used, unknown
}

// editWithDiff is an edit carrying every row a diff can hold: the three operations,
// two hunks so the gap marker is drawn between them, and code that reaches all six
// of the highlighter's classes. 07 draws diffs now, but all three of its hunks stand
// alone and none of their rows is a comment or a builtin, so the gap marker,
// code.comment and code.func are still proven here and nowhere else.
func editWithDiff() state.Item {
	return state.Item{
		Kind: state.KindTool, ID: "tool-diff", Tool: "update",
		Args: map[string]any{"path": "cmd/arxi/runshow.go"}, Status: state.ToolOK,
		Diff: &state.Diff{
			Path: "cmd/arxi/runshow.go", Lang: "go", Added: 4, Removed: 1,
			Hunks: []state.DiffHunk{{Rows: []state.DiffRow{
				{Op: state.DiffContext, Old: 140, New: 140, Text: "\tfor _, l := range st.Locks {"},
				{Op: state.DiffRemoved, Old: 141, Text: "\t\tlocks = append(locks, map[string]any{\"key\": l.Key})"},
				{Op: state.DiffAdded, New: 141, Text: "\t\tlk := map[string]any{\"key\": l.Key}"},
				{Op: state.DiffAdded, New: 142, Text: "\t\t// Omitted rather than \"\" when there is no expiry."},
				{Op: state.DiffAdded, New: 143, Text: "\t\tlk[\"ttl\"] = 300"},
			}}, {Rows: []state.DiffRow{
				{Op: state.DiffAdded, New: 150, Text: "\t\tlocks = append(locks, lk)"},
				{Op: state.DiffContext, Old: 151, New: 151, Text: "\t}"},
			}}},
		},
	}
}

// TestNoUndeclaredStyleKeyIsDrawn fails on a key the renderer invented. Such a key
// silently resolves to no styling, which is the worst possible outcome: it looks
// like a theme bug and it cannot be fixed from a theme.
func TestNoUndeclaredStyleKeyIsDrawn(t *testing.T) {
	_, unknown := drawnKeys(t)
	if len(unknown) == 0 {
		return
	}
	t.Fatalf("the renderer drew style keys that ui.Keys does not declare: %v", sortedSet(unknown))
}

// TestEveryDeclaredKeyIsDrawn keeps `theme keys` honest in the other direction.
func TestEveryDeclaredKeyIsDrawn(t *testing.T) {
	used, _ := drawnKeys(t)
	for _, d := range Keys {
		_, exempt := notYetDrawn[d.Key]
		switch {
		case used[d.Key] && exempt:
			t.Errorf("%s is drawn now: remove it from notYetDrawn", d.Key)
		case !used[d.Key] && !exempt:
			t.Errorf("%s is declared and documented as %q but nothing draws it", d.Key, d.Doc)
		}
	}
}

// TestEveryDeclaredKeyIsDocumented is cheap and catches the copy-paste that adds a
// key with an empty doc, which would print a blank line in `theme keys`.
func TestEveryDeclaredKeyIsDocumented(t *testing.T) {
	for _, d := range Keys {
		if d.Doc == "" {
			t.Errorf("style key %s has no documentation", d.Key)
		}
	}
	for _, d := range GlyphKeys {
		if d.Doc == "" {
			t.Errorf("glyph %s has no documentation", d.Key)
		}
		if d.Default == "" || d.Fallback == "" {
			t.Errorf("glyph %s must have both a default and an ascii fallback", d.Key)
		}
	}
	for _, d := range SlotKeys {
		if d.Doc == "" {
			t.Errorf("slot %s has no documentation", d.Slot)
		}
	}
}

func TestTasksGlyphRegistryIsUsed(t *testing.T) {
	g := DefaultGlyphs()
	g.Track = true
	for _, key := range []string{"tasks.pending", "tasks.active", "tasks.completed"} {
		if g.Get(key) == "" {
			t.Errorf("%s resolves empty", key)
		}
	}
	if got := g.Used(); len(got) != 3 {
		t.Fatalf("used Tasks glyphs = %v", got)
	}
	if got := g.Unknown(); len(got) != 0 {
		t.Fatalf("unknown Tasks glyphs = %v", got)
	}
}

// TestThemeRejectsUndeclaredKey is the load-time half of the contract: a typo in a
// user's theme file is an error at load, not a colour that silently never appears.
func TestThemeRejectsUndeclaredKey(t *testing.T) {
	if _, err := NewTheme("typo", map[string]Style{"prompt.txt": {}}); err == nil {
		t.Fatal("a theme naming prompt.txt was accepted")
	}
	if _, err := NewGlyphs(map[string]string{"promt.marker": ">"}, false); err == nil {
		t.Fatal("a glyph override naming promt.marker was accepted")
	}
}

// TestAsciiFallbackIsNarrow matters on a terminal that lies about its font: every
// fallback must be plain ascii, because the whole point is that it cannot be
// mismeasured.
func TestAsciiFallbackIsNarrow(t *testing.T) {
	g, err := NewGlyphs(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range GlyphKeys {
		got := g.Get(d.Key)
		for _, r := range got {
			if r > 0x7e || r < 0x20 {
				t.Errorf("glyph %s has a non-ascii fallback %q", d.Key, got)
			}
		}
	}
}

// TestDefaultThemeWithKeepsWhatItIsNotGiven is the promise a config's [styles] section rests
// on: naming one key recolours one key. NewTheme takes a whole table, so the obvious wiring —
// hand it the dozen lines the file holds — would silently unstyle the other sixty, and a
// reader who changed a colour would lose the interface.
func TestDefaultThemeWithKeepsWhatItIsNotGiven(t *testing.T) {
	th, err := DefaultThemeWith(map[string]Style{"md.code": {FG: Idx(Red)}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := th.Resolve("md.code"), (Style{FG: Idx(Red)}); got != want {
		t.Errorf("md.code = %+v, want %+v", got, want)
	}
	def := DefaultTheme()
	for _, d := range Keys {
		if d.Key == "md.code" {
			continue
		}
		if got, want := th.Resolve(d.Key), def.Resolve(d.Key); got != want {
			t.Errorf("%s = %+v, want the shipped %+v", d.Key, got, want)
		}
	}
	// The nil case is the one main takes when there is no config file at all, and it has to
	// be the shipped look rather than an empty one.
	th, err = DefaultThemeWith(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range Keys {
		if got, want := th.Resolve(d.Key), def.Resolve(d.Key); got != want {
			t.Errorf("with no overrides, %s = %+v, want %+v", d.Key, got, want)
		}
	}
}

// TestDefaultThemeWithReplacesRatherThanLayers states the other half of the semantics, and it
// is the half a reader can be surprised by. An override is the whole answer for its key: it
// does not sit Over the shipped style, because Over unions attributes and a default's dim
// would then be unremovable — thinking.text is dim and italic, and a config asking for a
// plain red would get a dim italic red with no way to say otherwise.
func TestDefaultThemeWithReplacesRatherThanLayers(t *testing.T) {
	if got := DefaultTheme().Resolve("thinking.text"); got.Attrs == 0 {
		t.Fatal("thinking.text carries no attributes: this test proves nothing now")
	}
	th, err := DefaultThemeWith(map[string]Style{
		"thinking.text": {FG: Idx(Red)},
		// "" is the spelling ui.Keys documents for taking the wash off a prompt, and it works
		// for the same reason: what the file writes is the whole style, and the whole style
		// here is nothing at all.
		"prompt.band": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := th.Resolve("thinking.text"), (Style{FG: Idx(Red)}); got != want {
		t.Errorf("thinking.text = %+v, want exactly %+v", got, want)
	}
	if got := th.Resolve("prompt.band"); !got.IsZero() {
		t.Errorf(`prompt.band = %+v after being set to "", want the zero style`, got)
	}
	// A shipped theme built afterwards still has its band. DefaultTheme building its table
	// fresh on every call is what makes that true, and a package-level var would be a config
	// override leaking into every theme in the process.
	if DefaultTheme().Resolve("prompt.band").IsZero() {
		t.Error("emptying prompt.band in one theme emptied the shipped one")
	}
}

// TestDefaultThemeWithRejectsUndeclaredKey is the line-numbered error's last line of defence:
// internal/config checks the same membership while it has a line to name, and this is what
// catches a key that reaches the constructor by any other road.
func TestDefaultThemeWithRejectsUndeclaredKey(t *testing.T) {
	if _, err := DefaultThemeWith(map[string]Style{"prompt.txt": {FG: Idx(Red)}}); err == nil {
		t.Fatal("an override naming prompt.txt was accepted")
	}
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
