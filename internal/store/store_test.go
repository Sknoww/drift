package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the real filesystem against throwaway directories, and the
// real git binary where the git dir itself is what's under test.

// fakeRepo stands in for *git.Repo, so tests that only care about JSON on disk
// don't need a repository. TestResolveUsesGitDir covers the real wiring.
type fakeRepo struct {
	dir string
	err error
}

func (f fakeRepo) GitDir(context.Context) (string, error) { return f.dir, f.err }

// driftDir returns a fake repo whose drift directory is a fresh temp dir.
func driftDir(t *testing.T) (fakeRepo, string) {
	t.Helper()
	gitDir := t.TempDir()
	return fakeRepo{dir: gitDir}, filepath.Join(gitDir, "drift")
}

func writeConfig(t *testing.T, dir string, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goodConfig = `{
  "targets": [
    {"key": "r2perf", "ref": "origin/release-to-performance"},
    {"key": "main", "ref": "origin/main"}
  ],
  "unmergeable": [
    {"name": "workflows", "globs": ["workflows/**/*.uwe"]}
  ]
}`

func TestSaveConfigRoundTrips(t *testing.T) {
	// The wizard's write path: SaveConfig lays down real targets, and a
	// subsequent LoadConfig must read them back as a usable config — not a
	// placeholder, and passing validation.
	repo, _ := driftDir(t)
	ctx := context.Background()

	cfg := Config{Targets: []Target{
		{Key: "main", Ref: "origin/main"},
		{Key: "perf", Ref: "origin/release-perf"},
	}}
	if err := SaveConfig(ctx, repo, cfg); err != nil {
		t.Fatalf("SaveConfig() = %v", err)
	}

	got, _, err := LoadConfig(ctx, repo)
	if err != nil {
		t.Fatalf("LoadConfig() after SaveConfig = %v, want a clean load", err)
	}
	if len(got.Targets) != 2 || got.Targets[0] != cfg.Targets[0] || got.Targets[1] != cfg.Targets[1] {
		t.Errorf("round-tripped targets = %+v, want %+v", got.Targets, cfg.Targets)
	}
}

func TestSaveConfigRejectsInvalid(t *testing.T) {
	// SaveConfig validates before writing, so a bad target set is reported
	// rather than persisted into a config the dashboard can't use. It must also
	// leave no file behind for LoadConfig to later trip over.
	ctx := context.Background()
	cases := []struct {
		name string
		cfg  Config
	}{
		{"no targets", Config{}},
		{"empty key", Config{Targets: []Target{{Key: "", Ref: "origin/main"}}}},
		{"empty ref", Config{Targets: []Target{{Key: "main", Ref: ""}}}},
		{"duplicate keys", Config{Targets: []Target{
			{Key: "main", Ref: "origin/main"},
			{Key: "main", Ref: "upstream/main"},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, dir := driftDir(t)
			if err := SaveConfig(ctx, repo, tc.cfg); err == nil {
				t.Fatal("SaveConfig() = nil error, want a validation error")
			}
			if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
				t.Errorf("SaveConfig() wrote a file for an invalid config; stat err = %v", err)
			}
		})
	}
}

func TestLoadConfigFirstRunWritesPlaceholder(t *testing.T) {
	repo, dir := driftDir(t)
	ctx := context.Background()

	cfg, paths, err := LoadConfig(ctx, repo)

	// First run is not a failure, but it is not a usable config either: the
	// caller must be told to send the user to the file.
	if !errors.Is(err, ErrPlaceholderConfig) {
		t.Fatalf("LoadConfig() on first run = %v, want ErrPlaceholderConfig", err)
	}
	if want := filepath.Join(dir, "config.json"); paths.Config != want {
		t.Errorf("paths.Config = %q, want %q", paths.Config, want)
	}
	if !strings.Contains(err.Error(), paths.Config) {
		t.Errorf("error %q should name the path to edit", err)
	}
	if _, err := os.Stat(paths.Config); err != nil {
		t.Fatalf("first run did not create config.json: %v", err)
	}
	if !isPlaceholder(cfg) {
		t.Error("LoadConfig() on first run returned a config that is not marked as a placeholder")
	}

	// The file on disk must round-trip: the user edits this, so it has to parse.
	data, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk Config
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("the placeholder Drift wrote does not parse: %v", err)
	}
	if len(onDisk.Targets) == 0 || len(onDisk.Unmergeable) == 0 {
		t.Error("placeholder should show an example of each list, to document the shape")
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("placeholder should end in a newline")
	}
}

func TestPlaceholderIsRecognizedAfterRewrite(t *testing.T) {
	// A user who opens the file, changes nothing, and re-runs must not be told
	// they are configured.
	repo, dir := driftDir(t)
	ctx := context.Background()

	if _, _, err := LoadConfig(ctx, repo); !errors.Is(err, ErrPlaceholderConfig) {
		t.Fatalf("first run = %v, want ErrPlaceholderConfig", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = LoadConfig(ctx, repo)
	if !errors.Is(err, ErrPlaceholderConfig) {
		t.Fatalf("second run on an unedited config = %v, want ErrPlaceholderConfig", err)
	}

	after, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("re-running rewrote config.json; it must never clobber the user's file")
	}
}

func TestPlaceholderDetectionIsPerValue(t *testing.T) {
	// Half-edited counts as unedited: a surviving mark would otherwise become a
	// target pointing at a ref that cannot resolve.
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{
			name: "fully seeded",
			cfg:  placeholderConfig(),
			want: true,
		},
		{
			name: "target key edited, ref left behind",
			cfg: Config{Targets: []Target{
				{Key: "r2perf", Ref: "EDIT ME - ref to compare against"},
			}},
			want: true,
		},
		{
			name: "targets done, unmergeable glob left behind",
			cfg: Config{
				Targets:     []Target{{Key: "r2perf", Ref: "origin/r2perf"}},
				Unmergeable: []Unmergeable{{Name: "workflows", Globs: []string{"EDIT ME - e.g. x"}}},
			},
			want: true,
		},
		{
			name: "lowercase edit me still counts",
			cfg:  Config{Targets: []Target{{Key: "edit me", Ref: "origin/main"}}},
			want: true,
		},
		{
			name: "fully edited",
			cfg: Config{
				Targets:     []Target{{Key: "r2perf", Ref: "origin/release-to-performance"}},
				Unmergeable: []Unmergeable{{Name: "workflows", Globs: []string{"workflows/**/*.uwe"}}},
			},
			want: false,
		},
		{
			name: "deliberately empty is not a placeholder",
			cfg:  Config{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlaceholder(tt.cfg); got != tt.want {
				t.Errorf("isPlaceholder() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	repo, dir := driftDir(t)
	writeConfig(t, dir, goodConfig)

	cfg, paths, err := LoadConfig(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "config.json"); paths.Config != want {
		t.Errorf("paths.Config = %q, want %q", paths.Config, want)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(cfg.Targets))
	}
	if cfg.Targets[0] != (Target{Key: "r2perf", Ref: "origin/release-to-performance"}) {
		t.Errorf("targets[0] = %+v", cfg.Targets[0])
	}
	if len(cfg.Unmergeable) != 1 || cfg.Unmergeable[0].Globs[0] != "workflows/**/*.uwe" {
		t.Errorf("unmergeable = %+v", cfg.Unmergeable)
	}
}

func TestLoadConfigAnyNumberOfTargets(t *testing.T) {
	// Three is the author's situation, not a design assumption. Nothing may
	// assume a count, so prove the extremes load.
	for _, n := range []int{1, 2, 3, 7, 25} {
		repo, dir := driftDir(t)
		targets := make([]Target, n)
		for i := range targets {
			targets[i] = Target{
				Key: "t" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
				Ref: "origin/branch-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			}
		}
		body, err := json.Marshal(Config{Targets: targets})
		if err != nil {
			t.Fatal(err)
		}
		writeConfig(t, dir, string(body))

		cfg, _, err := LoadConfig(context.Background(), repo)
		if err != nil {
			t.Fatalf("%d targets: %v", n, err)
		}
		if len(cfg.Targets) != n {
			t.Errorf("got %d targets, want %d", len(cfg.Targets), n)
		}
	}
}

func TestLoadConfigRejectsBadConfigs(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "malformed json",
			body:    `{"targets": [`,
			wantErr: "parse",
		},
		{
			name:    "no targets",
			body:    `{"targets": [], "unmergeable": []}`,
			wantErr: "no targets",
		},
		{
			name:    "duplicate target keys",
			body:    `{"targets": [{"key": "main", "ref": "origin/main"}, {"key": "main", "ref": "origin/other"}]}`,
			wantErr: "duplicate target key",
		},
		{
			name:    "empty target key",
			body:    `{"targets": [{"key": "", "ref": "origin/main"}]}`,
			wantErr: "empty key",
		},
		{
			name:    "empty target ref",
			body:    `{"targets": [{"key": "main", "ref": "  "}]}`,
			wantErr: "empty ref",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, dir := driftDir(t)
			writeConfig(t, dir, tt.body)

			_, paths, err := LoadConfig(context.Background(), repo)
			if err == nil {
				t.Fatalf("LoadConfig() with %s = nil error, want an error", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should mention %q", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), paths.Config) {
				t.Errorf("error %q should name the offending file", err)
			}
			if errors.Is(err, ErrPlaceholderConfig) {
				t.Error("a broken config is not an unconfigured one; the user must see the real error")
			}
		})
	}
}

func TestLoadConfigDoesNotOverwriteABrokenConfig(t *testing.T) {
	// Hand-edited file: a syntax error must be reported, never "fixed" by
	// replacing the user's work with a placeholder.
	repo, dir := driftDir(t)
	broken := `{"targets": [ THIS IS NOT JSON`
	writeConfig(t, dir, broken)

	if _, _, err := LoadConfig(context.Background(), repo); err == nil {
		t.Fatal("LoadConfig() on a broken config = nil error, want an error")
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != broken {
		t.Errorf("config.json was rewritten to %q; the user's file must survive", data)
	}
}

func TestConfigTarget(t *testing.T) {
	cfg := Config{Targets: []Target{
		{Key: "r2perf", Ref: "origin/release-to-performance"},
		{Key: "main", Ref: "origin/main"},
	}}

	got, ok := cfg.Target("main")
	if !ok {
		t.Fatal("Target(main) not found")
	}
	if got.Ref != "origin/main" {
		t.Errorf("Target(main).Ref = %q, want %q", got.Ref, "origin/main")
	}
	if _, ok := cfg.Target("nope"); ok {
		t.Error("Target(nope) = found, want not found")
	}
	if _, ok := cfg.Target("MAIN"); ok {
		t.Error("Target() should match keys exactly; they are identifiers, not search terms")
	}
}

func TestConfigSearchPath(t *testing.T) {
	// One entry today. The order is the contract: entry zero is what first run
	// writes and what wins when a later entry is added.
	got := ConfigSearchPath("/repo/.git/drift")
	if len(got) != 1 {
		t.Fatalf("ConfigSearchPath() = %v, want exactly one entry today", got)
	}
	if want := filepath.Join("/repo/.git/drift", "config.json"); got[0] != want {
		t.Errorf("ConfigSearchPath()[0] = %q, want %q", got[0], want)
	}
}

func TestStateRoundTrip(t *testing.T) {
	repo, dir := driftDir(t)
	ctx := context.Background()

	want := Store{Tickets: []Ticket{
		{
			ID:    "ABC-123",
			Title: "Fix the login flow",
			Branches: []TicketBranch{
				{Branch: "ABC-123-fix-login", TargetKey: "main"},
				{Branch: "feature/abc-123/r2perf", TargetKey: "r2perf"},
			},
		},
		{
			ID:       "XYZ-9",
			Branches: []TicketBranch{{Branch: "xyz-9", TargetKey: "main"}},
		},
	}}

	if err := SaveState(ctx, repo, want); err != nil {
		t.Fatal(err)
	}
	got, paths, err := LoadState(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "state.json"); paths.State != want {
		t.Errorf("paths.State = %q, want %q", paths.State, want)
	}

	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("LoadState() = %s, want %s", gotJSON, wantJSON)
	}

	// A ticket with no title must not grow an empty one on the way through.
	data, err := os.ReadFile(paths.State)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"title": ""`) {
		t.Error("state.json wrote an empty title; Title is optional and omitempty")
	}
}

func TestLoadStateMissingFileIsEmptyNotAnError(t *testing.T) {
	repo, _ := driftDir(t)

	got, _, err := LoadState(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadState() with no state.json = %v, want no error", err)
	}
	if len(got.Tickets) != 0 {
		t.Errorf("LoadState() with no state.json = %+v, want an empty Store", got)
	}
}

func TestLoadStateMalformed(t *testing.T) {
	repo, dir := driftDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"tickets": `), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := LoadState(context.Background(), repo)
	if err == nil {
		t.Fatal("LoadState() on malformed state.json = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "state.json") {
		t.Errorf("error %q should name the file", err)
	}
}

func TestSaveStateCreatesDriftDir(t *testing.T) {
	// First save happens before any directory exists.
	repo, dir := driftDir(t)
	if err := SaveState(context.Background(), repo, Store{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("SaveState() did not create %s: %v", dir, err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", dir)
	}
}

func TestSaveStateEmptyWritesEmptyList(t *testing.T) {
	// null tickets would be a valid decode but an ugly file to hand-inspect.
	repo, dir := driftDir(t)
	if err := SaveState(context.Background(), repo, Store{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("state.json = %s, want an empty list rather than null", data)
	}
}

func TestSaveStateOverwritesCleanly(t *testing.T) {
	// Shrinking the store must not leave a longer previous file behind.
	repo, dir := driftDir(t)
	ctx := context.Background()

	big := Store{Tickets: []Ticket{
		{ID: "ABC-1", Branches: []TicketBranch{{Branch: "abc-1", TargetKey: "main"}}},
		{ID: "ABC-2", Branches: []TicketBranch{{Branch: "abc-2", TargetKey: "main"}}},
	}}
	if err := SaveState(ctx, repo, big); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(ctx, repo, Store{Tickets: []Ticket{{ID: "ABC-1"}}}); err != nil {
		t.Fatal(err)
	}

	got, _, err := LoadState(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tickets) != 1 || got.Tickets[0].ID != "ABC-1" {
		t.Errorf("LoadState() after shrinking = %+v, want just ABC-1", got.Tickets)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("SaveState() left a temp file behind: %s", e.Name())
		}
	}
}

func TestStoreTicket(t *testing.T) {
	s := Store{Tickets: []Ticket{{ID: "ABC-123"}, {ID: "XYZ-9"}}}

	got, ok := s.Ticket("XYZ-9")
	if !ok || got.ID != "XYZ-9" {
		t.Errorf("Ticket(XYZ-9) = %+v, %t", got, ok)
	}
	if _, ok := s.Ticket("nope"); ok {
		t.Error("Ticket(nope) = found, want not found")
	}
}

func TestResolvePropagatesGitError(t *testing.T) {
	// Run outside a repo, GitDir fails; store must surface that, not invent a path.
	repo := fakeRepo{err: errors.New("not a git repository")}

	if _, err := Resolve(context.Background(), repo); err == nil {
		t.Error("Resolve() with a failing GitDir = nil error, want an error")
	}
	if _, _, err := LoadConfig(context.Background(), repo); err == nil {
		t.Error("LoadConfig() outside a repo = nil error, want an error")
	}
	if _, _, err := LoadState(context.Background(), repo); err == nil {
		t.Error("LoadState() outside a repo = nil error, want an error")
	}
	if err := SaveState(context.Background(), repo, Store{}); err == nil {
		t.Error("SaveState() outside a repo = nil error, want an error")
	}
}
