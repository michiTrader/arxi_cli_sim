package app

// The output level is ctrl+o's question with three answers. These tests drive it the
// way the keymap does — one keypress per step, the frame as the only witness — and
// hold the three promises the feature makes: a session opens compact, the cycle walks
// compact, standard, full and back, the notice naming the level is a transient on the
// input's border, and the change never moves a reader who was scrolled away from the
// tail.

import (
	"strings"
	"testing"
	"time"

	"arxi.local/sim/internal/scenario"
	"arxi.local/sim/internal/ui"
)

// outputSession applies the whole of the first recording synchronously and draws once,
// the way Run would have. The height is the caller's because the two families of test
// here want opposite screens: the frame-level claims want a terminal tall enough to
// hold the whole conversation, so "the level hid it" and "the window cut it" cannot
// be mistaken for each other, and the anchor wants the ordinary small one, because a
// conversation that fits has nowhere to scroll. The clock, when the caller brings
// one, is installed before any of that, because the notice's deadline is read
// against it from the first frame.
func outputSession(t *testing.T, path string, height int, clock func() time.Time) (*App, *probe) {
	t.Helper()
	sc, err := scenario.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	p := &probe{}
	a := New(Config{
		Scenario: sc,
		Out:      p,
		Emitter:  &ui.Emitter{Theme: ui.DefaultTheme(), Mode: ui.ModeInline, Profile: ui.ProfileMono},
		Width:    80,
		Height:   height,
	})
	if clock != nil {
		a.cfg.Now = clock
	}
	for a.next < len(a.steps) {
		a.advance()
	}
	if err := a.draw(); err != nil {
		t.Fatalf("first frame: %v", err)
	}
	return a, p
}

// TestCtrlOWalksCompactStandardAndFull plays the first conversation and holds each
// level to what it shows of one thought and one result. The compact frame is the
// default frame — a session that never hears of ctrl+o draws it — and the walk ends
// where it began, which is what makes a three-answer question safe on a two-state key.
func TestCtrlOWalksCompactStandardAndFull(t *testing.T) {
	if got := DefaultKeymap().Lookup(mustKey(t, "ctrl+o")); got != ActionOutputLevel {
		t.Fatalf("ctrl+o does %q, want %q", got, ActionOutputLevel)
	}
	a, p := outputSession(t, firstConversation, 200, nil)

	// Compact, before anybody presses anything: the shape of the run without its
	// machinery. The reader is following the tail, and a level that changed that
	// would be a level that moved the session out from under them.
	if a.r.Detail != ui.DetailCompact {
		t.Fatalf("a session opened at level %d, want the compact level", a.r.Detail)
	}
	if a.scrolled {
		t.Fatal("the first frame is scrolled, so every claim below is about the wrong state")
	}
	compact := p.frame(t, a)
	for _, want := range []string{"Read(internal/auth/session.go)", "Bash(go test ./internal/auth/...) Denied"} {
		if !strings.Contains(compact, want) {
			t.Errorf("the compact frame never said %q:\n%s", want, compact)
		}
	}
	for _, banned := range []string{"Thought ·", "read 214 lines", "Refresh means the session cookie"} {
		if strings.Contains(compact, banned) {
			t.Errorf("the compact frame leaked %q:\n%s", banned, compact)
		}
	}

	if !a.key(mustKey(t, "ctrl+o")) {
		t.Fatal("ctrl+o changed nothing")
	}
	standard := p.frame(t, a)
	for _, want := range []string{"Thought ·", "read 214 lines", "Output level 2/3"} {
		if !strings.Contains(standard, want) {
			t.Errorf("the standard frame never said %q:\n%s", want, standard)
		}
	}
	if strings.Contains(standard, "Refresh means the session cookie") {
		t.Errorf("the standard frame drew the thought's text, which is the full level's answer:\n%s", standard)
	}

	if !a.key(mustKey(t, "ctrl+o")) {
		t.Fatal("the second ctrl+o changed nothing")
	}
	full := p.frame(t, a)
	for _, want := range []string{"Refresh means the session cookie", "read 214 lines", "Output level 3/3"} {
		if !strings.Contains(full, want) {
			t.Errorf("the full frame never said %q:\n%s", want, full)
		}
	}
	if strings.Contains(full, "Thought ·") {
		t.Errorf("the full frame kept the summary the thought's text replaces:\n%s", full)
	}

	if !a.key(mustKey(t, "ctrl+o")) {
		t.Fatal("the third ctrl+o changed nothing")
	}
	back := p.frame(t, a)
	if !strings.Contains(back, "Output level 1/3") {
		t.Errorf("the cycle did not come back to compact:\n%s", back)
	}
	if strings.Contains(back, "read 214 lines") || strings.Contains(back, "Thought ·") {
		t.Errorf("the level after the cycle is not the compact one:\n%s", back)
	}
	if a.scrolled {
		t.Error("cycling levels turned a tail-following session into a scrolled one")
	}
}

// TestTheLevelNoticeComesBackDown is the transient's whole contract: the notice is on
// the border in the frames the ttl covers and gone in the first frame past it, and
// what it named — the level — stays. A frozen clock makes "two seconds later" a fact
// instead of a race.
func TestTheLevelNoticeComesBackDown(t *testing.T) {
	clock := time.Now()
	a, p := outputSession(t, firstConversation, 200, func() time.Time { return clock })

	if !a.key(mustKey(t, "ctrl+o")) {
		t.Fatal("ctrl+o changed nothing")
	}
	if got := p.frame(t, a); !strings.Contains(got, "Output level 2/3") {
		t.Fatalf("the notice never went up:\n%s", got)
	}
	clock = clock.Add(outputToastTTL + 500*time.Millisecond)
	got := p.frame(t, a)
	if strings.Contains(got, "Output level") {
		t.Errorf("the notice outstayed its ttl:\n%s", got)
	}
	if !strings.Contains(got, "Thought ·") {
		t.Errorf("taking the notice down took the level with it:\n%s", got)
	}
}

// TestAToggledLevelKeepsTheReaderOnTheirItem is the anchor. The reader scrolls into
// the history of a recording whose tools carry diffs — the case where the two
// geometries disagree by the most rows — parks on an item that is not the first one,
// and cycles the whole ladder. What is on the top row after each change is the item
// that was there before it, which is the whole promise, and it holds at both ends of
// the cycle because the change is exactly as disruptive in both directions.
func TestAToggledLevelKeepsTheReaderOnTheirItem(t *testing.T) {
	a, _ := outputSession(t, linesChanged, 24, nil)
	if !a.key(mustKey(t, "ctrl+home")) {
		t.Fatal("ctrl+home moved nothing")
	}
	if !a.key(mustKey(t, "pgdown")) {
		t.Fatal("a page down moved nothing")
	}
	starts, _ := a.r.ItemSpans(a.st, a.vp)
	anchor, offset := spanAt(starts, a.top)
	if anchor < 0 || offset <= 0 {
		t.Fatalf("the window is parked at item %d offset %d, which anchors nothing", anchor, offset)
	}
	for step, want := range []ui.DetailLevel{ui.DetailStandard, ui.DetailFull, ui.DetailCompact} {
		if !a.key(mustKey(t, "ctrl+o")) {
			t.Fatalf("ctrl+o #%d changed nothing", step+1)
		}
		if a.r.Detail != want {
			t.Errorf("after ctrl+o #%d the level is %d, want %d", step+1, a.r.Detail, want)
		}
		now, _ := a.r.ItemSpans(a.st, a.vp)
		got, gotOffset := spanAt(now, a.top)
		if got != anchor {
			t.Errorf("after ctrl+o #%d the top row is inside item %d, want the item they were reading (item %d)", step+1, got, anchor)
		}
		// The offset survives exactly when the item is tall enough to hold it, which
		// at the fuller levels it is; the cap can only ever have cost rows, never
		// added them.
		if gotOffset > offset {
			t.Errorf("after ctrl+o #%d the reader is %d rows deeper into the item than they were (%d)", step+1, gotOffset, offset)
		}
	}
}
