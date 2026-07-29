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
16. 🛠️ **User-global preferences** — the second config root (`~/.config/drift/`,
    XDG-respecting), which `CONTEXT.md` has declared from the start. Purely additive: a
    new root, no migration, and the per-repo `<.git>/drift/config.json` is untouched.
    Split in two: **16a** shipped the layer on the smallest real setting, **16b** is
    theming — the half with a wrong answer.
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
    - ⏳ **16b — Theming: pick the accent, not just the shape.** Raised by dogfooding area 15's
      result: the marker reads well and the *blue* is not to taste. That is a second axis
      and the pair decomposes cleanly along it — a treatment is a **shape** (does it
      fill, does it mark) and a **palette** is the colours poured into it. Area 15 shipped
      four shapes with their colours baked in; splitting the two is what lets `pair` in
      someone's own accent exist without a fifth hardcoded treatment. 16a built the root,
      the file, the load path and the validation rule, so this adds fields to `Prefs`
      beside `selection` rather than a second file — which is why `newStyles` takes the
      whole `store.Prefs` and not the one string it uses today.
      - **Colour is the signal, so theming cannot be a free-for-all** (DESIGN.md §1). The
        alarm roles carry meaning — `behind` shouts, `unmergeable` is a *distinct* alarm
        from it, neutral recedes — and a theme that let two of them collide would not be
        a preference, it would be a broken screen. The likely shape is that the **accent
        is themable and the alarm roles are not**, or that everything is themable with
        distinctness validated the way `declare.destinations` is validated. Decide before
        building; this is the part with a wrong answer
      - **One accent currently serves three roles** — the title, the checked-out marker,
        and now the selection marker. Whether recolouring moves all three together or
        splits them is a real choice, not an implementation detail: they move together
        today because they *mean* "Drift is pointing at this", and that may be worth
        keeping
      - **A themed colour needs both ends or neither.** Every role names a light and a
        dark value now, so a user supplying one colour is supplying half a role. Either
        the config takes a pair, or Drift takes one value and uses it for both — which is
        the dark-terminal assumption walking back in through the front door, on exactly
        the surface area 15 spent itself fixing
      - **`DRIFT_BG` is the one to fold in.** It exists because the adaptive palette has a
        silent failure mode (16a kept it documented for exactly that reason), and a
        themable palette either gives it a `prefs.json` home or keeps it as the override
        it is. Decide alongside the palette, not after
