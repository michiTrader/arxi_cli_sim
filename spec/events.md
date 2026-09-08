# The event wire the player folds

Two repositories share one contract. [`spec/events.md`][arxi-spec] in the arxi
repository is the authority for what its log may contain; this file is what
`arxi-sim` actually reads, and the exact line where the two disagree is named
rather than discovered. Its purpose is to make drift a diff against something:
when either side changes its half of the wire, this file changes in the same
commit or the seam is lying.

Everything here is grounded in `internal/event` (what is accepted) and
`internal/state` (what is folded). If this file and those packages disagree,
the packages are right and this file is stale — fix the file.

[arxi-spec]: https://github.com/michiTrader/arxi/blob/main/spec/events.md

## Envelope

```json
{"seq": 42, "type": "tool.call", "scope": "run:r1", "source": "agent",
 "actor": "backend", "ts": "2026-08-26T14:03:11Z", "payload": {}}
```

The player reads `seq`, `type`, `scope`, `source`, `actor`, `ts` and `payload`,
and ignores every other field — `id`, `correlation_id`, `caused_by` and `depth`
included — so a newer runtime never breaks an older player. `source` is a
closed set: `human`, `agent`, `runtime`, `trigger`.

## The catalogue, as folded

| status | meaning |
|---|---|
| **contract** | Same names and shape as arxi's spec. |
| **renamed** | arxi emits this event; the field names differ. Must be reconciled at merge. |
| **extended** | arxi's fields plus additive ones. A log without them still plays. |
| **sim-only** | Not in arxi's contract at all. |
| **accepted** | Valid in a scenario, but the fold renders nothing for it. |

### Run and stage

| type | payload read | status |
|---|---|---|
| `run.started` | `run_id`, `actor`, `budget_usd`, `blueprint_sha` + `cwd?`, `git_branch?`, `members?` | contract + **extended** |
| `run.prompt` | `text` | contract |
| `run.quiescent` | `stage`, `diagnosis` (rendered as the run's idle notice) | contract |
| `stage.entered` | `stage`, `index` | contract |
| `run.paused`, `run.unpaused`, `run.cancelled`, `run.expired`, `run.result`, `stage.submitted`, `stage.advanced`, `stage.timeout` | — | **accepted** |

### Agents

| type | payload read | status |
|---|---|---|
| `agent.activated` | folds a member from `run.started.members` or first sight | contract |
| `agent.turn_done` | — | contract |
| `agent.failed` | `error` | contract |
| `agent.blocked` | `blocked_on`, `blocked_ref` (the Team monitor's remedy row) | contract |
| `agent.unblocked` | — | contract |
| `agent.steered`, `agent.notified` | `text`, `to?` | **extended** shapes: arxi's spec names these fields but declares no payload; these structs are the player's proposal |

### Tools and model

| type | payload read | status |
|---|---|---|
| `tool.call` | `tool`, `args` | contract |
| `tool.call_completed` | `tool`, `result{exit_code?, lines, summary, diff?}` | contract core (`tool`, `result?`) + **extended** result body |
| `tool.call_denied` | `tool`, `policy` | contract |
| `llm.response` | `cost_usd`, `tokens_in`, `tokens_out`, `model` + `context_used?`, `context_capacity?` | contract + **extended** (paired pointers; omission on a later response clears the previous value) |
| `llm.turn_started` | `turn` | **sim-only** |
| `llm.part` | `part_id`, `kind` (`thinking`\|`text`), `index`, `effort?` | **sim-only** |
| `llm.delta` | `part_id`, `text` | **sim-only** |
| `llm.part_done` | `part_id` | **sim-only** |
| `sim.task.created` / `.updated` / `.status_changed` | task roster for `/tasks` | **sim-only**, experimental |

### Resources, budget, human in the loop

| type | payload read | status |
|---|---|---|
| `lock.acquired`, `lock.released` | `resource`, `holder`, `mode` | **renamed**: arxi writes `key`, `expires_at?`, `previous_holder?`, `reason?` |
| `resource.conflict` | `resource`, `holder`, `waiter`, `wanted` | **renamed**: arxi writes `path`, `agents?` |
| `budget.warning`, `budget.exceeded` | `spent_usd`, `limit_usd`, `fraction` | **renamed**: arxi writes `tree_spent_usd`, `budget_usd`, `pct` — and the tree scoping is arxi's point, which the player's flat names erase |
| `inbox.created` | `inbox_id`, `kind`, `question`, `agent`, `on_timeout` | contract |
| `inbox.replied`, `inbox.timeout` | `inbox_id`, `text` | contract |
| `timer.tick` | — | **accepted** |
| `state.set`, `custom.*` | — | not in `event.Known` at all; a scenario carrying them is rejected |

## What arxi must decide before the merge

This is the list the wire extensions exist to argue. Ordered by how much of the
interface is unsupportable without each one:

1. **Transcript content.** `llm.turn_started`/`part`/`delta`/`part_done` exist
   because arxi's log records that a turn happened and what it cost, but not
   what was said — and a TUI cannot be replayed from costs. The canonical
   transcript work (roadmap Phase 5) is the natural home; whichever shape it
   settles on, the player adopts it wholesale.
2. **Write diffs.** `tool.call_completed.result.diff` exists because the runtime
   completes a write but says nothing about *what* changed, and a transcript
   that cannot show the change makes the reader open an editor to find out. The
   hunks are unified-diff rows because that is what an edit tool already has in
   hand.
3. **Tool call correlation.** arxi's log carries no call id, so the fold pairs a
   `tool.call_completed` with its `tool.call` by tool name, actor and arrival
   order (`findOpenTool`). With concurrent members that is a heuristic, and the
   code says so; a `call_id` on both events is the fix, and it is cheaper to
   decide before the host contract freezes than to retrofit.
4. **Member roster.** `run.started.members` mirrors `kernel.MemberConfig`
   because the runtime owns this data in the frozen blueprint snapshot but does
   not put it in the stream. The Team monitor is unsupportable without it.
5. **The three renames.** `LockPayload`, `ConflictPayload` and `BudgetPayload`
   predate the alignment of arxi's spec; when the host contract lands, the
   player renames the fields in `internal/event/payload.go` and nothing else —
   that is the file the code already points to as the one that changes.

## Merge rule

When arxi's host contract (roadmap Phase 1) makes real logs available, the
reconciliation is: edit `internal/event/event.go`, `internal/event/payload.go`
and this file in one commit, then re-record every scenario under
`testdata/scenarios/` against the real emitter. The fold in `internal/state`
and the UI in `internal/ui` must not change shape — that is the test that the
merge was an alignment and not a port.

A follow-up worth doing when this file stops being the only record: a test in
the player comparing `event.Known` and the payload structs against this file,
the way arxi's `TestEveryDocumentStatingTheSplitIsCurrent` guards its frozen
surface numbers.
