# Scenarios

A scenario is an arxi-shaped replay: one NDJSON line per step, played back by
`internal/scenario` so that the interface can be built, watched and tested before
the orchestrator that produces the real log is finished.

NDJSON has no comment syntax and the loader rejects blank lines, so a scenario
cannot document itself. That is what this file is for.

## The three line shapes

    {"after_ms": 170, "event": {…}}   play the event 170 ms after the previous one
    {"await": "inbox-1"}              stop until that inbox is answered for real
    {"await": "prompt"}               stop until the human types a new prompt

`after_ms` and `await` are the only playback-wrapper fields. Inside `event`, the
simulator uses arxi's core event catalogue plus explicitly proposed replay extensions:
`run.started.members`, `cwd`, and `git_branch`; the `llm.*` family; the experimental
`sim.task.created`, `sim.task.updated`, and `sim.task.status_changed` family; and
payload shapes otherwise documented as proposals. Arxi has no Tasks contract, so
`sim.task.*` is not upstream. `MemberSpec` mirrors the real arxi `MemberConfig`, but
putting that data on `run.started` is still a simulator extension.

### `after_ms` is a delta, not a timestamp

It is the gap from the *previous* step, in milliseconds. `Load` accumulates the
deltas into `Step.At` and synthesizes `event.TS` as `Epoch + At`, with
`Epoch = 2026-01-01T00:00:00Z` fixed so that timestamps are reproducible and
golden files stay stable. Editing one delay shifts every line after it, which is
what makes a recording readable: `1400` on a `tool.call_completed` is how long
that command took.

The player divides every delay by `-speed`; `-instant` ignores them entirely.

### A barrier waits for a real human

`{"await": "prompt"}` hands the input bar back. `{"await": "inbox-1"}` stops until
that inbox is answered — by a keypress in a terminal, or by the recording's own
`inbox.replied` when the log is folded with no tty. A barrier line carries only
`await`: no `after_ms`, no `event`.

A barrier is the only place a scenario becomes interactive, and it is why the last
line of every file here is `{"await": "prompt"}`: the run ends by waiting for you
rather than by falling off the end of the file.

## The loader is strict on purpose

A typo has to fail loudly instead of playing silently, so `rawStep` decodes with
`DisallowUnknownFields` and only `after_ms`, `event` and `await` exist. `Load`
reports *every* problem in a file at once rather than only the first:

- a blank line, or a line carrying an unknown field
- a barrier carrying anything besides `await`
- an event with no `after_ms`, or a negative one
- a line that is neither shape

`Validate` then checks the simulator's accepted log contract, including its replay
extensions, so the TUI is tuned only against input with a fully declared shape:

- `seq` is contiguous from 1
- every event has a type from `event.Known`, a `scope`, and a `source` from the
  catalogue
- every event has a payload (`{}` when there is nothing to say)
- `agent.blocked` carries `blocked_ref` — without it a stuck run cannot be
  explained, which is the one thing arxi promises to always be able to do
- `run.quiescent` carries a `diagnosis`
- an `llm.part` is declared once, has kind `thinking` or `text`, and is closed by
  `llm.part_done`; a delta names a part that is open
- `llm.response` carries `context_used` and `context_capacity` together or omits
  both; used is non-negative, capacity is positive, and used cannot exceed it
- an inbox is created before it is replied to, timed out, or awaited

Recorded provenance is carried by the replay extensions on `run.started`: `cwd` and
`git_branch` describe the simulated run, not whichever directory happens to play it back.
Likewise, context occupancy is an exact response metric and is never inferred from
`tokens_in`, whose value remains the latest response's input-token count.

## The twelve recordings

| file | what it is for |
| --- | --- |
| `01-first-conversation.ndjson` | the baseline, and the one the golden file is taken from |
| `02-narrow-terminal.ndjson` | content wider than a phone |
| `03-team-of-four.ndjson` | four members, two `bash` calls open at once |
| `04-budget-exhausted.ndjson` | the money runs out and the raise is denied |
| `05-lock-contention.ndjson` | two members want one file |
| `06-lines-changed.ndjson` | an edit that shows the lines it changed |
| `07-tool-fails.ndjson` | a tool call that comes back non-zero |
| `08-long-session.ndjson` | four prompts in one run, and enough height to scroll |
| `09-long-output.ndjson` | a tool result too long for the transcript to draw whole |
| `10-markdown.ndjson` | a reply written in markdown, arriving one delta at a time |
| `11-team-blocked.ndjson` | the dedicated Team monitor, recorded provenance, and exact context |
| `12-tasks-live.ndjson` | live Tasks creation, partial updates, status transitions, and continued run output |

**01 — first conversation.** One agent, one `read`, one `bash` denied by policy,
an approval inbox that blocks the run until it is answered, and prose with a
heading, bold, inline code and a fenced Go block. It ends quiescent with a
diagnosis that names what the run is waiting for. This is the scenario
`TestGoldenFirstConversation` pins at 72 columns, so a change here is a change to
`internal/ui/testdata/01-first-conversation.72.txt` (`go test ./internal/ui
-update` rewrites it).

**02 — narrow terminal.** Everything hostile to 20 columns, in one turn: an
identifier nothing can break, a bare URL, a CJK paragraph whose cells are twice
its runes, an emoji ZWJ sequence, a markdown table, and a `tool.call_completed`
summary long enough to wrap several times. Its table is the one that cannot be a
grid: three columns whose last cell is a whole sentence need 55 columns before
every column can still hold its widest word, so at anything narrower each row is
drawn as its own small labelled record — the shape a table takes on a phone. This
is the recording that proves that path, and 06 the one that stays in the grid.

**03 — team of four.** scout, builder, tester and scribe are all activated;
scout's `go test` and builder's `go build` are open at the same time under the
same tool name, which is the case that forces a result to be bound to the oldest
open call *by the same actor*, since the log carries no call id. Blocks also stop
sealing in order here — builder's write finishes while scout's call is still
open — which is what the commit gate in the renderer exists for. scribe is
activated and never speaks, so the diagnosis says the run waits on scribe or on a
steer.

**04 — budget exhausted.** Four turns whose `llm.response` costs add up past a
$0.50 limit: `budget.warning` at 81%, then `budget.exceeded`, then a
`budget_raise` inbox the human denies. The agent stops and reports what is done,
what is not, and what the next run needs. The `agent.blocked` that follows draws
no line of its own — the budget notice one row up already said why.

**05 — lock contention.** builder takes the write lock on
`internal/auth/session.go`, tester wants the same file, waits 1.2 s, and writes
after the release. `lock.acquired`, `lock.released` and `resource.conflict` all
become transcript notices, so a run that serialized on one file no longer reads
like a run that stalled for no reason.

**06 — lines changed.** Two `update` calls, and the only recording that draws a
diff. The first touches two places in one file, so a gap marker is drawn between
its hunks — and by the second the new side has drifted five lines from the old,
which is where the numbering convention shows itself: a removed row is numbered on
the old side and everything else on the new, because one column of gutter cannot
say both. Neither result carries a `summary`, so the wording under the elbow is
the one `(*Diff).Summary()` derives: "Added 9 lines, removed 2 lines" for the
first, and "Added 8 lines" for the second, where nothing was removed and the clause
that would have said zero is dropped instead of printed. That second edit also
inserts a blank line, which still draws its sign, its number and its band. The
closing turn carries a small three-column table whose counts are pinned to their
right edge by a `---:` delimiter, which is what makes "+9 -2" and "+8" read as one
column: it is a grid down to 41 columns, the width at which the longest file name
still fits whole, and a record per row below that.

**07 — a tool that fails.** `budget.Reserve` returns an `error` where it used to
return a `bool`, and the run is the compiler walking the agent to every caller: a
failing `go build`, two edits, a clean build, then a *failing* `go test` — because
`go build` never compiles a `_test.go` file, so a green build was not a green test
run. It is the only recording whose tool calls fail, and that is a fact about
style rather than about text: `tool.marker.pending`, `.ok`, `.error` and
`.denied` are one glyph with four styles, so a failed call and a passing one are
the same `●` to `-instant`, to a golden file and to the corpus sweep alike. Only
a test that resolves style keys can tell them apart, which is why this file is
named in `drawnKeys` in `internal/ui/theme_test.go`, and is what retired the
hand-built failing item that used to stand in for it there. Three other shapes
are new here too: a result with `exit_code` 0 and nothing else, which draws its
header row and no elbow at all; two `update` calls open at the same time under
the *same* actor, the other half of the oldest-open-by-the-same-actor rule 03
exercises across actors; and diffs that remove as much as they add and then more
than they add, where 06 only ever added.

**08 — a long session.** Four prompts answered in one recording, 157 steps, 52
seconds of recorded time and about 550 lines at 72 columns: the only recording
tall enough that the wheel, the page keys and the commit rule have real scrollback
to work against. Its height is bought with fenced blocks, multi-hunk diffs and
multi-line summaries rather than with steps, because a sweep costs steps × lines
and a taller step is cheaper than another step. What is new is the seam between
turns: every prompt but the first is preceded by a `run.quiescent`, so three times
over, a diagnosis that says what the run is waiting for is answered by the human
it was waiting for — everywhere else quiescence is the last thing that happens.
There is no barrier until the last line, which is the point, since scrollback has
to arrive on its own before a wheel is worth testing. Three more shapes are new
here: three calls open at once under one actor, two `update`s and a `bash`, so a
result has to match a tool name as well as an actor; the bare `exit_code` 0 that
07 introduced, carrying the opposite meaning, because a `--follow` past the last
event printed nothing (right) and returned anyway (the bug), which makes the empty
result the finding itself; and `lang: "md"` on a diff plus a `console` fence, the
first two languages the corpus asks for that `langs` does not have — `langFor`
returns nil and `Highlight` draws one span in the base style, which is why the
markdown table inside that diff stays source instead of becoming a grid. There is
no inbox and no budget event: an inbox with no barrier after it would flash past
in a live play, and 01 and 04 already own both.

**09 — a long result.** One `make check` whose output is 31 lines and one focused
re-run whose output is 12, which are the two sides of one rule: above
`resultHead + resultTail + 1` lines a result keeps its first 3 and its last 8 with
a `… 20 more lines` between them, and at that boundary it is drawn whole, because
eliding a single line would spend a row to save a row. The two ends get different
budgets because they do not carry the same thing — `make check` announces itself at
the top, where the commands it ran are, and reports at the bottom, where the
failure, the `FAIL` lines and make's own exit status are — so the tail is worth
more rows than the head and is given them. The cut is measured in the result's own
lines and not in the rows they wrap to: a count the reader can check by running the
command themselves is worth more than one that changes with the width of their
window. It is not measured in bytes either, since bytes are what
`max_output_bytes` caps upstream, which is a fact about the runtime's budget rather
than about what a screen can show. The line between the ends is `tool.result.gap`,
one name for both the glyph and the style key the way `diff.gap` is, and a style is
not a property of the text, so this file is named in `drawnKeys` as well. It is 31
steps and tall rather than long, for the reason 08 is. It is also where the wrapper's
two kinds of leading space are read in anger: the four and eight columns that put
`scenarios_test.go:96:` under its `--- FAIL:` header survive, because a space starting a
line the *text* broke is somebody's indentation, and only one starting a line the
*width* broke is the break itself. The row after that hangs under the line it
continues rather than restarting at the result's own four-column prefix, which is
what `    at step 12` under `        scenarios_test.go:96: … committed 3 lines` used
to do: the left edge is where a `--- FAIL:` header sits, so a detail drawn there is
one subtest drawn at two depths, and the reader is told about a failure that is
really the second half of a sentence. It is the hanging indent `RenderMarkdown`
gives a wrapped list item, one layer down, and it gives way where that one does —
an indent with no room for the word beside it is not an indent, and kept in a pane
that narrow it would wrap the detail into the columns left over rather than into the
pane.

**10 — markdown.** A release note written the way an assistant writes one, and the
only recording that says a reply is markdown rather than prose with a fence in it:
`*emphasis*` and `**bold**`, an ordered list, a quote inside a quote, a link and the
url it goes to, checkbox bullets, a heading, and inline code. Every one of those had
tests and no recording, so the styles they draw were proven by handing a string
straight to `RenderMarkdown` — which says the markdown renderer can emit a key, not
that a transcript ever shows it. This file draws them through a fold, a block and the
renderer instead, which is why it is named in `drawnKeys` and why the hand-built prose
that used to stand in for it there is gone, the same way 07 retired the hand-built
failing call.

Two of its constructs are split across delta boundaries on purpose, because that is
the only way a streaming reply arrives: `*strict` ends one delta and `* now` begins the
next, and `[the event contract]` and `(https://arxi.dev/events)` are likewise two
steps. An unterminated marker stays literal, so the half-arrived emphasis draws as an
asterisk and becomes emphasis when its pair lands, and a reader watching the play sees
no flicker of style. The asterisks in `O(steps * lines)` never become emphasis at all,
and `[1]` and `[2]` never become links: both are what the corpus contributes to rules
that were only in unit tests before.

Its ordered list is the one place in the corpus where an indented row lands inside
assistant prose. `1. Regenerate your fixtures: …` runs to a newline the author typed
and continues on the next source row, indented three columns; the renderer redraws that
row at the column the item's text starts at, so the item reads as one item at every
width instead of as an item followed by a stray indented line. A blank line closes the
item, and one space of indentation is not enough to continue it — two are — because a
single space in front of a sentence is far more often a typo than a continuation.

**11 — a blocked team.** Four blueprint members make the Team monitor a surface of
its own rather than a status-line inventory. scout reports a finding, builder is denied
a write and waits on an approval inbox, then resumes; reviewer runs the requested test
and fails while scribe remains idle. Team, Ctrl+T, and `/team` are local simulator UI
inspired by Claude Code, not UI observed in arxi. Opening that view exposes
working, blocked, failed, steered and notified states, plus the deterministic approval
remedy, and later events alter that same live view rather than a snapshot. The roster
is deliberately tall enough to scroll at phone widths and at short heights.

This run also records `D:\projects\arxi` and branch `main` in `run.started`, so the
provenance row cannot accidentally read the playback process's environment. Each
completed model turn records its own model, input/output tokens, and a valid exact
`context_used`/`context_capacity` pair. The final reviewer response leaves
`63750/200000` visible: that number is intentionally unrelated to `tokens_in`, proving
the semantic context row does not estimate occupancy from traffic.

**12 — live tasks.** Two experimental `sim.task.*` records exercise the Tasks
summary and dedicated monitor without adding task bookkeeping to the conversation.
The first is created pending, becomes active, and receives owner and detail through a
partial update; the second is created while that work remains active. Both complete
while the run continues through tool output and an assistant response, so every frame
must derive counts and rows from current folded state rather than from a snapshot.
Creation order remains stable across every update. Tasks and `/tasks` are simulator UI
inspired by Claude Code; the `sim.task.*` family remains a replay extension rather than
an upstream arxi contract.

## Playing one

Flags come before the file: Go's `flag` package stops at the first non-flag
argument, so `play <file> -instant` is a usage error.

    go build -o arxi-sim ./cmd/arxi-sim

    ./arxi-sim play testdata/scenarios/01-first-conversation.ndjson
    ./arxi-sim play -speed 4 testdata/scenarios/03-team-of-four.ndjson
    ./arxi-sim play -instant -width 40 testdata/scenarios/02-narrow-terminal.ndjson
    ./arxi-sim check testdata/scenarios/*.ndjson

In a terminal, `y` and `n` answer an approval, a typed line becomes a real
`run.prompt` at an `{"await": "prompt"}` barrier, and `ctrl+d` leaves — `ctrl+c`
clears the input line and only leaves on a second press in a row. The wheel is
left to the terminal so that a drag still selects, and `ctrl+↑`/`alt+↑`/`pgup`
move the conversation; `-mouse` trades that the other way. On Termux the trade has
no other side and the mouse is claimed for you, because a swipe there arrives as
the arrow keys and would otherwise walk the input's history. `-instant`
folds the whole log and prints the last frame as plain text, which is also what
happens when stdin is not a terminal — a pipe, a CI job, a `| less`. `check` loads
and validates without playing, which is what a recording gets edited under.

## Adding one

Every file in this directory is swept by `TestEveryScenarioFitsEveryWidth` and
`TestNoScenarioHandsOverMidSession` in `internal/ui`, both at every prefix of the
recording's steps: the first renders it as a document at seven widths and fails
on a line that overflows the frame, ends in a bare space, or carries a control
character; the second gives it a short screen and fails if a row is handed over
mid-session or if the live region is not exactly as tall as the screen. Dropping
a file in is all it takes to get that coverage — and all it took, twice, to find
a real defect.

What the sweeps cannot see is a row that is the right width and still wrong. Adding 10
turned up one by eye that both of them had been passing over for the whole corpus: a
space whose column landed exactly on the width was dropped by a rule written for
indentation, and a one- or two-column word behind it then measured as fitting on the row
the space had just left, so `rather than a warning` was drawn as `rather thana warning`.
Eight of the ten recordings carried at least one, at widths 20, 24, 40 and 100. Nothing
we had could report it: the row does not overflow, does not end in a bare space and
holds no control character, and the fixture that compares a markdown render against its
source compares them with the spaces taken out, which is exactly the difference. The
rule is now in `WrapSpans` — a space between two words is the break and is never
deleted, only an indent with no room left for a word gives way — and
`TestWrappingLosesNoWord` states it as a sweep of its own: whatever a wrapper does to
the spaces, the words on either side of one it drops have to stay two words. A render
is worth reading even when the suite is green.

Both sweeps are quadratic in step count, since every prefix re-folds the log and
re-renders the whole document, so a long recording is paid for at every width. 08
is what that costs in practice: it is 4.2 s of the width sweep's 5.6 s on its own,
and it took `internal/ui` from 2.6 s to 7.2 s. 09 is what the other shape costs: a
result of 31 lines makes a tall document out of 31 steps, and taking the file out
of the directory moves the same sweep from 4.6 s to 4.2 s. The width sweep is the
expensive half — the other one renders at one width against a short screen and
stays under a second for the whole corpus.

Style keys are the one thing a sweep cannot pick up, because they are not a
property of the text: two calls that draw the same dot in different colours read
identically to anything that inspects what a frame *says*. A recording that is
the first in the corpus to draw some style has to be named in `drawnKeys` in
`internal/ui/theme_test.go` as well, or `TestEveryDeclaredKeyIsDrawn` still
reports that key as never drawn. Two hand-built stand-ins have been retired that way so
far — 07 took over the failing tool call, 10 the markdown prose — and that is the point
of naming them: a fixture proves the renderer can draw a key, and a recording proves the
interface does.
