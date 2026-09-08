package ui

// Layout is where a widget's request to live somewhere meets what the terminal can
// actually do. The slot names are relative to the input, because the input is the
// one thing always on screen and the only fixed point inline mode gives us.
//
// A slot is not a mode. Render never asks "am I on the alternate screen"; it asks
// "can I hold a fixed right column", and the Viewport answers. That is the whole
// reason the same renderer serves both modes.

// Slot is where a widget asks to be drawn.
type Slot string

// The declared slots.
const (
	SlotTop        Slot = "top"
	SlotLeft       Slot = "left"
	SlotRight      Slot = "right"
	SlotBottom     Slot = "bottom"
	SlotAboveInput Slot = "above_input"
	SlotBelowInput Slot = "below_input"
	SlotInputLeft  Slot = "input_left"
	SlotInputRight Slot = "input_right"
	SlotFull       Slot = "full"
)

// SlotDecl documents a slot and what the terminal must be able to do to honour it.
type SlotDecl struct {
	Slot      string
	Doc       string
	NeedsSide bool // a column beside the transcript, which scrollback cannot hold
	NeedsTop  bool // a row above the transcript that stays put while it scrolls
	Degrades  string
}

// SlotKeys is every slot a widget may ask for.
var SlotKeys = []SlotDecl{
	{"top", "a fixed row above everything", false, true, "drawn once, above the transcript"},
	{"left", "a fixed column left of the transcript", true, false, "dropped, or its Fallback slot"},
	{"right", "a fixed column right of the transcript", true, false, "dropped, or its Fallback slot"},
	{"bottom", "the last row of the frame, under the input", false, false, ""},
	{"above_input", "between the transcript and the input", false, false, ""},
	{"below_input", "between the input and the bottom row", false, false, ""},
	{"input_left", "inside the input, left of the cursor line", false, false, ""},
	{"input_right", "inside the input, right of the cursor line", false, false, ""},
	{"full", "the whole frame, owned by an app-level view", true, true, "unavailable; the renderer does not compose full-frame widgets"},
}

var slotIndex = func() map[Slot]SlotDecl {
	m := make(map[Slot]SlotDecl, len(SlotKeys))
	for _, d := range SlotKeys {
		if _, dup := m[Slot(d.Slot)]; dup {
			panic("ui: slot declared twice: " + d.Slot)
		}
		m[Slot(d.Slot)] = d
	}
	return m
}()

// Available reports whether this viewport can honour the slot. An unavailable slot
// is not an error: the widget either names a Fallback or is left out of the frame,
// and `arxi-sim theme keys` prints the Degrades column so the behaviour is
// documented rather than discovered.
func (s Slot) Available(vp Viewport) bool {
	d, ok := slotIndex[s]
	if !ok {
		return false
	}
	if d.NeedsSide && !vp.SidePanels {
		return false
	}
	if d.NeedsTop && !vp.FixedTop {
		return false
	}
	return true
}

// Declared reports whether the slot is on the list.
func (s Slot) Declared() bool {
	_, ok := slotIndex[s]
	return ok
}

// SideCols is how wide a reserved side column is: one column of ink and one of air, so the
// transcript's longest line never touches the thing beside it. Two is also the narrowest a
// column can be and still be a column — one would be a bar welded to the prose — and it is
// spent by the layout rather than by the widget, which only ever sees the width it was given.
const SideCols = 2

// TranscriptWidth is the width the transcript is wrapped to: the frame's width, less whatever
// a side column has taken out of it.
//
// It is a function of the viewport alone, and deliberately not of what the caller happens to
// have installed. A width that depended on whether a bar was *drawn* would be circular — a
// narrower transcript is a taller one, a taller one is a scrollbar, and a scrollbar is a
// narrower transcript — so the wrap point would oscillate at whatever width the log is exactly
// one screen long. A width that depended on whether a bar was *installed* would be worse in a
// quieter way: it re-wraps the entire conversation on the frame the bar appears, so every line
// of a screen the reader is in the middle of moves sideways at the moment they scroll. The
// column is therefore reserved by the surface that says it can hold one, painted or not, and
// the two columns cost the same on every frame of the session.
//
// A document reserves nothing. vp.Height <= 0 is the caller asking for the whole transcript
// rather than a screen — a fold, a pipe, a golden file — and there is no window there to be a
// fraction of, so there is nothing a bar could say. Fold keeps SidePanels and only zeroes the
// height, which is exactly why this clause is here: without it every golden file at a hundred
// columns would wrap two columns early to leave room for a bar nobody was going to draw.
//
// The last guard is not defensive. A one- or two-column terminal exists — it is a drag caught
// mid-flight — and a transcript zero columns wide is not a narrower transcript, it is no
// transcript at all. There the reservation is refused and the prose keeps the whole width;
// sideWidth then reports nothing to fill, so the column is dropped instead of drawn over the
// only text on the screen.
func TranscriptWidth(vp Viewport) int {
	if vp.Height <= 0 || !SlotRight.Available(vp) || vp.Width <= SideCols {
		return vp.Width
	}
	return vp.Width - SideCols
}

// sideWidth is how many columns the layout took out of the transcript for a side column: either
// SideCols or nothing at all.
//
// It is asked as a difference rather than kept as a second constant so the two answers cannot
// disagree. Everything that fills the column measures it this way, which means a frame that
// reserved nothing has nothing to fill, and one that reserved two columns fills exactly the two
// the prose gave up — with no third place where the same rule is spelled out and could be spelled
// out differently.
func sideWidth(vp Viewport) int { return vp.Width - TranscriptWidth(vp) }
