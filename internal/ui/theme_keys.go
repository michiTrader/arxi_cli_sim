package ui

// This file is the contract between the interface and the person configuring it.
// Every style the renderer can ask for is declared here with the reason it exists,
// and a test fails if the renderer uses a key that is not on this list or if a key
// on this list is never used. That is what keeps `arxi-sim theme keys` honest: the
// list a user reads is the list the code obeys, forever, by construction.

// KeyDecl is one declared style key.
type KeyDecl struct {
	Key string
	Doc string
}

// Keys is every style key the interface understands, in the order it is shown to
// the user.
var Keys = []KeyDecl{
	{"prompt.marker", "the glyph in front of a prompt the human sent"},
	{"prompt.text", "the text of a prompt the human sent"},
	{"prompt.band", "the wash behind a whole prompt, to the right edge; empty removes it"},

	{"thinking.marker", "the glyph in front of a reasoning block"},
	{"thinking.text", "reasoning text, while it is still streaming"},
	{"thinking.summary", "the one-line replacement once reasoning is finished"},

	{"md.text", "assistant prose"},
	{"md.heading", "a markdown heading"},
	{"md.strong", "text between double asterisks"},
	{"md.em", "text between single asterisks"},
	{"md.code", "an inline code span"},
	{"md.code.block", "the body of a fenced code block"},
	{"md.code.fence", "the gutter drawn beside a fenced code block"},
	{"md.bullet", "the glyph of a list item, and the number of an ordered one"},
	{"md.quote", "the text of a blockquote"},
	{"md.quote.bar", "the bar drawn beside a blockquote, one per level of nesting"},
	{"md.link", "the text of a link"},
	{"md.link.url", "the target of a link, drawn after its text"},
	{"md.table.header", "the header cells of a table, and the labels its narrow form uses"},
	{"md.table.frame", "the rule under a table's header and the verticals between its columns"},

	{"code.comment", "a comment, in a fence or in a diff"},
	{"code.keyword", "a language keyword: func, if, range"},
	{"code.type", "a type name or a literal the language reserves: string, nil, true"},
	{"code.func", "a builtin the language provides: make, len, append"},
	{"code.string", "a string literal"},
	{"code.number", "a numeric literal"},

	{"tool.marker.pending", "the dot in front of a tool call that is still running"},
	{"tool.marker.ok", "the dot in front of a tool call that succeeded"},
	{"tool.marker.error", "the dot in front of a tool call that failed"},
	{"tool.marker.denied", "the dot in front of a tool call policy refused"},
	{"tool.name", "the name of the tool being called"},
	{"tool.args", "the arguments of a tool call"},
	{"tool.result.marker", "the elbow that introduces a tool result"},
	{"tool.result.text", "the summary of a tool result"},
	{"tool.result.error", "the summary of a tool result that failed or was denied"},
	{"tool.result.gap", "the line standing in for the middle of a result too long to draw"},

	{"diff.gutter", "the line number of an unchanged row"},
	{"diff.gutter.added", "the line number of an added row"},
	{"diff.gutter.removed", "the line number of a removed row"},
	{"diff.context", "code on a row the edit did not change"},
	{"diff.added", "the band an added row is drawn on; a background, so syntax colour survives"},
	{"diff.added.sign", "the plus in front of an added row"},
	{"diff.removed", "the band a removed row is drawn on; a background, for the same reason"},
	{"diff.removed.sign", "the minus in front of a removed row"},
	{"diff.gap", "the marker standing in for the rows between two hunks"},

	{"notice.marker", "the glyph in front of a runtime remark"},
	{"notice.text", "a runtime remark: quiescence, budget, an answered approval"},
	{"notice.warn", "a runtime remark that deserves attention"},

	{"input.frame", "the border drawn around the input"},
	{"input.title", "the title written into the input's top border"},
	{"input.marker", "the glyph in front of what the human is typing"},
	{"input.text", "what the human is typing"},
	{"input.placeholder", "the hint shown while the input is empty"},
	{"input.shine", "the band of light that crosses the input while it is your turn"},
	{"input.shine.soft", "one step out from the middle of that band"},
	{"input.shine.dim", "two steps out, where the light is nearly gone"},
	{"input.shine.faint", "the outermost step, which has to stay above the frame's own grey"},

	{"approval.marker", "the glyph in front of a pending approval"},
	{"approval.question", "the question a pending approval asks"},
	{"approval.key", "a key the human can press to answer"},
	{"approval.hint", "the explanation next to those keys"},

	{"status.spinner", "the animated glyph while an agent is working"},
	{"status.verb", "the word next to the spinner"},
	{"status.shine", "the band of light that crosses that word while it says working"},
	{"status.shine.soft", "the edge of that band: the same colour without the weight"},
	{"status.text", "a status segment"},
	{"status.dim", "a status segment of secondary importance"},
	{"status.sep", "the separator between status segments"},
	{"status.member.busy", "the spinner in front of a team member with an open turn"},
	{"status.member.blocked", "the glyph in front of a team member waiting on a dependency"},
	{"status.member.failed", "the glyph in front of a team member whose turn failed"},
	{"status.member.idle", "the glyph in front of a team member with no open turn"},
	{"status.member.name", "a team member's name in the status cluster"},

	{"scroll.track", "the scrollbar's unfilled length, beside the transcript"},
	{"scroll.thumb", "the part of the scrollbar standing for the rows on screen"},
	{"scroll.thumb.held", "that part again, while the pointer has hold of it"},

	{"overlay.frame", "the border drawn around a floating overlay"},
	{"overlay.title", "the title written into the overlay's top border"},
	{"overlay.text", "body text inside a floating overlay"},
	{"overlay.highlight", "the selected item inside a floating overlay"},

	{"team.name", "a member name in the Team monitor"},
	{"team.meta", "a member's blueprint role, model, tools, activation, and stages"},
	{"team.state", "a member's current runtime state in the Team monitor"},
	{"team.remedy", "the concrete command or explanation that can unblock a member"},

	{"tasks.summary", "the task count summary beside the input and atop the Tasks monitor"},
	{"tasks.action", "the slash command that opens the Tasks monitor"},
	{"tasks.title", "a task title in the Tasks monitor"},
	{"tasks.meta", "task counts, owner, identity, and navigation hints"},
	{"tasks.detail", "a task's secondary detail in the Tasks monitor"},
	{"tasks.pending", "the glyph and status text of a pending task"},
	{"tasks.active", "the glyph and status text of an active task"},
	{"tasks.completed", "the glyph and status text of a completed task"},

	{"effort.label", "the Faster/Smarter labels at each end of the effort bar"},
	{"effort.track", "the horizontal line of the effort bar"},
	{"effort.fill.medium", "the track filled up to the marker at the medium level, a solid purple"},
	{"effort.fill.high", "the track filled up to the marker at the high level, a lighter purple"},
	{"effort.fill.xhigh", "the track filled up to the marker at the xhigh level, the lightest purple"},
	{"effort.marker", "the position indicator on the effort bar"},
	{"effort.level", "an unselected level name below the bar"},
	{"effort.selected", "the selected level name below the bar"},
	{"effort.desc", "the description of the selected level"},
	{"effort.hint", "the key hint at the bottom of the effort bar"},
	{"effort.rainbow.r", "rainbow red for the max-level animation"},
	{"effort.rainbow.ro", "the blend between rainbow red and orange"},
	{"effort.rainbow.o", "rainbow orange for the max-level animation"},
	{"effort.rainbow.oy", "the blend between rainbow orange and yellow"},
	{"effort.rainbow.y", "rainbow yellow for the max-level animation"},
	{"effort.rainbow.yg", "the blend between rainbow yellow and green"},
	{"effort.rainbow.g", "rainbow green for the max-level animation"},
	{"effort.rainbow.gb", "the blend between rainbow green and blue"},
	{"effort.rainbow.b", "rainbow blue for the max-level animation"},
	{"effort.rainbow.bp", "the blend between rainbow blue and purple"},
	{"effort.rainbow.p", "rainbow purple for the max-level animation"},
	{"effort.rainbow.pr", "the blend between rainbow purple and red"},
	{"effort.ultra.deep", "the dark purple of the ultracode wave"},
	{"effort.ultra.deepMid", "the purple between deep and mid in the ultracode wave"},
	{"effort.ultra.mid", "the mid purple of the ultracode wave"},
	{"effort.ultra.midBright", "the purple between mid and bright in the ultracode wave"},
	{"effort.ultra.bright", "the bright purple of the ultracode wave"},
}

// keyIndex is the declared set, built once.
var keyIndex = func() map[string]string {
	m := make(map[string]string, len(Keys))
	for _, k := range Keys {
		if _, dup := m[k.Key]; dup {
			panic("ui: style key declared twice: " + k.Key)
		}
		m[k.Key] = k.Doc
	}
	return m
}()

// Declared reports whether key is on the list.
func Declared(key string) bool {
	_, ok := keyIndex[key]
	return ok
}
