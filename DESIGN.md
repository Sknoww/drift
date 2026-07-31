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
  band *and* a left-edge marker (below). Pinned on the first build (ANSI-256, in
  `internal/ui/styles.go`, a considered-not-sacred starting point): `warning 214`
  (behind), `neutral 245` (ahead / in-sync), `dirty dot 220`, `checked-out marker 39`,
  `title 39`, `border/faint 240`, `error 203`, `unmergeable 170` (a
  branch with a collision that must be reconciled by hand — a distinct alarm from
  `behind`, since it means "moved *and* unmergeable", not just "moved").
  **Diff panel** ✅: `add 71`, `remove 167`, `hunk header 109`, git's bookkeeping
  lines faint `240`, context left unstyled. Muted rather than saturated — a whole
  screen of incoming change is the normal case there, so `+`/`-` must read as
  structure, not as alarm; `behind` stays the only thing on screen shouting.
- **Every role names a light end and a dark end** ✅ — the values above are the *dark*
  half of a `lipgloss.AdaptiveColor`, and Lip Gloss resolves which end to use from
  the background it detects at startup. The palette was pinned against a dark
  terminal and read as simply "fixed ANSI-256", which is two decisions wearing one
  coat: the **depth** is still right, the **assumption** was not. On a light
  background the dark values invert — `dirty 220` all but vanishes on white — and the
  selection band was worse than that (§3). The light values are the same hues a few
  steps darker, so the roles keep their relationship to each other: warning still
  shouts, neutral still recedes. The one deliberate asymmetry is the border, a shade
  fainter than the hint text on light and identical to it on dark: on white, `240`
  draws an outline that competes with the rows inside it, and a border's whole job is
  to be found without being read.
  - This has a **silent failure mode**, which is why it is called out rather than
    left to the code: if detection decides the terminal is dark when it is not, every
    light value is inert and the result is indistinguishable from light values that
    were badly chosen. `DRIFT_BG` forces an end, and the title names the detected one
    while any override is set, so the two can be told apart from the screen. The
    overrides are documented (area 16a), not a temporary harness — a preference saved
    in `prefs.json` deliberately leaves the title alone, since the label belongs to a
    run being experimented on rather than to a decision already made. `DRIFT_BG` also
    has a **file** now (area 16b): it was built to diagnose a misdetection, but a
    terminal that reports the wrong thing reports it every run, and "for this run" is
    the wrong shape for that fix.
- **One role is themable, and it is the accent** ✅ (area 16b) — `"accent"` in
  `prefs.json` recolours the title, the checked-out branch marker and the selected
  row's `▌`. A selection treatment is a **shape** (does it fill, does it mark) and the
  palette is what is poured into it; area 15 shipped four shapes with their colours
  baked in, and splitting the two is what lets `pair` exist in someone else's accent
  without a fifth hardcoded treatment.
  - **The three roles move together, as one field.** They held the same value before
    and that read as a coincidence; they are one role because they mean one thing —
    *drift is pointing at this*. Three separate settings would be three ways to make
    an incoherent screen, and it answers a question nobody asked.
  - **Nothing else is on offer, and that is the decision, not a first increment.**
    Colour *is* the signal: `behind` is the one alarm that shouts, `unmergeable` is a
    distinct alarm beside it, neutral recedes. A preference that let two of those
    collide would not be a preference, it would be a broken screen — and validating
    distinctness across arbitrary colours means a perceptual-distance threshold that
    either rejects good choices or admits broken ones. The accent carries no alarm, so
    it needs no such check. Asserted, so the surface cannot widen by accident.
  - **One value, used for both ends** — deliberately not the pair every built-in role
    names. Drift's *own* defaults are adaptive because Drift is choosing for a terminal
    it has never seen; a user is choosing for the terminal in front of them and sees
    the result immediately, so asking for a light end they will never look at buys
    precision nobody wants. The default stays an adaptive pair.
  - Both depths are accepted — an ANSI-256 index or a hex colour. ANSI-256 is right for
    Drift's own palette for the reason above, but the value a user actually has in hand
    is a hex code out of their terminal theme, and Lip Gloss degrades one to the nearest
    indexed colour on a 256-colour profile.
  - **`selection: "accent"` is unrelated to the `accent` setting**, which the shared
    word hides and the README says outright. That treatment is a *background*, and a
    background is only half a decision — it needs a foreground pinned against it (the
    light-terminal defect below), and one user value cannot pin a pair. The setting
    colours foregrounds, where one value carries the signal alone.
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
  renders a cell in exactly the width its column was given: elided when it is wider,
  padded when it is narrower, **measured in display cells and ANSI-aware**. Before it the
  package had two padding helpers that disagreed on how to measure (one `lipgloss.Width`,
  one `len()`) and *neither truncated*, so a column was only ever capped against
  over-padding — a 60-character branch name in a column "capped" at 32 rendered all 60 and
  shoved the status cluster off the right-hand end.
  - **The order of allocation is the rule.** A row's fixed cost is paid first, then the
    cell that carries the row's *point* — the status cluster on the dashboard, `⚠ pick a
    target` on the pairing checklist, the primitive on the hold picker — and the
    name/path column absorbs what is left. A long name shortens itself; the signal it
    was meant to sit beside never gives way. Inverting that is what the pass fixed.
  - **A column caps itself against the row, never against a constant** ✅ — the rule above
    run to completion (area 20). Five constants used to ceiling the user-supplied columns
    (a branch name at 32, a held path at 48), and a ceiling that never grows is sized to
    protect a narrow terminal and taxes a wide one: at 110 columns a 56-cell branch name
    was cut to 32 with forty cells sitting empty beside it. A column is now
    `min(its content, what the row has left)`; the `min*Col` **floors** stay, because a
    floor is a different thing from a ceiling and the squeeze path still needs one. The
    accepted cost, stated so it is not rediscovered as a regression: one long value sets
    the column for every row, so short rows carry a ragged gap out to the cell beside
    them. A gap is legible, a truncated path is not.
    - **Reserve what the row spends, and spend what is reserved.** The hold picker did
      neither — it took the widest detail out of the path's budget and rendered each
      detail unpadded, so a `tracked → skip-worktree` row bought alignment nothing and
      cost the path thirty cells. A per-row explanation of a per-screen fact is the shape
      to watch for: the reason a staged change is refused moved to the header, and the
      column is padded to what it reserves.
  - **Long values are cut where they carry the least meaning** ✅ — `elide` keeps as much
    of the head as fits plus the final segment, `…` between: `main-connector/src/main/…/
    Log4j2Configurer.java`, `update/PSOT-2222…-to-vscode-mvp3`. A tail cut removes exactly
    the half being read — for a path the identity, for a branch name under a
    `…-to-vscode-mvp3` convention the *target*, which is the half a dashboard row exists
    to let you check. The tail is a whole final segment when it fits in half the budget
    and a character-level cut when the last segment *is* the long part (a branch name
    carries no interior `/`, so a boundary-only rule degenerates to a tail cut).
    - **Head-weighted is what reconciles this with 19a**, which rejected a middle-elide
      for hiding the half worth reading. That objection was to an even split or a
      first-segment-only keep; the tail here can never take more than half the budget, so
      `origin/fix/PSOT-22114-…` — the half that gives a wrong target away — is what the
      arithmetic guarantees, and `/mvp-3` comes back beside it rather than instead of it.
      The rule to hold onto is the one 19a was defending: **the head must survive.**
  - **A detail line under the list, as the width-independent floor** ✅ — every list screen
    reserves one line beneath its rows, and the selected row's *full* value goes there in
    the `help` style when the row could not show it (`listBody` / `detailValue`). Elision
    decides which half of a value survives a column; this is where you read the half it
    cut, and it is the only answer that does not depend on the terminal being wide enough.
    - **Reserved always, drawn only when the value was elided.** A line that appeared and
      vanished with the cursor would make the panel grow and shrink as you move — the
      defect the status line's one-line rule already guards against, one line lower. For
      the same reason it is **one** line rather than wrapped: a detail whose *height*
      followed the selected value has the identical defect in the other axis. It takes the
      panel's whole width and falls back to the head-weighted elide above when even that
      is not enough, so it is a floor rather than a guarantee.
    - **Whether the value was elided is asked of the rendered row, never predicted.** The
      row is what the user is looking at, so it is what the question is about — and testing
      it catches a column that fitted itself *and* `clipRow` cutting a row no budget
      anticipated. A row already showing the value whole draws nothing: the same value
      twice reads as two values.
    - It is drawn **after** the selection band, never inside it. The detail is *about* the
      selected row, not part of it, and one background across both would read as two
      selected lines.
  - `clipRow` (Windowing, above) stays underneath as the **backstop**. It clips blind —
    whatever overflows off the right-hand end — so reaching it means dropping a trailing
    cell rather than ellipsising in place. A column that sizes itself never does.
  - **A header line costs the lines it wraps to.** `listBody` used to cost its fixed
    header as one line each, which is right only while each one fits; the wizard's intro
    wraps to three at the width floor, and the window then drew one row too many and ran
    the frame off the terminal. Prose can break the row budget exactly as rows can
    (`headerLines`).
- **The chrome measures itself too** ✅ — the header, the status line and the help line
  sit *outside* the panel, so neither windowing (rows) nor `fit` (columns) ever touched
  them, and all three overflowed. They are now measured against `chromeWidth` — the
  width inside the app's padding but outside the panel's border, so wider than
  `contentWidth` — in `internal/ui/chrome.go`.
  - **The help line elides against the real width; it is not shortened.** Shortening is
    lossy at *every* width (a 140-column terminal would show the same cut line as an 80)
    and goes stale the moment a binding is added. `helpLine` takes `lead` and `tail`:
    the **tail is what the line must never stop saying** — how to leave, and where the
    full key list is — and is paid for first, the same order of allocation a branch row
    uses. The lead is spent from the front, so what survives a narrow terminal is the
    start of the line, and `…` marks what went: an elided line must never read as the
    whole list. Segment order is therefore load-bearing, and the dashboard's is ordered
    by what a reader needs first rather than by grouping.
  - **A `esc`-means-two-things segment is always an anchor.** Where the line's job is to
    say which meaning is live (§3), elision can never be what removes it.
  - **One line means one line.** The status line's real defect was not its width but the
    newline: `err.Error()` is repo-supplied *and* multi-line, while `listChrome` costs
    this line as exactly one, so a three-line git error ran the frame off the top of the
    terminal — the same failure as a wrapping header line, from prose again.
    `chromeText` collapses whitespace first, then clips.
  - Before the first `WindowSizeMsg` nothing is clipped, the rule `contentWidth` already
    follows. The newline collapse is not a width decision and applies regardless.
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
- **A seeded name is the whole name, or no name** ✅ (area 19d) — the wizard seeds a
  target's key from its ref, and `deriveKey` may only ever hand back the ref's *whole*
  path after the remote. Put a seeded key back under its remote and it reproduces the ref
  it came from; a key that cannot be reconstructed names something other than what it
  points at. Where there is no honest whole-path key to offer, the row is seeded with
  **nothing** and the user names it (`e`).
  - **It is the never-guess rule applied to a label rather than to a choice.** The old
    rule fell back to the ref's last segment past a width threshold, so
    `origin/fix/PSOT-22114-…/mvp-3` was offered as `mvp-3` — a real main's name on
    somebody's ticket branch, sorted to the top by recency because a feature branch moves
    more than a main does. That target's key then read correctly on every dashboard row
    while its ref pointed at a feature branch: `↓behind` never converged, and one `u`
    published a merge into an open merge request. A guard against *mistyping* a ref (area
    4) is no guard against a list offering the wrong ref under the right name.
  - **Depth decides, then width.** A single-segment path is the ref's name at any length
    — nothing was dropped to reach it, and the key column bounds it. Only a multi-segment
    path has a shorter form to be tempted by, so only there does terseness gate the seed
    (`keySeedWidth`), which keeps `release/2.0` and `hotfix/2.0` distinct rather than
    collapsing both to `2.0`. Gating on width alone would refuse to name
    `origin/release-2-stability` — a real main, honestly named — and buy no honesty for it.
  - **An unnamed row states what it lacks**, in the key column's own place
    (`name it (e)`): a blank cell reads as a rendering fault. It is quiet until the row is
    selected and shouts (`⚠`) once it is, because a ref nobody picked is missing nothing —
    the pairing checklist's grammar exactly, where `⚠ pick a target` appears only on an
    included candidate. The blocked save then names the **ref**, never the key, on 19a's
    rule: the key is the string that made the wrong target look right.
  - **No advisory marker on a deep ref, and that is settled rather than pending.**
    Flagging is not narrowing and would have been admissible on area 14's distinction, but
    the only convention-free signal available — a last segment repeating another offered
    ref's name — is exactly what refusing to seed already neutralises. Keying one off path
    depth instead would put the same glyph on somebody's ticket branch and on a legitimate
    deep main (`origin/releases/2024/lts-maintenance`), which Drift cannot tell apart
    without enforcing a naming convention it has no business enforcing. Branch naming in
    the repo this came from is per-person, not per-repo.
- **Status cluster** — per branch: target label · `↓behind ↑ahead` · unpublished ·
  dirty dot · checked-out marker. Fixed order, aligned into columns so the eye scans
  down. The target label is **variable width** — column widths are computed from the
  config's longest `Target.Key`, never hardcoded, though bounded like every other column
  above (a key is terse by intent and nothing in the config enforces it). **`↓behind
  ↑ahead` is a column too**: unpadded, a `↓3 ↑1` row and a `↓12 ↑345` row put the dirty
  dot in different places, and "aligned so the eye scans down" stops being true of the
  two glyphs that most need it.
- **One signal on the row is not about the target** ✅ (area 17b) — `⇡` means the branch
  holds commits `origin/<branch>` does not, `⊘` that it has no upstream at all, blank
  that it is published and current. It is what makes `s` and `u` legible on screen
  rather than only in the help: `s` leaves `⇡` by design, `u` clears it, and without the
  glyph a branch merged locally and one merged *and published* render identically.
  - **A glyph, not a count, and the reason is the denominator.** `↑N` is already on the
    row and counts against the *target*; a second number would put two up-arrows with
    two different meanings side by side and ask the reader to hold which is which. A
    glyph reads as a **state** — there is work here that has not left this machine —
    which is the whole of what it has to say.
  - **It takes the dirty colour, and adds no alarm.** That colour has always meant "work
    that exists only here", and uncommitted and unpublished are its two kinds — the same
    reuse, on the same argument, as the local-only list's `◆`. `behind` stays the only
    thing on screen shouting. `⊘` recedes into the hint style instead: an unpublished
    branch is a fact about the branch, not something that went wrong.
  - **Fixed width in every state**, because it sits between the pair and the two glyphs
    the alignment above exists for. A cell that grew with its content would break the
    column for every row.
  - A degraded probe renders **blank**, never a guessed state — the rule the unmergeable
    marker already follows. "No upstream" is a third answer, not a zero, and the two are
    never conflated.

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
  - **It scrolls, because it outgrew the terminal.** Measured at 80×24: 28 lines on the
    dashboard, 26 on pairing — the keys it exists to teach were the ones off the top.
    Area 14's windowing does not apply (there is no cursor to window around) but a
    viewport does, and it is the only fix that stays fixed: shortening buys back four
    lines once, and areas 11 and 12 both add actions. **Only the scroll offset is kept**
    — the pane is derived on every render, so a resize refits it with no wiring and an
    offset can never point past content it was measured against (§1: derived, never
    tracked). An overlay that fits is drawn straight rather than through the viewport,
    so the panel keeps hugging its content.
  - **"Any key closes" survives, with one carve-out that says so.** The scroll keys are
    an **allowlist** (`j`/`k`/arrows/`pgup`/`pgdn`), never the viewport's own keymap —
    that one binds `d`, `u`, `f`, `b`, `space`, `h` and `l`, and on the dashboard `d` is
    delete. The carve-out applies **only while there is something to scroll**, and the
    footer states which contract is live (`any key closes` / `↑↓ N more · j/k scroll ·
    any other key closes`) — the same one-meaning-at-a-time rule `esc` follows (§3). The
    diff panel can let every unbound key fall through precisely because it has no such
    contract to keep.
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
    the target moved. **A screen that draws no glyphs gets no legend and no heading** —
    the targets screen is two plain columns, and an empty "Glyphs" section would promise
    an explanation with nothing under it while the dashboard's would explain signals
    that are not on the screen you are on. The same carve-out fires *within* a screen
    where a glyph is conditional: `u`'s plan prompt draws `●` only when there is
    uncommitted work to stash, and since 19a it opens on clean trees too.
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
- **Plan prompt** (area 17b, widened by 19a) ✅ — the moment `u` states what it is about
  to do and waits. 17b opened it for one case, leaving a branch with uncommitted work on
  it; 19a made it unconditional on the step that actually needed gating, the push. Drawn
  in the panel's place, the same mechanism as the declare overlay and the target picker;
  bound like the delete confirmation (`y`/`enter` · `n`/`esc`), because it is a yes/no
  question and not a list. `s` never opens it — it publishes nothing.
  - **It names the plan, in run order, rather than asking "are you sure?"** — the stash,
    the checkout, the merge, the publish, the return. A prompt that said less would be
    the same surprise with an extra keystroke.
  - **It states only what this run will do.** The stash and the return appear when there
    is work to stash, the checkout when a boundary is crossed, and the `●` line with
    them. A listed step that will not run is the same class of lie as a step that runs
    unlisted — and a conditional glyph is why the "no glyphs, no legend" rule now fires
    within a screen as well as between screens (§3, `?` overlay).
  - **The ref, never the key** — the one word the whole of 19a rests on. A target's key
    is a label the user chose and its ref is what gets merged; the dashboard shows the
    key, so a key reading correctly over the wrong branch is invisible until the merge is
    published. A ref too long for the line **keeps its head**: `origin/fix/PSOT-22114-…`
    is what gives a wrong target away, and the trailing `/mvp-3` is what made it look
    right. That was a tail cut until area 20 made it a head-weighted elide (§1), which
    keeps the same half and adds the suffix back; what is still forbidden is
    `origin/…/mvp-3`, the misleading half alone. The push destination is named on the
    same argument — an upstream under a
    different name is exactly the assumption a bare "publish it" would leave standing.
  - The guarantee line under a dirty plan is what ADR 0002 kept when it traded away
    "Drift never checks anything out", and this is the one screen where it has to be
    taken on trust before it happens — so it is **name-free and bounded**, where the plan
    above it interpolates branches: a line naming the branch twice measured 79 cells into
    a 76-cell panel at an ordinary 80-column terminal, and the clip cut the sentence
    carrying the guarantee mid-word.
  - **A prompt, not a refusal.** Being blocked by unrelated dirt is the friction `u`
    exists to remove, and the split between the two dirty cases now settles the *wording*
    rather than whether it opens at all.
  - The screen does not window (there is no cursor to window around), so its frame is
    **measured directly** at the width floor and at 80×24, in both shapes, with branch
    names and refs long enough to be what would overflow. Prose breaks a frame exactly as
    rows do (§1).
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
| `u` | Update: bring the selected branch up to date and publish it (area 17) |
| `s` | Shelve: merge the target into the checked-out branch, publish nothing (area 7) |
| `p` | Re-pair the selected branch row to another target (19b) |
| `l` | Manage local-only changes (area 6) |
| `t` | Show the configured targets and the refs they point at (19e) |
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

Target picker overlay: `j` / `k` move, `1`–`9` pick directly, `enter` selects, `esc`
cancels — deliberately the same shape as the dashboard, so the overlay needs no
learning. Targets past the 9th are reachable only through the picker, which is exactly
why it's the mechanism and the number keys are only a shortcut.

**It is one overlay in two places** (19b): over the pairing checklist, where `t` chooses
a target for a candidate, and over the dashboard, where `p` re-pairs a branch row.
One body, one keymap — two renderings of the same choice would be two things to keep in
step. The accelerators are bound *inside* it and not only on the checklist underneath,
because the body draws a digit beside each of the first nine targets: a drawn accelerator
has to be a live one. The target the branch aims at now is marked `current`, which is
19e's argument about its ref picker applied here — the cursor opens on that row, but that
signal is gone as soon as the user moves, and a list of targets with nothing
distinguishing one from another cannot say what is being changed *from*. A word rather
than a glyph, since this overlay binds no `?` and a glyph would have nowhere to be
explained.

**`p` commits on `enter`, with no confirmation, and that is the rule rather than the
exception** — the re-point below is still the one picker in Drift that asks. What earned
it a `y`/`n` was reach: re-pointing a target silently re-bases *every* paired branch's
`↓behind`. Re-pairing one branch re-bases one row, and that row shows its new target key
the moment the overlay closes. There is no success notice for the same reason: the row is
the feedback, permanently, where a notice would be wiped by the sweep the write itself
starts. Picking the target the branch already aims at is said out loud instead of written
— the one outcome where "it worked" and "nothing happened" leave the row identical.

The two fields are corrected on different screens on purpose, and the split is worth
keeping straight. A **target's `Ref`** (config.json) is what a key points at — `e` on the
targets screen. A **branch's `TargetKey`** (state.json) is which target it aims for — `p`
on the dashboard. Neither screen can fix the other's field, and 19's incident needed the
first while 19b is the second.

Targets screen (19e):

| Key | Action |
|---|---|
| `j` / `k` / arrows | Move |
| `e` | Point the selected target at a different ref |
| `esc` | Back to the dashboard |
| `q` / `ctrl+c` | Quit |

`enter` is deliberately **unbound** here. It means "commit this screen" everywhere else,
and this screen has nothing to commit — editing the selected row is `e`, which is what it
already means on the first-run wizard. `e` opens a ref picker (the wizard's list again:
`RemoteBranches`, recency-sorted, with the age column and `/`), then a `y`/`n` before the
write. **The confirmation is the one place a picker in Drift does not commit on `enter`**,
and the reason is the same one 19a widened `u`'s prompt for: the write is local and
reversible, but it silently re-bases every dashboard row's `↓behind` onto a different ref,
and that is the one effect no row can show as it happens. Both refs are named — the *from*
as prominently as the *to*, since the whole finding behind the area is that the ref being
replaced is the one nobody has seen. `e` reaches the **ref alone**: a target's key is what
every pairing in `state.json` references, so renaming one there would orphan them silently
(see roadmap 19b).

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

Plan prompt (area 17b, unconditional since 19a), open over the report before anything
runs — on every `u`, and never on `s`:

| Key | Action |
|---|---|
| `y` / `enter` | Run it — "stash it and go" when there is work on the tree |
| `n` / `esc` | Decline — nothing has been touched |
| `?` | Keys for this screen, and `●` when the plan draws one |

The help line's `y` and the prompt's own last line carry the same wording, so the two
cannot disagree about what is being agreed to.

`q` is deliberately **unbound**, the same as on the delete confirmation: while a yes/no
is on screen the contract is yes or no, and a key that quietly means a third thing is
not part of it. `ctrl+c` still quits, as everywhere. The `?` overlay opened over it obeys
its own "any key closes, and is consumed" rule — so the `y` that dismisses the help never
also answers the prompt underneath, which is the one screen where that would cost
something.

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

**The selected row is a band *under* a left-edge marker** ✅ — `▌` in an accent colour,
plus a subtle background. The band alone was measured at **1.06:1** against One Dark
(1.08 Dracula, 1.12 Gruvbox, 1.26 VS Code Dark, 1.59 on pure black, its best case): it
was not badly designed so much as almost not drawn. Pairing the two is what fzf and
Telescope do, and the property that decided it is **degradation** — a band is at the
mercy of a background Drift does not control, and a disappearing band is exactly the
failure being fixed, while a glyph in an accent colour does not depend on the
background at all. Neither half has to carry the signal alone. It reverses the
band-only rule this section used to state, so it earns
[ADR 0001](./docs/adr/0001-selection-band-and-marker.md); the alternatives that lost
are still in `band.go`, and are now a per-user setting ✅ — `"selection"` in
`~/.config/drift/prefs.json` (area 16a). Their names are `store`'s rather than the ui
package's, because a name in a config file is persistent and public where the
rendering behind it is not; a test asserts the two sets agree, so a treatment can
never be added without becoming selectable, or a name shipped without a treatment
behind it. Resolution is `DRIFT_BAND` → `prefs.json` → the default, and a bad value
reads differently at each level on purpose: a typo in the file refuses to start (it
would otherwise render the default and look like it worked), while a typo in the
env var falls through to the file and is named in the title.
- **The marker's colour is the themable accent** ✅ (area 16b, §1), so a treatment is
  a shape and the accent is what fills it — resolved `DRIFT_ACCENT` → `prefs.json` →
  the default, the same order and the same asymmetry. It is applied where the shape is
  resolved rather than stored on the treatment, so a marker treatment cannot reach a
  screen with an uncoloured glyph.
- **The band pins both ends, background *and* foreground.** A background with no
  foreground was the same defect's other half: on a light terminal the default
  foreground is dark, so the band rendered dark-on-dark and the one thing that must
  always be legible was the thing that disappeared.
- **The marker's gutter is not the row's to spend.** Rows size against `rowWidth` —
  the panel less the gutter — while the panel, the band and the chrome keep sizing
  against `contentWidth`. This is correctness, not bookkeeping: a row built to the
  full panel and then pushed right overflows by exactly the gutter, and `clipRow`
  cuts the trailing cell — which on a branch row is the status cluster, the very
  signal §1's order of allocation exists to protect.
- **The gutter is drawn on every row**, blank except the cursor's, so marking a row
  never puts its columns out of line with the rest of the list.

**The band has a second trap, distinct from the `Width()` one in §1.** A row is
assembled from independently styled cells (branch name, target, `↓behind ↑ahead`, dirty
dot, unmergeable marker), and each closes with a *full* SGR reset — which switches the
band's background off partway along the line. Wrapping the row in a background style
therefore paints the branch name, skips the middle of the row, and reappears only in
the trailing pad. `selectBand` re-arms the band's sequence after every inner reset
(`reopenBand`), discovering that sequence by rendering a sentinel through the style so
it follows the terminal's actual color profile. Neither band trap is visible to a test
— a test profile has no color, so nothing wraps and nothing resets — which is why both
are asserted structurally rather than by eyeballing rendered output. **This machinery
is now permanent** — keeping the band alongside the marker spends the argument that a
marker-only selection would delete it (ADR 0001), so every future row-assembling
screen has to keep the property it protects.
