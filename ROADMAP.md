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
   `<.git>/drift/`. First run writes a placeholder `config.json` marked "edit me"
   and prints its path.
3. ⏳ **Dashboard + manual pairing + status** — add/list/delete tickets, pair
   candidate branches to targets, show per-branch dirty + `↓behind ↑ahead`. This is
   the minimum useful tool: ship it and dogfood it while building the rest.
4. ⏳ **`.uwe` detection + diff panel** — after a fetch, flag each `.uwe` file that
   changed upstream on its target *and* has local edits, and show that exact diff.
   Replaces the "open GitLab and hunt for what changed" step. Non-`.uwe` changes
   merge normally and are never surfaced.
5. ⏳ **One-key shelve sequence** — stash → merge target main → pop, as one keypress
   per branch. Stops and hands back to the web app the moment a `.uwe` conflict
   appears; the reconciliation itself stays manual, always.
6. ⏸️ **Jira lookup** — deferred. Optional prefill (ticket title) and discovery
   ("assigned to me"). Slots in as a pure lookup source; the core must never depend
   on it and must work fully offline with hand-typed IDs.
7. ⏸️ **GitLab API (MR/pipeline status)** — deferred. Never a foundation: the `.uwe`
   diff comes from local Git after a fetch, so the core needs no GitLab access.
