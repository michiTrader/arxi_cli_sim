package ui

import "sort"

// Glyphs are the characters the interface draws, declared with the same
// discipline as style keys: a closed list with a documented purpose per entry, so
// the whole vocabulary of the interface is one table a user can read and override.
// This is where taste lives. Claude Code introduces a tool result with an elbow,
// pi indents instead; both are one line of configuration apart because neither is
// written into the renderer.

// GlyphDecl is one declared glyph.
type GlyphDecl struct {
	Key      string
	Default  string
	Doc      string
	Fallback string // used when the terminal cannot be trusted with the default
}

// GlyphKeys is every glyph the interface draws.
var GlyphKeys = []GlyphDecl{
	{"prompt.marker", "❯ ", "in front of a prompt the human sent", "> "},
	// Two spaces and no star. A finished thought reads as a sentence — "Thought for 0.5s
	// (high effort)" — and a marker in front of it makes it an entry in a list of events
	// instead, which is what it stopped being once it collapsed to one dim line. The two
	// columns stay because the transcript's whole left edge is two columns wide and the
	// reasoning text hangs under them; a reader who wants the star back writes
	// `thinking.marker = "✻ "` and gets exactly what was here.
	{"thinking.marker", "  ", "in front of a reasoning block; two spaces, so a finished thought reads as a sentence", "  "},
	{"tool.marker", "● ", "in front of a tool call", "o "},
	{"tool.result", "⎿ ", "introduces the result of a tool call", "L "},
	{"tool.result.gap", "…", "stands in for the lines of a result too long to draw", "..."},
	{"notice.marker", "· ", "in front of a runtime remark", "- "},
	{"bullet", "• ", "replaces the dash of a markdown list item", "- "},
	{"code.gutter", "│ ", "beside the lines of a fenced code block", "| "},
	{"quote.bar", "▏ ", "beside the lines of a blockquote, once per level", "> "},
	{"diff.gap", "…", "stands in for the rows between two hunks of one diff", "..."},
	{"input.marker", "❯ ", "in front of what the human is typing", "> "},
	{"approval.marker", "◆ ", "in front of a pending approval", "? "},
	{"frame.h", "─", "the horizontal run of a border", "-"},
	{"frame.v", "│", "the vertical run of a border", "|"},
	{"frame.tl", "╭", "the top-left corner of a border", "+"},
	{"frame.tr", "╮", "the top-right corner of a border", "+"},
	{"frame.bl", "╰", "the bottom-left corner of a border", "+"},
	{"frame.br", "╯", "the bottom-right corner of a border", "+"},
	{"table.cross", "┼", "where a table's rule crosses a column separator", "+"},
	// status.spinner is the one glyph whose value is not a glyph: it is the whole cycle,
	// read a rune at a time, because a spinner is only a spinner if its frames are one
	// choice. Overriding a single frame is meaningless, and ten declared keys would be
	// ten chances to leave a set half-changed.
	{"status.spinner", "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏", "the frames of the status line's spinner, one per rune", `|/-\`},
	{"status.member.blocked", "⧗", "in front of a team member waiting on a dependency", "!"},
	{"status.member.failed", "✗", "in front of a team member whose turn failed", "x"},
	{"status.member.idle", "○", "in front of a team member with no open turn", "o"},
	{"status.sep", "·", "between two segments of the status line", "|"},
	{"tasks.pending", "○", "in front of a pending task", "o"},
	{"tasks.active", "◐", "in front of an active task", ">"},
	{"tasks.completed", "✓", "in front of a completed task", "x"},
	// A light vertical and a full block, so the difference between them is weight rather than
	// shape: the bar is one object seen at two brightnesses, and a reader picks the thumb out of
	// the track without having to compare two symbols. The ASCII pair is the same idea with what
	// a 7-bit font has — a pipe and a hash — and both are one column wide, which the column the
	// bar is drawn in requires.
	{"scroll.track", "│", "the unfilled length of the scrollbar beside the transcript", "|"},
	{"scroll.thumb", "█", "the part of the scrollbar standing for the rows on screen", "#"},
}

var glyphIndex = func() map[string]GlyphDecl {
	m := make(map[string]GlyphDecl, len(GlyphKeys))
	for _, g := range GlyphKeys {
		if _, dup := m[g.Key]; dup {
			panic("ui: glyph declared twice: " + g.Key)
		}
		m[g.Key] = g
	}
	return m
}()

// Glyphs is a resolved glyph set.
type Glyphs struct {
	set     map[string]string
	Track   bool
	used    map[string]int
	unknown map[string]int
}

// NewGlyphs starts from the declared defaults and applies overrides. An override
// for something nothing declares is an error, not a silent no-op.
func NewGlyphs(overrides map[string]string, ascii bool) (Glyphs, error) {
	g := Glyphs{set: make(map[string]string, len(GlyphKeys)), used: map[string]int{}, unknown: map[string]int{}}
	for _, d := range GlyphKeys {
		if ascii {
			g.set[d.Key] = d.Fallback
		} else {
			g.set[d.Key] = d.Default
		}
	}
	for k, v := range overrides {
		if _, ok := glyphIndex[k]; !ok {
			return Glyphs{}, &UndeclaredGlyphError{Key: k}
		}
		g.set[k] = v
	}
	return g, nil
}

// DefaultGlyphs is the shipped vocabulary.
func DefaultGlyphs() Glyphs {
	g, err := NewGlyphs(nil, false)
	if err != nil {
		panic(err)
	}
	return g
}

// UndeclaredGlyphError names a glyph key nothing declares.
type UndeclaredGlyphError struct{ Key string }

func (e *UndeclaredGlyphError) Error() string {
	return "glyph " + e.Key + " is not declared in ui.GlyphKeys"
}

// Get returns a glyph.
func (g Glyphs) Get(key string) string {
	if g.Track {
		if _, ok := glyphIndex[key]; ok {
			g.used[key]++
		} else {
			g.unknown[key]++
		}
	}
	return g.set[key]
}

// Value returns a resolved glyph without recording renderer usage. It is for read-only
// inspectors such as /config; drawing code should use Get so vocabulary tests still track it.
func (g Glyphs) Value(key string) string { return g.set[key] }

// Span returns a glyph already wearing a style.
func (g Glyphs) Span(key, style string) Span { return Span{Text: g.Get(key), Style: style} }

// Used and Unknown mirror Theme's tracking, for the same test.
func (g Glyphs) Used() []string    { return sortedKeys(g.used) }
func (g Glyphs) Unknown() []string { return sortedKeys(g.unknown) }

// GlyphDocs returns the declared glyphs sorted by key, for `theme keys`.
func GlyphDocs() []GlyphDecl {
	out := append([]GlyphDecl(nil), GlyphKeys...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
