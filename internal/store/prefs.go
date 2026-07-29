package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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

// Background names. Which end of the adaptive palette is in force is normally
// detected from the terminal, and these force it.
//
// This began as DRIFT_BG alone, an instrument for the one part of the adaptive
// palette with a silent failure mode: if detection decides the terminal is dark
// when it is not, every Light value is inert and the result is
// indistinguishable from Light values that were simply chosen badly. An env var
// says "for this run", which is the right thing for diagnosing that — and the
// wrong thing for a terminal that is misdetected *every* run. A permanent
// misdetection is a permanent setting, so it gets a home here beside the
// preferences it distorts.
const (
	BackgroundLight = "light"
	BackgroundDark  = "dark"
)

// BackgroundNames lists the valid background values, in the order an error
// offers them back. There is no default entry: unset means "detect it", which
// is not a value anyone writes.
func BackgroundNames() []string {
	return []string{BackgroundLight, BackgroundDark}
}

// accentFormats is the offer half of an error about an accent Drift cannot
// render, and the sentence the README documents the field with.
//
// Both depths are accepted on purpose. ANSI-256 is right for Drift's *own*
// palette, which has to be legible on a terminal Drift knows nothing about
// (DESIGN.md §1) — but a user picking their own accent has it in front of them,
// and the value they have in hand is a hex code out of their terminal theme,
// not an xterm-256 index. Lip Gloss degrades a hex colour to the nearest
// indexed one on a 256-colour profile, so accepting hex costs nothing and asks
// nothing of the user that the ANSI-only rule would.
const accentFormats = `an ANSI-256 index "0"-"255", or a hex colour like "#ff8800"`

var hexAccent = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// ParseAccent canonicalises an accent value, reporting whether it is one Drift
// can render.
//
// It is the single rule behind both entry points — the file, which this package
// validates, and DRIFT_ACCENT, which internal/ui resolves — so a value that is
// good in one is good in the other. Canonicalising rather than merely accepting
// matters because the resolved value is *reported*: the title names the accent
// actually in force while an override is set, and a title reading `accent:007`
// when `7` is what got rendered would be the same class of lie the declared
// badge exists to prevent.
func ParseAccent(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if hexAccent.MatchString(v) {
		return strings.ToLower(v), true
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 255 {
		return strconv.Itoa(n), true
	}
	return "", false
}

// ParseBackground reports whether v names an end of the adaptive palette. The
// same single rule serves the file and DRIFT_BG.
func ParseBackground(v string) (string, bool) {
	switch strings.TrimSpace(v) {
	case BackgroundLight:
		return BackgroundLight, true
	case BackgroundDark:
		return BackgroundDark, true
	}
	return "", false
}

// Prefs is the user-global preferences file: ~/.config/drift/prefs.json,
// hand-edited, and absent on most machines.
//
// Every field is optional and an absent file is the whole default set, so a
// user who has never heard of this file loses nothing. Theming (roadmap area
// 16b) added fields beside Selection rather than a second file, which is what
// the root existing already bought.
type Prefs struct {
	// Selection names the selected-row treatment (SelectionPair and friends).
	// Empty means unset, which is the default — not an error.
	Selection string `json:"selection,omitempty"`

	// Accent is the one themable colour role (roadmap area 16b): the title, the
	// checked-out branch marker, and the selected row's left-edge marker. Those
	// three move together because they mean one thing — "Drift is pointing at
	// this" — so recolouring is one field rather than three.
	//
	// The alarm roles are deliberately *not* themable. Colour is the signal
	// (DESIGN.md §1): `behind` is the one thing on screen that shouts,
	// `unmergeable` is a distinct alarm beside it, and neutral recedes. A theme
	// that let two of those collide would not be a preference, it would be a
	// broken screen — and validating distinctness across arbitrary colours means
	// a perceptual-distance threshold that either rejects good choices or admits
	// broken ones. The accent carries no alarm, so it needs no such check.
	//
	// One value, used for both ends of the palette. Drift's *own* default is an
	// adaptive pair, because Drift is choosing on behalf of a terminal it has
	// never seen; a user is choosing for the terminal in front of them and can
	// see the result immediately, so asking them for a light end they will never
	// look at buys precision nobody wants. Empty means the adaptive default.
	Accent string `json:"accent,omitempty"`

	// Background forces which end of the adaptive palette is used, for a
	// terminal Lip Gloss misdetects. Empty means detect it, which is right on
	// every terminal that reports honestly.
	Background string `json:"background,omitempty"`
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
// absence. Empty is unset and always valid on every field; anything else must
// name something Drift can actually render.
//
// The rule is the same for all three, and it is the declare.destinations rule:
// a value that quietly did not apply is indistinguishable on screen from one
// that did. A mistyped selection renders the default treatment, a mistyped
// accent renders the default blue, and a mistyped background renders whatever
// detection was going to render anyway — in each case the screen looks like it
// worked. So the file refuses to start, and names both the file and the values
// it would have taken.
func (p Prefs) validate() error {
	if v := strings.TrimSpace(p.Selection); v != "" {
		if !validSelection(p.Selection) {
			return fmt.Errorf("unknown selection %q (want %s)", p.Selection, quotedList(SelectionNames()))
		}
	}
	if v := strings.TrimSpace(p.Accent); v != "" {
		if _, ok := ParseAccent(p.Accent); !ok {
			return fmt.Errorf("unknown accent %q (want %s)", p.Accent, accentFormats)
		}
	}
	if v := strings.TrimSpace(p.Background); v != "" {
		if _, ok := ParseBackground(p.Background); !ok {
			return fmt.Errorf("unknown background %q (want %s)", p.Background, quotedList(BackgroundNames()))
		}
	}
	return nil
}

func validSelection(v string) bool {
	for _, name := range SelectionNames() {
		if v == name {
			return true
		}
	}
	return false
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
