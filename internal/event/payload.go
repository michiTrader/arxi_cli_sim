package event

// Payload shapes. Only the fields the TUI reads are declared; unknown fields in
// a log are ignored so a newer runtime never breaks an older simulator.

// TaskStatus belongs to the experimental sim.task.* replay contract, not to
// upstream arxi. Completed is terminal.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskActive    TaskStatus = "active"
	TaskCompleted TaskStatus = "completed"
)

// InitialTaskStatus normalizes an omitted create status to pending and reports
// whether an explicitly supplied status is valid for task creation.
func InitialTaskStatus(status *TaskStatus) (TaskStatus, bool) {
	if status == nil {
		return TaskPending, true
	}
	return *status, *status == TaskPending || *status == TaskActive
}

// CanChangeTaskStatus is the shared lifecycle rule used by validation and the
// reducer. completed is terminal, and a status event must actually change it.
func CanChangeTaskStatus(from, to TaskStatus) bool {
	switch from {
	case TaskPending:
		return to == TaskActive || to == TaskCompleted
	case TaskActive:
		return to == TaskPending || to == TaskCompleted
	default:
		return false
	}
}

// TaskCreatedPayload declares a task. Status is optional and defaults to pending.
type TaskCreatedPayload struct {
	TaskID string      `json:"task_id"`
	Title  string      `json:"title"`
	Detail string      `json:"detail,omitempty"`
	Owner  string      `json:"owner,omitempty"`
	Status *TaskStatus `json:"status,omitempty"`
}

// TaskUpdatedPayload uses pointers so empty detail and owner values clear fields.
type TaskUpdatedPayload struct {
	TaskID string  `json:"task_id"`
	Title  *string `json:"title,omitempty"`
	Detail *string `json:"detail,omitempty"`
	Owner  *string `json:"owner,omitempty"`
}

type TaskStatusChangedPayload struct {
	TaskID string     `json:"task_id"`
	Status TaskStatus `json:"status"`
}

// MemberSpec mirrors the part of kernel.MemberConfig that a transcript needs.
// The members field on run.started is the simulator's additive proposal rather
// than arxi's current event contract: the runtime owns this data in the frozen
// blueprint snapshot, but does not yet put it in the event stream. If the
// runtime settles on another shape, this is the one file that changes.
type MemberSpec struct {
	Name       string   `json:"name"`
	Role       string   `json:"role,omitempty"`
	Model      string   `json:"model,omitempty"`
	Advisory   bool     `json:"advisory,omitempty"`
	Tools      []string `json:"tools,omitempty"`
	Activation string   `json:"activation,omitempty"`
	Stages     []string `json:"stages,omitempty"`
}

type RunStartedPayload struct {
	RunID        string       `json:"run_id"`
	Actor        string       `json:"actor"`
	BudgetUSD    float64      `json:"budget_usd"`
	BlueprintSHA string       `json:"blueprint_sha"`
	CWD          string       `json:"cwd,omitempty"`
	GitBranch    string       `json:"git_branch,omitempty"`
	Members      []MemberSpec `json:"members,omitempty"`
}

type RunPromptPayload struct {
	Text string `json:"text"`
}

type StageEnteredPayload struct {
	Stage string `json:"stage"`
	Index int    `json:"index"`
}

type RunQuiescentPayload struct {
	Stage     string `json:"stage"`
	Diagnosis string `json:"diagnosis"`
}

type ToolCallPayload struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

type ToolResultPayload struct {
	Tool   string `json:"tool"`
	Policy string `json:"policy"`
	Result struct {
		ExitCode *int         `json:"exit_code"`
		Lines    int          `json:"lines"`
		Summary  string       `json:"summary"`
		Diff     *DiffPayload `json:"diff"`
	} `json:"result"`
}

// DiffPayload is what an edit reports about the lines it changed, and like
// LockPayload it is the simulator's proposal rather than arxi's contract: the
// runtime already completes a write or an update, but says nothing about *what*
// changed, and a transcript that cannot show the change is a transcript that makes
// the reader open an editor to find out what the agent did.
//
// It carries no counts. Added and removed lines are countable from the rows, and a
// number that can be derived is a number a hand-written log can get wrong.
type DiffPayload struct {
	Path  string     `json:"path"`
	Lang  string     `json:"lang"` // "go", "md", …; "" means do not colour it
	Hunks []DiffHunk `json:"hunks"`
}

// DiffHunk is one run of changed lines with its surrounding context. Rows are
// unified-diff strings — " context", "-removed", "+added" — because that is what an
// edit tool already has in hand, it survives a hand-edited NDJSON file without a
// nested object per line, and the sign is where a reader looks anyway.
//
// OldLine and NewLine are the 1-based numbers the hunk starts at on each side, which
// is the only way the gutter can be numbered: a removed line has no new number and an
// added line has no old one.
type DiffHunk struct {
	OldLine int      `json:"old_line"`
	NewLine int      `json:"new_line"`
	Rows    []string `json:"rows"`
}

type InboxCreatedPayload struct {
	InboxID   string `json:"inbox_id"`
	Kind      string `json:"kind"`
	Question  string `json:"question"`
	Agent     string `json:"agent"`
	OnTimeout string `json:"on_timeout"`
}

type InboxRepliedPayload struct {
	InboxID string `json:"inbox_id"`
	Text    string `json:"text"`
}

type AgentBlockedPayload struct {
	BlockedOn  string         `json:"blocked_on"`
	BlockedRef map[string]any `json:"blocked_ref"`
}

// These agent-detail payloads are the simulator's proposal for fields the
// catalogue names but does not yet shape. They contain only what the team
// monitor can display; if the runtime settles on other names, this is the one
// file that changes.
type AgentSteeredPayload struct {
	Text string `json:"text"`
	To   string `json:"to,omitempty"`
}

type AgentNotifiedPayload struct {
	Text string `json:"text"`
	To   string `json:"to,omitempty"`
}

type AgentFailedPayload struct {
	Error string `json:"error"`
}

// LockPayload and ConflictPayload are the simulator's proposal rather than arxi's
// contract. The runtime already emits lock.acquired, lock.released and
// resource.conflict, but declares no shape for them, so these are the fields a
// transcript needs in order to say who is waiting on what and who is holding it. If
// the runtime settles on other names, this is the one file that changes.
type LockPayload struct {
	Resource string `json:"resource"`
	Holder   string `json:"holder"`
	Mode     string `json:"mode"`
}

type ConflictPayload struct {
	Resource string `json:"resource"`
	Holder   string `json:"holder"`
	Waiter   string `json:"waiter"`
	Wanted   string `json:"wanted"`
}

type TurnStartedPayload struct {
	Turn int `json:"turn"`
}

// PartKind is the shape of one piece of an assistant turn.
type PartKind string

const (
	PartThinking PartKind = "thinking"
	PartText     PartKind = "text"
)

type PartPayload struct {
	PartID string   `json:"part_id"`
	Kind   PartKind `json:"kind"`
	Index  int      `json:"index"`
	Effort string   `json:"effort,omitempty"`
}

type DeltaPayload struct {
	PartID string `json:"part_id"`
	Text   string `json:"text"`
}

type ResponsePayload struct {
	CostUSD         float64 `json:"cost_usd"`
	TokensIn        int     `json:"tokens_in"`
	TokensOut       int     `json:"tokens_out"`
	Model           string  `json:"model"`
	ContextUsed     *int    `json:"context_used,omitempty"`
	ContextCapacity *int    `json:"context_capacity,omitempty"`
}

type BudgetPayload struct {
	SpentUSD float64 `json:"spent_usd"`
	LimitUSD float64 `json:"limit_usd"`
	Fraction float64 `json:"fraction"`
}
