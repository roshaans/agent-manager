package ui

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
)

func scanDriver(t *testing.T) *git.Driver {
	t.Helper()
	drv, err := git.New()
	if err != nil {
		t.Skip("git not installed")
	}
	return drv
}

// A branch's badge is the pull requests opened off it, and nobody else's.
func TestScanMatchesTheBranchOnly(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	captureListing(t,
		pullRequest{Number: 1, Head: "main", URL: "u1"},
		pullRequest{Number: 2, Head: "someone-else", URL: "u2"},
	)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:owner/repo.git")

	found, _ := scanPullRequests(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if len(found["s1"]) != 1 || found["s1"][0].Number != 1 {
		t.Fatalf("scan found %v, want only the pull request on this branch", found["s1"])
	}
}

// The listing is cached by remote, not by checkout: several worktrees of one
// repository are what this feature exists to sit under, and asking once per
// worktree would multiply a reader's API budget by however many they run.
func TestScanListsARemoteOnce(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	calls := captureListing(t, pullRequest{Number: 1, Head: "main", URL: "u1"})
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:owner/repo.git")
	worktree := filepath.Join(t.TempDir(), "wt")
	gitAt(t, repo, "worktree", "add", "-b", "am/side", worktree)

	found, _ := scanPullRequests(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo},
		{sessID: "s2", dir: repo},
		{sessID: "s3", dir: worktree},
	})

	if *calls != 1 {
		t.Fatalf("the scan listed the remote %d times, want once", *calls)
	}
	if len(found["s1"]) != 1 {
		t.Fatalf("the session on main found %v, want its pull request", found["s1"])
	}
	if len(found["s3"]) != 0 {
		t.Fatalf("the worktree on another branch found %v, want none", found["s3"])
	}
}

// A checkout whose origin is a fork can hold pull requests either way round,
// and gh's own repository resolution only ever answers with the parent. The
// remote has to be named for a pull request opened against the fork itself to
// be found at all.
func TestScanListsTheRemoteAndWhatGHResolves(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	var asked []string
	prev := listPullRequests
	listPullRequests = func(_ context.Context, root, repoURL string) []pullRequest {
		return ghOpenPullRequests(context.Background(), root, repoURL)
	}
	prevRun := ghListRun
	ghListRun = func(_ context.Context, _, repo string) []pullRequest {
		asked = append(asked, repo)
		if repo == "" {
			return []pullRequest{{Number: 328, URL: "upstream/328", Head: "main"}}
		}
		return []pullRequest{{Number: 1, URL: "fork/1", Head: "main"}}
	}
	t.Cleanup(func() { listPullRequests, ghListRun = prev, prevRun })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _ := scanPullRequests(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if want := []string{"https://github.com/me/fork", ""}; !slices.Equal(asked, want) {
		t.Fatalf("listed %v, want the remote first and gh's own resolution second", asked)
	}
	if len(found["s1"]) != 2 || found["s1"][0].URL != "fork/1" {
		t.Fatalf("found %v, want both, the fork's own first", found["s1"])
	}
}

// A repository that is nobody's fork answers both listings with the same
// pull requests, and a row must not wear the same number twice.
func TestScanDropsTheRepeatedListing(t *testing.T) {
	pretendGH(t)
	same := []pullRequest{{Number: 5, URL: "u5", Head: "main"}}
	prev := ghListRun
	ghListRun = func(context.Context, string, string) []pullRequest { return same }
	t.Cleanup(func() { ghListRun = prev })

	got := ghOpenPullRequests(context.Background(), t.TempDir(), "https://github.com/o/r")

	if len(got) != 1 {
		t.Fatalf("listing returned %v, want the repeat dropped", got)
	}
}

// A directory outside any repository, and one in a repository nobody
// published, are both rows with nothing to badge.
func TestScanSkipsWhatCannotHaveAPullRequest(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	calls := captureListing(t, pullRequest{Number: 1, Head: "main"})
	loose := t.TempDir()
	unpublished := seedRepo(t)

	found, _ := scanPullRequests(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: loose},
		{sessID: "s2", dir: unpublished},
	})

	if len(found) != 0 {
		t.Fatalf("scan found %v, want nothing", found)
	}
	if *calls != 0 {
		t.Fatalf("the scan listed %d remotes that do not exist", *calls)
	}
}

// Without gh there is nothing to ask, and no badge is the right amount of
// noise about a tool the reader never asked to install.
func TestScanWithoutTheCLI(t *testing.T) {
	drv := scanDriver(t)
	prev := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = prev })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:owner/repo.git")

	if found, _ := scanPullRequests(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}}); found != nil {
		t.Fatalf("scan found %v without gh, want nothing", found)
	}
}

// A shell opened for a session shares its checkout, so badging it too would
// print one pull request on two rows sitting one under the other. An
// archived session is not work in flight and has no badge to keep either.
func TestScanTargetsLeaveOutShellsAndArchived(t *testing.T) {
	m := buildModel(t)
	shell, _, ok := m.shellTool()
	if !ok {
		t.Fatal("the test config declares no shell tool")
	}
	m.sessions = []store.Session{
		{ID: "agent", Cwd: "/tmp/a", Tool: "claude"},
		{ID: "shell", Cwd: "/tmp/a", Tool: shell},
		{ID: "gone", Cwd: "/tmp/a", Tool: "claude", Archived: true},
		{ID: "nowhere", Tool: "claude"},
	}

	targets := m.prScanTargets()

	if len(targets) != 1 || targets[0].sessID != "agent" {
		t.Fatalf("the scan would cover %v, want only the agent", targets)
	}
}

func TestPRChip(t *testing.T) {
	m := buildModel(t)
	m.prs = map[string][]pullRequest{
		"open":  {{Number: 328}},
		"draft": {{Number: 327, IsDraft: true}},
		"both":  {{Number: 12}, {Number: 11}},
	}
	for _, tc := range []struct{ id, want string }{
		{"open", "#328"},
		{"draft", "#327" + prDraftMark},
		{"both", "#12+1"},
		{"none", ""},
	} {
		got := m.prChip(store.Session{ID: tc.id})
		if tc.want == "" {
			if got != "" {
				t.Fatalf("chip for %s = %q, want none", tc.id, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Fatalf("chip for %s = %q, want it to carry %q", tc.id, got, tc.want)
		}
	}
}

func gitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// noPane stands in for a session that printed nothing, which is what most of
// these tests are about: the branch, and nothing observed.
func noPane(string) string { return "" }

// captureCommitPulls swaps the seam that asks which pull requests contain a
// commit, recording the SHA it was asked about.
func captureCommitPulls(t *testing.T, prs ...pullRequest) *string {
	t.Helper()
	var sha string
	prev := commitPullRequests
	commitPullRequests = func(_ context.Context, _, _, want string) []pullRequest {
		sha = want
		return prs
	}
	t.Cleanup(func() { commitPullRequests = prev })
	return &sha
}

// A commit is a git fact where a branch name is a label, so a pull request
// whose head branch nobody here is on is still found through the work itself.
func TestScanFindsAPullRequestByCommit(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	captureListing(t) // the branch listing knows nothing
	byCommit := pullRequest{Number: 5, URL: "https://github.com/me/fork/pull/5", Head: "renamed", State: "OPEN"}
	sha := captureCommitPulls(t, byCommit)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _ := scanPullRequests(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if len(found["s1"]) != 1 || found["s1"][0].Number != 5 {
		t.Fatalf("found %v, want the pull request the commit is in", found["s1"])
	}
	head := gitOut(t, repo, "rev-parse", "HEAD")
	if *sha != head {
		t.Fatalf("asked about %q, want the session's own commit %q", *sha, head)
	}
}

// Sessions sharing a checkout share a commit, so they share the one lookup.
func TestScanAsksAboutACommitOnce(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	captureListing(t)
	calls := 0
	prev := commitPullRequests
	commitPullRequests = func(context.Context, string, string, string) []pullRequest {
		calls++
		return nil
	}
	t.Cleanup(func() { commitPullRequests = prev })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	scanPullRequests(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo},
		{sessID: "s2", dir: repo},
	})

	if calls != 1 {
		t.Fatalf("asked about the same commit %d times, want once", calls)
	}
}

// The sources add to each other rather than replacing, and the one that
// knows most leads, since that is the one P opens.
func TestScanOrdersCreatedThenCommitThenBranch(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	recorded := pullRequest{Number: 1, URL: "u/1", State: "OPEN"}
	byCommit := pullRequest{Number: 2, URL: "u/2", State: "OPEN", Head: "other"}
	byBranch := pullRequest{Number: 3, URL: "u/3", State: "OPEN", Head: "main"}
	prev := ghListRun
	ghListRun = func(context.Context, string, string) []pullRequest { return []pullRequest{byBranch} }
	prevView := viewPullRequest
	viewPullRequest = func(context.Context, string, string) pullRequest { return recorded }
	t.Cleanup(func() { ghListRun, viewPullRequest = prev, prevView })
	captureCommitPulls(t, byCommit)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _ := scanPullRequests(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo, prURL: recorded.URL, prSource: prSourceCreated},
	})

	var got []int
	for _, pr := range found["s1"] {
		got = append(got, pr.Number)
	}
	if !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("found %v, want created, then commit, then branch", got)
	}
}

// A session asked to look at somebody else's pull request prints that address
// too. It is a reading, not a fact, so it must never displace the commit that
// says which pull request the session's own work is in.
func TestScanRanksAPrintedAddressBehindTheCommit(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	mine := pullRequest{Number: 9, URL: "https://github.com/me/fork/pull/9", State: "OPEN", Head: "other"}
	theirs := pullRequest{Number: 4, URL: "https://github.com/me/fork/pull/4", State: "OPEN", Head: "someone"}
	prev := ghListRun
	ghListRun = func(context.Context, string, string) []pullRequest { return []pullRequest{theirs} }
	prevView := viewPullRequest
	viewPullRequest = func(context.Context, string, string) pullRequest { return theirs }
	t.Cleanup(func() { ghListRun, viewPullRequest = prev, prevView })
	captureCommitPulls(t, mine)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")
	pane := func(string) string { return "have a look at " + theirs.URL }

	found, _ := scanPullRequests(drv, pane, []prScanTarget{{sessID: "s1", dir: repo}})

	if len(found["s1"]) == 0 || found["s1"][0].Number != 9 {
		t.Fatalf("found %v, want the commit's own pull request leading", found["s1"])
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
