package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These drive real repos and read the written files back through git itself
// where it matters: what proves a declaration worked is check-attr agreeing.

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestTopLevelFromSubdirectory(t *testing.T) {
	dir := newRepo(t)
	sub := filepath.Join(dir, "deep", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := New(sub).TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// macOS hands out /var symlinks for temp dirs; compare resolved paths.
	want, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != want {
		t.Errorf("TopLevel() = %q, want %q", gotResolved, want)
	}
}

func TestAttrPathPerDestination(t *testing.T) {
	dir := newRepo(t)
	r := New(dir)

	repoPath, err := r.AttrPath(context.Background(), AttrRepo)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(repoPath) != ".gitattributes" {
		t.Errorf("AttrRepo path = %q, want a .gitattributes", repoPath)
	}

	localPath, err := r.AttrPath(context.Background(), AttrLocal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(localPath, filepath.Join("info", "attributes")) {
		t.Errorf("AttrLocal path = %q, want <git-dir>/info/attributes", localPath)
	}
}

// The declaration is only worth anything if git agrees, so this asserts through
// CheckAttrMerge rather than by reading our own file back.
func TestDeclareUnmergeableTeachesGit(t *testing.T) {
	for _, dest := range []AttrDest{AttrRepo, AttrLocal} {
		t.Run(dest.Label(), func(t *testing.T) {
			dir := newRepo(t)
			r := New(dir)
			ctx := context.Background()

			flagged, err := r.CheckAttrMerge(ctx, []string{"workflows/a.uwe"})
			if err != nil {
				t.Fatal(err)
			}
			if flagged["workflows/a.uwe"] {
				t.Fatal("file was already unmergeable before declaring")
			}

			decl, err := r.DeclareUnmergeable(ctx, dest, "workflows/**/*.uwe")
			if err != nil {
				t.Fatal(err)
			}
			if decl.Already {
				t.Error("first declaration reported as already present")
			}

			flagged, err = r.CheckAttrMerge(ctx, []string{"workflows/a.uwe"})
			if err != nil {
				t.Fatal(err)
			}
			if !flagged["workflows/a.uwe"] {
				t.Errorf("git does not report the file unmergeable after declaring:\n%s", readFile(t, decl.Path))
			}
		})
	}
}

func TestDeclareUnmergeableIsIdempotent(t *testing.T) {
	dir := newRepo(t)
	r := New(dir)
	ctx := context.Background()

	first, err := r.DeclareUnmergeable(ctx, AttrLocal, "*.uwe")
	if err != nil {
		t.Fatal(err)
	}
	after := readFile(t, first.Path)

	second, err := r.DeclareUnmergeable(ctx, AttrLocal, "*.uwe")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Already {
		t.Error("re-declaring the same pattern should report Already")
	}
	if now := readFile(t, second.Path); now != after {
		t.Errorf("re-declaring rewrote the file:\nbefore:\n%s\nafter:\n%s", after, now)
	}
}

func TestDeclareUnmergeablePreservesExistingRules(t *testing.T) {
	dir := newRepo(t)
	path := filepath.Join(dir, ".gitattributes")
	if err := os.WriteFile(path, []byte("*.png binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := New(dir).DeclareUnmergeable(context.Background(), AttrRepo, "*.uwe"); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	if !strings.HasPrefix(got, "*.png binary\n") {
		t.Errorf("existing rule was disturbed:\n%s", got)
	}
	if !strings.Contains(got, "*.uwe -merge") {
		t.Errorf("declaration missing:\n%s", got)
	}
}

// --- the fenced block, tested on the pure function --------------------------

func TestInsertDeclarationOpensAFencedBlock(t *testing.T) {
	got, already := insertDeclaration("*.png binary\n", "*.uwe")
	if already {
		t.Fatal("nothing was declared yet")
	}
	want := "*.png binary\n" +
		"\n" +
		fenceHeader + "\n" +
		"*.uwe -merge\n" +
		fenceEnd + "\n"
	if got != want {
		t.Errorf("insertDeclaration() =\n%q\nwant\n%q", got, want)
	}
}

func TestInsertDeclarationJoinsTheExistingBlock(t *testing.T) {
	start := fenceHeader + "\n*.uwe -merge\n" + fenceEnd + "\ntrailing rule\n"
	got, _ := insertDeclaration(start, "*.pbxproj")

	want := fenceHeader + "\n*.uwe -merge\n*.pbxproj -merge\n" + fenceEnd + "\ntrailing rule\n"
	if got != want {
		t.Errorf("second declaration did not join the block:\n%q\nwant\n%q", got, want)
	}
}

// A begin with no end is hand-mangled and not ours to repair; a complete new
// block at the end is additive and never corrupts what is there.
func TestInsertDeclarationIgnoresAHalfFence(t *testing.T) {
	got, _ := insertDeclaration(fenceBegin+"\n*.uwe -merge\n", "*.pbxproj")
	if strings.Count(got, fenceEnd) != 1 {
		t.Errorf("want exactly one closing fence:\n%s", got)
	}
	if !strings.HasPrefix(got, fenceBegin+"\n*.uwe -merge\n") {
		t.Errorf("existing content disturbed:\n%s", got)
	}
}

func TestInsertDeclarationSeesAnExistingRuleAnywhere(t *testing.T) {
	// Declared by hand, outside any Drift block — already true is already true.
	if _, already := insertDeclaration("# team rules\n*.uwe -merge\n", "*.uwe"); !already {
		t.Error("a hand-written declaration should count as already declared")
	}
	// A different attribute on the same pattern leaves merge alone.
	if _, already := insertDeclaration("*.uwe -diff\n", "*.uwe"); already {
		t.Error("-diff on the pattern is not a -merge declaration")
	}
	// A comment that happens to contain the rule is not a rule.
	if _, already := insertDeclaration("# *.uwe -merge (todo)\n", "*.uwe"); already {
		t.Error("a commented-out rule should not count")
	}
}

func TestInsertDeclarationQuotesAPatternWithSpaces(t *testing.T) {
	got, _ := insertDeclaration("", "Assets/My Scenes/**")
	if !strings.Contains(got, `"Assets/My Scenes/**" -merge`) {
		t.Errorf("pattern with a space was not quoted:\n%s", got)
	}
	// And a quoted rule is recognised on the way back in.
	if _, already := insertDeclaration(got, "Assets/My Scenes/**"); !already {
		t.Error("quoted declaration not recognised as already present")
	}
}

func TestDeclareUnmergeableRejectsAnEmptyPattern(t *testing.T) {
	dir := newRepo(t)
	if _, err := New(dir).DeclareUnmergeable(context.Background(), AttrLocal, "  "); err == nil {
		t.Error("an empty pattern must not be written")
	}
}
