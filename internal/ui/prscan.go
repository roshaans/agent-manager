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
	// prScanInterval is how often the badges are re-read. A pull request
	// opened elsewhere is worth showing within the minute; asking any harder
	// than that spends a reader's API budget on a number that rarely moves.
	prScanInterval = time.Minute
	// prScanFirstDelay lets the first poll fill the session list before the
	// first scan reads it, so startup does not spend a pass on no rows.
	prScanFirstDelay = 3 * time.Second
	// prScanTimeout bounds a whole pass. A pass that hangs is one that would
	// otherwise hold its repositories' listings until the manager quits.
	prScanTimeout = 30 * time.Second
	// prListLimit caps one repository's listing. Past this many open pull
	// requests, the ones a session's branch is on are no longer findable by
	// reading them all, and nothing on screen is worth the extra pages.
	prListLimit = "100"
	// prDraftMark rides after the number on a draft, since the dimmed chip
	// alone says "draft" only to a reader who already knows it does.
	prDraftMark = "↑"
)

type prScanTickMsg struct{}

// prScanMsg carries a finished pass: each session's pull requests, and the
// links a pass newly observed, which Update writes to the store so they
// outlive the manager run that noticed them.
type prScanMsg struct {
	prs   map[string][]pullRequest
	links map[string]string
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
	// finds nothing new keeps showing what an earlier one established.
	prURL string
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
		prs, links := scanPullRequests(drv, capture, targets)
		return prScanMsg{prs: prs, links: links}
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
			sessID: sess.ID,
			dir:    sess.Cwd,
			branch: sess.WorktreeBranch,
			prURL:  sess.PRURL,
		})
	}
	return targets
}

// prPlace is what a directory turns out to be: the checkout it belongs to,
// the branch that checkout is on, and the web address of the repository it
// pushes to.
type prPlace struct{ root, branch, repoURL string }

// scanPullRequests is a whole pass, off the event loop.
//
// The listings are cached by remote rather than by checkout, so a repository
// with six worktrees under six sessions costs one request and not six. That
// is the whole reason the badge reads a repository's open pull requests
// instead of asking after each branch in turn.
func scanPullRequests(drv *git.Driver, capture func(string) string, targets []prScanTarget) (map[string][]pullRequest, map[string]string) {
	if _, err := lookPath("gh"); err != nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), prScanTimeout)
	defer cancel()

	places := map[string]prPlace{}
	listings := map[string][]pullRequest{}
	viewed := map[string]pullRequest{}
	found := map[string][]pullRequest{}
	links := map[string]string{}
	for _, target := range targets {
		place, known := places[target.dir]
		if !known {
			place = locatePlace(drv, target.dir)
			places[target.dir] = place
		}
		if place.repoURL == "" {
			continue
		}
		listing, known := listings[place.repoURL]
		if !known {
			listing = listPullRequests(ctx, place.root, place.repoURL)
			listings[place.repoURL] = listing
		}

		// The recorded link comes first, and a fresher one printed since
		// replaces it. Both outrank the branch, which is a guess about what
		// the session is doing where these two are a record of what it did.
		link := target.prURL
		if seen := observePR(capture(target.sessID), allowedRepos(place.repoURL, listing)); seen != "" && seen != link {
			link, links[target.sessID] = seen, seen
		}
		var prs []pullRequest
		if link != "" {
			if pr, ok := lookupPR(ctx, place.root, link, listing, viewed); ok && pr.open() {
				prs = append(prs, pr)
			}
		}

		branch := place.branch
		if branch == "" {
			branch = target.branch
		}
		// The branch's own pull requests join the recorded one rather than
		// losing to it: a session that opened one somewhere else has not
		// stopped working on the branch it is sitting on.
		if branch != "" {
			for _, pr := range listing {
				if pr.Head == branch && pr.URL != link {
					prs = append(prs, pr)
				}
			}
		}
		if len(prs) > 0 {
			found[target.sessID] = prs
		}
	}
	return found, links
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
	return place
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
	prs := m.prs[sess.ID]
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
