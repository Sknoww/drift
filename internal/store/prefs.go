package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Preferences — the user-global half of Drift's configuration (roadmap area 16).
//
// Everything else Drift persists belongs to a repository and lives under
// <git-dir>/drift/. A preference does not: the selection style is a property of
// the person reading the screen, and putting it beside `targets` would mean
// re-declaring it in every repo they clone. This is the second root CONTEXT.md
// declared from the start, and it is purely additive — nothing under
// <git-dir>/drift/ moves or changes shape.
//
// It is a separate *file* rather than a second entry on ConfigSearchPath. A
// user-global config.json would be a file that could plausibly hold `targets`,
// which is meaningless outside a repo; one file with one purpose cannot make
// that offer. For the same reason there is no search path here: a preference is
// a person's, so a second root would be a repo or a machine overriding a choice
// that was never theirs to make.

// Selection style names. These are the vocabulary prefs.json is written in, so
// they are public and persistent — the rendering behind each one may be tuned,
// but a name that has shipped is a name someone has in a file.
//
// The treatments themselves live in internal/ui (band.go); the names live here
// because this is the layer that parses and validates the file. A test in the
// ui package asserts the two sets agree, so a treatment can never be added
// there without becoming selectable here, or removed while a name still
// promises it.
const (
	SelectionPair     = "pair"     // a subtle band under a left-edge marker — the default
	SelectionContrast = "contrast" // a grey band that actually reads, no marker
	SelectionAccent   = "accent"   // an accent hue rather than a lighter grey
	SelectionMarker   = "marker"   // left-edge marker only, no background at all
)

// SelectionNames lists the valid selection styles, default first. It is the
// order the README documents them in, and the order an error message offers
// them back.
func SelectionNames() []string {
	return []string{SelectionPair, SelectionContrast, SelectionAccent, SelectionMarker}
}

// Prefs is the user-global preferences file: ~/.config/drift/prefs.json,
// hand-edited, and absent on most machines.
//
// Every field is optional and an absent file is the whole default set, so a
// user who has never heard of this file loses nothing. Theming (roadmap area
// 16b) adds fields beside Selection rather than a second file.
type Prefs struct {
	// Selection names the selected-row treatment (SelectionPair and friends).
	// Empty means unset, which is the default — not an error.
	Selection string `json:"selection,omitempty"`
}

// UserConfigDir is Drift's user-global root: $XDG_CONFIG_HOME/drift, or
// ~/.config/drift when that is unset.
//
// The XDG rule is applied by hand rather than through os.UserConfigDir because
// the two disagree on Drift's primary platform: on macOS os.UserConfigDir
// returns ~/Library/Application Support, and a terminal tool's config belongs
// with the other terminal tools' config, in ~/.config, where a user editing it
// expects to find it. Windows has no ~/.config convention to honour, so it
// keeps the platform answer.
func UserConfigDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); filepath.IsAbs(dir) {
		return filepath.Join(dir, "drift"), nil
	}
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "drift"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "drift"), nil
}

// PrefsPath is the preferences file Drift reads, whether or not it exists.
func PrefsPath() (string, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "prefs.json"), nil
}

// LoadPrefs reads the user-global preferences.
//
// No file is not an error — it is the default set, which is what a machine that
// has never been given a preference has. Drift never *writes* this file: unlike
// config.json, whose placeholder exists because a repo cannot work
// unconfigured, prefs.json is fully optional, and seeding one on every machine's
// first run would leave a file behind for a user who wanted nothing.
//
// A file that exists and is wrong is a different matter, and is reported rather
// than worked around. An unknown selection name is a typo, and the same rule
// applies as to declare.destinations: silently falling back would render the
// default treatment, which is indistinguishable on screen from the requested one
// working. The error names the file and offers the valid values back.
func LoadPrefs() (Prefs, error) {
	path, err := PrefsPath()
	if err != nil {
		return Prefs{}, err
	}
	return loadPrefsAt(path)
}

func loadPrefsAt(path string) (Prefs, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("read %s: %w", path, err)
	}

	var p Prefs
	if err := json.Unmarshal(data, &p); err != nil {
		return Prefs{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := p.validate(); err != nil {
		return Prefs{}, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// validate rejects a preference that would otherwise be applied as its own
// absence. Empty is unset and always valid; anything else must name a treatment
// that exists.
func (p Prefs) validate() error {
	if strings.TrimSpace(p.Selection) == "" {
		return nil
	}
	for _, name := range SelectionNames() {
		if p.Selection == name {
			return nil
		}
	}
	return fmt.Errorf("unknown selection %q (want %s)", p.Selection, quotedList(SelectionNames()))
}

// quotedList renders names as `"a", "b", "c"` — the offer half of an error
// about an unknown one, so the fix never needs the README.
func quotedList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}
