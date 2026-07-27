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

	"github.com/bmatcuk/doublestar/v4"
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

// Destination names for Declare.Destinations — the two attributes files Drift
// can write a `-merge` declaration into (CONTEXT.md §Unmergeable).
const (
	DestShared = "shared" // .gitattributes at the repo root — committed, team-wide
	DestLocal  = "local"  // $GIT_DIR/info/attributes — local, unversioned
)

// Declare constrains where the declare flow may write a `-merge` attribute.
//
// Omit the whole key and both destinations are offered. A team that does not
// keep a committed .gitattributes lists only "local", and the shared
// destination stops being offered at all — so it can never be picked by
// accident, and Drift can never dirty a file the team does not use.
//
// It lives in config.json, hand-edited, rather than behind a keypress: a guard
// against an unwanted commit is worth more when it cannot be toggled off by a
// stray keystroke.
type Declare struct {
	// Destinations allow-lists destination names, in the order they are offered
	// — so this also reorders the picker, not just filters it.
	Destinations []string `json:"destinations"`
}

// Config is the hand-edited half: target mains, unmergeable file classes, and
// where declarations may be written.
//
// The number of targets is per-repo and unbounded. Nothing here or downstream
// may assume a count.
type Config struct {
	Targets     []Target      `json:"targets"`
	Unmergeable []Unmergeable `json:"unmergeable"`

	// A pointer so an absent key stays absent from a config Drift writes: the
	// first-run wizard must not seed an opinion the user never expressed.
	Declare *Declare `json:"declare,omitempty"`
}

// DeclareDestinations is the allow-listed destination names, or nil when the
// config does not constrain them — nil means "offer everything", which is not
// the same as an empty list (rejected by validate as almost certainly a
// mistake, since it would leave nowhere to write).
func (c Config) DeclareDestinations() []string {
	if c.Declare == nil {
		return nil
	}
	return c.Declare.Destinations
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

// LocalOnly annotates a path held back from commits (roadmap area 6). Git's own
// flags decide *whether* a path is held — the skip-worktree bit for a tracked
// file, $GIT_DIR/info/exclude for an untracked one — and this records only the
// human context, so it can never contradict reality. Its kind (tracked vs.
// untracked) is derived from Git at read time, never stored, so a file that
// crosses that line cannot leave a stale label behind.
type LocalOnly struct {
	Path string `json:"path"`           // repo-relative
	Note string `json:"note,omitempty"` // why it's held, e.g. "debug log level"
}

// Store is the tool-written half: every tracked ticket, and the notes on
// whatever is held locally.
type Store struct {
	Tickets []Ticket `json:"tickets"`

	// Flat and repo-global, with no ticket association — matching the global
	// nature of the hold, which is an index/ignore flag and so applies to every
	// branch the user checks out.
	LocalOnly []LocalOnly `json:"localOnly,omitempty"`
}

// LocalOnlyNote is the note recorded for a held path, or "" when there is none.
func (s Store) LocalOnlyNote(path string) string {
	for _, l := range s.LocalOnly {
		if l.Path == path {
			return l.Note
		}
	}
	return ""
}

// SetLocalOnlyNote records the note for a path and returns the updated store. An
// empty note drops the annotation outright rather than persisting a blank one:
// with the note the only thing stored, a pathless-purpose entry is just noise.
//
// The slice is copied before it is changed, never written through — Store is
// passed by value all over the UI, and an in-place edit would reach every copy.
func (s Store) SetLocalOnlyNote(path, note string) Store {
	note = strings.TrimSpace(note)

	out := make([]LocalOnly, 0, len(s.LocalOnly)+1)
	found := false
	for _, l := range s.LocalOnly {
		if l.Path != path {
			out = append(out, l)
			continue
		}
		found = true
		if note != "" {
			l.Note = note
			out = append(out, l)
		}
	}
	if !found && note != "" {
		out = append(out, LocalOnly{Path: path, Note: note})
	}

	s.LocalOnly = out
	return s
}

// PruneLocalOnly drops annotations for paths Git no longer reports as held, and
// says whether it dropped any. It is what keeps the store from contradicting
// Git: a path released outside Drift simply stops appearing, and its orphaned
// note goes with it on the next load rather than lingering as a claim about a
// hold that no longer exists.
func (s Store) PruneLocalOnly(held []string) (Store, bool) {
	isHeld := make(map[string]bool, len(held))
	for _, p := range held {
		isHeld[p] = true
	}

	kept := make([]LocalOnly, 0, len(s.LocalOnly))
	for _, l := range s.LocalOnly {
		if isHeld[l.Path] {
			kept = append(kept, l)
		}
	}
	if len(kept) == len(s.LocalOnly) {
		return s, false
	}
	s.LocalOnly = kept
	return s, true
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
	return c.validateDeclare()
}

// validateDeclare rejects a declare block that would quietly do the opposite of
// what it says. A typo'd destination name must never be skipped over: silently
// ignoring it would leave the shared .gitattributes on offer for someone who
// wrote the key precisely to get rid of it.
func (c Config) validateDeclare() error {
	if c.Declare == nil {
		return nil
	}
	if len(c.Declare.Destinations) == 0 {
		return fmt.Errorf("declare.destinations is empty — remove the %q key to offer both", "declare")
	}
	seen := make(map[string]bool, len(c.Declare.Destinations))
	for _, name := range c.Declare.Destinations {
		if name != DestShared && name != DestLocal {
			return fmt.Errorf("unknown declare destination %q (want %q or %q)", name, DestShared, DestLocal)
		}
		if seen[name] {
			return fmt.Errorf("duplicate declare destination %q", name)
		}
		seen[name] = true
	}
	return nil
}

// UnmergeableMatch is one configured glob that matched a path, tagged with the
// class it belongs to. The class name is what makes a match explainable in the
// UI: "workflows/**/*.uwe (workflows)" says why the file was flagged.
type UnmergeableMatch struct {
	Name string // the Unmergeable class the glob belongs to
	Glob string // the pattern that matched
}

// UnmergeableMatches returns every configured glob matching a repo-relative
// path (git's own forward-slash form), in config order. This is the config half
// of the hybrid detection rule (CONTEXT.md §Unmergeable); what `git check-attr
// merge` reports is the additive other half, resolved in the caller. `**` spans
// path segments, so `workflows/**/*.uwe` covers the file at any depth under the
// directory.
//
// The matches are also what the declare flow offers as writable patterns
// (area 5 part 2): promoting a config glob to a `-merge` attribute declares the
// whole class at once, where the file's own path declares just the one file.
//
// A malformed glob is skipped, never fatal: one bad pattern in config must not
// blind detection to every other class.
func (c Config) UnmergeableMatches(path string) []UnmergeableMatch {
	var got []UnmergeableMatch
	for _, u := range c.Unmergeable {
		for _, g := range u.Globs {
			if ok, err := doublestar.Match(g, path); err == nil && ok {
				got = append(got, UnmergeableMatch{Name: u.Name, Glob: g})
			}
		}
	}
	return got
}

// MatchesUnmergeable reports whether any configured glob matches the path — the
// predicate half of UnmergeableMatches, which is what detection needs.
func (c Config) MatchesUnmergeable(path string) bool {
	return len(c.UnmergeableMatches(path)) > 0
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

// SaveConfig writes cfg to the first entry on the search path, the same file
// LoadConfig seeds a placeholder into. It is the first-run wizard's write path
// (roadmap area 4): the wizard builds real targets from real refs, so Drift
// writes the config the user would otherwise hand-edit.
//
// It validates before writing, so a bad set of targets (none, empty fields,
// duplicate keys) is reported rather than persisted into a broken config. It
// does not itself guard against overwriting a good config — callers reach it
// only after LoadConfig has reported the repo unconfigured.
func SaveConfig(ctx context.Context, r gitDirer, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	paths, err := Resolve(ctx, r)
	if err != nil {
		return err
	}
	return writeJSON(paths.Config, cfg)
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
