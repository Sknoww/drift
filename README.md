# drift
A ticket based git TUI

## Sandbox

`scripts/sandbox.sh` builds a throwaway repo that exercises everything drift does, so a
change can be tried against realistic state instead of against your own work.

```sh
scripts/sandbox.sh                                  # build it (also builds the binary)
cd ~/dev/repos/drift-sandbox/repo && ../drift       # run it
```

It creates a bare `origin`, two targets (`main`, and `r2perf` → `origin/release-to-performance`)
and four feature branches, each covering a case the dashboard has to get right:

| Branch | ↓↑ vs target | What it covers |
|---|---|---|
| `ABC-101-main` | ↓4 ↑2 | 2 unmergeable — one committed, one **working-tree only** |
| `ABC-101-r2perf` | ↓3 ↑2 | 2 unmergeable, one per detection half (config glob / `.gitattributes`) |
| `ABC-202-main` | ↓4 ↑1 | behind, but everything it touched merges — **no marker** |
| `ABC-303-main` | ↓0 ↑0 | in sync, so detection is skipped entirely |

It leaves you on `ABC-101-main` with an uncommitted edit, which is what makes that
branch's second collision appear — commit or stash it and the count drops to 1.
`.gitattributes` declares `*.pbxproj -merge`, so git's own half of the hybrid rule is
live from the start, and the config allow-lists `"local"` as the only declare
destination.

Re-run it any time to reset; it is idempotent, and it refuses to wipe a directory that
is not already one of its sandboxes. Pass a path to build it somewhere else, or
`--wizard` to leave the repo unconfigured so the first-run wizard opens instead of the
dashboard.
