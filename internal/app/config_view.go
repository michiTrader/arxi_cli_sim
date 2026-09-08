package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

// ConfigID identifies one setting across the controller and full-frame view.
type ConfigID string

const (
	ConfigInputTitle  ConfigID = "input.title"
	ConfigScrollLines ConfigID = "scroll.lines"
	ConfigMouse       ConfigID = "scroll.mouse"
	ConfigShine       ConfigID = "anim.shine"
	ConfigPeriod      ConfigID = "anim.period"
	ConfigTravel      ConfigID = "anim.travel"
	ConfigWidth       ConfigID = "anim.width"
)

// ErrConfigDirty asks the view for explicit confirmation before reloading.
var ErrConfigDirty = errors.New("config draft has unsaved changes")

type ConfigValueKind uint8

const (
	ConfigText ConfigValueKind = iota
	ConfigInt
	ConfigBool
	ConfigReadOnly
)

type ConfigRow struct {
	ID                                                        ConfigID
	Label, Value, Persisted, Effective, Source, Apply, Detail string
	Kind                                                      ConfigValueKind
	Dirty, Masked                                             bool
}

type ConfigCategory struct {
	Name string
	Rows []ConfigRow
}

type ConfigSnapshot struct {
	Path, Runtime      string
	SaveEnabled, Dirty bool
	Categories         []ConfigCategory
}

// ConfigController is implemented by internal/config without making app import it.
type ConfigController interface {
	Snapshot() ConfigSnapshot
	Set(ConfigID, string) error
	Save() error
	Cancel()
	Reload(discard bool) error
}

// configViewState belongs to the surface, while values and persistence belong to the controller.
type configViewState struct {
	category      int
	row           int
	detail        bool
	editID        ConfigID
	editor        *ui.Input
	errorText     string
	confirmReload bool
	rowTops       []int
	rowEnds       []int
}

func newConfigViewState() *configViewState { return &configViewState{editor: ui.NewInput()} }

func (a *App) openConfig() {
	if a.cfg.Config == nil {
		return
	}
	a.configView = newConfigViewState()
	a.openFullView(viewConfig)
}

func (a *App) closeConfig() {
	if a.cfg.Config != nil {
		a.cfg.Config.Cancel()
		a.applyConfigSnapshot(a.cfg.Config.Snapshot())
	}
	a.configView = nil
	a.closeFullView()
}

func (a *App) applyConfigSnapshot(s ConfigSnapshot) {
	for _, category := range s.Categories {
		for _, row := range category.Rows {
			if row.Masked || row.Kind == ConfigReadOnly {
				continue
			}
			switch row.ID {
			case ConfigInputTitle:
				a.ed.Title = row.Effective
				a.cfg.InputTitle = row.Effective
			case ConfigScrollLines:
				if n, err := strconv.Atoi(row.Effective); err == nil && n > 0 {
					a.cfg.WheelLines = n
				}
			case ConfigShine:
				if on, err := strconv.ParseBool(row.Effective); err == nil {
					if on {
						a.cfg.Shine.Style = ui.InputShine
					} else {
						a.cfg.Shine.Style = ""
					}
				}
			case ConfigPeriod:
				if n, err := strconv.Atoi(row.Effective); err == nil {
					a.cfg.Shine.Period = n
				}
			case ConfigTravel:
				if n, err := strconv.Atoi(row.Effective); err == nil {
					a.cfg.Shine.Travel = n
				}
			case ConfigWidth:
				if n, err := strconv.Atoi(row.Effective); err == nil {
					a.cfg.Shine.Width = n
				}
			}
		}
	}
}

func (a *App) configSnapshot() ConfigSnapshot {
	if a.cfg.Config == nil {
		return ConfigSnapshot{}
	}
	s := a.cfg.Config.Snapshot()
	s.Categories = append([]ConfigCategory(nil), s.Categories...)
	for i := range s.Categories {
		s.Categories[i].Rows = append([]ConfigRow(nil), s.Categories[i].Rows...)
	}
	for i := range s.Categories {
		if s.Categories[i].Name != "Session" {
			continue
		}
		for j := range s.Categories[i].Rows {
			switch s.Categories[i].Rows[j].ID {
			case "session.effort":
				s.Categories[i].Rows[j].Value, s.Categories[i].Rows[j].Effective = a.st.Effort, a.st.Effort
			case "session.recap":
				v := strconv.FormatBool(a.st.Recap)
				s.Categories[i].Rows[j].Value, s.Categories[i].Rows[j].Effective = v, v
			}
		}
	}
	return s
}

func (v *configViewState) clamp(s ConfigSnapshot) {
	if len(s.Categories) == 0 {
		v.category, v.row = 0, 0
		return
	}
	v.category = min(max(0, v.category), len(s.Categories)-1)
	rows := s.Categories[v.category].Rows
	if len(rows) == 0 {
		v.row = 0
	} else {
		v.row = min(max(0, v.row), len(rows)-1)
	}
}

func configPlainRune(k term.Key) (rune, bool) {
	if k.Type != term.KeyRunes || k.Mod != 0 || len(k.Runes) != 1 {
		return 0, false
	}
	return k.Runes[0], true
}

func (a *App) configKey(act Action, k term.Key) bool {
	v := a.configView
	if v == nil || a.cfg.Config == nil {
		return false
	}
	s := a.configSnapshot()
	v.clamp(s)
	if v.editID != "" {
		return a.configEditKey(act, k)
	}
	if v.confirmReload {
		if r, ok := configPlainRune(k); ok && (r == 'y' || r == 'Y') {
			v.confirmReload = false
			if err := a.cfg.Config.Reload(true); err != nil {
				v.errorText = err.Error()
			} else {
				v.errorText = ""
				a.applyConfigSnapshot(a.cfg.Config.Snapshot())
			}
			return true
		}
		if act == ActionCancel || act == ActionInterrupt || act == ActionSubmit || (k.Type == term.KeyRunes) {
			v.confirmReload = false
			return true
		}
		return false
	}
	if r, ok := configPlainRune(k); ok {
		switch r {
		case 's', 'S':
			if !s.SaveEnabled {
				v.errorText = "Save is disabled for this run."
				return true
			}
			if err := a.cfg.Config.Save(); err != nil {
				v.errorText = err.Error()
			} else {
				v.errorText = "Saved."
				a.applyConfigSnapshot(a.cfg.Config.Snapshot())
			}
			return true
		case 'r', 'R':
			if err := a.cfg.Config.Reload(false); errors.Is(err, ErrConfigDirty) {
				v.confirmReload = true
			} else if err != nil {
				v.errorText = err.Error()
			} else {
				v.errorText = "Reloaded."
				a.applyConfigSnapshot(a.cfg.Config.Snapshot())
			}
			return true
		}
	}
	wide := a.vp.Width >= 88
	switch act {
	case ActionCancel, ActionInterrupt:
		if v.detail && !wide {
			v.detail = false
			v.row = 0
			return true
		}
		a.closeConfig()
		return true
	case ActionHistoryPrev, ActionScrollUp:
		if wide || v.detail {
			if v.row > 0 {
				v.row--
				return true
			}
		} else if v.category > 0 {
			v.category--
			return true
		}
	case ActionHistoryNext, ActionScrollDown:
		if wide || v.detail {
			if v.row+1 < len(s.Categories[v.category].Rows) {
				v.row++
				return true
			}
		} else if v.category+1 < len(s.Categories) {
			v.category++
			v.row = 0
			return true
		}
	case ActionPageUp:
		return a.configMove(-max(1, a.viewRows-1), s, wide)
	case ActionPageDown:
		return a.configMove(max(1, a.viewRows-1), s, wide)
	case ActionHome, ActionScrollTop:
		if wide || v.detail {
			v.row = 0
		} else {
			v.category = 0
		}
		return true
	case ActionEnd, ActionScrollBottom:
		if wide || v.detail {
			v.row = max(0, len(s.Categories[v.category].Rows)-1)
		} else {
			v.category = max(0, len(s.Categories)-1)
		}
		return true
	case ActionLeft, ActionRight:
		return a.configAdjust(act == ActionRight, s)
	case ActionSubmit:
		if !wide && !v.detail {
			v.detail = true
			v.row = 0
			return true
		}
		return a.configActivate(s)
	case ActionScrollUpFast:
		return a.configMove(-max(1, a.cfg.WheelLines), s, wide)
	case ActionScrollDownFast:
		return a.configMove(max(1, a.cfg.WheelLines), s, wide)
	}
	return false
}

func (a *App) configMove(n int, s ConfigSnapshot, wide bool) bool {
	v := a.configView
	if wide || v.detail {
		if len(s.Categories[v.category].Rows) == 0 {
			return false
		}
		to := min(max(0, v.row+n), len(s.Categories[v.category].Rows)-1)
		if to == v.row {
			return false
		}
		v.row = to
		return true
	}
	to := min(max(0, v.category+n), len(s.Categories)-1)
	if to == v.category {
		return false
	}
	v.category = to
	v.row = 0
	return true
}

func (a *App) configSelected(s ConfigSnapshot) (ConfigRow, bool) {
	v := a.configView
	v.clamp(s)
	if len(s.Categories) == 0 || len(s.Categories[v.category].Rows) == 0 {
		return ConfigRow{}, false
	}
	return s.Categories[v.category].Rows[v.row], true
}

func (a *App) configAdjust(right bool, s ConfigSnapshot) bool {
	row, ok := a.configSelected(s)
	if !ok {
		return false
	}
	value := ""
	switch row.Kind {
	case ConfigBool:
		value = strconv.FormatBool(!strings.EqualFold(row.Value, "true"))
	case ConfigInt:
		n, err := strconv.Atoi(row.Value)
		if err != nil {
			return false
		}
		if right {
			n++
		} else if n > 1 {
			n--
		} else {
			return false
		}
		value = strconv.Itoa(n)
	default:
		return false
	}
	if err := a.cfg.Config.Set(row.ID, value); err != nil {
		a.configView.errorText = err.Error()
	} else {
		a.configView.errorText = ""
		a.applyConfigSnapshot(a.cfg.Config.Snapshot())
	}
	return true
}

func (a *App) configActivate(s ConfigSnapshot) bool {
	row, ok := a.configSelected(s)
	if !ok || row.Kind == ConfigReadOnly {
		return false
	}
	if row.ID == "session.effort" {
		return a.configCycleEffort(row.Value)
	}
	if row.ID == "session.recap" {
		a.st.Recap = !a.st.Recap
		return true
	}
	if row.Kind == ConfigBool {
		return a.configAdjust(true, s)
	}
	a.configView.editID = row.ID
	a.configView.editor.SetText(row.Value)
	a.configView.errorText = ""
	return true
}

func (a *App) configCycleEffort(current string) bool {
	idx := -1
	for i, v := range effortLevels {
		if v == current {
			idx = i
			break
		}
	}
	a.st.Effort = effortLevels[(idx+1)%len(effortLevels)]
	return true
}

func (a *App) configEditKey(act Action, k term.Key) bool {
	v := a.configView
	switch act {
	case ActionCancel, ActionInterrupt:
		v.editID = ""
		v.errorText = ""
		return true
	case ActionSubmit:
		if err := a.cfg.Config.Set(v.editID, v.editor.Text()); err != nil {
			v.errorText = err.Error()
			return true
		}
		v.editID = ""
		v.errorText = ""
		a.applyConfigSnapshot(a.cfg.Config.Snapshot())
		return true
	case ActionBackspace:
		v.editor.Backspace()
	case ActionDelete:
		v.editor.Delete()
	case ActionKillWord:
		v.editor.KillWord()
	case ActionKillToEnd:
		v.editor.KillToEnd()
	case ActionKillLine:
		v.editor.KillLine()
	case ActionLeft:
		v.editor.Left()
	case ActionRight:
		v.editor.Right()
	case ActionWordLeft:
		v.editor.WordLeft()
	case ActionWordRight:
		v.editor.WordRight()
	case ActionHome:
		v.editor.Home()
	case ActionEnd:
		v.editor.End()
	default:
		if k.Type == term.KeyRunes && k.Mod == 0 {
			v.editor.Insert(string(k.Runes))
			return true
		}
		return false
	}
	return true
}

func renderConfig(s ConfigSnapshot, v *configViewState, vp ui.Viewport) ui.Frame {
	f := ui.Frame{Width: vp.Width, Height: vp.Height, Cursor: ui.Cursor{Hidden: true}}
	if vp.Height <= 0 || v == nil {
		return f
	}
	v.clamp(s)
	if vp.Height == 1 {
		status := "clean"
		if s.Dirty {
			status = "unsaved"
		}
		if v.errorText != "" {
			status = "error"
		}
		f.Live = []ui.Line{teamFit(ui.Line{{Text: "Config · " + status + " · Esc", Style: "tasks.summary"}}, vp.Width)}
		return f
	}
	body, cursor := configBody(s, v, vp.Width)
	rows := vp.Height - 2
	if vp.Height == 2 {
		rows = 1
	}
	selectedTop, selectedEnd := 0, 1
	if len(v.rowTops) > 0 {
		i := min(v.row, len(v.rowTops)-1)
		selectedTop = v.rowTops[i]
		selectedEnd = selectedTop + 1
		if i < len(v.rowEnds) {
			selectedEnd = v.rowEnds[i]
		}
	}
	maxTop := max(0, len(body)-rows)
	top := min(max(0, selectedTop), maxTop)
	if selectedEnd-top > rows {
		top = min(maxTop, max(0, selectedEnd-rows))
	}
	visible := min(rows, max(0, len(body)-top))
	below := max(0, len(body)-top-visible)
	f.Scroll = ui.Scroll{Above: top, Rows: visible, Below: below}
	if vp.Height >= 3 {
		f.Live = append(f.Live, configHeader(s, v, vp.Width))
	}
	if visible > 0 {
		f.Live = append(f.Live, body[top:top+visible]...)
	}
	for len(f.Live) < vp.Height-1 {
		f.Live = append(f.Live, ui.Line{})
	}
	f.Live = append(f.Live, configFooter(s, v, vp.Width, top, below))
	if v.editID != "" && cursor.Line >= top && cursor.Line < top+visible {
		base := 0
		if vp.Height >= 3 {
			base = 1
		}
		f.Cursor = ui.Cursor{Line: base + cursor.Line - top, Col: cursor.Col}
	}
	return f
}

func configHeader(s ConfigSnapshot, v *configViewState, width int) ui.Line {
	status := ""
	if s.Dirty {
		status = "  unsaved"
	}
	if v.confirmReload {
		status = "  discard draft? y/N"
	}
	return teamFit(ui.Line{{Text: "Config", Style: "tasks.summary"}, {Text: status, Style: "notice.warn"}}, width)
}

func configFooter(s ConfigSnapshot, v *configViewState, width, above, below int) ui.Line {
	text := "Esc back  ·  Enter open/edit  ·  ←/→ adjust"
	if s.SaveEnabled {
		text += "  ·  s save"
	}
	text += "  ·  r reload"
	if above > 0 || below > 0 {
		text += fmt.Sprintf("  ·  %d above %d below", above, below)
	}
	return teamFit(ui.Line{{Text: text, Style: "tasks.meta"}}, width)
}

func configBody(s ConfigSnapshot, v *configViewState, width int) ([]ui.Line, ui.Cursor) {
	v.rowTops = nil
	v.rowEnds = nil
	cursor := ui.Cursor{Hidden: true}
	if width <= 0 {
		return nil, cursor
	}
	var out []ui.Line
	if v.errorText != "" {
		out = append(out, ui.WrapText(v.errorText, "notice.warn", width, nil)...)
		out = append(out, ui.Line{})
	}
	wide := width >= 88
	if !wide && !v.detail {
		for i, c := range s.Categories {
			mark := "  "
			style := "tasks.title"
			if i == v.category {
				mark = "› "
				style = "overlay.highlight"
			}
			out = append(out, teamFit(ui.Line{{Text: mark + c.Name, Style: style}}, width))
		}
		return out, cursor
	}
	if wide {
		return configWideBody(s, v, width, out)
	}
	category := s.Categories[v.category]
	out = append(out, teamFit(ui.Line{{Text: category.Name, Style: "team.name"}}, width))
	for i, row := range category.Rows {
		v.rowTops = append(v.rowTops, len(out))
		lines, cur := configRowLines(row, i == v.row, v, width)
		if v.editID == row.ID {
			cur.Line += len(out)
			cursor = cur
		}
		out = append(out, lines...)
		v.rowEnds = append(v.rowEnds, len(out))
	}
	return out, cursor
}

func configWideBody(s ConfigSnapshot, v *configViewState, width int, prefix []ui.Line) ([]ui.Line, ui.Cursor) {
	left := min(22, max(16, width/4))
	right := width - left - 3
	cursor := ui.Cursor{Hidden: true}
	category := s.Categories[v.category]
	detail := []ui.Line{{{Text: category.Name, Style: "team.name"}}}
	for i, row := range category.Rows {
		v.rowTops = append(v.rowTops, len(detail))
		lines, cur := configRowLines(row, i == v.row, v, right)
		if v.editID == row.ID {
			cur.Line += len(detail)
			cur.Col += left + 3
			cursor = cur
		}
		detail = append(detail, lines...)
		v.rowEnds = append(v.rowEnds, len(detail))
	}
	height := max(len(s.Categories), len(detail))
	out := prefix
	for i := 0; i < height; i++ {
		var line ui.Line
		if i < len(s.Categories) {
			mark := "  "
			style := "tasks.title"
			if i == v.category {
				mark = "› "
				style = "overlay.highlight"
			}
			line = append(line, ui.Span{Text: ansi.Truncate(mark+s.Categories[i].Name, left, ""), Style: style})
		}
		if pad := left - line.Width(); pad > 0 {
			line = append(line, ui.Span{Text: strings.Repeat(" ", pad)})
		}
		line = append(line, ui.Span{Text: " │ ", Style: "tasks.meta"})
		if i < len(detail) {
			line = append(line, detail[i]...)
		}
		out = append(out, teamFit(line, width))
	}
	for i := range v.rowTops {
		v.rowTops[i] += len(prefix)
		v.rowEnds[i] += len(prefix)
	}
	return out, cursor
}

func configEditorWindow(in *ui.Input, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	text := in.Text()
	cursor := in.CursorColumn()
	start := 0
	if cursor >= width {
		start = cursor - width + 1
	}
	visible := ansi.Cut(text, start, start+width)
	return visible, min(width-1, max(0, cursor-start))
}

func configRowLines(row ConfigRow, selected bool, v *configViewState, width int) ([]ui.Line, ui.Cursor) {

	mark := "  "
	labelStyle := "tasks.title"
	if selected {
		mark = "› "
		labelStyle = "overlay.highlight"
	}
	value := row.Value
	cursor := ui.Cursor{Hidden: true}
	if v.editID == row.ID {
		value = v.editor.Text()
	}
	label := mark + row.Label + "  "
	rows := []ui.Line{teamFit(ui.Line{{Text: label, Style: labelStyle}, {Text: value, Style: "team.state"}}, width)}
	if v.editID == row.ID {
		labelWidth := ansi.StringWidth(label)
		room := max(1, width-labelWidth)
		text, col := configEditorWindow(v.editor, room)
		rows[0] = teamFit(ui.Line{{Text: label, Style: labelStyle}, {Text: text, Style: "team.state"}}, width)
		cursor = ui.Cursor{Line: 0, Col: min(max(0, width-1), labelWidth+col)}
	}
	meta := row.Source + " · " + row.Apply
	if row.Masked {
		meta += " · saved value masked by CLI"
	} else if row.Dirty {
		meta += " · draft"
	}
	if row.Persisted != "" && row.Persisted != row.Effective {
		meta += " · persisted " + row.Persisted + " · effective " + row.Effective
	}
	rows = append(rows, ui.WrapText(meta, "tasks.meta", width, nil)...)
	if row.Detail != "" {
		rows = append(rows, ui.WrapText(row.Detail, "tasks.detail", width, nil)...)
	}
	return rows, cursor
}
