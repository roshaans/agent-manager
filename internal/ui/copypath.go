package ui

import (
	"github.com/YoanWai/agent-manager/internal/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

// copyPathText is the seam tests swap, so a copy under test never reaches
// the machine's own clipboard.
var copyPathText = clipboard.WriteText

// pathCopiedMsg ends a copy. The status line waits on it rather than
// announcing the path from copyPath, so a write that failed is never
// reported as one that worked.
type pathCopiedMsg struct {
	path string
	err  error
}

// copyPath puts the row's directory on the clipboard: the checkout a
// session was given - its worktree, for a session spawned into one - or a
// group's default path.
//
// The recorded directory is what is copied, not the live one an attached
// shell has since cd'd into: the path is wanted for what the session is,
// somewhere to cd to or hand an agent, and a shell three directories deep
// in a build tree would answer with the wrong one. A session whose
// directory is gone still copies it, since a path is worth having to look
// at even when the checkout behind it has been removed.
func (m *Model) copyPath() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	path := entry.sess.Cwd
	if entry.isGroup {
		path, _ = resolveExistingDir(m.groupPaths[entry.group], m.groupDefaultDir(entry.group))
	}
	if path == "" {
		m.errBar.text = "no directory to copy"
		return m, nil
	}
	m.errBar.text = ""
	// Writing to the clipboard forks a helper on most platforms, which
	// Update must not wait on.
	return m, func() tea.Msg {
		return pathCopiedMsg{path: path, err: copyPathText(path)}
	}
}
