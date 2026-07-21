// Command drift is a terminal UI that organizes Git work by ticket.
//
// It loads the per-repo config and state, then hands off to the Bubble Tea
// dashboard (internal/ui). An unconfigured repo is not an error: drift points
// the user at the config file to edit and exits, since a first-run wizard
// (roadmap area 4) is not built yet.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

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
		// Not a failure: there is nothing to show yet, and the user needs the
		// path more than they need an error.
		fmt.Println("drift is not configured for this repo yet.")
		fmt.Println("edit its config, then run drift again:")
		fmt.Println("  " + paths.Config)
		return nil
	}
	if err != nil {
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
