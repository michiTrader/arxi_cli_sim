package app

// The composition is the widget list, and this file pins it twice over: the order it
// installs in, by the widgets' own names, and the bytes the default order draws. The
// first is the vocabulary a [layout] table will one day be allowed to rewrite, so it
// is spelled out here rather than derived; the second is the guarantee the extraction
// from the hand-written chrome was asked for — the default reproduces today byte for
// byte, and any reordering has to argue with this file.

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arxi.local/sim/internal/state"
	"arxi.local/sim/internal/ui"
)

var update = flag.Bool("update", false, "rewrite composition golden files")

// compositionApp is the player every case starts from: eighty by twenty-four, mono,
// idle, with the case's own state layered on top. Nothing runs — the composition is
// read directly, the way draw would read it, and the frame is rendered the way
// tasksFrameText renders, for the same reason: the claims here are about rows and
// their order, which the frame answers without the emitter's escapes in the way.
func compositionApp(t *testing.T, tune func(a *App)) *App {
	t.Helper()
	a := New(Config{
		Out:     &probe{},
		Emitter: &ui.Emitter{Theme: ui.DefaultTheme(), Mode: ui.ModeInline, Profile: ui.ProfileMono},
		Width:   80,
		Height:  24,
	})
	if tune != nil {
		tune(a)
	}
	return a
}

func compositionFrame(t *testing.T, a *App) string {
	t.Helper()
	a.r.Widgets = a.chrome()
	a.ed.Shine = a.inputShine()
	a.vp.Scrolled, a.vp.ScrollTop = a.scrolled, a.top
	return a.r.Render(a.st, a.ed, a.vp).Plain() + "\n"
}

// theInbox is the open question the approval cases are asked, and theItems is a turn
// for the recap case to summarize: one prompt, one tool call over it, one answer.
var (
	theInbox = []*state.Inbox{{ID: "inbox-1", Question: "Allow the write to go.mod?", OnTimeout: "deny"}}
	theItems = []state.Item{
		{Kind: state.KindPrompt, Text: "clean up the cache"},
		{Kind: state.KindTool, ID: "t1", Tool: "read", Status: state.ToolOK},
		{Kind: state.KindText, Text: "Cache cleared."},
	}
)

// compositionCases is one case per row of the composition, named for the row it
// exercises, plus "all", which turns every gate on at once and pins the whole order
// in one list. names is the widget sequence the default composition installs, spelled
// out: it is the [layout] vocabulary, one unique name per row — the armed warning is
// "interrupt" and the end of the scenario is "notice", the split spec/look.md made of
// the two rows that used to share a name.
func compositionCases(t *testing.T) []struct {
	name  string
	tune  func(a *App)
	names string
} {
	t.Helper()
	return []struct {
		name  string
		tune  func(a *App)
		names string
	}{
		{"idle", nil, "scrollbar,status"},
		{"tasks-open", func(a *App) { a.st = tasksFixture() }, "tasks,scrollbar,status"},
		{"tasks-closed", func(a *App) { a.st = tasksFixture(); a.tasksOpen = false }, "tasks,scrollbar,status"},
		{"approval", func(a *App) { a.st.Inboxes = theInbox }, "approval,scrollbar,status"},
		{"effort", func(a *App) { a.effortSlider = NewEffortSlider("", 40) }, "effort,scrollbar,status"},
		{"slashmenu", func(a *App) {
			a.ed.Insert("/")
			a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
		}, "slashmenu,scrollbar,status"},
		{"finished", func(a *App) { a.st.Finished = true }, "scrollbar,notice,status"},
		{"armed", func(a *App) { a.armed, a.armedAt = true, a.cfg.Now() }, "scrollbar,interrupt,status"},
		{"recap", func(a *App) {
			a.st.Recap = true
			a.st.Items = theItems
		}, "scrollbar,recap,status"},
		{"all", func(a *App) {
			a.st = tasksFixture()
			a.tasksOpen = false
			a.st.Inboxes = theInbox
			a.effortSlider = NewEffortSlider("", 40)
			a.ed.Insert("/")
			a.smenu = slashMenuFor(a.ed.Text(), a.smenu)
			a.st.Finished = true
			a.st.Recap = true
			a.st.Items = append(a.st.Items, theItems...)
			a.armed, a.armedAt = true, a.cfg.Now()
		}, "approval,tasks,effort,slashmenu,scrollbar,notice,interrupt,recap,status"},
	}
}

// TestTheDefaultCompositionIsToday pins the composition twice. The names subtest
// asserts the order the default installs in, case by case; the frame subtest pins the
// bytes that order draws, one golden file per case. Run with -update to rewrite the
// goldens, then read the diff: the composition is the interface's skeleton, and a
// change to it should be as reviewable as a change to the code.
func TestTheDefaultCompositionIsToday(t *testing.T) {
	cases := compositionCases(t)
	// The scrolled case needs a real transcript to pin a header over, and one row up
	// from the tail of a long session is where the header earns its keep. Its gate
	// lives in the composition like every other, so its names belong in the same list.
	a, _ := scrollableFile(t, longSession, ui.ModeAlt, 0)
	if !a.scroll(-1) {
		t.Fatal("one row up did not scroll")
	}
	scrolled := a
	t.Run("scrolled/names", func(t *testing.T) {
		want := "header,scrollbar,notice,status"
		if got := strings.Join(names(scrolled.chrome()), ","); got != want {
			t.Fatalf("widget order = %q, want %q", got, want)
		}
	})

	for _, tc := range cases {
		t.Run(tc.name+"/names", func(t *testing.T) {
			a := compositionApp(t, tc.tune)
			if got := strings.Join(names(a.chrome()), ","); got != tc.names {
				t.Fatalf("widget order = %q, want %q", got, tc.names)
			}
		})
		t.Run(tc.name+"/frame", func(t *testing.T) {
			a := compositionApp(t, tc.tune)
			got := compositionFrame(t, a)
			path := filepath.Join("testdata", "composition-"+tc.name+".frame")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Log("wrote " + path)
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run: go test ./internal/app -update)", err)
			}
			if got != string(want) {
				t.Errorf("frame differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
			}
		})
	}
	t.Run("scrolled/frame", func(t *testing.T) {
		got := compositionFrame(t, scrolled)
		path := filepath.Join("testdata", "composition-scrolled.frame")
		if *update {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Log("wrote " + path)
			return
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run: go test ./internal/app -update)", err)
		}
		if got != string(want) {
			t.Errorf("frame differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
		}
	})
}

// TestTheCompositionNamesItsWidgets keeps a step's name and slot and the widgets it
// builds from drifting apart. The name is the stable ID a [layout] table will say, and
// Name() is "what a user writes in config"; the slot is the half of that vocabulary a
// row is addressed by, and the widget's own Slot() is its answer; if any two disagree,
// one of them is a lie. Every step is built against every case — a gate that never
// opens in any of them still has to name what it would install — and the armed step's
// expiry and the effort step's tick run here exactly as they run in chrome(), because
// the builders act and not only ask.
func TestTheCompositionNamesItsWidgets(t *testing.T) {
	cases := compositionCases(t)
	apps := make([]*App, len(cases))
	for i, tc := range cases {
		apps[i] = compositionApp(t, tc.tune)
	}
	// The scrolled case is the only one that opens the header's gate, and it lives
	// outside compositionCases because it needs a real transcript to pin a header
	// over. It joins the audit here for the same reason: a step that built nothing
	// anywhere is a step whose name nothing checks.
	scrolled, _ := scrollableFile(t, longSession, ui.ModeAlt, 0)
	if !scrolled.scroll(-1) {
		t.Fatal("one row up did not scroll")
	}
	apps = append(apps, scrolled)
	for _, step := range defaultComposition {
		seen := false
		for i, a := range apps {
			for _, wd := range step.build(a) {
				seen = true
				if got := wd.Name(); got != step.name {
					t.Errorf("step %q built a widget named %q (app %d)", step.name, got, i)
				}
				if got := wd.Slot(); got != step.slot {
					t.Errorf("step %q declares slot %q but its widget asks for %q (app %d)", step.name, step.slot, got, i)
				}
			}
		}
		if !seen {
			t.Errorf("step %q built nothing in any case; its name is unaudited", step.name)
		}
	}
}

// names is the widget list read as the vocabulary it is.
func names(widgets []ui.Widget) []string {
	out := make([]string, len(widgets))
	for i, wd := range widgets {
		out[i] = wd.Name()
	}
	return out
}

func TestLayoutReordersVetoesAndResolvesTiers(t *testing.T) {
	all := func(a *App) {
		a.st = tasksFixture()
		a.tasksOpen = false
		a.st.Inboxes = theInbox
		a.st.Finished = true
		a.armed, a.armedAt = true, a.cfg.Now()
		a.st.Recap = true
		a.st.Items = append(a.st.Items, theItems...)
	}
	t.Run("reorder and veto", func(t *testing.T) {
		a := compositionApp(t, all)
		a.layout = []LayoutOverride{
			{Slot: ui.SlotAboveInput, Names: []string{"recap", "tasks"}},
			{Slot: ui.SlotBelowInput, Names: []string{"interrupt", "approval", "notice"}},
			{Slot: ui.SlotBottom},
		}
		if got, want := strings.Join(names(a.chrome()), ","), "interrupt,approval,notice,recap,tasks,scrollbar"; got != want {
			t.Fatalf("widget order = %q, want %q", got, want)
		}
	})
	t.Run("width tier and height priority", func(t *testing.T) {
		a := compositionApp(t, all)
		a.layout = []LayoutOverride{
			{Slot: ui.SlotBelowInput, Names: []string{"notice"}},
			{Slot: ui.SlotBelowInput, Width: 100, Names: []string{"interrupt"}},
			{Slot: ui.SlotBelowInput, Width: 60, Names: []string{"approval"}},
			{Slot: ui.SlotBelowInput, Height: 30, Names: []string{"approval", "interrupt"}},
		}
		// At 80x24 both width<100 and height<30 match; height wins.
		if got := strings.Join(names(a.chrome()), ","); !strings.Contains(got, "approval,interrupt") || strings.Contains(got, "notice") {
			t.Fatalf("height tier did not win: %q", got)
		}
		// In a fold height has no meaning, so width<100 wins instead.
		a.vp.Height = 0
		if got := strings.Join(names(a.chrome()), ","); !strings.Contains(got, "interrupt") || strings.Contains(got, "approval,interrupt") || strings.Contains(got, "notice") {
			t.Fatalf("fold width tier = %q", got)
		}
	})
}

// This is the layout: mutation family: frames that differ only because a [layout]
// table asked them to. The composition-*.frame family above remains untouched, which
// is the default-behaviour invariant; these goldens make a configured reorder a review
// event of its own rather than noise in the default corpus.
func TestLayoutMutationFrames(t *testing.T) {
	a := compositionApp(t, func(a *App) {
		a.st = tasksFixture()
		a.tasksOpen = false
		a.st.Inboxes = theInbox
		a.st.Finished = true
		a.armed, a.armedAt = true, a.cfg.Now()
	})
	a.layout = []LayoutOverride{
		{Slot: ui.SlotAboveInput, Names: []string{"tasks"}},
		{Slot: ui.SlotBelowInput, Names: []string{"interrupt", "notice", "approval"}},
		{Slot: ui.SlotBottom},
	}
	got := compositionFrame(t, a)
	path := filepath.Join("testdata", "layout-reorder-and-veto.frame")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("wrote " + path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/app -update)", err)
	}
	if got != string(want) {
		t.Errorf("layout mutation differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
