package ui

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pretendGH makes gh look installed without one being on the machine. Every
// other binary still answers from PATH, so an editor or lazygit lookup in the
// same test is unaffected.
func pretendGH(t *testing.T) {
	t.Helper()
	prev := lookPath
	lookPath = func(name string) (string, error) {
		if name == "gh" {
			return "/usr/bin/gh", nil
		}
		return prev(name)
	}
	t.Cleanup(func() { lookPath = prev })
}

// captureListing swaps the gh seam. The returned pointer counts the calls, so
// a test can tell a listing that was reused from one that was re-fetched.
func captureListing(t *testing.T, prs ...pullRequest) *int {
	t.Helper()
	calls := 0
	prev := listPullRequests
	listPullRequests = func(context.Context, string, string) []pullRequest {
		calls++
		return prs
	}
	t.Cleanup(func() { listPullRequests = prev })
	return &calls
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

// pressPRKey runs P the way the list does. A cold row resolves in two hops —
// the lookup answers with what it found, and the browser command opens it —
// so the press follows the chain rather than stopping at the first message.
func pressPRKey(t *testing.T, m *Model) {
	t.Helper()
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	*m = *updated.(*Model)
	if cmd == nil && m.mode != modePRPick {
		t.Fatalf("P produced no command, err = %q", m.errBar.text)
	}
	follow(t, m, cmd)
}

// follow runs a command and everything its message sets off, the way the
// event loop does. The hop count is a runaway guard, not a limit any of
// these paths comes near.
func follow(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for hops := 0; cmd != nil; hops++ {
		if hops > 4 {
			t.Fatal("a command chain that will not settle")
		}
		msg := cmd()
		if msg == nil {
			return
		}
		updated, next := m.Update(msg)
		*m = *updated.(*Model)
		cmd = next
	}
}

// prRepo is a repository with an origin and a session sitting in it, which is
// the setup every one of these tests needs before it can press anything.
func prRepo(t *testing.T, m *Model, remote string) string {
	t.Helper()
	repo := seedRepo(t)
	remoteAt(t, repo, remote)
	createSession(t, m, "agent", repo, "")
	m.selectSessionRow(t, "agent")
	return repo
}

// The badge scan has usually already answered, and the key should spend that
// answer rather than ask the network the same question again.
func TestPRKeyOpensTheScannedPullRequestWithoutAsking(t *testing.T) {
	m := buildModel(t)
	pretendGH(t)
	opened := captureOpenedLink(t)
	calls := captureListing(t)
	prRepo(t, m, "git@github.com:owner/repo.git")
	pr := pullRequest{Number: 7, URL: "https://github.com/owner/repo/pull/7", Head: "main"}
	m.prs = map[string][]pullRequest{m.sessionRows()[0].ID: {pr}}

	pressPRKey(t, m)

	if *opened != pr.URL {
		t.Fatalf("opened %q, want the scanned pull request %q", *opened, pr.URL)
	}
	if *calls != 0 {
		t.Fatalf("a row the scan already covered asked gh %d times", *calls)
	}
	if !strings.Contains(m.errBar.text, pr.URL) || !m.errBar.worked() {
		t.Fatalf("status line should name what opened, got %q", m.errBar.text)
	}
}

// A row the scan has not reached yet asks for itself.
func TestPRKeyLooksUpAColdRow(t *testing.T) {
	m := buildModel(t)
	pretendGH(t)
	opened := captureOpenedLink(t)
	captureListing(t, pullRequest{Number: 9, URL: "https://github.com/owner/repo/pull/9", Head: "main"})
	prRepo(t, m, "git@github.com:owner/repo.git")

	pressPRKey(t, m)

	if want := "https://github.com/owner/repo/pull/9"; *opened != want {
		t.Fatalf("opened %q, want %q", *opened, want)
	}
}

// A listing that holds nobody else's branch leaves this one with no pull
// request, and the repository is what a reader gets instead.
func TestPRKeyFallsBackToTheRepository(t *testing.T) {
	m := buildModel(t)
	pretendGH(t)
	opened := captureOpenedLink(t)
	captureListing(t, pullRequest{Number: 4, URL: "https://github.com/owner/repo/pull/4", Head: "someone-else"})
	prRepo(t, m, "git@github.com:owner/repo.git")

	pressPRKey(t, m)

	if want := "https://github.com/owner/repo"; *opened != want {
		t.Fatalf("opened %q, want the repository %q", *opened, want)
	}
}

// Two pull requests off one branch cannot be picked between by guessing, so
// the reader picks.
func TestPRKeyOpensTheChooserForSeveral(t *testing.T) {
	m := buildModel(t)
	pretendGH(t)
	opened := captureOpenedLink(t)
	prRepo(t, m, "git@github.com:owner/repo.git")
	m.prs = map[string][]pullRequest{m.sessionRows()[0].ID: {
		{Number: 12, URL: "https://github.com/owner/repo/pull/12", Title: "the fix", Base: "main", Head: "main"},
		{Number: 11, URL: "https://github.com/owner/repo/pull/11", Title: "the backport", Base: "release-1", Head: "main"},
	}}

	pressPRKey(t, m)

	if m.mode != modePRPick {
		t.Fatalf("mode = %v, want the pull request chooser", m.mode)
	}
	if *opened != "" {
		t.Fatalf("the chooser opened %q before anyone picked", *opened)
	}
	card := m.viewPRPick()
	for _, want := range []string{"#12", "#11", "the backport", "release-1"} {
		if !strings.Contains(card, want) {
			t.Fatalf("chooser should show %q, got:\n%s", want, card)
		}
	}

	follow(t, m, pressPickKey(t, m, tea.KeyMsg{Type: tea.KeyDown}))
	follow(t, m, pressPickKey(t, m, tea.KeyMsg{Type: tea.KeyEnter}))

	if want := "https://github.com/owner/repo/pull/11"; *opened != want {
		t.Fatalf("opened %q, want the second pull request %q", *opened, want)
	}
	if m.mode != modeList {
		t.Fatalf("mode = %v, want the list back after opening", m.mode)
	}
}

// The chooser stands in for the fallback P would have taken, so the
// repository stays reachable from inside it.
func TestPRPickOpensTheRepository(t *testing.T) {
	m := buildModel(t)
	opened := captureOpenedLink(t)
	m.prPick = prPickState{
		prs:  []pullRequest{{Number: 12, URL: "https://github.com/owner/repo/pull/12"}},
		repo: "https://github.com/owner/repo",
	}
	m.mode = modePRPick

	follow(t, m, pressPickKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}))

	if want := "https://github.com/owner/repo"; *opened != want {
		t.Fatalf("opened %q, want the repository %q", *opened, want)
	}
}

func TestPRPickEscapesBackToTheList(t *testing.T) {
	m := buildModel(t)
	m.prPick = prPickState{prs: []pullRequest{{Number: 12}}}
	m.mode = modePRPick

	pressPickKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.mode != modeList {
		t.Fatalf("mode = %v, want the list back", m.mode)
	}
}

func pressPickKey(t *testing.T, m *Model, msg tea.KeyMsg) tea.Cmd {
	t.Helper()
	updated, cmd := m.handleKey(msg)
	*m = *updated.(*Model)
	return cmd
}

// A repository nobody has pushed anywhere has no page at all, and the reason
// belongs on the status bar rather than in a browser tab full of nothing.
func TestPRKeyWithoutARemoteExplainsItself(t *testing.T) {
	m := buildModel(t)
	pretendGH(t)
	opened := captureOpenedLink(t)
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
	pretendGH(t)
	prRepo(t, m, t.TempDir())

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
func TestPullRequestsOnWithoutTheCLI(t *testing.T) {
	prev := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = prev })
	captureListing(t, pullRequest{Number: 1, Head: "am/feature"})

	if got := pullRequestsOn(context.Background(), t.TempDir(), "https://github.com/o/r", "am/feature"); got != nil {
		t.Fatalf("pullRequestsOn = %v, want nothing without gh", got)
	}
}

// A detached or unborn HEAD reaches the lookup with no branch, and there is
// no pull request to ask about.
func TestPullRequestsOnWithoutABranch(t *testing.T) {
	pretendGH(t)
	calls := captureListing(t, pullRequest{Number: 1, Head: "am/feature"})

	if got := pullRequestsOn(context.Background(), t.TempDir(), "https://github.com/o/r", ""); got != nil {
		t.Fatalf("pullRequestsOn = %v, want nothing without a branch", got)
	}
	if *calls != 0 {
		t.Fatalf("a branchless row asked gh %d times", *calls)
	}
}
