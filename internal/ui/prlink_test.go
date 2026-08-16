package ui

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// capturePullRequest swaps the gh seam. The returned pointer is the root the
// lookup ran in, so a test can tell which repository was asked about.
func capturePullRequest(t *testing.T, url string) *string {
	t.Helper()
	var asked string
	prev := findPullRequest
	findPullRequest = func(root, branch string) string {
		asked = root + " " + branch
		return url
	}
	t.Cleanup(func() { findPullRequest = prev })
	return &asked
}

// captureOpenedLink swaps the browser seam, so a test reads the URL that
// would have been opened instead of opening it.
func captureOpenedLink(t *testing.T) *string {
	t.Helper()
	var opened string
	prev := openBrowser
	openBrowser = func(target string) error {
		opened = target
		return nil
	}
	t.Cleanup(func() { openBrowser = prev })
	return &opened
}

// remoteAt gives a repository an origin, since a checkout with nowhere to
// push has nothing for P to open.
func remoteAt(t *testing.T, dir, url string) {
	t.Helper()
	cmd := exec.Command("git", "remote", "add", "origin", url)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}
}

// pressPRKey runs P the way the list does: the handler, then the command it
// hands back for the lookup and the open.
func pressPRKey(t *testing.T, m *Model) {
	t.Helper()
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("P produced no command, err = %q", m.errBar.text)
	}
	// Two hops: the lookup answers with a target, and the target is what the
	// browser command then opens.
	msg := cmd()
	updated, cmd = m.Update(msg)
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("%T opened nothing, err = %q", msg, m.errBar.text)
	}
	m.applyCmd(t, cmd)
}

// The pull request is what the session produced, so it wins over the
// repository page whenever the branch has one.
func TestPRKeyOpensTheBranchesPullRequest(t *testing.T) {
	m := buildModel(t)
	opened := captureOpenedLink(t)
	pr := "https://github.com/owner/repo/pull/7"
	asked := capturePullRequest(t, pr)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:owner/repo.git")
	createSession(t, m, "agent", repo, "")
	m.selectSessionRow(t, "agent")

	pressPRKey(t, m)

	if *opened != pr {
		t.Fatalf("opened %q, want the pull request %q", *opened, pr)
	}
	if want := resolved(t, repo) + " main"; *asked != want {
		t.Fatalf("looked up %q, want %q", *asked, want)
	}
	if !strings.Contains(m.errBar.text, pr) || !m.errBar.worked() {
		t.Fatalf("status line should name what opened, got %q", m.errBar.text)
	}
}

// A branch nobody has opened a pull request for still has a repository worth
// looking at, which is the whole reason this is one key and not two.
func TestPRKeyFallsBackToTheRepository(t *testing.T) {
	m := buildModel(t)
	opened := captureOpenedLink(t)
	capturePullRequest(t, "")
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:owner/repo.git")
	createSession(t, m, "agent", repo, "")
	m.selectSessionRow(t, "agent")

	pressPRKey(t, m)

	if want := "https://github.com/owner/repo"; *opened != want {
		t.Fatalf("opened %q, want the repository %q", *opened, want)
	}
}

// A session in a worktree is on its own branch, and that branch is the one
// with the pull request on it.
func TestPRKeyLooksUpTheWorktreesBranch(t *testing.T) {
	m := buildModel(t)
	captureOpenedLink(t)
	asked := capturePullRequest(t, "https://github.com/owner/repo/pull/9")
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:owner/repo.git")
	sess := createWorktreeSession(t, m, "wt", repo)
	m.selectSessionRow(t, "wt")

	pressPRKey(t, m)

	if want := resolved(t, sess.Cwd) + " " + sess.WorktreeBranch; *asked != want {
		t.Fatalf("looked up %q, want the worktree and its branch %q", *asked, want)
	}
}

// A group row carries a path but no branch of its own; its repository is
// still worth opening.
func TestPRKeyOpensAGroupsRepository(t *testing.T) {
	m := buildModel(t)
	opened := captureOpenedLink(t)
	capturePullRequest(t, "")
	repo := seedRepo(t)
	remoteAt(t, repo, "https://gitlab.com/team/repo.git")
	if err := m.store.CreateGroup("backend", repo); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	pressPRKey(t, m)

	if want := "https://gitlab.com/team/repo"; *opened != want {
		t.Fatalf("opened %q, want %q", *opened, want)
	}
}

// A repository nobody has pushed anywhere has no page at all, and the reason
// belongs on the status bar rather than in a browser tab full of nothing.
func TestPRKeyWithoutARemoteExplainsItself(t *testing.T) {
	m := buildModel(t)
	opened := captureOpenedLink(t)
	capturePullRequest(t, "")
	repo := seedRepo(t)
	createSession(t, m, "agent", repo, "")
	m.selectSessionRow(t, "agent")

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatalf("a repository with no remote should open nothing, got %T", cmd())
	}
	if *opened != "" {
		t.Fatalf("opened %q for a repository with no remote", *opened)
	}
	if !strings.Contains(m.errBar.text, "remote") {
		t.Fatalf("status line should say why, got %q", m.errBar.text)
	}
}

// A remote pointing at a directory on disk is a repository shared without a
// host, and there is no page to send anyone to.
func TestPRKeyWithAPathRemoteExplainsItself(t *testing.T) {
	m := buildModel(t)
	capturePullRequest(t, "")
	repo := seedRepo(t)
	remoteAt(t, repo, t.TempDir())
	createSession(t, m, "agent", repo, "")
	m.selectSessionRow(t, "agent")

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatalf("a path remote should open nothing, got %T", cmd())
	}
	if !strings.Contains(m.errBar.text, "remote") {
		t.Fatalf("status line should say why, got %q", m.errBar.text)
	}
}

// Outside a repository there is nothing to resolve.
func TestPRKeyOutsideARepositoryExplainsItself(t *testing.T) {
	m := buildModel(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	capturePullRequest(t, "")
	createSession(t, m, "agent", t.TempDir(), "")
	m.selectSessionRow(t, "agent")

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatalf("a directory outside a repository should open nothing, got %T", cmd())
	}
	if !strings.Contains(m.errBar.text, "repository") {
		t.Fatalf("status line should say why, got %q", m.errBar.text)
	}
}

// Without gh there is nothing to ask, and the repository page is the answer
// rather than an error about a tool the reader never asked to install.
func TestGHPullRequestWithoutTheCLI(t *testing.T) {
	prev := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = prev })

	if got := ghPullRequest(t.TempDir(), "am/feature"); got != "" {
		t.Fatalf("ghPullRequest = %q, want nothing without gh", got)
	}
}

// A detached or unborn HEAD reaches the lookup with no branch, and there is
// no pull request to ask about.
func TestGHPullRequestWithoutABranch(t *testing.T) {
	if got := ghPullRequest(t.TempDir(), ""); got != "" {
		t.Fatalf("ghPullRequest = %q, want nothing without a branch", got)
	}
}
