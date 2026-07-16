// Command drift is a terminal UI that organizes Git work by ticket.
//
// The Bubble Tea dashboard lands in roadmap area 3. Until then main is a smoke
// check over the layers below it: run it inside a repo to see the git wrapper
// and the store working.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"drift/internal/git"
	"drift/internal/store"
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

	branch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return err
	}
	if branch == "" {
		branch = "(detached)"
	}
	dirty, err := repo.IsDirty(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("on %s (dirty: %t)\n", branch, dirty)
	fmt.Printf("config: %s\n", paths.Config)

	fmt.Printf("%d target(s):\n", len(cfg.Targets))
	for _, t := range cfg.Targets {
		fmt.Printf("  %s -> %s\n", t.Key, t.Ref)
	}

	fmt.Printf("%d tracked ticket(s):\n", len(state.Tickets))
	for _, t := range state.Tickets {
		fmt.Printf("  %s %s\n", t.ID, t.Title)
		for _, b := range t.Branches {
			fmt.Printf("    %s -> %s\n", b.Branch, b.TargetKey)
		}
	}
	return nil
}
