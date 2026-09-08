package app

import (
	"fmt"
	"strings"

	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

// appView names the surface that owns the frame and the keyboard.
type appView uint8

const (
	viewConversation appView = iota
	viewTeam
	viewTasks
	viewConfig
)

// renderTeam builds the complete Team surface from current folded state. It owns
// no snapshot: callers pass the state on every draw, so new events appear live.
func renderTeam(st *state.State, vp ui.Viewport, requestedTop int) ui.Frame {
	f := ui.Frame{Width: vp.Width, Height: vp.Height, Cursor: ui.Cursor{Hidden: true}}
	if vp.Height <= 0 {
		return f
	}
	if vp.Height == 1 {
		f.Live = []ui.Line{teamOneLine(st, vp.Width)}
		return f
	}

	body := teamBody(st, vp.Width)
	bodyRows := vp.Height - 2
	if vp.Height == 2 {
		bodyRows = 1
	}
	visible := min(bodyRows, len(body))
	maxTop := max(0, len(body)-visible)
	top := min(max(0, requestedTop), maxTop)
	below := max(0, len(body)-top-visible)
	f.Scroll = ui.Scroll{Above: top, Rows: visible, Below: below}

	if vp.Height >= 3 {
		f.Live = append(f.Live, teamHeader(st, vp.Width))
	}
	f.Live = append(f.Live, body[top:top+visible]...)
	for len(f.Live) < vp.Height-1 {
		f.Live = append(f.Live, ui.Line{})
	}
	f.Live = append(f.Live, teamFooter(vp.Width, top, below))
	return f
}

func teamHeader(st *state.State, width int) ui.Line {
	label := "Team"
	if st != nil {
		if n := len(st.Members); n > 0 {
			label += fmt.Sprintf("  %d members", n)
		}
	}
	return teamFit(ui.Line{{Text: label, Style: "team.name"}}, width)
}

func teamFooter(width, above, below int) ui.Line {
	text := "Esc back"
	if above > 0 || below > 0 {
		text += fmt.Sprintf("  ·  %d above  %d below", above, below)
	}
	return teamFit(ui.Line{{Text: text, Style: "team.meta"}}, width)
}

func teamOneLine(st *state.State, width int) ui.Line {
	working, blocked, failed, members := 0, 0, 0, 0
	if st != nil {
		members = len(st.Members)
		for _, m := range st.Members {
			switch {
			case m.Blocked != nil:
				blocked++
			case m.Error != "":
				failed++
			case m.Busy:
				working++
			}
		}
	}
	text := fmt.Sprintf("Team %d · %d working · %d blocked · %d failed · Esc", members, working, blocked, failed)
	return teamFit(ui.Line{{Text: text, Style: "team.state"}}, width)
}

func teamBody(st *state.State, width int) []ui.Line {
	if width <= 0 {
		return nil
	}
	if st == nil || len(st.Members) == 0 {
		return ui.WrapText("No team roster in this run.", "team.state", width, nil)
	}
	var out []ui.Line
	for i, m := range st.Members {
		if i > 0 {
			out = append(out, ui.Line{})
		}
		out = append(out, teamMemberRows(st.RunID, m, width)...)
	}
	return out
}

func teamMemberRows(runID string, m *state.Member, width int) []ui.Line {
	stateText := memberState(m)
	if width >= 72 {
		first := ui.Line{{Text: m.Name, Style: "team.name"}, {Text: "  "}, {Text: stateText, Style: "team.state"}}
		rows := []ui.Line{teamFit(first, width)}
		if meta := memberMeta(m); meta != "" {
			rows = append(rows, ui.WrapText(meta, "team.meta", width, nil)...)
		}
		if remedy := memberRemedy(runID, m.Blocked); remedy != "" {
			rows = append(rows, ui.WrapText(remedy, "team.remedy", width, nil)...)
		}
		return rows
	}

	rows := ui.WrapSpans([]ui.Span{{Text: m.Name, Style: "team.name"}, {Text: " · "}, {Text: stateText, Style: "team.state"}}, width, nil)
	meta := memberMeta(m)
	if width < 32 {
		meta = shortMemberMeta(m)
	}
	if meta != "" {
		rows = append(rows, ui.WrapText(meta, "team.meta", width, nil)...)
	}
	if remedy := memberRemedy(runID, m.Blocked); remedy != "" {
		rows = append(rows, ui.WrapText(remedy, "team.remedy", width, nil)...)
	}
	return rows
}

func shortMemberMeta(m *state.Member) string {
	var parts []string
	if m.Role != "" {
		parts = append(parts, "role "+m.Role)
	}
	if m.Model != "" {
		parts = append(parts, "model "+m.Model)
	}
	if len(m.Tools) > 0 {
		parts = append(parts, "tools "+strings.Join(m.Tools, ","))
	}
	if m.Activation != "" {
		parts = append(parts, "via "+m.Activation)
	}
	return strings.Join(parts, " · ")
}

func teamFit(line ui.Line, width int) ui.Line {
	if width <= 0 {
		return nil
	}
	if line.Width() <= width {
		return line.TrimRight()
	}
	var out ui.Line
	room := width
	for _, span := range line {
		if room <= 0 {
			break
		}
		text := ansi.Truncate(span.Text, room, "")
		if text == "" {
			continue
		}
		out = append(out, ui.Span{Text: text, Style: span.Style, Fill: span.Fill})
		room -= ansi.StringWidth(text)
	}
	return out.TrimRight()
}
