package ui

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/git"
	tea "github.com/charmbracelet/bubbletea"
)

// prLookupTimeout bounds the pull request lookup. Past it the repository's
// own page is the answer: the key is pressed for a quick look, and a look
// that keeps the list waiting is worth less than one that lands.
const prLookupTimeout = 5 * time.Second

// prLinkMsg carries what P resolved to, once the lookup that needed the
// network has come back.
type prLinkMsg struct{ target string }

// prKey opens what a session produced, on the host its repository is
// published to: the branch's pull request when it has one, and the
// repository's own page when it does not.
//
// One key rather than two, because "show me this branch on the remote" is
// the same wish either side of the moment a pull request gets opened, and
// nobody wants to remember which side of it they are on before pressing.
//
// The row's live directory decides the repository, as it does for G, so a
// session in a worktree answers with its own branch rather than with the
// branch of the repository it was cut from.
func (m *Model) prKey() (tea.Model, tea.Cmd) {
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to open a remote for: " + dir
		return m, nil
	}
	root, err := m.gitRoot(dir)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	remote, err := m.gitDrv.RemoteURL(root)
	if err != nil || remote == "" {
		m.errBar.text = "no remote to open: this repository is not published anywhere"
		return m, nil
	}
	web, err := git.WebURL(remote)
	if err != nil {
		m.errBar.text = "no web page for the remote " + remote
		return m, nil
	}
	repo, err := m.gitDrv.OpenRepo(root)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	branch := repo.Branch
	if repo.Detached || repo.Unborn {
		branch = ""
	}
	m.errBar.text = ""
	// Asking after the pull request is a network round trip, so it waits for
	// a command instead of holding the keystroke.
	return m, func() tea.Msg {
		if pr := findPullRequest(root, branch); pr != "" {
			return prLinkMsg{target: pr}
		}
		return prLinkMsg{target: web}
	}
}

// findPullRequest is the seam tests swap for the real gh call.
var findPullRequest = ghPullRequest

// ghPullRequest asks the GitHub CLI for the checkout's pull request. Every
// way it can come up short — no gh installed, not logged in, a host gh does
// not know, a branch nobody opened a pull request for — is the same answer
// here: nothing to open, and the repository page is what the caller falls
// back to.
//
// The branch is not passed, only checked for: gh qualifies the checked-out
// branch with the owner of its own remote, and names one given as an argument
// against the repository the pull request would be merged into. Work pushed
// to a fork is only found the first way, and a worktree's HEAD is already the
// branch being asked about.
func ghPullRequest(root, branch string) string {
	if branch == "" {
		return ""
	}
	if _, err := lookPath("gh"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), prLookupTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", "--json", "url", "--jq", ".url")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
