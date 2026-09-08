package app

import (
	"fmt"
	"strings"

	"arxi.local/sim/internal/event"
	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/ui"
)

// renderTasks builds the app-owned Tasks surface from the current folded state.
// State.Tasks already preserves creation order, so rows need no sorting or snapshot.
func renderTasks(st *state.State, vp ui.Viewport, requestedTop int, g ui.Glyphs) ui.Frame {
	f := ui.Frame{Width: vp.Width, Height: vp.Height, Cursor: ui.Cursor{Hidden: true}}
	if vp.Height <= 0 {
		return f
	}
	if vp.Height == 1 {
		f.Live = []ui.Line{tasksOneLine(st, vp.Width)}
		return f
	}

	body := tasksBody(st, vp.Width, g)
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
		f.Live = append(f.Live, tasksHeader(st, vp.Width))
	}
	f.Live = append(f.Live, body[top:top+visible]...)
	for len(f.Live) < vp.Height-1 {
		f.Live = append(f.Live, ui.Line{})
	}
	f.Live = append(f.Live, tasksFooter(vp.Width, top, below))
	return f
}

func taskCounts(st *state.State) (completed, active, pending, total int) {
	if st == nil {
		return 0, 0, 0, 0
	}
	total = len(st.Tasks)
	for _, task := range st.Tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case event.TaskCompleted:
			completed++
		case event.TaskActive:
			active++
		case event.TaskPending:
			pending++
		}
	}
	return
}

func tasksHeader(st *state.State, width int) ui.Line {
	completed, active, pending, total := taskCounts(st)
	text := fmt.Sprintf("Tasks  %d/%d completed · %d active · %d pending", completed, total, active, pending)
	return teamFit(ui.Line{{Text: text, Style: "tasks.summary"}}, width)
}

func tasksOneLine(st *state.State, width int) ui.Line {
	completed, active, pending, total := taskCounts(st)
	text := fmt.Sprintf("Tasks %d/%d · %d active · %d pending · Esc", completed, total, active, pending)
	return teamFit(ui.Line{{Text: text, Style: "tasks.summary"}}, width)
}

func tasksFooter(width, above, below int) ui.Line {
	text := "Esc back"
	if above > 0 || below > 0 {
		text += fmt.Sprintf("  ·  %d above  %d below", above, below)
	}
	return teamFit(ui.Line{{Text: text, Style: "tasks.meta"}}, width)
}

func tasksBody(st *state.State, width int, g ui.Glyphs) []ui.Line {
	if width <= 0 {
		return nil
	}
	if st == nil || len(st.Tasks) == 0 {
		return ui.WrapText("No tasks in this run.", "tasks.meta", width, nil)
	}
	var out []ui.Line
	for i, task := range st.Tasks {
		if task == nil {
			continue
		}
		if i > 0 && len(out) > 0 {
			out = append(out, ui.Line{})
		}
		out = append(out, taskRows(task, width, g)...)
	}
	return out
}

func taskRows(task *state.Task, width int, g ui.Glyphs) []ui.Line {
	status, glyph, style := taskStatus(task.Status, g)
	lead := []ui.Span{{Text: glyph + " ", Style: style}, {Text: status, Style: style}, {Text: "  "}, {Text: task.Title, Style: "tasks.title"}}
	if width >= 72 {
		if task.Owner != "" {
			lead = append(lead, ui.Span{Text: "  owner " + task.Owner, Style: "tasks.meta"})
		}
		rows := ui.WrapSpans(lead, width, nil)
		if task.Detail != "" {
			rows = append(rows, ui.WrapText(task.Detail, "tasks.detail", width, nil)...)
		}
		return rows
	}

	rows := ui.WrapSpans(lead, width, nil)
	var meta []string
	if task.Owner != "" {
		meta = append(meta, "owner "+task.Owner)
	}
	if task.ID != "" {
		meta = append(meta, "id "+task.ID)
	}
	if len(meta) > 0 {
		rows = append(rows, ui.WrapText(strings.Join(meta, " · "), "tasks.meta", width, nil)...)
	}
	if task.Detail != "" {
		rows = append(rows, ui.WrapText(task.Detail, "tasks.detail", width, nil)...)
	}
	return rows
}

func taskStatus(status event.TaskStatus, g ui.Glyphs) (text, glyph, style string) {
	switch status {
	case event.TaskActive:
		return "active", g.Get("tasks.active"), "tasks.active"
	case event.TaskCompleted:
		return "completed", g.Get("tasks.completed"), "tasks.completed"
	default:
		return "pending", g.Get("tasks.pending"), "tasks.pending"
	}
}
