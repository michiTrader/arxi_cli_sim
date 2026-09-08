package app

import (
	"fmt"
	"sort"
	"strings"

	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/term"
	"arxi.local/sim/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

// OverlayHandler is the interface an overlay's key handler must satisfy. The
// overlay system routes keys here while the overlay is open; when Handle
// returns done=true the overlay is dismissed.
type OverlayHandler interface {
	Handle(act Action, k term.Key) (dirty, done bool)
	Build() *ui.Overlay
}

// memberMeta joins blueprint metadata for the dedicated Team monitor. A member
// discovered only from agent events has no blueprint metadata to imply.
func memberMeta(m *state.Member) string {
	var parts []string
	if m.Role != "" {
		parts = append(parts, "role "+m.Role)
	}
	if m.Model != "" {
		parts = append(parts, "model "+m.Model)
	}
	if m.Advisory {
		parts = append(parts, "advisory yes")
	}
	if len(m.Tools) > 0 {
		parts = append(parts, "tools "+strings.Join(m.Tools, ","))
	}
	if m.Activation != "" {
		parts = append(parts, "activation "+m.Activation)
	}
	if len(m.Stages) > 0 {
		parts = append(parts, "stages "+strings.Join(m.Stages, ","))
	}
	return strings.Join(parts, " · ")
}

func memberState(m *state.Member) string {
	switch {
	case m.Blocked != nil:
		return "blocked on " + valueOr(m.Blocked.On, "unknown")
	case m.Error != "":
		return "failed: " + m.Error
	case m.Busy:
		return "working"
	case m.Steered != "":
		return detailVerb("steered", m.Steered, m.SteeredTo)
	case m.Notified != "":
		return detailVerb("notified", m.Notified, m.NotifiedTo)
	default:
		return "idle"
	}
}

func detailVerb(verb, text, to string) string {
	if to != "" {
		verb += " to " + to
	}
	if text != "" {
		verb += ": " + text
	}
	return verb
}

func memberRemedy(runID string, b *state.Blocked) string {
	if b == nil {
		return ""
	}
	ref := b.Ref
	switch b.On {
	case "approval":
		if id := refString(ref, "inbox_id"); id != "" {
			return "remedy: arxi inbox approve " + id
		}
	case "lock":
		if key := refString(ref, "key"); key != "" {
			return "remedy: arxi state unlock " + valueOr(runID, "<run>") + " " + key
		}
	case "budget":
		return "remedy: arxi run unpause " + valueOr(runID, "<run>") + " --budget <higher>"
	case "workspace":
		return "remedy: arxi run show " + valueOr(runID, "<run>") + " --workspace"
	case "peer":
		return "waiting for peer " + valueOr(refString(ref, "peer"), "(unknown)")
	case "timer":
		return "waiting for timer " + valueOr(refString(ref, "timer_id"), "(unknown)")
	case "tool":
		return "waiting for tool " + valueOr(refString(ref, "tool"), "(unknown)")
	}
	return "blocked reference: " + refSummary(ref)
}

func refString(ref map[string]any, key string) string {
	if v, ok := ref[key].(string); ok {
		return v
	}
	return ""
}

func refSummary(ref map[string]any) string {
	if len(ref) == 0 {
		return "schema violation: missing structured reference"
	}
	keys := make([]string, 0, len(ref))
	for key := range ref {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, ref[key]))
	}
	return strings.Join(parts, ", ")
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func listOr(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return strings.Join(values, ",")
}

// effortLevels is the scale the effort slider shows, in display order.
var effortLevels = []string{"low", "medium", "high", "xhigh", "max", "ultracode"}

// EffortSlider holds the state of the inline effort bar while it is open. It is
// not an overlay any more — it renders as a Widget in SlotBelowInput — but it
// still implements the same Handle protocol so the dispatch in app.go can route
// keys identically.
type EffortSlider struct {
	Selected int
	Width    int
	Phase    int // animation tick, advanced by the app's wall clock
}

// NewEffortSlider returns a slider initialised to the given level, or to
// "high" if the level is not on the scale.
func NewEffortSlider(current string, width int) *EffortSlider {
	idx := 2 // default to "high"
	for i, l := range effortLevels {
		if l == current {
			idx = i
			break
		}
	}
	if width < 30 {
		width = 30
	}
	return &EffortSlider{Selected: idx, Width: width}
}

// Handle processes a key inside the effort slider.
func (e *EffortSlider) Handle(act Action, k term.Key) (dirty, done bool) {
	switch act {
	case ActionScrollUpFast, ActionHistoryPrev, ActionScrollUp:
		if e.Selected > 0 {
			e.Selected--
			return true, false
		}
		return false, false
	case ActionScrollDownFast, ActionHistoryNext, ActionScrollDown:
		if e.Selected < len(effortLevels)-1 {
			e.Selected++
			return true, false
		}
		return false, false
	case ActionLeft:
		if e.Selected > 0 {
			e.Selected--
			return true, false
		}
		return false, false
	case ActionRight:
		if e.Selected < len(effortLevels)-1 {
			e.Selected++
			return true, false
		}
		return false, false
	case ActionSubmit:
		return true, true
	case ActionCancel, ActionInterrupt:
		e.Selected = -1 // signal: cancelled
		return true, true
	}
	return false, false
}

// Level returns the selected effort level, or "" if cancelled.
func (e *EffortSlider) Level() string {
	if e.Selected < 0 || e.Selected >= len(effortLevels) {
		return ""
	}
	return effortLevels[e.Selected]
}

// Build is kept so that EffortSlider still satisfies OverlayHandler for the
// overlay compositing path. The effort slider no longer uses the overlay — it
// is drawn as a widget — so Build returns nil.
func (e *EffortSlider) Build() *ui.Overlay { return nil }

// EffortWidget wraps an open EffortSlider and draws it as a Widget in
// SlotBelowInput. The horizontal bar is the Claude Code style: a track that
// fills from the left edge out to a position marker, with Faster/Smarter
// labels, level names, a description, and a hint row.
type EffortWidget struct {
	Slider *EffortSlider
}

func (EffortWidget) Name() string      { return "effort" }
func (EffortWidget) Slot() ui.Slot     { return ui.SlotBelowInput }
func (EffortWidget) Fallback() ui.Slot { return "" }

// Animated returns true while the selected level is max or ultracode, where a
// decorative animation plays.
func (ew EffortWidget) Animated() bool {
	if ew.Slider == nil {
		return false
	}
	return ew.Slider.Selected >= 4 // max or ultracode
}

// Render draws a vertical effort selector. On short surfaces it keeps a window
// around the selected level; on very narrow ones the option rows are the entire UI.
func (ew EffortWidget) Render(w, h int, g ui.Glyphs) []ui.Line {
	e := ew.Slider
	if e == nil || w <= 2 {
		return nil
	}
	compact := w < 24
	reserved := 0
	if !compact {
		reserved = 3 // heading, description, hint
	}
	visible := len(effortLevels)
	if h > 0 && visible+reserved > h {
		if h <= reserved {
			reserved = 0
			visible = min(h, len(effortLevels))
		} else {
			visible = h - reserved
		}
	}
	if visible < 1 {
		return nil
	}
	start := e.Selected - visible/2
	if start < 0 {
		start = 0
	}
	if end := start + visible; end > len(effortLevels) {
		start = max(0, len(effortLevels)-visible)
	}

	margin := 2
	if compact {
		margin = 1
	}
	pad := strings.Repeat(" ", margin)
	var rows []ui.Line
	if reserved > 0 {
		rows = append(rows, ui.Line{{Text: pad}, {Text: "Thinking effort", Style: "effort.label"}})
	}
	for i := start; i < start+visible; i++ {
		marker, markerStyle := "  ", "effort.level"
		if i == e.Selected {
			marker, markerStyle = "● ", "effort.marker"
		}
		row := ui.Line{{Text: pad}, {Text: marker, Style: markerStyle}}
		row = append(row, effortNameSpans(e, i)...)
		if !compact && i == e.Selected {
			gap := "  "
			room := w - row.Width() - len(gap)
			if room > 8 {
				row = append(row, ui.Span{Text: gap}, ui.Span{Text: ansi.Truncate(effortDesc(i), room, ""), Style: "effort.desc"})
			}
		}
		rows = append(rows, row.TrimRight())
	}
	if reserved > 0 {
		rows = append(rows,
			ui.Line{{Text: pad}, {Text: effortDesc(e.Selected), Style: "effort.desc"}}.TrimRight(),
			ui.Line{{Text: pad}, {Text: "↑/↓ move · Enter confirm · Esc cancel", Style: "effort.hint"}}.TrimRight(),
		)
	}
	return rows
}

func effortNameSpans(e *EffortSlider, level int) ui.Line {
	name := effortLevels[level]
	if level != e.Selected || (level != 4 && level != 5) {
		style := "effort.level"
		if level == e.Selected {
			style = "effort.selected"
		}
		return ui.Line{{Text: name, Style: style}}
	}
	out := ui.Line{}
	runes := []rune(name)
	for i, r := range runes {
		style := specAt(e.Phase/2 + i*len(effortSpectrum)/len(runes))
		if level == 5 {
			style = waveAt(e.Phase/2 + i*len(effortWave)/len(runes))
		}
		out = append(out, ui.Span{Text: string(r), Style: style})
	}
	return out
}

func effortDesc(idx int) string {
	if idx < 0 || idx >= len(effortLevels) {
		return ""
	}
	descs := []string{
		"Minimal thinking. Fastest responses.",
		"Light thinking for straightforward tasks.",
		"Balanced thinking for most tasks.",
		"Extended thinking for complex tasks.",
		"Maximum thinking. May use excessive tokens.",
		"xhigh + workflows. Use sparingly for the hardest tasks.",
	}
	return descs[idx]
}

// effortSpectrum is the twelve rainbow keys in spectral order: six hues with
// the blend between each pair, so stepping along the slice is a gradient and
// not a jump. effortWave is the ultracode counterpart — five purples folded
// into one symmetric cycle, out to bright and back, for the same reason.
var effortSpectrum = []string{
	"effort.rainbow.r", "effort.rainbow.ro", "effort.rainbow.o", "effort.rainbow.oy",
	"effort.rainbow.y", "effort.rainbow.yg", "effort.rainbow.g", "effort.rainbow.gb",
	"effort.rainbow.b", "effort.rainbow.bp", "effort.rainbow.p", "effort.rainbow.pr",
}
var effortWave = []string{
	"effort.ultra.deep", "effort.ultra.deepMid", "effort.ultra.mid", "effort.ultra.midBright",
	"effort.ultra.bright", "effort.ultra.midBright", "effort.ultra.mid", "effort.ultra.deepMid",
}

// specAt and waveAt answer a key by index in the cycle, wrapping: the phase
// only ever grows, so a cell's index passes a full lap within a few seconds
// of the slider being open.
func specAt(i int) string { return effortSpectrum[mod(i, len(effortSpectrum))] }
func waveAt(i int) string { return effortWave[mod(i, len(effortWave))] }

func mod(i, n int) int {
	if i < 0 {
		i += n
	}
	return i % n
}
