# Plan: ten requested changes, and what each one costs

Ten changes were asked for at once, and eleven more arrived while they were being built.
Seven of the ten are in, and ten of the eleven. What is left is one item that can be built now
and needs no decision from you, and three that are not ours to implement at all.

This file exists so that the order and the reasoning survive the next session. Nothing in
it is a commitment to a date, and every "not possible" here names the thing that makes it
impossible rather than leaving it as an opinion.

One constraint shapes the whole list: **maximum customization**. That is why the config
file was the first piece of work even though it was not one of the ten — two of the ten are
only fully satisfiable through it, and it is the difference between an interface I chose
and an interface you can choose.

## At a glance

| #  | Asked for                                        | Verdict |
|----|--------------------------------------------------|---------|
| 1  | `Thought for 0.5s (high effort)` with no `✻`      | **done** |
| 2  | shift+enter / ctrl+enter for a second line         | **done**; ctrl+enter starts a row on this terminal today |
| 3  | plain up/down = input history                     | **done**, on the plain arrows; the scroll went to the keyboard |
| 4  | your message pinned on top while scrolling        | **done**, while scrolled only; a page key pays for its rows |
| 8  | weaker green/red diff backgrounds                 | **done**, and the hue question is answered: green added, red removed |
| 9  | a background behind user messages by default      | **done** |
| 10 | no `prompt` written into the border, and settable | **done** |
| —  | a wheel notch worth three lines, not two          | **done**: `defaultWheelLines = 3`, still `-scroll`; 1 on Termux |
| —  | `ctrl+↑`/`ctrl+↓` worth the same notch             | **done**; the single row moved to `alt+↑`/`alt+↓` |
| —  | the removed band red rather than pink             | **done**: `#8a2620`, and the blue may no longer outrun the green |
| —  | a plain drag to select, with no shift             | **done**: the mouse is released on a desktop; `-mouse` claims it back |
| —  | a config file: `[keys]`, `[glyphs]`, `[styles]`   | **done**: `-config`, the default path, and `check` on a `.toml` |
| —  | ctrl+c clears the line; twice, or ctrl+d, leaves  | **done**: `ActionInterrupt`, and the second press is the door |
| —  | a swipe on Termux scrolls, not walks the history  | **done**: the mouse is claimed there, one row per report |
| —  | lime, light indigo and fuchsia in the code        | **done**, six named colours; a number is amber, a comment slate |
| —  | the two bottom rows off the edge of the terminal  | **done**: `bottomMargin = 1`, spent before the text is fitted |
| —  | a light sweeping the input bar while you idle     | **done**, and the verb too: `ui.Shimmer`, `-shine=false` stops it |
| B2 | multi-agent status and a team roster sheet        | **done**: live member glyphs, Ctrl+T, and `/team` |
| —  | app-owned Tasks monitor                          | **done**: `/tasks`, responsive full-frame task state |
| —  | app-owned Config editor                          | **done**: `/config`, live previews, provenance, and persistence |
| 6  | a draggable scrollbar pill, accelerating wheel     | **done**: the pill drags and the notch ramps, both under `-mouse` |
| 5  | ctrl+wheel to resize the terminal                 | **not ours**: the emulator owns that gesture |
| 7  | a selection that survives a scroll                | **not fixable** inside the alternate screen; accepted |
| —  | shift+arrow selecting past the edge of the screen | **not fixable** either, and for the same reason; below |

## Done, and what holds each one down

**1 — the thought loses its star.** `thinking.marker` now defaults to two spaces rather
than `✻ `, so a finished thought reads as a sentence and keeps the two columns of indent
every other row in the transcript has. The star is one line of config away:
`thinking.marker = "✻ "`. Held by `TestAFinishedThoughtHasNoMarker` and the golden file.

**8 — the diff bands are a green and a red of matched weight.** `diff.removed` went from
`#4b1c1f` to `#3a1518`, then to `#7f2830`, and — when you called that one *rosado* — to
`#8a2620`, which is a red that leans the other way. `diff.added` went from `#17304d` to a dark
blue `#12253a`, then — when you called the blue *demasiado fuerte* — to `#0e2216`, which came
out **black** and you rejected it in exactly those terms: *verde o azul, no tan oscuro ni tan
claro*. It is `#1c5a38` now, a green a reader can see is a green. The hue was argued for on
colour-blindness grounds and the argument lost to the reader who has to read it; what
survives of it is the gutter sign, which stays a brighter bold green or red and is the one
thing that tells the two sides apart when the hue does not.

The pink was a hue and not a weight, which is why it took a new assertion to catch: `#7f2830`
is dominantly red, sits inside the window and is matched against the green, so every check
that existed passed it. What was wrong is that its blue outran its green by eight, and past
red, more blue than green is the road to magenta. `#8a2620` cuts the blue by a third and
leaves the green where it stood, so the tilt is towards orange instead — a dark red rather
than a washed one — at the same weight.

The test that holds this asserts relations and not hexes, so the bands can be re-tuned
without editing it: both bands have to be RGB (no indexed colour is both a colour and a
surface to put text on, and its actual shade is the terminal's opinion), the added band has
to be greener than it is anything else, the removed one redder **and no bluer than it is
green**, the two may not differ by more than a shade in Rec. 601 luma — neither side of a
change gets to shout over the other — and the two signs may not be the same colour. The
weight has a window rather than a ceiling now, and the window is where the two corrections
meet: **48 to 80 of 255**, the floor above the band that read as black (26) and above the
quieter wash under your own turns (44), the ceiling where a `#cccccc` foreground stops
clearing 4.5:1 on a green of that weight. Both bands sit inside it, at 67 apiece.

**9 — a prompt is a rectangle now.** A new style key, `prompt.band`, is stamped on every
span of every row of a user message and on the padding that runs it out to the right edge.
It is carried as `Span.Fill` rather than `Span.Style` on purpose: the two are merged at emit
as Style over Fill, so a bold word inside your message keeps being bold and gains the
background instead of the wash flattening the line into one colour. Emptying the key removes
the band. Held by four tests — the width of every row, every rune of the text surviving the
padding, the bold word keeping its attribute, and the golden file — and by six mutations.

**10 — the border stopped describing itself.** The marker inside the box already says a
prompt goes there, so `prompt` in the rule was the frame naming itself. The default is now
an unbroken rule; `-title "branch: main"` or `InputTitle` in a config writes a word into it,
whole or not at all. Held at both ends: `TestTheBorderHasNoTitleByDefault` for the default,
`TestTheInputTitleComesFromTheConfig` for the wire from the flag to a drawn frame.

**3 — the arrows are the history, and the scroll is the keyboard's.** The argument and the whole
table are in *Decided: what the arrows do* below, in the section that used to ask and then
answered it wrongly, twice. The short version: plain `↑`/`↓` recall the input history the way a
shell does, `ctrl+↑`/`ctrl+↓` move a wheel-sized notch, `alt+↑`/`alt+↓` move one row,
`shift+↑`/`shift+↓` walk your own turns, and the wheel itself scrolls — with or without shift —
under `-mouse`. You said the history had to be possible on the plain arrows because it works in
Claude Code, in every TUI editor and in ishakat, and you were right, for a reason that survives
both turns of the argument: alternate scroll only rewrites a notch into arrow bytes **while no
program has claimed the mouse**, so the arrows are ours either way — by claiming the mouse, or by
switching the rewriting off, which is what the default does now so that no arrow ever arrives
unpressed. Held by the walk in
`TestTheArrowsAreTheHistoryAndTheWheelIsTheWheel`, which presses each key and checks both the
line it recalled and the row the window is on, by `TestJumpingWalksTheReadersOwnTurns` for the
landmarks, by `TestTheMouseIsTheReadersUnlessAskedFor` for the modes that make it true, and by
`TestPromptRowsNamesEveryTurnAndNothingElse` for the seam underneath the jumps.

**A wheel notch is three lines**, whole: `defaultWheelLines` in `internal/app/app.go`. It used
to be three lines per *step* with the terminal deciding how many steps a notch was worth,
which is why a spin used to cross a screen and a notch used to move about nine rows. Under
`-mouse` a notch is now one report and three rows; by default the wheel is the terminal's and
the number is what `ctrl+↑`/`ctrl+↓` move instead. `-scroll` is still the setting either way,
because how far a flick of a finger should carry is a fact about a mouse and a hand rather than
something this program can know. The number is asserted literally, because every other case in that test is
arithmetic about whatever the constant happens to be and would not have noticed it going back
to two.

**And `ctrl+↑`/`ctrl+↓` are that same notch**, which is what you asked for: they are the
keyboard's spelling of the wheel, not a finer version of it, so a hand that has learnt the
wheel's step keeps it when it leaves the mouse alone. One number, `-scroll`, sets both. The
single row had to go somewhere, because a declared action with no default key fails
`TestDefaultBindingsAreCanonicalAndReachable`, and it went to `alt+↑`/`alt+↓` — alt is where
the other fine moves already live (`alt+←`/`alt+→` are the word steps), and it arrives as
`CSI 1;3A`, unlike the `ctrl+shift+↑` that never reached you at all. That last point is the one
thing here your terminal has to settle rather than the suite: if alt+arrow turns out to be
claimed by your emulator or your desktop, the single row moves again and only a keypress can
tell us.

**And the config file reads.** `internal/config` is the loader the vocabulary was waiting for:
five flat tables — `[keys]`, `[glyphs]`, `[styles]`, `[input] title`, `[scroll] lines` — one
`key = "value"` per line, `#` comments, and no dependency added. The `.toml` name is a promise
about the shape and not the grammar: a file this reader accepts a real parser would accept
too, with one exception kept on purpose, a bare key on the left, because `+` is not a
character TOML allows in a bare one and `ctrl+up = scroll-up-fast` is what a person writes.
Quotes are there for the values that need them — `prompt.marker = "❯ "` is a marker and `"❯"`
is a different one — and `""` is a real value in two places: it takes a binding away and it
takes the wash off a prompt.

Three doors, because three things can be true at startup. `-config p` is a request, so a file
that is missing or broken there is an error rather than a mistyped path running the program
unconfigured and never saying so; no `-config` at all reads
`~/.config/arxi-sim/config.toml` (`%AppData%\arxi-sim\config.toml` on Windows) when there is
one, and having none is the ordinary case and not a problem; and `-config ""` reads nothing,
which is the only way to see the shipped look on a machine that has a file. A flag you typed
still beats the file, which is what `flag.Visit` is for: `-title ""` and not having mentioned
`-title` are different sentences. `arxi-sim check f.toml` loads without playing and prints
`[keys] 4, [glyphs] 3, [styles] 4, [input] title="prompt", [scroll] lines=5` — counts and not
tables, because the question in front of a config that did nothing is whether the program read
the file you have been editing.

The vocabularies did not move, and that was the point of doing this last rather than first.
Every name a file may write is still declared by whoever owns it — `app.ActionKeys`,
`ui.GlyphKeys`, `ui.Keys`, `term.ParseKey` — and the reader checks membership against those
tables and then hands the maps to the real constructors, which keep the last word. The check
is duplicated for exactly one reason: a line number. `prompt.txt is not a style key` sends a
reader hunting through a file for a word they cannot see, and
`config.toml:12: prompt.txt is not a style key` does not. Every problem in the file is
reported rather than the first, so three typos cost one run to fix — and a deleted `[keys]`
header is reported once rather than once per orphaned line.

Held by eleven tests: one fixture carrying every section at once, twenty-seven files it
refuses with a `t.Run` each, twelve spellings of a value, ten pinned line numbers, and the zero
config, which has to build exactly the program we ship or every user without a file is running
a fourth configuration nobody tests. Then by 33 mutations, which is where two of those tests
came from — dropping four of the reader's five membership checks left every bad file still
rejected, by the constructor, with no line on the message, and nothing failed until the line
numbers were pinned one per check. Then by hand, against the real binary: a good file, each
error shape, both `-config` spellings, `check`, and an override winning over `-ascii`.

One asymmetry this turned up and did not fix: the config refuses `lines = 0` and `lines = -2`,
and `-scroll` still takes both — `0` becomes 3 because `app.New` fills a zero, and `-2`
reaches `WheelLines` as `-2`. `main` validates none of its numeric flags. It is a small,
separable change and it is not part of this one.

**2 — the second line is on shift+enter and ctrl+enter, and enter still sends.** Both keys are
`newline` in the default keymap, because the two hands that reach for them are different and the
action is the same. Getting them there took more than a map entry, and it took two unrelated
things, because the two chords are not one problem. shift+enter first: in the encoding every
terminal has shipped with since the VT100 a modifier on the return key is *dropped* before
anything leaves, so shift+enter is `0x0d` and is enter. A binding on a chord that cannot arrive
is worse than no binding, so the keyboard is asked to tell them apart. `ui.Emitter.Enter` pushes
`CSI > 1 u`, the first progressive-enhancement flag
of the Kitty keyboard protocol, and a terminal that speaks it reports shift+enter as
`CSI 13;2u` and ctrl+enter as `CSI 13;5u`. One flag and not the whole set, deliberately: the
second asks for key-release events as well, the decoder does not filter them, and every binding
in the program would fire twice. A terminal that has never heard of the sequence ignores it,
which is the entire cost of asking — six bytes. What it is not is a mode: it is a stack shared
with whatever launched us, so `Exit` pops exactly one entry and only if we pushed one, guarded
by `e.pushed`, because `Exit` runs from a deferred call *and* from the signal handler and an
unguarded second pop would take the keyboard out from under the program above us.

The decoder needed nothing at all, which is the one piece of luck here: `{"\x1b[13;2u",
"shift+enter"}` and `{"\x1b[13;5u", "ctrl+enter"}` are rows in its own table, `\x1b[?1u` — the
flags reply, which a terminal may send unasked — was already consumed and reported as nothing,
and flag 1 also makes esc arrive whole as `CSI 27u` instead of as a lone `0x1b` waiting out the
50 ms timeout. `ctrl+h`, `ctrl+i` and `ctrl+m` are unbindable in practice, because their bytes are
`0x08`, `0x09` and `0x0d` and the decoder names those backspace, tab and enter — a binding on them
is a row no keypress can reach. `ctrl+j` was listed beside them in an earlier draft of this page,
on the same reasoning and wrongly, and the next paragraph is what finding that out cost.
`alt+enter` was the previous answer here and
is now gone, its absence asserted rather than merely omitted: it is ESC CR, it needs no protocol
at all and the decoder has always read it, and Windows Terminal and conhost both bind it to
fullscreen and never pass it on — so on the machine this is written on it could only ever look
broken. On a terminal that neither eats it nor speaks `CSI u` it is one config line away:
`"alt+enter" = newline` under `[keys]`.

**And ctrl+enter needed no protocol at all — it needed a byte the decoder was throwing away.**
This page used to tell you the chord could not arrive before 1.25 and that the remedy was on your
machine. You answered that Claude Code, ishakat and pi all break a line on this very terminal, and
you were right: the claim was wrong. Windows Terminal and conhost do not *drop* ctrl on the return
key — ctrl folds the key into the control range, so ctrl+enter leaves as `0x0a`, which is LF, which
is `ctrl+j`'s own byte and the one spelling of the chord a pre-`CSI u` terminal can express.
`term.decodeText` was collapsing `0x0d` and `0x0a` into a single `KeyEnter`, and that fold is
precisely what made ctrl+enter send the line. It no longer folds, and naming the byte took no new
code: the generic control-range case computes `rune(c + 0x60)`, so `0x0a` names itself `ctrl+j`,
`term.ParseKey("ctrl+j")` round-trips it, and `"ctrl+j": ActionNewline` in `DefaultBindings` is the
whole of the fix. That is the binding ishakat and Claude Code carry, and for this reason. Anyone who
really means ctrl+j on its own gets a newline, which is what ctrl+j has meant since the teletype.
Raw mode is what makes the split safe: `MakeRaw` clears `ICRNL`, so a plain return stays `0x0d` and
cannot be turned into a newline behind our back, and a pasted LF never reaches this code at all,
because `decodeCSI` consumes the whole bracketed-paste payload before `decodeText` sees a byte of it.

One thing measured rather than assumed, and it is now a statement about shift+enter alone:
the terminal here is **Windows Terminal 1.24.11911.0**, and the Kitty keyboard protocol first
ships in **1.25**. So the request goes out, 1.24 ignores it, and shift+enter arrives as `0x0d`
— enter — and sends the line. The binding is right and starts
working the moment 1.25 lands; until then ctrl+enter is the chord that starts a row, which is the
outcome you said you would be content with. To have the shift spelling before the update it is two
`sendInput` keybindings in your own `settings.json` sending `\x1b[13;2u` and `\x1b[13;5u` —
your machine, not this repository.

The editor was the real cost, as predicted. `Input` holds the breaks, `Render` splits on them
and hard-wraps each segment so the box grows a row per line, and `cursorAt` counts them. A
cursor sitting *on* a break is the one position with no cell of its own — a break is where a
row ends, not something drawn — so it reports the end of the row the break closes, which
inside the box is a padding cell and is the honest place for it, because what gets typed there
joins the row above. Unboxed there is no padding cell to sit in: the column it wants is the
terminal's right edge plus one, where the cursor lands wherever the emulator likes, so it is
clamped one column back. That clamp is reachable only by a cursor on a break at a width below
the box's threshold, which is why `TestInputCursorSitsOnTheCharacter` walks every cursor index
at widths 3, 5, 6 and 7 rather than trusting `SetText`, which always leaves the cursor at the
end.

A paste keeps its rows now, which inverted an assertion that used to say the opposite:
`TestAPasteBecomesOneLine` is `TestAPasteKeepsItsLinesAndNothingElse`, and each arm is a thing
a clipboard can carry that a row cannot — a CR becomes a break, a tab becomes a space, an ESC
and a DEL are dropped. That last pair is the point of the test rather than tidiness: a paste is
the only path by which text nobody typed reaches the input line, so it is the only path by
which a sequence that addresses the terminal could.

Held by the box's three existing rules run over seven texts carrying breaks at all 21 widths,
by `TestBoxGrowsARowPerLine` — which asserts the row **count** as well as the rejoined text,
because a layout that split on some other byte leaves the break inside a single row and
rejoining one row gives the text back — and by
`TestTheSecondLineIsAKeyAndTheFirstStillSubmits`, which runs all three names — shift+enter,
ctrl+enter and ctrl+j — and then pins
`alt+enter`'s absence, because an omission and a decision look identical in a map. The bytes are
held separately by `TestTheKeyboardIsAskedToDisambiguate`: the flag is spelled into the expected
output as `\x1b[>1u` rather than computed from the constant the emitter uses, the push is
required in **both** modes because input belongs to the reader and not to a surface, and the pop
is required to come off before the alternate buffer is given back — a shell prompt drawn under a
keyboard still in `CSI u` is a shell whose every arrow key is a sequence it cannot read — and to
happen exactly once however many times `Exit` is called. The model terminal in `emit_test.go`
learnt these two sequences as no-ops, and only with a private marker: a bare `CSI u` is SCORC,
which restores a saved cursor, and modelling a move as nothing is the one lie that terminal
exists to refuse. The LF half is held in `internal/term` instead, by `TestDecodeNamesEveryKey`,
because nothing in `app` or `ui` can see it: `app` builds its keys from names, so a decoder that
folds `0x0a` back into enter passes every test in the other three packages — which is why
`./internal/term` is now in `mut.py`'s `PKGS`. Then by 13 mutations under
`python3 tools/mut.py multiline`, 5 under `python3 tools/mut.py keyboard`, and 4 more that hold
the byte and the bindings: `LF is`, `ctrl+j` and the two under `chord`.

One thing this leaves exactly as it was: `Home`, `End`, `KillToEnd` and `KillLine` are
buffer-wide, not row-relative, so on a two-row input `Home` goes to the very start rather than
the start of the row. Only the layout and the cursor know about breaks, which is the cleaner
story to hold; making the editing keys row-aware is a separable change and this plan does not
ask for it.

**And ctrl+c throws the line away rather than the program.** You typed a line, reached for the
reflex every shell has taught, and the run was gone. In Claude Code that press clears the input and
nothing else, and leaving takes ctrl+d once or ctrl+c twice — which is the arrangement here now:
`ActionInterrupt` is a declared action bound to `ctrl+c` beside `ctrl+d`'s `ActionQuit`, the first
press empties the input line, and only a second press, with nothing left to throw away, opens the
door.

"A second press" is the one piece of this no keymap can hold, because a keymap answers what a key
means and not what the key before it was. So `App` carries one `armed bool`, and the interesting
half is where it is *cleared*: in `key`, which became a wrapper around `dispatch` for this and
nothing else. Every other key disarms, and `dispatch` has some thirty return sites that would each
have had to remember to — the wrapper reads the flag, clears it, dispatches, and returns
`a.dispatch(act, k) || disarmed`, so a keypress that changed no state still repaints when it took
the arm down. Without that `|| disarmed` the promise stays on screen after it stopped being true,
which is the same defect as a stale notice and harder to see.

There is no deadline on it, deliberately. A timer would make the row lie: the screen says *press
ctrl+c again to leave* and two seconds later still says it while no longer meaning it. The notice is
the state, and it goes when a key takes it away.

Both notices ask `KeyFor` for the key they name rather than spelling it, because `[keys]` can move
either binding and a sentence naming ctrl+d on a terminal where ctrl+d types a newline is worse than
no sentence. `KeyFor` can also answer `""`, since `ctrl+d = ""` unbinds the door outright, and both
call sites guard on it — the end-of-scenario row then stops after the part of itself that is still
true instead of offering `, to leave`.

Held by `TestInterruptClearsTheLineAndOnlyLeavesOnTheSecondPress`,
`TestAnyOtherKeyDisarmsTheInterrupt`, `TestTheArmedInterruptSaysSoOnScreen` and
`TestTheNoticesNameWhateverTheConfigBinds`, then by 11 mutations under
`python3 tools/mut.py interrupt:`. One of the eleven fails no assertion at all: unbind ctrl+d and
the test that presses keys until the app quits presses them forever, so that mutant is caught by a
timeout instead — which is why the harness's runs now carry `-timeout 90s` of their own rather than
Go's ten minutes.

**And on Termux the mouse is claimed, because a swipe there *is* the arrow keys.** You copied the
tree to a phone, built it, played a scenario, and a finger on the conversation walked the input
history instead of scrolling it. Everything written above about the trade is a sentence about a
pointer, and on Android neither half of it holds. There is no drag to protect: a long press is how
Termux selects, and `onLongPress` and `onSingleTapUp` have no tracking check on them, so claiming
the mouse costs neither the selection nor tap-to-type. And there is no 1007 to switch off:
`TerminalView.java` never consults the mode at all. What it does instead is decide in `doScroll` —
`sendMouseEventCode(WHEELUP/WHEELDOWN)` when `isMouseTrackingActive()`, and otherwise synthesize
`KEYCODE_DPAD_UP`/`DOWN` on the alternate buffer. Those are the arrow keys themselves, byte for
byte, so neither `ui.Emitter` nor `term.Decode` could ever have told a swipe from a press of up.
Claiming tracking is the only door there is, and on a phone it is free.

So `play` turns the default around when `term.IsTermux()` says so: `-mouse` on unless it was given,
`-scroll` 1 unless a number was. The 1 is right twice over. `onScroll`'s finger branch computes
`deltaRows = distanceY / mFontLineSpacing` and reports once per row travelled, so one row per report
is a swipe that tracks the hand; and `onGenericMotionEvent` passes ±3, so a real notch — a phone with
a mouse attached — arrives as three reports and still moves three rows. Both are defaults and neither
is a policy: `-mouse=false` counts as given whichever way it reads, and `-scroll` is a number nobody
typed only while it is still zero, so the order that holds everywhere else holds here too — what you
typed, then the file, then this, then the shipped 3.

`IsTermux` is one question with one caller, and it lives in `internal/term` because `internal/ui` may
not import `os`. It reads `TERMUX_VERSION`, falls back to `com.termux` inside `PREFIX` for a launcher
that exports no version, and answers `false` outright when `SSH_CONNECTION` or `SSH_TTY` is set:
Termux's own sshd passes `TERMUX_VERSION` to every login, so without that guard sshing in from a
laptop would take the laptop's drag-to-select away. ssh *out* of the phone and a proot distro are
misses in the other direction, and `-mouse` answers both.

The keymap does not fork, which is the invariant this had to be built inside: one table serves every
run, `wheelup`/`wheeldown` stay bound whether or not anything can send them, and a terminal changes
whether a notch arrives but never what a key means. What it costs on a phone is that `ctrl+↑`/`ctrl+↓`
move one row there, the same as `alt+↑`/`alt+↓`, because one number sets both — `pgup`/`pgdown` still
move a screen.

Held by `TestIsTermux`, eight cases each setting all four variables so the test cannot inherit half
the machine's environment, and by 5 mutations under `python3 tools/mut.py termux:`. Everything under
`IsTermux` is a path already held: `Mouse: true` is the other arm of
`TestTheMouseIsTheReadersUnlessAskedFor` and `WheelLines: 1` is arithmetic `app_test.go` already
does. **The remaining oracle is your own swipe**, on the phone, because it is the only thing that can
say whether the finger now carries the conversation.

**And the code palette is the one you pointed at, in six named colours.** You sent a screenshot
of Claude Code's own picker at Monokai Extended and asked for three substitutions by name: lime
where it had yellow, a light indigo where it had that aqua, a fuchsia where it had purple. So
`lime #a6e22e`, `paleLime #d3e88f`, `indigo #9db2ff`, `fuchsia #ff5f9e` — softer than Monokai's
`#f92672`, which on black reads as an error rather than as a keyword — and each named once in a
`var` block, because a colour spent in three places must not be able to drift between them.

Two of the six are not in the picture and are worth the sentence. A numeric literal is Monokai's
constant violet, and violet is the colour you took off the list; keyword had already spent the
fuchsia that replaced it, and two keys in one colour is one fewer thing the highlighter can say —
so a number is `amber #ffa657`, the only warm thing in the theme and therefore the one token that
separates at a glance. A comment is `slate #767d95` and no longer green, for two reasons that
agree: a lime string and a green comment are neighbours on the wheel and the comment is the token
the eye is meant to skip, and a green comment inside a diff's added band is green on green — the
one place in this interface where a colour can disappear outright.

**What it costs.** These six are RGB, so they are the part of the theme that assumes a dark
terminal: a pale lime string on white is legible but weak. `Emitter.Profile` keeps them safe
rather than correct — quantised to the nearest cube entry on 256 colours, to the nearest ANSI on
16, not sent at all on mono — and a reader on a light background overrides six keys in `[styles]`
and keeps the rest of the theme, which still leans on the terminal's own palette everywhere else.
Held by `TestEveryDeclaredKeyIsDrawn`, `TestEveryDeclaredKeyIsDocumented` and the golden files,
which is what makes a hex change a visible diff rather than a silent one.

**One column of air under the last two rows.** You asked for the `· end of scenario …` notice
and the status row to sit *un poco mas alejadas del borde de la terminal*, and they are the only
text in the interface welded to the first cell: every other row is either inside the input's
border or indented by a marker of its own. `bottomMargin = 1`, spent in `NoticeWidget.Render` and
`StatusWidget.Render`.

It is a margin and not a change to `marked`, because the same helper draws the transcript's
notices and those are already indented by their marker. And the column is taken off the width
*before* the text is laid out rather than added to the row afterwards, which is the whole of the
care this needed: a row that reached the right edge still reaches it, neither row may end in a
blank — a trailing space in the last cell is how a terminal is talked into wrapping a row we
thought we owned — and a terminal too narrow for the text draws no row at all instead of a row
holding one space. Held by `TestBottomRowsKeepTheirMargin`, which checks both rows together
because they are one edge of the frame and a margin on only one of them would read as a
misalignment.

**And a light that crosses the input frame, and the word `working`, on a clock you can retune.**
You asked for a glint, only while the model is not working, on the box and on the verb, and you
asked for it in order to find out whether somebody outside this repository could write their own
animation. So the answer is a value and not a widget. `ui.Shimmer{Style, Phase, Period, Travel,
Width}` is handed a `Line` that has already been laid out and hands one back: it owns no slot on
the screen, nothing in `internal/ui` arms one, and its zero value is off — `Style == ""` means
`Apply` returns the line byte for byte, which is why every golden file and both corpus sweeps
still see the frame they saw before the type existed. Two call sites arm it, both in `app`:
`a.ed.Shine` and the `StatusWidget` the chrome builds.

The composition is the part worth stealing. A `Span` carries two theme keys — `Style` and `Fill`,
emitted as `Style.Over(Fill)` — so lighting a span does not erase what the span already was: the
light takes `Style` and the span's own key moves down to `Fill`, where it still supplies whatever
the light leaves unsaid. `input.shine` sets a foreground and nothing else, so the band crosses the
prompt's wash, a diff row and the placeholder's dim with all three still underneath it. And the
text is only ever *divided*: the pieces of a span cut at a band edge concatenate back to exactly
the string that arrived, so `Line.Text()` and `Line.Width()` are unchanged and the light can run
last, after wrapping, padding and the overflow check, without being able to invalidate any of them.

Two numbers and a ladder are the whole of the tuning you asked for. **Less often but faster** is
`shineTravel` 12 and `shinePeriod` 40 — a pass of about a second and a half every five seconds,
where an 18-tick pass on a 36-tick period was a slow wipe that came round as often. **A better
falloff** is a ladder of declared keys rather than one: `internal/ui` cannot see a `Theme` — only
`Emitter` resolves keys — so a gradient here cannot be RGB interpolation and has to be several
keys at once. The box gets four (`input.shine`, `.soft`, `.dim`, `.faint`), the verb gets two,
and the reason is arithmetic: sixteen columns over four rungs is four columns a step, while
`working` is seven letters and a band `Band` has already halved to three columns cannot show four
of anything. **Twice the wait on the box alone** is `Slower(2)` at one call site, which lengthens
the rest and leaves the pass exactly as it was — the verb is a word you are watching for news,
where a pulse every five seconds is news; the box is a shape your eyes rest between turns on,
where the same rate is a blink in the corner of the room. All six keys are in `[styles]`, so the
colour, the falloff and — through `[anim]` — the cadence are yours without touching Go.

`Travel` is ticks per pass and not columns per tick, which is what makes a pass take the same
wall-clock time on a 40-column window and a 200-column one, and which is also the one place the
type can tear: the jump per tick is `(w+Width)/(Travel+1)`, so once the jump is longer than the
band is wide the columns between one frame and the next are never lit at all and the light stops
sliding and starts landing. `Width >= ceil(w/Travel)` is exactly the width at which it cannot,
and it is the same number that makes the band enter at the left edge and leave past the right
one. Below 192 columns nothing widens. A band over half the row is then clamped back to `w/2`,
because there has to be something unlit for the light to be crossing — and the clamp is written
*after* the widening so that it also holds the widening down.

The braille spinner is `amber` now, the same `#ffa657` `notice.warn` is drawn in rather than a
second orange a shade off it: the two never share a row, and two colours nobody can tell apart
are two keys nobody can retune apart either. It replaced a fuchsia that was also the thinking
marker's colour, which read as though the two were the same event.

Held by fourteen tests in `shimmer_test.go`, by
`TestTheStatusBandCrossesTheVerbAndNothingElse`, by
`TestTheBandsShapeIsTheConfigsAndTheClockIsOurs` and
`TestTheBoxGlintsOnlyWhileItIsTheHumansTurn`, by `TestEveryDeclaredKeyIsDrawn` for the six keys —
`drawnKeys` arms both bands at phase 5 of 40 because that is the one tick where no rung of either
ladder is clipped off the row — and then by 34 mutations under `python3 tools/mut.py shine:`. Four
of those mutations are caught by exactly one test each, which is the only reason those four tests
exist: that the verb's band is measured against the seven columns of `working` and not against the
terminal, that the widening cannot outrun the half-row cap, that `draw` actually hands the box the
band it derives, and that a config's `Phase` is overwritten by `App.phase` — one clock is what
keeps the two bands and the spinner from needing a second timer.

**What it costs, and you should know the number.** The documented "idles at zero CPU" property is
gone while the box is armed: an empty prompt now repaints about eight times a second, because
`Input.Animated()` is true whenever a shine is on. `-shine=false`, or `shine = false` under
`[anim]`, puts it back exactly — that is the same switch as the zero value, so an idle run with
the flag off is byte-identical to one built before any of this existed.

**4 — your own message is held above the transcript while you are scrolled away from it.** A long
conversation is searched by landmark, and the only landmark in one is what you asked; once that has
gone off the top of the window every row on screen is an answer to a question the screen no longer
names. So `HeaderWidget` takes the turn the top row falls inside and repeats at most its first two
rows — `ui.HeaderRows`, exported because a page key has to know it — and only while scrolled. While
the recording plays there is nothing to look for and the pin would be a row of conversation spent
on nothing, which is the rule you gave and it is a test.

It is a quotation and not a summary: same marker, same wash, drawn through the same `banded` the
transcript uses, because all you have to do with that row is recognise it and a second styling for
your own words would be a second thing to learn. Two details in it are corrections rather than
choices. The cut mark goes where the words stop and not out at the right edge, where a lone glyph
in the last column reads as a scrollbar instead of as the end of a cut sentence — so the row is
trimmed back to its ink, marked, and padded out again, and both new spans carry `prompt.band` by
hand, because `banded` has already run and a span added after it with no `Fill` punches a hole in
the wash. And `Rows` is the rows you have actually **lost**, capped at the question's own height,
which is what stops the pin printing a line that is also on screen directly beneath it:
`PromptInside` walks to the last turn beginning at or above the top row and answers
`min(row - start, height)`. A jump lands exactly on a turn's first row, so it reports zero and pins
nothing — there the landmark is on screen already.

The widget asks for `SlotTop` with **no fallback**, so `Place` leaves it out of any frame without a
fixed top, which is inline mode's whole story: there the rows above the input belong to the
terminal's history the moment they are printed, so a row "pinned" into history is the same message
printed again on every frame. The app installs it on identical terms on both surfaces and lets
`Place` decide, so the rule about where a header can exist lives in one place instead of half in
the widget and half in the chrome.

**What it actually cost was the page key.** The header's rows come out of the transcript window, so
the window is not the same height at every reading position, and a page measured against a window
with no header in it can land on a window two rows shorter — with the rows in between belonging to
neither screen. Nobody read them and nothing would have said so. The fix is to pay for the header
whether or not it is up: `page` is `rows - 1 - slack()` and `slack` is `HeaderRows - pinned()`, so
the step is the constant `W - 1 - HeaderRows` on every press and the two screens always overlap by
`HeaderRows + 1 - pin`, which is between one row and three and is never zero. What varies is only
how much of the far screen is already familiar, and re-reading a line is not the kind of mistake
skipping one is. Without the subtraction, a press from an unpinned row onto a two-row pin skips a
row outright.

`slack` is zero on a surface with no fixed top, and that clause is a claim about the reader rather
than about the arithmetic: inline windows are all one height, so a page there keeps exactly the one
row it promises, and charging inline for rows that surface can never draw would make you re-read
three familiar rows per press — a third of a small screen — for a header that cannot appear.

Held at four levels. `TestTheHeaderQuotesWhatScrolledOffAndNoMore` is an independent oracle and not
a copy of the widget: it renders the same text through `BlockFor` and requires the pin to be that
block's first `n` rows for `n` of 1, `HeaderRows` and `HeaderRows+3`, then covers `Rows` of 0 and
-1, widths 0, 1 and 2, and `Place` on both surfaces. `TestPromptInsideNamesTheTurnTheWindowIsIn`
holds the seam underneath — the zero at a turn's own first row, the cap at the far end of it, a nil
state, a blank prompt. `TestThePinnedHeaderNamesTheTurnTheReaderIsInside` and
`TestThePinnedHeaderStaysOffASurfaceThatCannotHoldIt` are the app's two halves, the text and the
surface. And `TestAPageKeyCannotStepOverARowNobodyRead` presses `pgup` from every row of a long
recording on **both** surfaces: the two screens always share at least one row, the alt arm has to
land on a full pin somewhere or the recording is too short to be testing anything, and the inline
arm shares exactly one row and never changes height. A press that ran into the top of the log
stopped short of a page and so says nothing about what a page is worth, which is why the exact
clause is gated on it and the loop fails if no unclamped press ever happened.

Then by 12 mutations under `python3 tools/mut.py "pin: "`. One of them is why that test sweeps two
surfaces at all: deleting `slack`'s `!a.vp.FixedTop` guard is invisible on the alternate screen,
where the flag is always true, and the only thing that catches it is the inline arm — which did not
exist until the mutation was written down and would have been listed as a known hole otherwise.
**The remaining oracle is your own scroll**, because a green suite has already been wrong about
this interface three times.

**6 — the transcript says how far down it goes, in the column beside it.** First of the three
pieces demand 6 asks for, and the one that needed no mouse: `ui.ScrollbarWidget` is a one-column
widget in the `right` slot, its length the window's share of the whole log and its offset where you
are looking. The width it costs is `SideCols`, which is **2** — one column of ink and one of air,
because a glyph pressed against the last word of a sentence reads as punctuation, and the air is
what makes it a margin instead.

The reservation is the whole trick, and it happens before any row is built: `TranscriptWidth(vp)`
takes those two columns off the width every block wraps to, so the composite paints the bar into a
gap that already exists rather than widening a finished row past the screen. A row the column has
nothing to say about is left exactly as short as it was — padding it out to the wrap point and
stopping there ends it in bare air, which is how a terminal is talked into wrapping a row we own.
And at two columns or fewer there is nothing to give up, so the prose keeps the whole width and the
column is dropped instead of being drawn over the only text on the screen.

The geometry is two fractions and a floor. The thumb is `h*rows/(above+rows+below)` rows long but
never shorter than one, so a log ten screens deep still has something to grab, and it sits at
`above*span/(above+below)` inside the free span `span = h - n` — measured in the room the thumb
leaves rather than in the whole track, which is what makes it touch the top only at the top and the
bottom only at the end. Both ends are claims a reader checks without meaning to, and getting them
from the whole track instead is off by a thumb's length at the far end. When nothing is hidden the
bar draws nothing at all, which is why it is installed unconditionally — unlike the pinned header,
which is for somebody who has lost their place. *How much is above me* is what a reader following
the tail wants to know before they have touched a key, so the gate is the geometry's, not the app's.
`Fallback()` is `""`: no fixed side means the widget is left out of the frame, and inline mode gets
no bar rather than a bar somewhere else.

Held at three levels and then swept. `TestASideColumnCostsTheTranscriptItsWidthAndNothingElse` is
the composition — five claims, including that every row of the window is the transcript's row and
is exactly the screen's width, so a bar drawn wherever the prose happened to stop is a ragged line
down the middle of the screen and fails as one. `TestTheBarSaysHowMuchAndWhere` is the arithmetic,
walked monotonically down a long log. `TestTheBarSurvivesItsOwnGlyphs` covers a track configured
too wide for the column and one configured to nothing at all, which is the only way a column row
arrives empty. `TestTheBarIsPlacedOnlyWhereAColumnCanBeHeld` is the slot rule on both surfaces, and
`TestTheScrollbarReachesAScrolledScreen` is the app's only share of it: the widget is installed, on
a screen widened by the same `resize` a SIGWINCH calls, at the tail *and* scrolled away.

Then by 25 mutations under `python3 tools/mut.py "pill: "`. Two of those tests exist because a
mutation had nowhere to be caught: nothing in the tree had ever combined side panels with a
two-column screen, and nothing in `internal/app` had ever asserted that the bar is installed at
all — deleting that line left the suite green. One candidate is deliberately **not** in the family.
Copying a window row before the bar is appended to it cannot be caught by anything here: each memo
`Line` owns its backing array, so appending in place writes past that line's own length where no
reader looks. It stays in the code as an invariant — the day a block renders its rows by slicing
one buffer, row *i*'s spare capacity is row *i+1*'s spans — but a family entry for a line nothing
observes is a `SURVIVED`, not a gap, so the comment above it now says which of the two it is.

**And a notch grows while the wheel is still spinning.** Second of demand 6's three pieces, and the
one thing in it no report can tell you: a wheel event carries a button and a position and nothing
about the hand behind it, so a finger stepping through rows and a flick that freewheels arrive byte
for byte identical. What separates them is the gap *before* the report. A hand stepping sends three
or four reports a second; a flick sends five or ten times that, because the wheel keeps turning
after the finger has left it. `accelPeriod` is **150 ms**, which sits between the two and close to
neither, and that is the whole of its tuning: two reports inside the window are one gesture, so
`a.spin` counts up and a notch is worth `spin` notches — `min(n*a.spin, a.page())`.

The ceiling is the one a notch already had, kept for the reason it had it: a step longer than a page
skips rows nobody read, which is the one thing a scroll may not do however fast the wheel is going.
So a spin *converges on* `pgup` rather than outrunning it, and what the ramp buys is the ground
covered on the way there — seven reports at three rows a notch carry 3+6+9+12+15+18+21 = 84 rows
instead of 21.

**It keys off the device and not the action, and that is not the keymap forking.** `wheelup` and
`ctrl+↑` still mean one action, either can be rebound to anything, and both still move a notch; what
differs is what a hand can afford to send. A wheel report is one flick of one finger and there is no
way to send fifty of them but to mean fifty — whereas `ctrl+↑` auto-repeats, so a ramp that could not
tell them apart would accelerate for a hand merely *resting* on the key, and the row it came to rest
on would be the keyboard controller's decision rather than the reader's. `fast` tests `k.Type`
against the two wheel keys and hands back the flat notch for everything else. The keyboard's ladder
is already four rungs deep — a row, a notch, a screen, an end — and a hand that wants to go further
reaches for the next rung.

Two smaller rules, both corrections of the obvious version. The direction it compares is **the
view's and not the key's**, so a reader with an inverted wheel spins on the direction they can see —
and a reversal resets the ramp instead of carrying its momentum into the correction, because the row
being hunted for is one of the ones that just went past and arriving at it at speed is arriving past
it. And a notch worth one row is left alone outright (`n <= 1`), which is where Termux is: a report
there is one row of a swipe's travel, so the distance is already the hand's own measurement of
itself and multiplying it would take the text off the thumb that was dragging it. It is also exactly
what a reader who set `-scroll 1` asked for, and undoing that on their second report would be
answering some other question.

**The clock is the caller's.** `Config.Now func() time.Time` sits beside `Config.Size func() (int,
int)` for the reason every other edge of this program has one: the world is where the caller is.
Simulated scenario time is emphatically not this — a recording's clock is divided by `Speed` and
stops at barriers, and a hand does neither.

Which is what makes a ramp testable with no sleep in it, and that file's own doctrine is that these
tests synchronize on the output and never on a sleep. `scrollableFile` installs a clock that **steps
past the window on every reading**, so every wheel report in every other test is its own press of
the wheel — which is what all four of those tests always meant, and what they would silently have
stopped meaning: two presses written two lines apart in Go land microseconds apart, so
`TestTheWheelScrollsThroughTheWholePath` asserting `bottom - 3*notch` would have been measuring a
spin. Nothing was patched test by test and nothing was coupled to the ramp's formula. It also asserts
what nothing else can see, because every test overwrites the field: that the clock `New` defaults is
neither nil — a panic on the first notch — nor stopped, which is a spin that never ends.

`TestTheWheelAcceleratesOnlyWhileItIsSpinning` then freezes a clock of its own and steps it by hand,
with the gap written into each of ten rows: the far edge of the window, one millisecond past it, a
shift that rides in the report's own byte and so spins with the rest, a reversal starting over, two
`ctrl+↑`s that must move a flat notch however fast they are pressed, and a wheel report proving the
keyboard neither fed the spin nor ended it. Then the ceiling, walked `4*page` times against `a.fast`
directly — through the view the end of the log would be what stopped the ramp and the claim would be
about how long the recording is — and a `-scroll 1` arm where five reports a millisecond apart move
one row each.

Held by 14 mutations under `python3 tools/mut.py wheel:`. `accelPeriod`'s own *value* is deliberately
not among them: every gap in the test is written in terms of the constant, so a mutant that retunes
it moves both sides of every comparison and survives by construction. That is the right shape for it
— what a spin is worth is `-scroll`, a fact about a hand, and is pinned by name; when two reports are
one gesture is a fact about a wheel and has nothing in it for a reader to have an opinion about. Two
lines came out of the code because the harness could not exercise them: an `a.spin > 0` clause that
was only ever false before the first report of a session, and a four-line ceiling textually identical
to `notch()`'s own, which is an anchor matching twice and therefore a `SKIP` rather than a guard.
**The remaining oracle is your own flick**, because 150 ms is a claim about a wheel and a hand, and a
suite can only hold the arithmetic that follows from it.

**And the pill drags.** Last of demand 6's three pieces, and the one that needed a hit-test: a press
inside the column the bar is drawn in takes hold of the thumb, motion carries the window under it, and
a release lets go. It is not a mode. Under `-mouse` the reports already arrive, so nothing is
negotiated for the drag and nothing in the keymap forks for it; without `-mouse` there is no press to
answer, which is the same door the notch goes through. `Frame.Side` is what makes it honest — the
renderer publishes the rectangle the column ended up in, so the geometry that drew the bar is the
geometry that answers the click, and the two cannot drift apart.

**A press on bare track jumps; a press on the pill moves nothing.** Two halves of one gesture, split
on what the hand meant. A click on the track is a request to go somewhere and so it ends up there. A
press on the pill is a reader taking hold of something already on the screen, and one row of track can
be worth many rows of transcript — asking the track where the pointer is would answer with the nearest
row of that coarse grid, and the log would jump the instant it was touched. So the press remembers
where inside the thumb it landed and every report after it subtracts that grab. `Held` is a style and
nothing else, `scroll.thumb.held`, which is what makes taking hold visible before anything has moved.
And a release nobody was holding is not ours: answering it with a frame would repaint the screen on
every click anywhere on it.

**The rows are the widget's question and the columns are app's.** `ui.Grab` is handed the strip's
height and refuses a row outside it; `ui.Offset` is that same arithmetic read backwards and clamps
rather than refuses, which is why dragging past either end of the track means the end of the log and
not a row past it. app tests the column only, because a column is the one part no widget can know.
That was not true when this landed — the press arm checked the rows as well — and the sweep is what
settled it: no mutation of the second copy could fail in a way the first one would not, so it came
out. One copy of the arithmetic, which is what the widget's own doc already promised.

The five tests in `internal/app` are written in the screen's rows wherever they can be, because the
strip moves. The pinned header is up only while the reader is inside a turn, so a jump to the top of
the log takes it off and hands the bar the row it had been spending — the strip grows, and the thumb
grows with it. The pointer went through none of that, and it is under the pointer that the thumb has
to be, so `TestAPressOnBareTrackPutsThePillUnderThePointer` compares the two in the coordinates the
pointer's row is spoken in, walks every row of bare track, and asserts the ends outright: the first
row is the top of the log, the last is the tail with the conversation handed back. The thumb's length
is a claim only in the frames where the strip kept its own height, and the test counts those rather
than assuming it ever gets one.

`TestADragCarriesTheWindowAndTheBottomHandsItBack` is the grab. It presses the thumb's *last* row and
walks up: a hold that forgot where it was taken would read the pointer as the thumb's first row and
send the window the other way, and both readings keep the pointer inside a thumb of the right length,
so what is asserted is the direction — a walk up never moves the window down, a walk back down never
moves it up, and the two ends are the log's own. Where the thumb was drawn is asked through a helper
that declines to ask in the frames where the strip changed height under the pointer, because the finger
is then a row off a bar that stopped moving, and the next report is what corrects that. Nothing a hit
test could have done would.

`TestAPointerTheBarWasNotGivenChangesNothing` is the other half of owning a column: nine reports the
bar has to leave alone — a middle click, a right click, a middle drag, a release and a motion nobody
pressed for, the transcript's last column, the column past the bar's last, and the rows just above and
just below the strip. Refusing them is not politeness. The app answers a report it handled with a
frame, so a bar that swallowed a text selection would repaint over the terminal's own highlight and
take hold of a thumb the reader never touched. Then one report it must take: the bar's *other* column,
the one with no ink in it, without which every refusal above could be a bar that answers nothing at
all. And `TestASurfaceWithNoColumnBesideItHasNoBarToPress` is the surfaces that reserve nothing — an
inline session, which claims no mouse either way, and an alternate screen with no column to spare —
where a press, a drag and a release at nine points of the screen each have to be nothing, rather than
arithmetic run against a strip of zero height.

Then by 40 mutations under `python3 tools/mut.py drag:`, over the four files one gesture crosses: the
SGR decoder, the widget's two halves, the rectangle the renderer publishes, and app's own arm. Three
had nowhere to be caught, and each was answered differently, which is the sweep doing the job it is
kept for. app's copy of the row range was deleted, being the dead second copy above. Two reports no
test had ever fed the decoder became three rows in the table of reports that must decode to nothing: an
extra mouse button carrying the notch bit as well, where bit 7 has to be what decides and not bit 6 —
128 alone is dropped either way, so only a report with both bits set can tell the two spellings apart —
and a coordinate of zero in either axis, which one-based counting leaves no cell for and which without
its guard becomes a negative row or column handed to a hit test. And the `d/2` that `Offset`'s interior
falls back to when there is no span is documented as a freedom rather than mutated, for the reason the
`span/2` rounding already is: a track with two spare rows draws its thumb on the middle one for every
offset between the ends, so which of those offsets a press asks for decides what the reader is shown
and not where the thumb goes. A family entry for a line nothing can observe is a `SURVIVED` for good
and not a gap — the same call the memo copy and `accelPeriod`'s value got.

**The remaining oracle is your own drag**, and it is the one the column was drawn for: whether the
thumb stays under your finger through the two places a reader aims hardest, the top of the log and the
tail, where the row each end was holding back is released and the track the pointer is crossing stops
being the track the thumb will be redrawn in.

Every item above is covered by mutation tests. The harness now includes the command package as well
as `internal/ui`, `internal/app`, `internal/config`, `internal/term`, `internal/event`,
`internal/state` and `internal/scenario`; run `python3 tools/mut.py` for the current total.

**Task 5 — Config is an app-owned full frame.** `/config` opens the canonical surface and the
hidden `/settings` alias reaches the same one. Wide terminals keep a category master beside the
selected detail and use Tab/Shift+Tab between categories; narrower terminals stage the category
list and detail. Both tiers preserve the selected category or setting through short heights, down
to an honest one-row status. The Config scalar editor reuses editing operations but none of the
conversation prompt's border, marker, placeholder, wrapping, or history, and only it exposes the
cursor. Enter, paste, and printable input cannot leak into the conversation draft; closing restores
that draft and its scroll position unchanged.

The controller remains in `internal/config`, behind DTOs and the narrow interface in `internal/app`,
so app never imports config. It keeps persisted baseline, editable draft, runtime-effective value,
and source as separate facts. Explicit title, scroll, mouse, and shine flags mask live preview while
the underlying draft remains saveable. Title, scroll distance, shine, period, travel, and width
preview immediately when unmasked; mouse is labelled and treated as next-launch only. Effort and
Recap are session state and never enter the document. Keys, Glyphs, and Styles are read-only
inspectors that show default, configured, and effective layers.

Save writes all seven persistent scalar settings through the lossless `config.Document`, retains
validation and conflict errors in the frame, and advances the Cancel baseline only after success.
Reload refuses a dirty draft until the reader explicitly confirms discard. An explicit
`-config ""` constructs a disabled controller: Save and Reload stay unavailable and no default file
is created. Focused app, controller, and command tests cover responsive geometry, aliases, input
isolation, preview masks, all animation fields, next-launch mouse behavior, invalid edits, baseline
advancement, conflict retention, CLI provenance, the default path, and disabled persistence. The
focused `config controller:`, `config command:`, and `config view:` mutation families hold those
seams, and `--anchors` also checks the older families after the full-view refactor.

**B2 — a run with a team reads as a team.** arxi has a real `MemberConfig`; placing its
member data in `run.started.members` is an additive simulator replay extension, not the current
arxi event contract. The fold keeps that order, appends actors first discovered in later
`agent.*` events, and holds each member's busy, blocked,
failed, steered and notified state beside the legacy run-level fields. A status with two or more
members replaces the old actor segment with one indivisible glyph-and-name cluster; a solo run
keeps the old bytes. Busy members share the run spinner's phase, while blocked, failed and idle
members carry their own configurable glyph and style keys.

The first B2 surface was a read-only sheet. The final architecture deliberately replaces it.
Team, `/team`, and Ctrl+T are local simulator UI inspired by Claude Code, not UI observed in
arxi. Ctrl+T and `/team` enter the same app-owned, full-frame Team monitor, and `Esc` restores the
conversation without moving its scroll position or editor draft. Team folds current state on
every draw, owns independent scroll geometry, ignores printable input and Enter, and does not
pass through the generic overlay compositor or the renderer's unavailable `full` widget slot.
Its width tiers preserve full metadata at 72 columns, shorten it below 32, and wrap the middle;
its height tiers are header/body/footer at three or more rows, body/footer at two, and a compact
state summary plus back hint at one. Approval, lock, budget and workspace blocks name a concrete
`arxi` remedy; peer, timer and tool blocks say what is awaited; malformed references are exposed
rather than guessed from prose.

The information area now spends at most three semantic rows according to the terminal's height.
At eight rows or fewer it keeps only live operational state; from 9 through 13 it adds the model
row; at 14 or more it also adds provenance. The model row carries the replayed model, exact
`context_used/context_capacity`, percentage where it fits, and thinking effort. Context is an
explicit paired `llm.response` replay metric — never `tokens_in` — and omission on a later response
clears the old value. The provenance row carries the simulator extensions `cwd` and `git_branch`
from `run.started`, keeps
the branch where possible, and middle-ellipsizes long paths. Older recordings omit all four
fields and retain their prior behavior.

The thinking-effort control is vertical: Up/Down is primary, Left/Right remains compatible, and
all six levels stay in display order. Short surfaces window around the selection after dropping
the heading, description, and hint; below 24 columns only option rows remain. The selected max
and ultracode labels keep their existing animation.

Scenario `11-team-blocked.ndjson` is the end-to-end record: its replay-only Windows-style cwd and
branch come from `run.started`, its extended response events carry exact context pairs, and its roster passes through an
approval, resume, and later reviewer failure. Automated coverage holds payload validation,
folding, complete-frame Team geometry and input isolation, responsive information rows, and the
vertical selector. The new focused mutation families and full repository sweep are the remaining
automated gate; desktop and Termux playback remain the required human oracle for interaction.

## Next

The automated implementation gate for Team, Tasks, and Config is complete. The remaining oracle is
manual terminal interaction: play scenario 11 on a desktop terminal and Termux to check Team entry
and return, independent scrolling and live updates, resize transitions down to one row, the vertical
effort selector below 24 columns, desktop drag/selection, and Termux swipes. Config additionally
needs a hand check of wide Tab category movement, narrow staged navigation, local scalar editing,
live preview and Cancel/Save/Reload feedback against a disposable config file.

Demand 6's three pieces remain completed and are retained here as their design record:

- ~~**Drawing the pill.**~~ **Done** — a one-column widget in the `right` slot, dropped rather than
  moved on a surface with no side to hold it.
- ~~**Wheel acceleration.**~~ **Done** under `-mouse`: a notch multiplied by how long the wheel has
  been spinning, measured by the gap between reports rather than counted, keyed off the wheel keys
  themselves so a held `ctrl+↑` cannot accelerate by leaning, and converging on a page rather than
  outrunning it. Without `-mouse` there is no notch to accelerate, and the keyboard's own ladder — a
  row, a notch, a screen, an end — is what a hand reaches for instead.
- ~~**Dragging the pill.**~~ **Done**, and under `-mouse` for the reason the tracking is: the drag you
  asked to have back is the same drag the pill's would have taken away, so the flag is what separates
  them, and where the reports already arrive this is a hit-test and a highlight rather than a mode to
  negotiate. Inline mode claims no mouse on either setting, which is the same surface the pill is
  already dropped on. The column the pill is drawn in is the hit-test's own target, so the geometry
  that draws it is the geometry that answers a click.

What is left is not code. Three of the things above are claims about a hand, and a suite can only hold
the arithmetic that follows from them — your own drag, your own flick, and your own swipe on Termux.
The one outstanding dependency is **shift+enter**, which keeps arriving as enter until Windows Terminal
1.25 answers the `CSI > 1 u` negotiation; ctrl+enter already starts a row on 1.24.

## Decided: what the arrows do (demand 3)

This was the one request that looked like it contradicted another one you made, and the
contradiction turned out not to exist. The argument is kept on the page because two wrong
versions of it shipped before this one, and each was wrong in a way worth not repeating.

You asked to select text with the mouse and to scroll with the wheel, without a toggle. The
answer here was alternate scroll (`CSI ? 1007 h`): leave mouse tracking **off**, the terminal
keeps clicks and drags and its own selection works, and in exchange it converts each wheel
notch into **plain Up and Down arrow keypresses** — the wheel and the arrows being literally
the same bytes, with no timing trick that separates them. So scroll went on the plain arrows
and the history went one modifier over.

You rejected that and named three counter-examples: Claude Code, every TUI editor, and
ishakat. The mechanism you were pointing at is real and is the missing half of the paragraph
above: **1007 only translates while no program has claimed the mouse.** Claim tracking and the
terminal sends a real wheel report instead — `CSI < 64 ; col ; row M` for a notch up, with
shift riding in the same button byte. That is what shipped next: `?1002h` (button-event
tracking) and `?1006h` (SGR coordinates) on the alternate screen, which is the pair ishakat
uses, and the arrows free.

Then you used it, and asked for the plain drag back — which is the same sentence read from the
other end, because a program that owns the mouse owns the clicks. The way out is that the
translation needs 1007 **and** an unclaimed mouse, so the arrows are ours in either arrangement,
and the default now takes the other door: `ui.Emitter.Enter` claims no mouse at all and sends
`?1007l` instead, `Exit` sends `?1007h` so the next program gets its wheel back, and `-mouse`
claims `?1002h`+`?1006h` for a reader who would rather have the wheel than the drag. Your drag
selects with no modifier again. What it costs is the wheel doing nothing inside the default run,
which is why the ladder in the table below is not a convenience but the whole of scrolling.

| Key | Moves |
|-----|-------|
| `↑` / `↓`, `ctrl+p` / `ctrl+n` | the input history, one line of it |
| wheel, `shift`+wheel | the conversation, by a notch — three lines, `-scroll` — under `-mouse`, and further per report while it is still spinning |
| `ctrl+↑` / `ctrl+↓` | the conversation, by the same notch |
| `alt+↑` / `alt+↓` | the conversation, exactly one line |
| `shift+↑` / `shift+↓` | the conversation, to your own previous or next turn |
| `pgup` / `pgdown`, `ctrl+home` / `ctrl+end` | a screen, and the two ends |

On Termux read the wheel row as a swipe and take the `-mouse` out of it: the trade is turned around
there because a released mouse hands a finger's travel to the program as `↑`/`↓` themselves, so a
notch is a swipe, a report is one row, and the two `ctrl` rows collapse onto the two `alt` rows
because `-scroll` is 1. The ramp is off there for the same reason: at one row a report the swipe is
already the hand's own measurement of itself, and multiplying it takes the text off the thumb.
*And on Termux the mouse is claimed* above is the whole of the argument.

The jump pair is `jump-prev-message`/`jump-next-message`, and it is on `shift` rather than on
`ctrl+shift` because you reported that `ctrl+shift+↑` never reached the program at all — that
combination is claimed by the terminal or the desktop on enough setups to be worth nothing,
whereas `shift+↑` is `CSI 1;2A` and arrives. It is a landmark search and not a scroll: nobody
scrolls back looking for a tool call, they scroll back looking for what they asked, so the row
it lands on is the first row of a turn of yours with the blank line above it. That row comes
from `Renderer.PromptRows`, which measures the transcript at the current width every time it
is asked — a remembered row would survive a resize as a wrong answer.

**What it costs, and there is a cost.** In the default run the wheel is the terminal's own and
does nothing to the conversation — and on the alternate screen there is no scrollback for it to
move either, so a spin moves nothing at all and the six rows above are the whole answer. Under
`-mouse` the trade inverts: the wheel scrolls, and a plain click and drag stops selecting, because
a program that owns the mouse owns the clicks — there, **hold shift to select and copy**, which
every terminal keeps open. Both ways round, quitting prints the whole conversation onto the main
screen, where it is selectable with no modifier at all. The keymap does not fork on the flag:
`wheelup`/`wheeldown` stay bound whether or not anything can send them, because a binding for a
report nobody makes costs one map entry while a table with a fork in it costs every reader a
question. The config is where a different layout comes from (`up = "scroll-up-fast"` in `[keys]`
puts the old arrangement back).

## Not free: a plain drag to select

You asked for selection on a bare click and drag, with no shift, and called it a small change.
It is a one-line change — and the line it changes is the one that makes the wheel a wheel.
There is no mode in any terminal that forwards wheel reports while leaving clicks and drags to
the terminal's own selection: `?1002h` and `?1000h` alike hand us the whole mouse or none of
it, and `?1007h` hands us the wheel only by rewriting it into arrow keypresses. So the three
things you have asked for — a plain drag that selects, a wheel we can give a three-line notch
to, and plain arrows that recall the input history — are any two of the three, and which two is
a choice rather than a bug to be found. There is also a fourth door, which is the one the
default takes now: give the wheel up rather than trade it away. That is not the same move as B
below, because `?1007l` stops the terminal rewriting a notch into arrows at all, and *that* is
what leaves the plain arrows safely ours while the drag goes back to the terminal.

- **A, what shipped first, and is now `-mouse`.** Alt screen, `?1002h`+`?1006h`. The wheel is a
  real wheel with a notch we set, `ctrl+↑`/`ctrl+↓` are that notch, the plain arrows are the
  history — and a selection needs shift held while dragging. Quitting also prints the whole
  conversation onto the main screen, where it selects with no modifier at all.
- **B, a plain drag.** Alt screen, `?1007h`, no tracking claimed. Your drag selects natively.
  In exchange the wheel arrives as plain `↑`/`↓` bytes that are *the same bytes* a typed arrow
  sends, so scroll has to go back onto the plain arrows and the history back to `ctrl+p`/
  `ctrl+n`; and a notch's size becomes the terminal's opinion again — three lines per *step*,
  with the emulator deciding how many steps a spin is worth, which is the "a spin crossed a
  screen" complaint you had already made and which is fixed. It also contradicts the same
  message this request came in: `ctrl+↑` cannot be worth exactly three lines in a build where
  the wheel is pretending to be `↑`.
- **C, drawing the selection ourselves.** Keep tracking, hit-test the drag, highlight the cells,
  copy with OSC 52. This is the only way to have all three, and it is the largest single piece
  of work on this page: a selection that survives a re-wrap, a highlight style, the clipboard,
  and at the end of it the terminal's own selection has been taken away and reimplemented worse.
- **D, what ships today.** Alt screen, `?1007l`, no tracking claimed. Your drag selects natively
  and the plain arrows stay the history, because a wheel that is not rewritten sends nothing the
  keyboard also sends. The wheel does nothing inside the run — on the alternate screen there is
  no scrollback for the terminal to give it either — so every scroll is a keypress:
  `ctrl+↑`/`ctrl+↓` for a notch whose size is ours, `alt+↑`/`alt+↓` for one row, `pgup`/`pgdown`
  for a screen, `ctrl+home`/`ctrl+end` for the ends. `Exit` sends `?1007h` so the next program
  gets its wheel back.
- **A again, chosen for you, on one terminal.** Termux implements no 1007 and synthesizes the arrow
  keys for an unclaimed swipe, so D there is not "the wheel does nothing" but "a finger walks the
  input history" — the one outcome none of the four doors is allowed to have. `play` asks
  `term.IsTermux()` and takes A, with `-scroll` 1 so a report is the row it stands for. It is the
  default turned around and not a fifth door: the modes are A's, the keymap is the same table, and
  `-mouse=false` still says otherwise.

**D is what ships, 2026-09-05, and A is one flag away.** You accepted A on 2026-09-04 —
*"acato recomendacion"* — then used it and asked for the bare drag back, which is the same
sentence read from the other end: a program that owns the mouse owns the clicks. What made a
fourth door exist is that 1007's translation needs 1007 **and** an unclaimed mouse, so the plain
arrows are the app's under either mode, and the wheel was the only thing left to spend. So
`ui.Emitter.Enter` claims no mouse and sends `?1007l`, and `-mouse` restores A verbatim
(`?1002h`+`?1006h`, 1007 untouched) for a reader who would rather have the wheel than the drag.
No keymap change is needed to move between them, which is the point of binding `wheelup` in a
build that cannot receive it. C is still unbuilt and still the only way to have all three.

**Except on Termux, where D is A**, and that is the exception the four doors did not anticipate: they
were all reasoned from a terminal that has a pointer and implements 1007, and a phone has neither. The
choice is made once, in `play`, by `term.IsTermux()`, and it is a default rather than a policy.

## Not ours: ctrl+wheel to resize (demand 5)

ctrl+wheel is the emulator's own font-zoom gesture. It is consumed before any program sees
it, and a program inside the terminal cannot change the window's size from the inside — the
size is reported *to* us. The default run no longer competes with it: no mouse is claimed and
alternate scroll is switched off, so nothing of ours stands between the notch and the terminal
and the gesture is the terminal's alone. Under `-mouse` one genuinely open question remains, and
only your terminal can answer it: whether a claimed mouse takes the zoom away by forwarding the
notch to us with the ctrl bit set instead of acting on it. If it does, that is one more line in
that flag's price. `-width` remains the deterministic knob for a fixed pane.

## Not fixable here: the frozen selection (demand 7) — accepted

The highlight staying on screen while the text scrolls under it is a consequence of the
alternate screen, not a bug in our drawing. A terminal anchors a selection to screen cells.
In the alternate buffer a scroll is a repaint we perform — the terminal is not told that
anything moved, so it keeps the highlight on the same cells while different text arrives in
them. No escape sequence clears a terminal's selection; there is nothing to send.

You then drew a distinction worth recording, because it is the right distinction and it still
cannot be honoured: cell-anchored is the correct behaviour when the selection is over a
*widget* — the status row, the input box, a scrollbar — and the wrong behaviour when it is
over the *conversation*, where a selection should stay on the characters it was dragged
across even as they move. That is exactly the line a text editor draws. We cannot draw it,
for a reason that is one layer below us: the selection is not ours to move. It lives in the
terminal, which knows a rectangle of cells and nothing about which of them are chat and which
are chrome. There is no sequence that reads a selection, none that moves one, and none that
tells the terminal a region scrolled; the only report we could ever get is a paste of what
was already copied. Honouring the distinction would mean drawing the selection ourselves, and
the tracking half of that is no longer paid for either — the default claims no mouse, so it is
`-mouse`'s modes that would have to come back and the bare drag you asked for that would go —
so what is left is a hit-test, a selection that survives a reflow, a highlight style and the
clipboard, and at the end of it we would have taken the terminal's own selection away in order
to reimplement it worse.

Inline mode would fix it, because there a scroll is the terminal's own, and inline mode was
already confirmed broken by a real drag — that is why the alternate screen is the surface.
The practical answer is the same one every full-screen terminal program gives: select, copy,
then scroll.

**And the other half of the same wall: a selection cannot grow past the edge of the screen.** You
reported it as its own problem — start a selection, hold shift and press an arrow to extend it, reach
the top or the bottom row, and the view does not follow, so nothing outside the pane can be selected
at all. It is demand 7 read from the other end, and it has the same cause and the same two readings,
which is worth writing down because they lead to the same place:

- If your terminal spends shift+arrow on extending its own selection, the keypress never leaves it,
  and the terminal cannot scroll what it is not drawing — the alternate buffer has no scrollback, and
  the rows above the pane exist only inside `Renderer` where the terminal cannot see them.
- If it does not, the key reaches us as `jump-prev-message`/`jump-next-message` and the view *does*
  move. The highlight then does not, because it is anchored to cells and belongs to the terminal:
  which is exactly the frozen selection above, arriving as a selection that shrinks and jumbles
  instead of one that grows.

Either way the missing piece is the one C would build and nothing else can: a selection the program
owns, so that extending it is a scroll we perform and a highlight we draw. Until then the two answers
that do work are the two that already ship — scroll to the text first and then select it, and quit,
which prints the whole conversation onto the main screen where the terminal's own scrollback and its
own selection apply with no modifier at all. That second one is not a consolation: it is the only
place a selection can cross the whole run, and it is why the transcript is printed on the way out
rather than left on the alternate buffer to be discarded.

What this section does **not** claim is that C is unaffordable, only that it is unbuilt and that
building it takes the bare drag back — under C the program owns the mouse, so `?1002h` returns and
your plain drag stops being the terminal's. It is four separable pieces: a hit-test from a drag
report to a `(row, col)` in the folded transcript, an anchor that survives a re-wrap at a new width,
a `selection` style key, and a copy — OSC 52, whose Termux support is unverified and would have to be
measured on the phone before a binding for it shipped, because a key that silently does nothing is
worse here than a key that does not exist. If you want it, that is the order to build it in and the
price to accept, and it is a decision rather than a discovery.

## Order of work

1. ~~The config file — `[keys]`, `[glyphs]`, `[styles]`, `-config`, and `check` validating it.~~
   **Done**, with `-config ""` for the shipped look and a line number on every complaint.
2. ~~Multi-line input on shift+enter and ctrl+enter, with `Input` holding newlines.~~ **Done**,
   including the `CSI > 1 u` negotiation that makes shift+enter's byte exist at all; the editing
   keys stay buffer-wide. ctrl+enter starts a row on this terminal today, through the `0x0a` the
   decoder used to fold into enter; shift+enter is the one still arriving as enter on Windows
   Terminal 1.24, until 1.25 answers the negotiation.
3. ~~`jump-prev-message` / `jump-next-message` on shift+up/down.~~ **Done**, with the input
   history back on the plain arrows and the notch that came with them now on `ctrl+↑`/`ctrl+↓`.
4. ~~ctrl+c clears the line and only the second press leaves; ctrl+d is the door once.~~ **Done**,
   held by one `armed` flag that `key` clears for every other keypress, and by the on-screen row
   that is the promise instead of a timer.
5. ~~A swipe on Termux scrolls the conversation instead of walking the input history.~~ **Done**,
   by claiming the mouse there and setting `-scroll` to 1; your own swipe is the oracle left.
6. ~~A light sweeping the input frame while it is your turn, and the word `working` while it is
   not.~~ **Done**, as a value a caller arms rather than a widget: `ui.Shimmer`, a ladder of
   theme keys for the falloff, `App.phase` as the one clock, and `-shine=false` to stop it.
7. ~~The scroll-mode header widget in the `top` slot.~~ **Done**, and what it cost was the page
   key: the header spends rows out of the transcript window, so a page now steps the constant
   `W - 1 - HeaderRows` and the two screens overlap by between one row and three instead of
   skipping the rows a pin swallowed at the far end.
8. ~~The scrollbar pill and wheel acceleration.~~ **Done**, all three pieces. The pill is drawn in a
   two-column margin the wrap point gives up before a row is built, dropped whole on any surface with
   no side to hold it. A notch accelerates while the wheel is still spinning, on the gap between
   reports rather than on a count of them, off the wheel keys themselves so no held key can ramp by
   leaning, and converging on a page rather than outrunning it. And the pill drags under `-mouse`: one
   hit-test against the rectangle the renderer publishes, a grab the press remembers so the thumb
   stays where it was taken hold of, and a jump only from the bare track, where a click means
   somewhere to go rather than something to carry.

## How each step is held to be true

- `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test ./... -count=1`.
- `python3 tools/mut.py [substring]` breaks the code on purpose, one edit at a time, and
  reports any mutation the suite let through. A `SURVIVED` line is a missing assertion, not a
  bug in the code. It is at **284 of 284** and every new test above is expected to add to it.
  A `SKIP` counts against that number as well, and it is the half worth explaining: an anchor is
  a literal find-and-replace that has to match a pristine file exactly once, so refactoring the
  line it names unhooks the guard from it without failing anything. Six went that way under the
  shine's own edits — `w` becoming `room` where the status row spends its bottom margin, the
  working predicate moving out of `spins` into the exported `ui.Working` that the shine reads
  too, and the number floor collecting into `parser.count`, where `[scroll] lines` and the three
  `[anim]` numbers now share one line. Every one of them was still covered by a test; the sweep
  had simply stopped asking. The pin then produced a seventh in the other direction: `PromptInside`
  introduced a second `if st == nil || width <= 0` and an anchor that had matched one line began
  matching two, which is ambiguity rather than absence and is refused for the same reason — a
  find-and-replace that could hit either line is a guard on neither. Both shapes are fixed by
  naming more of the line. That is why a stale anchor is reported as a failure and not as a note,
  and why the count in this file is worth keeping honest. `python3 tools/mut.py --anchors`
  answers that half alone, in a second and with no test run, because whether a find-and-replace
  still matches is decidable by reading the tree — it is the thing to run after a rename, on a
  tree with no sweep in flight, since a sweep in flight has a mutant on disk and every anchor is
  read against that.
  Each run carries `-timeout 90s` of its own, because one way for a mutant to fail is to never
  finish — unbind `ctrl+d` and the test that presses keys until the app quits presses them
  forever — and Go's own ten-minute default would spend that on a single line. Such a mutant is
  still caught; only the report reads `panic/build` instead of naming a test, since a timeout
  kills the binary before it can print a `FAIL` line.
  The run now begins by testing the untouched tree and refuses to report anything if that
  fails, because a red baseline calls every mutation caught and proves nothing — which is
  exactly what an interrupted run produced once, by leaving a mutant behind in `app.go` for
  the next run to inherit.
- Then your terminal. A green suite is not evidence: three changes that passed everything
  were still wrong in a real drag, which is why nothing here counts as finished until you have
  scrolled it yourself.




