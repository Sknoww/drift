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
5. 🛠️ **Unmergeable detection + diff panel** — resolve the unmergeable set via the
   hybrid rule in `CONTEXT.md` (`git check-attr merge` first, config globs additive).
   Split into detection+diff (shipped) and attribute-writing (next). Spec:
   `docs/specs/unmergeable-detection.md`.
   - ✅ **Detection + diff panel** — per branch, gated on `behind>0`, intersect what the
     target changed with what the branch changed (committed **+** working-tree for the
     checked-out branch), keep only the unmergeable ones (`check-attr -merge` ∪ config
     globs via `doublestar`), and show each file's incoming `git diff B...T -- <path>`
     plain-text in a scrollable panel. Branch rows are individually selectable now
     (flat visible-row cursor); `enter` on a branch opens its diff, because MVP2 and
     MVP3 can hold different versions of the same file — a ticket-scoped diff would
     conflate them. Mergeable changes are never surfaced. New area-1 calls:
     `ChangedFiles`, `FileDiff`, `WorkingTreeModified`, `CheckAttrMerge`
   - ⏳ **Write the `-merge` attribute on request** — to either `.gitattributes`
     (committed, team-wide) or `$GIT_DIR/info/attributes` (local, highest precedence)
     at the user's choice. Detection only *reads* the attribute today; this teaches Git
     the constraint so it behaves correctly even when Drift isn't running
6. ⏳ **Local-only changes** — a first-class, *visible* manager for changes you keep on
   this machine but never commit: logging tweaks, local config, scratch files. A mix of
   tracked and untracked paths under one list. Spec: `docs/specs/local-only-changes.md`.
   - **Tracked** paths → `git update-index --skip-worktree`; **untracked** paths → a
     drift-fenced block in `$GIT_DIR/info/exclude`. Drift routes each path by whether Git
     tracks it — the user marks a change, never a mechanism
   - **Git's own flags are the source of truth**, exactly as with unmergeables: list via
     `git ls-files -v` (the `S` tag) plus the fenced exclude block; the store holds only
     a per-path note. Drift can never fall out of sync, and Git stays correct when drift
     isn't running
   - The whole win over raw `skip-worktree` is **visibility** — the flag hides a file from
     `git status`, so people forget it exists. Drift surfaces the held set so it's never lost
   - Repo/worktree-global by nature: `skip-worktree` is an index flag, not per-branch —
     exactly right for "keep my log tweak on every branch I check out"
   - New area-1 calls: set/clear skip-worktree, list `ls-files -v`, a name-only
     changed-files diff for area 7's collision check. Exclude I/O is file-level, off `gitDir()`
7. ⏳ **One-key shelve sequence** — stash → merge target main → pop, as one keypress
   per branch. Stops and hands back the moment an unmergeable conflict appears; the
   reconciliation itself stays manual, always.
   - **Rides local-only changes through untouched.** Plain `git stash` (no `-u`) ignores
     skip-worktree files and untracked files, so both survive stash → merge → pop with no
     re-apply step — the auto-preserve area 6 promises
   - **The one collision it must catch:** before merging, intersect the incoming
     changed-file set with the held set. If the target main changed a file you hold locally,
     halt *before* the merge and surface it — same shape as an unmergeable handoff, never a
     silent clobber. No collision → fully automatic
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
    - Drift's first config outside `<.git>/drift/`; keymaps are per-user, so a per-repo
      home would be the wrong scope
