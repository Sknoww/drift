// Command drift is a terminal UI that organizes Git work by ticket.
//
// The Bubble Tea dashboard lands in roadmap area 3. Until then main is a smoke
// check over the git layer: run it inside a repo to see the wrapper working.
package main

import (
	"context"
	"fmt"
	"os"

	"drift/internal/git"
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

	branches, err := repo.LocalBranches(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("on %s (dirty: %t)\n", branch, dirty)
	fmt.Printf("%d local branch(es):\n", len(branches))
	for _, b := range branches {
		fmt.Println("  " + b)
	}
	return nil
}
