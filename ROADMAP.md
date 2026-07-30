# Drift — Roadmap

> **Read this first, every session.** The map: where the build is, what's next,
> and which spec/ADR governs each area. It tracks *areas of work*, not individual
> commits (git is the build history). Update the status line when an area's state
> changes — in the same commit as the work.
>
> System-of-record: [`CONTEXT.md`](./CONTEXT.md) · [`DESIGN.md`](./DESIGN.md).
> Feature specs live in `docs/specs/` (added per feature, only when one has real
> content/rules to pin). Decisions live in `docs/adr/` (added only on a deviation
> from the documented stack/conventions).

Status: ✅ shipped · 🛠️ in progress · ⏳ next · ⏸️ deferred

## Areas

Dependency-ordered. Each row is a coming session's headline — one line on what it
is; link its spec/ADR once one exists.

1. ✅ **Git wrapper layer** — `internal/git`. Small `os/exec` shell-outs returning
   structured data. No checkouts, no Git library:
   - `localBranches()` — `git for-each-ref --format=%(refname:short) refs/heads`
   - `currentBranch()` — `git branch --show-current` (`""` if detached)
   - `isDirty()` — `git status --porcelain` non-empty. Note: working-tree dirty
     applies to the *checked-out* branch only, absent worktrees
   - `aheadBehind(branch, targetRef)` — see `CONTEXT.md`; the key signal
   - `candidateBranches(ticketID)` — local branches containing the ID, case-insensitive
   - `fetch()` — `git fetch --quiet`, so ahead/behind reflects the server
   - `gitDir()` — `git rev-parse --absolute-git-dir`; where `<.git>/drift/` hangs off.
     Asking git is what keeps it right in a worktree or submodule
2. ✅ **Config & store** — `internal/store`. `Config`/`Store` types and JSON persistence
   under `<.git>/drift/`, resolved through the config search path (`CONTEXT.md`). First
   run writes a placeholder `config.json` marked "edit me" and prints its path. Targets
   are a list of any length; the placeholder must not imply otherwise:
   - `Resolve()` / `Paths` — `<git-dir>/drift/`, and the two files under it
   - `ConfigSearchPath()` — ordered, one entry today; entry zero is what first run
     writes and what wins. A team-wide config later is a new entry, not a migration
   - `LoadConfig()` — first hit on the path; returns `ErrPlaceholderConfig` (naming the
     file) when unconfigured, so callers point at the file instead of failing. Never
     rewrites a config that exists, including a broken one
   - `LoadState()` / `SaveState()` — no `state.json` yet is an empty `Store`, not an
     error. Writes are atomic via temp-file + rename
3. ✅ **Dashboard + manual pairing + status** — add/list/delete tickets, pair
   candidate branches to targets, show per-branch dirty + `↓behind ↑ahead`. This is
   the minimum useful tool: ship it and dogfood it while building the rest.
   - ✅ **Dashboard (read side)** — `internal/ui`, Bubble Tea. Ticket list, expand →
     branch rows, async status sweep (dirty + `↓behind ↑ahead` vs each target),
     `r` refresh / `f` fetch-then-refresh, empty + error states. Named-action dispatch
     is in place from day one (`keys.go`), so area 12 is a pure override, not a retrofit
   - ✅ **Add / pair / delete (write side)** — ID entry → pairing checklist → target
     picker overlay + `1`–`9` accelerators; `d` delete behind a `y/n` confirm →
     `SaveState`. Included-but-unassigned branches block the save (never a guessed
     target); a bare ticket is allowed. `l` stays bound and announces until area 6
   - ✅ **Polish carried over** — `esc` cancels an in-flight fetch (kills the git
     process, discards the stale sweep via a monotonic sweep id); a plain refresh is
     local and stays non-cancellable. Panels now span the full terminal width and the
     selection band fills the panel instead of hugging its text
   - ✅ **Two band traps, found by dogfooding** — the full-width band was right in
     intent and wrong twice over in practice, and **neither is visible to a test**
     (a test's color profile has no color, so nothing wraps and nothing resets).
     Both are written up in `DESIGN.md` §1/§3 and asserted structurally:
     (a) Lip Gloss counts a style's padding inside `Width()` but not its border, so a
     panel set to `contentWidth` had a text area two cells narrower than the rows built
     to fill it — every selected row wrapped, dropping its tail onto the next line;
     (b) each styled cell in a row closes with a *full* SGR reset, which switched the
     band's background off partway along the line, so the highlight covered the branch
     name, skipped the middle, and reappeared in the trailing pad
   - ✅ **`?` help overlay** — keys and glyphs for the screen you are on, drawn in the
     panel's place; any key closes it and is consumed. The key table is **generated from
     the live keymap**, so it is a view of the bindings actually in force rather than a
     hand-maintained list that drifts. Keys deliberately left unbound so they reach a
     component (the diff viewport's scrolling) are added as static rows. The glyph
     legend draws each signal in its own role's style — color *is* the signal, so a
     glyph explained in the wrong color explains the wrong glyph
4. ✅ **First-run setup wizard** — on first run in an unconfigured repo, pick the target
   mains from the repo's own refs and write `config.json`, instead of handing the user
   a JSON file to edit. Runs as its own Bubble Tea program (`internal/ui/wizard.go`)
   before the dashboard; `main.go` wires it into the `ErrPlaceholderConfig` path and
   `store.SaveConfig` writes the chosen targets. Same rule as pairing — show real
   things, let the user choose, never guess.
   - Offers **remote** refs (`RemoteBranches` — `git for-each-ref refs/remotes`, the new
     area-1 call, `<remote>/HEAD` filtered out; area 14 later added the recency sort and
     the tip date it carries). Targets are compared against `origin/<target>`, so a wizard
     offering local branches would silently produce targets that compare against the wrong
     thing
   - `Target.Key` is seeded from the ref and editable inline (`e`); `Ref` is the picked
     ref, so a target can never be a typo. The seed drops the remote prefix and keeps the
     rest of the path while it stays terse, falling back to the ref's last segment past
     that (area 15 tightened this — the original rule kept the whole path, which seeded
     keys nobody would type)
   - Any number of targets, including one. The wizard no more implies a count than the
     placeholder does; save blocks only on nothing-selected, an empty key, or a
     duplicate key — the guards `SaveConfig`'s `validate()` would reject anyway
   - Targets only. Unmergeable globs are typed patterns, not a list to pick from, and
     area 5 already owns that surface
   - The area 2 placeholder path stays as the fallback — wizard declined (`esc`),
     non-interactive run (stdin/stdout not a TTY), no remote refs to offer, or a config
     that exists but is broken. The wizard is the front door, not the only one
5. ✅ **Unmergeable detection + diff panel** — resolve the unmergeable set via the
   hybrid rule in `CONTEXT.md` (`git check-attr merge` first, config globs additive).
   Built in two parts: detection+diff, then attribute-writing. Spec:
   `docs/specs/unmergeable-detection.md`.
   - ✅ **Detection + diff panel** — per branch, gated on `behind>0`, intersect what the
     target changed with what the branch changed (committed **+** working-tree for the
     checked-out branch), keep only the unmergeable ones (`check-attr -merge` ∪ config
     globs via `doublestar`), and show each file's incoming `git diff B...T -- <path>`
     in a scrollable panel — plain text for every format, but colored by line role
     (`+`/`-`/hunk/meta), since telling an added line from a removed one is what reading
     a diff *is*; `tab`/`shift+tab` cycle the files, wrapping at both ends. Branch rows
     are individually selectable (flat visible-row cursor); `enter` on a branch opens
     its diff, because MVP2 and MVP3 can hold different versions of the same file — a
     ticket-scoped diff would conflate them. Mergeable changes are never surfaced. New area-1 calls:
     `ChangedFiles`, `FileDiff`, `WorkingTreeModified`, `CheckAttrMerge`
   - ✅ **Write the `-merge` attribute on request** — `w` on the diff panel opens a
     two-step overlay: **what** to declare (a matched config glob, which covers the
     whole class, or the file's own path) and **where** it goes (`.gitattributes`,
     committed and team-wide; or `$GIT_DIR/info/attributes`, local and highest
     precedence). Both steps are the user's choice, never a default — the same rule as
     pairing. Detection only *read* the attribute; this teaches Git the constraint so
     it behaves correctly even when Drift isn't running. Lines land in a Drift-fenced
     block, a pattern already declared anywhere in the file is a reported no-op, and
     the write is atomic. New in area 1: `TopLevel`, `AttrPath`, `DeclareUnmergeable` —
     the one place the git layer writes a file rather than shelling out, since an
     attributes file has no plumbing to write it (its *location* is still asked of git)
   - ✅ **Making the write visible** — declaring a file the panel already flags changes
     nothing on screen unless the panel says *which half* of the hybrid rule flagged it.
     Each collision now carries whether Git itself knows, and the panel badges the file
     (`✓ declared to git` / `not declared to git — w declares it`). After a write Drift
     **re-reads `check-attr`** rather than assuming what its own write achieved, so a
     glob covering several listed files flips all of them and the badge can never lie
   - ✅ **Destination allowlist** — `"declare": {"destinations": ["local"]}` in
     `config.json` filters *and* orders the picker, so a team that keeps no committed
     `.gitattributes` never sees it offered and cannot dirty it by accident. Hand-edited
     config rather than a keypress on purpose: a guard against an unwanted commit is
     worth more when it cannot be toggled off by a stray keystroke. An unknown name, a
     duplicate, or an empty list is a validation error, never something skipped over
6. ✅ **Local-only changes** — a first-class, *visible* manager (`l`) for changes you keep
   on this machine but never commit: logging tweaks, local config, scratch files. A mix of
   tracked and untracked paths under one list. Spec: `docs/specs/local-only-changes.md`.
   - **Tracked** paths → `git update-index --skip-worktree`; **untracked** paths → a
     drift-fenced block in `$GIT_DIR/info/exclude`. Drift routes each path by whether Git
     tracks it — the user marks a change, never a mechanism. The block reuses area 5's
     `# drift:begin`/`# drift:end` fence, so one Drift block shape appears wherever a user
     meets one, and an empty block is removed rather than left as markers around nothing
   - **Git's own flags are the source of truth**, exactly as with unmergeables: the list is
     `git ls-files -v` (the `S` tag) plus the fenced exclude block, re-read after every
     write rather than assumed; the store holds only a per-path note, and an annotation
     Git no longer backs is pruned on load. Drift can never fall out of sync, and Git
     stays correct when drift isn't running
   - The whole win over raw `skip-worktree` is **visibility** — the flag hides a file from
     `git status`, so people forget it exists. Drift surfaces the held set so it's never lost
   - Repo/worktree-global by nature: `skip-worktree` is an index flag, not per-branch —
     exactly right for "keep my log tweak on every branch I check out", and the screen says
     so in its header rather than leaving the scope to be inferred
   - **A staged change is refused, not held** — `skip-worktree` hides the working tree, not
     the index, so holding one would look like protection and give none. Listed, flagged,
     blocked, with the fix named. "Release and discard" is deliberately not built: it would
     be the only irreversible action here, and git is one command away
   - **Two mechanics that are correctness, not polish**: exclude entries are written
     anchored (`/path`) and glob-escaped, since an unanchored gitignore pattern matches a
     *basename at any depth* and would hold back every `config.yml` in the tree; and
     `update-index`/`ls-files` run from the working-tree root, since one resolves paths
     against the current directory and the other reports only the directory it runs in
   - New area-1 calls: `SetSkipWorktree`, `ClearSkipWorktree`, `SkipWorktreeFiles`,
     `WorkingChanges`, `ExcludePath`, `ExcludedPaths`, `AddExclude`, `RemoveExclude`.
     `ChangedFiles` (area 5) already covers area 7's collision check. Exclude I/O is
     file-level, off `gitDir()`; the fence and the atomic write are now shared in
     `fence.go`, extracted on its second consumer
7. ✅ **One-key shelve sequence** — `s` on a branch row runs pull → merge target main →
   pop as one keypress. Stops and hands back the moment a conflict appears; the
   reconciliation itself stays manual, always. Spec: `docs/specs/shelve-sequence.md`.
   - **Rides local-only changes through untouched.** Plain `git stash` (no `-u`) ignores
     skip-worktree files and untracked files, so both survive stash → merge → pop with no
     re-apply step — the auto-preserve area 6 promises, now asserted against a real repo
   - **The one collision it must catch:** before merging, intersect the incoming
     changed-file set with the held set. If the target main changed a file you hold locally,
     halt *before* the merge and surface it — same shape as an unmergeable handoff, never a
     silent clobber. No collision → fully automatic
   - **Pull, then merge.** Drift compares against `origin/<target>` and never checks a
     target out, so "pull the target" is: fetch that ref, then merge it — the two halves of
     `git pull` against a ref it never visits. Scoped to the **one** target being merged
     (`FetchRef`), so a sequence started for one branch can't quietly move every other
     branch's numbers. The remote/branch split asks `git remote` rather than cutting at the
     first slash, since branch names contain slashes routinely. A target no remote owns is
     said to be unfetchable and merged as it stands, never silently "fetched"
   - **Read-only until the last possible moment** — the ordering is the central mechanic,
     not an implementation detail. Preconditions, the recomputed `behind`, and the held-set
     check all run *before* the stash, so a sequence that refuses has stashed nothing and
     has nothing to undo. There is no partially-applied refusal
   - **The mutating half is atomic**: a merge conflict is aborted *and* the stash restored,
     so it either lands whole or leaves no trace. A pop conflict deliberately does **not**
     restore — `git stash pop` retains its entry on conflict, so nothing is at risk, and
     that halt *is* the reconciliation point the sequence was run to reach. Undoing it would
     undo the one thing that went right
   - **Checked-out branch only.** Drift's no-checkout invariant is correctness here, not
     squeamishness: a stash belongs to the branch it was taken on, so any cross-branch
     arrangement carries uncommitted work over a branch boundary to put it back. Another
     branch's row names the fix (`git switch <branch>`) instead. Auto-checkout is deferred
     with its questions open, and would earn an ADR — it reverses a documented invariant
   - **Two footguns pinned as correctness, not polish**: `git stash push` on a clean tree
     *succeeds and creates no entry*, so a sequence that popped unconditionally would pop
     someone else's work — `Stash` resolves `refs/stash` before and after and reports what
     really happened; and `stash@{0}` is a position, not an identity, so the created stash's
     **OID** is recorded and `StashPop` refuses (`ErrStashMoved`) if the top has shifted
   - New in area 1: `Remotes`, `RemoteRef`, `FetchRef`, `OperationInProgress`,
     `ConflictedFiles`, `StashRef`, `Stash`, `StashPop`, `Merge`, `MergeAbort`. Nothing
     parses git's English — outcomes are read from refs, exit status, and the unmerged
     index. `run` now sets an editor-proof environment for *every* shell-out, in one place:
     a git subprocess launching `vim` into a Bubble Tea render strands the user in an editor
     they never asked for
8. ⏸️ **Jira lookup** — deferred. Optional prefill (ticket title) and discovery
   ("assigned to me"). Slots in as a pure lookup source; the core must never depend
   on it and must work fully offline with hand-typed IDs.
9. ⏸️ **GitLab API (MR/pipeline status)** — deferred. Never a foundation: the
   unmergeable diff comes from local Git after a fetch, so the core needs no GitLab
   access.
10. ⏸️ **Pattern-based target pre-assignment** — deferred. For teams with rigid branch
    naming (unlike the author's), let config map name patterns to targets so the add
    flow arrives pre-filled. Strictly an accelerator on the pairing checklist: the user
    still confirms, and an unmatched branch stays unassigned rather than guessed.
    Manual pairing (area 3) remains the mechanism underneath.
11. ⏸️ **Unmergeable handoff command** — deferred. Let each unmergeable class in config
    name an external command (open the workflow web app, `open -a Unity`, launch Power
    BI). Drift will never reconcile these files — that's permanent — but it can compress
    "stop, find the right tool, hunt for what changed" into one keypress that shows the
    diff and launches the tool. Extends areas 5/7; never a prerequisite for them.
12. ⏸️ **Custom keymaps** — deferred. Rebind any action via a user-global
    `~/.config/drift/keymap.json` (XDG) — a sibling of `prefs.json` under the root area
    16a built and proved, on the same argument that kept prefs out of `config.json`: one
    file, one purpose. Defaults are a considered starting point, not sacred; an override
    rebinds any action, and an action left unbound keeps its default.
    - **The structural half is not deferred:** every screen dispatches on a *named
      action*, never a key literal, starting with the dashboard (area 3). That makes
      customization a pure override layer instead of a retrofit — see `DESIGN.md` §3
    - Conflicts (two actions bound to one key) are surfaced, never silently shadowed —
      the same "never guess" rule as pairing
    - **The `?` overlay already rides the keymap** (area 3): its key table is generated
      from whatever bindings are live, so a rebind documents itself with no code change
    - Keymaps are per-user, so a per-repo home would be the wrong scope. Area 16a is
      what makes that a settled point rather than an open one: the root, the XDG rule,
      the never-write rule and the reject-a-typo rule all exist now, so this area is a
      file format and nothing else
13. ⏸️ **Side-by-side diff** — deferred. A second rendering of the area-5 diff, as a
    **toggle** on the panel (never a mode, and never a replacement — the unified view
    stays the default). Deferred on three counts, all worth re-testing before building:
    - **Width is the binding constraint.** At 100 columns the panel's text area is 94,
      so two panes plus a gutter is ~45 each — and the formats this exists for (Unity
      YAML, `.pbxproj`, XML-ish `.uwe`) are deeply indented with long lines. It would
      wrap or truncate exactly the content it's meant to clarify, so it must be
      width-gated: below roughly 120 columns, fall back to unified rather than render
      something unreadable
    - **The diff is for orientation, not resolution.** Drift never edits an unmergeable
      file — that's permanent — so the panel answers "what moved upstream, so I know
      what to redo in the external tool", and the unified form is the compact answer.
      Side-by-side earns its keep when reconciling *in place*, the one thing Drift will
      never do
    - **Area 11 is the better lever** on the same problem: one keypress that shows the
      diff *and* launches the tool beats a second rendering of the same information
    - **Decide which comparison when it's on the table:** side-by-side of the *diff*
      (what changed) or of the *versions* (your file left, the target's right). Those
      are different tools, and the second may be the more useful one for hand
      reconciliation. Not a detail to settle in advance
14. ✅ **Large lists — windowing, then filtering.** Found by dogfooding v0.1.x on a real
    work repo with hundreds of remote refs: the first-run wizard rendered every ref,
    ran off the top of the terminal, and appeared to **freeze hard enough to force-quit
    the window**. Not cosmetic — the tool is unusable on exactly the repo shape it was
    built for. Every list screen has the flaw; the diff panel is the only one that
    doesn't, because it already windows through a `viewport` (`diff.go:528`).
    Windowing, row clipping, type-to-filter and the recency sort have all shipped. The
    one open question is settled (second-to-last bullet); what remains is deferred, not
    unresolved.
    - **What was measured**, so the fix targets the real cause rather than the symptom:
      - *Not git.* `RemoteBranches` is a local `for-each-ref refs/remotes` — no network,
        fast at any ref count. Nothing hangs before the wizard opens
      - *Not algorithmic.* `View()` is linear: 0.7 ms at 50 refs, 35 ms at 5000. Sluggish
        at the top end, nowhere near a freeze on its own
      - *The frame is unbounded.* 2000 refs is a ~500 KB frame; 5000 is ~1.26 MB — and
        Bubble Tea rewrites the **whole** frame on every keystroke. The freeze is the
        terminal drowning in ANSI, not Go being slow, which is why it presents as a hang
        rather than as lag
      - *Rows wrap, doubling everything.* 5000 short-named refs render 5007 lines; 5000
        long-named refs render 10007 — every row wrapping. `deriveKey` keeps the whole
        path after the remote (`origin/feature/TEAM-1234-x` → key `feature/TEAM-1234-x`)
        and `keyColWidth` pads **every** row to the widest key, so one long branch name
        inflates the column past the panel width for all of them
    - ✅ **Windowing was the fix and the prerequisite.** Render only the rows that fit,
      around the cursor. It bounds the frame at terminal height whatever the ref count,
      which kills the freeze outright — filtering to 200 matches still overflows without
      it. Built once in `internal/ui/window.go` and routed through by every list screen:
      wizard, dashboard, pairing checklist, target picker, local-only list and its hold
      picker, and both declare overlays. `wizardModel` now keeps a height (it took the
      `WindowSizeMsg` and stored only width). **The window is derived from the cursor,
      not tracked as scroll state** — nothing to keep in sync, nothing to reset when a
      list is rebuilt, and "the cursor is always drawn" holds by construction
    - ✅ **The cursor must never be invisible.** That was the actual bug the user hit,
      and the acceptance test: with N far larger than the terminal, the selection band is
      on screen at every cursor position, with an "N more" affordance at the clipped
      edge. Asserted by walking the cursor through all 400 positions and checking the
      frame stays inside the terminal with the selected ref on it. **Measured:** a
      5000-ref wizard frame went from ~1.26 MB to a flat 2.8 KB / 23 lines, unchanged by
      ref count *or* name length
    - ✅ **Row clipping came with it, because windowing alone is fiction.** Bounding the
      row *count* does nothing if each row wraps: 400 long refs still rendered a 37-line
      frame on a 24-line terminal, so the cursor still left the screen on exactly the
      repo shape this area exists for. `clipRow` caps each windowed row at the panel's
      content width, ANSI-aware (`charmbracelet/x/ansi`) — slicing a row of styled cells
      by bytes would sever an escape sequence and bleed color down the frame. It runs
      *before* `selectBand`, so the resets it introduces are re-armed by `reopenBand`.
      This is the floor that keeps the geometry honest, **not** area 15's first bullet:
      sizing each column properly, `padRight`'s `len()` bug, `deriveKey`, and the
      minimum-width floor all remain open there
    - ✅ **Then type-to-filter.** `j`/`k` through 400 branches is not navigation. `/` opens
      an incremental, case-insensitive substring filter, built once in `internal/ui/filter.go`
      and routed through by the wizard and the pairing checklist. **The matching set is
      derived from the query, never stored** — the same move as the window: the cursor
      means "the n-th visible row", so there is no second copy of the list to fall out of
      sync. Changing a query keeps the cursor on the row it was on when that row survives
      (`cursorFor`), so narrowing does not throw you to the top and clearing returns you
      to where you left off in 418 refs
    - ✅ **The counts are the component, not decoration.** `12 of 418` is what distinguishes
      "my query missed" from "this repo is empty", and a query matching nothing says so in
      words. Alongside it, `⚠ N selected rows hidden by the filter` — see the never-drop
      rule below
    - ✅ **A text field on a screen of single-letter verbs.** While the field has focus only
      `↑`/`↓` · `enter` · `esc` · `ctrl+c` act; everything else types, because `e`, `j`, `t`,
      `space`, the 1–9 accelerators and `/` itself all occur in real branch names. Movement
      is the arrows for the same reason. **`esc` now means two things one step at a time** —
      with a filter applied it clears the filter, only otherwise does it decline the wizard
      or abandon the add. Found by writing the test, not by reasoning: accepting a query
      with `enter` and then pressing `esc` quit first-run setup outright
    - **The wizard is the one that actually needs it** — confirmed twice by dogfooding.
      It offers *every* ref under `refs/remotes`, unnarrowed, and asks the user to find
      their handful of long-lived mains in it. The pairing checklist looks like the same
      problem and isn't: `CandidateBranches` already narrows to local branches containing
      the ticket ID (`git.go:153`), so a 400-branch repo still shows two or three rows
      when adding `ABC-123`. Filter it too for consistency, but the wizard is where this
      is load-bearing — build it there first and let the pairing screen inherit it
    - ✅ **Filtering must not silently drop selections.** A ref checked and then filtered
      out is still selected — the same "never guess" rule as pairing. The count of
      selected-but-hidden is on screen so the save can never quietly disagree with it,
      and the invariant is pinned on both screens. Its corollary fell out of building it:
      **save validates every row, not just the visible ones**, so a block can land on a row
      the query is hiding — the screen clears the filter and puts the cursor on that row
      rather than naming something the user cannot see. Revealing a choice the user already
      made is not guessing on their behalf
    - ✅ **Settled: sort by recency, never narrow by it.** The open question turned out to
      be two questions with opposite risk, and separating them decided it. *Ordering* is
      free of the stated hazard — it moves the likely mains to the top and removes nothing,
      so a repo whose main is a dormant maintenance branch still lists it. *Narrowing* is
      where "a heuristic that hides the right answer is worse than a long list" actually
      bites, and the roadmap's own argument settled it against: with `/` in hand the long
      list is navigable, which lowers what a default narrowing has to buy below what its
      risk costs. It would also be a filter the user never typed — the counts line could
      not distinguish "this repo has 20 refs" from "Drift chose 20 for you" — and it
      inverts the model by making `/` the key that *broadens*. Declined on the never-guess
      rule; building it later would earn an ADR, since it reverses that
      - `RemoteBranches` sorts `--sort=-committerdate` and returns `(Ref, Updated)` pairs.
        The date is `%(committerdate:unix)`, **not** `%(committerdate:relative)`: the
        relative form is English ("3 months ago") and nothing in the git layer parses git's
        English (area 7). An unparseable date leaves `Updated` zero rather than failing —
        the ref is the load-bearing half, and one odd date must not take first-run setup
        down with it
      - Alphabetical order only ever bought predictability when you already knew the name
        you wanted, and type-to-filter now does that strictly better. That leaves the list
        order one job — *discovery* — which is the one recency answers
    - ✅ **The age column, because an unexplained order reads as a bug.** A sort with
      nothing on screen accounting for it looks arbitrary. Each row leads with a
      fixed-width relative age (`20m`, `2d`, `1mo`, `2y`), so read top to bottom the column
      *is* the sort order, and it answers the wizard's own question directly: a ref touched
      two days ago reads as a main, one untouched for fourteen months does not
      - **Leading, not trailing, and fixed-width.** It is the only column here with a
        bounded width, so at the left edge it always aligns and `clipRow` can never eat it
        — trailing it would put the explanation behind the one column (the ref) that
        overflows. Sized to the longest value `relativeAge` emits (`12mo`); a column that
        grew with its content would be one more thing pushing the ref off the panel, which
        is the failure this area spent itself fixing
      - A zero date renders an **empty** cell, never a guessed age — the column stating the
        one thing it exists to state, or nothing
      - `padLeft` measures with `len()`, and that is correct *here and only here*:
        `relativeAge` emits ASCII by construction. The columns that pad arbitrary refs with
        `len()` are area 15's bug, not this one
    - ⏸️ **Other list screens** — the local-only manager, its hold picker, the target
      picker, and the declare overlays all window but do not filter. The shared piece makes
      each a few lines. The hold picker is the next real candidate (a working tree mid-
      refactor is a long list, and finding one file in it is exactly the filter's job); the
      target picker and the declare overlays are bounded by config and by a file's matched
      globs, so they are unlikely ever to need it
15. ✅ **UI polish pass.** The accumulated nits, found by auditing the render layer after
    area 14's measurements. Deliberately after 14: polish on a screen that can't be
    navigated is wasted work. Each item below was verified against the code, not guessed
    — the file:line is the evidence. Slice A (geometry and truncation), slice B (the
    chrome: header, status line, help line, `?` overlay) and slice C (the two color
    items) have all shipped. Slice C is the one the area could not settle by argument,
    and didn't try to: it was prototyped and looked at, and it produced the project's
    first ADR.
    - ✅ **Per-column truncation, and one way to measure.** The systemic item, and the
      root of several symptoms that looked unrelated. `padRight` padded to a width but
      *returned long input unchanged*, so every "capped" column was only capped against
      over-padding: `branchNameWidth` capped at 32 and a 60-character branch rendered all
      60, shoving the status cluster right — the branch name ate the row and the signals
      it was meant to sit beside were what got cut. Alongside it the package had **two**
      padding helpers that disagreed on how to measure: `padCell` used `lipgloss.Width`,
      `padRight` used `len()`, as did `branchNameWidth`, `widestTargetKey` and the
      wizard's `keyColWidth` — so any non-ASCII or wide rune in a branch name misaligned
      every column beside it. Both are one helper now (`fit`, in the new
      `internal/ui/columns.go`): display-width measured, ANSI-aware, truncating *and*
      padding to exactly the width its column was given. Every column in the package
      routes through it — dashboard, pairing checklist, target picker, wizard, local-only
      list and hold picker, both declare overlays, and the `?` overlay's key table
      - **The order of allocation is the actual fix**, not the truncation. A row's fixed
        cost is paid first, then the cell carrying the row's *point* — the status cluster,
        `⚠ pick a target`, the hold picker's primitive — and the name column absorbs what
        is left. The name shortens itself; the signal never gives way. Asserted at the
        width floor as well as at 100 columns, since "which one gives way" is only
        visible once something has to
      - **`↓behind ↑ahead` turned out to be a column too.** Unpadded, a `↓3 ↑1` row and a
        `↓12 ↑345` row put the dirty dot and the checked-out marker in different places —
        DESIGN.md §1 has always said the cluster is "aligned into columns so the eye scans
        down", and for the two glyphs that most need it, it wasn't
      - `clipRow` stays as the **backstop** and its doc now says so. It clips blind, off
        the right-hand end; a column that sizes itself ellipsises in place instead
    - ✅ **Key derivation is no longer too eager.** `deriveKey` kept the whole path after
      the remote, so `origin/feature/TEAM-1234-some-long-name` seeded a *key* of
      `feature/TEAM-1234-some-long-name` — and padded every wizard row to it, the
      row-wrapping that doubled the frame in area 14's measurements. It now keeps the
      path while it stays terse (≤16 cells) and falls back to the ref's **last segment**
      past that. Cutting to the last segment unconditionally was the obvious rule and the
      wrong one: it turns `release/2.0` and `hotfix/2.0` into two targets both called
      `2.0`. The threshold keeps the short multi-segment refs whole and shortens only
      what was the actual complaint. It stays a *seed* either way — the key is shown
      beside the ref it came from, `e` renames it, and a duplicate blocks the save with
      the row revealed
    - ✅ **A minimum usable width, declared rather than clamped.** `contentWidth` clamped
      to 1 and rendered into it, which produces garbage rather than a compressed view.
      Below **60 columns** every screen — dashboard and wizard alike — now draws one
      notice saying so and nothing else. Sized from the row it has to fit: at 60 the
      content width is 54, leaving the name ~27 cells beside a full cluster, and it sits
      well clear of the near-universal 80. Before the first `WindowSizeMsg` the size is
      genuinely unknown, so the screen draws — refusing on a guess would blank a terminal
      with room to spare
      - **Declaring the floor found a live bug in area 14's invariant.** At 60 columns the
        wizard frame was **25 lines on a 24-line terminal**: `listBody` costed its fixed
        header as one line each, but the wizard's two-line intro wraps to three at that
        width, so the window drew one row too many and the frame ran off the top — the
        exact failure windowing exists to prevent, reintroduced by *prose* rather than by
        rows. `headerLines` now costs a header line at the lines it really wraps to. The
        row budget is honest at every width; shortening the prose so it stops wrapping at
        all is the next item's job, not this one's
      - `listCapacity`'s floor-at-one-row in the other axis is left as it was — it keeps a
        short terminal correct rather than usable, which is the right trade until a
        height floor has a reason to exist
    - ✅ **Slice B — the chrome now measures itself too.** The three fixed-text items
      turned out to be one bug in three places: area 14 bounded the *rows* and slice A
      bounded the *columns*, and neither touched the header, the status line or the help
      line — the lines drawn outside the panel. They are now measured against
      `chromeWidth` (the width outside the panel, which is wider than `contentWidth`) in
      the new `internal/ui/chrome.go`
      - ✅ **The help line elides against the real width**, rather than being shortened.
        It was 108 columns (`view.go:385`) plus the app's 2 of padding against a
        110-column need, so it wrapped into the panel border at the near-universal 80 and
        still wrapped at 100 — and it was not alone: pairing-with-filter (85), the
        wizard's filter line (81), pairing (79) and local-only (79) all overflowed 80.
        Shortening was the worse trade twice over — lossy at *every* width, and stale the
        moment area 11 or 12 adds a binding. `helpLine` takes the segments as `lead` and
        `tail`: the tail is what the line must never stop saying (how to leave, and where
        the full key list is), it is paid for first exactly as a branch row pays for its
        status cluster, and the lead is spent from the front with `…` marking what went.
        Every help line in the package routes through it, so a new screen cannot
        reintroduce the bug
      - ✅ **The dashboard's segment order is now load-bearing**, so it was re-ordered: a
        narrow terminal keeps the front of the line, and at 80 columns the old order
        would have elided `a add` — how a new user does the first thing Drift is for.
        Move and open, then add and delete, then the sweep and the two screens
      - ✅ **The status line's real bug was the newline, not the width.** `statusLine`
        rendered `err.Error()` raw, and a git error is unbounded *and* multi-line while
        `listChrome` costs this line as exactly one — so a three-line error broke the row
        budget the same way the wrapping wizard header did, and ran the frame off the
        top. `chromeText` collapses whitespace first, then clips
      - ✅ **The header was the same defect, unlisted.** It carries the checked-out branch
        name, which is as unbounded as any name on a row below it. The title is the fixed
        cost and the branch cell absorbs what is left — the slice-A allocation rule,
        applied to the one row that had never been through it
    - ✅ **The selection band barely renders.** Raised by dogfooding as "not the cleanest
      looking thing", and measurement backs it: the band was ANSI 236 = `rgb(48,48,48)`,
      which against common terminal backgrounds gives a contrast ratio of **1.06:1** on
      One Dark, 1.08 Dracula, 1.12 Gruvbox, 1.26 VS Code Dark, and 1.59 on pure black —
      its best case. On most modern themes it sits within ~3% luminance of the page. The
      selected row is not badly designed so much as **almost not drawn**
      - *The full-row band is the right concept* — htop, lazygit, k9s, ranger, gitui and
        yazi all highlight the whole row, so this is not a reason to abandon it. What
        those do differently is make it read: an accent **hue** rather than a lighter
        grey, and/or a left-edge marker (`▌`, `>`) so the eye finds the row instantly
        even when the background shift is subtle (fzf, Telescope pair both)
      - *It is also under-specified.* `ticketSel` (`styles.go:65`) sets a background and
        **no foreground**, which is the same defect as the light-terminal item below —
        one style, two symptoms. Whatever replaces it should pin both ends
      - *A marker-based selection would delete machinery.* The full-width background is
        precisely what forces `reopenBand` (`view.go:201`) to re-arm the SGR after every
        inner cell reset — subtle, test-invisible, and the source of a real bug already
        (DESIGN.md §3). A left marker or a foreground treatment needs none of it. That is
        an argument worth weighing, not a decision
      - *Process note:* raising the contrast is a tweak to a value DESIGN.md §1 already
        calls "considered-not-sacred" — no ADR needed. Replacing the band with a marker
        reverses a documented §1/§3 decision and **earns an ADR**
      - *Settled by looking, as it had to be.* Four treatments were built behind
        `DRIFT_BAND` — raised grey, accent hue, marker-only, and the two paired — and
        looked at on a dark theme and a light one. **`pair` won**: a subtle band under
        a left-edge `▌`. The deciding property was **degradation**, which is the one
        the measurement above is really about — a band is at the mercy of a background
        Drift does not control, and a band vanishing into an unanticipated theme *is*
        the bug; a glyph in an accent colour does not depend on the background at all.
        Neither half has to carry the signal alone, which is why the pair beat a
        better-tuned version of either
      - Keeping the band **and** adding a marker reverses the documented band-only rule
        and spends the "a marker would delete `reopenBand`" argument, so it earned
        [ADR 0001](./docs/adr/0001-selection-band-and-marker.md) — the project's first.
        `reopenBand` is permanent, and it now has a second invariant beside it: the
        marker's gutter is not the row's to spend, so rows size against the new
        `rowWidth` while the panel, the band and the chrome keep sizing against
        `contentWidth`. Sizing a row to the panel and *then* pushing it right overflows
        by exactly the gutter, and `clipRow` cuts the trailing cell — the status
        cluster, the signal slice A's allocation rule exists to protect
      - The original 236 band is **not** a supported option, not even as a choice: it is
        the defect, and offering it would ship it. The three that lost stay in
        `band.go`, selectable by `DRIFT_BAND` — undocumented and temporary, since a
        selection style is a per-user preference and its home is area 16
    - ✅ **The palette assumed a dark terminal.** Colors were fixed ANSI-256 (`styles.go:9`),
      and `ticketSel` set `Background(236)` — near-black — with **no foreground**. On a
      light-background terminal the default foreground is dark, so the selected row
      rendered dark-on-dark: the selection band, the one thing that must always be
      legible, was the thing that disappeared. `lipgloss.AdaptiveColor` was the fix, and
      every role now names both a light and a dark end — the dark values are the ones
      that shipped, unchanged; the light ones are the same hues a few steps darker, so
      the roles keep their relationship to each other. The ANSI-256 choice itself stays
      right (DESIGN.md §1); this was about light vs dark, not color depth
      - **The band was the acute case, not the general one.** Pinning a foreground
        wherever there is a background is now a rule rather than a fix applied once —
        asserted over every treatment, so it cannot come back under another name
      - **It has a silent failure mode, so it is instrumented.** If detection decides the
        terminal is dark when it is not, every light value is inert and the result is
        indistinguishable from light values that were simply chosen badly. `DRIFT_BG`
        forces an end and the title names the detected one, so the two can be told apart
        from the screen rather than by reading the source
      - The one deliberate asymmetry: the border is a shade fainter than the hint text on
        light and identical to it on dark. On white, `240` draws an outline that competes
        with the rows inside it, and a border's job is to be found without being read
      - ✅ **The `?` overlay scrolls.** Measured at 80×24 before the fix: **28 lines on
        the dashboard**, 26 on pairing, 23 on the diff screen — so the keys it exists to
        teach were the ones scrolled off the top. The count decided the fix: shortening
        buys back four lines *once*, and areas 11 and 12 both add actions. Windowing does
        not apply (no cursor to window around) but a viewport does, and the diff panel
        already had one. Now a flat ≤23 lines on a 24-line terminal at every width and on
        every screen
        - **Only the offset is kept; the pane is derived on every render** (`helpPane`),
          the same move as area 14's window and its filter's matching set. A resize
          refits it with no wiring, opening it on a different screen needs no reset, and
          `SetYOffset` clamps an offset against content it was actually measured against
        - **The scroll keys are an allowlist, not the viewport's own keymap.** That keymap
          binds `d`, `u`, `f`, `b`, `space`, `h` and `l` — and on the dashboard `d` is
          delete, so a user pressing it over the help expects the overlay to close, not to
          half-page down. The diff panel can let every unbound key fall through precisely
          because it has no "any key closes" contract to keep
        - **The carve-out only applies while there is something to scroll**, and the
          footer says which contract is live — `any key closes` when it fits, `↑/↓ N more
          · j/k scroll · any other key closes` when it doesn't. The same
          one-meaning-at-a-time rule `esc` follows. An overlay that fits is still drawn
          straight rather than through the viewport, so the panel hugs its content instead
          of sitting in a box of blank lines
        - Before the first `WindowSizeMsg` the height is unknown, so the overlay draws
          whole and never scrolls — clipping against a guessed height would make a short
          overlay claim a scroll it doesn't have
    - Add items here as dogfooding turns them up — this area is the bucket, not a fixed list
16. ✅ **User-global preferences** — the second config root (`~/.config/drift/`,
    XDG-respecting), which `CONTEXT.md` has declared from the start. Purely additive: a
    new root, no migration, and the per-repo `<.git>/drift/config.json` is untouched.
    Split in two: **16a** shipped the layer on the smallest real setting, **16b** was
    theming — the half with a wrong answer. Both shipped; the split paid off exactly as
    intended, since 16b added two fields to an existing file rather than a file.
    - ✅ **16a — Selection style, and the root it lives in.** `~/.config/drift/prefs.json`,
      hand-edited, optional, and absent on most machines. Area 15 built four treatments,
      all four read well, and picking one for everybody is the wrong shape of answer — but
      the choice is a *person's*, not a repo's, so it must not live in the per-repo config
      where it would be re-declared in every repo. That is precisely the scope this root
      exists for.
      - **A differently-named file, not a second `config.json` on the search path.** The
        open question the area carried, settled before building: a user-global
        `config.json` would be a file that could plausibly hold `targets`, which is
        meaningless outside a repo, and one file with one purpose cannot make that offer.
        For the same reason `prefs.json` has **no search path of its own** — a preference
        is a person's, so a second root would be a repo or a machine overriding a choice
        that was never theirs to make
      - **Drift never writes it.** `config.json` has a placeholder because a repo cannot
        work unconfigured; a machine with no `prefs.json` simply has the defaults, and
        seeding one on first run would leave a file behind for a user who wanted nothing
      - **A wrong value is an error naming the file**, the same rule as
        `declare.destinations` and for a sharper reason: a bad selection falling back
        silently renders the *default treatment*, which on screen is indistinguishable from
        the requested one working. The message quotes what was written and offers the four
        valid names back, so the fix never needs the README
      - **The names are `store`'s, the treatments are `ui`'s.** A name in a config file is
        persistent and public where the rendering behind it is not, so `store.SelectionPair`
        and friends are the vocabulary and `band.go` implements them. They are two halves of
        one thing in two packages, so a test pins them together: a treatment added without a
        name is unselectable, and a name shipped without a treatment is a promise the file
        cannot keep
      - **`DRIFT_BAND` and `DRIFT_BG` stayed, and are now documented.** The roadmap offered
        "go away *or* become documented overrides", and the override reading won: an env var
        says **"for this run"**, which is exactly what trying a treatment needs and exactly
        what an edited file cannot say — you would be editing the file whose contents you are
        trying to decide. Resolution is `DRIFT_BAND` → `prefs.json` → default. `DRIFT_BG` had
        to survive regardless: it instruments the adaptive palette's silent failure mode and
        has no prefs equivalent until 16b, so keeping both is one story rather than two
      - A bad value reads differently at each level, on purpose. In the file it refuses to
        start; in the env var it falls through to the file, because a shell typo in a
        throwaway override is not a reason to refuse — and it is not silent either, since the
        title names the treatment actually in force whenever an override is set. A treatment
        chosen in `prefs.json` deliberately does **not** light that label up: it is a decision
        already made, and stamping it on every run afterwards is noise
      - **Area 12 (custom keymaps) now rides on this** rather than building it. Keymaps were
        always going to need this root; a one-string setting was a far better first load for
        it than a whole keymap format, so the layer got built once and proved on something
        small. `keymap.json` is a sibling file under the same root, on the same argument that
        kept `prefs.json` out of `config.json`
    - ✅ **16b — Theming: pick the accent, not just the shape.** Raised by dogfooding area 15's
      result: the marker reads well and the *blue* is not to taste. That is a second axis
      and the pair decomposes cleanly along it — a treatment is a **shape** (does it
      fill, does it mark) and a **palette** is the colours poured into it. Area 15 shipped
      four shapes with their colours baked in; splitting the two is what lets `pair` in
      someone's own accent exist without a fifth hardcoded treatment. 16a built the root,
      the file, the load path and the validation rule, so this added fields to `Prefs`
      beside `selection` rather than a second file — which is why `newStyles` already took
      the whole `store.Prefs` and not the one string it then used. New: `internal/ui/theme.go`,
      where the palette is resolved and `band.go` is left owning the shape alone.
      All four open questions were settled before a line was written, which is what the
      area asked for. No ADR: nothing here reverses a documented decision — DESIGN.md §1
      has always called the palette "considered-not-sacred"
      - ✅ **Settled: the accent is themable and nothing else is.** Colour is the signal
        (DESIGN.md §1), and the alarm roles carry meaning — `behind` shouts,
        `unmergeable` is a *distinct* alarm from it, neutral recedes. The alternative on
        the table was everything-themable with distinctness validated the way
        `declare.destinations` is, and it lost on what that validation would actually
        have to be: mapping arbitrary colours to a perceptual-distance threshold, which
        either rejects good choices or admits broken ones. The accent carries no alarm,
        so it needs no such check — the scope *is* the reason the feature needs no
        machinery. It is also the whole of the complaint that raised the area. Pinned by
        a test that rebuilds the style set under an accent and asserts every other role
        is byte-identical, so the surface cannot widen by accident
      - ✅ **One accent, three roles, one field.** The title, the checked-out marker and
        the selection marker move together, because they *mean* "Drift is pointing at
        this" — they held the same value before, and that read as a coincidence rather
        than as a role. Three settings would have been three ways to make an incoherent
        screen. `colTitle` and `colMarker` are now one `colAccent`, which is the decision
        expressed in the code rather than only in a doc. Splitting them later stays
        additive (new fields defaulting to `accent`), so nothing is foreclosed
      - ✅ **One value, used for both ends — and that is not the assumption walking back
        in.** The roadmap framed this as a pair-or-nothing rule, and building it found
        the distinction the rule was missing: Drift's *own* defaults are adaptive because
        Drift is choosing on behalf of a terminal it has never seen, which is precisely
        the bug area 15 fixed. A user is choosing for the terminal in front of them and
        sees the result immediately, so asking them for a light end they will never look
        at buys precision nobody wants. The default stays an adaptive pair; only an
        override collapses to one value, and only because someone typed it
        - Both depths accepted — an ANSI-256 index *or* a hex colour. ANSI-256 stays
          right for Drift's own palette (DESIGN.md §1) for the same
          terminal-it-has-never-seen reason, but the value a user has in hand comes out
          of their terminal theme as hex, and Lip Gloss degrades it to the nearest
          indexed colour on a 256-colour profile. `store.ParseAccent` is the one rule
          behind the file and the env var, and it **canonicalises** rather than merely
          accepting, because the resolved value is reported in the title — a title
          reading `accent:007` when `7` is what rendered would be the same class of lie
          the declared badge exists to prevent
      - ✅ **`DRIFT_BG` folded in, and the argument generalised.** It was built as an
        instrument for the adaptive palette's silent failure mode, and "for this run" is
        the right shape for *diagnosing* that — but a terminal Lip Gloss misdetects is
        misdetected every run, so the diagnostic was being handed to people who needed a
        setting. `"background": "light"|"dark"` is that setting; `DRIFT_BG` survives
        unchanged above it. That completes the symmetry: all three settings resolve
        env → file → default, and a bad value still reads differently at each level
      - **The one wrinkle, documented rather than designed away:** `selection: "accent"`
        (a fixed-hue *band*) and the `accent` key are unrelated, and the shared word
        hides it. Renaming the treatment was rejected — it has shipped, and 16a's own
        rule is that a name in a file is a name someone has. The treatment stays baked
        because a band is only half a decision: it needs a foreground pinned against it
        (the light-terminal defect area 15 fixed), and one user value cannot pin a pair.
        The `accent` key colours *foregrounds*, where one value carries the signal alone.
        README and DESIGN.md §1 both say so outright
      - `DRIFT_ACCENT` joins the other two as a documented single-run override, on 16a's
        argument verbatim — and it bites harder here than it did for the selection, since
        deciding a colour by editing the file whose contents you are deciding is the
        worst version of that loop. The title's label is now `overrideLabel` and names
        the accent **only when it was overridden**: an accent is literally the colour the
        label is drawn beside, so naming it otherwise is noise, where `pair` and
        `contrast` differ subtly enough that the screen does not tell you which you got
17. ✅ **`u` update — carry a branch all the way, including the checkout and the push.**
    Raised by dogfooding v0.2.0, and it is the sharpest kind of finding: the tool does not
    do the thing it was built to do. Going down the branch list and pressing `s` refuses on
    every row but the one you happen to be standing on, so the "one keypress per branch"
    payoff only ever arrives for one branch. **17a** shipped the git layer, the sequence,
    the unwind, the spec rewrite and the ADR; **17b** shipped the confirmation overlay and
    the dashboard's ahead-of-`origin/<branch>` signal, which together are what make the
    dirty cross-branch case runnable and its result visible. Three pieces were missing, and
    only the first was a design question — the other two were simply absent:
    - ✅ **No checkout.** `beginShelve` refused when the row was not `m.current`, and
      `internal/git`'s package doc opened with "nothing checks anything out". Both are
      gone: `Checkout` exists, deliberately **unforced**, because git refusing to switch
      is a result the sequence needs — something the stash could not see (a skip-worktree
      file that differs between the branches) is exactly what area 6 protects
    - ✅ **No push.** `Push` reads its outcome from `git push --porcelain`'s flag column,
      not from git's English, and distinguishes updated / already-up-to-date / **rejected**.
      It is the one call in the git layer that keeps stdout on a non-zero exit, because
      that is where a rejection is reported — and a rejected push and an unreachable
      remote are otherwise the same exit code needing opposite responses
    - ✅ **The branch's own upstream is never pulled.** `Upstream` reads it with
      `for-each-ref`, so "no upstream" is empty output and exit 0 rather than an exit code
      to be told apart from a real failure — and a branch that has never been published is
      an *answer*, not an error: nothing to pull into it, nowhere to publish it, and
      `git push -u` named rather than a remote guessed on the user's behalf
    - ✅ **This reversed a documented invariant, so it earned
      [ADR 0002](./docs/adr/0002-update-checks-out.md).** The invariant's *reason* survives
      intact and is the thing worth keeping: a stash belongs to the branch it was taken on,
      which is now enforced by the **return** rather than by never leaving.
      `docs/specs/shelve-sequence.md` was rewritten in the same commit to cover both verbs
      — a spec still saying "checked-out branch only" while the code checks out is worse
      than no spec
    - ✅ **The sequence**, in run order. Everything before the stash stays read-only, which
      is area 7's central mechanic and was not up for renegotiation: a refusal must still
      have nothing to undo. Holding that for a verb which merges *two* refs is what
      decided the one refinement to the order below — **both fetches are hoisted above the
      stash**, since fetching is how the numbers a refusal rests on become true and it
      touches no files. So the divergence recompute and the held-set check still run before
      anything mutates, exactly as they do for `s`:
      1. Preconditions — no operation in progress, the row has a paired target, the target
         resolves to a real ref. All as today
      2. Fetch the target ref **and** the branch's own upstream
      3. Recompute the divergence against both, then the held-set collision check — over
         **every** incoming ref, since the branch's own upstream can carry a change to a
         held file exactly as readily as the target can
      4. Stash, if the tree is dirty
      5. Check out the branch, if it is not already current
      6. Merge the branch's own upstream. Normally a fast-forward; a *conflict* here is a
         genuine halt, since it means the branch diverged from itself
      7. Merge the target in
      8. Push
      9. Return to the branch you started on, and pop the stash
    - ✅ **The unwind is one path, and it asks rather than being told.** Every post-stash
      halt shares it: abort a merge *if one is actually in flight*, return, pop — stopping
      at the first failure rather than stacking the next step on a rollback already going
      wrong. The probe is what lets a merge that failed *without* conflicting (nothing to
      abort) use the same path as one that conflicted, and it is safe precisely because the
      preconditions refused to start on top of anybody else's operation. The one thing it
      will not do is pop when it could not get back — popping wherever Drift happens to be
      standing is the single thing the whole arrangement exists to prevent
    - ✅ **A step with nothing to do says so.** "Pending" and "not needed" are the two
      things a stopped sequence must never conflate, and the answer comes from what git
      reported rather than from a prediction made a step earlier: `stepReady` knows the
      tree is clean, but whether there is anything to *put back* is `stepStash`'s own
      answer, and marking it early would be a claim about a result git had not given yet
    - ✅ **Settled: the dirty tree splits in two, and only one half is new.** Conflating
      them is what made this look harder than it is:
      - **Same branch, dirty** — exactly today's shelve. Stash and pop happen on one
        branch, no boundary is crossed, and it is already atomic. It needs no new prompt
        and no new argument; it only gains the pull and the push
      - **Different branch, dirty** — the new case, and the one the spec was worried
        about. Drift stashes on the branch you are leaving and pops on that same branch
        when it returns, so the work still never lands on a tree it was not taken from.
        That is a narrower claim than the spec's blanket refusal, and it is the reason
        this is now buildable
    - ✅ **17b — the confirmation overlay, and with it the full automated round trip.**
      The machinery shipped with 17a, so this was the prompt and nothing else: the gate at
      `stepReady` that refused is now the gate that asks. Not a flat refusal — being
      blocked by unrelated dirt is the friction the area exists to remove. The overlay
      names the actual plan before anything runs (which branch is being left, that the
      work is stashed, that Drift comes back and pops), and the user confirms. Same shape
      as the `y/n` delete confirm and the declare overlay, so an overlay is still an
      overlay wherever the user meets one. A clean tree gets no overlay at all, and
      neither does a dirty tree on the branch you are already standing on
      - **The placement was the one decision, and it is step 0 rather than step 3.** The
        prompt could have waited until the fetches had settled what there was to do, which
        would spare the case that prompts and then finds nothing. It asks first anyway,
        for two reasons pointing the same way: the question is about the user's own
        uncommitted work, which is fully known before a single ref is touched, so nothing
        a fetch returns could change the answer; and a verb whose whole promise is *one
        keypress* must not stop for input in the middle. Press `u`, press `y`, walk away —
        the alternative pauses after a network round trip, which is the babysitting the
        area exists to remove. The cost is a prompt on a sequence that then reports
        nothing was touched, which is true
      - **The prompt gates whether the sequence runs, never how.** Accepting resumes at
        the fetches and nowhere else, so there is exactly one path through the mutating
        steps and no second arrangement to keep in step with the first. Declining is the
        screen's ordinary cancel, unchanged — everything is still read-only, so there is
        nothing to undo and the notice already said so
      - **Prose breaks a frame exactly as rows do**, and this screen does not window,
        so it is measured rather than assumed. The guarantee line is deliberately
        name-free where the plan above it interpolates branches: naming the branch twice
        measured 79 cells into a 76-cell panel at an ordinary 80-column terminal, and the
        clip cut the one sentence carrying the guarantee mid-word. What is left is bounded
        whatever the branches are called
      - **The `?` overlay's "any key closes, and is consumed" rule earns its keep here.**
        This is the one screen where the key dismissing the help would otherwise stash the
        user's work, and it is pinned as such
    - ✅ **Settled: you end up where you started.** The list is a list, not a place you
      move to. Updating five branches must not silently relocate you, and each Update has
      to start from the same known place as the last. The return is part of the sequence,
      not a courtesy: **every halt path unwinds it too**, so a conflict at either merge or
      a failure at the push still puts you back and still pops. That is the "put me back
      where I was" bookkeeping the spec named as the price of admission, and it was the
      bulk of the work — pinned end to end against a real repo, not only over synthetic
      messages
    - ✅ **Settled: never force-push, and a rejected push is a handoff.** A rejection means
      the branch moved on the remote after the fetch read it, which is someone else's commit
      — exactly the class of thing Drift stops and hands back rather than resolving. The
      branch is left updated and merged locally; only the publish did not happen, and the
      report says so
    - ✅ **Settled: per-branch only.** A batch "update every paired branch" is where this
      is heading and is deliberately not here. One conflict mid-sweep raises questions
      about the other branches that the per-branch path does not have to answer yet, and
      the ADR should cover one reversal rather than two
    - ✅ **Settled: `u` and `s` both, differing by commitment.** `u` updates and publishes;
      `s` stays exactly as it shipped — merge the target, push nothing, checked-out branch
      only. Keeping `s` means the local-only path is still reachable for the case where you
      want to see the merge before it goes anywhere, and a verb that has shipped is a verb
      someone has. The cost is real and accepted: two near-identical sequences are two
      entries in a help table generated per action, so their descriptions have to carry
      the distinction on their own — "merge the target in" versus "bring it up to date and
      publish it". If that cannot be said in one line each, the split is wrong. Shipped as
      "merge the target into this branch — nothing is published" against "bring the
      selected branch up to date and publish it", and pinned by a test, so the two can
      never drift into describing the same thing
    - ✅ **17b — the dashboard gained an ahead-of-`origin/<branch>` signal.** Without it
      `u`'s push is invisible: today's `↓behind ↑ahead` measures against the *target*, so a
      pushed branch and a locally-merged-but-unpushed one render identically, and the
      column would be silent about the only step that touches the remote. This is the
      state `s` leaves you in by design, which is what makes the two verbs legible on
      screen rather than only in the help — `s` leaves the branch ahead of its own remote,
      `u` does not. Unpaired with a target and unrelated to it: a branch can be current
      with its target and still unpushed
      - **Settled: a glyph, not a count, and the denominator is why.** `↑N` is already on
        the row and counts against the *target*; a second number would put two up-arrows
        with two different meanings side by side and ask the reader to remember which was
        which. `⇡` reads as a **state** — there is work here that has not left this
        machine — which is the whole of what the signal has to say, and it costs two fixed
        cells rather than another variable column on a row area 15 spent itself narrowing
      - **No new alarm colour.** `⇡` takes the dirty style on the argument area 6 already
        made for `◆`: that colour has always meant "work that exists only here", and
        uncommitted and unpublished are its two kinds. `behind` stays the only thing on
        screen shouting. `⊘` recedes into the hint style — an unpublished branch is a fact
        about the branch, not something that went wrong
      - **Three answers, never conflated.** A branch with no upstream is `⊘`, not
        zero-unpublished — the same distinction the push itself already draws — and a
        probe that fails renders blank, making no claim rather than a wrong one. That
        rests on `Upstreams`, the one new git call: present-with-an-empty-value means a
        branch that has never been published, absent means a branch that is not there.
        One shell-out for the whole repo per sweep rather than one per row

18. ✅ **CI only runs at the point of no return.** `release.yml` triggers on `v*` tags and
    nothing else, so the first time CI ever executes a commit is the tag that publishes
    it. Cutting v0.3.0 is what surfaced this: the break rode `master` for two commits and
    announced itself by burning the tag. The test gate inside `release.yml` did its job —
    it stopped the run before GoReleaser and nothing was published — but a gate that only
    fires at the moment of commitment is a gate you meet too late
    - ✅ **A `go test ./...` job on push and PR** is the whole of it — `.github/workflows/ci.yml`,
      one job, no matrix, no lint or vet step, nothing else. The release workflow's own
      test step stays where it is: it guards the tag specifically, which is a different
      claim from "this commit is good", and the tag is the one that cannot be taken back
      - **Every branch, not just `master`.** A push filter of `["**"]` beside
        `pull_request` double-runs a branch that has a PR open, and that was accepted:
        work here lands on `master` directly, so the duplicate is theoretical while
        narrowing to `master` would leave a branch untested until the moment it merges —
        the same found-too-late shape, one step in
      - **Ubuntu only, matching `release.yml`.** The divergence that actually bit was
        never Mac-vs-Linux, it was declared-vs-inherited git config, and the Testing row's
        `GIT_CONFIG_NOSYSTEM` fix closed that. macOS is covered by the dev machine running
        the suite before anything is pushed
    - **The suite being hermetic is what makes this worth having.** Before, a green local
      run and a red CI run were both honest and neither was wrong; a push-triggered job
      would just have moved the surprise earlier. Now that `TestMain` pins
      `GIT_CONFIG_NOSYSTEM` (see `CONTEXT.md`'s Testing row), local and CI answer the same
      question, so a push job that passes means something on every machine

19. ⏳ **`u` published a merge nobody agreed to, onto a target that named a feature
    branch.** Raised by dogfooding v0.3.0 on a work repo, and it is the same kind of finding
    as area 17 — the verb 17 shipped did its job and the job was wrong. One keypress put a
    merge into an open merge request: the merged branch's commits showed up in the MR one at
    a time, the MR went conflicted against its own target, and the way out was `reset --hard`
    to the merge's first parent plus a force-push. **The merge mechanics are not at fault and
    are not in scope.** `Merge` is `git merge --no-edit` (`internal/git/shelve.go:202`) — the
    same command the IntelliJ flow this verb is modelled on runs — and nothing anywhere in
    the sequence rebases, cherry-picks or squashes. Three other things were each true, and
    each is a separate piece of work: nobody was asked before the push, the ref being
    merged was never shown, and once it was wrong there was no way to correct it in the
    tool
    - **Build order, after the diagnosis below.** 19d and 19e are what *caused* the
      incident; 19a is what failed to stop it; 19b is real but was not implicated; 19c is
      unrelated fidelity work. Build 19e and 19a first — together they make a wrong target
      visible at rest and at the one moment it can still be stopped for free.
      **19a and both halves of 19e have shipped**, so a wrong target is now visible on its
      own screen, named on the way past at the one moment it can still be stopped for free,
      and correctable in the tool. That closes everything the incident itself needed. What is
      left is 19b (a *pairing* cannot be changed — real, but not implicated), 19c (fidelity)
      and 19d (the wizard can still seed a key that lies about its ref — the deeper cause,
      now mitigated rather than fixed: a bad pick is visible and repairable, but still
      offerable)
    - ✅ **Settled: the `mvp-3` target's ref pointed at a feature branch.** Diagnosed from
      the work repo's reflog and its `config.json`. The roadmap's reading was right in every
      part — merging a branch into a source whose MR *targets* that branch cannot add commits
      to the MR (the merge base moves forward and the commit list is unchanged, which is why
      the manual flow shows one merge commit and nothing else), so for the commits to appear
      individually the merged ref had to be one the MR does not target. It was:

      ```
      "key": "mvp-3",
      "ref": "origin/fix/PSOT-22114-PickHistory-API-…-for-audit/mvp-3"
      ```

      A colleague's live ticket branch that happens to *end* in `/mvp-3`, not `origin/mvp-3`.
      The repo's other two targets were correct, so exactly one target was wrong and it was
      the one in use
      - **The reflog is the confirmation, and it reads backwards at first.** The only
        `merge origin/…` line in months of history is
        `merge origin/fix/PSOT-22114-…/mvp-3` — which looks like step 6 merging the branch's
        own upstream and is in fact **step 7 merging the target**. There is no
        `merge origin/mvp-3` anywhere in the repo's history, and there never could have been.
        Everything else in the reflog is bare (`merge mvp-3`, `merge release-2-stability`) —
        the user's own manual workflow, not Drift. `merge origin/…` is Drift's fingerprint,
        which is worth remembering the next time a reflog has to be read
      - **Drift never hit a conflict of its own**, exactly as predicted: `stepMerge` aborts
        and rolls the whole sequence back on conflict (`internal/ui/shelve.go:409`), so the
        run that reached the push was clean on its own terms. The sequence did precisely what
        it was told. It was told the wrong thing
      - **Three shipped decisions combined to tell it that**, each defensible alone, and the
        confluence is the finding:
        1. `deriveKey` (`internal/ui/wizard.go:179`) falls back to the ref's **last path
           segment** once the path is past `keySeedWidth`, so
           `origin/fix/PSOT-22114-…/mvp-3` seeds the key `mvp-3`
        2. The wizard sorts by **recency** (area 14), and an actively-developed feature branch
           sorts *above* a long-lived main, which by its nature moves less
        3. *Some* people in the repo end a branch with the main it targets, so those
           branches literally end in a main's name. It is a personal habit, not a team
           convention — others use the full string, others nothing of the sort. The ref that
           caused this belonged to someone who does it
        Net effect: the wizard put a feature branch at the top of the list and labelled it
        `mvp-3` — the exact string the user was looking for
      - **Area 4's safeguard held perfectly and bought nothing.** "`Ref` is the picked ref, so
        a target can never be a typo" is still true; this was not a typo but a correctly
        recorded wrong pick. A guard against mistyping is no guard at all against a list that
        offers the wrong thing under the right name — see 19d
      - **It has a second symptom, and that one is permanent rather than momentary.** The
        dashboard row was measured against an active feature branch, so it read `↓behind`
        forever and moved every time that branch's owner pushed. Pulling the real `mvp-3`
        could never converge it. A wrong target does not just misfire once when `u` is
        pressed — it makes the dashboard's central signal quietly untrue for as long as it
        stands
      - **The crossed-pairing reading was wrong and is worth recording as such.** Every
        `targetKey` in `state.json` was correct (`-mvp3`→`mvp-3`, `-r2stab`→
        `release-2-stability`, `-r2perf`→`release-2-performance`). The two branch names on
        that ticket differ only in that tail and the dashboard truncates tails (`fit` keeps the head,
        `internal/ui/columns.go:40`), which made a crossed pairing look like the obvious
        cause. It was not this bug — but the truncation is real and 19e carries it
    - ✅ **19a — the gate asked about the stash, and the step that needed gating was the
      push.** `stepReady` opened the confirmation on `leaves() && dirty`, so it fired only
      when `u` had to leave a branch with uncommitted work on it. That was 17b's question
      and 17b answered it correctly — being stashed without having agreed to it is a
      surprise. But it meant a clean tree, or a branch you were already standing on, took
      the entire sequence *including the push* on one keypress with nothing shown first.
      The two are not comparable risks: a stash is recoverable and local, while the push is
      the only step in the sequence with no unwind and the only one other people can see.
      17a already settled that a rejected push is a handoff rather than a force; this is
      the same argument one step earlier — the sequence must not reach the remote on a
      keypress that was never told it would. Shipped as a widening of 17b's overlay rather
      than a second prompt: one overlay, one place the plan is stated, and the
      `?`-consumes-its-key rule keeps working unchanged
      - ✅ **Settled: it fires on every `u`.** The open question turned out to be
        near-decided by 17b's placement, which was never open. "Only when it will publish"
        is not knowable at step 0 — whether there is anything to send is `stepHolds`'
        answer, after both fetches — so the quieter rule could only be had by *predicting*
        it from the dashboard's last sweep, which is the class of claim this package
        refuses everywhere else and which a stale sweep gets wrong in the direction that
        skips the prompt; or by moving the question into the middle of the run, which 17b
        ruled out on the grounds that a one-keypress verb must not stop for input halfway.
        Every-time is also the rule whose *absence* carries nothing to learn. The accepted
        cost is real and small: `u` on a clean tree is now `u` then `y`
      - ✅ **The prompt names the refs, which is what also closed the hole above.** The
        overlay states the plan in run order and now carries `merge in <targetRef>` and
        `publish it to <remote>/<branch>`, so a mispaired target is visible at the one
        moment it can still be stopped for free
      - ✅ **Validated by the diagnosis, and sharpened by it: the overlay names the
        `Ref`, never the `Key`.** An overlay reading `merge in origin/fix/PSOT-22114-…/mvp-3`
        would have stopped the incident dead, for free, before anything was published. One
        reading `merge in mvp-3` would have printed the key — which was correct, which is
        what made the target look right on the dashboard, and which was the whole of the
        lie. That is why the ref is the load-bearing word rather than an arbitrary choice
        between two strings, and it is pinned by a test
      - ✅ **The ref loses its tail, and that end is a decision rather than a detail.**
        `boundRef` sizes it against what the line has left and `fit` ellipsises at the tail
        — `origin/fix/PSOT-22114-…` is the giveaway and the trailing `/mvp-3` is the
        misleading part. Pinned by a test asserting *both* halves, since the obvious later
        "improvement" is a middle-elide showing `origin/…/mvp-3`, which hides the one thing
        worth reading. The ref is also placed **last** on its line, so the blind
        `clipPanelLine` backstop underneath cuts the same end the bound does
      - ✅ **The push destination is named too, on a hazard the spec already carried.** A
        branch may track an upstream under a different name, and publishing the right
        commits to the wrong ref is the failure a bare push hides. It comes from the
        sweep's `Upstreams`, already read for 17b's `⇡` and now keeping the ref rather than
        only the count. Three answers kept distinct exactly as they are on the row: a known
        ref, a branch that has never been published (no destination *exists* yet), and
        nothing known — which states the branch's upstream rather than guessing at one
        - **What the plan states and what the push acts on are deliberately two fields.**
          `planUpstream` is what the last sweep saw and is the overlay's alone;
          `upstreamRef` is what git reports at `stepPull` and is what gets pushed. A plan
          may be stated from what was known; only git's answer may be acted on
      - ✅ **The plan states only what this run will do.** Opening on clean trees and on
        the branch you are standing on means the old fixed three-line script would have
        promised a stash that never happens. The steps are built and numbered from `dirty`
        and `leaves()`, and the closing question matches the help line's `y` so the two
        cannot disagree. A listed step that will not run is the same class of lie as a step
        that runs unlisted
      - ✅ **A conditional glyph took the "no glyphs, no legend" rule inside a screen.**
        19e established it between screens; the prompt draws `●` only when there is work to
        stash, so on a clean plan the `?` overlay's Glyphs heading would have promised an
        explanation of something not on screen. Same carve-out, one level down (DESIGN.md §3)
      - ✅ **`s` still never asks, and that is an argument rather than an omission.** It
        publishes nothing, so every step it takes is local and covered by the same unwind
        every halt already runs. Pinned, so the widening cannot creep across the split 17a
        spent itself establishing
    - ⏳ **19b — a pair's target cannot be changed once it is made.** The target picker
      exists but is reachable only from the pairing checklist inside the add flow
      (`ActionOpenPicker`, handled at `internal/ui/addflow.go:186`); the dashboard keymap has
      no equivalent — 19e's `t` opens the targets screen, whose `e` re-points a *target's*
      ref, which is a different question (what a target points at, not which target a branch
      is paired to). `TargetKey` is written in exactly one
      place — `savePairing` (`addflow.go:326`), which builds a fresh `store.Ticket` and
      appends it — and `store` exposes no setter, only the read-only `Ticket` accessor
      (`internal/store/store.go:487`). So a wrong pairing is visible on the dashboard and
      correctable nowhere: `d` deletes the whole ticket (re-pairing every branch on it to
      fix one), re-adding the same ID is refused as already tracked (`addflow.go:110`), and
      the only other route is hand-editing `targetKey` in `<git-dir>/drift/state.json` with
      Drift closed, since `SaveState` rewrites the file whole
      - **Not implicated in 19's incident** — every pairing in the work repo was correct,
        and the wrong field was the *target's* `Ref` rather than a branch's `TargetKey` (see
        the diagnosis above, and 19e, which is the correction path that would actually have
        helped). This bullet previously claimed 19b was what turned the incident into a
        trap; that was written before the diagnosis and is wrong
      - **It is still real work, on its own merits.** Prevention (19a) and correction are
        different jobs, and a mispaired branch is genuinely uncorrectable in the tool today.
        It is also the cheapest of the three: the picker screen, the accelerators and the
        store round-trip all exist, so the work is a dashboard entry point, a `TargetKey`
        setter, and a save. It just is not urgent on this evidence
      - **Open: re-pair the selected branch row only, or reopen the ticket's whole
        checklist?** The row-only version matches how `s` and `u` already read the
        selection and is one keypress from the thing being fixed. Reopening the checklist
        reuses a whole screen unchanged but asks the user to re-confirm rows that were
        never wrong
    - ⏳ **19c — `u` merges the branch's own upstream, and the flow it is modelled on does
      not.** `shelveUpstreamCmd` (`internal/ui/shelve.go:849`) merges `origin/<branch>`
      into the branch before the target is merged at all. The manual sequence this verb
      reproduces — check out the target, pull it, check out your branch, merge, push — has
      no counterpart step: if the branch and its remote had diverged, the manual flow finds
      out at the push and hands it back. 17a added this deliberately and for a real reason,
      recorded in area 17: on a second machine, merging the target into a stale branch
      produces something that cannot be pushed. Both halves of that are true, which is why
      this is a question and not a defect
      - **Open: fast-forward only, or keep the merge?** Restricting it to a fast-forward
        keeps 17a's reason intact — a stale-but-not-diverged branch still catches up — while
        a genuine divergence becomes a halt with a named next step instead of a merge
        commit of the branch with itself. That is closer to the manual flow and to Drift's
        standing habit of handing back anything that needs a human. The cost is that a case
        which currently resolves itself would start stopping, and 17a's halt for this case
        (`shelve.go:397`) already exists but only fires on conflict
      - **Not the cause of the incident**, and it should not be bundled with 19a/19b on that
        pretext. It is a fidelity question about matching a documented flow, and it can be
        settled on its own evidence
    - ⏳ **19d — the wizard can seed a key that lies about its ref.** The first of the two
      causes the diagnosis turned up, and the deeper one: `deriveKey`'s last-segment fallback
      (`internal/ui/wizard.go:179`) plus area 14's recency sort plus a repo whose feature
      branches end in their main's name produced a wizard row reading `mvp-3` that pointed at
      `origin/fix/PSOT-22114-…/mvp-3`. Every part of that behaved as designed. Area 15 chose
      the fallback deliberately and rejected cutting to the last segment *unconditionally* on
      exactly the right grounds — it collapses `release/2.0` and `hotfix/2.0` into two targets
      both called `2.0`. The case it did not have in front of it is the one where the last
      segment is not merely ambiguous but **actively wrong**: a real main's name attached to
      something that is not a main
      - **Open: is this the key's problem or the list's?** Two different fixes. Making the
        *key* honest means never seeding a name the ref does not justify — but "justify" is a
        heuristic about what a main looks like, and area 14 already declined exactly that
        class of reasoning on the never-guess rule when it refused to narrow the wizard list
        by recency. Making the *list* honest means the ref is never the small print: show it
        at full weight beside the key, or refuse to seed a key at all for a ref with a deep
        path and make the user type one. The second is more in keeping with how the project
        has settled every prior version of this question
      - **Open: should a ref that is plainly somebody's ticket branch be flagged rather than
        filtered?** Flagging is not narrowing — area 14's settled distinction, and it survives
        here: an advisory marker removes nothing and hides nothing, so a repo whose main
        genuinely lives at a deep path still lists it and still works. What the marker keys
        off is the open part, and it must not become a naming convention Drift enforces
      - **Branch naming is per-person, not per-repo, which rules most of the answer out.**
        The work repo has no house convention: one person suffixes the target, another writes
        it out in full, others do neither. So there is no lexical pattern to key off, and
        anything trained on one person's habit would mislabel everyone else's branches in the
        same list. What *is* stable is structural — path depth, how many segments the ref has
        past the remote, whether the last segment repeats another offered ref's name — and a
        signal that survives the naming question is the only kind worth building on. This is
        the same shape as area 7's rule about never parsing git's English: read structure,
        not prose
      - **A test that would have caught it exists in shape already.** Area 16b pins that an
        accent cannot widen its surface by accident; the equivalent here is a case asserting
        that a seeded key round-trips to a ref a reader would recognise. Worth writing
        whichever way the two questions above land
    - ✅ **19e — a target's ref is write-once and invisible, so a wrong one cannot be seen or
      fixed.** The second cause, and the cheaper half. `Target.Ref` is written in exactly one
      place — the first-run wizard — and *was* rendered in exactly one, the wizard's own
      picker row (`internal/ui/view.go:587`). The dashboard shows `br.TargetKey`
      (`view.go:366`) and never the ref behind it, so a target pointing at the wrong branch
      looked *correct* on the screen the user lives in: the key said `mvp-3`, and `mvp-3` was
      what they wanted. The only route to a fix was hand-editing `config.json` with Drift
      closed — which is what the incident actually required, and it is a route nobody finds
      without reading the source. **Both halves have shipped: `t` shows the ref, `e` changes
      it.**
      - **This is the correction path 19b was miscast as.** Same argument, different field:
        prevention and correction are separate jobs, and 19a alone would have left a user who
        already has a bad target editing JSON. Unlike 19b it is load-bearing on real evidence
      - ✅ **Settled: showing first, and as its own screen rather than in the `?` overlay.**
        The area offered three homes — the overlay, a header line, or a keypress on the row —
        and the overlay lost on what the incident actually was. It is the reference card you
        open when you are *already* suspicious, and the whole finding is that nobody was
        suspicious: the key read correctly, which is exactly why it was never questioned.
        `t` on the dashboard opens a screen instead — one keypress from the list the user
        lives in, and somewhere a later message can send them. `internal/ui/targets.go`
      - ✅ **The ref is the subject, so it is weighted as one.** The row inverts the target
        picker's: there the key is what you are choosing and the ref is the disambiguator in
        the hint style; here the key takes its own bounded column first and the ref absorbs
        everything after it, in the ordinary foreground. A ref too long for what is left
        ellipsises **at its tail**, which 19a had already settled as the right end to lose —
        `origin/fix/PSOT-22114-…` is what gives a wrong target away, `/mvp-3` is what made it
        look right. Pinned by a test, since 19a's bullet exists precisely because someone
        could later "improve" it into a middle-elide
      - ✅ **A cursor from day one, though nothing is selectable yet.** Re-pointing a target
        is an action that hangs off a selected row, so a cursor-less screen would make the
        editing half a retrofit rather than an addition — the argument area 3 made for named
        actions, applied one screen later
      - ✅ **It names `config.json`, because editing is not built.** Hand-editing with Drift
        closed is the correction path today, so the header carries the resolved path and it
        stops being a route that needs the source. The path is left to **wrap** rather than
        clipped — half a path is a path you cannot act on, and `headerLines` already costs a
        wrapping header line at the lines it really takes. `New` takes `store.Paths` for it,
        which `main.go` had already resolved before the program opened
      - ✅ **It asks git nothing, and that is a finding rather than a shortcut.** The obvious
        addition — flag a target whose ref no longer resolves — would not have caught this:
        the wrong ref was a real branch that resolved perfectly. The screen's subject is what
        the *config* says, and a probe would have added a shell-out that answers a question
        nobody was asking
      - ✅ **A screen with no glyphs gets no legend.** The `?` overlay drew its "Glyphs"
        heading unconditionally, and this is the first screen with nothing to put under it.
        Guarded, with the screens that do have glyphs pinned so the carve-out cannot widen
        (DESIGN.md §3)
      - **`t targets` costs `l local` its slot at 120 columns**, and that is area 15's
        elision mechanism working rather than a regression. The lead is spent from the front,
        so the newest and least urgent segment sits last and is the first to go; every
        *doing* verb still survives to the 60-column floor, and `? help` — where everything
        elided lives — names both in full
      - ✅ **Editing: `e` on the row, and it re-points the ref alone.** The roadmap's guess
        was "re-running target selection", and the shape that survived contact is narrower:
        a **ref picker overlay** over the targets list — the same move/enter/esc overlay as
        the target picker, the declare overlay and the hold picker — offering the wizard's
        own list (`RemoteBranches`, recency-sorted, with the age column and `/`), then a
        `y/n`, then the write. Re-running the *wizard* was rejected on inspection: it
        replaces the whole config, so it would discard keys the user had already renamed,
        and a changed key orphans every `targetKey` in `state.json`. New: `store.SetTargetRef`,
        `internal/ui/targets.go`'s `repointState`, `ActionRepoint`, and two keymaps
        - **Ref only, and that is a constraint rather than an omission.** A key is what every
          pairing in `state.json` references, so renaming one here would silently orphan
          them — the *other* field's correction path, and 19b's job. `store.SetTargetRef`
          exposes the one field on purpose, and the header still names `config.json` so a
          user who reads `e` and assumes it reaches the key finds out where it doesn't
        - **The write is confirmed, and 19a's argument is why it isn't waved through.** Every
          other picker in Drift commits on `enter`, and this one does not: the pick is local
          and reversible, but it silently re-bases *every* row's `↓behind` onto a different
          ref, which is the one effect a row cannot show as it happens. The overlay names
          both refs, and the **from** is as load-bearing as the **to** — the whole finding is
          that the ref being replaced is the one the user has never seen, because its key
          read correctly. Both bound by 19a's `boundRef`, so a long one loses its *tail*
        - **Read-only until the last moment, and there is no partial refusal.** The picker,
          the pick and the confirmation all touch nothing, so declining has nothing to undo —
          area 7's central mechanic, applied to a two-step flow that only writes on `y`
        - **The re-sweep is local, not a fetch, and the reason is a finding.** The picker can
          only offer refs already under `refs/remotes`, so a ref you can pick is one you
          already have: there is nothing a fetch would make *resolvable*, only fresher, which
          is `f`'s question and not this one. Folding a network round trip into a config
          correction would also make it fail offline for nothing, and `applyStatus` clears
          the notice on every completed sweep — so a fetch path would routinely replace the
          re-point's own message with "fetch failed"
        - **No success notice at all: the row is the feedback.** It shows the new ref the
          moment the config folds in, which is permanent where a notice is transient — and
          making the ref legible at rest is the whole reason this area exists. A *failed*
          write does get one, and nothing clears it, since a failed write starts no sweep
        - **The model is not updated until the write succeeds.** The Cmd carries the whole new
          config and `applyRepoint` folds it in only on success. Optimism is cheap almost
          everywhere else in the package and expensive here: the very next sweep measures
          every branch against that ref, so a model ahead of the file would report correct
          numbers about a target that does not exist on disk
        - **Picking the ref already configured is said out loud, not written.** It is the one
          outcome where "it worked" and "nothing happened" leave the row identical, so the
          screen has to be what tells them apart — and the picker marks the current ref for
          the same reason, since a list of refs otherwise cannot say what you are changing
          *from*
      - ✅ **It re-reads rather than assuming.** The rule areas 5 and 6 both landed on: after
        a write, ask git what is true rather than trusting what Drift just did. A re-pointed
        target changes every row's `↓behind` at once, and a stale sweep would report the old
        number against the new ref — the same class of lie as the declared badge before it
        re-read `check-attr`. The sweep id advances with it, so an in-flight sweep against the
        *old* ref cannot land afterwards and clobber the new one
