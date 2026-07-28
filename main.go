// Command drift is a terminal UI that organizes Git work by ticket.
//
// It loads the per-repo config and state, then hands off to the Bubble Tea
// dashboard (internal/ui). An unconfigured repo is not an error: on first run
// drift offers a setup wizard that seeds config.json from the repo's own remote
// refs (roadmap area 4). When the wizard can't or shouldn't run — declined, a
// non-interactive run, or no remotes to offer — it falls back to pointing the
// user at the config file to hand-edit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/Sknoww/drift/internal/git"
	"github.com/Sknoww/drift/internal/store"
	"github.com/Sknoww/drift/internal/ui"
)

// version is the release stamp, set at build time with
// -ldflags "-X main.version=<tag>" — which is how the Homebrew formula builds
// it. Left alone, buildVersion falls back to what the toolchain recorded.
var version = "dev"

// buildVersion reports the release this binary came from, however it was built.
// The three install paths stamp a version in three different places, and only
// one of them is ldflags:
//
//   - Homebrew passes -ldflags, so version is already the release. A packager
//     naming the version explicitly is the most authoritative answer, and wins.
//   - `go install ...@v0.1.2` reaches no ldflags, but the toolchain records the
//     module version in the build info. This is the case the fallback exists
//     for — without it the install path the README advertises reported "dev".
//   - `go build` inside a checkout records a VCS-derived version instead, so a
//     working-tree build reports something like "0.1.1+dirty" — which is more
//     honest than a bare "dev", since it says which release it is ahead of.
//
// "dev" survives only when there is neither a stamp nor VCS info to derive one
// from, such as an unpacked source tarball built without ldflags.
//
// The leading "v" is trimmed so every path prints the same shape: ldflags carry
// Homebrew's bare "0.1.2", build info carries "v0.1.2".
func buildVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Main.Version == "" || bi.Main.Version == "(devel)" {
		return version
	}
	return strings.TrimPrefix(bi.Main.Version, "v")
}

func main() {
	// -version is the one thing drift does without a terminal or a repo: the
	// packaging story (Homebrew's `test do` block, `brew upgrade` diffing an
	// installed build) needs a way to ask which build this is that doesn't
	// open the full-screen dashboard.
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("drift", buildVersion())
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "drift:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	repo := git.New(".")

	cfg, paths, err := store.LoadConfig(ctx, repo)
	if errors.Is(err, store.ErrPlaceholderConfig) {
		// Unconfigured: the wizard is the front door, the placeholder file the
		// fallback. A wizard that runs to completion leaves cfg configured and
		// falls through to the dashboard; anything else prints where to edit.
		configured, err := firstRun(ctx, repo, paths, &cfg)
		if err != nil {
			return err
		}
		if !configured {
			fmt.Println("drift is not configured for this repo yet.")
			fmt.Println("edit its config, then run drift again:")
			fmt.Println("  " + paths.Config)
			return nil
		}
	} else if err != nil {
		// A config that exists but is broken is never overwritten — the wizard
		// stays out of it, and the user sees the parse/validation error.
		return err
	}

	state, _, err := store.LoadState(ctx, repo)
	if err != nil {
		return err
	}

	prog := tea.NewProgram(ui.New(repo, cfg, state), tea.WithAltScreen())
	_, err = prog.Run()
	return err
}

// firstRun offers the setup wizard for an unconfigured repo. It reports whether
// the repo ended up configured, writing the chosen config and updating *cfg when
// so. It declines quietly — leaving *cfg untouched and the placeholder in place
// — when the run is non-interactive or the repo has no remote refs to offer, so
// the caller can fall back to the hand-edit path.
func firstRun(ctx context.Context, repo *git.Repo, paths store.Paths, cfg *store.Config) (bool, error) {
	if !interactive() {
		return false, nil
	}

	refs, err := repo.RemoteBranches(ctx)
	if err != nil {
		return false, err
	}
	if len(refs) == 0 {
		return false, nil
	}

	targets, ok, err := ui.RunWizard(repo, refs)
	if err != nil || !ok {
		return false, err
	}

	chosen := store.Config{Targets: targets}
	if err := store.SaveConfig(ctx, repo, chosen); err != nil {
		return false, err
	}
	*cfg = chosen
	return true, nil
}

// interactive reports whether both stdin and stdout are a terminal, the
// precondition for a full-screen wizard. A piped or redirected run takes the
// placeholder fallback instead of stalling on input that will never come.
func interactive() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}
