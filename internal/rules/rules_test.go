package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func labels(files []File) []string {
	out := make([]string, len(files))
	for i, file := range files {
		out[i] = file.Label
	}
	return out
}

func TestFindReportsAnExistingProjectFile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "AGENTS.md"), "be nice\n")

	files := Find([]string{"AGENTS.md"}, root, root, "")

	if len(files) != 1 {
		t.Fatalf("files = %v, want one", labels(files))
	}
	file := files[0]
	if file.Label != "AGENTS.md" || !file.Exists || file.Scope != Project {
		t.Fatalf("file = %+v, want an existing project AGENTS.md", file)
	}
	if file.Size != int64(len("be nice\n")) {
		t.Fatalf("size = %d, want the file's own", file.Size)
	}
}

// Agents read the whole chain from the working directory up, so a session
// started a level down is governed by the rules above it too.
func TestFindWalksUpToTheRepositoryRootNearestFirst(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "AGENTS.md"), "repo\n")
	sub := filepath.Join(root, "packages", "api")
	write(t, filepath.Join(sub, "AGENTS.md"), "package\n")

	files := Find([]string{"AGENTS.md"}, sub, root, "")

	want := []string{filepath.Join("packages", "api", "AGENTS.md"), "AGENTS.md"}
	if got := labels(files); !equal(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

// The walk is bounded by the repository for the same reason project settings'
// is: above it sits a shared parent that is not the user's project.
func TestFindStopsAtTheRepositoryRoot(t *testing.T) {
	parent := t.TempDir()
	write(t, filepath.Join(parent, "AGENTS.md"), "someone else's\n")
	root := filepath.Join(parent, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	files := Find([]string{"AGENTS.md"}, root, root, "")

	if len(files) != 1 || files[0].Exists {
		t.Fatalf("files = %+v, want only the missing candidate inside the repo", files)
	}
	if dir := filepath.Dir(files[0].Path); resolve(dir) != resolve(root) {
		t.Fatalf("candidate in %s, want it inside the repository", dir)
	}
}

// "this project tells the agent nothing" is the answer a reader came for, and
// the row they then create the file from.
func TestFindOffersTheRepositoryRootWhenNothingExists(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "cmd")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	files := Find([]string{"CLAUDE.md"}, sub, root, "")

	if len(files) != 1 {
		t.Fatalf("files = %v, want the one candidate", labels(files))
	}
	if files[0].Exists {
		t.Fatal("candidate reported as existing")
	}
	if want := filepath.Join(resolve(root), "CLAUDE.md"); files[0].Path != want {
		t.Fatalf("path = %s, want %s", files[0].Path, want)
	}
}

func TestFindExpandsAGlobalSpecAgainstHome(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "mine\n")
	root := t.TempDir()

	files := Find([]string{"~/.claude/CLAUDE.md"}, root, root, home)

	if len(files) != 1 {
		t.Fatalf("files = %v, want one", labels(files))
	}
	file := files[0]
	if file.Scope != Global || !file.Exists {
		t.Fatalf("file = %+v, want an existing global file", file)
	}
	if want := filepath.Join("~", ".claude", "CLAUDE.md"); file.Label != want {
		t.Fatalf("label = %q, want %q", file.Label, want)
	}
}

// A global file that has not been written is still listed: it is where the
// answer goes, and the reader is one key from creating it.
func TestFindListsAMissingGlobalFile(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()

	files := Find([]string{"~/.codex/AGENTS.md"}, root, root, home)

	if len(files) != 1 || files[0].Exists {
		t.Fatalf("files = %+v, want the missing global candidate", files)
	}
	if want := filepath.Join(home, ".codex", "AGENTS.md"); files[0].Path != want {
		t.Fatalf("path = %s, want %s", files[0].Path, want)
	}
}

// Two CLIs reading the same AGENTS.md is the normal case, and the list is
// read as "what governs this directory", not as one entry per tool.
func TestFindDedupesRepeatedSpecs(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "AGENTS.md"), "shared\n")

	files := Find([]string{"AGENTS.md", "AGENTS.md"}, root, root, "")

	if len(files) != 1 {
		t.Fatalf("files = %v, want the file once", labels(files))
	}
}

// A spec is configuration, and one that climbs out of the checkout would
// reach exactly the shared parent the walk refuses to read.
func TestFindDropsSpecsThatEscapeTheProject(t *testing.T) {
	parent := t.TempDir()
	write(t, filepath.Join(parent, "AGENTS.md"), "someone else's\n")
	root := filepath.Join(parent, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	if files := Find([]string{"../AGENTS.md"}, root, root, ""); len(files) != 0 {
		t.Fatalf("files = %+v, want the escaping spec dropped", files)
	}
}

func TestReadTruncatesPastTheCap(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "AGENTS.md"), strings.Repeat("x", MaxRead+100))

	text, truncated, err := Read(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !truncated {
		t.Fatal("a file past the cap did not report the cut")
	}
	if len(text) != MaxRead {
		t.Fatalf("read %d bytes, want the cap of %d", len(text), MaxRead)
	}
}

func TestReadReturnsAnEmptyFileAsEmptyText(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "AGENTS.md"), "")

	text, truncated, err := Read(path)
	if err != nil || truncated || text != "" {
		t.Fatalf("read = %q, %v, %v; want empty text and no error", text, truncated, err)
	}
}

// The directory a global rules file lives in is routinely the part missing,
// and an editor pointed inside one that does not exist fails on save.
func TestCreateMakesTheParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "CLAUDE.md")

	if err := Create(path); err != nil {
		t.Fatalf("create: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("created file holds %q, want it empty", body)
	}
}

// Whatever the manager wrote into a rules file would become a rule the agent
// then obeys, so an existing file is never touched.
func TestCreateLeavesAnExistingFileAlone(t *testing.T) {
	path := write(t, filepath.Join(t.TempDir(), "AGENTS.md"), "already here\n")

	if err := Create(path); err != nil {
		t.Fatalf("create: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "already here\n" {
		t.Fatalf("file = %q, want it untouched", body)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
