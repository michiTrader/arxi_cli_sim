package ui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateExtPanel = flag.Bool("update-ext-panel", false, "rewrite extension panel golden files")

func extPanelFixture() ExtPanel {
	return ExtPanel{
		Title: "Deploy 日本", Focused: true,
		Rows: []ExtRow{
			{Spans: []ExtSpan{{Text: "Running ", Role: ExtText}, {Text: "health checks", Role: ExtAccent}}},
			{Spans: []ExtSpan{{Text: "✓ api 東京", Role: ExtSuccess}}},
			{Spans: []ExtSpan{{Text: "Latency is above the preferred threshold", Role: ExtWarning}}},
			{Spans: []ExtSpan{{Text: "Press ", Role: ExtMuted}, {Text: "r", Role: ExtKey}, {Text: " to retry", Role: ExtMuted}}},
		},
		Footer: []ExtSpan{{Text: " ↑↓ scroll ", Role: ExtMuted}},
	}
}

func TestExtPanelGoldens(t *testing.T) {
	cases := []struct {
		name  string
		w, h  int
		top   int
		stale bool
	}{
		{"focused", 32, 8, 0, false},
		{"stale", 32, 8, 0, true},
		{"narrow", 12, 10, 0, false},
		{"short", 24, 2, 0, false},
		{"unicode", 18, 6, 0, false},
		{"scrolling", 24, 5, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := extPanelFixture()
			p.ScrollTop, p.Stale = tc.top, tc.stale
			f := p.Render(tc.w, tc.h)
			if f.Width != tc.w || f.Height != tc.h || len(f.Live) != tc.h {
				t.Fatalf("geometry = %dx%d with %d rows, want %dx%d", f.Width, f.Height, len(f.Live), tc.w, tc.h)
			}
			if !f.Cursor.Hidden || len(f.Overflow()) != 0 {
				t.Fatalf("unsafe frame: cursor=%+v overflow=%v", f.Cursor, f.Overflow())
			}
			got := f.Plain() + "\n"
			path := filepath.Join("testdata", "extension-panel."+tc.name+".txt")
			if *updateExtPanel || *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run: go test ./internal/ui -run ExtPanel -update)", err)
			}
			if got != string(want) {
				t.Errorf("frame differs from %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
			}
		})
	}
}

func TestExtPanelScrollMetadataAndImmutability(t *testing.T) {
	p := extPanelFixture()
	before := p.Rows[0].Spans[0].Text
	p.ScrollTop = 999
	f := p.Render(16, 5)
	if f.Scroll.Rows != 3 || f.Scroll.Below != 0 || f.Scroll.Above == 0 {
		t.Fatalf("scroll = %+v", f.Scroll)
	}
	if p.Rows[0].Spans[0].Text != before {
		t.Fatal("render mutated declarative rows")
	}
}

func TestExtPanelDegenerateGeometry(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {-1, 2}, {1, 1}, {1, 4}, {2, 3}} {
		f := (ExtPanel{Title: "界", Rows: []ExtRow{{Spans: []ExtSpan{{Text: "界界"}}}}}).Render(size[0], size[1])
		if len(f.Overflow()) != 0 || !f.Cursor.Hidden {
			t.Fatalf("%v: overflow=%v cursor=%+v", size, f.Overflow(), f.Cursor)
		}
	}
}
