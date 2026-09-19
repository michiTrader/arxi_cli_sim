package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ExtRole is the closed semantic vocabulary available to extension content.
// Extensions describe meaning; UI alone chooses theme keys.
type ExtRole uint8

const (
	ExtText ExtRole = iota
	ExtMuted
	ExtAccent
	ExtSuccess
	ExtWarning
	ExtError
	ExtKey
)

// ExtSpan and ExtRow are declarative extension content. Render treats their
// values as immutable and translates roles to UI-owned styles.
type ExtSpan struct {
	Text string
	Role ExtRole
}

type ExtRow struct {
	Spans []ExtSpan
}

// ExtPanel is a pure full-frame surface. It has no process or protocol state;
// callers replace this value when extension output changes.
type ExtPanel struct {
	Title     string
	Rows      []ExtRow
	Footer    []ExtSpan
	Focused   bool
	Stale     bool
	ScrollTop int
}

var extRoleKeys = [...]string{
	"extension.text",
	"extension.muted",
	"extension.accent",
	"extension.success",
	"extension.warning",
	"extension.error",
	"extension.key",
}

func (r ExtRole) style() string {
	if int(r) < 0 || int(r) >= len(extRoleKeys) {
		return "extension.text"
	}
	return extRoleKeys[r]
}

// Render returns a frame of exactly width by height cells. Content wraps by
// terminal display width, is clipped to the body window, and never overflows.
func (p ExtPanel) Render(width, height int) Frame {
	f := Frame{Width: max(0, width), Height: max(0, height), Cursor: Cursor{Hidden: true}}
	if width <= 0 || height <= 0 {
		return f
	}

	frameStyle := "extension.frame"
	titleStyle := "extension.title"
	if p.Focused {
		frameStyle, titleStyle = "extension.frame.focused", "extension.title.focused"
	}
	inner := max(0, width-2)
	top := extRule(width, p.Title, frameStyle, titleStyle)
	bottom := extRule(width, extFooterText(p), frameStyle, "extension.footer")
	if width == 1 {
		f.Live = make([]Line, height)
		for i := range f.Live {
			f.Live[i] = Line{{Text: "│", Style: frameStyle}}
		}
		f.Live[0] = top
		if height > 1 {
			f.Live[height-1] = bottom
		}
		return f
	}
	if height == 1 {
		f.Live = []Line{top}
		return f
	}

	bodyHeight := height - 2
	body := p.body(inner)
	first := min(max(0, p.ScrollTop), max(0, len(body)-bodyHeight))
	keep := min(bodyHeight, len(body)-first)
	f.Scroll = Scroll{Above: first, Rows: bodyHeight, Below: len(body) - first - keep}
	f.Live = make([]Line, 0, height)
	f.Live = append(f.Live, top)
	for i := 0; i < bodyHeight; i++ {
		line := Line{{Text: "│", Style: frameStyle}}
		if first+i < len(body) {
			line = append(line, body[first+i]...)
		}
		line = append(line, Span{Text: strings.Repeat(" ", inner-line.Width()+1)}, Span{Text: "│", Style: frameStyle})
		f.Live = append(f.Live, line)
	}
	f.Live = append(f.Live, bottom)
	return f
}

func (p ExtPanel) body(width int) []Line {
	if width <= 0 {
		return nil
	}
	var out []Line
	for _, row := range p.Rows {
		spans := make([]Span, 0, len(row.Spans))
		for _, sp := range row.Spans {
			spans = append(spans, Span{Text: sp.Text, Style: sp.Role.style()})
		}
		out = append(out, WrapSpans(spans, width, nil)...)
	}
	return out
}

func extFooterText(p ExtPanel) Line {
	var out Line
	if p.Stale {
		out = append(out, Span{Text: " stale ", Style: "extension.stale"})
	}
	for _, sp := range p.Footer {
		out = append(out, Span{Text: sp.Text, Style: sp.Role.style()})
	}
	return out
}

func extRule(width int, content any, frameStyle, contentStyle string) Line {
	line := Line{{Text: "┌", Style: frameStyle}}
	close := "┐"
	if l, ok := content.(Line); ok {
		line = append(line, l...)
		close = "┘"
	} else if text, ok := content.(string); ok && text != "" {
		line = append(line, Span{Text: " " + text + " ", Style: contentStyle})
	}
	room := width - line.Width() - 1
	if room < 0 {
		flat := ansi.Truncate(line.Text(), width-1, "")
		line = Line{{Text: flat, Style: contentStyle}}
		room = width - line.Width() - 1
	}
	if room > 0 {
		line = append(line, Span{Text: strings.Repeat("─", room), Style: frameStyle})
	}
	if line.Width() < width {
		line = append(line, Span{Text: close, Style: frameStyle})
	}
	return line
}
