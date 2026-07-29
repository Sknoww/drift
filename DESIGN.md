# Drift — Design System

> Living design reference for Drift. The **single source of truth** for visual and
> interaction decisions. Update it as decisions are made — every new pattern,
> spacing rule, or component convention goes here before it goes in code.

The visual tone is **dense, technical, keyboard-driven**. The quality bar is
**lazygit**: polished bordered panels, color-coded state, everything reachable from
the home row. "Looks like raw text output" is a bug.

**Status legend:** ✅ Decided · 🚧 In progress · ❓ Open question · 📝 Placeholder (TBD)

---

## 1. Foundations 🚧

- **Color roles** ✅ — meaning first. `behind > 0` is the one alarm that matters (the
  target main moved under me) and reads as a warning; `ahead` is neutral. Dirty is a
  dot, not a color shift. The checked-out branch gets a marker. The selected row is a
  highlighted band. Pinned on the first build (ANSI-256, in `internal/ui/styles.go`,
  a considered-not-sacred starting point): `warning 214` (behind), `neutral 245`
  (ahead / in-sync), `dirty dot 220`, `checked-out marker 39`, `title 39`,
  `border/faint 240`, `selected band bg 236`, `error 203`, `unmergeable 170` (a
  branch with a collision that must be reconciled by hand — a distinct alarm from
  `behind`, since it means "moved *and* unmergeable", not just "moved").
  **Diff panel** ✅: `add 71`, `remove 167`, `hunk header 109`, git's bookkeeping
  lines faint `240`, context left unstyled. Muted rather than saturated — a whole
  screen of incoming change is the normal case there, so `+`/`-` must read as
  structure, not as alarm; `behind` stays the only thing on screen shouting.
- **Panels** ✅ — Lip Gloss rounded border (`240`), 1-col horizontal padding; the
  title (`drift`) sits on its own line above the panel, with the checked-out branch or
  a refresh spinner to its right. The panel spans the **full terminal width** (computed
  in `panelStyle` from the window size less the app/panel frame), so every screen — the
  dashboard, the diff panel, the wizard — shares one full-width layout rather than a
  snug content-sized box. Before the first `WindowSizeMsg` it falls back to content
  sizing. **The one trap:** Lip Gloss counts a style's horizontal padding inside
  `Width()` but *not* its border, so the panel is set to `contentWidth + padding`.
  Setting it to `contentWidth` alone leaves a text area two cells narrower than the
  rows built to fill it, and every selected row wraps — dropping its tail onto the next
  line. Tests can't see it (the ASCII color profile lets trailing band spaces be
  trimmed), so the geometry is asserted directly instead.
- **Density** — a ticket is one row; expanding it lists its branches one per row
  beneath, one per target it aims at. Seeing a ticket's whole fan-out without
  scrolling is the point — but the target count is config-driven and unbounded, so
  "fits on screen" is a goal, not an invariant. An expanded ticket whose fan-out
  overflows must scroll gracefully rather than break the layout — which is what
  **Windowing** below now guarantees, for every list screen rather than just this one.
- **Windowing** ✅ — **every list screen draws only the rows that fit, around the
  cursor** (`listBody` / `rowWindow` in `internal/ui/window.go`; the diff panel is the
  exception and always was, since it scrolls free text through a `viewport` rather than
  selectable rows). This is a correctness rule, not an optimisation: Bubble Tea rewrites
  the *whole* frame on every keystroke, so an unwindowed list of a few thousand rows is
  a megabyte-plus of ANSI per press, and the terminal drowning in it presents as a **hard
  freeze** rather than as lag. Measured on the first-run wizard: 5000 remote refs went
  from a ~1.26 MB frame to a flat 2.8 KB.
  - **The window is derived from the cursor, never tracked as scroll state.** There is
    nothing to keep in sync and nothing to reset when a list is rebuilt, and the one
    invariant that matters — *the cursor is always drawn* — holds by construction rather
    than by remembering to clamp an offset.
  - A clipped edge says so (`↑ N more` / `↓ N more`). A list that silently hides rows is
    worse than a long one, the same rule filtering will inherit.
  - **Windowing bounds the row count; clipping bounds what each row costs.** Without the
    second the first is fiction — 400 long branch names still wrapped into a 37-line
    frame on a 24-line terminal. `clipRow` caps each row at the panel's content width,
    **ANSI-aware**: a row is assembled from styled cells, and slicing one by bytes would
    sever an escape sequence and bleed color down the frame. It runs *before* the
    selection band, so the resets it introduces are re-armed by `reopenBand` (§3).
  - Windowing is a **render** concern only. A row selected and then scrolled out of view
    is still selected and still saved — the same "never guess" rule as pairing.
- **Every column bounds its own cells** ✅ — one helper (`fit` in `internal/ui/columns.go`)
  renders a cell in exactly the width its column was given: truncated with an ellipsis
  when it is wider, padded when it is narrower, **measured in display cells and
  ANSI-aware**. Before it the package had two padding helpers that disagreed on how to
  measure (one `lipgloss.Width`, one `len()`) and *neither truncated*, so a column was
  only ever capped against over-padding — a 60-character branch name in a column "capped"
  at 32 rendered all 60 and shoved the status cluster off the right-hand end.
  - **The order of allocation is the rule.** A row's fixed cost is paid first, then the
    cell that carries the row's *point* — the status cluster on the dashboard, `⚠ pick a
    target` on the pairing checklist, the primitive on the hold picker — and the
    name/path column absorbs what is left. A long name shortens itself; the signal it
    was meant to sit beside never gives way. Inverting that is what the pass fixed.
  - **A cap is a ceiling, not a width.** A column shorter than its cap still shrinks to
    its content, and one squeezed by a narrow terminal shrinks below the cap to a floor.
    Caps exist only on columns whose content is user- or repo-supplied; a column of
    literals (a destination label, a hold's mechanism) is unbounded because its content
    is.
  - `clipRow` (Windowing, above) stays underneath as the **backstop**. It clips blind —
    whatever overflows off the right-hand end — so reaching it means dropping a trailing
    cell rather than ellipsising in place. A column that sizes itself never does.
  - **A header line costs the lines it wraps to.** `listBody` used to cost its fixed
    header as one line each, which is right only while each one fits; the wizard's intro
    wraps to three at the width floor, and the window then drew one row too many and ran
    the frame off the terminal. Prose can break the row budget exactly as rows can
    (`headerLines`).
- **A minimum usable width, declared rather than clamped** ✅ — below **60 columns**
  (`minTerminalWidth`) every screen draws one notice saying so and nothing else. The old
  behaviour was to clamp the content width to 1 and render into it, which produces
  garbage rather than a compressed view. Sized from the row it has to fit: at 60 the
  panel's content width is 54, which leaves the branch name ~27 cells beside a full
  cluster, and it sits well clear of the near-universal 80-column default. Before the
  first `WindowSizeMsg` the size is genuinely unknown, so the screen draws — refusing on
  a guess would blank a terminal with room to spare.
- **Type-to-filter** ✅ — `/` opens an incremental, case-insensitive substring filter over
  a list (`filterState` in `internal/ui/filter.go`; live on the first-run wizard and the
  pairing checklist). Windowing made a long list *renderable*; it did nothing to make one
  *navigable*, and `j`/`k` through 418 remote refs is not navigation. Like the window,
  the matching set is **derived from the query on every render, never stored** — the
  cursor means "the n-th visible row", so there is no second copy of the list to fall out
  of sync with a query that has since changed.
  - **The counts are part of the component, not decoration.** `12 of 418` is the answer
    to "did my query find it, or is it just not there" — without it a narrowed list is
    indistinguishable from an empty repo. A query matching nothing says so in words.
  - **Filtering never drops a selection**, and the screen says how many it is hiding
    (`⚠ N selected rows hidden by the filter`, in the error style — it means the same
    thing that style always means here: what is about to happen is not what is on
    screen). A row filtered out is still selected and still saved, exactly as a row
    scrolled out of view is.
  - **A blocked save reveals the row it names.** Save validates every row, not just the
    visible ones, so a block can land on one the query is hiding — the screen clears the
    filter and puts the cursor on it rather than naming a row the user cannot see.
    Revealing a choice the user already made is not guessing on their behalf.
  - **Column widths are measured over the visible rows only.** The column exists to align
    what is on screen; padding every drawn row to fit a name the filter is hiding is the
    row-wrapping that doubled the frame before windowing.
  - Interaction and the two-meanings-of-`esc` rule are in §3.
- **Order a long list, never narrow it by heuristic** ✅ — the first-run wizard offers the
  repo's refs most-recently-committed first (`--sort=-committerdate`). The question it
  asks is "which of these 418 refs are your long-lived mains?", and a main is the ref
  everything gets merged into, so recency answers it. The rule is the boundary, not the
  sort: **ordering moves the likely answer up and removes nothing**, so a repo whose main
  is a dormant maintenance branch still lists it. A default *narrowing* was declined on
  the never-guess rule — it would be a filter the user never typed, the counts line could
  not distinguish "this repo has 20 refs" from "Drift chose 20 for you", and it inverts
  the model by making `/` the key that *broadens*. Alphabetical order only ever bought
  predictability when you already knew the name you wanted, and type-to-filter does that
  strictly better; that leaves the list order one job — discovery.
  - **An unexplained order reads as a bug**, so the ordering is on screen: each row leads
    with a fixed-width relative age (`20m`, `2d`, `1mo`, `2y`). Read top to bottom the
    column *is* the sort order, and it answers the wizard's question directly — a ref
    touched two days ago reads as a main, one untouched for fourteen months does not.
  - **Leading and fixed-width, both for the same reason.** It is the only column on that
    screen with a bounded width, so at the left edge it always aligns and `clipRow` can
    never eat it; trailing it would put the explanation behind the one column (the ref)
    that overflows. Sized to the longest value it can emit — a column that grew with its
    content would be one more thing pushing the ref off the panel, which is what
    windowing exists to prevent.
  - A ref whose date git could not report renders an **empty** cell, never a guessed age.
    The column states the one thing it exists to state, or nothing.
- **Status cluster** — per branch: target label · `↓behind ↑ahead` · dirty dot ·
  checked-out marker. Fixed order, aligned into columns so the eye scans down. The
  target label is **variable width** — column widths are computed from the config's
  longest `Target.Key`, never hardcoded, though bounded like every other column above
  (a key is terse by intent and nothing in the config enforces it). **`↓behind ↑ahead`
  is a column too**: unpadded, a `↓3 ↑1` row and a `↓12 ↑345` row put the dirty dot in
  different places, and "aligned so the eye scans down" stops being true of the two
  glyphs that most need it.

**The Model holds:** loaded `Config` + `Store`; current screen; cursor position; which
ticket is expanded; a computed status map keyed by `ticketID + branch`; a
`textinput.Model`; add-flow state; window size; last error.

**Screens:** Dashboard (tickets, selected one expanded to its branches) · Add ticket
(ID entry) · Add ticket (pairing checklist, with the target picker as an overlay on
top of it) · Unmergeable diff panel (with the declare overlay on top) · Local-only
changes (with the hold picker and the note editor as overlays) · First-run wizard (a checklist of the repo's remote refs, newest first, each an
editable `age  Key`←`Ref` row; own Bubble Tea program, runs before the dashboard when the
repo is unconfigured — DESIGN reuses the checklist + `Key`←`Ref` shape, not the dashboard Model).

## 2. Components 📝

- **Ticket row** — ID, optional title, collapsed/expanded affordance.
- **Branch row** — branch name + the status cluster above.
- **Candidate checklist** (add flow) — space toggles a branch. Must make "which target
  does this map to?" unmissable — an unassigned selection is an error state, since the
  software never guesses.
- **Target picker overlay** ✅ — the general mechanism for assigning a target to a
  selected candidate branch. The original number/cycle-key idea assumed three targets
  and does not survive an unbounded config: number keys run out at 9, and cycling
  through 12 targets to reach the last one is miserable. The picker lists every
  configured target in config order, showing `Key` and `Ref` so the choice is
  unambiguous when keys are terse. Number keys stay as an accelerator for the first 9
  (see §3) — the picker is the mechanism, numbers are the shortcut. Because targets are
  unbounded, the overlay windows like every other list (§1) and never assumes its list fits.
- **Diff panel** (area 5) ✅ — the incoming diff for an unmergeable file, read-only,
  the replacement for opening the web UI to hunt for changes. Scoped to **one branch**
  (`screenDiff`): its colliding files are cycled through with `tab`/`shift+tab`
  (**wrapping at both ends** — reconciling a branch is a round trip, not a walk to a
  dead end), each file's `git diff B...T -- <path>` scrolls in a viewport
  (`bubbles/viewport`), and a header names `file X/N` and the `branch → target` being
  reconciled. Per-branch, not per-ticket, because MVP2 and MVP3 can hold different
  versions of the same file. **Plain text for every unmergeable format, always** —
  format-specific rendering is a different product and explicitly out of scope.
  *Plain text is not uncolored:* the diff is colored by line role (`+`/`-`/hunk/meta —
  §1) like any diff reader, because telling an added line from a removed one is what
  reading a diff **is**. What stays out of scope is understanding the *format* — no
  Unity scene tree, no workflow graph. On the dashboard, a collision shows as
  a trailing `⚠ N unmergeable` marker (`unmerge` color) on the branch row.
- **Declare overlay** (area 5, part 2) ✅ — `w` on the diff panel teaches Git the
  constraint by writing a `-merge` attribute. Drawn in the panel's place, the same
  mechanism as the target picker, and the same `j`/`k` · `enter` · `esc` shape, so an
  overlay is an overlay wherever the user meets one. Two steps, each a real choice
  shown with the reason it exists: **what** to declare (a matched config glob, tagged
  with its class — declares the whole class; or the file's own path — declares one
  file), then **where** it is written (`.gitattributes`, "committed and shared with the
  team"; or `info/attributes`, "local only, never committed, highest precedence").
  Never a default on either step, and never a guess — the same rule as pairing. `esc`
  unwinds one step at a time and lands back on the choice just made. A repo may
  allow-list destinations in `config.json`, in which case only the allowed ones are
  offered at all — a team without a committed `.gitattributes` never sees it and cannot
  pick it by accident.
- **Declared badge** ✅ — the diff panel names, per file, whether **Git itself** has been
  told never to merge it (`✓ declared to git`) or only Drift's config globs know (`not
  declared to git — w declares it`, `unmerge` color). Without it the declare flow is
  invisible: the file was already flagged as unmergeable, so writing the attribute
  changes nothing else on screen. The badge is the state, `w` is the verb, and the flip
  is the confirmation. Its new value is re-read from Git after a write rather than
  assumed, so one glob can flip several files at once and the badge can never lie.
- **Help overlay** (`?`) ✅ — keys and glyphs for **the screen you are on**, drawn in
  the panel's place; any key closes it (and is consumed, so the closing key never also
  acts on the screen underneath). `ctrl+c` still quits, as everywhere.
  - The key table is **generated from the live keymap**, never hand-written. Named
    actions are the contract (§3), so the help is a view of the bindings actually in
    force: an area-12 rebind updates it with no code change, and it cannot drift from
    what the keys do — the failure mode of every hand-maintained help screen. The nine
    `1`–`9` accelerators collapse to one row, and an action with no wording yet shows
    its own name rather than a blank row.
  - Keys deliberately left *unbound* so they reach a component (the diff panel's
    `j`/`k`/arrows, which the viewport handles) are added as static rows — otherwise
    the help would claim the panel cannot scroll.
  - The glyph legend is static, since glyphs are not rebindable. It is what pays back
    the density §1 buys: a dot is not self-describing. Entries stay short — a legend
    that wraps is harder to read than the row it explains — with the reasoning behind
    each signal left to §1. **Each glyph is drawn in its own role's style**, not one
    flat color: color *is* the signal in the status cluster, so a glyph explained in
    the wrong color explains the wrong glyph. `↓N` appears in its warning style even
    though zero-behind renders faint, because the case worth teaching is the one where
    the target moved.
  - Not offered on the ID-entry screen (every key there is text) or inside the target
    picker / declare overlays (momentary choice steps with their own one-line help).
- **Local-only list** (area 6) ✅ — a first-class screen (`l`), not a footnote, because
  **visibility is the whole feature**: raw `skip-worktree` already holds a tracked file
  back from every commit, and then hides it from `git status` so thoroughly that people
  forget it exists. One row per held path: a kind glyph, the path, the **primitive**
  holding it (`skip-worktree` / `info/exclude`), and the note. The mechanism is shown
  per row for the same reason the declared badge is on the diff panel — it is the honest
  answer to "what did Drift actually do, and what undoes it outside Drift". Two header
  lines state the scope outright (*held on every branch you check out*), because a hold
  is an index/ignore flag and the UI must never imply per-branch scope (CONTEXT.md).
  Glyphs: `◆` tracked in the **dirty** style, `◇` untracked in faint. The dirty color is
  reused deliberately — a held tracked file *is* uncommitted work Git is hiding, which
  is what that dot has always meant — and the filled/colored one is the tracked case
  precisely because it is the one Git makes invisible everywhere else. No new alarm
  color: `behind` stays the only thing on screen shouting.
  - **Hold picker** ✅ — `a` opens the working-tree changes as a checklist-shaped
    overlay, each row naming what holding it would do (`tracked → skip-worktree`,
    `untracked → info/exclude`), so the routing is visible rather than magic. A
    **staged** change is listed but refused, in `error` style with the fix named: the
    hold covers the working tree, not the index, so holding it would look like
    protection and give none.
  - **Note editor** ✅ — `n` opens an inline field over the list, the same shape as the
    wizard's key rename. The note is the only thing Drift persists about a hold, and it
    answers the question the list exists to answer three weeks later: why is this here?
- **Branch row selection** ✅ — the dashboard cursor moves over a **flat list of
  visible rows** (ticket headlines plus each expanded ticket's branch rows), so a
  branch is selectable in its own right — the prerequisite for a per-branch diff (and
  for area 7's per-branch shelve). Non-selectable lines (the "no branches" hint, the
  delete prompt) are drawn but never landed on.

## 3. Motion & interaction 📝

**Named actions are the contract; keys are a rebindable default.** The *actions* below
(move, expand, add, fetch, `local_only`…) are the stable interface every screen
dispatches on — never a raw key literal, so customization is a pure override layer and
not a retrofit. The default **keys** are considered, not sacred: a user-global keymap
(area 12) can rebind any action, and any action left unbound keeps the default here.
Dashboard:

| Key | Action |
|---|---|
| `j` / `k` / arrows | Move (over ticket **and** branch rows) |
| `enter` / `space` | On a ticket: expand / collapse · on a branch: open its unmergeable diff |
| `a` | Add ticket |
| `d` | Delete selected ticket |
| `r` | Refresh statuses |
| `f` | Fetch, then refresh |
| `esc` | Cancel an in-flight fetch (no-op when idle) |
| `s` | Shelve: pull the selected branch's target and merge it in (area 7) |
| `l` | Manage local-only changes (area 6) |
| `?` | Keys and glyphs for the current screen |
| `q` / `ctrl+c` | Quit |

Add flow (pairing checklist):

| Key | Action |
|---|---|
| `j` / `k` / arrows | Move |
| `space` | Toggle candidate branch |
| `t` | Open the target picker for the selected candidate |
| `1`–`9` | Accelerator: assign the Nth configured target directly, no picker |
| `/` | Filter the list (area 14) |
| `enter` | Save |
| `esc` | Clear the filter if one is applied · else cancel |

**The filter field is a text field, and binds like one.** While it has focus only
`↑`/`↓` · `enter` · `esc` · `ctrl+c` act (`DefaultFilterKeys`); every other key types.
That is not a shortcut — it is the only arrangement that works on a screen whose verbs
are single letters, since `e`, `j`, `t`, `space` and the digits all appear in real branch
names and all have to be typeable. Movement is deliberately the **arrows** and not `j`/`k`
for the same reason. `/` itself types too: ref names are full of slashes.

`enter` accepts the query and hands the keys back, so `j`/`k` navigate what is left;
`esc` clears the filter. **`esc` therefore means two things on these screens, one step at
a time** — the same unwinding the declare overlay does. With a filter applied it undoes
the filter; only with no filter does it decline the wizard or abandon the add. Declining
first-run setup by accident because the last thing you did was narrow a list is exactly
the surprise the one-step rule exists to stop, and the help line says which meaning is
live.

The **`?` table documents `/` with no code change**, because it is generated from the live
keymap — the property that makes an area-12 rebind free. The field itself binds no `?`,
for the same reason the ID-entry screen does not: every key there is text.

First-run wizard: the same shape, plus `e` to rename a key. It is the screen the filter
was built for — it offers *every* ref under `refs/remotes`, unnarrowed, and asks the user
to find their handful of long-lived mains in it. The pairing checklist looks like the same
problem and is not (`CandidateBranches` already narrows to branches containing the ticket
ID), and filters anyway: one list screen that filters and one that does not is a worse
tool than either.

Target picker overlay: `j` / `k` move, `enter` selects, `esc` cancels — deliberately
the same shape as the dashboard, so the overlay needs no learning. Targets past the
9th are reachable only through the picker, which is exactly why it's the mechanism and
the number keys are only a shortcut.

Diff panel (area 5):

| Key | Action |
|---|---|
| `tab` / `shift+tab` | Next / previous colliding file (wraps at both ends) |
| `j` / `k` / arrows / pgup / pgdn | Scroll the diff |
| `w` | Declare this file unmergeable to Git (write the `-merge` attribute) |
| `esc` | Back to the dashboard |
| `q` / `ctrl+c` | Quit |

Only file-stepping, declaring, and back-out are named actions here; scrolling is left
unbound so it falls through to the viewport's own keys — the panel needs no bespoke
scroll bindings, and a rebind can still name the actions it does define.

Declare overlay: `j` / `k` move, `enter` chooses, `esc` steps back — and unlike the
panel underneath, **every** key is bound while it is open, so `j`/`k` can never leak
through and scroll the diff behind the choice.

Local-only changes (area 6):

| Key | Action |
|---|---|
| `j` / `k` / arrows | Move |
| `a` | Hold a working-tree change on this machine |
| `d` | Release the selected hold |
| `n` | Note why it's held |
| `r` | Re-read the held set from git |
| `esc` | Back to the dashboard |
| `?` | Keys and glyphs for this screen |
| `q` / `ctrl+c` | Quit |

Shelve report (area 7):

| Key | Action |
|---|---|
| `esc` | Cancel while nothing has been touched · back to the dashboard once it ends |
| `?` | Keys and glyphs for this screen |
| `q` / `ctrl+c` | Quit |

The screen is a **report, not a list**: there is nothing to move over and nothing
to choose, so `esc` is the only verb and `j`/`k`/`enter` are deliberately unbound.
`esc` means two different things at two different moments, and the help line says
which is live: while the read-only steps run it **cancels**; once the stash is
taken it is **refused** with "it stops on its own", because there is no cancelling
into an undefined middle. Both are the same named action — the screen decides what
backing out means, exactly as every other screen does.

The six steps are drawn as a checklist with the running one spinning, so the user
can see which of pull / check / stash / merge / restore is happening. That is not
decoration: the back half mutates the working tree, and a sequence that stops
behind one undifferentiated spinner gives the user nothing to reason about. The
first three steps are read-only, so a halt in that half has visibly touched
nothing — the reassurance the step ordering was designed to give.

Glyphs are their own legend under `?`, and the distinction that carries the weight
is `■` against `✗`: one is git handing you something to reconcile, the other is
the sequence refusing or failing to run. `⚠ unmergeable` on a reported file is the
same signal in the same color as the dashboard's collision marker, because it
answers the same question — text merge, or external tool.

`a` and `r` carry their dashboard meaning across. `d` takes the dashboard's "remove the
selected thing" key rather than the more mnemonic `r` — bound the other way, a reflexive
refresh would silently drop a hold. Release needs **no** `y/n` confirm, unlike deleting
a ticket: it destroys nothing, since a released file's edits reappear at once as
ordinary working-tree changes.

Holding is its own named action (`hold_local`), *not* `add` reused. The `?` table is
generated per action, so one action serving two screens would have to describe itself as
both "add a ticket" and "hold a change" — the named-action contract only holds if a name
means one thing. Hold picker: `j` / `k` move, `enter` holds, `esc` backs out — the same
overlay shape as everywhere else. Note editor: `enter` saves, `esc` cancels, every other
key types, the same split as the ID-entry screen.

Git work runs as async `Cmd`s, so every one of these stays responsive — the UI must
never freeze on a fetch. States ✅ (built with the dashboard): a **loading** spinner
in the header while a status sweep is in flight; an **empty** dashboard that teaches
how to seed tickets; a one-line **error/notice** row under the panel that surfaces a
failed sweep (or a stale-status warning after a failed fetch) without tearing down the
view. ✅ (polish closing area 3): `esc` **cancels an in-flight fetch** — the git
process is killed (the fetch runs on a cancellable context) and its now-stale sweep is
discarded via a monotonic sweep id, so a hung fetch never traps the user. A plain
refresh is local and fast, so it stays non-cancellable. The **selection band fills the
panel's full inner width** (which now spans the terminal — see §1 Panels) rather than
hugging its text; the band width is applied once every row is built (`selectBand` in
`view.go`).

**The band has a second trap, distinct from the `Width()` one in §1.** A row is
assembled from independently styled cells (branch name, target, `↓behind ↑ahead`, dirty
dot, unmergeable marker), and each closes with a *full* SGR reset — which switches the
band's background off partway along the line. Wrapping the row in a background style
therefore paints the branch name, skips the middle of the row, and reappears only in
the trailing pad. `selectBand` re-arms the band's sequence after every inner reset
(`reopenBand`), discovering that sequence by rendering a sentinel through the style so
it follows the terminal's actual color profile. Neither band trap is visible to a test
— a test profile has no color, so nothing wraps and nothing resets — which is why both
are asserted structurally rather than by eyeballing rendered output.
