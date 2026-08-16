package ui

import (
	"errors"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// gitUI is the program G hands the terminal to. It is named rather than
// configured because it is the whole point of the key: a reader who wants a
// different git client already has o for their editor and ctrl+r for the
// review this manager draws itself.
const gitUI = "lazygit"

// gitUIDoneMsg ends a lazygit overlay, carrying a launch that failed or an
// exit that was not clean. It always took the screen — a git TUI has nowhere
// else to draw — so the terminal is put back the way an attach puts it back.
type gitUIDoneMsg struct{ err error }

// gitKey opens lazygit on the repository under the cursor, full screen, and
// comes back to the list when it quits.
//
// ctrl+r reviews what an agent changed; this is the other half — staging,
// committing, branches, stashes, the log. Both read the same worktree, and
// neither is a substitute for the other, so the diff review keeps ctrl+r and
// the git client gets a key of its own.
//
// A session's live pane directory decides the repository, so a session in a
// worktree opens on its own branch rather than on the repository it branched
// from, and a group row opens on its default path.
func (m *Model) gitKey() (tea.Model, tea.Cmd) {
	if _, ok := m.selectedRow(); !ok {
		return m, nil
	}
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to open " + gitUI + " in: " + dir
		return m, nil
	}
	if _, err := lookPath(gitUI); err != nil {
		m.errBar.text = gitUI + " is not on PATH: install it to open it with G"
		return m, nil
	}
	root, err := m.gitRoot(dir)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.errBar.text = ""
	return m, execGitUI(root)
}

// gitRoot is the repository a directory belongs to. Reported as an error
// rather than a bare miss so the reason reaches the status bar: a directory
// outside any repository and a machine with no git are different problems.
func (m *Model) gitRoot(dir string) (string, error) {
	if m.gitDrv == nil {
		return "", errors.New("git is not installed, so there is no repository to open")
	}
	return m.gitDrv.RepoRoot(dir)
}

// execGitUI is the seam tests swap to observe the launch instead of handing
// the terminal to a real lazygit.
var execGitUI = func(root string) tea.Cmd {
	cmd := exec.Command(gitUI)
	// The repository is given as the working directory rather than as a flag,
	// so nothing here depends on which options this lazygit build parses.
	cmd.Dir = root
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return gitUIDoneMsg{err: err}
	})
}
