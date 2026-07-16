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

1. ⏳ **Git wrapper layer** — small `os/exec` shell-outs returning structured data.
   No checkouts, no Git library:
   - `localBranches()` — `git for-each-ref --format=%(refname:short) refs/heads`
   - `currentBranch()` — `git branch --show-current` (`""` if detached)
   - `isDirty()` — `git status --porcelain` non-empty. Note: working-tree dirty
     applies to the *checked-out* branch only, absent worktrees
   - `aheadBehind(branch, targetRef)` — see `CONTEXT.md`; the key signal
   - `candidateBranches(ticketID)` — local branches containing the ID, case-insensitive
   - `fetch()` — `git fetch --quiet`, so ahead/behind reflects the server
2. ⏳ **Config & store** — `Config`/`Store` types and JSON persistence under
   `<.git>/drift/`, resolved through the config search path (`CONTEXT.md`). First run
   writes a placeholder `config.json` marked "edit me" and prints its path. Targets are
   a list of any length; the placeholder must not imply otherwise.
3. ⏳ **Dashboard + manual pairing + status** — add/list/delete tickets, pair
   candidate branches to targets, show per-branch dirty + `↓behind ↑ahead`. This is
   the minimum useful tool: ship it and dogfood it while building the rest.
4. ⏳ **Unmergeable detection + diff panel** — resolve the unmergeable set via the
   hybrid rule in `CONTEXT.md` (`git check-attr merge` first, config globs additive).
   After a fetch, flag each unmergeable file that changed upstream on its target *and*
   has local edits, and show that exact diff, plain text. Replaces the "open the web UI
   and hunt for what changed" step. Mergeable changes merge normally and are never
   surfaced. Includes writing the `-merge` attribute on request, to either
   `.gitattributes` or `$GIT_DIR/info/attributes` at the user's choice.
5. ⏳ **One-key shelve sequence** — stash → merge target main → pop, as one keypress
   per branch. Stops and hands back the moment an unmergeable conflict appears; the
   reconciliation itself stays manual, always.
6. ⏸️ **Jira lookup** — deferred. Optional prefill (ticket title) and discovery
   ("assigned to me"). Slots in as a pure lookup source; the core must never depend
   on it and must work fully offline with hand-typed IDs.
7. ⏸️ **GitLab API (MR/pipeline status)** — deferred. Never a foundation: the
   unmergeable diff comes from local Git after a fetch, so the core needs no GitLab
   access.
8. ⏸️ **Pattern-based target pre-assignment** — deferred. For teams with rigid branch
   naming (unlike the author's), let config map name patterns to targets so the add
   flow arrives pre-filled. Strictly an accelerator on the pairing checklist: the user
   still confirms, and an unmatched branch stays unassigned rather than guessed.
   Manual pairing (area 3) remains the mechanism underneath.
9. ⏸️ **Unmergeable handoff command** — deferred. Let each unmergeable class in config
   name an external command (open the workflow web app, `open -a Unity`, launch Power
   BI). Drift will never reconcile these files — that's permanent — but it can compress
   "stop, find the right tool, hunt for what changed" into one keypress that shows the
   diff and launches the tool. Extends areas 4/5; never a prerequisite for them.
