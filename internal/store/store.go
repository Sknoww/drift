// Package store holds Drift's persisted data and the JSON files behind it.
//
// Two files live side by side under <git-dir>/drift/: config.json, which the
// user hand-edits, and state.json, which Drift writes. Inside the git directory
// they are per-repo and unversioned for free.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Target is one long-lived main branch that feature branches aim at.
type Target struct {
	Key string `json:"key"` // short UI label, e.g. "r2perf"
	Ref string `json:"ref"` // git ref for comparison, e.g. "origin/release-to-performance"
}

// Unmergeable is a class of files Git must never attempt to merge. Additive to
// what `git check-attr merge` reports, and able to override it.
type Unmergeable struct {
	Name  string   `json:"name"`  // UI label for the class, e.g. "workflows"
	Globs []string `json:"globs"` // path patterns, e.g. "workflows/**/*.uwe"
}

// Config is the hand-edited half: target mains and unmergeable file classes.
//
// The number of targets is per-repo and unbounded. Nothing here or downstream
// may assume a count.
type Config struct {
	Targets     []Target      `json:"targets"`
	Unmergeable []Unmergeable `json:"unmergeable"`
}

// TicketBranch pairs one local branch with the target it aims at. The pairing
// is always made by the user — target is never parsed from the branch name.
type TicketBranch struct {
	Branch    string `json:"branch"`    // full local branch name, any naming style
	TargetKey string `json:"targetKey"` // references Target.Key
}

// Ticket is one unit of work and its fan-out across targets.
type Ticket struct {
	ID       string         `json:"id"`
	Title    string         `json:"title,omitempty"` // optional; a later Jira lookup could fill this
	Branches []TicketBranch `json:"branches"`
}

// Store is the tool-written half: every tracked ticket.
type Store struct {
	Tickets []Ticket `json:"tickets"`
}

// placeholderMark tags every value in a freshly written config.json. It is how
// both Drift and the user tell "never configured" from "deliberately empty".
const placeholderMark = "EDIT ME"

// gitDirer is the slice of the git layer this package needs. Taking an
// interface keeps store testable without a real repo.
type gitDirer interface {
	GitDir(ctx context.Context) (string, error)
}

// Paths locates Drift's files for one repository.
type Paths struct {
	Dir    string // <git-dir>/drift
	Config string // the config.json that Load resolved to, or would create
	State  string // <git-dir>/drift/state.json
}

// Resolve finds Drift's directory for the repo that r points at. It does not
// create anything.
func Resolve(ctx context.Context, r gitDirer) (Paths, error) {
	gitDir, err := r.GitDir(ctx)
	if err != nil {
		return Paths{}, err
	}
	return pathsIn(filepath.Join(gitDir, "drift")), nil
}

func pathsIn(dir string) Paths {
	return Paths{
		Dir:    dir,
		Config: ConfigSearchPath(dir)[0],
		State:  filepath.Join(dir, "state.json"),
	}
}

// ConfigSearchPath lists the locations a config may live in, highest
// precedence first. Today it holds exactly one entry: the local, unversioned
// <git-dir>/drift/config.json.
//
// It is a list because a committed team-wide config is a plausible later
// addition, and expressing the lookup as a search path now means adding one is
// purely additive — a new entry here, no migration. LoadConfig takes the first
// entry that exists; first run writes to entry zero.
func ConfigSearchPath(dir string) []string {
	return []string{filepath.Join(dir, "config.json")}
}

// ErrPlaceholderConfig reports that config.json exists but still holds the
// values Drift seeded it with, so the repo is not configured yet. Callers
// should point the user at Paths.Config rather than proceed. Test for it with
// errors.Is.
var ErrPlaceholderConfig = errors.New("config.json has not been edited yet")

// LoadConfig reads the first config.json on the search path.
//
// On first run — no config anywhere on the path — it creates the directory,
// writes a placeholder to the first entry, and returns ErrPlaceholderConfig
// wrapping the path it wrote. It returns the same error, without rewriting, if
// the config still holds placeholder values. Paths.Config always names the file
// the user should open, whether it was found or just created.
func LoadConfig(ctx context.Context, r gitDirer) (Config, Paths, error) {
	paths, err := Resolve(ctx, r)
	if err != nil {
		return Config{}, Paths{}, err
	}
	return loadConfigIn(paths.Dir)
}

func loadConfigIn(dir string) (Config, Paths, error) {
	paths := pathsIn(dir)

	for _, candidate := range ConfigSearchPath(dir) {
		data, err := os.ReadFile(candidate)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Config{}, paths, fmt.Errorf("read %s: %w", candidate, err)
		}

		paths.Config = candidate
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, paths, fmt.Errorf("parse %s: %w", candidate, err)
		}
		if isPlaceholder(cfg) {
			return cfg, paths, fmt.Errorf("%s: %w", candidate, ErrPlaceholderConfig)
		}
		if err := cfg.validate(); err != nil {
			return cfg, paths, fmt.Errorf("%s: %w", candidate, err)
		}
		return cfg, paths, nil
	}

	// Nothing on the path: seed the first entry so the user has a file to edit.
	if err := writePlaceholder(paths.Config); err != nil {
		return Config{}, paths, err
	}
	return placeholderConfig(), paths, fmt.Errorf("%s: %w", paths.Config, ErrPlaceholderConfig)
}

// placeholderConfig is the seed config.json. Every value is marked, and each
// one documents its own field, so the file teaches its shape without a schema.
//
// One example entry per list is a sample of the shape, not a statement about
// length — a JSON array reads as variable-length on its own. Nothing may imply
// an expected number of targets.
func placeholderConfig() Config {
	return Config{
		Targets: []Target{{
			Key: placeholderMark + " - short label, e.g. r2perf",
			Ref: placeholderMark + " - ref to compare against, e.g. origin/release-to-performance",
		}},
		Unmergeable: []Unmergeable{{
			Name:  placeholderMark + " - label for the class, e.g. workflows",
			Globs: []string{placeholderMark + " - e.g. workflows/**/*.uwe"},
		}},
	}
}

// isPlaceholder reports whether cfg still carries seeded values anywhere. It is
// deliberately generous: one surviving mark means the user has not finished, and
// half-edited config would otherwise produce targets that match no real ref.
func isPlaceholder(cfg Config) bool {
	for _, t := range cfg.Targets {
		if marked(t.Key) || marked(t.Ref) {
			return true
		}
	}
	for _, u := range cfg.Unmergeable {
		if marked(u.Name) {
			return true
		}
		for _, g := range u.Globs {
			if marked(g) {
				return true
			}
		}
	}
	return false
}

func marked(s string) bool {
	return strings.Contains(strings.ToUpper(s), placeholderMark)
}

// validate catches the config mistakes that would otherwise surface later as
// confusing dashboard state rather than as an error about config.json.
func (c Config) validate() error {
	if len(c.Targets) == 0 {
		return errors.New("no targets configured")
	}
	seen := make(map[string]bool, len(c.Targets))
	for _, t := range c.Targets {
		switch {
		case strings.TrimSpace(t.Key) == "":
			return errors.New("a target has an empty key")
		case strings.TrimSpace(t.Ref) == "":
			return fmt.Errorf("target %q has an empty ref", t.Key)
		case seen[t.Key]:
			// Keys are how tickets reference targets, so duplicates would make
			// a branch's pairing ambiguous.
			return fmt.Errorf("duplicate target key %q", t.Key)
		}
		seen[t.Key] = true
	}
	return nil
}

// Target returns the target with the given key.
func (c Config) Target(key string) (Target, bool) {
	for _, t := range c.Targets {
		if t.Key == key {
			return t, true
		}
	}
	return Target{}, false
}

// LoadState reads state.json. A repo with no state file yet is not an error —
// it is an empty Store, which is exactly what a repo tracking no tickets has.
func LoadState(ctx context.Context, r gitDirer) (Store, Paths, error) {
	paths, err := Resolve(ctx, r)
	if err != nil {
		return Store{}, Paths{}, err
	}
	s, err := loadStateAt(paths.State)
	return s, paths, err
}

func loadStateAt(path string) (Store, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Store{}, nil
	}
	if err != nil {
		return Store{}, fmt.Errorf("read %s: %w", path, err)
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return Store{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// SaveState writes state.json, creating Drift's directory if needed.
func SaveState(ctx context.Context, r gitDirer, s Store) error {
	paths, err := Resolve(ctx, r)
	if err != nil {
		return err
	}
	return saveStateAt(paths.State, s)
}

func saveStateAt(path string, s Store) error {
	if s.Tickets == nil {
		s.Tickets = []Ticket{}
	}
	return writeJSON(path, s)
}

// Ticket returns the tracked ticket with the given ID.
func (s Store) Ticket(id string) (Ticket, bool) {
	for _, t := range s.Tickets {
		if t.ID == id {
			return t, true
		}
	}
	return Ticket{}, false
}

func writePlaceholder(path string) error {
	return writeJSON(path, placeholderConfig())
}

// writeJSON writes v as indented JSON, atomically. The rename keeps a crash or
// a full disk from truncating good state into an unparseable half-file.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
