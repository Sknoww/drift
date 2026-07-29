package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The user-global root is the one thing here that is not under a git dir, so
// these drive it through XDG_CONFIG_HOME rather than through a real home
// directory. That is also the documented way in, so testing it tests what a
// user can actually do.

func prefsHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "drift", "prefs.json")
}

func writePrefs(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The common case by a mile: no file at all. It is the full default set, and
// nothing about it is an error — a user who has never heard of prefs.json must
// lose nothing by not having one.
func TestLoadPrefsWithNoFile(t *testing.T) {
	prefsHome(t)

	p, err := LoadPrefs()
	if err != nil {
		t.Fatalf("LoadPrefs with no file: %v", err)
	}
	if p.Selection != "" {
		t.Errorf("Selection = %q, want empty (unset)", p.Selection)
	}
}

// And Drift never creates one. config.json has a placeholder because a repo
// cannot work unconfigured; prefs.json is optional, so seeding one would leave
// a file behind on the machine of a user who wanted nothing.
func TestLoadPrefsWritesNothing(t *testing.T) {
	path := prefsHome(t)

	if _, err := LoadPrefs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("LoadPrefs created %s — it must never write", filepath.Dir(path))
	}
}

func TestLoadPrefsReadsTheSelection(t *testing.T) {
	path := prefsHome(t)
	writePrefs(t, path, `{"selection": "marker"}`)

	p, err := LoadPrefs()
	if err != nil {
		t.Fatalf("LoadPrefs: %v", err)
	}
	if p.Selection != SelectionMarker {
		t.Errorf("Selection = %q, want %q", p.Selection, SelectionMarker)
	}
}

// The decision this file exists to enforce: a typo is reported, never applied
// as its own absence. Falling back silently would render the default treatment,
// which on screen is indistinguishable from the requested one working.
func TestLoadPrefsRejectsAnUnknownSelection(t *testing.T) {
	path := prefsHome(t)
	writePrefs(t, path, `{"selection": "makrer"}`)

	_, err := LoadPrefs()
	if err == nil {
		t.Fatal("an unknown selection was accepted")
	}
	msg := err.Error()
	if !strings.Contains(msg, path) {
		t.Errorf("error must name the file to edit, got %q", msg)
	}
	if !strings.Contains(msg, `"makrer"`) {
		t.Errorf("error must quote what was written, got %q", msg)
	}
	// And offer the valid names back, so the fix never needs the README.
	for _, name := range SelectionNames() {
		if !strings.Contains(msg, `"`+name+`"`) {
			t.Errorf("error does not offer %q: %q", name, msg)
		}
	}
}

// An empty value is unset, not a name that happens to be blank.
func TestLoadPrefsAcceptsAnEmptySelection(t *testing.T) {
	path := prefsHome(t)
	writePrefs(t, path, `{"selection": ""}`)

	if _, err := LoadPrefs(); err != nil {
		t.Errorf("an empty selection is unset, not an error: %v", err)
	}
}

func TestLoadPrefsReportsBrokenJSON(t *testing.T) {
	path := prefsHome(t)
	writePrefs(t, path, `{"selection": `)

	_, err := LoadPrefs()
	if err == nil {
		t.Fatal("unparseable prefs.json was accepted")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error must name the file, got %q", err)
	}
}

// Every name Drift offers is one it accepts. The band treatments behind them
// live in internal/ui, and a test there asserts the two sets agree.
func TestEverySelectionNameValidates(t *testing.T) {
	for _, name := range SelectionNames() {
		if err := (Prefs{Selection: name}).validate(); err != nil {
			t.Errorf("%s is offered but rejected: %v", name, err)
		}
	}
	if SelectionNames()[0] != SelectionPair {
		t.Errorf("the default must be listed first, got %q", SelectionNames()[0])
	}
}

// XDG_CONFIG_HOME wins where it is set, on every platform — that is what makes
// the rest of these tests possible, and what lets a user put the file where
// their other tools' config lives.
func TestUserConfigDirHonoursXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got, err := UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "drift"); got != want {
		t.Errorf("UserConfigDir = %q, want %q", got, want)
	}
}

// A relative XDG_CONFIG_HOME is not a config home. The spec says to ignore a
// non-absolute value, and honouring one would resolve Drift's preferences
// against whatever directory the user happened to run from.
func TestUserConfigDirIgnoresARelativeXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "relative/path")

	got, err := UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "relative") {
		t.Errorf("UserConfigDir = %q, want the home fallback", got)
	}
}

// The reason the XDG rule is applied by hand rather than through
// os.UserConfigDir: on macOS — Drift's primary platform — that returns
// ~/Library/Application Support, and a terminal tool's config belongs in
// ~/.config with every other terminal tool's config.
func TestUserConfigDirFallsBackToDotConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows keeps the platform answer; there is no ~/.config convention to honour")
	}
	t.Setenv("XDG_CONFIG_HOME", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory to resolve against")
	}
	got, err := UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "drift"); got != want {
		t.Errorf("UserConfigDir = %q, want %q", got, want)
	}
}
