package ui

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// captureGitUI swaps both seams G goes through: PATH answers for lazygit
// only when installed is true, and the launch is recorded instead of taking
// the terminal. The recorded value is the repository lazygit would run in.
func captureGitUI(t *testing.T, installed bool) *string {
	t.Helper()
	var root string
	prevLook, prevExec := lookPath, execGitUI
	lookPath = func(name string) (string, error) {
		if name == gitUI && installed {
			return "/usr/bin/" + gitUI, nil
		}
		return "", errors.New("not found")
	}
	execGitUI = func(dir string) tea.Cmd {
		root = dir
		return func() tea.Msg { return nil }
	}
	t.Cleanup(func() { lookPath, execGitUI = prevLook, prevExec })
	return &root
}

func pressGitKey(t *testing.T, m *Model) tea.Cmd {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	*m = *updated.(*Model)
	return cmd
}

func TestGitKeyOpensLazygitOnTheRowsRepository(t *testing.T) {
	m := buildModel(t)
	root := captureGitUI(t, true)
	repo := seedRepo(t)
	createSession(t, m, "agent", repo, "")
	m.selectSessionRow(t, "agent")

	if cmd := pressGitKey(t, m); cmd == nil {
		t.Fatalf("G issued no command, err = %q", m.errBar.text)
	}
	if *root != resolved(t, repo) {
		t.Fatalf("lazygit opened in %q, want %q", *root, resolved(t, repo))
	}
	if m.errBar.text != "" {
		t.Fatalf("a working launch should report nothing, got %q", m.errBar.text)
	}
}

// A session in a worktree is working on its own branch, so that is the
// checkout lazygit has to open: the repository it branched from would show
// none of what the agent has done.
func TestGitKeyOpensTheSessionsWorktree(t *testing.T) {
	m := buildModel(t)
	root := captureGitUI(t, true)
	repo := seedRepo(t)
	sess := createWorktreeSession(t, m, "wt", repo)
	m.selectSessionRow(t, "wt")

	pressGitKey(t, m)

	if want := resolved(t, sess.Cwd); *root != want {
		t.Fatalf("lazygit opened in %q, want the worktree %q", *root, want)
	}
}

// A group row carries a path but no pane, and its repository is worth
// reading too.
func TestGitKeyOpensAGroupsDefaultPath(t *testing.T) {
	m := buildModel(t)
	root := captureGitUI(t, true)
	repo := seedRepo(t)
	if err := m.store.CreateGroup("backend", repo); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	pressGitKey(t, m)

	// git answers with the toplevel it resolved, which on macOS is the target
	// of the /var symlink the temp directory sits behind.
	if want := resolved(t, repo); *root != want {
		t.Fatalf("lazygit opened in %q, want %q", *root, want)
	}
}

func TestGitKeyWithoutLazygitNamesIt(t *testing.T) {
	m := buildModel(t)
	root := captureGitUI(t, false)
	repo := seedRepo(t)
	createSession(t, m, "agent", repo, "")
	m.selectSessionRow(t, "agent")

	if cmd := pressGitKey(t, m); cmd != nil {
		t.Fatalf("nothing should launch without %s, got %T", gitUI, cmd())
	}
	if *root != "" {
		t.Fatalf("lazygit ran from %q while missing from PATH", *root)
	}
	if !strings.Contains(m.errBar.text, gitUI) {
		t.Fatalf("status line should name %s, got %q", gitUI, m.errBar.text)
	}
}

// Outside a repository there is nothing to show, and the reason has to reach
// the status bar: lazygit started there would exit on its own error and take
// the screen for as long as it took to print it.
func TestGitKeyOutsideARepositoryExplainsItself(t *testing.T) {
	m := buildModel(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := captureGitUI(t, true)
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")

	if cmd := pressGitKey(t, m); cmd != nil {
		t.Fatalf("a directory outside a repository should launch nothing, got %T", cmd())
	}
	if *root != "" {
		t.Fatalf("lazygit ran in %q, which is not a repository", *root)
	}
	if !strings.Contains(m.errBar.text, "repository") {
		t.Fatalf("status line should say why, got %q", m.errBar.text)
	}
}

// A lazygit that failed to start, or exited badly, is the only account of
// what went wrong, so it lands on the status bar rather than being swallowed.
func TestGitUIDoneReportsAFailure(t *testing.T) {
	m := buildModel(t)

	updated, _ := m.Update(gitUIDoneMsg{err: errors.New("exit status 1")})
	*m = *updated.(*Model)

	if !strings.Contains(m.errBar.text, gitUI) || !strings.Contains(m.errBar.text, "exit status 1") {
		t.Fatalf("status line should carry the failure, got %q", m.errBar.text)
	}
}

// lazygit took the terminal, so it hands it back without the mouse reporting
// focus mode armed on the way in.
func TestGitUIDoneRearmsMouseWhenFocused(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "agent", t.TempDir(), "")
	m.selectSessionRow(t, "agent")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)

	updated, cmd := m.Update(gitUIDoneMsg{})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatal("returning from lazygit issued no mouse command")
	}
	if !batchContains(cmd(), tea.EnableMouseCellMotion()) {
		t.Fatalf("mouse reporting was not re-armed: %T", cmd())
	}
}
