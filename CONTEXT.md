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

A repo may **allow-list** which of the two it offers — `"declare": {"destinations":
["local"]}` in `config.json`. A team that keeps no committed `.gitattributes` lists only
`local`, and the shared destination stops being offered at all, so Drift can never dirty
a file that team does not use. It is hand-edited config rather than a keypress on
purpose: a guard against an unwanted commit is worth more when it cannot be toggled off
by accident.

Because both halves of the hybrid rule produce the same outcome, the UI has to say which
one is in play — otherwise declaring a file already flagged as unmergeable looks like it
did nothing. Each collision therefore records whether **Git's own attribute** is set, as
against only a config glob; the diff panel badges it per file, and declaring flips the
badge. The new value is re-read from `check-attr`, never assumed from what Drift wrote.

## Local-only changes (first-class concept)

Full rules: [`docs/specs/local-only-changes.md`](./docs/specs/local-only-changes.md).
Some working-tree changes belong on **this machine only** and must never reach a commit
— a bumped log level in a committed config, a local `docker-compose.override.yml`, a
scratch script. IntelliJ change lists solve this inside the IDE, and that boundary is
the weakness: a terminal `git commit -am`, CI, or a teammate's tooling ignores them.
Drift sits above Git, so it holds these with Git's **own** primitives — protection that
stops at no boundary — and then does what raw Git can't: makes the held set **visible**.

The design copies the unmergeable decision's shape exactly:

- **Two primitives, routed by whether Git tracks the path.** Tracked →
  `git update-index --skip-worktree`; untracked → a Drift-fenced block in
  `$GIT_DIR/info/exclude`. The user marks a change, never a mechanism.
- **Git's own flags are the source of truth**, never a Drift registry: held-tracked is
  read from `git ls-files -v` (the `S` tag), held-untracked from the fenced exclude
  block. The store persists only a per-path note, which can never contradict Git and is
  reconciled away if orphaned. As with unmergeables, Git stays correct when Drift isn't
  running.
- **Repo/worktree-global, not per-branch** — `skip-worktree` is an index flag, and one
  worktree has one index. That is the use case ("keep my log tweak on every branch"),
  not a limitation; the UI must not imply per-branch scope.
- **Rides both one-key sequences untouched.** Plain `git stash` (no `-u`) ignores
  skip-worktree and untracked files, so both survive stash → merge → pop with no
  re-apply — and the checkout `u` adds too, since a held file identical in both branches'
  trees is not something the switch has to touch. The one hazard — something incoming
  changed a file you hold locally — is caught *before* the merge by intersecting **every**
  incoming changed-file set with the held set, then surfaced like an unmergeable handoff.
  Drift never clobbers it silently, and it never forces the checkout past git's own
  refusal, which is the same protection arriving from the other direction.

## The one-key sequences (the one place Drift writes)

Full rules: [`docs/specs/shelve-sequence.md`](./docs/specs/shelve-sequence.md). Two verbs
share one state machine, differing by **commitment**. `s` shelves: pull → merge the target
main → put your work back, on the checked-out branch, publishing nothing. `u` updates: the
same merge on *any* paired branch, with the branch checked out, fast-forwarded to its own
upstream first, the result pushed, and the user returned to where they were standing. Everywhere
else Drift reads; this is where it writes to the working tree and to history, so the rules
about *when* it may are the substance of the feature.

- **Drift checks branches out, and owns the return.** The original invariant ("Drift never
  checks anything out") was reversed by area 17 under
  [ADR 0002](./docs/adr/0002-update-checks-out.md), because keeping it meant the tool did
  not do the thing it was built for: `s` refuses on every row but the one you are standing
  on, so "one keypress per branch" only ever arrived for one branch. The invariant's
  *reason* survives intact — **a stash belongs to the branch it was taken on** — because
  Drift stashes on the branch it leaves and pops on that same branch once it has come
  back. The guarantee is now enforced by the return rather than by never leaving. Targets
  are still never checked out.
- **You end up where you started, on every path.** The branch list is a list, not a place
  you move to. Every halt unwinds — roll back the merge, return, pop — and the unwind stops
  at its first failure rather than stacking the next step on a rollback already going
  wrong. The one thing it will not do is pop when it could not get back.
- **"Pull the target" is fetch-then-merge against a ref Drift never visits.** Targets are
  compared as `origin/<target>`, so the pull half is a fetch **scoped to that one ref** —
  a sequence started for one branch must not quietly move every other branch's numbers.
  `u` applies the same split to the branch's own upstream, which is what keeps it correct
  on a second machine — but **fast-forward only** (19c). Catching a stale branch up is the
  reason that step exists; merging a *diverged* one is the branch merged with itself, a
  commit nobody agreed to on the way to a push, so it is refused before the stash and the
  reconciliation is named.
- **Read-only until the last possible moment.** Every check that can refuse the sequence
  runs before the stash, so a refusal has stashed nothing and has nothing to undo. There
  is no partially-applied refusal. This is why *both* fetches are hoisted above the stash:
  fetching is how the numbers a refusal rests on become true, and it touches no files.
- **The mutating half is atomic, with one deliberate exception.** A merge conflict is
  rolled back whole — aborted, returned, and the stash restored: it either lands or leaves
  no trace. A stash-pop conflict is **not** restored, because git retains the stash entry
  on conflict — nothing is at risk, and that halt is the hand-reconciliation point the
  sequence exists to reach.
- **Never force-push.** A rejection means the branch moved on the remote, which is someone
  else's commit. The branch is left updated and merged locally; only the publish did not
  happen, and the report says so.
- **Every halt is a handoff**, the same permanent rule as an unmergeable file: Drift
  surfaces what it found, names the git command that resolves it, and stops.

---

## Architecture decisions

Seeded from the project brief. No vault stack note exists for Go/TUI projects yet;
if these defaults prove reusable, they earn one. A deviation from what's below earns
an ADR in `docs/adr/`.

| Area | Decision |
|---|---|
| Language | Go 1.24+. Binary and module both named `drift`. The `go.mod` floor tracks the **minimum the chosen stack requires** — 1.24.2 today, raised there by the Charm libraries' own `go.mod` directives, not by the installed toolchain (1.26). The point stands: the floor is never bumped just because a newer toolchain is installed |
| Layout | Entry point at root `main.go`; packages under `internal/` (`internal/git` is the Git layer, `internal/store` the config/state layer, `internal/ui` the Bubble Tea dashboard). `internal/` makes them unimportable outside the module, which is right for an app binary — no `pkg/` public API this tool has no reason to offer |
| TUI framework | Bubble Tea, with Lip Gloss for styling and Bubbles for text input. Pinned to the **v1 line** (bubbletea v1.3.x, lipgloss v1.x, bubbles v1.0.x); the v2 tags are still pre-release |
| Git access | Shell out via `os/exec`, parse machine-readable output (`for-each-ref`, `status --porcelain`, `rev-list --count`). No Git library — this is how lazygit works and is the fastest path. Every call takes a `context.Context`, so a hung `fetch` is cancellable from the UI |
| State | Elm-style `Model`/`Update`/`View`. Git calls run as async `Cmd`s so the UI never blocks; results return as messages |
| Persistence | JSON under `<.git>/drift/` (found via `git rev-parse --absolute-git-dir`) — `config.json` (targets, unmergeable globs, allowed declare destinations) and `state.json` (tickets). Inside `.git` makes it per-repo and unversioned for free. `config.json` is always hand-editable and Drift never rewrites one that exists, but hand-editing is not the *only* way in: a first-run wizard seeds targets from real refs (roadmap area 4), and the placeholder is the fallback for when it's declined or unavailable |
| Config resolution | **Two roots, split by scope.** `<.git>/drift/` is per-repo, local-only, and holds `config.json` (found through a **search path**, entry zero today — a committed team-wide config later is a new entry, not a migration). `~/.config/drift/` is the **user-global** root, XDG-respecting, and holds `prefs.json` — the **selection style** is its first inhabitant (area 16a), **theming** its second (16b: `accent` and `background`), and keymaps (area 12) ride on the root rather than building it. A one-string setting is a better first load for a new root than a whole keymap format, and 16b is what proves it: theming added two fields beside `selection` rather than a second file |
| Why prefs is its own file | The user-global root holds a **differently-named file**, not a second `config.json` on the search path. A user-global `config.json` would be a file that could plausibly hold `targets`, which is meaningless outside a repo; one file with one purpose cannot make that offer. For the same reason `prefs.json` has **no search path of its own**: a preference is a person's, so a second root would be a repo or a machine overriding a choice that was never theirs to make. Drift never *writes* it — `config.json` has a placeholder because a repo cannot work unconfigured, while a machine with no `prefs.json` simply has the defaults. A file that exists and names something Drift doesn't recognize is an **error naming the file**, the same rule as `declare.destinations`: a preference that quietly didn't apply is indistinguishable on screen from one that did. `DRIFT_BAND`, `DRIFT_ACCENT` and `DRIFT_BG` are documented single-run overrides above the file — "for this run" is a thing an edited file cannot say, and it is exactly what deciding a colour needs, since you would otherwise be editing the file whose contents you are trying to decide. The converse is 16b's: `DRIFT_BG` also needed a *file*, because a terminal Lip Gloss misdetects is misdetected every run |
| Keybindings | **Named actions are the contract; keys are a rebindable default.** Every screen dispatches on a named action, never a key literal, so a user-global `~/.config/drift/keymap.json` can override any binding as a pure additive layer. The named-action dispatch is adopted in the dashboard (area 3) from day one, so customization (area 12) is never a retrofit. Full keymap lives in `DESIGN.md` |
| Grouping | **Manual pairing.** Ticket ID substring-matches candidate branches to pre-filter; the user confirms and assigns targets. Branch naming is inconsistent, so target is **never** parsed from the branch name. Optional pattern-based *pre-assignment* for teams with rigid conventions is deferred (roadmap area 10) and would still never be silent |
| Invocation | Run from inside the repo, lazygit-style |
| Backend | None. The core is fully local and offline; Jira and GitLab are deferred, optional lookup sources only |
| Build target | macOS primary; Linux/Windows fine (Bubble Tea is cross-platform, so not a constraint) |
| Testing | **Real throwaway repos** (`t.TempDir()` + `git init`), never mocks — mocking git would only prove the parser matches our idea of git's output. The suite is **hermetic**: `TestMain` sets `GIT_CONFIG_NOSYSTEM=1` in every package that shells out to git, because the system gitconfig differs by machine — Apple ships one setting `init.defaultBranch=main` and CI's git has no equivalent, which was enough to make seven tests pass on a Mac and fail on CI for a reason that had nothing to do with the code. Everything a test depends on is **declared, not inherited**: `user.name`, `user.email` and the initial branch are written per repo by the helpers, and `--initial-branch` is passed even to a `--bare` init, since that sets the bare HEAD and a HEAD naming a branch we never push leaves `git clone` with nothing to check out. Reproduce a CI-shaped run locally with `GIT_CONFIG_NOSYSTEM=1 go test ./...` |
| CI | **`go test ./...` on every push and pull request** (`.github/workflows/ci.yml`). The release workflow's own test step stays where it is and is not a duplicate: it guards the *tag*, which is a different claim from "this commit is good", and the tag is the one that cannot be taken back. Before the suite was hermetic this job would only have moved the surprise earlier — a green local run and a red CI run were both honest — so the `GIT_CONFIG_NOSYSTEM` fix in the Testing row is what makes a green push job mean something on every machine. Cutting v0.3.0 is what surfaced the gap: the break rode `master` for two commits and announced itself by burning the tag |
| Distribution | **GoReleaser, driven by a tag.** `git push --tags` builds darwin/linux × amd64/arm64, cuts the GitHub release, and pushes an updated cask to `Sknoww/homebrew-tap`; `brew install Sknoww/tap/drift` is the install path. A **cask, not a formula** — the tap ships the built binary rather than building from source, and a `postflight` strips the quarantine attribute macOS puts on a downloaded binary. `main.version` is stamped via ldflags, so `drift -version` reports the release on every install path. Two things guard the tag, which is the point of no return: the tap token is checked *before* anything is published (GoReleaser cuts the release before it pushes the cask, so a missing token would otherwise fail after the release exists, leaving the tag burned and the tap stale), and `go test ./...` runs before GoReleaser. Exercise the whole pipeline without publishing via `goreleaser release --snapshot --clean`. No `zap` stanza: `~/.config/drift/prefs.json` would be the honest target, but the per-repo `<.git>/drift/` must never be zapped — it lives inside the user's own repositories. **Existing formula installs (≤ v0.2.0) do not upgrade themselves.** `tap_migrations.json` maps `drift → sknoww/tap`, but Homebrew's migration runs off the *deleted-formula diff* of a single `brew update` (`cmd/update_report/reporter.rb`), so it fires once and only for a machine that pulls the retiring commit in that run. A machine already past it keeps an orphaned keg, and `brew upgrade drift` then reads the formula cached inside that keg and reports it up to date — silently, indefinitely. The way out is `brew uninstall --formula --force drift && brew install --cask Sknoww/tap/drift` |

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

// LocalOnly — annotation for a path held back from commits. Git's own flags decide
// *whether* a path is held (skip-worktree bit for tracked, info/exclude for untracked);
// this records only the human context, reconciled against Git on load so it can never
// contradict reality. Kind (tracked/untracked) is derived at read time, never stored.
type LocalOnly struct {
    Path string // repo-relative
    Note string // why it's held, e.g. "debug log level" — optional
}
type Store struct {
    Tickets   []Ticket
    LocalOnly []LocalOnly // flat, repo-global — never tied to a ticket
}

// Prefs — the user-global half. ~/.config/drift/prefs.json, hand-edited, and
// absent on most machines. Every field is optional and the zero value is the
// default set, so a user who has never heard of the file loses nothing.
type Prefs struct {
    Selection string // "pair" | "contrast" | "accent" | "marker"; "" is unset
}
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
