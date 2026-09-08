package state

import "time"

// ItemKind is what a transcript entry is. The transcript is a flat, ordered list
// because that is what a terminal's scrollback is: append-only, never rearranged.
type ItemKind int

const (
	KindPrompt ItemKind = iota
	KindThinking
	KindText
	KindTool
	KindNotice
)

// ToolStatus is the outcome of one tool call.
type ToolStatus int

const (
	ToolPending ToolStatus = iota
	ToolOK
	ToolFailed
	ToolDenied
)

// NoticeKind labels a runtime remark that is not assistant prose.
type NoticeKind string

const (
	NoticeQuiescent NoticeKind = "quiescent"
	NoticeBudget    NoticeKind = "budget"
	NoticeBlocked   NoticeKind = "blocked"
	NoticeApproval  NoticeKind = "approval"
	NoticeEndOfLog  NoticeKind = "end_of_log"
	NoticeLock      NoticeKind = "lock"
	NoticeConflict  NoticeKind = "conflict"
)

// Item is one transcript entry. One struct rather than an interface: every field
// a renderer might want is inspectable, which is what makes a frame dump readable
// in a golden file and a state dump usable from a shell script.
type Item struct {
	Kind  ItemKind
	Seq   int    // seq of the event that opened the item
	ID    string // part_id, or tool-<seq>; stable for the render memo
	Actor string

	Text string // prompt text, accumulated part text, notice text
	Open bool   // still streaming, or still running

	Effort string // thinking effort, when the runtime reports one

	Tool    string
	Args    map[string]any
	Status  ToolStatus
	Summary string
	Policy  string
	Diff    *Diff // what an edit changed, when the result reported it

	Notice NoticeKind

	StartedAt time.Duration
	EndedAt   time.Duration
}

// Elapsed is how long the item took, or how long it has been running.
func (it Item) Elapsed(now time.Duration) time.Duration {
	if it.Open {
		return now - it.StartedAt
	}
	return it.EndedAt - it.StartedAt
}

// Inbox is an open question the runtime is waiting on a human to answer.
type Inbox struct {
	ID        string
	Kind      string
	Question  string
	Agent     string
	OnTimeout string
	CreatedAt time.Duration
	Answered  bool
	Answer    string
}

// Blocked records why an agent cannot proceed. blocked_ref is kept whole: it is
// the difference between "stuck" and "stuck on inbox-1, tool bash, policy ask".
type Blocked struct {
	Agent string
	On    string
	Ref   map[string]any
}
