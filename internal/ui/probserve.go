package ui

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// prScrollback is how far back a pass reads for a pull request an agent
// printed. Deep enough to survive a long turn's output on top of it, shallow
// enough that reading every session costs a fraction of a second.
const prScrollback = 2000

// prURLPattern is a pull request address as an agent prints one, on any host
// that lays its pull requests out the way GitHub does. The trailing boundary
// keeps a URL that runs into punctuation — a full stop, a closing bracket,
// the ")" of a markdown link — from taking those characters with it.
var prURLPattern = regexp.MustCompile(`https?://[\w.-]+(?:/[\w.-]+){2}/pull/\d+`)

// observePR is the pull request a session produced, read out of what it
// printed.
//
// No git fact ties a session to a pull request. An agent that pushes a branch
// it never checks out — which is what a careful one does, so the working tree
// it was given stays as the reader left it — leaves the checkout on a branch
// that has nothing to do with the pull request it just opened. What it does
// leave behind is the address, printed where the reader can see it, and that
// is the record this reads.
//
// The last address in the pane wins: an agent that opened one pull request,
// closed it and opened another has said which is current by saying it later.
//
// Only addresses on the session's own host count, and only ones whose
// repository is the session's or its parent. A reader who pasted somebody
// else's pull request in to ask about it has not produced it, and the check
// is what keeps that out of the badge.
func observePR(pane string, allowed []string) string {
	if pane == "" || len(allowed) == 0 {
		return ""
	}
	matches := prURLPattern.FindAllString(pane, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		found := strings.TrimSuffix(matches[i], "/")
		for _, repo := range allowed {
			if prRepoOf(found) == repo {
				return found
			}
		}
	}
	return ""
}

// prRepoSlug is the owner and name a repository address carries, which is
// what an API path is built from.
func prRepoSlug(repoURL string) string {
	parsed, err := url.Parse(repoURL)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.Trim(parsed.Path, "/")
}

// prNumberOf is the number at the end of a pull request address, for a
// recorded link nothing has yet been read back about.
func prNumberOf(prURL string) int {
	_, tail, found := strings.Cut(prURL, "/pull/")
	if !found {
		return 0
	}
	number, err := strconv.Atoi(strings.TrimSuffix(tail, "/"))
	if err != nil {
		return 0
	}
	return number
}

// prRepoOf is the repository half of a pull request address — scheme, host,
// owner and name, without the pull request itself — normalised so an http
// address and an https one compare equal against a repository read from a
// remote.
func prRepoOf(prURL string) string {
	parsed, err := url.Parse(prURL)
	if err != nil || parsed.Host == "" {
		return ""
	}
	repo, _, found := strings.Cut(parsed.Path, "/pull/")
	if !found {
		return ""
	}
	return "https://" + parsed.Host + repo
}
