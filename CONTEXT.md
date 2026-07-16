# Drift — Project Context

> The single source of truth for **architectural and product decisions**.
> Visual/interaction decisions live in [`DESIGN.md`](./DESIGN.md). Update this
> document when a decision is made — it should describe what *is*, not the history
> of how it got here (that's git).

## App

Drift is a personal terminal UI that sits one layer above Git and organizes work by
**ticket** instead of by flat branch list. The codebase it serves has three
long-lived "main" branches in flight at once, so a single ticket fans out into 2–3
feature branches — one per target main, each based on a different version of the
code. Git has no concept of that grouping, and neither do general-purpose Git tools
(IntelliJ, lazygit), which is exactly why they bury you. **Storing and surfacing the
ticket → `(target-main, feature-branch)` grouping is this tool's entire reason to
exist.** The core loop: see every tracked ticket's branches at a glance, with which
target each one aims at, whether it's dirty, and whether its target main has moved
underneath it.

The name: a snowdrift gathers scattered snow into a bank — which is what the tool
does with scattered branches. It also nods to state drifting apart over time, which
is the signal the dashboard exists to show.

## The `.uwe` constraint (load-bearing — read before touching merge logic)

Part of the codebase is a smart-glasses app built on a proprietary XML-ish + JS
"workflow" format. These files live under a known fixed path with the extension
`.uwe`, and they **cannot be edited in Git or locally** — only through a proprietary
web-app GUI. Therefore they **cannot be merged by Git**; conflicts are reconciled by
hand in that web app.

This is immovable and out of scope to fix. Its consequences bind the design:

- **Never** merge, auto-resolve, or otherwise touch a `.uwe` file. The manual
  reconciliation step stays manual, permanently.
- **Shelving is load-bearing.** A commit-based/WIP approach does not work — it just
  produces a Git-level conflict inside a file that can't be hand-edited. The flow is
  pull → merge target into branch → unshelve over the conflict so no conflict markers
  ever touch the file → reconcile by hand.
- The tool's job is to kill everything *around* the manual step — above all the
  "open GitLab and hunt for what changed" part, since that diff already exists
  locally after a fetch.

---

## Architecture decisions

Seeded from the project brief. No vault stack note exists for Go/TUI projects yet;
if these defaults prove reusable, they earn one. A deviation from what's below earns
an ADR in `docs/adr/`.

| Area | Decision |
|---|---|
| Language | Go 1.24+. Binary and module both named `drift` |
| TUI framework | Bubble Tea, with Lip Gloss for styling and Bubbles for text input |
| Git access | Shell out via `os/exec`, parse machine-readable output (`for-each-ref`, `status --porcelain`, `rev-list --count`). No Git library — this is how lazygit works and is the fastest path |
| State | Elm-style `Model`/`Update`/`View`. Git calls run as async `Cmd`s so the UI never blocks; results return as messages |
| Persistence | JSON under `<.git>/drift/` (found via `git rev-parse --absolute-git-dir`) — `config.json` (targets, hand-edited) and `state.json` (tickets). Inside `.git` makes it per-repo and unversioned for free |
| Grouping | **Manual pairing.** Ticket ID substring-matches candidate branches to pre-filter; the user confirms and assigns targets. Branch naming is inconsistent, so target is **never** parsed from the branch name |
| Invocation | Run from inside the repo, lazygit-style |
| Backend | None. The core is fully local and offline; Jira and GitLab are deferred, optional lookup sources only |
| Build target | macOS primary; Linux/Windows fine (Bubble Tea is cross-platform, so not a constraint) |

## Data model

```go
// Config — the fixed set of target mains. config.json, hand-edited.
type Target struct {
    Key string // short UI label, e.g. "r2perf"
    Ref string // git ref for comparison, e.g. "origin/release-to-performance"
}
type Config struct { Targets []Target }

// Store — tracked tickets with their manually-paired branch fan-out. state.json.
type TicketBranch struct {
    Branch    string // full local branch name, any naming style
    TargetKey string // references Target.Key
}
type Ticket struct {
    ID       string
    Title    string // optional; a later Jira lookup could fill this
    Branches []TicketBranch
}
type Store struct { Tickets []Ticket }
```

**Ahead/behind is the key signal**, computed per branch against the fresh
`origin/<target>` ref after a fetch, with nothing checked out:
`git rev-list --left-right --count <targetRef>...<branch>` → `"<behind>\t<ahead>"`.
**behind** = commits on target not in the branch (main moved under me).
**ahead** = my commits not yet on target.

## Open placeholders

- The three real `Target` refs for `config.json`.
- The workflow directory path, and confirmation of the `.uwe` glob (area 4).
- Confirm targets compare as `origin/<name>` post-fetch (recommended) vs local refs.
