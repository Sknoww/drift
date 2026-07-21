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
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"drift/internal/git"
	"drift/internal/store"
	"drift/internal/ui"
)

func main() {
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
