package ui

import "sort"

// Theme maps declared style keys to styles. A theme may leave a key out; the
// result is the terminal's own default, which is a reasonable answer and never a
// crash.
type Theme struct {
	Name   string
	styles map[string]Style

	// Track records which keys were asked for. Off in normal use, on in the test
	// that proves the declared list and the rendered output agree, and behind
	// --debug-theme for anyone writing a theme.
	Track   bool
	used    map[string]int
	unknown map[string]int
}

// NewTheme builds a theme from a key/style table. Keys that are not declared are
// rejected here, so a typo in a theme is found at load time.
func NewTheme(name string, styles map[string]Style) (*Theme, error) {
	for k := range styles {
		if !Declared(k) {
			return nil, &UndeclaredKeyError{Key: k}
		}
	}
	return &Theme{Name: name, styles: styles, used: map[string]int{}, unknown: map[string]int{}}, nil
}

// UndeclaredKeyError names a style key nothing declares.
type UndeclaredKeyError struct{ Key string }

func (e *UndeclaredKeyError) Error() string {
	return "style key " + e.Key + " is not declared in ui.Keys"
}

// Resolve returns the style for a key. The empty key means "no styling", which is
// how a renderer says it has no opinion.
func (t *Theme) Resolve(key string) Style {
	if key == "" {
		return Style{}
	}
	if t.Track {
		if _, ok := keyIndex[key]; ok {
			t.used[key]++
		} else {
			t.unknown[key]++
		}
	}
	return t.styles[key]
}

// Used returns the tracked keys that exist, sorted.
func (t *Theme) Used() []string { return sortedKeys(t.used) }

// Unknown returns the tracked keys that nothing declares, sorted. A non-empty
// result is a bug in the renderer.
func (t *Theme) Unknown() []string { return sortedKeys(t.unknown) }

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DefaultTheme is the shipped look. Most of it leans on the terminal's own palette rather
// than hard-coded hex, so it inherits whatever scheme the user already likes and stays
// legible on a light background, on Termux, and over ssh in a 16-colour tty.
//
// The exceptions are the three washes — the two diff bands and the prompt's — and the code
// palette below, which is named in RGB because the reader chose it by pointing at a
// screenshot, and an indexed colour cannot be pointed at. Those are the parts of the theme
// tuned for a dark terminal: a pale lime string on a white background is legible but weak.
// A reader on a light background overrides six keys in [styles] and keeps the rest.
func DefaultTheme() *Theme {
	dim := Style{Attrs: AttrDim}
	// The six colours the code palette is made of, named once so that a colour used in
	// three places cannot drift between them, and written as hex because this is the one
	// part of the theme the reader chose by pointing at a picture rather than by naming a
	// terminal colour. Idx(Yellow) is whatever the terminal says yellow is; lime is lime.
	//
	// The picture was Claude Code's own theme picker at Monokai Extended, plus three
	// substitutions the reader asked for by name: lime where it had yellow, a light indigo
	// where it had that aqua blue, and a fuchsia where it had purple. Monokai's function
	// green and its pale string yellow survive as lime and pale lime; its cyan storage type
	// becomes the indigo; its pink keyword becomes the fuchsia, a little softer than the
	// #f92672 it started as, because #f92672 on black is loud enough to read as an error.
	//
	// Number is the one that does not come from the picture. Monokai draws a numeric literal
	// in the same violet it draws a constant in, and violet is the colour the reader took
	// off the list — but keyword has already spent the fuchsia it was replaced by, and two
	// keys in one colour is one fewer thing the highlighter can say. So a literal is amber:
	// nothing else in the theme is warm, which is exactly what makes it separate at a
	// glance from every other token on the row.
	//
	// Comment is a grey and not the green it used to be, for two reasons that agree. A lime
	// string and a green comment are neighbours on the wheel, and the comment is the token
	// the eye is meant to skip; and a green comment inside a diff's added band is green on
	// green, which is the one place in the interface where a colour can vanish outright.
	// It leans cool so that it reads as ink rather than as a dimmed version of the code.
	//
	// Every one of them is an RGB colour, so Emitter.Profile is what makes them safe: on a
	// 256-colour terminal they are quantised to the nearest cube entry, on a 16-colour one
	// to the nearest ANSI, and on a mono one they are not sent at all.
	var (
		lime     = MustHex("#a6e22e")
		paleLime = MustHex("#d3e88f")
		indigo   = MustHex("#9db2ff")
		fuchsia  = MustHex("#ff5f9e")
		amber    = MustHex("#ffa657")
		slate    = MustHex("#767d95")
		// The light in the two shines, a pale wash of the same indigo, because they are one
		// phenomenon drawn in two places and a reader who retunes one will want the other to
		// follow. It is a foreground and nothing else: a shine that carried a background
		// would win over the diff bands and the prompt's wash wherever it crossed them,
		// which is a rectangle sliding over the interface rather than a light moving under it.
		glint = MustHex("#cdd9ff")
		// And the rungs under it, three steps down toward the grey the input's own frame is
		// drawn in, which is what turns two vertical edges into a falloff. They are picked by
		// eye rather than interpolated, and the constraint that fixed the last one is that it
		// has to stay above the ambient: a step darker than the frame it crosses would read as
		// a shadow running ahead of the light instead of as the end of the light. The three
		// are about evenly spaced in perceived brightness — roughly .85, .75, .64 and .53 of
		// white — so the band steps down at a constant rate rather than falling off a cliff at
		// the last column.
		glintSoft  = MustHex("#b3c0e8")
		glintDim   = MustHex("#97a3cb")
		glintFaint = MustHex("#7b85a8")
	)
	th, err := NewTheme("default", map[string]Style{
		"prompt.marker": {FG: Idx(Bright + Black)},
		"prompt.text":   {},
		// The one thing in the transcript the reader wrote themselves, and in a long
		// scroll the thing they are looking for. A marker alone does not find it — every
		// block has one — so the turn gets a wash the width of the pane, the diff's own
		// mechanism (Fill under Style, merged at emit) spent on prose instead of code. It
		// is a neutral grey rather than a tint of the diff's blue so that a banded
		// paragraph is never read as a changed line, and it sets no foreground: the text
		// keeps whatever the inline markdown gave it. Emptying it in a config leaves the
		// marker and the text exactly as they were before the band existed.
		"prompt.band": {BG: MustHex("#2c2c31")},

		"thinking.marker":  {FG: fuchsia, Attrs: AttrDim},
		"thinking.text":    {Attrs: AttrDim | AttrItalic},
		"thinking.summary": dim,

		"md.text":       {},
		"md.heading":    {Attrs: AttrBold},
		"md.strong":     {Attrs: AttrBold},
		"md.em":         {Attrs: AttrItalic},
		"md.code":       {FG: indigo},
		"md.code.block": {FG: Idx(Bright + Black)},
		"md.code.fence": {FG: Idx(Bright + Black), Attrs: AttrDim},
		"md.bullet":     {FG: Idx(Bright + Black)},
		// A quote is somebody else's words, so it steps back rather than forward: the
		// bar carries the structure the way a fence's gutter does, and the text is dim,
		// which is the one attribute that still reads as quieter on a terminal that has
		// no colour at all.
		"md.quote":     dim,
		"md.quote.bar": {FG: Idx(Bright + Black), Attrs: AttrDim},
		// Underlined and blue is what a link looks like everywhere, and the url after it
		// is a fallback for a reader whose terminal cannot make it clickable — so the
		// text is the link and the url is an aside.
		"md.link":     {FG: Idx(Bright + Blue), Attrs: AttrUnderline},
		"md.link.url": dim,
		// A table is structure, so the header carries it and the rule stays out of the
		// way: bold reads as a heading at any colour depth, and a dim frame lets the
		// columns line up without the grid becoming the loudest thing on the screen.
		"md.table.header": {Attrs: AttrBold},
		"md.table.frame":  {FG: Idx(Bright + Black), Attrs: AttrDim},

		"code.comment": {FG: slate},
		"code.keyword": {FG: fuchsia},
		"code.type":    {FG: indigo},
		"code.func":    {FG: lime},
		"code.string":  {FG: paleLime},
		"code.number":  {FG: amber},

		"tool.marker.pending": {FG: Idx(Bright + Black)},
		"tool.marker.ok":      {FG: Idx(Green)},
		"tool.marker.error":   {FG: Idx(Red)},
		"tool.marker.denied":  {FG: Idx(Red)},
		"tool.name":           {Attrs: AttrBold},
		"tool.args":           dim,
		"tool.result.marker":  {FG: Idx(Bright + Black)},
		"tool.result.text":    dim,
		"tool.result.error":   {FG: Idx(Red)},
		"tool.result.gap":     {FG: Idx(Bright + Black), Attrs: AttrDim},

		// The bands are the one place the shipped theme has to name an RGB colour
		// (here and at prompt.band, which is the same mechanism spent on a turn the
		// reader wrote). A diff wash is a background covering a whole row, and there is
		// no indexed colour that both reads as a colour and lets text sit on it: Idx(Red)
		// as a background is a fire-engine block with code on top of it. They set no
		// foreground of their own — the code keeps whatever colour the highlighter gave it.
		// On a 16-colour terminal both collapse to their nearest ANSI background and the
		// signs carry the meaning alone; on a mono one the bands are simply not sent.
		//
		// The pair has been tuned three times by the reader who has to read it. Added was a blue for
		// a while, on the argument that a green band sits under a syntax highlighter's own greens;
		// the reader asked for the green back and called the blue too strong, so it is a green —
		// and the argument against it is answered by hue rather than by weight, in that it leans
		// far enough towards a sea green to separate from the flat ANSI green a comment is drawn
		// in. Then the whole pair was too dark: the first attempt at "legible under text" put
		// added at a twelfth of full brightness, which on a dark terminal is indistinguishable
		// from no band at all, and the reader said so — black is not one of the two colours a
		// diff is allowed to use.
		//
		// The third correction was the removed side reading as pink rather than as red. It was
		// #7f2830, whose blue channel outran its green by eight, and a red whose blue outweighs
		// its green is a red tilted towards magenta — which is what pink is. It is #8a2620 now:
		// a third less blue, the green left where it stood, so the tilt is the other way and the
		// band reads as a dark red instead of a washed one. Its weight did not move — it was the
		// hue that was wrong — and diff_test.go states it in the only terms that survive a
		// re-tune: on the removed band, blue may not outrun green.
		//
		// So they are a middle weight, and the range is stated as a floor and a ceiling in
		// diff_test.go rather than as the hex it happens to be written as: dark enough that the
		// terminal's own foreground still has better than 4.5:1 on it, light enough to be seen
		// as a colour. The two sides are matched to within a shade of each other on Rec. 601
		// luma, so neither side of a change shouts over the other. What tells the two sides apart
		// for a reader who cannot separate red from green is still the sign in the gutter, which
		// is bright and bold and says it in a glyph.
		"diff.gutter":         {FG: Idx(Bright + Black)},
		"diff.gutter.added":   {FG: Idx(Bright + Black)},
		"diff.gutter.removed": {FG: Idx(Bright + Black)},
		"diff.context":        {FG: Idx(Bright + Black)},
		"diff.added":          {BG: MustHex("#1c5a38")},
		"diff.added.sign":     {FG: Idx(Bright + Green), Attrs: AttrBold},
		"diff.removed":        {BG: MustHex("#8a2620")},
		"diff.removed.sign":   {FG: Idx(Bright + Red), Attrs: AttrBold},
		"diff.gap":            {FG: Idx(Bright + Black), Attrs: AttrDim},

		"notice.marker": {FG: Idx(Bright + Black)},
		"notice.text":   dim,
		"notice.warn":   {FG: amber},

		"input.frame":       {FG: Idx(Bright + Black)},
		"input.title":       {FG: Idx(Bright + Black), Attrs: AttrDim},
		"input.marker":      {FG: indigo, Attrs: AttrBold},
		"input.text":        {},
		"input.placeholder": dim,
		// No attribute of its own, deliberately. The band's style is merged over the style of
		// whatever it crosses and attributes are unioned rather than replaced, so a bold here
		// would land on the placeholder's dim as 1;2 — where the terminal picks a winner and
		// it is usually the dim. The colour is the whole effect, and it lands on the two rules
		// and the two verticals as well as on the text, which is where most of it is seen.
		"input.shine": {FG: glint},
		// The rungs, which carry no attribute either and for a second reason on top of the
		// first: the falloff is the brightness, so an attribute on one of these would be a step
		// that differs from its neighbour in weight rather than in light, and the eye reads that
		// as an edge. Three of them because sixteen columns is four to a step, and four steps is
		// what a reader can see as a gradient without it becoming a smear.
		"input.shine.soft":  {FG: glintSoft},
		"input.shine.dim":   {FG: glintDim},
		"input.shine.faint": {FG: glintFaint},

		"approval.marker":   {FG: amber},
		"approval.question": {Attrs: AttrBold},
		"approval.key":      {FG: Idx(Green), Attrs: AttrBold},
		"approval.hint":     dim,

		// A light orange, and the same one notice.warn is drawn in rather than a second orange a
		// shade off it: the two never share a row — a notice is a line of the transcript and the
		// spinner is the foot of the frame — and two colours nobody can tell apart are two keys
		// nobody can retune apart either. It replaces the fuchsia, which was the thinking
		// marker's colour and read as though the two were the same event.
		"status.spinner": {FG: amber},
		"status.verb":    {},
		// Bold here and not above, because status.verb carries no attribute for it to fight:
		// seven letters brightening as the light goes over them is what the shine is on the
		// text for, and bold is what says "brighter" on a terminal with no colour at all.
		"status.shine": {FG: glint, Attrs: AttrBold},
		// Which is also what makes the edge of this band expressible in one step: the same
		// colour without the weight is genuinely between bold glint and the verb's own default,
		// and it is a step in the direction the light is falling off. A dimmer colour would not
		// be — the verb is near-white already, so down in brightness from glint is down toward
		// the frame and away from the text the band is crossing.
		"status.shine.soft": {FG: glint},
		"status.text":       dim,
		"status.dim":        dim,
		"status.sep":        {FG: Idx(Bright + Black)},
		// A member's live state is carried by the glyph, while the name stays
		// neutral so a four-person cluster reads as one segment rather than four
		// competing alerts. Busy shares the run spinner's amber; blocked uses the
		// same warning colour; failed alone turns red; idle recedes in slate.
		"status.member.busy":    {FG: amber},
		"status.member.blocked": {FG: amber},
		"status.member.failed":  {FG: Idx(Red)},
		"status.member.idle":    {FG: slate, Attrs: AttrDim},
		"status.member.name":    dim,

		// One colour at three weights, matching the two glyphs: the bar is a single object and the
		// thumb is the lit part of it, so they read as brightness rather than as two things that
		// happen to be adjacent. Slate is the quiet structural grey the comments are drawn in —
		// quiet enough to sit beside prose all session without competing with it, and light enough
		// that dimming it once still leaves a visible track on a terminal with no colour depth to
		// spare.
		//
		// The third weight is the thumb with the pointer on it, and it is the same colour again for
		// that reason: bold is what says "brighter" where there is no colour depth to spend, the
		// way the status band's light is weight rather than hue, while a colour of its own would say
		// the reader had grabbed some other object instead of taking hold of this one.
		"scroll.track":      {FG: slate, Attrs: AttrDim},
		"scroll.thumb":      {FG: slate},
		"scroll.thumb.held": {FG: slate, Attrs: AttrBold},

		"overlay.frame":     {FG: indigo},
		"overlay.title":     {FG: indigo, Attrs: AttrBold},
		"overlay.text":      {},
		"overlay.highlight": {FG: Idx(Black), BG: indigo},
		// The team sheet keeps blueprint metadata quiet and reserves weight and
		// colour for the two things a reader acts on: whose row this is, and what
		// can move a blocked member. Runtime state is plain prose between them.
		"team.name":   {FG: indigo, Attrs: AttrBold},
		"team.meta":   dim,
		"team.state":  {},
		"team.remedy": {FG: amber},

		"tasks.summary":   {FG: indigo, Attrs: AttrBold},
		"tasks.action":    {FG: indigo},
		"tasks.title":     {Attrs: AttrBold},
		"tasks.meta":      dim,
		"tasks.detail":    dim,
		"tasks.pending":   {FG: slate, Attrs: AttrDim},
		"tasks.active":    {FG: amber},
		"tasks.completed": {FG: lime},

		"effort.label": {FG: slate},
		"effort.track": {FG: slate},
		// The static fills: one purple at three weights, because the fill is a
		// single object whose brightness is the level — medium a solid purple,
		// high a lighter one, xhigh the lightest. They start as the ultracode
		// wave's mid purples so the whole bar speaks one family of purple, but
		// they are their own keys: retuning the wave does not move the fills.
		"effort.fill.medium": {FG: MustHex("#9b59b6")},
		"effort.fill.high":   {FG: MustHex("#af7ac4")},
		"effort.fill.xhigh":  {FG: MustHex("#c39bd3")},
		"effort.marker":      {FG: indigo, Attrs: AttrBold},
		"effort.level":       {FG: slate},
		"effort.selected":    {FG: indigo, Attrs: AttrBold},
		"effort.desc":        dim,
		"effort.hint":        {FG: slate, Attrs: AttrDim},
		// The animated levels wear interpolated scales rather than flat hues: the
		// rainbow is twelve keys — six hues and the blend between each pair — and
		// ultracode five purples, so neighbouring cells on the bar differ by one
		// blend and the effect reads as a gradient instead of a flicker between
		// colours. The blends are channel midpoints of the hues either side.
		"effort.rainbow.r":       {FG: MustHex("#ff5555")},
		"effort.rainbow.ro":      {FG: MustHex("#ff7d56")},
		"effort.rainbow.o":       {FG: amber},
		"effort.rainbow.oy":      {FG: MustHex("#f8d071")},
		"effort.rainbow.y":       {FG: MustHex("#f1fa8c")},
		"effort.rainbow.yg":      {FG: MustHex("#cbee5d")},
		"effort.rainbow.g":       {FG: lime},
		"effort.rainbow.gb":      {FG: MustHex("#a1ca96")},
		"effort.rainbow.b":       {FG: indigo},
		"effort.rainbow.bp":      {FG: MustHex("#ce88ce")},
		"effort.rainbow.p":       {FG: fuchsia},
		"effort.rainbow.pr":      {FG: MustHex("#ff5a79")},
		"effort.ultra.deep":      {FG: MustHex("#6c3d99")},
		"effort.ultra.deepMid":   {FG: MustHex("#834ba7")},
		"effort.ultra.mid":       {FG: MustHex("#9b59b6")},
		"effort.ultra.midBright": {FG: MustHex("#af7ac4")},
		"effort.ultra.bright":    {FG: MustHex("#c39bd3")},
	})
	if err != nil {
		panic(err) // a compiled-in theme with an undeclared key is a build bug
	}
	return th
}

// DefaultThemeWith is the shipped look with some keys replaced, which is what a config's
// [styles] section asks for. It exists because NewTheme takes a whole table: handing it the
// dozen keys a config names would leave the other sixty unstyled, and a reader who recoloured
// one thing would lose everything else.
//
// An override replaces a key's style outright rather than laying it Over the default. Two
// reasons, and both matter: attributes are unioned by Over, so a default's dim would survive
// a config that never asked for it and could not be taken off; and "" would then mean
// "change nothing" instead of "remove this", which is the spelling ui.Keys documents for
// prompt.band. What a config writes for a key is the whole answer for that key.
//
// Keys are applied in name order so that a table with a mistake in it always reports the
// same one first. Map iteration is random, and an error message that changes between runs of
// the same file is a bug report nobody can act on.
func DefaultThemeWith(overrides map[string]Style) (*Theme, error) {
	th := DefaultTheme()
	names := make([]string, 0, len(overrides))
	for k := range overrides {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if !Declared(k) {
			return nil, &UndeclaredKeyError{Key: k}
		}
		th.styles[k] = overrides[k]
	}
	return th, nil
}
