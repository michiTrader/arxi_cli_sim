#!/usr/bin/env python3
# Mutation harness: break one thing at a time and confirm a test says so.
#
# The suite is green after every change, and green is not evidence — a test that asserts
# nothing passes too. This breaks the code on purpose, one edit at a time, and reports any
# mutation the suite let through. A SURVIVED line is a hole in the tests, not a bug in the
# code, and it is the only reliable way to find one before the terminal does.
#
# Each mutation is a literal find-and-replace that must match exactly once in a pristine
# file: an ambiguous anchor is reported as SKIP rather than guessed at. The file is restored
# in a finally, which covers a failing test run and a ctrl+c — but not a killed process, so a
# run torn down rather than interrupted can leave a mutant behind in the tree.
#
#   python3 tools/mut.py            every mutation
#   python3 tools/mut.py band       only those whose label contains "band"
#   python3 tools/mut.py --anchors  no tests: just which anchors no longer match, after a rename
import subprocess, sys, os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
# Every package with tests. internal/term is here for one line's sake: the byte that makes
# ctrl+enter a second line is decoded there and asserted there, and nothing in app or ui can
# see it — app builds its keys from names, so a decoder that folds 0x0a back into enter passes
# every test in the other three packages.
PKGS = ["./internal/ui", "./internal/app", "./internal/config", "./internal/term",
        "./internal/event", "./internal/state", "./internal/scenario"]
# A mutant is expected to fail, and one way to fail is to never finish: unbind ctrl+d and the
# test that presses keys until the app quits presses them forever. Go's own default is ten
# minutes, which the sweep would then spend on one line, so the runs below carry a limit of
# their own. It is a limit and not a budget — the whole untouched suite is about seven seconds,
# nearly all of it internal/ui's width sweep — and a mutation that trips it is still CAUGHT,
# because a panic is a non-zero exit like any other. Only the report reads "panic/build"
# instead of naming a test, since a timeout kills the binary before it can print a FAIL line.
GOTEST = ["go", "test"] + PKGS + ["-count=1", "-timeout", "90s"]

M = []


def mut(label, path, frm, to):
    """A mutation in internal/ui, which is where most of the drawing lives."""
    M.append((label, os.path.join(ROOT, "internal/ui", path), frm, to))


def at(label, path, frm, to):
    """A mutation anywhere else, by path from the repository root."""
    M.append((label, os.path.join(ROOT, path), frm, to))


# The input box: a border is arithmetic, and every off-by-one in it is invisible in a
# screenshot and load-bearing in a terminal.

mut("box threshold room>=1", "input.go",
    "boxed := width-hw-box >= 2", "boxed := width-hw-box >= 1")

mut("box pad one short", "input.go",
    "pad(inner-1-l.Width())", "pad(inner-2-l.Width())")

mut("cursor col misses the border space", "input.go",
    "cur.Col += vw + 1", "cur.Col += vw")

mut("cursor line ignores the top rule", "input.go",
    "\tcur.Line++\n\tcur.Col += vw + 1", "\tcur.Col += vw + 1")
mut("cursorAt answers before wrapping", "input.go", """		w := ansi.StringWidth(string(r))
		if col+w > room {
			line++
			col = 0
		}
		if i == in.cursor {
			return Cursor{Line: line, Col: col}
		}
		col += w""", """		if i == in.cursor {
			return Cursor{Line: line, Col: col}
		}
		w := ansi.StringWidth(string(r))
		if col+w > room {
			line++
			col = 0
		}
		col += w""")

mut("hrun drops the space remainder", "input.go", """	h := fill(g.Get("frame.h"), n)
	if w := ansi.StringWidth(h); w < n {
		h += strings.Repeat(" ", n-w)
	}""", """	h := fill(g.Get("frame.h"), n)""")

mut("title fits with no horizontal beside it", "input.go",
    "ansi.StringWidth(t)+2*lead <= mid", "ansi.StringWidth(t)+lead <= mid")

mut("title lead-in is one column, not one glyph", "input.go",
    'lead := max(1, ansi.StringWidth(g.Get("frame.h")))', "lead := 1")

# Multi-line input. A break is not a character in a row, it is where a row ends, and every
# mutation here is a plausible way to confuse the two: draw it, split the rows on something
# else, count it a step late, or leave the cursor in a column the row it closes does not own.
# The pair that decides whether a line is sent or continued is down here too, because a
# keymap where those two change places is a prompt nobody can either finish or send.

mut("multiline: the rows are split on something else", "input.go",
    r'strings.Split(string(in.runes), "\n")', r'strings.Split(string(in.runes), "\r")')

mut("multiline: the break is wrapped instead of ending a row", "input.go", r"""		var rows []Line
		for _, seg := range strings.Split(string(in.runes), "\n") {
			rows = append(rows, HardWrap(seg, "input.text", room, nil)...)
		}""", r"""		rows := HardWrap(string(in.runes), "input.text", room, nil)""")

mut("multiline: cursorAt does not count the breaks", "input.go", r"""		if r == '\n' {
			if i == in.cursor {
				return Cursor{Line: line, Col: col}
			}
			line, col = line+1, 0
			continue
		}
""", "")

mut("multiline: the break belongs to the row after it", "input.go", r"""		if r == '\n' {
			if i == in.cursor {
				return Cursor{Line: line, Col: col}
			}
			line, col = line+1, 0
			continue
		}""", r"""		if r == '\n' {
			line, col = line+1, 0
			if i == in.cursor {
				return Cursor{Line: line, Col: col}
			}
			continue
		}""")

mut("multiline: the unboxed cursor is not clamped", "input.go", """		if cur.Col >= width {
			cur.Col = width - 1
		}
""", "")

mut("multiline: the clamp lands one column past the last", "input.go",
    "cur.Col = width - 1", "cur.Col = width")

at("multiline: enter and shift+enter change places", "internal/app/keymap.go", """		"enter":       ActionSubmit,
		"shift+enter": ActionNewline,""", """		"enter":       ActionNewline,
		"shift+enter": ActionSubmit,""")

# Both keys or neither. They are one action and one line of the keymap apart, so a table that
# binds the one the test happens to try first passes while the other hand finds nothing.
at("multiline: ctrl+enter sends the line instead", "internal/app/keymap.go",
    '"ctrl+enter":  ActionNewline,', '"ctrl+enter":  ActionSubmit,')

at("multiline: the newline action types nothing", "internal/app/app.go",
    r'a.ed.Insert("\n")', 'a.ed.Insert("")')

at("multiline: a pasted break is flattened", "internal/app/app.go", r"""		case r == '\n':
			return r""", r"""		case r == '\n':
			return ' '""")

at("multiline: a pasted CR survives", "internal/app/app.go", r"""		case r == '\r':
			return '\n'""", r"""		case r == '\r':
			return r""")

at("multiline: a pasted tab survives", "internal/app/app.go", r"""		case r == '\t':
			return ' '""", r"""		case r == '\t':
			return r""")

at("multiline: a delete byte reaches the line", "internal/app/app.go",
    "case r < ' ' || r == 0x7f:", "case r < ' ':")

# The status row, which no recording draws and no golden file covers.

mut("status drops a segment that fits", "widget.go",
    "if used+cost > room {", "if used+cost >= room {")

# The two below are aimed at ui.Working rather than at StatusWidget.spins, which is now one
# line long and calls it. That is worth knowing when one of them fails: the predicate is
# shared with the input's shine, so a mutation here says two things on the screen at once —
# the verb stops spinning and the box starts glinting through a tool call — and the report
# will name a test from each half.

mut("Working ignores Blocked", "widget.go",
    'return st != nil && st.Blocked == nil && st.Quiescent == "" && st.Active',
    'return st != nil && st.Quiescent == "" && st.Active')

mut("Working ignores Quiescent", "widget.go",
    'return st != nil && st.Blocked == nil && st.Quiescent == "" && st.Active',
    "return st != nil && st.Blocked == nil && st.Active")
mut("spinner has no negative phase", "widget.go", """	i := s.Phase % len(frames)
	if i < 0 {
		i += len(frames)
	}""", """	i := s.Phase % len(frames)""")

mut("spinner indexed by byte", "widget.go",
    "return string(frames[i])", 'return string(g.Get("status.spinner")[i])')

mut("emptied spinner keeps its indent", "widget.go", """		if f := s.frame(g); f != "" {
			out = append(out, Span{Text: f + " ", Style: "status.spinner"})
		}""",
    """		out = append(out, Span{Text: s.frame(g) + " ", Style: "status.spinner"})""")

mut("verb ladder puts Finished first", "widget.go", """	switch {
	case st.Blocked != nil:""", """	switch {
	case st.Finished:
		return append(out, Span{Text: "done", Style: "status.verb"})
	case st.Blocked != nil:""")

mut("status returns a blank row", "widget.go", """	op := statusRow(s.operationalSegments(g, w), w, g)
	if len(op) == 0 {
		return nil // narrower than the verb: no row at all beats a blank one
	}
""", """	op := statusRow(s.operationalSegments(g, w), w, g)
""")

mut("status trusts a nil state", "widget.go",
    "if s.St == nil || w <= bottomMargin {", "if w <= bottomMargin {")

# The frame the viewport hands the terminal.

mut("frame is not trimmed to the height", "render.go", """		if d := len(live) - vp.Height; d > 0 {
			live = live[d:]
			f.Cursor.Line = max(0, f.Cursor.Line-d)
		}
""", "")

mut("trimmed frame leaves the cursor behind", "render.go",
    "f.Cursor.Line = max(0, f.Cursor.Line-d)", "_ = d")

mut("trim keeps the top rows instead of the bottom", "render.go",
    "live = live[d:]", "live = live[:vp.Height]")
# The prompt band. Its whole risk is that a wash made of trailing spaces looks like the
# thing every other row in the transcript has trimmed off it, so each of these is a plausible
# tidy-up by somebody who did not know the spaces were content.

mut("the band stops at the last word", "block.go", """		if n := width - row.Width(); n > 0 {
			row = append(row, Span{Text: strings.Repeat(" ", n)})
		}
""", "")

mut("the band is one column short", "block.go",
    "if n := width - row.Width(); n > 0 {", "if n := width - row.Width() - 1; n > 0 {")

mut("the band is stamped before the pad", "block.go", """		if n := width - row.Width(); n > 0 {
			row = append(row, Span{Text: strings.Repeat(" ", n)})
		}
		for j := range row {
			row[j].Fill = band
		}""", """		for j := range row {
			row[j].Fill = band
		}
		if n := width - row.Width(); n > 0 {
			row = append(row, Span{Text: strings.Repeat(" ", n)})
		}""")

mut("the banded row is trimmed like every other", "block.go", """		for j := range row {
			row[j].Fill = band
		}
		out = append(out, row)""", """		for j := range row {
			row[j].Fill = band
		}
		row[len(row)-1].Text = strings.TrimRight(row[len(row)-1].Text, " ")
		out = append(out, row)""")

mut("the band is a Style and flattens the text", "block.go",
    "row[j].Fill = band", "row[j].Style = band")

# The `w >= width` guard at the top of banded is not mutated, and the reason is worth
# writing down: its only differing case is w == width, where WrapSpans is handed room 0 and
# returns nil under its own guard, so both spellings draw nothing at every width. A mutant
# no behaviour can distinguish cannot be caught by any test, and listing one would report a
# permanent hole where there is none. What is load-bearing about a narrow prompt is the
# subtraction below it — the room the marker leaves — so that is what is broken instead.
# banded and marked are identical down to the wrap call, hence the anchor reaching the row.
mut("the prompt wraps as if the marker were free", "block.go",
    """	for i, ln := range WrapSpans(inlineSpans(text, style), width-w, nil) {
		p := prefix
		if i > 0 {
			p = cont
		}
		row := append(append(Line{}, p...), ln...)""",
    """	for i, ln := range WrapSpans(inlineSpans(text, style), width, nil) {
		p := prefix
		if i > 0 {
			p = cont
		}
		row := append(append(Line{}, p...), ln...)""")

mut("the default theme has no band", "theme.go",
    '"prompt.band": {BG: MustHex("#2c2c31")}', '"prompt.band": {}')

# The hanging indent under a wrapped result line. Every one of these is a row drawn at the
# wrong depth rather than a row missing, so none of them shows up as an overflow or a
# trailing blank — the only thing that can see them is a test that measures the column a
# row's own text starts at.

mut("the result's continuation restarts at the elbow", "block.go",
    r"""		for _, src := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			for _, ln := range wrapHanging(src, key, width-ew) {""",
    r"""		for _, src := range []string{text} {
			for _, ln := range WrapText(src, key, width-ew, nil) {""")

mut("a result ending in a newline gains a row", "block.go",
    r'strings.Split(strings.TrimRight(text, "\n"), "\n")', r'strings.Split(text, "\n")')

mut("the hang is one column short", "block.go",
    'hang := Line{{Text: strings.Repeat(" ", indent)}}',
    'hang := Line{{Text: strings.Repeat(" ", indent-1)}}')

mut("the first row of a result line is not hung", "block.go",
    "for i := range rows {\n\t\trows[i] = join(hang, rows[i])",
    "for i := 1; i < len(rows); i++ {\n\t\trows[i] = join(hang, rows[i])")

mut("the tabs are counted raw", "block.go",
    "src = stripControls(src)", "_ = src")

mut("the hang gives way where it fits", "block.go",
    "indent+ansi.StringWidth(word) > room", "indent+ansi.StringWidth(word) >= room")

# The give-way is broken through the word it measures rather than through the comparison.
# Dropping ansi.StringWidth from the condition leaves the import unused, and a mutant that
# does not compile is caught by the compiler instead of by a test, which is a hole reported
# as a pass. Emptying the word is the same edit in behaviour — the indent alone is measured,
# which is the weaker rule listItem uses — and it still builds.
mut("the hang never gives way", "block.go",
    "word := body", 'word := ""')

mut("a line of nothing but indent is hung", "block.go",
    "indent == 0 || body == \"\" || indent+ansi.StringWidth(word) > room",
    "indent == 0 || indent+ansi.StringWidth(word) > room")

# The two defaults a later edit restores by accident, both of them removals: a glyph that
# went back to being a glyph, and a border that went back to describing itself.

mut("the thought wears its star again", "glyphs.go",
    '{"thinking.marker", "  ",', '{"thinking.marker", "✻ ",')

mut("a fresh editor titles its own border", "input.go",
    'return &Input{Placeholder: "ask anything, or / for commands"}',
    'return &Input{Placeholder: "ask anything, or / for commands", Title: "prompt"}')

at("the config's title never reaches the editor", "internal/app/app.go",
    "a.ed.Title = cfg.InputTitle", "_ = cfg.InputTitle")

# The keys the reader asked for by name, and the ways a later edit takes them back. History
# is on the plain arrows, which is where a shell keeps it, and the wheel cannot forge one: a
# notch is either a report we asked for or nothing at all, because Emitter.Enter switches the
# alternate buffer's wheel-to-arrow translation off whenever it has not claimed the mouse.
# Both directions are mutated: the walk in the arrows test presses up and then down, and a
# table whose second row is never reached is a row that proves nothing.

at("history is not on the arrows", "internal/app/keymap.go",
    '"up":     ActionHistoryPrev,', '"up":     ActionScrollUpFast,')

at("the arrow only walks history backwards", "internal/app/keymap.go",
    '"down":   ActionHistoryNext,', '"down":   ActionScrollDownFast,')

# And the three keys that had to move out of the arrows' way. shift+wheel is a separate
# binding because the modifier rides in the report's button byte and an unbound spelling
# falls through to nothing — a reader holding shift to scroll would get silence. ctrl+up is
# the wheel's notch spelled on the keyboard, because the reader asked for one step and not two;
# alt+up is the single row that has to survive somewhere, and shift+up is the jump, which is on
# shift because ctrl+shift+up never reached this reader's program at all.
at("shift on the wheel scrolls nothing", "internal/app/keymap.go",
    '"shift+wheelup":   ActionScrollUpFast,', '"shift+wheelup":   ActionNone,')

at("ctrl on the arrow goes back to one row", "internal/app/keymap.go",
    '"ctrl+up":         ActionScrollUpFast,', '"ctrl+up":         ActionScrollUp,')

at("the single row loses its only key", "internal/app/keymap.go",
    '"alt+up":          ActionScrollUp,', '"alt+up":          ActionNone,')

at("the jump goes back to a key that never arrives", "internal/app/keymap.go",
    '"shift+up":   ActionJumpPrevMessage,', '"ctrl+shift+up":   ActionJumpPrevMessage,')

# Three lines a notch is a number the reader gave us out loud, and the only assertion that
# can hold it is one that names it. Everything else in that test is arithmetic about
# whatever the constant happens to be, which is why two would otherwise survive.
at("the wheel goes back to two lines", "internal/app/app.go",
    "const defaultWheelLines = 3", "const defaultWheelLines = 2")

# And how far one notch goes while the wheel is spinning, which is measured rather than counted:
# a report says nothing about the hand that sent it, and the only thing that separates a flick
# from a finger resting on the wheel is the gap before it. Every mutation below is a plausible way
# to lose one half of that — the ramp itself, the silence that ends it, the reversal that restarts
# it, the ceiling that keeps it from outrunning the eye, or the clock the whole thing is measured
# against.
#
# accelPeriod's own value is deliberately not mutated. Every gap in the test is written in terms of
# it — a report at exactly accelPeriod, one a millisecond past — so a mutant that retunes the
# constant moves both sides of every comparison and survives by construction. That is the right
# shape for it: what a spin is worth is -scroll, which is a fact about a hand and is pinned by name
# above, while this only decides when two reports are one gesture, which is a fact about a wheel and
# has nothing in it for a reader to have an opinion about.

at("wheel: a spin is worth no more than the notch it started at", "internal/app/app.go",
    "return min(n*a.spin, a.page())", "return min(n, a.page())")

at("wheel: a spin outruns the page and steps over rows nobody read", "internal/app/app.go",
    "return min(n*a.spin, a.page())", "return n * a.spin")

# The device, which is the one thing the action cannot say. A wheel report is one flick of one
# finger, and ctrl+up auto-repeats — so a ramp that cannot tell them apart is a hand accelerating
# by leaning on a key, and the row it stops on is the keyboard controller's decision.
at("wheel: a held key accelerates by leaning", "internal/app/app.go",
    "\tif k.Type != term.KeyWheelUp && k.Type != term.KeyWheelDown {\n\t\treturn n\n\t}", "\t_ = k")

at("wheel: only the up notch accelerates", "internal/app/app.go",
    "if k.Type != term.KeyWheelUp && k.Type != term.KeyWheelDown {",
    "if k.Type != term.KeyWheelUp {")

# The one terminal that reports like a finger instead of like a wheel. Termux sends a report per
# row a swipe has travelled and -scroll is 1 there, so the distance is already the hand's own and
# multiplying it takes the text off the thumb dragging it.
at("wheel: a swipe worth one row a report accelerates too", "internal/app/app.go",
    "\tif n <= 1 {\n\t\treturn n\n\t}", "\tif n < 1 {\n\t\treturn n\n\t}")

# The two halves of "the same gesture continuing", each dropped in turn. Without the clock a spin
# never ends, and the reader's careful last notch throws the page away; without the direction a
# reversal carries the flick's momentum into the correction, and the row they came back for goes
# past a second time.
at("wheel: a spin never ends", "internal/app/app.go",
    "if dir == a.spinDir && now.Sub(a.spinAt) <= accelPeriod {", "if dir == a.spinDir {")

at("wheel: a reversal keeps the momentum it was correcting", "internal/app/app.go",
    "if dir == a.spinDir && now.Sub(a.spinAt) <= accelPeriod {",
    "if now.Sub(a.spinAt) <= accelPeriod {")

at("wheel: the window shuts a moment early", "internal/app/app.go",
    "now.Sub(a.spinAt) <= accelPeriod {", "now.Sub(a.spinAt) < accelPeriod {")

# And the spin's own memory, which is a timestamp and a direction because nothing announces a hand
# coming off a wheel. A report that does not record when it arrived measures every later one
# against the first, and a spin that starts from zero moves no rows at all.
at("wheel: a report does not note when it arrived", "internal/app/app.go",
    "a.spinAt, a.spinDir = now, dir", "a.spinDir = dir")

at("wheel: a report does not note which way it went", "internal/app/app.go",
    "a.spinAt, a.spinDir = now, dir", "a.spinAt = now")

at("wheel: a spin starts from a standstill", "internal/app/app.go",
    "\t} else {\n\t\ta.spin = 1\n\t}", "\t} else {\n\t\ta.spin = 0\n\t}")

# The clock itself. Nothing outside a test sets Config.Now, so the default in New is the whole of
# what a real session is measured against: nil is a panic on the first notch, and a clock that
# does not run is a spin with no end.
at("wheel: New leaves the clock to the caller", "internal/app/app.go",
    "\tif cfg.Now == nil {\n\t\tcfg.Now = time.Now\n\t}\n", "")

at("wheel: the default clock is stopped", "internal/app/app.go",
    "cfg.Now = time.Now", "cfg.Now = func() time.Time { return time.Time{} }")

# And the direction the ramp is measured in, which is the view's and not the key's, so that an
# inverted wheel spins on what the reader can see. One caller passing the other's sign makes a
# reversal look like the same gesture continuing.
at("wheel: a spin cannot tell it turned back", "internal/app/app.go",
    "return a.scroll(-a.fast(k, -1))", "return a.scroll(-a.fast(k, 1))")

# jump's two inequalities. Both mutants are the same mistake in opposite directions: a
# landmark search that accepts the row it is already standing on, which reports a move and
# makes none. The keypress still succeeds, so only a test that watches the offset catches it.
at("a jump up settles for the row it is on", "internal/app/app.go",
    "case dir < 0 && row < a.top:", "case dir < 0 && row <= a.top:")

at("a jump down settles for the row it is on", "internal/app/app.go",
    "case dir > 0 && row > a.top && target < 0:", "case dir > 0 && row >= a.top && target < 0:")

# The landmark itself, in the two ways it can be one row wrong. transcript charges the
# blank between two items to the item that follows it, so a start recorded before that
# append names the separator — the jump lands on an empty row with the turn under it, which
# is the sort of thing a reader notices and a green suite does not.
mut("a turn's landmark is the blank above it", "render.go",
    """		if i > 0 {
			rows = append(rows, Line{})
		}
		starts[i] = len(rows)""",
    """		starts[i] = len(rows)
		if i > 0 {
			rows = append(rows, Line{})
		}""")

# And the filter that keeps -1 out of a list of rows. An item that drew nothing keeps the
# -1 transcript initialises it to, so a turn too narrow to draw is a landmark at row minus
# one — a number the app hands to Frame.Scroll and a test indexes a document with.
mut("a turn that drew nothing is still a landmark", "render.go",
    "if it.Kind == state.KindPrompt && starts[i] >= 0 {", "if it.Kind == state.KindPrompt {")

mut("a nil state is walked for its turns", "render.go",
    "if st == nil || width <= 0 {\n\t\treturn nil\n\t}", "if width <= 0 {\n\t\treturn nil\n\t}")

# The diff bands, which are the one part of this theme a reader has corrected four times.
# Six mutants, one per correction and one per invariant the corrections rest on: the blue that
# was called too strong, the green so dark it read as black, the red that read as pink because
# its blue outran its green, a red loud enough to lose the code lying on it, an indexed colour
# whose actual shade is the terminal's opinion and not ours, and two gutter signs of one colour,
# which is the whole fallback for an eye that cannot separate red from green.

mut("the added band goes back to the rejected blue", "theme.go",
    '"diff.added":          {BG: MustHex("#1c5a38")},',
    '"diff.added":          {BG: MustHex("#12253a")},')

mut("the added band goes back to reading as black", "theme.go",
    '"diff.added":          {BG: MustHex("#1c5a38")},',
    '"diff.added":          {BG: MustHex("#0e2216")},')

mut("the removed band goes back to reading as pink", "theme.go",
    '"diff.removed":        {BG: MustHex("#8a2620")},',
    '"diff.removed":        {BG: MustHex("#7f2830")},')

mut("the removed band shouts over its own code", "theme.go",
    '"diff.removed":        {BG: MustHex("#8a2620")},',
    '"diff.removed":        {BG: MustHex("#a03038")},')

mut("the added band is an indexed colour", "theme.go",
    '"diff.added":          {BG: MustHex("#1c5a38")},',
    '"diff.added":          {BG: Idx(Green)},')

mut("both gutter signs are the same colour", "theme.go",
    '"diff.added.sign":     {FG: Idx(Bright + Green), Attrs: AttrBold},',
    '"diff.added.sign":     {FG: Idx(Bright + Red), Attrs: AttrBold},')

# Who has the mouse, which is the load-bearing half of the keymap above and the one decision in
# this file a reader reversed after using it. Three things want the mouse and only two can have
# it: tracking makes a notch a report and takes drag-to-select away, releasing the mouse gives
# the drag back, and alternate scroll would then rewrite a notch into the arrow keys the input's
# history is bound to. So the default releases the mouse and switches 1007 off, and -mouse claims
# tracking and leaves 1007 alone. Nothing in the player can see any of it — a wrong mode is a
# wheel that reports nothing, or a drag that needs shift, or an arrow nobody pressed — so the
# assertions are on the bytes, and every branch of the two above is worth a mutation.

mut("the wheel is claimed without SGR coordinates", "emit.go",
    "\t\t\tb.WriteString(ansi.SetMode(ansi.ModeMouseButtonEvent))\n"
    "\t\t\tb.WriteString(ansi.SetMode(ansi.ModeMouseExtSgr))",
    "\t\t\tb.WriteString(ansi.SetMode(ansi.ModeMouseButtonEvent))")

mut("the mouse is settled before the buffer exists", "emit.go",
    "\t\tb.WriteString(ansi.SetMode(ansi.ModeAltScreenSaveCursor))\n"
    "\t\te.claimed = true\n"
    "\t\tif e.Mouse {\n"
    "\t\t\tb.WriteString(ansi.SetMode(ansi.ModeMouseButtonEvent))\n"
    "\t\t\tb.WriteString(ansi.SetMode(ansi.ModeMouseExtSgr))\n"
    "\t\t} else {\n"
    "\t\t\tb.WriteString(ansi.ResetMode(modeAlternateScroll))\n"
    "\t\t}",
    "\t\tif e.Mouse {\n"
    "\t\t\tb.WriteString(ansi.SetMode(ansi.ModeMouseButtonEvent))\n"
    "\t\t\tb.WriteString(ansi.SetMode(ansi.ModeMouseExtSgr))\n"
    "\t\t} else {\n"
    "\t\t\tb.WriteString(ansi.ResetMode(modeAlternateScroll))\n"
    "\t\t}\n"
    "\t\tb.WriteString(ansi.SetMode(ansi.ModeAltScreenSaveCursor))\n"
    "\t\te.claimed = true")

# The flag, in both directions. A mouse claimed without being asked for is the arrangement the
# reader rejected — every drag needs shift — and it is the one mutation here that a passing suite
# would once have called correct.
mut("the mouse is claimed whether or not it was asked for", "emit.go",
    "\t\te.claimed = true\n\t\tif e.Mouse {", "\t\te.claimed = true\n\t\tif true {")

mut("alternate scroll is left on when the mouse is released", "emit.go",
    "\t\t} else {\n"
    "\t\t\tb.WriteString(ansi.ResetMode(modeAlternateScroll))\n"
    "\t\t}",
    "\t\t}")

mut("alternate scroll is refused even when tracking already suppresses it", "emit.go",
    "\t\t\tb.WriteString(ansi.SetMode(ansi.ModeMouseExtSgr))\n\t\t} else {",
    "\t\t\tb.WriteString(ansi.SetMode(ansi.ModeMouseExtSgr))\n"
    "\t\t\tb.WriteString(ansi.ResetMode(modeAlternateScroll))\n"
    "\t\t} else {")

# The number itself, which is spelled once because x/ansi has no constant for it. The tests write
# the bytes out rather than asking ansi.SetMode, and this is the mutation that says so.
mut("alternate scroll is some other private mode", "emit.go",
    "const modeAlternateScroll = ansi.DECMode(1007)",
    "const modeAlternateScroll = ansi.DECMode(1004)")

mut("exit keeps the mouse it borrowed", "emit.go",
    "\t\t\tb.WriteString(ansi.ResetMode(ansi.ModeMouseExtSgr))\n"
    "\t\t\tb.WriteString(ansi.ResetMode(ansi.ModeMouseButtonEvent))",
    "\t\t\t_ = ansi.ModeMouseExtSgr")

mut("exit leaves the next program without a wheel", "emit.go",
    "\t\t\tb.WriteString(ansi.SetMode(modeAlternateScroll))",
    "\t\t\t_ = modeAlternateScroll")

mut("inline takes the mouse it has no use for", "emit.go",
    "\te.started = true\n\tvar b strings.Builder\n\tif e.Mode == ModeAlt {",
    "\te.started = true\n\tvar b strings.Builder\n\tif true {")

# Asking the keyboard to disambiguate, which is the other half of the second line in the input:
# enter, shift+enter and ctrl+enter are one byte until the terminal is asked to tell them apart,
# so the keymap above binds a chord that cannot arrive unless these six bytes go out. Nothing in
# the player can see the difference and no golden file covers it — a missing request is a chord
# that arrives as plain enter, which looks like a program that simply sent the line — so the
# assertions are on the bytes. A push is also not a mode: it is a stack shared with whoever
# launched us, which is why the pop and the guard on it are worth a mutation each.

mut("the keyboard is never asked to disambiguate", "emit.go",
    "\tb.WriteString(ansi.PushKittyKeyboard(ansi.KittyDisambiguateEscapeCodes))\n"
    "\te.pushed = true\n", "")

mut("the keyboard is asked for every flag", "emit.go",
    "ansi.PushKittyKeyboard(ansi.KittyDisambiguateEscapeCodes)",
    "ansi.PushKittyKeyboard(ansi.KittyAllFlags)")

mut("only the alternate buffer asks the keyboard", "emit.go",
    "\t\t}\n"
    "\t}\n"
    "\tb.WriteString(ansi.SetMode(ansi.ModeBracketedPaste))\n"
    "\tb.WriteString(ansi.PushKittyKeyboard(ansi.KittyDisambiguateEscapeCodes))",
    "\t\t}\n"
    "\t\tb.WriteString(ansi.PushKittyKeyboard(ansi.KittyDisambiguateEscapeCodes))\n"
    "\t}\n"
    "\tb.WriteString(ansi.SetMode(ansi.ModeBracketedPaste))")

mut("the shell is handed back a keyboard in CSI u", "emit.go",
    "\tif e.pushed {\n"
    "\t\tb.WriteString(ansi.PopKittyKeyboard(1))\n"
    "\t\te.pushed = false\n"
    "\t}\n", "")

mut("the keyboard pop is unguarded and takes the entry below ours", "emit.go",
    "\tif e.pushed {\n"
    "\t\tb.WriteString(ansi.PopKittyKeyboard(1))\n"
    "\t\te.pushed = false\n"
    "\t}",
    "\tb.WriteString(ansi.PopKittyKeyboard(1))")

# And the half of the second line that needs no protocol at all, which is the half that works on
# the terminal this was written on. Windows Terminal and conhost send LF for ctrl+enter: ctrl folds
# the key into the control range instead of dropping the modifier, so 0x0a is ctrl+j and is the one
# spelling of the chord a legacy terminal can express. Folding it in beside 0x0d is exactly what
# this decoder used to do, and it is what made ctrl+enter send the line — the reader reported it as
# a lie, and they were right. Both mutants restore that bug from the two ends it can be restored
# from: the decoder that throws the byte away, and the keymap row that has nothing to bind it to.

at("LF is a second spelling of enter", "internal/term/decode.go",
   "\tcase c == 0x0d:", "\tcase c == 0x0d, c == 0x0a:")

at("ctrl+j starts no second line", "internal/app/keymap.go",
   '"ctrl+j":      ActionNewline,', '"ctrl+j":      ActionNone,')

at("the chord a newer terminal will send is unbound", "internal/app/keymap.go",
   '"shift+enter": ActionNewline,', '"shift+enter": ActionNone,')

at("the chord this terminal sends today is unbound", "internal/app/keymap.go",
   '"ctrl+enter":  ActionNewline,', '"ctrl+enter":  ActionNone,')

# The config reader, whose labels all begin "config:" so that one filtered run covers the
# file. It is read once, by hand, from something a person typed into an editor, and it has no
# golden file behind it: the tests are the whole specification, so every rule the reader
# states in prose gets a mutation here. The three at the end live in internal/ui because that
# is where the promises a config's [glyphs] and [styles] sections rest on are kept.

at("config: the line count starts at zero", "internal/config/config.go",
   "for p.line = 1; sn.Scan(); p.line++ {", "for p.line = 0; sn.Scan(); p.line++ {")

at("config: only the first problem is reported", "internal/config/config.go",
   "\t\treturn nil, errors.Join(p.problems...)", "\t\treturn nil, p.problems[0]")

at("config: any # opens a comment, as in toml", "internal/config/config.go",
   r"if s[i] == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {",
   "if s[i] == '#' {")

at("config: a header's own comment stays in it", "internal/config/config.go",
   "\thead := strings.TrimSpace(cutComment(text))", "\thead := strings.TrimSpace(text)")

at("config: a header need not close", "internal/config/config.go",
   '\tif strings.HasSuffix(head, "]") {\n'
   "\t\tname = strings.TrimSpace(head[1 : len(head)-1])\n"
   "\t}",
   '\tname = strings.TrimSpace(strings.TrimSuffix(head[1:], "]"))')

at("config: a skipped section's lines are read", "internal/config/config.go",
   '\tp.section, p.skip = "", true', '\tp.section, p.skip = "", false')

at("config: a headerless file complains per line", "internal/config/config.go",
   '\t\tp.bad("%s comes before any [section] header", strconv.Quote(key))\n'
   "\t\tp.skip = true\n",
   '\t\tp.bad("%s comes before any [section] header", strconv.Quote(key))\n')

at("config: a blank line is a setting", "internal/config/config.go",
   '\tif text == "" || strings.HasPrefix(text, "#") {',
   '\tif strings.HasPrefix(text, "#") {')

at("config: the first = is not the separator", "internal/config/config.go",
   '\tname, rest, ok := strings.Cut(text, "=")',
   '\tparts := strings.Split(text, "=")\n'
   "\tname, rest, ok := parts[0], strings.Join(parts[1:], \"\"), len(parts) > 1")

at("config: a quoted key keeps its quotes", "internal/config/config.go",
   '\tkey := strings.Trim(strings.TrimSpace(name), `"`)',
   "\tkey := strings.TrimSpace(name)")

# The five membership checks the reader repeats after the constructors, each of which is worth
# a line number and nothing else. A surviving mutation here means the duplicated check has
# stopped earning its place, or that the test which reads the line number has narrowed.

at("config: an action nobody declared is an action", "internal/config/config.go",
   "\t\tif a != app.ActionNone && !declaredAction(a) {", "\t\tif false {")

at("config: a glyph nobody draws is a glyph", "internal/config/config.go",
   "\t\tif !declaredGlyph(key) {", "\t\tif false {")

at("config: a style key nobody declared is one", "internal/config/config.go",
   "\t\tif !ui.Declared(key) {", "\t\tif false {")

at("config: a style value is not parsed here", "internal/config/config.go",
   "\t\tst, err := ui.ParseStyle(val)\n"
   "\t\tif err != nil {\n"
   '\t\t\tp.bad("%s: %v", key, err)\n'
   "\t\t\treturn\n"
   "\t\t}",
   "\t\tst, _ := ui.ParseStyle(val)")

at("config: [input] takes any setting", "internal/config/config.go",
   '\t\tif key != "title" {', "\t\tif false {")

# [scroll] grew a second setting and its membership check became a switch, so the mutation is
# now the default arm going quiet rather than one comparison flipping. It is the same hole
# either way: a misspelled key that silently does nothing is the failure mode a config reader
# exists to prevent.
at("config: [scroll] takes any setting", "internal/config/config.go",
   '\t\tdefault:\n\t\t\tp.bad("[scroll] has no %s; it has lines and mouse", strconv.Quote(key))\n\t\t}',
   "\t\t}")

# And the floor moved into parser.count, which [scroll] lines and the three [anim] numbers all
# share, so this one line is the whole program's answer to "0 is not a number of rows".
at("config: a count of zero is taken rather than refused", "internal/config/config.go",
   "\tif err != nil || n < 1 {", "\tif err != nil {")

# The duplicate check, which is the one rule in the reader that no constructor repeats: a key
# written twice would otherwise be settled by whichever line came last, silently.

at("config: a key set twice is forgotten", "internal/config/config.go",
   "\tp.seen[at] = p.line", "\t_ = at")

at("config: two sections share one namespace", "internal/config/config.go",
   '\tat := p.section + "." + name', "\tat := name")

at("config: one binding under two spellings passes", "internal/config/config.go",
   "\t\tif !p.dup(k.String()) {", "\t\tif !p.dup(key) {")

at("config: a key is stored as it was written", "internal/config/config.go",
   "\t\t\tp.f.Keys[k.String()] = a", "\t\t\tp.f.Keys[key] = a")

# The quoting rules, which are the half of the format a person meets by getting it wrong. A
# value is where a trailing space, a # and a backslash all have to survive being read.

at("config: a quoted value is never unquoted", "internal/config/config.go",
   "\tif raw[0] != '\"' {", "\tif true {")

at("config: an empty bare value is a value", "internal/config/config.go",
   "\t\tv := strings.TrimSpace(cutComment(raw))\n"
   '\t\tif v == "" {\n'
   '\t\t\treturn "", errors.New("no value before the comment")\n'
   "\t\t}\n"
   "\t\treturn v, nil",
   "\t\treturn strings.TrimSpace(cutComment(raw)), nil")

at("config: any escape is the character after it", "internal/config/config.go",
   "\t\t\tif raw[i] != '\\\\' && raw[i] != '\"' {", "\t\t\tif false {")

at("config: an unclosed quote closes at the end", "internal/config/config.go",
   '\treturn "", errors.New("unclosed quote")', "\treturn b.String(), nil")

at("config: text after the quote is ignored", "internal/config/config.go",
   '\t\t\tif rest := strings.TrimSpace(cutComment(raw[i+1:])); rest != "" {\n'
   '\t\t\t\treturn "", fmt.Errorf("%s after the closing quote", strconv.Quote(rest))\n'
   "\t\t\t}\n"
   "\t\t\treturn b.String(), nil",
   "\t\t\treturn b.String(), nil")

# The four methods main calls, where a mutation is a config that loaded and then did nothing.

at("config: -ascii never reaches the glyphs", "internal/config/config.go",
   "return ui.NewGlyphs(f.Glyphs, ascii)", "return ui.NewGlyphs(f.Glyphs, false)")

at("config: an unset title is still printed", "internal/config/config.go",
   '\tif f.InputTitle != "" {', "\tif true {")

at("config: a missing default file is an error", "internal/config/config.go",
   "\tif _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {",
   "\tif _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) && false {")

at("config: the default file is found and not read", "internal/config/config.go",
   "\treturn Load(path)\n}", "\treturn &File{}, nil\n}")

# And the two constructors in internal/ui that exist only because a config file does. Both
# promises are invisible from inside ui — a theme with sixty unstyled keys still draws, and a
# glyph override that loses to -ascii still draws — so they are proven by the config's tests
# and by ui's own, and a mutation is the only thing that says which.

mut("config: a themed key unstyles the others", "theme.go",
    "\tth := DefaultTheme()\n\tnames := make([]string, 0, len(overrides))",
    "\tth := DefaultTheme()\n\tth.styles = map[string]Style{}\n"
    "\tnames := make([]string, 0, len(overrides))")

mut("config: an override lays over the default", "theme.go",
    "\t\tth.styles[k] = overrides[k]", "\t\tth.styles[k] = overrides[k].Over(th.styles[k])")

mut("config: an override loses to -ascii", "glyphs.go",
    "\t\tg.set[k] = v", "\t\tif !ascii {\n\t\t\tg.set[k] = v\n\t\t}")

# The line and the door. ctrl+c used to be ActionQuit, and the reader found it the way anyone
# would: a half-typed line, the reflex every shell has taught, and the program was gone. So the
# two keys divide the work — ctrl+c throws the line away and only leaves on a second press with
# nothing left to throw, ctrl+d is the door once — and "a second press" is the one piece of state
# no keymap can hold. Every mutation below is a way for that state to be wrong that leaves the
# suite green if nothing is watching: an arm that never goes up, one that never comes down, one
# that says nothing on screen, and a door bound somewhere else.

at("interrupt: the first ctrl+c leaves", "internal/app/app.go",
   "\tif a.armed && a.cfg.Now().Sub(a.armedAt) <= armTimeout {\n\t\ta.quit = true",
   "\tif true {\n\t\ta.quit = true")

at("interrupt: it is dispatched like any other action", "internal/app/app.go",
   "\tif act == ActionInterrupt {\n\t\treturn a.interrupt()\n\t}\n", "")

at("interrupt: the line survives it", "internal/app/app.go",
   "\n\ta.ed.KillLine()\n\treturn true\n}", "\n\treturn true\n}")

# The disarm is in key and not in the thirty-odd returns of dispatch, which is the whole reason
# key exists. One arm left standing is a door that opens on a keypress nobody meant as a second
# one — and the repaint is half of it, because a key that changed nothing while the arm was up
# has still changed the screen it promised on.
at("interrupt: no key ever disarms", "internal/app/app.go",
   "\tdisarmed := a.armed\n\ta.armed = false\n", "\tdisarmed := a.armed\n")

at("interrupt: disarming is not worth a repaint", "internal/app/app.go",
   "\tdirty := a.dispatch(act, k) || disarmed",
   "\t_ = disarmed\n\tdirty := a.dispatch(act, k)")

# Both notices ask the keymap for the key they name, because a config may have moved it and a
# sentence naming ctrl+d on a terminal where ctrl+d does nothing is worse than no sentence. The
# guard is the other half: ActionNone can take the door away entirely, and then the notice has
# to stop after the part of it that is still true.
at("interrupt: the armed run says nothing", "internal/app/app.go",
   "\tif a.armed {\n\t\tif a.cfg.Now().Sub(a.armedAt) > armTimeout {",
   "\tif false {\n\t\tif a.cfg.Now().Sub(a.armedAt) > armTimeout {")

at("interrupt: the armed notice names the key we shipped", "internal/app/app.go",
   '"press " + a.km.KeyFor(ActionInterrupt) + " again to leave"',
   '"press ctrl+c again to leave"')

at("interrupt: the end-of-scenario notice names the key we shipped", "internal/app/app.go",
   'leave += ", " + k + " to leave"', 'leave += ", ctrl+d to leave"')

at("interrupt: a door nobody bound is promised anyway", "internal/app/app.go",
   '\tif k := a.km.KeyFor(ActionQuit); k != "" {',
   "\tif k := a.km.KeyFor(ActionQuit); true {")

at("interrupt: ctrl+c goes back to being the door", "internal/app/keymap.go",
   '"ctrl+c": ActionInterrupt,', '"ctrl+c": ActionQuit,')

at("interrupt: ctrl+d stops being the door", "internal/app/keymap.go",
   '"ctrl+d": ActionQuit,', '"ctrl+d": ActionNewline,')

# And the terminal that reverses the mouse trade for itself. A phone has no drag to protect and
# no 1007 to switch off: an unclaimed swipe arrives as the arrow keys, so the reader's finger
# walked the input's history instead of scrolling. term.IsTermux is the whole of the decision,
# and being wrong in either direction costs something real — a false positive takes drag-to-select
# off a desktop mouse, a false negative leaves the bug in place — which is why so small a function
# gets four mutations.

at("termux: an ssh session is the phone's own view", "internal/term/termux.go",
   '\tif os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {\n\t\treturn false\n\t}\n', "")

at("termux: only one spelling of ssh is checked", "internal/term/termux.go",
   ' || os.Getenv("SSH_TTY") != ""', "")

at("termux: the app's own signal is not consulted", "internal/term/termux.go",
   '\tif os.Getenv("TERMUX_VERSION") != "" {\n\t\treturn true\n\t}\n', "")

at("termux: any prefix at all is the bootstrap's", "internal/term/termux.go",
   'strings.Contains(os.Getenv("PREFIX"), "com.termux")',
   'strings.Contains(os.Getenv("PREFIX"), "")')

at("termux: the bootstrap is never the fallback", "internal/term/termux.go",
   '\treturn strings.Contains(os.Getenv("PREFIX"), "com.termux")',
   '\treturn false && strings.Contains(os.Getenv("PREFIX"), "com.termux")')

# The shimmer, which is the one thing in the interface that moves without being asked to and
# therefore the one thing whose bugs a screenshot cannot hold. Half of these are arithmetic a
# reader would have to watch for five seconds to catch — a band that never fully leaves the row,
# a falloff with a bright side, a pass that skips columns on a wide monitor — and the other half
# are the wiring: a light drawn under the row instead of over it, or armed in the one state where
# it means the opposite of what it says.

mut("shine: the switch is always on", "shimmer.go",
    'func (s Shimmer) On() bool { return s.Style != "" }',
    "func (s Shimmer) On() bool { return true }")

mut("shine: every band is hard-edged", "shimmer.go",
    "\tif r, ok := ramps[s.Style]; ok {\n\t\treturn r\n\t}\n", "")

mut("shine: a rung of the input's ladder is misspelled", "shimmer.go",
    'InputShine + ".soft"', 'InputShine + ".Soft"')

mut("shine: the status band loses its falloff", "shimmer.go",
    'StatusShine: {StatusShine, StatusShine + ".soft"}', "StatusShine: {StatusShine}")

mut("shine: the falloff is measured from the left edge", "shimmer.go",
    "d := 2*c - (lo + hi - 1)", "d := 2 * (c - lo)")

mut("shine: the falloff is off by half a column", "shimmer.go",
    "d := 2*c - (lo + hi - 1)", "d := 2*c - (lo + hi)")

mut("shine: the dimmest rung is unreachable", "shimmer.go",
    "return min(n-1, n*d/(hi-lo))", "return min(n-2, n*d/(hi-lo))")

mut("shine: Band answers past the ends of the row", "shimmer.go",
    "\tfrom, to = max(0, lo), min(w, hi)", "\tfrom, to = lo, hi")

mut("shine: the rungs are measured against the clipped band", "shimmer.go",
    "key := r[level(c, lo, hi, len(r))]", "key := r[level(c, from, to, len(r))]")

mut("shine: a wide row tears", "shimmer.go",
    "\tif step := (w + s.Travel - 1) / s.Travel; s.Width < step {\n\t\ts.Width = step\n\t}\n", "")

mut("shine: the tear width is floored, not ceilinged", "shimmer.go",
    "(w + s.Travel - 1) / s.Travel", "w / s.Travel")

mut("shine: the widening outruns the half-row cap", "shimmer.go",
    "\tt := ((s.Phase % s.Period) + s.Period) % s.Period",
    "\tif step := (w + s.Travel - 1) / s.Travel; s.Width < step {\n\t\ts.Width = step\n\t}\n"
    "\tt := ((s.Phase % s.Period) + s.Period) % s.Period")

mut("shine: a narrow row lights up whole", "shimmer.go",
    "\tif 2*s.Width > w {\n\t\ts.Width = max(1, w/2)\n\t}\n", "")

mut("shine: the pass never rests", "shimmer.go",
    "if t >= s.Travel {", "if t >= s.Period {")

mut("shine: the band does not slide in from the edge", "shimmer.go",
    "edge := (w + s.Width) * (t + 1) / (s.Travel + 1)",
    "edge := w * (t + 1) / (s.Travel + 1)")

mut("shine: the band trails the tick instead of leading it", "shimmer.go",
    "return edge - s.Width, edge, true", "return edge, edge + s.Width, true")

mut("shine: the light erases what it crosses", "shimmer.go",
    '\tif sp.Fill == "" {\n\t\tsp.Fill = sp.Style\n\t}\n', "")

mut("shine: the light goes under the row instead of over it", "shimmer.go",
    "\tsp.Style = key\n\treturn sp", "\tsp.Fill = key\n\treturn sp")

mut("shine: a rung starts one column early", "shimmer.go",
    "cutCols(rest, st.from-at)", "cutCols(rest, st.from-at-1)")

mut("shine: a rung ends one column late", "shimmer.go",
    "cutCols(rest, st.to-at)", "cutCols(rest, st.to-at+1)")

mut("shine: Slower slows the pass as well as the rest", "shimmer.go",
    "\ts.Period *= n", "\ts.Period, s.Travel = s.Period*n, s.Travel*n")

mut("shine: Slower arms the zero shimmer", "shimmer.go",
    "if !s.On() || n <= 1 {", "if n <= 1 {")

mut("shine: a pass as long as the period leaves no rest", "shimmer.go",
    "s.Period = max(shinePeriod, s.Travel+1)", "s.Period = max(shinePeriod, s.Travel)")

mut("shine: a negative phase is never lit", "shimmer.go",
    "t := ((s.Phase % s.Period) + s.Period) % s.Period", "t := s.Phase % s.Period")

mut("shine: the box is never lit", "input.go",
    "rows[i] = in.Shine.Apply(l, width)", "rows[i] = l")

at("shine: the verb's band is measured against the terminal", "internal/ui/widget.go",
   "s.Shine.Apply(verb, verb.Width())", "s.Shine.Apply(verb, verb.Width()+8)")

at("shine: the verb is never lit", "internal/ui/widget.go",
   "return append(out, s.Shine.Apply(verb, verb.Width())...)", "return append(out, verb...)")

# And the four lines in app that decide when each band is armed. The state they read is the whole
# meaning of the animation: a light on the box says "your turn", so arming it one state further
# out says the opposite of what it says.

at("shine: the drawn box never receives its band", "internal/app/app.go",
   "\ta.ed.Shine = a.inputShine()\n", "")

at("shine: the box glints under a half-typed line", "internal/app/app.go",
   "if ui.Working(a.st) || !a.ed.Empty() {", "if ui.Working(a.st) {")

at("shine: the box goes dark on an open approval", "internal/app/app.go",
   "if ui.Working(a.st) || !a.ed.Empty() {", "if a.st.Active || !a.ed.Empty() {")

at("shine: both bands come round at the same rate", "internal/app/app.go",
   "return a.shine(ui.InputShine).Slower(2)", "return a.shine(ui.InputShine)")

at("shine: the box is drawn in the verb's key", "internal/app/app.go",
   "return a.shine(ui.InputShine).Slower(2)", "return a.shine(ui.StatusShine).Slower(2)")

at("shine: a band runs on the clock the config wrote", "internal/app/app.go",
   "\ts.Style, s.Phase = key, a.phase", "\ts.Style = key")

at("shine: the status row is handed no band", "internal/app/app.go",
   "Shine: a.shine(ui.StatusShine)", "Shine: ui.Shimmer{}")

# The pinned header, which is the reader's own message held above a transcript they have scrolled
# away from. Every rule in it is a boundary — which turn the top edge has come to rest inside, how
# much of that turn is gone, how much of the window the row may spend saying so — and a mutation
# here draws a row rather than losing one: the wrong turn's words, or a line the transcript is
# already drawing directly underneath it. Neither is visible in a screenshot of one frame, and the
# second is the one thing this row must never do, so all four of the first group are here. The
# labels share a prefix so that one filtered run covers the whole feature.

at("pin: the last turn in the log is the landmark", "internal/ui/render.go",
   "\t\tif starts[i] > row {\n\t\t\tbreak // this turn begins below the top edge, and so does every turn after it\n\t\t}\n", "")

# The nil guard is PromptRows' one over again, and it is listed twice for the same reason the two
# functions have it twice: each is called with whatever the app happens to hold, and one of the two
# callers is a widget the app installs before a recording has been folded.
at("pin: a nil state is walked for the turn above the window", "internal/ui/render.go",
   "if st == nil || width <= 0 {\n\t\treturn \"\", 0\n\t}", "if width <= 0 {\n\t\treturn \"\", 0\n\t}")

at("pin: a turn's own first row belongs to the turn above it", "internal/ui/render.go",
   "if starts[i] > row {", "if starts[i] >= row {")

at("pin: any block at all is one of the reader's turns", "internal/ui/render.go",
   "if it.Kind != state.KindPrompt || starts[i] < 0 {", "if starts[i] < 0 {")

at("pin: the count runs past the question into the answer", "internal/ui/render.go",
   "text, hidden = it.Text, min(row-starts[i], len(r.blockLines(it, width)))",
   "text, hidden = it.Text, row-starts[i]")

# The widget's two numbers. The count is what keeps a pinned line off a screen that is already
# drawing it a row below; the cap is what stops a pasted paragraph from spending the conversation's
# own rows on a label for itself. Both spellings of losing them are here because they fail in
# opposite directions, and the guard is the third: a turn the reader has not scrolled past at all.
mut("pin: the cap is drawn whether or not it was lost", "widget.go",
    "n := min(h.Rows, HeaderRows)", "n := HeaderRows")

mut("pin: a pasted paragraph is pinned whole", "widget.go",
    "n := min(h.Rows, HeaderRows)", "n := h.Rows")

mut("pin: a turn nobody scrolled past is still pinned", "widget.go",
    'if h.Text == "" || h.Rows <= 0 {', 'if h.Text == "" {')

# And the four lines in app that decide when the row is up and what it costs. The gate is the rule
# as the reader gave it — only while scrolled — and the arithmetic under it is the defect the row
# introduced: the header's rows come out of the transcript window, so a page measured on a window
# with no header in it can land on one two rows shorter, and the rows in between are on neither
# screen. Nobody read them and nothing on the screen would say so. The last two are the correction
# being paid in the wrong places: at the tail, where no header is up, and inline, where Place leaves
# the row out and the window can never be shortened by one.
at("pin: the row is up while the recording plays", "internal/app/app.go",
   "\tif a.scrolled {\n\t\tif t, n := a.r.PromptInside(",
   "\tif true {\n\t\tif t, n := a.r.PromptInside(")

at("pin: a page does not pay for a header that is not up yet", "internal/app/app.go",
   "n := a.rows - 1 - a.slack()", "n := a.rows - 1")

at("pin: a page pays again for the header already up", "internal/app/app.go",
   "\tif !a.scrolled || !a.vp.FixedTop {\n\t\treturn 0\n\t}\n\tt, n := a.r.PromptInside",
   "\tif !a.vp.FixedTop {\n\t\treturn 0\n\t}\n\tt, n := a.r.PromptInside")

at("pin: a page inline pays for a row that surface leaves out", "internal/app/app.go",
   "func (a *App) slack() int {\n\tif !a.vp.FixedTop {\n\t\treturn 0\n\t}\n",
   "func (a *App) slack() int {\n")

# The scrollbar, in four parts. The reservation is first, and it is the part that can be wrong
# without anybody seeing a bar at all: two columns come out of every row of the transcript on a
# surface that says it can hold a column, painted or not, and a reservation that asked a different
# question would re-wrap the whole conversation the moment the reader scrolled. The two guards
# either side of it are the ends nobody looks at — a document, which has no window to be a
# fraction of, and a screen too narrow to give anything up, which is a drag caught mid-flight.

mut("pill: a document reserves a column nobody draws", "layout.go",
    "if vp.Height <= 0 || !SlotRight.Available(vp) || vp.Width <= SideCols {",
    "if !SlotRight.Available(vp) || vp.Width <= SideCols {")

mut("pill: the reservation ignores whether a column can be held", "layout.go",
    "if vp.Height <= 0 || !SlotRight.Available(vp) || vp.Width <= SideCols {",
    "if vp.Height <= 0 || vp.Width <= SideCols {")

mut("pill: a screen too narrow gives up its only columns", "layout.go",
    "if vp.Height <= 0 || !SlotRight.Available(vp) || vp.Width <= SideCols {",
    "if vp.Height <= 0 || !SlotRight.Available(vp) {")

mut("pill: the column is a column of ink alone", "layout.go",
    "return vp.Width - SideCols", "return vp.Width - 1")

mut("pill: the width filled is a second constant, not the difference", "layout.go",
    "func sideWidth(vp Viewport) int { return vp.Width - TranscriptWidth(vp) }",
    "func sideWidth(vp Viewport) int { return SideCols }")

# Then the geometry: how long the thumb is and where it sits. The two end-claims are the whole
# point of the thing — a bar that does not touch the top when there is nothing above it is a bar
# that lies about the one fact a reader checks it for — and here they are held by the cell each
# occupied end keeps back, so every spelling of losing that cell is below.

mut("pill: a transcript that fits is given a bar anyway", "widget.go",
    "if h <= 0 || above+below == 0 {", "if h <= 0 {")

mut("pill: the top of the log sits a row down the track", "widget.go",
    "\tif above > 0 {\n\t\tlo++\n\t}", "\tif above >= 0 {\n\t\tlo++\n\t}")

mut("pill: the end of the log stops a row short of the track", "widget.go",
    "\tif below > 0 {\n\t\thi--\n\t}", "\tif below >= 0 {\n\t\thi--\n\t}")

mut("pill: a track with no room for the held-back cells draws a bar", "widget.go",
    "\treturn lo, hi, hi-lo >= 1\n", "\treturn lo, hi, hi-lo >= 0\n")

mut("pill: the thumb measures the hidden rows instead of the visible ones", "widget.go",
    "if n = h * rows / (above + rows + below); n > hi-lo {",
    "if n = h * (above + below) / (above + rows + below); n > hi-lo {")

mut("pill: a long enough log has no thumb at all", "widget.go",
    "\tif n < 1 {\n\t\tn = 1\n\t}\n", "")

mut("pill: the position is measured from the wrong end of the log", "widget.go",
    "top = lo + (above*span+d/2)/d", "top = lo + (below*span+d/2)/d")

mut("pill: the thumb is positioned in the whole track, not the free span", "widget.go",
    "top = lo + (above*span+d/2)/d", "top = (above*span+d/2)/d")

mut("pill: the free span is the whole track", "widget.go",
    "if span, d := hi-n-lo, above+below; span > 0 {",
    "if span, d := hi-lo, above+below; span > 0 {")

# The drawing, which is where a two-column budget is spent. A glyph is a themeable string and a
# themed string is a wide one sooner or later, so the row it cannot fit into is left empty rather
# than allowed to push the frame out by a column; the air is the other cell, and without it the bar
# is welded to the prose. The last two are the thumb's own span, off by one at either end.

mut("pill: a glyph too wide for the column is drawn anyway", "widget.go",
    "if gw == 0 || gw > w {", "if gw > w {")

mut("pill: a glyph with no width still takes a row", "widget.go",
    "if gw == 0 || gw > w {", "if gw == 0 {")

mut("pill: the column is ink with no air beside it", "widget.go",
    "\t\trow := Line{}\n\t\tif air := w - gw; air > 0 {\n\t\t\trow = append(row, pad(air))\n\t\t}\n",
    "\t\trow := Line{}\n")

mut("pill: the thumb starts a row late", "widget.go",
    "if i >= top && i < top+n {", "if i > top && i < top+n {")

mut("pill: the thumb runs a row long", "widget.go",
    "if i >= top && i < top+n {", "if i >= top && i <= top+n {")

# The two slot answers, which are how the bar is kept off a surface that cannot hold it without a
# single line anywhere asking what mode we are in. An empty Fallback is a widget asking to be
# dropped; any other answer is a bar drawn down the edge of the reader's own scrollback.

mut("pill: the bar asks for a slot that hides the transcript", "widget.go",
    "func (ScrollbarWidget) Slot() Slot   { return SlotRight }",
    "func (ScrollbarWidget) Slot() Slot   { return SlotFull }")

mut("pill: the bar falls back instead of being dropped", "widget.go",
    'func (ScrollbarWidget) Fallback() Slot { return "" }',
    "func (ScrollbarWidget) Fallback() Slot { return SlotBottom }")

# And the composition, which is the half that can corrupt a terminal rather than merely mislead a
# reader: the column is painted into the gap the wrap point already left, so a row is padded back
# out to it and the cell appended. Padding a row the column had nothing to say about would end it
# in bare air, which is how a terminal is talked into wrapping a row we own; and skipping the padding
# draws a ragged edge down the middle of the screen.
#
# The copy that guards the memo is deliberately not mutated here. Aliasing the window row survives
# every test in the tree, and it should: each memo Line owns its backing array, so appending in
# place writes past that Line's own length and no reader ever looks there. The copy is an invariant
# rather than a fix, and a family entry for a line nothing observes is a SURVIVED, not a gap.

at("pill: an empty column row is padded to the wrap point and left there", "internal/ui/render.go",
   "if i >= len(col) || len(col[i]) == 0 {", "if i >= len(col) {")

at("pill: the column is drawn wherever the prose stopped", "internal/ui/render.go",
   "\t\t\tif p := tw - row.Width(); p > 0 {\n\t\t\t\trow = append(row, pad(p))\n\t\t\t}\n", "")

# Two lines in app, and the second is the one the header taught us to look for. The bar is installed
# whether or not the reader has scrolled, unlike the pinned row: a landmark is for somebody who has
# lost their place, but "how much is above me" is what a reader following the tail wants to know
# before they have touched a key, and the bar's own geometry already declines to draw on a
# transcript that fits.

at("pill: nobody installs the bar", "internal/app/app.go",
   "\tout = append(out, ui.ScrollbarWidget{Held: a.dragging})\n", "")

at("pill: the bar is up only while the reader has scrolled", "internal/app/app.go",
   "\tout = append(out, ui.ScrollbarWidget{Held: a.dragging})\n",
   "\tif a.scrolled {\n\t\tout = append(out, ui.ScrollbarWidget{Held: a.dragging})\n\t}\n")

# Demand 6's second half: the pill can be taken hold of and carried. It is the first gesture in this
# program that is not a key, and it is assembled out of four files that each own a different third of
# it. The decoder turns three numbers and a final byte into a press, a motion or a release. The
# widget owns every row of arithmetic, forwards for drawing and backwards for dragging. The renderer
# publishes the one rectangle that says where the bar ended up. And app is a hit test, a flag and a
# subtraction, and nothing else. A drag is therefore the place where an off-by-one has three files to
# hide in, and where "it looked right in my terminal" is worth the least it is ever worth: the pill
# sticking to the finger is a claim about somebody's terminal, and this family is what is left of it
# once the terminal is taken away.

# The decoder first, because a gesture that arrives wrong cannot be hit-tested right. Every field in
# an SGR report is a chance to be off by exactly enough to matter: the final byte is the whole of the
# difference between taking hold and letting go, bit 5 is the whole of the difference between the
# pointer arriving somewhere and the pointer being dragged through it, and the coordinates are
# one-based here and zero-based everywhere above. A swapped pair puts the bar along the screen's top
# row; an unshifted pair puts it one column to the right of the one that was drawn.

at("drag: a release is a press", "internal/term/decode.go",
   "\tcase final == 'm':\n\t\tact = MouseRelease\n", "")

at("drag: motion is arrival", "internal/term/decode.go",
   "\t\tact = MouseDrag\n", "\t\tact = MousePress\n")

at("drag: the middle button is the left one", "internal/term/decode.go",
   "\t\tb = MouseMiddle\n", "\t\tb = MouseLeft\n")

at("drag: the report's cells are taken as they came", "internal/term/decode.go",
   "\tcol, row := params[1]-1, params[2]-1\n", "\tcol, row := params[1], params[2]\n")

at("drag: the row is the column", "internal/term/decode.go",
   "\tcol, row := params[1]-1, params[2]-1\n", "\tcol, row := params[2]-1, params[1]-1\n")

at("drag: a cell off the screen is hit-tested", "internal/term/decode.go",
   "\tif col < 0 || row < 0 {\n", "\tif false {\n")

# And the two reports that are not gestures at all. A notch has no release, so a terminal that sends
# one is describing a scroll that has already been reported; button 3 without the wheel bit is the
# pointer merely passing through, which only 1003 asks for and we ask for 1002. Both are dropped
# here, and a decoder that forwards either turns a mouse being moved across the window into a log
# that scrolls under it or clicks it has not been given.

at("drag: a notch's release is a second notch", "internal/term/decode.go",
   "\t\tif final != 'M' {\n\t\t\treturn nil, n, true\n\t\t}\n", "")

at("drag: a report with no button is a click", "internal/term/decode.go",
   "\t\t// this is a terminal being generous; a drag with nothing held is not a gesture.\n"
   "\t\treturn nil, n, true\n",
   "\t\tb = MouseLeft\n")

at("drag: the wheel's bit 7 is not looked at", "internal/term/decode.go",
   "func isWheel(btn int) bool { return btn >= 0 && btn&0xc0 == 0x40 }\n",
   "func isWheel(btn int) bool { return btn >= 0 && btn&0x40 != 0 }\n")

# The widget, which is where a drag is actually decided. Grab answers "what did that press land on"
# and Offset answers "where does the window go now", and between them they own the two rules a reader
# feels: a press on bare track jumps the pill to the finger, and a press on the pill moves nothing at
# all until the finger does. The row range is Grab's refusal and not app's — app's second copy of it
# was deleted the day this family caught it being dead code — so the mutation that lets a press off
# the end of the track take hold is aimed at the only test of that boundary the tree now has.
#
# Offset's two clamps are deliberately not mutated. min(max(row-grab, 0), h-n) is the domain the three
# cases below it already cover: a negative top and a top past h-n come back out of the switch as 0 and
# d whether they were clamped or not, so every spelling of dropping them is an equivalent mutant, and
# a family entry for a line nothing can observe is a SURVIVED, not a gap. The clamp stays because it
# is what makes the interior's arithmetic true on its face rather than true by argument. The span/2
# rounding is left alone for the reason its own doc gives — where the jumps are long, every offset that
# rounds differently still draws the pill under the finger — and so is the d/2 the interior falls back
# to when the span is nothing, which is the same freedom with the numbers small: a track with two spare
# rows draws its pill on the middle one for every offset between the ends, so which of those offsets a
# press asks for is a choice about what the reader is shown and not about where the pill goes. The
# cases themselves are mutated instead, since those are what a reader reaching for either end of the
# log is aiming at.

mut("drag: a press off the end of the track takes hold", "widget.go",
    "\tif !ok || row < 0 || row >= h {\n", "\tif !ok {\n")

mut("drag: the thumb's last row is bare track", "widget.go",
    "\tif row >= top && row < top+n {\n", "\tif row >= top && row < top+n-1 {\n")

mut("drag: the thumb's first row is bare track", "widget.go",
    "\tif row >= top && row < top+n {\n", "\tif row > top && row < top+n {\n")

mut("drag: the thumb is taken hold of where the track was pressed", "widget.go",
    "\t\treturn row - top, true, true\n", "\t\treturn row, true, true\n")

mut("drag: bare track is the thumb", "widget.go",
    "\treturn n / 2, false, true\n", "\treturn n / 2, true, true\n")

mut("drag: a press on bare track grabs the thumb's first row", "widget.go",
    "\treturn n / 2, false, true\n", "\treturn 0, false, true\n")

mut("drag: a thumb flush at the top is a row down the log", "widget.go",
    "\tcase top <= 0:\n\t\treturn 0, true\n", "\tcase top <= 0:\n\t\treturn 1, true\n")

mut("drag: a thumb flush at the bottom stops a row short of the log's end", "widget.go",
    "\tcase top >= h-n:\n\t\treturn d, true\n", "\tcase top >= h-n:\n\t\treturn d - 1, true\n")

mut("drag: the track's last row is interior", "widget.go",
    "\tcase top >= h-n:\n", "\tcase top > h-n:\n")

mut("drag: the interior's first row is the top of the log", "widget.go",
    "\t\t\tstep = ((top-1)*d + span/2) / span\n", "\t\t\tstep = (top*d + span/2) / span\n")

mut("drag: the interior may answer with an end", "widget.go",
    "\t\treturn min(max(step, 1), d-1), true\n", "\t\treturn step, true\n")

# The renderer's one line, which is the whole of what app is told about where the bar went. Frame.Side
# is a rectangle four numbers wide and each of them is its own way to hit-test the wrong cells: a
# column out and the presses land on the prose, a row out and the pill is grabbed one row from where
# it was drawn, a row too tall and the screen row under the bar takes hold of it. The guard is the
# fifth number: a surface with no column beside it has to hand back an empty rectangle, because "no
# bar here" is spelled with a zero width and app asks the question by comparing against nothing.

at("drag: a surface with no column beside it still says where the strip is", "internal/ui/render.go",
   "\tif w, h := sideWidth(vp), len(window); w > 0 && h > 0 {\n",
   "\tif w, h := sideWidth(vp), len(window); w >= 0 && h > 0 {\n")

at("drag: a frame with no window has a strip with no rows", "internal/ui/render.go",
   "\tif w, h := sideWidth(vp), len(window); w > 0 && h > 0 {\n",
   "\tif w, h := sideWidth(vp), len(window); w > 0 {\n")

at("drag: the strip is drawn at the left edge", "internal/ui/render.go",
   "\t\tf.Side = Rect{X: tw, Y: len(top), W: w, H: h}\n",
   "\t\tf.Side = Rect{X: 0, Y: len(top), W: w, H: h}\n")

at("drag: the strip starts at the top of the screen", "internal/ui/render.go",
   "\t\tf.Side = Rect{X: tw, Y: len(top), W: w, H: h}\n",
   "\t\tf.Side = Rect{X: tw, Y: 0, W: w, H: h}\n")

at("drag: the strip is one column wide", "internal/ui/render.go",
   "\t\tf.Side = Rect{X: tw, Y: len(top), W: w, H: h}\n",
   "\t\tf.Side = Rect{X: tw, Y: len(top), W: 1, H: h}\n")

at("drag: the strip is a row taller than the window it measures", "internal/ui/render.go",
   "\t\tf.Side = Rect{X: tw, Y: len(top), W: w, H: h}\n",
   "\t\tf.Side = Rect{X: tw, Y: len(top), W: w, H: h + 1}\n")

# And app's half: a hit test, a flag and a subtraction. The subtraction is the single line that turns
# the screen's rows into the strip's, and it is exact only because the alternate screen starts Live on
# the screen's first row — the sort of fact that is true until somebody adds a banner. The flag is two
# things at once, the gate that keeps a drag begun on the transcript from stealing the bar when it
# crosses into the column, and the style the pill is drawn held in; both spellings of a stuck flag are
# below. The hit test is the columns alone, because the rows are Grab's.

at("drag: the report's row is the screen's, not the strip's", "internal/app/app.go",
   "\trow := m.Row - a.side.Y\n", "\trow := m.Row\n")

at("drag: every button is the bar's", "internal/app/app.go",
   "\tif m.Button != term.MouseLeft {\n\t\treturn false\n\t}\n", "")

at("drag: the bar owns every column", "internal/app/app.go",
   "\t\tif m.Col < a.side.X || m.Col >= a.side.X+a.side.W {\n", "\t\tif false {\n")

at("drag: a release nobody was holding is ours", "internal/app/app.go",
   "\t\tif !a.dragging {\n\t\t\treturn false\n\t\t}\n\t\ta.dragging = false\n",
   "\t\ta.dragging = false\n")

at("drag: the thumb is never let go of", "internal/app/app.go",
   "\t\ta.dragging = false\n\t\treturn true\n", "\t\treturn true\n")

at("drag: motion is ours whether the press was", "internal/app/app.go",
   "\t\tif !a.dragging {\n\t\t\treturn false\n\t\t}\n\t\treturn a.drag(sb, row)\n",
   "\t\treturn a.drag(sb, row)\n")

at("drag: the thumb is taken hold of at its first row", "internal/app/app.go",
   "\t\ta.dragging, a.grab = true, grab\n", "\t\t_ = grab\n\t\ta.dragging, a.grab = true, 0\n")

at("drag: a press on bare track moves nothing", "internal/app/app.go",
   "\t\tif !onThumb {\n\t\t\ta.drag(sb, row)\n\t\t}\n", "\t\t_ = onThumb\n")

at("drag: a press on the thumb jumps the log", "internal/app/app.go",
   "\t\tif !onThumb {\n\t\t\ta.drag(sb, row)\n\t\t}\n",
   "\t\t_ = onThumb\n\t\ta.drag(sb, row)\n")

at("drag: the window goes where the thumb's top is, not by what it moved", "internal/app/app.go",
   "\treturn a.scroll(above - a.top)\n", "\treturn a.scroll(above)\n")

at("drag: the pill is never drawn held", "internal/app/app.go",
   "ui.ScrollbarWidget{Held: a.dragging}", "ui.ScrollbarWidget{Held: false}")

at("drag: the pill is always drawn held", "internal/app/app.go",
   "ui.ScrollbarWidget{Held: a.dragging}", "ui.ScrollbarWidget{Held: true}")

# The last two are the frame's own report coming back, which is the only way a press can know
# anything about a bar it was never handed. Losing the rectangle refuses every press; losing the
# count of what is below builds the mouse's widget out of a different scroll position than the one
# that was drawn, so the pill the reader is looking at and the pill the hit test is asking about are
# two different pills — the failure that looks exactly like a mis-aimed finger and is not one.

at("drag: the strip's rectangle is never recorded", "internal/app/app.go",
   "\ta.side = f.Side\n", "")

at("drag: nothing below is nothing hidden", "internal/app/app.go",
   "\ta.top, a.rows, a.below = f.Scroll.Above, f.Scroll.Rows, f.Scroll.Below\n",
   "\ta.top, a.rows, a.below = f.Scroll.Above, f.Scroll.Rows, 0\n")

# B2, the team surface. These families protect the roster entering through
# run.started, live agent facts folding by actor, the app-owned Team monitor,
# recorded run metadata, exact context, responsive information rows, and the
# vertical effort selector.

at("team payload: members use the wrong wire name", "internal/event/payload.go",
   'Members      []MemberSpec `json:"members,omitempty"`', 'Members      []MemberSpec `json:"team,omitempty"`')

at("team payload: steer text uses the wrong wire name", "internal/event/payload.go",
   'type AgentSteeredPayload struct {\n\tText string `json:"text"`\n\tTo   string `json:"to,omitempty"`\n}',
   'type AgentSteeredPayload struct {\n\tText string `json:"message"`\n\tTo   string `json:"to,omitempty"`\n}')

at("team payload: notification target uses the wrong wire name", "internal/event/payload.go",
   'type AgentNotifiedPayload struct {\n\tText string `json:"text"`\n\tTo   string `json:"to,omitempty"`\n}',
   'type AgentNotifiedPayload struct {\n\tText string `json:"text"`\n\tTo   string `json:"target,omitempty"`\n}')

at("team payload: failure uses the wrong wire name", "internal/event/payload.go",
   'Error string `json:"error"`', 'Error string `json:"message"`')

at("team roster: run.started discards members", "internal/state/state.go",
   "\t\t\ts.seedMembers(p.Members)\n", "")

at("team roster: duplicate blueprint names survive", "internal/state/state.go",
   'if spec.Name == "" || s.findMember(spec.Name) != nil {', 'if spec.Name == "" {')

at("team roster: unknown actors are not appended", "internal/state/state.go",
   "\ts.Members = append(s.Members, m)\n\treturn m", "\treturn m")

at("team roster: empty actors create nameless members", "internal/state/state.go",
   'if name == "" {\n\t\treturn nil\n\t}\n', "")

at("team fold: activation leaves the member idle", "internal/state/state.go",
   "\t\t\tm.Busy, m.Blocked, m.Error = true, nil, \"\"", "\t\t\tm.Busy, m.Blocked, m.Error = false, nil, \"\"")

at("team fold: turn_done leaves the member busy", "internal/state/state.go",
   "\t\t\tm.Busy, m.Blocked = false, nil", "\t\t\tm.Blocked = nil")

at("team fold: failure drops its explanation", "internal/state/state.go",
   "m.Busy, m.Blocked, m.Error = false, nil, p.Error", "m.Busy, m.Blocked, m.Error = false, nil, \"\"")

at("team fold: a block does not reach the member", "internal/state/apply.go", """\t\tif m := s.member(ev.Actor); m != nil {
\t\t\tm.Busy = true
\t\t\tm.Blocked = &Blocked{Agent: ev.Actor, On: p.BlockedOn, Ref: p.BlockedRef}
\t\t}
""", "")

at("team fold: unblocked closes the resumed turn", "internal/state/apply.go", """\tcase event.AgentUnblocked:
\t\ts.Blocked = nil
\t\tif m := s.member(ev.Actor); m != nil {
\t\t\tm.Blocked = nil
\t\t\tm.Busy = true
\t\t}""", """\tcase event.AgentUnblocked:
\t\ts.Blocked = nil
\t\tif m := s.member(ev.Actor); m != nil {
\t\t\tm.Blocked = nil
\t\t\tm.Busy = false
\t\t}""")

at("team fold: steer text is dropped", "internal/state/apply.go",
   "m.Steered, m.SteeredTo = p.Text, p.To", "m.Steered, m.SteeredTo = \"\", p.To")

at("team fold: notification target is dropped", "internal/state/apply.go",
   "m.Notified, m.NotifiedTo = p.Text, p.To", "m.Notified, m.NotifiedTo = p.Text, \"\"")

mut("team cluster: one member activates the cluster", "widget.go",
    "if len(st.Members) >= 2 {", "if len(st.Members) >= 1 {")

mut("team cluster: blocked loses to busy", "widget.go",
    "case m.Blocked != nil:", "case false && m.Blocked != nil:")

mut("team cluster: busy has a private phase", "widget.go",
    'glyph, style = s.frame(g), "status.member.busy"', 'glyph, style = (StatusWidget{}).frame(g), "status.member.busy"')

mut("team cluster: member names are separated from their glyphs", "widget.go",
    'Span{Text: m.Name, Style: "status.member.name"}', 'Span{Text: " " + m.Name, Style: "status.member.name"}')

at("team monitor: approval remedy names no inbox", "internal/app/overlay.go",
   'return "remedy: arxi inbox approve " + id', 'return "remedy: arxi inbox approve"')

at("team monitor: runtime state ignores failures", "internal/app/overlay.go",
   'case m.Error != "":\n\t\treturn "failed: " + m.Error', 'case false && m.Error != "":\n\t\treturn "failed: " + m.Error')

at("team monitor: ctrl+t opens nothing", "internal/app/keymap.go",
   '"ctrl+t": ActionTeam,', '"ctrl+t": ActionNone,')

at("team monitor: slash team opens nothing", "internal/app/app.go", """\tcase "team":
\t\ta.openTeam()
\t\treturn true""", """\tcase "team":
\t\treturn false""")

# recorded-env: provenance must be decoded from run.started and survive the fold.

at("recorded-env: cwd uses the wrong wire name", "internal/event/payload.go",
   'CWD          string       `json:"cwd,omitempty"`',
   'CWD          string       `json:"workdir,omitempty"`')

at("recorded-env: branch uses the wrong wire name", "internal/event/payload.go",
   'GitBranch    string       `json:"git_branch,omitempty"`',
   'GitBranch    string       `json:"branch,omitempty"`')

at("recorded-env: fold discards provenance", "internal/state/state.go",
   "\t\t\ts.CWD, s.GitBranch = p.CWD, p.GitBranch\n", "")

# context: the pair is optional but exact, validated together, and cleared when omitted.

at("context: used uses the wrong wire name", "internal/event/payload.go",
   'ContextUsed     *int    `json:"context_used,omitempty"`',
   'ContextUsed     *int    `json:"tokens_in,omitempty"`')

at("context: half pairs are accepted", "internal/scenario/validate.go",
   "\t\t\tif (p.ContextUsed == nil) != (p.ContextCapacity == nil) {",
   "\t\t\tif false && (p.ContextUsed == nil) != (p.ContextCapacity == nil) {")

at("context: used may exceed capacity", "internal/scenario/validate.go",
   "} else if *p.ContextUsed > *p.ContextCapacity {",
   "} else if false && *p.ContextUsed > *p.ContextCapacity {")

at("context: omitted response leaves stale occupancy", "internal/state/state.go",
   "\t\t\ts.ContextUsed, s.ContextCapacity = 0, 0\n", "")

at("context: fold estimates occupancy from input tokens", "internal/state/state.go",
   "s.ContextUsed, s.ContextCapacity = *p.ContextUsed, *p.ContextCapacity",
   "s.ContextUsed, s.ContextCapacity = p.TokensIn, *p.ContextCapacity")

# info-zone: viewport height chooses semantic rows, and whole values fit responsively.

mut("info-zone: renderer hides viewport height from widgets", "render.go",
    "wd.Render(w, vp.Height, r.Glyphs)", "wd.Render(w, 0, r.Glyphs)")

mut("info-zone: short screens still draw secondary rows", "widget.go",
    "if h <= 8 {", "if h < 8 {")

mut("info-zone: medium screens draw provenance", "widget.go",
    "if h < 14 {", "if h < 9 {")

mut("info-zone: context percentage uses response tokens", "widget.go",
    "pct := 100 * st.ContextUsed / st.ContextCapacity",
    "pct := 100 * st.TokensIn / st.ContextCapacity")

mut("info-zone: provenance drops its recorded branch", "widget.go",
    'branch := st.GitBranch', 'branch := ""')

# team-view: this is complete-frame app state, not a floating overlay or snapshot.

at("team-view: entry leaves the conversation in control", "internal/app/app.go",
   "\ta.view = viewTeam\n", "\ta.view = viewConversation\n")

at("team-view: draw uses a stale empty Team state", "internal/app/app.go",
   "f := renderTeam(a.st, a.vp, a.teamTop)",
   "f := renderTeam(state.New(), a.vp, a.teamTop)")

at("team-view: escape does not restore conversation", "internal/app/app.go",
   "func (a *App) closeTeam() { a.view = viewConversation }",
   "func (a *App) closeTeam() {}")

at("team-view: printable input reaches the editor", "internal/app/app.go",
   """func (a *App) dispatch(act Action, k term.Key) bool {
	if a.view == viewTeam {
		return a.teamKey(act)
	}""",
   """func (a *App) dispatch(act Action, k term.Key) bool {
	if a.view == viewTeam && act != ActionNone {
		return a.teamKey(act)
	}""")

at("team-view: one-row surface draws no compact summary", "internal/app/team.go",
   "\tif vp.Height == 1 {", "\tif vp.Height == 0 {")

# tasks: live summary, app-owned monitor, routing, glyph states, and local full-view scroll.

mut("tasks widget: empty state still costs a row", "widget.go",
    'if tw.St == nil || len(tw.St.Tasks) == 0 || w <= 0 {',
    'if tw.St == nil || w <= 0 {')

mut("tasks widget: active tasks count as pending", "widget.go",
    'case event.TaskActive:\n\t\t\tactive++',
    'case event.TaskActive:\n\t\t\tpending++')

mut("tasks widget: narrow summary may overflow", "widget.go",
    'if row.Width() <= w {', 'if row.Width() <= w+1 {')

at("tasks monitor: slash tasks opens nothing", "internal/app/app.go", """\tcase "tasks":
\t\ta.openTasks()
\t\treturn true""", """\tcase "tasks":
\t\treturn false""")

at("tasks monitor: draw uses Team instead", "internal/app/app.go",
   'case viewTasks:\n\t\t\tf = renderTasks(a.st, a.vp, a.viewTop, a.r.Glyphs)',
   'case viewTasks:\n\t\t\tf = renderTeam(a.st, a.vp, a.viewTop)')

at("tasks monitor: entry leaves conversation in control", "internal/app/app.go",
   'func (a *App) openTasks() { a.openFullView(viewTasks) }',
   'func (a *App) openTasks() { a.openFullView(viewConversation) }')

at("tasks monitor: escape does not restore conversation", "internal/app/app.go",
   'func (a *App) closeFullView() { a.view = viewConversation }',
   'func (a *App) closeFullView() {}')

at("tasks monitor: printable input reaches editor", "internal/app/app.go", """func (a *App) dispatch(act Action, k term.Key) bool {
\tif a.view != viewConversation {
\t\treturn a.fullViewKey(act)
\t}""", """func (a *App) dispatch(act Action, k term.Key) bool {
\tif a.view != viewConversation && act != ActionNone {
\t\treturn a.fullViewKey(act)
\t}""")

at("tasks monitor: completed status is shown as pending", "internal/app/tasks.go",
   'case event.TaskCompleted:\n\t\treturn "completed", g.Get("tasks.completed"), "tasks.completed"',
   'case event.TaskCompleted:\n\t\treturn "pending", g.Get("tasks.pending"), "tasks.pending"')

at("tasks monitor: one-row surface draws no compact summary", "internal/app/tasks.go",
   '\tif vp.Height == 1 {', '\tif vp.Height == 0 {')

# effort-vertical: order, primary keys, short-height window, and compact width.

at("effort-vertical: down arrow no longer moves", "internal/app/overlay.go",
   "case ActionScrollDownFast, ActionHistoryNext, ActionScrollDown:",
   "case ActionScrollDownFast, ActionScrollDown:")

at("effort-vertical: narrow mode starts too late", "internal/app/overlay.go",
   "compact := w < 24", "compact := w < 20")

at("effort-vertical: short surface keeps chrome over options", "internal/app/overlay.go",
   "\t\t\treserved = 0\n\t\t\tvisible = min(h, len(effortLevels))",
   "\t\t\tvisible = min(1, len(effortLevels))")

at("effort-vertical: selection window starts at the first level", "internal/app/overlay.go",
   "start := e.Selected - visible/2", "start := 0")

at("effort-vertical: selected row loses its marker", "internal/app/overlay.go",
   'marker, markerStyle = "● ", "effort.marker"',
   'marker, markerStyle = "  ", "effort.marker"')

# approval-vertical: each response remains an independently rendered row.

at("approval-vertical: allow and deny share one row", "internal/ui/widget.go",
   '''out = append(out,
		approvalOption("y", "allow"),
		approvalOption("n", "deny"),
	)''',
   '''out = append(out, Line{
		{Text: "  y", Style: "approval.key"},
		{Text: "  allow   n", Style: "approval.hint"},
		{Text: "  deny", Style: "approval.hint"},
	})''')

only = sys.argv[1] if len(sys.argv) > 1 else ""

# --anchors is the sweep's own rot check, and it is worth having because the answer needs no
# test run at all: every mutation above has to find its line exactly once in a pristine tree,
# and a refactor that moves that line unhooks the guard from it without anything going red.
# Learning that six anchors have gone stale is not worth twenty minutes of go test, so this
# asks the question by itself, in a second, and is the thing to run after a rename.
#
# It reads the tree, so the tree has to be pristine: run it while a sweep is in flight and the
# one file that sweep currently has patched will report a stale anchor that is not one.
if only == "--anchors":
    stale = 0
    for label, path, frm, _ in M:
        n = open(path, encoding="utf-8").read().count(frm)
        if n != 1:
            print("SKIP    %-46s (%d matches) %s" % (label, n, os.path.basename(path)))
            stale += 1
    print("\n%d of %d anchors are stale" % (stale, len(M)))
    sys.exit(1 if stale else 0)

run = [m for m in M if only in m[0]]
if not run:
    sys.exit("no mutation's label contains %r; %d are defined" % (only, len(M)))

# The baseline first, and it is not a formality. Every verdict below is "the suite failed, so
# the mutation was noticed", which is only an argument while the suite passes on the untouched
# tree — a tree with one test already failing reports every mutation as caught and proves
# nothing. This has happened twice. Once an interrupted run left a mutant behind in app.go, the
# next run inherited it, and fifty CAUGHT lines all named the same collateral test. The second
# time it outlived the harness entirely: keymap.go's "down" binding stayed on ActionScrollDownFast
# until an ordinary `go test ./...` went red on a tree nobody had edited. One test run buys the
# whole report its meaning back, and a red baseline is worth reading as a leftover before it is
# read as a bug.
print("baseline", end=" ", flush=True)
base = subprocess.run(GOTEST, cwd=ROOT, capture_output=True, text=True)
if base.returncode != 0:
    print("FAILED\n")
    sys.exit(base.stdout + base.stderr +
             "\nthe suite does not pass on the untouched tree, so no mutation below could "
             "mean anything. Fix that first — and if a run was interrupted, look for a "
             "mutant left in place.")
print("ok\n")

fails = 0
for label, path, frm, to in run:
    orig = open(path, encoding="utf-8").read()
    if orig.count(frm) != 1:
        # An anchor that matches twice would mutate an arbitrary one of them, and one that
        # matches none has already been refactored away. Both are the harness rotting, and
        # both count as a failure so that a stale mutation cannot pass as a caught one.
        print("SKIP    %-46s (%d matches)" % (label, orig.count(frm)))
        fails += 1
        continue
    open(path, "w", encoding="utf-8").write(orig.replace(frm, to, 1))
    try:
        p = subprocess.run(GOTEST, cwd=ROOT, capture_output=True, text=True)
    finally:
        open(path, "w", encoding="utf-8").write(orig)
    caught = p.returncode != 0
    names = sorted({l.split()[2].rstrip(":")
                    for l in (p.stdout + p.stderr).splitlines()
                    if l.startswith("--- FAIL")})
    if not caught:
        fails += 1
    # The names are the useful half: they say which test is standing guard over this line,
    # which is what tells you whether the one you just wrote is doing any work.
    print("%-8s %-46s %s" % ("CAUGHT" if caught else "SURVIVED", label,
                             " ".join(names[:6]) or ("panic/build" if caught else "")))

print("\n%d of %d mutations caught" % (len(run) - fails, len(run)))
sys.exit(1 if fails else 0)
