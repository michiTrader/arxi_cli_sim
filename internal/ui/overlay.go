package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Overlay is a floating box drawn on top of the frame after the frame is
// assembled. It uses the same border glyphs and drawing vocabulary as the input
// box — rule, hrun, the frame.* glyph keys — so an overlay looks like a second
// instance of the input's chrome rather than a new kind of surface.
//
// Only one overlay is active at a time: the app sets it before draw, and the
// renderer composites it onto Live in one pass. That limit is a simplification,
// not a shortcut — a stack of overlays is two z-orders, a focus ring and an
// occlusion policy, and the return on that is a second floating box nobody has
// asked for yet.
type Overlay struct {
	Title string
	Body  []Line

	// Width is how wide the box wants to be, borders included. The compositor
	// clamps it to the frame and centres the result, so a caller can ask for
	// its ideal width and never overflow.
	Width int
}

// OverlayFrame renders the overlay into a slice of Lines ready for compositing:
// a top border, the body rows wrapped in verticals, and a bottom border. The
// returned lines are exactly as wide as the box, which is min(o.Width, frameW).
func (o Overlay) OverlayFrame(frameW int, g Glyphs) []Line {
	if len(o.Body) == 0 || frameW <= 0 {
		return nil
	}
	w := o.Width
	if w <= 0 || w > frameW {
		w = frameW
	}

	v := g.Span("frame.v", "overlay.frame")
	vw := ansi.StringWidth(v.Text)
	inner := w - 2*vw
	if inner < 1 {
		return nil
	}

	out := make([]Line, 0, len(o.Body)+2)
	out = append(out, overlayRule("frame.tl", "frame.tr", o.Title, w, g))
	for _, bl := range o.Body {
		// Truncate or pad each body line to exactly inner columns.
		bw := bl.Width()
		row := Line{v, pad(1)}
		usable := inner - 1 // one column of air after the left vertical
		if bw <= usable {
			row = append(row, bl...)
			row = append(row, pad(usable-bw))
		} else {
			// Truncate: walk spans until we fill usable columns.
			col := 0
			for _, sp := range bl {
				sw := ansi.StringWidth(sp.Text)
				if col+sw <= usable {
					row = append(row, sp)
					col += sw
				} else {
					trunc := ansi.Truncate(sp.Text, usable-col, "")
					if trunc != "" {
						row = append(row, Span{Text: trunc, Style: sp.Style, Fill: sp.Fill})
						col += ansi.StringWidth(trunc)
					}
					if p := usable - col; p > 0 {
						row = append(row, pad(p))
					}
					break
				}
			}
		}
		row = append(row, v)
		out = append(out, row)
	}
	out = append(out, overlayRule("frame.bl", "frame.br", "", w, g))
	return out
}

// overlayRule draws one horizontal of the overlay box, identical to the input's
// rule but with the overlay.frame fill so the border can carry its own
// background.
func overlayRule(left, right, title string, width int, g Glyphs) Line {
	l, r := g.Span(left, "overlay.frame"), g.Span(right, "overlay.frame")
	mid := width - ansi.StringWidth(l.Text) - ansi.StringWidth(r.Text)
	if mid < 0 {
		return Line{}
	}
	out := Line{l}
	lead := max(1, ansi.StringWidth(g.Get("frame.h")))
	if t := " " + title + " "; title != "" && ansi.StringWidth(t)+2*lead <= mid {
		out = append(out, overlayHrun(lead, g), Span{Text: t, Style: "overlay.title"})
		mid -= lead + ansi.StringWidth(t)
	}
	if mid > 0 {
		out = append(out, overlayHrun(mid, g))
	}
	return append(out, r)
}

// overlayHrun is hrun but with the overlay's frame style.
func overlayHrun(n int, g Glyphs) Span {
	h := fill(g.Get("frame.h"), n)
	if w := ansi.StringWidth(h); w < n {
		h += strings.Repeat(" ", n-w)
	}
	return Span{Text: h, Style: "overlay.frame"}
}

// Composite stamps a rendered overlay onto live, centred horizontally and
// vertically. It overwrites the spans that fall under the box and returns the
// modified slice. If the overlay is nil or empty, live is returned unchanged.
func Composite(live []Line, o *Overlay, frameW int, g Glyphs) []Line {
	if o == nil || len(o.Body) == 0 || frameW <= 0 || len(live) == 0 {
		return live
	}
	box := o.OverlayFrame(frameW, g)
	if len(box) == 0 {
		return live
	}

	boxW := box[0].Width()
	xOff := (frameW - boxW) / 2
	if xOff < 0 {
		xOff = 0
	}
	yOff := (len(live) - len(box)) / 2
	if yOff < 0 {
		yOff = 0
	}

	for i, row := range box {
		y := yOff + i
		if y >= len(live) {
			break
		}
		live[y] = stamp(live[y], row, xOff, frameW)
	}
	return live
}

// stamp replaces the columns [xOff, xOff+overlay.Width()) of dst with the
// overlay row, preserving any spans before and after the box.
func stamp(dst, overlay Line, xOff, frameW int) Line {
	ovW := overlay.Width()
	if ovW == 0 {
		return dst
	}

	out := make(Line, 0, len(dst)+len(overlay)+2)
	col := 0

	// Spans before the overlay.
	for _, sp := range dst {
		sw := ansi.StringWidth(sp.Text)
		if col+sw <= xOff {
			out = append(out, sp)
			col += sw
			continue
		}
		if col < xOff {
			// Partially before: keep the left slice.
			keep := xOff - col
			head := ansi.Truncate(sp.Text, keep, "")
			if head != "" {
				out = append(out, Span{Text: head, Style: sp.Style, Fill: sp.Fill})
			}
		}
		break
	}

	// Pad if dst was too short to reach xOff.
	if outW := lineWidth(out); outW < xOff {
		out = append(out, pad(xOff-outW))
	}

	// The overlay itself.
	out = append(out, overlay...)

	// Spans after the overlay.
	after := xOff + ovW
	col = 0
	for _, sp := range dst {
		sw := ansi.StringWidth(sp.Text)
		if col+sw <= after {
			col += sw
			continue
		}
		if col < after {
			// Partially inside: skip the covered part.
			skip := after - col
			full := sp.Text
			head := ansi.Truncate(full, skip, "")
			tail := ""
			if head != "" && len(head) < len(full) {
				tail = full[len(head):]
			}
			if tail != "" {
				out = append(out, Span{Text: tail, Style: sp.Style, Fill: sp.Fill})
			}
			col += sw
			continue
		}
		out = append(out, sp)
		col += sw
	}

	return out
}

func lineWidth(l Line) int {
	w := 0
	for _, sp := range l {
		w += ansi.StringWidth(sp.Text)
	}
	return w
}
