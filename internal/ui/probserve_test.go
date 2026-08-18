package ui

import (
	"context"
	"testing"
)

const forkRepo = "https://github.com/me/fork"

func TestObservePRReadsWhatTheSessionPrinted(t *testing.T) {
	pane := `● PR created: https://github.com/me/fork/pull/2

  - Branch test/empty-pr on me/fork, one empty commit.`

	if got := observePR(pane, []string{forkRepo}); got != "https://github.com/me/fork/pull/2" {
		t.Fatalf("observePR = %q, want the address it printed", got)
	}
}

// An agent that opened one pull request, closed it and opened another has
// said which is current by saying it later.
func TestObservePRTakesTheLastOne(t *testing.T) {
	pane := "opened https://github.com/me/fork/pull/2\nclosed it\nopened https://github.com/me/fork/pull/7\n"

	if got := observePR(pane, []string{forkRepo}); got != "https://github.com/me/fork/pull/7" {
		t.Fatalf("observePR = %q, want the last one printed", got)
	}
}

// A reader who pasted somebody else's pull request in to ask about it has not
// produced it, and it must not become this session's badge.
func TestObservePRIgnoresOtherRepositories(t *testing.T) {
	pane := "have a look at https://github.com/someone/else/pull/9 please"

	if got := observePR(pane, []string{forkRepo}); got != "" {
		t.Fatalf("observePR = %q, want nothing for another repository", got)
	}
}

// A fork's work is opened upstream as often as on the fork itself, and the
// parent is as much this session's repository as its own remote is.
func TestObservePRAcceptsTheParentRepository(t *testing.T) {
	pane := "PR created: https://github.com/upstream/project/pull/328"
	allowed := []string{forkRepo, "https://github.com/upstream/project"}

	if got := observePR(pane, allowed); got != "https://github.com/upstream/project/pull/328" {
		t.Fatalf("observePR = %q, want the upstream pull request", got)
	}
}

// Prose puts punctuation against a URL, and a markdown link wraps one in
// brackets. Neither is part of the address.
func TestObservePRTrimsTrailingPunctuation(t *testing.T) {
	for _, pane := range []string{
		"opened https://github.com/me/fork/pull/2.",
		"see [the PR](https://github.com/me/fork/pull/2)",
		"opened https://github.com/me/fork/pull/2/",
		"opened <https://github.com/me/fork/pull/2>, then waited",
	} {
		if got := observePR(pane, []string{forkRepo}); got != "https://github.com/me/fork/pull/2" {
			t.Fatalf("observePR(%q) = %q", pane, got)
		}
	}
}

func TestObservePRWithNothingToRead(t *testing.T) {
	if got := observePR("", []string{forkRepo}); got != "" {
		t.Fatalf("observePR = %q, want nothing from an empty pane", got)
	}
	if got := observePR("a busy pane with no links", nil); got != "" {
		t.Fatalf("observePR = %q, want nothing when no repository is allowed", got)
	}
}

// The whole point of the observed link: the session's checkout is on one
// branch and the pull request it produced is on another, so nothing about the
// branch could have found it.
func TestScanFindsAPullRequestOffTheSessionsBranch(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	elsewhere := pullRequest{
		Number: 2, URL: "https://github.com/me/fork/pull/2",
		Head: "test/empty-pr", State: "OPEN",
	}
	prev := ghListRun
	ghListRun = func(context.Context, string, string) []pullRequest { return []pullRequest{elsewhere} }
	t.Cleanup(func() { ghListRun = prev })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")
	pane := func(string) string { return "PR created: https://github.com/me/fork/pull/2" }

	found, links := scanPullRequests(drv, pane, []prScanTarget{{sessID: "s1", dir: repo}})

	if len(found["s1"]) != 1 || found["s1"][0].Number != 2 {
		t.Fatalf("found %v, want the pull request the session printed", found["s1"])
	}
	if links["s1"] != elsewhere.URL {
		t.Fatalf("links = %v, want the address recorded for the session", links)
	}
}

// A link established earlier keeps answering once it has scrolled out of the
// pane, which is why it is written down at all.
func TestScanKeepsARecordedLinkAfterItScrollsAway(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	recorded := "https://github.com/me/fork/pull/2"
	prev := ghListRun
	ghListRun = func(context.Context, string, string) []pullRequest { return nil }
	prevView := viewPullRequest
	viewPullRequest = func(_ context.Context, _, prURL string) pullRequest {
		return pullRequest{Number: 2, URL: prURL, State: "OPEN", Head: "test/empty-pr"}
	}
	t.Cleanup(func() { ghListRun, viewPullRequest = prev, prevView })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, links := scanPullRequests(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo, prURL: recorded, prSource: prSourceCreated},
	})

	if len(found["s1"]) != 1 || found["s1"][0].URL != recorded {
		t.Fatalf("found %v, want the recorded pull request", found["s1"])
	}
	if len(links) != 0 {
		t.Fatalf("links = %v, want nothing rewritten when nothing changed", links)
	}
}

// Work that has landed is not work in flight, so a merged pull request stops
// wearing a badge even though the session still remembers producing it.
func TestScanDropsAMergedRecordedLink(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	prev := ghListRun
	ghListRun = func(context.Context, string, string) []pullRequest { return nil }
	prevView := viewPullRequest
	viewPullRequest = func(_ context.Context, _, prURL string) pullRequest {
		return pullRequest{Number: 2, URL: prURL, State: "MERGED"}
	}
	t.Cleanup(func() { ghListRun, viewPullRequest = prev, prevView })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")

	found, _ := scanPullRequests(drv, noPane, []prScanTarget{
		{sessID: "s1", dir: repo, prURL: "https://github.com/me/fork/pull/2", prSource: prSourceCreated},
	})

	if len(found) != 0 {
		t.Fatalf("found %v, want nothing for a merged pull request", found)
	}
}

// A session that opened a pull request elsewhere has not stopped working on
// the branch it is sitting on, so it keeps both — the branch first, because
// the branch is a thing this manager can check and the printed address is a
// thing it can only believe.
func TestScanKeepsTheBranchPullRequestBesideThePrintedOne(t *testing.T) {
	drv := scanDriver(t)
	pretendGH(t)
	onBranch := pullRequest{Number: 1, URL: "https://github.com/me/fork/pull/1", Head: "main", State: "OPEN"}
	elsewhere := pullRequest{Number: 2, URL: "https://github.com/me/fork/pull/2", Head: "test/empty-pr", State: "OPEN"}
	prev := ghListRun
	ghListRun = func(context.Context, string, string) []pullRequest {
		return []pullRequest{elsewhere, onBranch}
	}
	t.Cleanup(func() { ghListRun = prev })
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")
	pane := func(string) string { return "PR created: " + elsewhere.URL }

	found, _ := scanPullRequests(drv, pane, []prScanTarget{{sessID: "s1", dir: repo}})

	if len(found["s1"]) != 2 {
		t.Fatalf("found %v, want both", found["s1"])
	}
	if found["s1"][0].Number != 1 || found["s1"][1].Number != 2 {
		t.Fatalf("found %v, want the branch first and the printed one after", found["s1"])
	}
}
