# Plan: pi-level UI customization for arxi-sim

This plan answers one question: what would it take for a user to shape this TUI the
way pi's users shape theirs — themes they can share, layouts they can compose,
panels and behaviour they can add — without breaking the two decisions the project
is built on: a near-stdlib-only binary, and closed vocabularies with named owners.

Reference point: pi (pi.dev) personalizes along two axes. **Values** — every colour,
key, glyph via JSON themes — which arxi-sim already matches with `[keys] [glyphs]
[styles] [anim]`. **Structure** — TypeScript extensions registering full TUI
components (`ctx.ui.custom`), event hooks (`pi.on`), commands, widgets, overlays,
distributed as npm packages — which arxi-sim has none of, by design. This plan
builds the second axis in three layers, each independently shippable.

Nothing here is a commitment to a date. Each phase needs its spec or ADR before
contracts freeze, per house rules.

## Options considered for the extension mechanism

**A — Everything declarative (config only).** Themes as packs plus a `[layout]`
table mapping slots to widget names. No user code, zero dependencies, zero new
risk. Reaches pi's *theme* story, not its *extension* story.

**B — Embedded interpreter (pi-style in-process).** Lua, JS (goja) or WASM
(wazero) inside the binary with a rich in-process API. Reaches the most, costs the
most: one heavyweight dependency (against the stated rule), no isolation without
extra machinery, a second runtime to govern, and it pre-empts a decision the arxi
vision explicitly keeps open (30-vision §30.5: compiled adapters vs subprocess vs
WASM vs remote workers). Rejected for now.

**C — Subprocess extensions speaking NDJSON.** Extensions are external processes;
stdio carries a versioned protocol. Zero new dependencies (`os/exec` is stdlib),
crash isolation, any language, governable by a declared-capability manifest. The
house already speaks this way: `arxi serve`, the event log, the scenario wire.
Costs: latency discipline, process lifecycle (Windows procgroups — a pattern arxi's
`internal/toolrun` already solved), distribution less frictionless than npm.

**D — Go native plugins.** `plugin` does not exist on Windows and is fragile
across Go versions. Disqualified on arrival.

**Recommended: E — layered hybrid.** A first (declarative core), C as the spine
(subprocess host), the ExtPanel rows/spans contract as the first shape of user
components — free-form template-views stay out of scope until a phase argues for
them — and WASM deferred until arxi chooses it. Every layer ships value alone; none
breaks a written decision; and the governance model (manifest + consent gate +
attribution) fixes pi's known weakness (extensions run with full user permissions,
trusted-source only).

```
┌────────────────────────── arxi-sim (Go, stdlib-only) ────────────────────────┐
│ cmd/arxi-sim     flags, surfaces, extension spawn/supervise                  │
│ internal/config  Document (lossless) + [theme] [layout] [extensions.<name>]  │
│ internal/app     dispatch → action registry (core + ext:<name>:<action>)     │
│                  chrome() reads [layout] (default = today, byte-identical)   │
│                  consent gate as a full view (the /config pattern)           │
│ internal/ui      Widget{Name,Slot,Fallback,Render,Animated} — exists         │
│                  + ExtPanel: rows/spans rendered through the theme keys      │
│ internal/ext     NEW: manifest, handshake, supervision, permission state     │
│        │ stdio NDJSON (spec/extensions.md; changes with the code)            │
└────────┼─────────────────────────────────────────────────────────────────────┘
         ▼  extension process, any language
   manifest.toml + executable; declares caps: events.subscribe,
   inbox.answer, actions.register, panel.render
```

## Target architecture

What already exists and is reused as-is:

- `ui.Widget` (`internal/ui/widget.go:19`) — `Name/Slot/Fallback/Animated/Render`
  is pi's component interface in Go shape. `Place` already resolves slots with
  fallbacks; `ChromeFor` is the default composition.
- `config.Document` (`internal/config/document.go:71`) — lossless edit, fingerprint,
  conflict and validation errors: the persistence layer themes and layout need.
- `App.dispatch` / `key` / `runSlash` (`internal/app/app.go:645,598,1750`) — one
  action registry and one keymap to extend with a namespace.
- The event fold — extensions receive the same events the player folds; they never
  write state directly, they propose events through the same gates a human uses.

New pieces:

1. **Widget registry.** `Name()` becomes the stable ID. `chrome()` moves from
   hard-coded order to a declared list the config can override per slot, with
   responsive tiers declared rather than coded — the declared list itself landed
   in Phase 0 (`defaultComposition`); what remains here is the config override.
   Unknown widget name in `[layout]`
   is an error with a line number, like every other vocabulary.

2. **Theme packs.** A theme is a `.toml` holding only `[glyphs]`, `[styles]` and
   optionally `[anim]`. Resolution: flag > config `theme = "name"` > default.
   Themes live in a themes dir; `check` validates them as ordinary Documents;
   themes layer over (never replace) the user's config so partial themes are legal.

3. **Extension host (`internal/ext`).** One package, no imports of `ui`/`app`
   (arch-tested, like the config controller's seam). Owns: manifest parsing,
   spawn and supervision (restart policy, timeouts, kill on exit), the handshake —
   `hello` advertises protocol + capability vocabulary, `ready` declares the
   extension's requested capabilities, anything undeclared is refused with an
   `unknown_type`/`not_declared` distinction copied from arxi's serve.

   The wire is `ext/v1`, and `spec/extensions.md` defines its capability vocabulary
   *against* arxi's `host/v1`, not in parallel: `inbox.answer` takes the shape of
   host/v1's `Answer`/`Approve`/`Reject` and their `DecisionKind`, `events.subscribe`
   the shape of `Subscribe` + `EventFilter`, and a grant answers to a `Principal` the
   way host/v1's `authorize` does. The transports differ — NDJSON stdio here,
   in-process Go there — on purpose; the decision shapes must not. The cross-reference
   rule is the one `spec/events.md` already lives by: when `host/v1` changes,
   `extensions.md` changes in the same commit or the seam is lying. And the sim must
   not presume arxi roadmap Phase 4's approval binding (principal, call ID, digest,
   single-use): it inherits whatever `host/v1` approves with, through that rule. The
   Windows procgroup supervision copies arxi's `internal/toolrun` pattern — a copy
   with its own orphan tests, never an import across repositories.

4. **Namespaced actions.** `ext:<name>:<action>` enters `ActionKeys`; `[keys]`
   binds them explicitly — extensions ship no default keys (the user grants the
   row, the same way they grant capabilities). `TestDefaultBindingsAreCanonicalAndReachable`
   gets a documented carve-out for the namespace rather than a silent exception.

5. **ExtPanel.** A `ui.Widget` whose rows come from the extension as spans
   referencing theme keys (or literal styles, only if the manifest declares it and
   the user consents). Width negotiation on `view.update`; the panel is cached and
   invalidated by the same tick clock; nothing it sends may end a row in bare air —
   the existing `Line`/`Overflow` machinery enforces it, not politeness.

6. **Governance.** First run of an unknown extension opens a consent full-frame
   listing its declared capabilities; grants persist as
   `[extensions.<name>] allow = [...]`. Every extension-originated effect lands in
   the log with attribution — the auditability pi skips. Attribution rides the
   existing closed vocabulary: the proposal enters through the same gates a human
   uses, so its `source` stays inside arxi's closed set — `human`, `agent`,
   `runtime`, `trigger`; which one per capability is a decision `spec/extensions.md`
   pins by argument, not by invention — and `actor` carries the extension name. A
   new `source` value is an arxi wire decision — it is recorded as one in
   `spec/events.md` ("what arxi must decide") and is never a sim-local addition.

## Phases and task division

Sizing is S/M/L relative to this codebase's norms (golden files, mutation
families, no silent drift). Phases 1–3 each end with a shippable cut.

**Phase 0 — Groundwork (S). Done.**
- Inventory: every `chrome()`/`Place` call site, every style key drawn (extend
  `TestEveryDeclaredKeyIsDrawn` into a coverage audit). Landed: every key is now
  attributed to named sources — corpus frames at each width, chrome widgets rendered
  in isolation (approval, tasks closed and open, header with the marked cut, recap),
  surfaces, and the literal stand-ins for app-package vocabularies — so an invented
  key names its inventor and a lost key names the drawer that lost it.
- Extract the composition from `chrome()` into a data structure whose default
  reproduces today byte for byte (`TestTheDefaultCompositionIsToday`). Landed as
  `compositionStep`/`defaultComposition` in `internal/app/app.go`: the order pinned
  by name per fixture, the frames pinned as goldens, and
  `TestTheCompositionNamesItsWidgets` keeping a step's name and `Name()` from
  drifting apart.
- Files: `internal/app/app.go` plus its tests and goldens; `internal/ui/widget.go`
  needed no change. No visible change — the extraction was verified byte for byte
  against the pre-refactor frames.

**Phase 1 — Themes and layout (M). Done.**
- 1.1 Theme packs: shipped. Packs live under the portable user config dir at
  `arxi-sim/themes/<name>.toml`; `-theme` > `[ui] theme` > none; a typed
  `-theme ""` is the escape hatch. Packs carry only `[glyphs]`, `[styles]` and
  `[anim]`, are checked through the config parser in a theme role, and layer
  under the user's own file per key/field (user file > pack > shipped default),
  so partial packs are legal and `/config` edits never land behind the pack.
  The contract and its argument are pinned in `spec/look.md`.
- 1.2 `[layout]`: shipped. Slot → ordered widget names, `""` vetoes a slot;
  `slot@width<N` and `slot@height<N` are declared tiers, height winning when
  both match and no height tier matching a fold. Unknown names, wrong-slot
  names, duplicate names, unused slots and malformed tiers are refused with
  line numbers. The duplicate-name wart is decided: end-of-scenario stays
  `notice`, the armed second-press warning is `interrupt`; every composition
  name is unique and `arxi-sim keys` prints name + slot + purpose.
- 1.3 `/config` has a read-only Layout category, resolving tiers against the
  live frame, and Overview names the effective theme and themes dir. Glyph and
  style inspectors attribute pack-supplied values without pretending they were
  written into the user's file. Editing remains deliberately later.
- Doctrine: default composition goldens remain untouched; configured frames
  live in the `layout-*.frame` mutation family. Theme layering, role rejection,
  layout vocabulary/tiers, defaults and the mutation family are test-pinned.
- Files: `spec/look.md`, `internal/config`, `internal/app`, `cmd/arxi-sim`, and
  tests/goldens. Zero theme/layout configured remains the byte-identical path.

**Phase 2 — Extension host (L). Done.**
- 2.1 `internal/ext`: manifest, spawn, handshake and supervision are shipped, including
  explicit minimal environment and manifest-relative executable resolution.
- 2.2 Event tap: accepted recorded, human and extension events publish complete event
  JSON asynchronously through the bounded supervisor queue.
- 2.3 Actions: process-scoped registration, collision/removal handling, dynamic action
  routing and `/ext:<name> <action> [args...]` are integrated through the nonblocking
  host-to-extension `actions.invoke` message. Extensions ship no default keys.
- 2.4 `inbox.answer` is gated against the current inbox and attributed to the extension.
- 2.5 Consent is a full-frame pre-spawn gate; exact grants and a persisted manifest
  identity digest cover name, version, executable, args and capabilities. Rejection is
  session-local and never spawns.
- 2.6 `spec/extensions.md`, protocol validation, supervisor tests, two portable Go
  examples, and recorded codec conformance fixtures are shipped.

**Phase 3 — Extension views (L). Done.**
- 3.1 `ExtPanel` and ext/v2 `view.update` are integrated through the app-owned
  manager seam. Panels are deterministically named `extension:panel`; `/panels`
  lists them and `/panel [extension:panel]` opens or switches them.
- 3.2 Focus is explicit (command/action or pointer press), and close, removal,
  disconnect, cancel, or interrupt revoke focus and pointer capture. Input is
  normalized as key/action/text/paste/wheel/pointer with panel-local geometry;
  host quit remains unconditional and first-party modal/full views win routing.
- 3.3 Updates replace the cache immediately but visual redraw is coalesced onto a
  one-shot 120 ms deadline. Resize requests are deduplicated and nonblocking;
  the last size-specific frame remains safely clipped and marked stale until a
  matching update arrives. With no extensions configured the path remains the
  existing byte-identical nil-channel path.
- Extension overlays remain deferred: ext/v2 deliberately has no `overlay.render`.
  First-party overlays block panel and dynamic-extension input.

**Phase 4 — Distribution and the WASM decision (M).**
- Local install = copy dir + trust prompt; `extensions list` command.
- Decision gate: WASM in-process only if arxi's host contract adopts it
  (30-vision §30.9 leaves this open). Until then the subprocess protocol is the
  only runtime — one ADR at the arxi level, not a sim-local exception. The same
  gate covers the capability manifest: 30-vision §30.5 already names third-party
  capabilities as versioned and attributable, so aligning the manifest's format
  with `host/v1`'s `CapabilitySet` is an arxi-level ADR too, not a sim-local
  choice. Until either lands, the manifest stays sim-local and is named as such
  in `spec/extensions.md`.

## Invariants this plan must not break

1. Zero extensions configured → byte-identical binary behaviour (the Shimmer
   zero-value rule, generalized). Its test home already exists:
   `TestTheDefaultCompositionIsToday` and the composition goldens pin the frame
   bytes, and the coverage audit holds the theme vocabulary to the same rule.
2. The kernel fold stays pure; extensions propose, they never write.
3. Vocabularies stay closed and owner-declared; the extension surface is a *new*
   declared vocabulary, namespaced, checked at handshake.
4. Every extension effect is an attributed event in the log.
5. No dependency crosses the stdlib line without an ADR that names its cost.

## Risks

| Risk | Mitigation |
|---|---|
| Subprocess latency in rendering | push-based `view.update`, cached frames, refresh budget |
| Windows process lifecycle | copy arxi's procgroup_windows pattern, with its own kill-on-exit and orphan tests |
| Keymap collisions | `ext:` namespace; user binds explicitly; canonical-binding test carve-out documented |
| Backpressure floods | drop-and-notify rule pinned by test; the fold never waits on an extension |
| Golden-file churn | every default preserved; golden diffs are review events, not noise; the arxi merge (`spec/events.md`'s rule) churns them exactly once, by design — fold and UI must not change shape through it |
| Malicious extensions | manifest + consent gate + attribution; no shell-out, no env inheritance beyond declared |
