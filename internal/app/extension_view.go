package app

import (
	"sort"
	"strings"

	"arxi.local/sim/internal/ext/viewmodel"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
)

type extensionPanel struct {
	extension string
	view      viewmodel.View
}

func panelKey(extension, id string) string { return extension + ":" + id }

func (a *App) panelMessage(m ExtensionMessage) bool {
	changed := false
	if m.View != nil {
		key := panelKey(m.Extension, m.View.ID)
		v := *m.View
		v.Rows = append([]viewmodel.Row(nil), m.View.Rows...)
		a.extPanels[key] = extensionPanel{extension: m.Extension, view: v}
		changed = true
	}
	if m.ViewClosed != "" {
		changed = a.removePanel(panelKey(m.Extension, m.ViewClosed)) || changed
	}
	if m.Removed {
		for key, panel := range a.extPanels {
			if panel.extension == m.Extension {
				changed = a.removePanel(key) || changed
			}
		}
	}
	return changed
}

func (a *App) removePanel(key string) bool {
	if _, ok := a.extPanels[key]; !ok {
		return false
	}
	delete(a.extPanels, key)
	if a.extPanel == key {
		a.blurPanel()
		a.closeFullView()
	}
	if a.extCapture == key {
		a.extCapture = ""
	}
	return true
}

func (a *App) panelNames() []string {
	out := make([]string, 0, len(a.extPanels))
	for key := range a.extPanels {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (a *App) openPanel(name string) bool {
	if name == "" {
		names := a.panelNames()
		if len(names) == 0 {
			return false
		}
		name = names[0]
	}
	if _, ok := a.extPanels[name]; !ok {
		return false
	}
	if a.extPanel != "" && a.extPanel != name {
		a.blurPanel()
	}
	a.extPanel = name
	a.openFullView(viewExtension)
	p := a.extPanels[name]
	a.cfg.Extensions.ViewFocus(p.extension, p.view.ID)
	a.sendPanelResize()
	return true
}

func (a *App) blurPanel() {
	if p, ok := a.extPanels[a.extPanel]; ok && a.cfg.Extensions != nil {
		a.cfg.Extensions.ViewBlur(p.extension, p.view.ID)
	}
	a.extPanel, a.extCapture = "", ""
}

func (a *App) sendPanelResize() {
	p, ok := a.extPanels[a.extPanel]
	if !ok || a.cfg.Extensions == nil {
		return
	}
	w, h := a.vp.Width, a.vp.Height
	if p.view.Width == w && p.view.Height == h {
		return
	}
	key := panelKey(p.extension, p.view.ID)
	if a.extResize[key] == [2]int{w, h} {
		return
	}
	if a.cfg.Extensions.ViewResize(p.extension, p.view.ID, w, h) {
		a.extResize[key] = [2]int{w, h}
	}
}

func (a *App) renderPanel() ui.Frame {
	p, ok := a.extPanels[a.extPanel]
	if !ok {
		return ui.Frame{Width: a.vp.Width, Height: a.vp.Height, Cursor: ui.Cursor{Hidden: true}}
	}
	rows := make([]ui.ExtRow, len(p.view.Rows))
	for i, row := range p.view.Rows {
		for _, span := range row.Spans {
			rows[i].Spans = append(rows[i].Spans, ui.ExtSpan{Text: span.Text, Role: panelRole(span.Role)})
		}
	}
	return (ui.ExtPanel{Title: a.extPanel, Rows: rows, Focused: true, Stale: p.view.Width != a.vp.Width || p.view.Height != a.vp.Height}).Render(a.vp.Width, a.vp.Height)
}

func panelRole(role viewmodel.Role) ui.ExtRole {
	switch role {
	case viewmodel.RoleMuted:
		return ui.ExtMuted
	case viewmodel.RoleEmphasis, viewmodel.RoleTitle:
		return ui.ExtAccent
	case viewmodel.RoleSuccess:
		return ui.ExtSuccess
	case viewmodel.RoleWarning:
		return ui.ExtWarning
	case viewmodel.RoleError:
		return ui.ExtError
	case viewmodel.RoleCode:
		return ui.ExtKey
	default:
		return ui.ExtText
	}
}

func (a *App) panelInput(in viewmodel.Input) bool {
	p, ok := a.extPanels[a.extPanel]
	if !ok || a.view != viewExtension || a.cfg.Extensions == nil {
		return false
	}
	in.Width, in.Height = a.vp.Width, a.vp.Height
	return a.cfg.Extensions.ViewInput(p.extension, p.view.ID, in)
}

func (a *App) panelKey(act Action, k term.Key) bool {
	if act == ActionCancel || act == ActionInterrupt {
		a.blurPanel()
		a.closeFullView()
		return true
	}
	if act != ActionNone {
		return a.panelInput(viewmodel.Input{Kind: "action", Key: k.String(), Action: string(act)})
	}
	if k.Type == term.KeyRunes && k.Mod == 0 {
		return a.panelInput(viewmodel.Input{Kind: "text", Key: k.String(), Text: string(k.Runes)})
	}
	return a.panelInput(viewmodel.Input{Kind: "key", Key: k.String()})
}

func (a *App) panelMouse(m term.Mouse) bool {
	key := a.extPanel
	if m.Action != term.MousePress && a.extCapture != "" {
		key = a.extCapture
	}
	if key == "" || a.view != viewExtension {
		return false
	}
	if m.Action == term.MousePress {
		a.extCapture = key
	}
	kind := map[term.MouseAction]string{term.MousePress: "pointer-press", term.MouseDrag: "pointer-drag", term.MouseRelease: "pointer-release"}[m.Action]
	ok := a.panelInput(viewmodel.Input{Kind: kind, X: m.Col, Y: m.Row})
	if m.Action == term.MouseRelease {
		a.extCapture = ""
	}
	return ok
}

func parsePanelName(s string) string { return strings.TrimSpace(s) }
