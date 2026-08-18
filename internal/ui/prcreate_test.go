package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// captureCreate swaps the seam that pushes and opens a pull request, so a
// test reads what would have been created without touching a host.
func captureCreate(t *testing.T, pr pullRequest, err error) *prCreateState {
	t.Helper()
	var asked prCreateState
	prev := createPullRequest
	createPullRequest = func(state prCreateState) (pullRequest, error) {
		asked = state
		return pr, err
	}
	t.Cleanup(func() { createPullRequest = prev })
	return &asked
}

// A session with no pull request is usually a session that wants one, so P
// offers rather than handing over the repository page and stopping there.
func TestPRKeyOffersToCreateWhenThereIsNone(t *testing.T) {
	m := buildModel(t)
	pretendGH(t)
	captureListing(t)
	prRepo(t, m, "git@github.com:me/fork.git")

	pressPRKey(t, m)

	if m.mode != modePRCreate {
		t.Fatalf("mode = %v, want the create card", m.mode)
	}
	card := m.viewPRCreate()
	for _, want := range []string{"main", "me/fork"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card should name %q, got:\n%s", want, card)
		}
	}
}

// The repository page P used to give is still one key away.
func TestPRCreateCardStillOpensTheRepository(t *testing.T) {
	m := buildModel(t)
	opened := captureOpenedLink(t)
	m.prCreate = prCreateState{repoURL: "https://github.com/me/fork", branch: "am/x", sessID: "s1"}
	m.mode = modePRCreate

	follow(t, m, pressPickKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}))

	if want := "https://github.com/me/fork"; *opened != want {
		t.Fatalf("opened %q, want %q", *opened, want)
	}
}

// Creating is the only moment the link is a fact rather than a reading, so
// what comes back is written to the session and shown at once.
func TestPRCreateRecordsAndOpensWhatItMade(t *testing.T) {
	m := buildModel(t)
	pretendGH(t)
	captureListing(t)
	opened := captureOpenedLink(t)
	made := pullRequest{Number: 3, URL: "https://github.com/me/fork/pull/3", State: "OPEN"}
	asked := captureCreate(t, made, nil)
	prRepo(t, m, "git@github.com:me/fork.git")
	sessID := m.sessionRows()[0].ID

	pressPRKey(t, m)
	follow(t, m, pressPickKey(t, m, tea.KeyMsg{Type: tea.KeyEnter}))

	if asked.branch != "main" || asked.slug != "me/fork" || asked.sessID != sessID {
		t.Fatalf("created with %+v, want this session's branch and repository", *asked)
	}
	if *opened != made.URL {
		t.Fatalf("opened %q, want the new pull request %q", *opened, made.URL)
	}
	// Written down, so it survives the manager quitting.
	stored, err := m.store.Get(sessID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.PRURL != made.URL {
		t.Fatalf("stored PRURL = %q, want %q", stored.PRURL, made.URL)
	}
	// And on the badge now, rather than at the next pass a minute away.
	if chip := m.prChip(m.sessionRows()[0]); !strings.Contains(chip, "#3") {
		t.Fatalf("chip = %q, want the new number straight away", chip)
	}
}

// A push refused, or a host that said no, is the only account of what went
// wrong, so it reaches the status bar rather than being swallowed.
func TestPRCreateReportsWhatTheToolSaid(t *testing.T) {
	m := buildModel(t)
	pretendGH(t)
	captureListing(t)
	captureCreate(t, pullRequest{}, errors.New("pushing am/x: permission denied"))
	prRepo(t, m, "git@github.com:me/fork.git")

	pressPRKey(t, m)
	follow(t, m, pressPickKey(t, m, tea.KeyMsg{Type: tea.KeyEnter}))

	if !strings.Contains(m.errBar.text, "permission denied") {
		t.Fatalf("status line = %q, want the tool's own words", m.errBar.text)
	}
	if m.errBar.worked() {
		t.Fatalf("a refused push should not read as an outcome, got %q", m.errBar.text)
	}
}

// A group row is a directory, not a piece of work: there is no session to
// record a pull request against, so the repository page is the whole answer.
func TestPRKeyOnAGroupStillOpensTheRepository(t *testing.T) {
	m := buildModel(t)
	pretendGH(t)
	captureListing(t)
	opened := captureOpenedLink(t)
	repo := seedRepo(t)
	remoteAt(t, repo, "git@github.com:me/fork.git")
	if err := m.store.CreateGroup("backend", repo); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	pressPRKey(t, m)

	if m.mode == modePRCreate {
		t.Fatal("a group row has no session to record a pull request against")
	}
	if want := "https://github.com/me/fork"; *opened != want {
		t.Fatalf("opened %q, want the repository %q", *opened, want)
	}
}

func TestLastLine(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"pushing\nremote: denied\nfatal: could not push\n", "fatal: could not push"},
		{"one line", "one line"},
		{"trailing\n\n\n", "trailing"},
	} {
		if got := lastLine(tc.in); got != tc.want {
			t.Fatalf("lastLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
