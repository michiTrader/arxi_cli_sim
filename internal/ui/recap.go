package ui

import (
	"fmt"
	"strings"

	"arxi.local/sim/internal/state"
)

// RecapWidget shows a one-line summary after a response completes, listing what
// the agent did: files read, files edited, tools called. It is installed by the
// app only when state.Recap is true and the last turn has finished.
type RecapWidget struct {
	St *state.State
}

func (RecapWidget) Name() string   { return "recap" }
func (RecapWidget) Slot() Slot     { return SlotAboveInput }
func (RecapWidget) Fallback() Slot { return SlotBelowInput }
func (RecapWidget) Animated() bool { return false }

func (r RecapWidget) Render(w, _ int, g Glyphs) []Line {
	if r.St == nil || w <= 0 {
		return nil
	}
	text := recapText(r.St)
	if text == "" {
		return nil
	}
	return marked(g.Span("notice.marker", "notice.marker"), text, "status.dim", w)
}

// recapText builds the recap from the most recent response (the items after the
// last prompt). It counts tool calls by type and reports a compact summary.
func recapText(st *state.State) string {
	// Walk backwards to find the last prompt, then scan forward from it.
	lastPrompt := -1
	for i := len(st.Items) - 1; i >= 0; i-- {
		if st.Items[i].Kind == state.KindPrompt {
			lastPrompt = i
			break
		}
	}
	if lastPrompt < 0 {
		return ""
	}

	var reads, edits, writes, bashes, others int
	var textLen int
	var thinking bool

	for i := lastPrompt + 1; i < len(st.Items); i++ {
		it := st.Items[i]
		switch it.Kind {
		case state.KindTool:
			switch strings.ToLower(it.Tool) {
			case "read":
				reads++
			case "edit":
				edits++
			case "write":
				writes++
			case "bash":
				bashes++
			default:
				others++
			}
		case state.KindText:
			textLen += len(it.Text)
		case state.KindThinking:
			thinking = true
		}
	}

	var parts []string
	if thinking {
		parts = append(parts, "thought")
	}
	if reads > 0 {
		parts = append(parts, fmt.Sprintf("%d read", reads))
	}
	if edits > 0 {
		parts = append(parts, fmt.Sprintf("%d edit", edits))
	}
	if writes > 0 {
		parts = append(parts, fmt.Sprintf("%d write", writes))
	}
	if bashes > 0 {
		parts = append(parts, fmt.Sprintf("%d bash", bashes))
	}
	if others > 0 {
		parts = append(parts, fmt.Sprintf("%d tool", others))
	}
	if len(parts) == 0 {
		if textLen > 0 {
			return "responded"
		}
		return ""
	}
	return strings.Join(parts, ", ")
}
