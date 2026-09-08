package app

import (
	"strings"

	"arxi.local/sim/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

// slashEntry is one command in the slash menu.
type slashEntry struct {
	Name string // without the leading /
	Desc string
	Hint string // grey usage example shown after the description
}

// slashRegistry is every command the menu can suggest. It is a package-level
// slice rather than a map because the order is the display order: the human
// reads top to bottom, and that order is ours to choose.
var slashRegistry = []slashEntry{
	{"effort", "Set thinking effort level", "[low|medium|high|xhigh|max|ultracode|auto]"},
	{"config", "Edit configuration and inspect effective values", ""},
	{"team", "Show the team roster and live state", ""},
	{"tasks", "Show the task list and live status", ""},
	{"recap", "Toggle recap line after responses", ""},
}

// slashMenuRows is how many rows the dropdown shows before it scrolls.
const slashMenuRows = 5

// slashMenu is the dropdown's own state: which commands currently match what
// has been typed, and which one is selected.
type slashMenu struct {
	items  []slashEntry
	sel    int
	offset int // first visible row
}

// slashMenuFor derives the dropdown from the current input. It returns an
// empty menu (Active() == false) when the line does not look like a command
// name being typed: it does not start with "/", it already has a space, or
// nothing matches.
func slashMenuFor(text string, prev slashMenu) slashMenu {
	if len(text) == 0 || text[0] != '/' || strings.ContainsAny(text, " \t\n") {
		return slashMenu{}
	}
	prefix := strings.ToLower(text[1:])
	var items []slashEntry
	for _, e := range slashRegistry {
		if strings.HasPrefix(e.Name, prefix) {
			items = append(items, e)
		}
	}
	if len(items) == 0 {
		return slashMenu{}
	}
	sel := prev.sel
	if sel < 0 || sel >= len(items) {
		sel = 0
	}
	return slashMenu{items: items, sel: sel}
}

// Active reports whether the dropdown has anything to draw.
func (m slashMenu) Active() bool { return len(m.items) > 0 }

// Selected returns the command the current selection points at.
func (m slashMenu) Selected() slashEntry { return m.items[m.sel] }

// moveDown and moveUp cycle the selection, wrapping at either end.
func (m slashMenu) moveDown() slashMenu { return m.move(1) }
func (m slashMenu) moveUp() slashMenu   { return m.move(-1) }

func (m slashMenu) move(delta int) slashMenu {
	if len(m.items) == 0 {
		return m
	}
	n := len(m.items)
	m.sel = ((m.sel+delta)%n + n) % n
	// Keep the selection visible.
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if m.sel >= m.offset+slashMenuRows {
		m.offset = m.sel - slashMenuRows + 1
	}
	return m
}

// SlashMenuWidget draws the suggestion dropdown below the input.
type SlashMenuWidget struct {
	Menu slashMenu
}

func (SlashMenuWidget) Name() string   { return "slashmenu" }
func (SlashMenuWidget) Slot() Slot     { return ui.SlotBelowInput }
func (SlashMenuWidget) Fallback() Slot { return "" }
func (SlashMenuWidget) Animated() bool { return false }

// Slot is a type alias for the widget interface.
type Slot = ui.Slot

func (s SlashMenuWidget) Render(w, _ int, g ui.Glyphs) []ui.Line {
	m := s.Menu
	if !m.Active() || w < 10 {
		return nil
	}

	// Left pad to align the / with the input text inside the box.
	// The input box has: border(1) + air(1) + marker("❯ "=2) = 4 columns.
	markerW := ansi.StringWidth(g.Get("input.marker"))
	lpad := 1 + 1 + markerW // border + air + marker
	if lpad < 1 {
		lpad = 1
	}
	padStr := strings.Repeat(" ", lpad)

	// Visible window.
	items := m.items
	offset := m.offset
	if len(items) > slashMenuRows {
		if offset < 0 {
			offset = 0
		}
		if offset > len(items)-slashMenuRows {
			offset = len(items) - slashMenuRows
		}
		items = items[offset : offset+slashMenuRows]
	} else {
		offset = 0
	}

	// Measure the widest command name.
	nameW := 0
	for _, e := range items {
		if n := len(e.Name) + 1; n > nameW { // +1 for the /
			nameW = n
		}
	}

	out := make([]ui.Line, len(items))
	for i, e := range items {
		name := "/" + e.Name
		gap := nameW - len(name)
		if gap < 0 {
			gap = 0
		}

		var row ui.Line
		if offset+i == m.sel {
			row = ui.Line{
				{Text: padStr},
				{Text: name, Style: "overlay.highlight"},
				{Text: strings.Repeat(" ", gap+2)},
				{Text: e.Desc, Style: "overlay.highlight"},
			}
			if e.Hint != "" {
				row = append(row, ui.Span{Text: "  "}, ui.Span{Text: e.Hint, Style: "status.dim"})
			}
			row = append(row, ui.Span{Text: " "})
		} else {
			row = ui.Line{
				{Text: padStr},
				{Text: name, Style: "overlay.text"},
				{Text: strings.Repeat(" ", gap+2)},
				{Text: e.Desc, Style: "status.dim"},
			}
			if e.Hint != "" {
				row = append(row, ui.Span{Text: "  "}, ui.Span{Text: e.Hint, Style: "status.dim"})
			}
			row = append(row, ui.Span{Text: " "})
		}
		out[i] = row
	}
	return out
}
