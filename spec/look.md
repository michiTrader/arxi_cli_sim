# The look: theme packs and `[layout]`

`docs/PLAN-ui-customization.md` Phase 1 froze two vocabularies: theme packs
(1.1) and the `[layout]` table (1.2). This file is where their contracts live.
Everything here is grounded in `internal/config` (what a file may say) and
`internal/app` (what the composition obeys). If this file and those packages
disagree, the packages are right and this file is stale — fix the file.

## Theme packs

A theme pack is a `.toml` holding only `[glyphs]`, `[styles]` and optionally
`[anim]` — taste, and nothing else. Any other section is refused with a line
number when the file is loaded as a theme: `[keys]` in a theme is refused
because a theme carries no behaviour, and `[ui]` is refused because a theme is
not addressed by another theme. The grammar and the key vocabularies are the
config file's own (`ui.GlyphKeys`, `ui.Keys`, the `[anim]` fields); a theme is
validated by the same parser, not a second one.

**Where themes live.** `<user config dir>/arxi-sim/themes/<name>.toml`, by the
same portable rule the config file already resolves by (`os.UserConfigDir`):
`~/.config/arxi-sim/themes/` on Linux and macOS, `%AppData%\arxi-sim\themes\`
on Windows. A theme's name is its file name without `.toml`, and a theme name
may not contain a path separator — the themes dir is the whole address space.

**Selection.** `-theme name` beats `[ui] theme = "name"` beats no theme at all.
`-theme ""` wears none, whatever the config says — the same escape hatch
`-config ""` already is. A selected theme that is missing or invalid is an
error and never a silent fall-back to the shipped look: the argument that
named it was typed for this run.

**Layering, per key.** The user's config wins over the theme pack, and the
theme pack wins over the shipped default, per key of `[styles]` and
`[glyphs]` and per field of `[anim]`. Three reasons, and the first is the
decisive one:

1. `/config` edits write to the user's config. If a theme outranked it,
   editing a themed key would change nothing on screen and the view would lie.
2. The house rule for every other setting is what you typed beats the file and
   the file beats the shipped default; a theme is a shipped default with a
   name, and it slots into the same ladder one rung down.
3. Partial themes are thereby legal: a pack that names six keys leaves every
   other key to whoever had the previous word.

`check` validates a `.toml` inside the themes dir as a theme (the role rule
above applied); any other `.toml` as an ordinary config, and a config that
names a theme gets that pack resolved and validated with it, so one `check`
reports both files.

## `[layout]`

`[layout]` rewrites the composition: which chrome rows are installed, in what
order, per slot. One row per setting, like every table in the file:

```toml
[layout]
above_input = "tasks, recap"
below_input = "approval, effort"
bottom = ""            # veto: no status row
below_input@width<60 = "approval"
```

**The vocabulary.** Slot names are `ui.SlotKeys` verbatim (`top`, `right`,
`above_input`, `below_input`, `bottom`, …). Widget names are the composition's
step names, printed by `arxi-sim keys` under *widgets* with the slot each one
asks for. The composition's known wart — two steps both named `notice` — is
decided here by namespacing: the end-of-scenario row keeps the name `notice`,
and the armed second-press warning is `interrupt`. Every step name is now
unique, which is what makes a layout row addressable at all.

**What a row may say.**

- The value is an ordered list of widget names, comma-separated, in stacking
  order. A name the composition does not know is refused with a line number.
- A name written under a slot it never asks for is refused with a line number
  naming the slot it belongs to: `[layout]` reorders and vetoes, it does not
  move widgets between slots — the slot a widget asks for is the widget's own
  answer, and `Place` still resolves it.
- The same name twice in one list is refused, as is the same key twice.
- `""` vetoes the slot: every widget that would have stacked there is left
  out.
- A slot the file never names keeps the composition's default rows in the
  default order. Veto is opt-in per named slot; naming one slot does not
  touch the others.
- A slot no widget asks for may not be addressed at all — the error names the
  slots the composition draws into. An empty vocabulary is a closed one.

**Tiers.** A key may carry one condition, `slot@width<N` or `slot@height<N`
(`N` a whole number, 1 or more): the row applies only while the terminal is
narrower than `N` columns, or the frame shorter than `N` rows. Selection per
slot, per frame:

- Among matching `width` rows the largest `N` (narrowest bracket) wins; among
  matching `height` rows the same. Unmatched rows of the same kind are inert.
- If a `width` row and a `height` row both match, the `height` row wins:
  height is the scarcer axis — the transcript scrolls and the chrome stacks,
  so a height tier is a claim about the whole frame's budget and outranks a
  claim about its width.
- A matching tier replaces the slot's rows outright; tiers do not merge with
  the base or with each other. With nothing matching, an unconditional row
  stands, and with neither, the default.
- A fold has no height (`-instant`, a pipe), so `height` rows never match
  there; width rows match against the fold's own width.

**Build order is behaviour, not layout.** The composition's builders all run,
in the default order, whatever the layout says — the effort slider is handed
its tick and an expired arm retires exactly as before. Only the installation
order and the veto change. No `[layout]` section at all means byte-identical
frames: the default goldens are the proof, and they do not move.

**`/config`** shows a read-only Layout category (one row per slot: effective
order, tiers, and the default beside it) and a Theme row (effective name, its
source, the themes dir). Editing both is a later phase; the inspector comes
first, so a reader can see what the file actually said.

## Doctrine

Default golden files do not move: a theme or a layout changes frames only when
a file asks for it, and the mutation is pinned under its own golden family —
`layout-*.frame` in `internal/app/testdata` — the same way
`composition-*.frame` pins the default. Every error carries `file:line:`.
