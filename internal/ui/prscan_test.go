package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if len(found["s1"].prs) != 1 || found["s1"].prs[0].Number != 1 {
		t.Fatalf("scan found %v, want only the pull request on this branch", found["s1"].prs)
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

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo},
		{sessID: "s2", dir: repo},
		{sessID: "s3", dir: worktree},
	})

	if *calls != 1 {
		t.Fatalf("the scan listed the remote %d times, want once", *calls)
	}
	if len(found["s1"].prs) != 1 {
		t.Fatalf("the session on main found %v, want its pull request", found["s1"].prs)
	}
	if len(found["s3"].prs) != 0 {
		t.Fatalf("the worktree on another branch found %v, want none", found["s3"].prs)
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
	listPullRequests = func(_ context.Context, root, repoURL string) ([]pullRequest, error) {
		return ghOpenPullRequests(context.Background(), root, repoURL)
	}
	prevRun := ghListRun
	ghListRun = func(_ context.Context, _, repo string) ([]pullRequest, error) {
		asked = append(asked, repo)
		if repo == "" {
			return []pullRequest{{Number: 328, URL: "upstream/328", Head: "main"}}, nil
		}
		return []pullRequest{{Number: 1, URL: "fork/1", Head: "main"}}, nil
	}
	t.Cleanup(func() { listPullRequests, ghListRun = prev, prevRun })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if want := []string{"https://github.com/me/fork", ""}; !slices.Equal(asked, want) {
		t.Fatalf("listed %v, want the remote first and gh's own resolution second", asked)
	}
	if len(found["s1"].prs) != 2 || found["s1"].prs[0].URL != "fork/1" {
		t.Fatalf("found %v, want both, the fork's own first", found["s1"].prs)
	}
}

// A repository that is nobody's fork answers both listings with the same
// pull requests, and a row must not wear the same number twice.
func TestScanDropsTheRepeatedListing(t *testing.T) {
	pretendGH(t)
	same := []pullRequest{{Number: 5, URL: "u5", Head: "main"}}
	prev := ghListRun
	ghListRun = func(context.Context, string, string) ([]pullRequest, error) { return same, nil }
	t.Cleanup(func() { ghListRun = prev })

	got, err := ghOpenPullRequests(context.Background(), t.TempDir(), "https://github.com/o/r")

	if err != nil {
		t.Fatalf("listing: %v", err)
	}
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

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{
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

// gh gates the pull requests and nothing else. Without it there are no
// numbers to show, but ahead and behind come out of git and a machine with no
// gh installed still deserves them.
func TestScanWithoutTheCLIStillReadsGit(t *testing.T) {
	drv := scanDriver(t)
	prev := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = prev })
	calls := captureListing(t, pullRequest{Number: 1, Head: "main"})
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _, ran := scanSessions(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if ran {
		t.Fatal("a pass that never asked gh reported that it did")
	}
	if len(found["s1"].prs) != 0 {
		t.Fatalf("found %v pull requests without gh", found["s1"].prs)
	}
	if *calls != 0 {
		t.Fatalf("asked gh %d times without it installed", *calls)
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
	m.insights = insightsOf(map[string][]pullRequest{
		"open":  {{Number: 328}},
		"draft": {{Number: 327, IsDraft: true}},
		"both":  {{Number: 12}, {Number: 11}},
	})
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

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if len(found["s1"].prs) != 1 || found["s1"].prs[0].Number != 5 {
		t.Fatalf("found %v, want the pull request the commit is in", found["s1"].prs)
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

	scanSessions(drv, noPane, []prScanTarget{
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
	ghListRun = func(context.Context, string, string) ([]pullRequest, error) { return []pullRequest{byBranch}, nil }
	prevView := viewPullRequest
	viewPullRequest = func(context.Context, string, string) pullRequest { return recorded }
	t.Cleanup(func() { ghListRun, viewPullRequest = prev, prevView })
	captureCommitPulls(t, byCommit)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo, prURL: recorded.URL, prSource: prSourceCreated},
	})

	var got []int
	for _, pr := range found["s1"].prs {
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
	ghListRun = func(context.Context, string, string) ([]pullRequest, error) { return []pullRequest{theirs}, nil }
	prevView := viewPullRequest
	viewPullRequest = func(context.Context, string, string) pullRequest { return theirs }
	t.Cleanup(func() { ghListRun, viewPullRequest = prev, prevView })
	captureCommitPulls(t, mine)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")
	pane := func(string) string { return "have a look at " + theirs.URL }

	found, _, _ := scanSessions(drv, pane, []prScanTarget{{sessID: "s1", dir: repo}})

	if len(found["s1"].prs) == 0 || found["s1"].prs[0].Number != 9 {
		t.Fatalf("found %v, want the commit's own pull request leading", found["s1"].prs)
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

// A pass that never reached the host says nothing about which pull requests
// exist. Clearing the badges on it would blank every row for a minute
// whenever the network hiccups or gh is briefly unavailable.
func TestFailedPassKeepsTheBadgesOnScreen(t *testing.T) {
	m := buildModel(t)
	m.insights = insightsOf(map[string][]pullRequest{"s1": {{Number: 7}}})

	// git still answered, so ahead and behind land; gh did not, so the
	// number it found earlier stays put.
	updated, _ := m.Update(prScanMsg{
		insights: map[string]sessionInsight{"s1": {ahead: 2, synced: true}},
		ran:      false,
	})
	*m = *updated.(*Model)

	if len(m.insights["s1"].prs) != 1 {
		t.Fatalf("a pass that never reached gh cleared the badges: %v", m.insights)
	}
	if got := m.insights["s1"]; got.ahead != 2 || !got.synced {
		t.Fatalf("the git half was thrown away with the gh half: %+v", got)
	}

	// A pass that did reach gh and found nothing is evidence, and does clear.
	updated, _ = m.Update(prScanMsg{insights: map[string]sessionInsight{}, ran: true})
	*m = *updated.(*Model)

	if len(m.insights["s1"].prs) != 0 {
		t.Fatalf("a pass that ran should have cleared: %v", m.insights)
	}
}

// Without gh the pass cannot have run, whatever it returns.
func TestScanWithoutTheCLIDidNotRun(t *testing.T) {
	drv := scanDriver(t)
	prev := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = prev })

	if _, _, ran := scanSessions(drv, noPane, []prScanTarget{{sessID: "s1", dir: t.TempDir()}}); ran {
		t.Fatal("a pass without gh reported that it ran")
	}
}

// A link already recorded is a fact about the session. A host that cannot be
// reached — offline, rate limited, or a gh with no credentials in this
// environment — leaves the title and draft mark unknown, not the pull
// request. The number is in the address, so the badge still answers.
func TestRecordedLinkBadgesWithoutTheHost(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	prev, prevView := ghListRun, viewPullRequest
	ghListRun = func(context.Context, string, string) ([]pullRequest, error) { return nil, nil }
	viewPullRequest = func(context.Context, string, string) pullRequest { return pullRequest{} }
	prevCommit := commitPullRequests
	commitPullRequests = func(context.Context, string, string, string) []pullRequest { return nil }
	t.Cleanup(func() { ghListRun, viewPullRequest, commitPullRequests = prev, prevView, prevCommit })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo, prURL: "https://github.com/me/fork/pull/42", prSource: prSourceCreated},
	})

	if len(found["s1"].prs) != 1 || found["s1"].prs[0].Number != 42 {
		t.Fatalf("found %v, want the recorded number from the address alone", found["s1"].prs)
	}
	if m := (&Model{insights: found}).prChip(store.Session{ID: "s1"}); !strings.Contains(m, "#42") {
		t.Fatalf("chip = %q, want it to carry the number", m)
	}
}

// A pull request gh can answer for and reports as merged is known, not
// unknown, so it still stops wearing a badge.
func TestRecordedLinkStillDropsWhenKnownMerged(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	prev, prevView := ghListRun, viewPullRequest
	ghListRun = func(context.Context, string, string) ([]pullRequest, error) { return nil, nil }
	viewPullRequest = func(_ context.Context, _, prURL string) pullRequest {
		return pullRequest{Number: 42, URL: prURL, State: "MERGED"}
	}
	prevCommit := commitPullRequests
	commitPullRequests = func(context.Context, string, string, string) []pullRequest { return nil }
	t.Cleanup(func() { ghListRun, viewPullRequest, commitPullRequests = prev, prevView, prevCommit })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo, prURL: "https://github.com/me/fork/pull/42", prSource: prSourceCreated},
	})

	if len(found) != 0 {
		t.Fatalf("found %v, want a merged pull request dropped", found)
	}
}

// The wiring, which every other test here skips past: a tick has to actually
// produce a pass, and schedule the next one. A feature that silently never
// runs would satisfy every test of scanPullRequests on its own.
func TestScanTickRunsAPassAndSchedulesTheNext(t *testing.T) {
	prevInterval := prScanInterval
	prScanInterval = time.Millisecond
	t.Cleanup(func() { prScanInterval = prevInterval })
	m := buildModel(t)
	pretendGH(t)
	captureListing(t, pullRequest{Number: 5, URL: "u/5", Head: "main", State: "OPEN"})
	prev := commitPullRequests
	commitPullRequests = func(context.Context, string, string, string) []pullRequest { return nil }
	t.Cleanup(func() { commitPullRequests = prev })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")
	createSession(t, m, "agent", repo, "")

	updated, cmd := m.Update(prScanTickMsg{})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatal("a tick produced no work at all")
	}
	// The pass and the next tick both come back from the one batch.
	var scanned *prScanMsg
	for _, sub := range cmd().(tea.BatchMsg) {
		if msg, ok := sub().(prScanMsg); ok {
			scanned = &msg
		}
	}
	if scanned == nil {
		t.Fatal("the tick scheduled no scan")
	}
	if !scanned.ran {
		t.Fatal("the pass reported that it never ran")
	}
	if len(scanned.insights) != 1 {
		t.Fatalf("the pass found %v, want the session's pull request", scanned.insights)
	}

	// And the result reaches the badge.
	updated, _ = m.Update(*scanned)
	*m = *updated.(*Model)
	if chip := m.prChip(m.sessionRows()[0]); !strings.Contains(chip, "#5") {
		t.Fatalf("chip = %q, want the number the pass found", chip)
	}
}

// The first pass is armed at startup, or the badges only ever appear a minute
// after the manager opens. Init's batch cannot simply be walked: it holds
// ticks measured in hours, and running one waits it out.
func TestInitArmsTheFirstPass(t *testing.T) {
	prevDelay := prScanFirstDelay
	prScanFirstDelay = time.Millisecond
	t.Cleanup(func() { prScanFirstDelay = prevDelay })
	m := buildModel(t)

	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init did not return a batch")
	}
	if !armsWithin(batch, time.Second, prScanTickMsg{}) {
		t.Fatal("Init never arms the pull request scan")
	}
}

// armsWithin runs a batch's commands concurrently and reports whether any of
// them produces want before the deadline. The slow ones are simply never
// waited on, which is the whole point.
func armsWithin(batch tea.BatchMsg, within time.Duration, want tea.Msg) bool {
	seen := make(chan tea.Msg, len(batch))
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		go func(cmd tea.Cmd) {
			defer func() { recover() }()
			seen <- cmd()
		}(cmd)
	}
	deadline := time.After(within)
	for {
		select {
		case msg := <-seen:
			if msg == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// insightsOf wraps plain pull requests as the records a pass produces, so a
// test that only cares about numbers does not have to spell the rest out.
func insightsOf(bySession map[string][]pullRequest) map[string]sessionInsight {
	out := make(map[string]sessionInsight, len(bySession))
	for id, prs := range bySession {
		out[id] = sessionInsight{prs: prs}
	}
	return out
}

// pushTo gives a repository a real remote to be ahead of and behind: a bare
// clone on disk, so a pass can fetch without a network.
func pushTo(t *testing.T, repo string) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	// -b main, so the bare repository's HEAD names the branch being pushed:
	// a clone of one whose HEAD points at a branch nobody made lands on an
	// unborn master, and the next push from it has no main to push.
	gitAt(t, repo, "init", "--bare", "--quiet", "-b", "main", remote)
	gitAt(t, repo, "remote", "add", "origin", remote)
	gitAt(t, repo, "push", "--quiet", "--set-upstream", "origin", "main")
	return remote
}

// The two halves of "do I need to push, do I need to pull", against a remote
// that really moved underneath the checkout.
func TestScanCountsAheadAndBehind(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	captureListing(t)
	repo := seedRepo(t)
	remote := pushTo(t, repo)
	// A second clone stands in for whoever else pushed.
	other := filepath.Join(t.TempDir(), "other")
	gitAt(t, repo, "clone", "--quiet", remote, other)
	writeCommit(t, other, "theirs.txt", "theirs")
	gitAt(t, other, "push", "--quiet", "origin", "main")
	// And this checkout has work of its own that never left.
	writeCommit(t, repo, "mine.txt", "mine")

	prev := fetchRemote
	fetchRemote = func(ctx context.Context, drv *git.Driver, root string) { drv.Fetch(ctx, root) }
	t.Cleanup(func() { fetchRemote = prev })

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	got := found["s1"]
	if !got.synced {
		t.Fatal("a branch with an upstream reported nothing to compare against")
	}
	if got.ahead != 1 || got.behind != 1 {
		t.Fatalf("ahead=%d behind=%d, want one each", got.ahead, got.behind)
	}
}

// A branch nobody has pushed has nothing to compare against, and zero would
// report that as being in step.
func TestScanLeavesAnUnpushedBranchUnsynced(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	captureListing(t)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if found["s1"].synced {
		t.Fatalf("a branch with no upstream reported %+v", found["s1"])
	}
	if chip := (&Model{insights: found}).syncChip(store.Session{ID: "s1"}); chip != "" {
		t.Fatalf("chip = %q, want nothing rather than a zero", chip)
	}
}

// Six worktrees under six sessions are six checkouts of one remote, and the
// answer is the same for all of them.
func TestScanFetchesARepositoryOnce(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	captureListing(t)
	repo := seedRepo(t)
	pushTo(t, repo)
	fetched := 0
	prev := fetchRemote
	fetchRemote = func(context.Context, *git.Driver, string) { fetched++ }
	t.Cleanup(func() { fetchRemote = prev })

	scanSessions(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo},
		{sessID: "s2", dir: repo},
		{sessID: "s3", dir: repo},
	})

	if fetched != 1 {
		t.Fatalf("fetched %d times for one repository, want once", fetched)
	}
}

func TestSyncChip(t *testing.T) {
	m := &Model{insights: map[string]sessionInsight{
		"ahead":   {ahead: 2, synced: true},
		"behind":  {behind: 3, synced: true},
		"both":    {ahead: 1, behind: 4, synced: true},
		"level":   {synced: true},
		"unknown": {},
	}}
	for _, tc := range []struct{ id, want string }{
		{"ahead", "❨↑2❩"},
		{"behind", "❨↓3❩"},
		{"both", "❨↑1 ↓4❩"},
		{"level", ""},
		{"unknown", ""},
	} {
		got := ansi.Strip(m.syncChip(store.Session{ID: tc.id}))
		if strings.TrimSpace(got) != tc.want {
			t.Errorf("chip for %s = %q, want %q", tc.id, strings.TrimSpace(got), tc.want)
		}
	}
}

func writeCommit(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAt(t, dir, "add", name)
	gitAt(t, dir, "commit", "--quiet", "-m", "add "+name)
}

// The rollup is what a reader would do with the list: anything broken makes
// the whole thing broken, anything still going makes it pending, and only a
// finished set with no failure is passing.
func TestChecksRollUpTheWayAReaderWould(t *testing.T) {
	run := func(status, conclusion string) checkRun {
		return checkRun{Status: status, Conclusion: conclusion}
	}
	for _, tc := range []struct {
		name string
		runs []checkRun
		want checksState
	}{
		{"nothing reported", nil, checksUnknown},
		{"all green", []checkRun{run("COMPLETED", "SUCCESS"), run("COMPLETED", "SUCCESS")}, checksPassing},
		{"one broken", []checkRun{run("COMPLETED", "SUCCESS"), run("COMPLETED", "FAILURE")}, checksFailing},
		{"still going", []checkRun{run("COMPLETED", "SUCCESS"), run("IN_PROGRESS", "")}, checksRunning},
		// Broken wins over pending: a run that has already failed is not made
		// provisional by another that has not finished.
		{"broken and pending", []checkRun{run("IN_PROGRESS", ""), run("COMPLETED", "FAILURE")}, checksFailing},
		{"timed out", []checkRun{run("COMPLETED", "TIMED_OUT")}, checksFailing},
		{"cancelled", []checkRun{run("COMPLETED", "CANCELLED")}, checksFailing},
		// Somebody arranged for these not to run, or not to matter.
		{"skipped and neutral", []checkRun{run("COMPLETED", "SKIPPED"), run("COMPLETED", "NEUTRAL")}, checksPassing},
		// A legacy commit status reports neither status nor conclusion.
		{"legacy status failing", []checkRun{{State: "FAILURE"}}, checksFailing},
		{"legacy status pending", []checkRun{{State: "PENDING"}}, checksRunning},
		{"legacy status green", []checkRun{{State: "SUCCESS"}}, checksPassing},
	} {
		if got := (pullRequest{Checks: tc.runs}).checks(); got != tc.want {
			t.Errorf("%s: checks = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The colour is the whole point, so it has to differ per state rather than
// merely be set.
func TestPRChipColoursByChecks(t *testing.T) {
	pass := checkRun{Status: "COMPLETED", Conclusion: "SUCCESS"}
	fail := checkRun{Status: "COMPLETED", Conclusion: "FAILURE"}
	busy := checkRun{Status: "IN_PROGRESS"}
	m := &Model{insights: map[string]sessionInsight{
		"green":   {prs: []pullRequest{{Number: 1, Checks: []checkRun{pass}}}},
		"red":     {prs: []pullRequest{{Number: 2, Checks: []checkRun{fail}}}},
		"amber":   {prs: []pullRequest{{Number: 3, Checks: []checkRun{busy}}}},
		"unknown": {prs: []pullRequest{{Number: 4}}},
	}}
	seen := map[string]string{}
	for _, id := range []string{"green", "red", "amber", "unknown"} {
		chip := m.prChip(store.Session{ID: id})
		if !strings.Contains(ansi.Strip(chip), "#") {
			t.Fatalf("%s: chip lost its number: %q", id, chip)
		}
		seen[id] = chip
	}
	for _, pair := range [][2]string{{"green", "red"}, {"green", "amber"}, {"red", "amber"}, {"green", "unknown"}} {
		if seen[pair[0]] == seen[pair[1]] {
			t.Errorf("%s and %s render identically: %q", pair[0], pair[1], seen[pair[0]])
		}
	}
}

// GitHub computes mergeability on demand and reports UNKNOWN while it works.
// A marker that flickers on every pass would be worse than none.
func TestConflictingOnlyWhenGitHubIsSure(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  bool
	}{
		{"CONFLICTING", true},
		{"MERGEABLE", false},
		{"UNKNOWN", false},
		{"", false},
	} {
		if got := (pullRequest{Mergeable: tc.state}).conflicting(); got != tc.want {
			t.Errorf("mergeable=%q conflicting=%v, want %v", tc.state, got, tc.want)
		}
	}
}

func TestSizeReadsAsNothingWhenUnknown(t *testing.T) {
	if got := (pullRequest{}).size(); got != "" {
		t.Fatalf("size = %q, want nothing when the listing did not say", got)
	}
	if got := (pullRequest{Additions: 588, Deletions: 99}).size(); got != "+588 −99" {
		t.Fatalf("size = %q", got)
	}
	// A pull request that only deletes still has a size worth showing.
	if got := (pullRequest{Deletions: 12, ChangedFiles: 1}).size(); got != "+0 −12" {
		t.Fatalf("size = %q", got)
	}
}

// The mark and the tint answer different questions, so a pull request that is
// both broken and unmergeable shows both.
func TestPRChipMarksAConflictBesideTheCheckTint(t *testing.T) {
	m := &Model{insights: map[string]sessionInsight{
		"clean":    {prs: []pullRequest{{Number: 1, Mergeable: "MERGEABLE"}}},
		"conflict": {prs: []pullRequest{{Number: 2, Mergeable: "CONFLICTING"}}},
		"both": {prs: []pullRequest{{
			Number: 3, Mergeable: "CONFLICTING",
			Checks: []checkRun{{Status: "COMPLETED", Conclusion: "FAILURE"}},
		}}},
	}}
	if chip := ansi.Strip(m.prChip(store.Session{ID: "clean"})); strings.Contains(chip, prConflictMark) {
		t.Fatalf("a mergeable pull request wore the conflict mark: %q", chip)
	}
	for _, id := range []string{"conflict", "both"} {
		if chip := ansi.Strip(m.prChip(store.Session{ID: id})); !strings.Contains(chip, prConflictMark) {
			t.Fatalf("%s: chip = %q, want the conflict mark", id, chip)
		}
	}
	// Both states at once must not collapse into one another's rendering.
	if m.prChip(store.Session{ID: "conflict"}) == m.prChip(store.Session{ID: "both"}) {
		t.Fatal("a failing conflicted pull request renders the same as a passing one")
	}
}

// GitHub being unreachable is not evidence that these sessions have no pull
// requests. A pass where every listing refused must report that it never got
// an answer, or the badges are cleared until the host comes back.
func TestUnreachableHostIsNotAnEmptyAnswer(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	prev := listPullRequests
	listPullRequests = func(context.Context, string, string) ([]pullRequest, error) {
		return nil, errors.New("dial tcp: connection refused")
	}
	t.Cleanup(func() { listPullRequests = prev })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	_, _, reached := scanSessions(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if reached {
		t.Fatal("a pass whose every listing refused reported that it reached the host")
	}
}

// A repository that really has no open pull requests is an answer, and has to
// stay one, or a merged pull request would keep its badge forever.
func TestAnEmptyRepositoryIsAnAnswer(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	captureListing(t)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	_, _, reached := scanSessions(drv, noPane, []prScanTarget{{sessID: "s1", dir: repo}})

	if !reached {
		t.Fatal("an empty listing that succeeded was treated as a failure")
	}
}

// A fork whose parent refuses is still a fork whose own pull requests were
// read, so one listing answering is enough.
func TestOneListingAnsweringIsEnough(t *testing.T) {
	pretendGH(t)
	prev := ghListRun
	ghListRun = func(_ context.Context, _, repo string) ([]pullRequest, error) {
		if repo == "" {
			return nil, errors.New("HTTP 404")
		}
		return []pullRequest{{Number: 1, URL: "u1", Head: "main"}}, nil
	}
	t.Cleanup(func() { ghListRun = prev })

	got, err := ghOpenPullRequests(context.Background(), t.TempDir(), "https://github.com/me/fork")

	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v; want the fork's own pull request and no error", got, err)
	}
}

// A recorded address nothing could be read back from leaves no number, and
// "#0" would be a badge claiming a pull request that does not exist.
func TestChipSaysNothingWithoutANumber(t *testing.T) {
	m := &Model{insights: map[string]sessionInsight{
		"broken": {prs: []pullRequest{{URL: "https://github.com/me/fork/pulls/nope"}}},
	}}
	if chip := m.prChip(store.Session{ID: "broken"}); chip != "" {
		t.Fatalf("chip = %q, want nothing for an address with no number in it", chip)
	}
}

// Everything a session can be that is not a repository with a remote: the
// pass has to walk past each without failing the ones behind it.
func TestScanWalksPastEverySessionItCannotRead(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	captureListing(t, pullRequest{Number: 9, URL: "u9", Head: "main", State: "OPEN"})
	good := seedRepo(t)
	remoteAt(t, good, "git@github.com:me/fork.git")
	gone := filepath.Join(t.TempDir(), "deleted-under-us")

	found, _, _ := scanSessions(drv, noPane, []prScanTarget{
		{sessID: "loose", dir: t.TempDir()},       // not a repository
		{sessID: "gone", dir: gone},               // directory no longer there
		{sessID: "unpublished", dir: seedRepo(t)}, // repository with no remote
		{sessID: "nowhere", dir: ""},              // no directory at all
		{sessID: "good", dir: good},               // and one that works
	})

	if len(found["good"].prs) != 1 {
		t.Fatalf("the readable session found %v; the unreadable ones took it down with them", found["good"].prs)
	}
	for _, id := range []string{"loose", "gone", "unpublished", "nowhere"} {
		if in := found[id]; len(in.prs) != 0 {
			t.Errorf("%s reported pull requests: %v", id, in.prs)
		}
	}
}
