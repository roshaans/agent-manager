package ui

import (
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"strconv"
	"time"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// pullRequest is one open pull request, in as much detail as a row badge and
// the picker behind P need to tell one from another.
type pullRequest struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	IsDraft bool   `json:"isDraft"`
	State   string `json:"state"`
	Head    string `json:"headRefName"`
	Base    string `json:"baseRefName"`
}

// prFields is what both the listing and a single lookup ask gh for, kept in
// one place so a badge reads the same whichever of the two found it.
const prFields = "number,url,title,isDraft,state,headRefName,baseRefName"

// open reports whether a pull request is still in flight. A listing only ever
// carries open ones; a pull request found by its recorded address can be any
// state, and one already merged is not something to badge as live work.
func (pr pullRequest) open() bool { return pr.State == "" || pr.State == "OPEN" }

const (
	// prScanTimeout bounds a whole pass. A pass that hangs is one that would
	// otherwise hold its repositories' listings until the manager quits.
	prScanTimeout = 30 * time.Second
	// prListLimit caps one repository's listing. Past this many open pull
	// requests, the ones a session's branch is on are no longer findable by
	// reading them all, and nothing on screen is worth the extra pages.
	prListLimit = "100"
	// prDraftMark rides after the number on a draft, since the dimmed chip
	// alone says "draft" only to a reader who already knows it does.
	//
	// A pencil rather than an arrow. ↑ and ↓ mean "move the cursor" on every
	// other row of this app, and beside a git-adjacent number an up arrow
	// reads as ahead-of-upstream — which is a thing this badge does not say,
	// and a thing a later badge may well want to.
	prDraftMark = "✎"
)

// How a recorded link was arrived at, which decides whether it leads or
// trails the sources that read git rather than a screen.
const (
	prSourceCreated  = "created"
	prSourceObserved = "observed"
)

// prScanInterval is how often the badges are re-read. A pull request opened
// elsewhere is worth showing within the minute; asking any harder than that
// spends a reader's API budget on a number that rarely moves.
//
// prScanFirstDelay lets the first poll fill the session list before the first
// scan reads it, so startup does not spend a pass on no rows. A variable so a
// test can arm them without waiting the real delays out.
var (
	prScanInterval   = time.Minute
	prScanFirstDelay = 3 * time.Second
)

// sessionInsight is everything one pass learned about a session. A pass
// already resolves each session to a checkout and each checkout to a
// repository, and caches both; anything else worth showing on a row costs a
// field here rather than a timer and a set of lookups of its own.
type sessionInsight struct {
	prs []pullRequest
	// ahead and behind are the checkout against its remote branch. synced
	// says the two were worked out at all: a branch nobody has pushed has
	// nothing to compare against, and "in step" is not the same as "no idea".
	ahead, behind int
	synced        bool
}

func (in sessionInsight) empty() bool { return len(in.prs) == 0 && !in.synced }

type prScanTickMsg struct{}

// prScanMsg carries a finished pass: each session's pull requests, and the
// links a pass newly observed, which Update writes to the store so they
// outlive the manager run that noticed them.
type prScanMsg struct {
	insights map[string]sessionInsight
	links    map[string]string
	// ran reports that the pass actually reached the host. A pass that could
	// not is not evidence that a session has no pull request, and must not be
	// allowed to clear what an earlier one established.
	ran bool
}

func (m *Model) prScanTick() tea.Cmd {
	return tea.Tick(prScanInterval, func(time.Time) tea.Msg { return prScanTickMsg{} })
}

func (m *Model) prScanSoon() tea.Cmd {
	return tea.Tick(prScanFirstDelay, func(time.Time) tea.Msg { return prScanTickMsg{} })
}

// prScanTarget is one session's place in a pass: where it works, and the
// branch the manager cut for it, which stands in when the checkout itself
// cannot name one.
type prScanTarget struct {
	sessID string
	dir    string
	branch string
	// prURL is the link already recorded for this session, so a pass that
	// finds nothing new keeps showing what an earlier one established, and
	// prSource is how it was arrived at.
	prURL    string
	prSource string
}

// prScanCmd reads the rows a pass covers on the event loop and hands the slow
// half — git, then the network — to a command.
//
// Shells are left out. One opened for a session shares that session's
// checkout, so badging it too would print the same pull request twice on two
// rows that sit one under the other.
func (m *Model) prScanCmd() tea.Cmd {
	targets := m.prScanTargets()
	if m.gitDrv == nil || len(targets) == 0 {
		return nil
	}
	drv, capture := m.gitDrv, m.paneHistory
	return func() tea.Msg {
		insights, links, ran := scanSessions(drv, capture, targets)
		return prScanMsg{insights: insights, links: links, ran: ran}
	}
}

// paneHistory is what a session has printed. A pane that has gone, or a tmux
// that will not answer, reads as nothing printed: a scan is a background pass
// over rows that may be dead, and a missing pane is the normal case rather
// than a failure worth reporting.
func (m *Model) paneHistory(sessID string) string {
	text, err := m.tmux.CaptureHistory(sessID, prScrollback)
	if err != nil {
		return ""
	}
	return text
}

func (m *Model) prScanTargets() []prScanTarget {
	targets := make([]prScanTarget, 0, len(m.sessions))
	for _, sess := range m.sessions {
		if sess.Archived || sess.Cwd == "" || m.isShell(sess.Tool) {
			continue
		}
		targets = append(targets, prScanTarget{
			sessID:   sess.ID,
			dir:      sess.Cwd,
			branch:   sess.WorktreeBranch,
			prURL:    sess.PRURL,
			prSource: sess.PRSource,
		})
	}
	return targets
}

// prPlace is what a directory turns out to be: the checkout it belongs to,
// the branch and commit that checkout is on, and the web address of the
// repository it pushes to.
type prPlace struct{ root, branch, head, repoURL string }

// scanPullRequests is a whole pass, off the event loop.
//
// The listings are cached by remote rather than by checkout, so a repository
// with six worktrees under six sessions costs one request and not six. That
// is the whole reason the badge reads a repository's open pull requests
// instead of asking after each branch in turn.
func scanSessions(drv *git.Driver, capture func(string) string, targets []prScanTarget) (map[string]sessionInsight, map[string]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), prScanTimeout)
	defer cancel()

	// gh gates the pull requests and nothing else. Ahead and behind come out
	// of git, and a machine without gh installed still deserves them.
	_, ghErr := lookPath("gh")

	places := map[string]prPlace{}
	listings := map[string][]pullRequest{}
	viewed := map[string]pullRequest{}
	commits := map[string][]pullRequest{}
	fetched := map[string]bool{}
	sync := map[string]sessionInsight{}
	found := map[string]sessionInsight{}
	links := map[string]string{}
	for _, target := range targets {
		place, known := places[target.dir]
		if !known {
			place = locatePlace(drv, target.dir)
			places[target.dir] = place
		}
		if place.root == "" {
			continue
		}

		var insight sessionInsight
		// One fetch per repository, not per session: six worktrees under six
		// sessions are six checkouts of one remote, and the answer is the
		// same for all of them.
		if cached, known := sync[place.root]; known {
			insight = cached
		} else {
			if !fetched[place.root] {
				fetched[place.root] = true
				fetchRemote(drv, place.root)
			}
			if ahead, behind, err := drv.AheadBehind(place.root); err == nil {
				insight.ahead, insight.behind, insight.synced = ahead, behind, true
			}
			sync[place.root] = insight
		}

		if place.repoURL == "" || ghErr != nil {
			if !insight.empty() {
				found[target.sessID] = insight
			}
			continue
		}
		listing, known := listings[place.repoURL]
		if !known {
			listing = listPullRequests(ctx, place.root, place.repoURL)
			listings[place.repoURL] = listing
		}

		// Sources in descending order of how much they know, each adding
		// what the ones before it missed rather than replacing them:
		//
		//  1. created — this manager opened the pull request. A fact.
		//  2. commit — the session's own commit is in the pull request.
		//     A git fact, and the one branch names get wrong.
		//  3. branch — the checkout sits on the pull request's head branch.
		//     A label, and only as good as labels are.
		//  4. printed — an address the session put on screen. A reading,
		//     and last, because a session asked to look at somebody else's
		//     pull request prints that one too. It must never displace a
		//     commit that says otherwise.
		var prs []pullRequest
		seen := map[string]bool{}
		add := func(candidates ...pullRequest) {
			for _, pr := range candidates {
				if pr.URL == "" || seen[pr.URL] || !pr.open() {
					continue
				}
				seen[pr.URL] = true
				prs = append(prs, pr)
			}
		}

		// A link this manager opened leads; one merely printed waits its
		// turn at the end.
		created, printed := "", ""
		if target.prSource == prSourceCreated {
			created = target.prURL
		} else {
			printed = target.prURL
		}
		if seen := observePR(capture(target.sessID), allowedRepos(place.repoURL, listing)); seen != "" && seen != target.prURL {
			printed, links[target.sessID] = seen, seen
		}
		if created != "" {
			add(resolveRecorded(ctx, place.root, created, listing, viewed))
		}

		slug := prRepoSlug(place.repoURL)
		byCommit, known := commits[slug+" "+place.head]
		if !known {
			byCommit = commitPullRequests(ctx, place.root, slug, place.head)
			commits[slug+" "+place.head] = byCommit
		}
		add(byCommit...)

		branch := place.branch
		if branch == "" {
			branch = target.branch
		}
		// The branch joins rather than losing: a session that opened a pull
		// request somewhere else has not stopped working on the branch it is
		// sitting on.
		if branch != "" {
			for _, pr := range listing {
				if pr.Head == branch {
					add(pr)
				}
			}
		}

		if printed != "" {
			add(resolveRecorded(ctx, place.root, printed, listing, viewed))
		}
		insight.prs = prs
		if !insight.empty() {
			found[target.sessID] = insight
		}
	}
	// A pass cut short by its own deadline has read some repositories and not
	// others, and the ones it missed look empty rather than unread. gh missing
	// is the same kind of gap: the git half still ran, but nothing can be
	// concluded about pull requests from a pass that never asked.
	return found, links, ctx.Err() == nil && ghErr == nil
}

// allowedRepos is which repositories an address printed in a session may name
// and still be that session's own work: the repository it pushes to, and
// whatever the listing came back from, which for a fork is its parent too.
func allowedRepos(repoURL string, listing []pullRequest) []string {
	allowed := []string{repoURL}
	for _, pr := range listing {
		if repo := prRepoOf(pr.URL); repo != "" && !slices.Contains(allowed, repo) {
			allowed = append(allowed, repo)
		}
	}
	return allowed
}

// resolveRecorded is a recorded address turned back into a pull request, in
// as much detail as can be had.
//
// A link that was recorded is a fact about this session, and stays one when
// the host cannot be reached to say more about it — offline, rate limited,
// signed out, or behind a gh that has no credentials in this environment.
// The number lives in the address itself, so the badge still answers and P
// still opens the right page; only the title and the draft mark are missing
// until a later pass can fetch them.
//
// A pull request that gh *can* answer for and reports as merged is a
// different thing: that is known, not unknown, and the caller drops it.
func resolveRecorded(ctx context.Context, root, prURL string, listing []pullRequest, viewed map[string]pullRequest) pullRequest {
	if pr, ok := lookupPR(ctx, root, prURL, listing, viewed); ok {
		return pr
	}
	return pullRequest{URL: prURL, Number: prNumberOf(prURL)}
}

// lookupPR is a recorded address turned back into a pull request: from the
// listing when it is still open, and from gh directly when it is not, since a
// listing of open pull requests cannot answer for one already merged.
func lookupPR(ctx context.Context, root, prURL string, listing []pullRequest, viewed map[string]pullRequest) (pullRequest, bool) {
	for _, pr := range listing {
		if pr.URL == prURL {
			return pr, true
		}
	}
	if pr, known := viewed[prURL]; known {
		return pr, pr.URL != ""
	}
	pr := viewPullRequest(ctx, root, prURL)
	viewed[prURL] = pr
	return pr, pr.URL != ""
}

// locatePlace resolves a session's directory to the checkout, branch and
// repository a pull request would be found through. A directory outside a
// repository, one nobody published, or one on a detached head all resolve to
// nothing to look up.
func locatePlace(drv *git.Driver, dir string) prPlace {
	root, err := drv.RepoRoot(dir)
	if err != nil {
		return prPlace{}
	}
	place := prPlace{root: root}
	if remote, err := drv.RemoteURL(root); err == nil {
		place.repoURL, _ = git.WebURL(remote)
	}
	if repo, err := drv.OpenRepo(root); err == nil && !repo.Detached && !repo.Unborn {
		place.branch = repo.Branch
	}
	if head, err := drv.HeadSHA(root); err == nil {
		place.head = head
	}
	return place
}

// commitPullRequests is the seam tests swap for asking which pull requests
// contain a commit.
var commitPullRequests = ghCommitPullRequests

// commitPullsShape rewrites the API's own field names into the ones a listing
// uses, so a pull request reads the same whichever call found it.
const commitPullsShape = `[.[] | {number, url: .html_url, title, isDraft: .draft, ` +
	`state: (.state|ascii_upcase), headRefName: .head.ref, baseRefName: .base.ref}]`

// ghCommitPullRequests asks which pull requests contain a commit.
//
// This is the identification a branch's name cannot give. A name is a label
// that two forks can both be using and that a rename detaches from the pull
// request it belonged to; a commit either is in a pull request or is not, and
// the answer comes from the same place the pull request does.
//
// Only a pushed commit resolves. One that exists nowhere but this machine
// answers 404, which is the right answer: nobody could have opened a pull
// request for work they have never seen.
func ghCommitPullRequests(ctx context.Context, root, slug, sha string) []pullRequest {
	if slug == "" || sha == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "gh", "api",
		"repos/"+slug+"/commits/"+sha+"/pulls", "--jq", commitPullsShape)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var prs []pullRequest
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil
	}
	return prs
}

// listPullRequests is the seam tests swap for the real gh call.
var listPullRequests = ghOpenPullRequests

// ghOpenPullRequests lists the open pull requests a checkout's branches could
// be on. Every way it can come up short — no gh, no network, a host gh does
// not know, a repository nobody here has a token for — means the same thing
// to a row, which is that it wears no badge.
//
// Branches are matched by name against this listing rather than asked after
// one at a time, because gh names a branch given as an argument against the
// repository a pull request would merge into, and so never finds work pushed
// to a fork.
//
// Two listings, because a fork holds pull requests either way round: one
// opened against the fork itself lives on the fork, one opened upstream lives
// on the parent, and gh's own repository resolution only ever answers with
// the parent. Naming the remote finds the first, letting gh resolve finds the
// second, and a checkout that is nobody's fork answers both with the same
// repository, where the merge drops the repeats. The remote's own listing
// goes first: it is the repository the branch was actually pushed to.
func ghOpenPullRequests(ctx context.Context, root, repoURL string) []pullRequest {
	seen := map[string]bool{}
	var found []pullRequest
	for _, repo := range []string{repoURL, ""} {
		for _, pr := range ghListRun(ctx, root, repo) {
			if seen[pr.URL] {
				continue
			}
			seen[pr.URL] = true
			found = append(found, pr)
		}
	}
	return found
}

// ghListRun is the seam tests swap to observe the two listings separately.
var ghListRun = ghListOnce

// viewPullRequest is the seam tests swap for reading one pull request by its
// address.
var viewPullRequest = ghViewPullRequest

// ghViewPullRequest reads a single pull request, for a recorded address the
// open listing cannot account for. A pull request that has been merged or
// closed still answers here, which is how a session keeps naming what it
// produced after the work has landed.
func ghViewPullRequest(ctx context.Context, root, prURL string) pullRequest {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prURL, "--json", prFields)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return pullRequest{}
	}
	var pr pullRequest
	if err := json.Unmarshal(out, &pr); err != nil {
		return pullRequest{}
	}
	return pr
}

func ghListOnce(ctx context.Context, root, repo string) []pullRequest {
	args := []string{"pr", "list", "--state", "open", "--limit", prListLimit,
		"--json", prFields}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var prs []pullRequest
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil
	}
	return prs
}

// pullRequestsOn is one branch's open pull requests, for the key that cannot
// wait for the next pass to have covered the row it is pressed on.
func pullRequestsOn(ctx context.Context, root, repoURL, branch string) []pullRequest {
	if branch == "" {
		return nil
	}
	if _, err := lookPath("gh"); err != nil {
		return nil
	}
	var found []pullRequest
	for _, pr := range listPullRequests(ctx, root, repoURL) {
		if pr.Head == branch {
			found = append(found, pr)
		}
	}
	return found
}

// prChip is the badge a row wears when its branch has an open pull request:
// the number, dimmed and marked when that pull request is still a draft, and
// how many others there are when the branch carries more than one.
func (m *Model) prChip(sess store.Session) string {
	prs := m.insights[sess.ID].prs
	if len(prs) == 0 {
		return ""
	}
	pr := prs[0]
	label := "#" + strconv.Itoa(pr.Number)
	if pr.IsDraft {
		label += prDraftMark
	}
	if extra := len(prs) - 1; extra > 0 {
		label += "+" + strconv.Itoa(extra)
	}
	if pr.IsDraft {
		return pill(label, colorSubtle)
	}
	return pill(label, colorAccent2)
}

// fetchRemote is the seam tests swap, so a pass under test never reaches a
// network and never writes to a repository it did not create.
var fetchRemote = func(drv *git.Driver, root string) { drv.Fetch(root) }

// syncChip is what a session's checkout owes its remote: work it has not
// pushed, work it has not pulled, or both.
//
// Behind is the half worth colouring for attention. Unpushed work is normal —
// it is what a session in progress looks like — where a checkout that has
// fallen behind is about to produce a conflict nobody has noticed yet.
//
// A branch with no upstream shows nothing at all. "In step" and "nothing to
// compare against" are different answers, and a zero would report the second
// as the first.
func (m *Model) syncChip(sess store.Session) string {
	in := m.insights[sess.ID]
	if !in.synced || (in.ahead == 0 && in.behind == 0) {
		return ""
	}
	label := ""
	if in.ahead > 0 {
		label += "↑" + strconv.Itoa(in.ahead)
	}
	if in.behind > 0 {
		if label != "" {
			label += " "
		}
		label += "↓" + strconv.Itoa(in.behind)
	}
	if in.behind > 0 {
		return pill(label, colorWaiting)
	}
	return pill(label, colorSubtle)
}
