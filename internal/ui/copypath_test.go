package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// captureCopy swaps the clipboard seam, so a test reads what would have
// been copied instead of putting it on the machine's own clipboard.
func captureCopy(t *testing.T, err error) *string {
	t.Helper()
	var copied string
	prev := copyPathText
	copyPathText = func(text string) error {
		copied = text
		return err
	}
	t.Cleanup(func() { copyPathText = prev })
	return &copied
}

// Y copies the directory the session runs in - the worktree it was given,
// for one spawned into a worktree - and the status line names it.
func TestCopyPathCopiesTheSessionDirectory(t *testing.T) {
	m := buildModel(t)
	copied := captureCopy(t, nil)
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")

	// The write belongs to a command, not to Update: the status line only
	// names the path once that command has run.
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("Y produced no copy, err = %q", m.errBar.text)
	}
	if *copied != "" {
		t.Fatalf("the copy ran on the update path: %q", *copied)
	}
	m.applyCmd(t, cmd)

	sess := m.sessionRows()[0]
	if *copied != sess.Cwd {
		t.Fatalf("copied %q, want the session's directory %q", *copied, sess.Cwd)
	}
	if !strings.Contains(m.errBar.text, sess.Cwd) {
		t.Fatalf("status line should name the path, got %q", m.errBar.text)
	}
	if !m.errBar.worked() {
		t.Fatalf("a finished copy should read as an outcome, got %q", m.errBar.text)
	}
}

// The session's own directory is what is copied, not the one its pane is
// live in: a session moved to a new worktree answers with the worktree,
// while its shell is still sitting in the old one.
func TestCopyPathCopiesTheRecordedDirectoryNotTheLiveOne(t *testing.T) {
	m := buildModel(t)
	copied := captureCopy(t, nil)
	started := t.TempDir()
	createSession(t, m, "agent", started, "")
	m.selectSessionRow(t, "agent")
	moved := t.TempDir()
	if err := m.store.MoveSessionWorktree(m.sessionRows()[0].ID, moved, "am/feature"); err != nil {
		t.Fatalf("move the worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "agent")

	copyPathNow(t, m)

	if *copied != moved {
		t.Fatalf("copied %q, want the worktree the session was moved to %q", *copied, moved)
	}
	if live, err := m.tmux.PaneCurrentPath(m.sessionRows()[0].ID); err == nil && *copied == live {
		t.Fatal("the pane's live directory was copied rather than the session's own")
	}
}

// A group row answers with the path its sessions are created in.
func TestCopyPathCopiesAGroupDefaultPath(t *testing.T) {
	m := buildModel(t)
	copied := captureCopy(t, nil)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	copyPathNow(t, m)

	if *copied != dir {
		t.Fatalf("copied %q, want the group's path %q", *copied, dir)
	}
}

// A clipboard that refused keeps the failure on screen: reporting the path
// as copied would send the reader to paste something that is not there.
func TestCopyPathReportsAClipboardThatRefused(t *testing.T) {
	m := buildModel(t)
	captureCopy(t, errors.New("no clipboard on this machine"))
	createSession(t, m, "agent", t.TempDir(), "")
	m.selectSessionRow(t, "agent")

	copyPathNow(t, m)

	if m.errBar.worked() {
		t.Fatalf("a failed copy should not read as an outcome, got %q", m.errBar.text)
	}
	if !strings.Contains(m.errBar.text, "no clipboard on this machine") {
		t.Fatalf("status line should carry the failure, got %q", m.errBar.text)
	}
}

// copyPathNow runs a copy the way a key press does: the handler, then the
// command it hands back for the clipboard write itself.
func copyPathNow(t *testing.T, m *Model) {
	t.Helper()
	updated, cmd := m.copyPath()
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("copyPath produced no command, err = %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
}
