# Drift — Project Context

> The single source of truth for **architectural and product decisions**.
> Visual/interaction decisions live in [`DESIGN.md`](./DESIGN.md). Update this
> document when a decision is made — it should describe what *is*, not the history
> of how it got here (that's git).

## App

Drift is a terminal UI that sits one layer above Git and organizes work by **ticket**
instead of by flat branch list. It serves codebases with several long-lived "main"
branches in flight at once, where a single ticket fans out into one feature branch per
target main, each based on a different version of the code. Git has no concept of that
grouping, and neither do general-purpose Git tools (IntelliJ, lazygit), which is
exactly why they bury you. **Storing and surfacing the ticket →
`(target-main, feature-branch)` grouping is this tool's entire reason to exist.** The
core loop: see every tracked ticket's branches at a glance, with which target each one
aims at, whether it's dirty, and whether its target main has moved underneath it.

**The number of targets is per-repo config, never a constant.** Three is the author's
situation, not a design assumption. Nothing in the model, storage, or UI may assume a
fixed count — this tool is meant to be picked up by any team with the same shape of
problem.

The name: a snowdrift gathers scattered snow into a bank — which is what the tool
does with scattered branches. It also nods to state drifting apart over time, which
is the signal the dashboard exists to show.

## Unmergeable files (load-bearing — read before touching merge logic)

Some file formats cannot be merged by Git in any meaningful way, and their conflicts
must be reconciled by hand in an external tool. Drift calls these **unmergeable**
files and treats them as a first-class concept, not a special case. Unity scenes and
prefabs, `.pbxproj`, Power BI workbooks, `.drawio`, Excel files, notebooks, and most
low-code platform exports all land in this category. Every team saddled with one has
independently invented the same painful manual dance. That general problem — not one
proprietary format — is what Drift addresses.

The motivating instance: part of the author's codebase is a smart-glasses app built on
a proprietary XML-ish + JS "workflow" format with the extension `.uwe`, living under a
known fixed path. These files **cannot be edited in Git or locally** — only through a
proprietary web-app GUI. Therefore Git cannot merge them, and conflicts are reconciled
by hand in that web app.

That constraint is immovable and out of scope to fix. Its consequences bind the design
for *every* unmergeable class, not just `.uwe`:

- **Never** merge, auto-resolve, or otherwise touch an unmergeable file. The manual
  reconciliation step stays manual, permanently.
- **Shelving is load-bearing.** A commit-based/WIP approach does not work — it just
  produces a Git-level conflict inside a file that can't be hand-edited. The flow is
  pull → merge target into branch → unshelve over the conflict so no conflict markers
  ever touch the file → reconcile by hand.
- The tool's job is to kill everything *around* the manual step — above all the
  "open the web UI and hunt for what changed" part, since that diff already exists
  locally after a fetch.

### Detection — hybrid, `.gitattributes`-first

1. **Git's own declaration.** `*.uwe -merge` in a `.gitattributes` file is the
   standard way to tell Git "never attempt a textual merge here." Drift reads it via
   `git check-attr merge -- <path>`, a shell-out consistent with the rest of the Git
   layer. Use `-merge`, **not** the `binary` macro: `binary` implies `-diff`, which
   would kill the diff panel. Unmergeable files are still text worth diffing.
2. **Config globs.** `config.json` lists path patterns, additive to the above and able
   to override it. This covers repos with no `.gitattributes` discipline, and expresses
   the "fixed path" half of a constraint that an extension alone cannot.

Drift can *write* the attribute, not just read it. **Where it writes is the user's
choice**, and both destinations are first-class:

- **`.gitattributes` at the repo root** — committed, shared with the whole team.
  Requires commit rights to the repo.
- **`$GIT_DIR/info/attributes`** — local, unversioned, per-repo, and the **highest
  precedence** of any attributes source. The path for users who cannot commit
  repo-wide files, which is the author's own situation.

Either destination means Drift teaches Git the constraint rather than routing around
it, so Git behaves correctly even when Drift isn't running.

---

## Architecture decisions

Seeded from the project brief. No vault stack note exists for Go/TUI projects yet;
if these defaults prove reusable, they earn one. A deviation from what's below earns
an ADR in `docs/adr/`.

| Area | Decision |
|---|---|
| Language | Go 1.24+. Binary and module both named `drift`. `go.mod` pins the floor at 1.24, not whatever toolchain is installed |
| Layout | Entry point at root `main.go`; packages under `internal/` (`internal/git` is the Git layer). `internal/` makes them unimportable outside the module, which is right for an app binary — no `pkg/` public API this tool has no reason to offer |
| TUI framework | Bubble Tea, with Lip Gloss for styling and Bubbles for text input |
| Git access | Shell out via `os/exec`, parse machine-readable output (`for-each-ref`, `status --porcelain`, `rev-list --count`). No Git library — this is how lazygit works and is the fastest path. Every call takes a `context.Context`, so a hung `fetch` is cancellable from the UI |
| State | Elm-style `Model`/`Update`/`View`. Git calls run as async `Cmd`s so the UI never blocks; results return as messages |
| Persistence | JSON under `<.git>/drift/` (found via `git rev-parse --absolute-git-dir`) — `config.json` (targets + unmergeable globs) and `state.json` (tickets). Inside `.git` makes it per-repo and unversioned for free. `config.json` is always hand-editable and Drift never rewrites one that exists, but hand-editing is not the *only* way in: a first-run wizard seeds targets from real refs (roadmap area 4), and the placeholder is the fallback for when it's declined or unavailable |
| Config resolution | A **search path**, today holding exactly one entry: `<.git>/drift/config.json`. Local-only, because the author has no rights to commit repo-wide files. Defining it as a search path now makes a committed team-wide config a purely additive change later, with no migration |
| Grouping | **Manual pairing.** Ticket ID substring-matches candidate branches to pre-filter; the user confirms and assigns targets. Branch naming is inconsistent, so target is **never** parsed from the branch name. Optional pattern-based *pre-assignment* for teams with rigid conventions is deferred (roadmap area 9) and would still never be silent |
| Invocation | Run from inside the repo, lazygit-style |
| Backend | None. The core is fully local and offline; Jira and GitLab are deferred, optional lookup sources only |
| Build target | macOS primary; Linux/Windows fine (Bubble Tea is cross-platform, so not a constraint) |

## Data model

```go
// Config — target mains and unmergeable file classes. config.json, hand-edited.
type Target struct {
    Key string // short UI label, e.g. "r2perf"
    Ref string // git ref for comparison, e.g. "origin/release-to-performance"
}
type Unmergeable struct {
    Name  string   // UI label for the class, e.g. "workflows"
    Globs []string // path patterns, e.g. "workflows/**/*.uwe"
}
type Config struct {
    Targets     []Target      // any number — never assume a fixed count
    Unmergeable []Unmergeable // additive to what `git check-attr merge` reports
}

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

- The author's real `Target` refs for their own `config.json` (any count).
- The workflow directory path, and confirmation of the `.uwe` glob, for the author's
  own config (area 5).
