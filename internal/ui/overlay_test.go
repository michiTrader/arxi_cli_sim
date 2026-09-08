package ui

import (
	"strings"
	"testing"
)

func TestOverlayFrameDrawsBorderedBox(t *testing.T) {
	g := DefaultGlyphs()
	o := Overlay{
		Title: "Pick",
		Width: 30,
		Body:  []Line{{{Text: "hello", Style: "overlay.text"}}},
	}
	box := o.OverlayFrame(80, g)
	if len(box) != 3 {
		t.Fatalf("want 3 rows (top + body + bottom), got %d", len(box))
	}
	// Every row must be exactly 30 columns wide.
	for i, row := range box {
		if w := row.Width(); w != 30 {
			t.Errorf("row %d is %d columns wide, want 30", i, w)
		}
	}
	// The title must appear in the top border.
	top := box[0].Text()
	if !strings.Contains(top, "Pick") {
		t.Errorf("top border %q does not contain the title \"Pick\"", top)
	}
}

func TestOverlayFrameClampsToFrameWidth(t *testing.T) {
	g := DefaultGlyphs()
	o := Overlay{
		Title: "",
		Width: 200,
		Body:  []Line{{{Text: "wide", Style: "overlay.text"}}},
	}
	box := o.OverlayFrame(40, g)
	for i, row := range box {
		if w := row.Width(); w != 40 {
			t.Errorf("row %d is %d columns, want 40 (clamped)", i, w)
		}
	}
}

func TestOverlayFrameEmptyBodyReturnsNil(t *testing.T) {
	g := DefaultGlyphs()
	o := Overlay{Title: "X", Width: 30}
	if got := o.OverlayFrame(80, g); got != nil {
		t.Errorf("empty body should return nil, got %d rows", len(got))
	}
}

func TestCompositeStampsOntoLive(t *testing.T) {
	g := DefaultGlyphs()
	// Build a 10-row, 60-column live frame of blank lines.
	live := make([]Line, 10)
	for i := range live {
		live[i] = Line{pad(60)}
	}
	o := &Overlay{
		Title: "Test",
		Width: 20,
		Body:  []Line{{{Text: "item", Style: "overlay.text"}}},
	}
	result := Composite(live, o, 60, g)
	if len(result) != 10 {
		t.Fatalf("composite changed row count: got %d, want 10", len(result))
	}
	// Every row must still be exactly 60 columns.
	for i, row := range result {
		if w := row.Width(); w != 60 {
			t.Errorf("row %d is %d columns, want 60", i, w)
		}
	}
	// The overlay box is 3 rows tall (top + body + bottom), centred vertically
	// in 10 rows, so it starts at row (10-3)/2 = 3. The middle row should
	// contain "item".
	mid := result[4].Text()
	if !strings.Contains(mid, "item") {
		t.Errorf("middle overlay row %q does not contain \"item\"", mid)
	}
}

func TestCompositeNilOverlayIsNoop(t *testing.T) {
	live := []Line{{pad(40)}}
	before := live[0].Text()
	result := Composite(live, nil, 40, DefaultGlyphs())
	if result[0].Text() != before {
		t.Error("nil overlay changed the frame")
	}
}

func TestCompositePreservesSpansOutsideBox(t *testing.T) {
	g := DefaultGlyphs()
	// A live frame with styled content.
	live := make([]Line, 8)
	for i := range live {
		live[i] = Line{{Text: strings.Repeat("x", 60), Style: "prompt.text"}}
	}
	o := &Overlay{
		Width: 20,
		Body:  []Line{{{Text: "hi", Style: "overlay.text"}}},
	}
	result := Composite(live, o, 60, g)
	// Rows not touched by the overlay must keep their original style.
	for _, row := range result[:2] {
		for _, sp := range row {
			if sp.Style != "" && sp.Style != "prompt.text" {
				t.Errorf("untouched row has unexpected style %q", sp.Style)
			}
		}
	}
}
