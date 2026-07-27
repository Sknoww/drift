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
     area-1 call, `<remote>/HEAD` filtered out). Targets are compared against
     `origin/<target>`, so a wizard offering local branches would silently produce
     targets that compare against the wrong thing
   - `Target.Key` defaults to the ref's short name (remote prefix stripped), editable
     inline (`e`); `Ref` is the picked ref, so a target can never be a typo
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
    `~/.config/drift/keymap.json` (XDG), added as a user-global entry on the config
    search path — the additive move the search path was designed for. Defaults are a
    considered starting point, not sacred; an override rebinds any action, and an action
    left unbound keeps its default.
    - **The structural half is not deferred:** every screen dispatches on a *named
      action*, never a key literal, starting with the dashboard (area 3). That makes
      customization a pure override layer instead of a retrofit — see `DESIGN.md` §3
    - Conflicts (two actions bound to one key) are surfaced, never silently shadowed —
      the same "never guess" rule as pairing
    - **The `?` overlay already rides the keymap** (area 3): its key table is generated
      from whatever bindings are live, so a rebind documents itself with no code change
    - Drift's first config outside `<.git>/drift/`; keymaps are per-user, so a per-repo
      home would be the wrong scope
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
