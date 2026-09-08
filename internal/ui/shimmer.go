package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Shimmer is a band of light that walks a row from left to right and then waits. It is
// the whole animation vocabulary of this package beyond the spinner, and it is a value
// rather than a widget on purpose: a widget owns a slot on the screen, and a shine owns
// nothing — it is handed a Line that has already been laid out and hands one back.
//
// That is what makes it composable with everything drawn before it. A Span carries two
// theme keys, Style and Fill, which emit merges as Style.Over(Fill), so lighting a span
// does not have to erase what the span already was: the shine takes Style and the span's
// own key moves down to Fill, where it still supplies whatever the shine leaves unsaid.
// A prompt band, a diff row and a plain word all keep their background under the light.
//
// Zero is off, and that is the field that matters most here. Style == "" means On is
// false and Apply returns the Line it was given, byte for byte, so every golden file,
// every fold and both corpus sweeps see exactly the frame they saw before this type
// existed. Nothing in this package arms a shimmer; the program does, in one place.
type Shimmer struct {
	// Style is the theme key the lit band is drawn in, and it is also the switch: no key
	// is no shine. A key and not a Style value because a colour that cannot be named in
	// [styles] is a colour the reader cannot change, and being changeable is the point.
	Style string

	// Phase is the tick this frame is on, counted by whatever clock the caller already
	// runs — in the player, the same counter the spinner is indexed by, so the two can
	// never drift apart or need two timers.
	Phase int

	// Period is how many ticks one cycle lasts, Travel how many of them the band is
	// moving, and Width how many columns wide it is. Period minus Travel is therefore
	// the dark rest between passes, which is what keeps a glint from reading as a
	// strobe. Zero in any of them means the shipped number below.
	Period int
	Travel int
	Width  int
}

// The two keys the player shines with. They are exported because they are the seam: a
// reader who wants a different sweep writes [styles] input.shine = ... and gets it, and
// a program embedding this package needs to be able to name the same thing.
const (
	InputShine  = "input.shine"
	StatusShine = "status.shine"
)

// The shipped numbers, at the player's 120 ms tick: a band sixteen columns wide crossing in
// eight ticks — just under a second — and then thirty-two ticks of dark, so a pass comes round
// every five seconds or so. Quick enough to read as a reflection rather than as a wipe, rare
// enough not to compete for attention with the text beside it.
//
// Eight and not twelve, because twelve was slow enough for the eye to follow the band across the
// row: what it is meant to do is catch attention on the way past and then leave, and a faster
// sweep does that better. The period stayed at forty so the rest between passes grew rather than
// shrank — a faster sweep that came round as often would be a flicker.
//
// Sixteen and not ten because the band is a gradient now. Travel is ticks per pass and not
// columns per tick, so eight ticks across eighty columns moves the band by its own width each
// tick, and consecutive frames still overlap — which is what makes it read as one thing sliding
// rather than a block reappearing further along. Ten columns divided into four steps would be
// two and a half columns a step; sixteen is four columns a step at the width an input box
// actually has.
const (
	shineWidth  = 16
	shineTravel = 8
	shinePeriod = 40
)

// The ladders the two bands are drawn in, brightest first. A shine is a gradient and not a
// block: a band is drawn in several keys at once, its core in the key the caller named and the
// columns either side in the ones below, which is what gives the light an edge instead of a
// border. Every key in here is declared in theme_keys.go and settable in [styles], so the
// falloff is as much the reader's to retune as the colour it falls off from.
//
// The input's ladder is the longer one, and the reason is arithmetic rather than taste. Its band
// is sixteen columns of a window seventy or more wide, so four steps land four columns apart and
// read as a falloff. The status band is at most three columns — the word it crosses is seven
// letters and Band halves a band it cannot fit in — and four steps on three columns is two steps
// nobody can tell apart. Two is what three columns can show.
var ramps = map[string][]string{
	InputShine: {
		InputShine, InputShine + ".soft", InputShine + ".dim", InputShine + ".faint",
	},
	StatusShine: {StatusShine, StatusShine + ".soft"},
}

// ramp is the ladder for this shimmer's key: the declared one for either of the two bands the
// player draws, and the key on its own for anything else. That last case is the seam kept open
// rather than an oversight — a program embedding this package can shine in a key of its own and
// gets a hard-edged band, which is a band and not a lookup failure.
func (s Shimmer) ramp() []string {
	if r, ok := ramps[s.Style]; ok {
		return r
	}
	return []string{s.Style}
}

// On reports whether this shimmer draws anything at all.
func (s Shimmer) On() bool { return s.Style != "" }

// norm fills the zero fields in, so that every other method can read them as numbers
// rather than as maybe-numbers. Period is floored above Travel rather than merely above
// zero: a period at or below the travel would leave no dark half, and the band would
// wrap round to the left edge the tick after it left the right one.
func (s Shimmer) norm() Shimmer {
	if s.Width <= 0 {
		s.Width = shineWidth
	}
	if s.Travel <= 0 {
		s.Travel = shineTravel
	}
	if s.Period <= s.Travel {
		s.Period = max(shinePeriod, s.Travel+1)
	}
	return s
}

// reach answers the band's own extent this tick, and answers it unclipped: [lo, hi) in columns
// of a w-wide row, where lo may be negative and hi may be past the right edge. Two callers want
// two different things from it, which is why the clipping is not done here — Band cuts it down
// to the row, and steps measures the ladder against it whole. The second is the reason: the
// brightest column belongs to the middle of the light, so a band still half off the left edge
// has to read as a trailing falloff rather than as a core sitting on column zero.
//
// The band travels w+Width columns rather than w, entering at the left edge and leaving at
// the right one, so a glint slides in and out instead of appearing whole and vanishing
// whole. The division is the whole trick: because it is Travel that is divided and not the
// width, a pass takes the same number of ticks — the same wall-clock time — on a forty
// column window and on a two hundred column one. The band moves faster on the wide one,
// which is exactly what a reflection does.
//
// Phase is taken modulo twice because Go's % keeps the sign of its left operand, and a
// caller counting a phase down, or handing us one that has wrapped past MinInt, would
// otherwise get a negative tick and no shine ever again.
func (s Shimmer) reach(w int) (lo, hi int, ok bool) {
	if !s.On() || w <= 0 {
		return 0, 0, false
	}
	s = s.norm()
	// The other end of the same argument, and the one a wide monitor finds. Travel is ticks and
	// not columns, so a wider row is crossed in the same twelve ticks by a band that jumps
	// further each one — and once the jump is longer than the band is wide, the columns between
	// one frame and the next are never lit at all. The light stops sliding and starts landing:
	// a dotted trail rather than a reflection. Sixteen columns over twelve ticks holds to 192,
	// which is wider than a terminal usually is and not wider than one can be.
	//
	// So the band widens on a row that would tear, rather than the pass slowing down — the
	// cadence is the tuned thing and a glint a fifteenth of the row wide still reads as a glint.
	// ceil(w/Travel) is exactly the width at which the jump can no longer outrun it, and it is
	// the same number that makes the sweep enter at the left edge and leave past the right one,
	// because both of those ask this of it too. Below 192 columns nothing here moves.
	if step := (w + s.Travel - 1) / s.Travel; s.Width < step {
		s.Width = step
	}
	// A band that covers more than half the row is not a band crossing it, it is the row
	// lighting up and going out again — there has to be something unlit for the light to be
	// moving across. Narrow rows get half their width instead of the configured number: the
	// seven letters of "working", or an input box on a phone in portrait. It is second rather
	// than first so that it also caps the widening above, which at a Travel of one or two would
	// otherwise ask for most of the row: half a row is the invariant, and a pass short enough
	// to need more than that is a wipe whose continuity nobody can see anyway.
	if 2*s.Width > w {
		s.Width = max(1, w/2)
	}
	t := ((s.Phase % s.Period) + s.Period) % s.Period
	if t >= s.Travel {
		return 0, 0, false // the dark half of the cycle
	}
	edge := (w + s.Width) * (t + 1) / (s.Travel + 1)
	return edge - s.Width, edge, true
}

// Band answers which columns of a w-wide row are lit this tick: [from, to), or ok false when
// nothing is. Half-open, like every other range in this package. It is reach cut down to the
// row, and it is the exported one because it answers the only question a caller outside this
// file has — whether there is light on this row, and where.
func (s Shimmer) Band(w int) (from, to int, ok bool) {
	lo, hi, ok := s.reach(w)
	if !ok {
		return 0, 0, false
	}
	from, to = max(0, lo), min(w, hi)
	if from >= to {
		return 0, 0, false
	}
	return from, to, true
}

// shineStep is one run of columns drawn in one key of the ladder: [from, to), and the key. A
// band is a handful of these rather than one. Runs and not columns because a Span is a run of
// text — four columns of one key is one cut and one style, where four columns each carrying
// their own key would be four of each for a light nobody could tell apart from this one.
type shineStep struct {
	from, to int
	key      string
}

// steps divides this tick's band into the runs of its ladder, left to right. The runs are
// disjoint and in order, which is what lets Apply lay them over the same Line one after another
// without a column ever being lit twice.
//
// A band whose key has no ladder comes back as the single run Band would have given, so the
// hard-edged case costs one allocation and no arithmetic.
func (s Shimmer) steps(w int) []shineStep {
	lo, hi, ok := s.reach(w)
	if !ok {
		return nil
	}
	from, to := max(0, lo), min(w, hi)
	if from >= to {
		return nil
	}
	r := s.ramp()
	if len(r) == 1 {
		return []shineStep{{from, to, r[0]}}
	}
	out := make([]shineStep, 0, 2*len(r)-1)
	for c := from; c < to; c++ {
		key := r[level(c, lo, hi, len(r))]
		if n := len(out) - 1; n >= 0 && out[n].key == key {
			out[n].to = c + 1
			continue
		}
		out = append(out, shineStep{c, c + 1, key})
	}
	return out
}

// level places column c of the band [lo, hi) on an n-rung ladder: 0 in the middle of the light,
// n-1 at either end, by distance from the centre. It is integer arithmetic on half-columns — d
// is |2c - (lo+hi-1)|, twice the distance from the centre, and the band's half-width in those
// same units is hi-lo — because a float here would be a rounding rule no reader could check,
// and the answer has to be symmetric about the centre to the column or the light has a bright
// side.
//
// Linear, and evenly spaced by construction: sixteen columns over four rungs is four columns a
// rung, which at the 120 ms tick reads as a falloff rather than as stripes. Three columns over
// two rungs is the middle one and its two neighbours, which is the most three columns can say
// and is the whole reason the status ladder has two rungs and not four.
func level(c, lo, hi, n int) int {
	d := 2*c - (lo + hi - 1)
	if d < 0 {
		d = -d
	}
	return min(n-1, n*d/(hi-lo))
}

// Apply returns l with the lit columns of a w-wide row drawn in this shimmer's ladder, brightest
// in the middle of the band. A span that straddles an edge — of the band, or of any rung inside
// it — is cut into the part inside and the part outside, so the light lands on columns and not
// on spans: a shine that snapped to span boundaries would jump a whole word at a time, which is
// not a reflection.
//
// One pass per rung, each over the Line the last one handed back, rather than a single walk that
// would have to know which rung a column is on and where the span boundaries are at the same
// time. Seven passes over a row of a dozen spans is nothing at eight frames a second, and what
// it buys is that paint below is exactly the one-band function this used to be.
//
// The text is never touched, only ever divided: the pieces of a cut span concatenate back
// to exactly the string that arrived, so Line.Text() and Line.Width() are byte-identical
// before and after. That is the property that lets this run last, after wrapping, padding
// and the overflow check, without being able to invalidate any of them.
func (s Shimmer) Apply(l Line, w int) Line {
	for _, st := range s.steps(w) {
		l = paint(l, st)
	}
	return l
}

// paint lays one rung over l: every column of [st.from, st.to) drawn in st.key, and every other
// column the span it already was.
func paint(l Line, st shineStep) Line {
	if len(l) == 0 {
		return l
	}
	out := make(Line, 0, len(l)+2)
	col := 0
	for _, sp := range l {
		end := col + ansi.StringWidth(sp.Text)
		switch {
		case end <= st.from || col >= st.to:
			out = append(out, sp) // wholly outside this rung
		case col >= st.from && end <= st.to:
			out = append(out, lit(sp, st.key)) // wholly inside it
		default:
			out = append(out, cut(sp, st, col)...)
		}
		col = end
	}
	return out
}

// cut splits one straddling span into up to three, at whichever of a rung's two edges fall
// inside it. It is paint's default branch and nothing else calls it.
func cut(sp Span, st shineStep, col int) Line {
	out := Line{}
	rest, at := sp.Text, col
	if at < st.from {
		head, tail := cutCols(rest, st.from-at)
		if head != "" {
			h := sp
			h.Text = head
			out = append(out, h)
			at += ansi.StringWidth(head)
		}
		rest = tail
	}
	if rest != "" && at < st.to {
		mid, tail := cutCols(rest, st.to-at)
		if mid != "" {
			m := sp
			m.Text = mid
			out = append(out, lit(m, st.key))
			at += ansi.StringWidth(mid)
		}
		rest = tail
	}
	if rest != "" {
		t := sp
		t.Text = rest
		out = append(out, t)
	}
	return out
}

// lit is where the composition happens. The rung's key takes Style, which is what emit resolves
// last and therefore what wins on colour; the span's own key falls back to Fill, where it still
// supplies everything the shine does not name. So a shine that sets only a foreground leaves a
// diff row's background and the prompt's band exactly where they were.
//
// A span that already has a Fill keeps it and loses its Style instead: two keys is what a
// Span holds, the deeper one is the one that is carrying a background, and dropping the
// background to keep a foreground the light is about to overwrite would be the wrong half
// to save.
func lit(sp Span, key string) Span {
	if sp.Fill == "" {
		sp.Fill = sp.Style
	}
	sp.Style = key
	return sp
}

// Slower returns s with n times as long between passes and the same pass: Travel and Width are
// untouched, so the band crosses at exactly the speed it crossed at and only the dark rest after
// it gets longer. The numbers are normalized on the way through, because a zero field means "the
// shipped default" and n times nothing is still nothing.
//
// It exists for the input box, and the asymmetry is the point. The status verb is seven letters
// the reader is already watching, where a glint every five seconds is a pulse saying the thing
// is alive; the input frame is a shape they are resting their eyes on, where the same cadence is
// a nag. So the box gets one pass in nine or ten seconds and the verb keeps five, which is the
// same light at two rates rather than two lights. A reader who disagrees writes [anim] period
// and moves both, since the relation between them is one multiplication and lives at the call.
func (s Shimmer) Slower(n int) Shimmer {
	if !s.On() || n <= 1 {
		return s
	}
	s = s.norm()
	s.Period *= n
	return s
}

// cutCols splits s at the first grapheme boundary at or after n columns, so that head is
// at most n columns wide and head+tail is s. A cut is refused rather than forced when it
// would land inside a grapheme: the alternative is a row whose text no longer measures what
// it measured, and a band edge one column off on a wide rune is a cost nobody can see.
func cutCols(s string, n int) (head, tail string) {
	if n <= 0 || s == "" {
		return "", s
	}
	head = ansi.Truncate(s, n, "")
	if head == "" || head == s || !strings.HasPrefix(s, head) {
		if head == s {
			return s, ""
		}
		return "", s
	}
	return head, s[len(head):]
}
