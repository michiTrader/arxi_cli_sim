package ui

// DetailLevel is how much of a transcript a frame spends its rows on. The
// transcript keeps everything it was handed — a finished thought's text, a tool
// result's whole summary, every row of a diff — and the level decides how much of
// that is drawn, so moving between levels is a rendering decision and never a loss:
// nothing leaves state, and what a level hides is one ctrl+o away.
//
// The three levels are a ladder by amount, and the names say what each one is for
// rather than how many rows it saves:
//
//  1. Compact is the level a session opens at, because the run's shape is the story
//     and the machinery is the noise: the reader's prompts, the assistant's prose, a
//     runtime notice, and one row per tool call with its outcome reduced to a diff's
//     two counts or a word. A thought draws nothing.
//  2. Standard is the transcript as it has always been drawn: a finished thought is
//     its duration and effort, a long result keeps its head and its tail, a diff
//     draws whole. Everything above is what it always was.
//  3. Full draws what state kept: the thought's text, the result without its
//     elision, the diff as standard draws it.
type DetailLevel int

const (
	DetailCompact  DetailLevel = 1
	DetailStandard DetailLevel = 2
	DetailFull     DetailLevel = 3
)

// OrStandard answers the level to render with. Zero is the unset value a
// zero-value ItemBlock or Renderer carries, and it means the level the transcript
// has always been drawn at, so a caller that knows nothing of levels cannot draw
// less than it used to; any other impossible value folds there too.
func (d DetailLevel) OrStandard() DetailLevel {
	if d < DetailCompact || d > DetailFull {
		return DetailStandard
	}
	return d
}
