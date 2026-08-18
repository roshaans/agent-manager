package ui

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// prCreateTimeout bounds a push and the pull request that follows it. Both
// are network, and a machine that cannot reach the host should say so rather
// than leave a card up until the manager quits.
const prCreateTimeout = 2 * time.Minute

// prCreateState is the offer P makes when a session has no pull request: the
// branch that would become one, and where it would go.
type prCreateState struct {
	sessID  string
	root    string
	repoURL string
	slug    string
	branch  string
}

// prCreatedMsg ends a creation, carrying the address of what was opened or
// the reason nothing was.
type prCreatedMsg struct {
	sessID string
	pr     pullRequest
	err    error
}

// openPRCreate offers to open a pull request for the row, and to open the
// repository instead.
//
// Creating it here is the only way the link between a session and its pull
// request is ever a fact rather than a reading. Everything else this file's
// neighbours do — matching a commit, matching a branch, reading what an agent
// printed — is working out after the event what this knows at it.
func (m *Model) openPRCreate(sessID, root, repoURL, branch string) (tea.Model, tea.Cmd) {
	// A group row has no session to record against, and a detached head has
	// no branch to push. Both keep the repository page P used to give.
	if branch == "" || sessID == "" {
		return m.openPRTarget(repoURL)
	}
	m.prCreate = prCreateState{
		sessID:  sessID,
		root:    root,
		repoURL: repoURL,
		slug:    prRepoSlug(repoURL),
		branch:  branch,
	}
	m.mode = modePRCreate
	m.errBar.text = ""
	return m, nil
}

func (m *Model) handlePRCreateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		m.mode = modeList
		return m, nil
	case "r":
		m.mode = modeList
		return m.openPRTarget(m.prCreate.repoURL)
	case "enter", "c":
		state := m.prCreate
		m.mode = modeList
		m.reportDone("opening a pull request for " + state.branch + "…")
		// A push and a round trip to the host, so it waits for a command
		// rather than holding the list.
		return m, func() tea.Msg {
			pr, err := createPullRequest(state)
			return prCreatedMsg{sessID: state.sessID, pr: pr, err: err}
		}
	}
	return m, nil
}

// handlePRCreated records what was opened and shows it. The address is
// written to the session before the browser is asked for anything: the link
// is the half worth keeping, and a machine with no browser must not lose it.
func (m *Model) handlePRCreated(msg prCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.errBar.text = "opening a pull request: " + msg.err.Error()
		return m, nil
	}
	if err := m.store.SetSessionPR(msg.sessID, msg.pr.URL, prSourceCreated); err != nil {
		m.errBar.text = "recording the pull request: " + err.Error()
	}
	// Shown now rather than at the next pass, so the badge appears with the
	// pull request it belongs to.
	if m.insights == nil {
		m.insights = map[string]sessionInsight{}
	}
	insight := m.insights[msg.sessID]
	insight.prs = append([]pullRequest{msg.pr}, insight.prs...)
	m.insights[msg.sessID] = insight
	m.requestRefresh()
	return m.openPRTarget(msg.pr.URL)
}

// createPullRequest is the seam tests swap for the push and the round trip
// that follows it.
var createPullRequest = ghCreatePullRequest

// ghCreatePullRequest pushes the branch and opens a pull request for it.
//
// The push comes first because gh cannot open one for a branch the host has
// never seen, and --set-upstream leaves the branch tracking so every later
// push from the session's own shell behaves the way the reader expects.
//
// The repository is named rather than left to gh, which resolves a fork to
// its parent: a pull request belongs where the branch was pushed, and a
// reader who wanted the parent has a fork with its own pull request to send
// on. --fill takes the title and body from the commits, so nothing here
// invents prose on the reader's behalf.
func ghCreatePullRequest(state prCreateState) (pullRequest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), prCreateTimeout)
	defer cancel()

	if out, err := runIn(ctx, state.root, "git", "push", "--set-upstream", "origin", state.branch); err != nil {
		return pullRequest{}, wrapCLI("pushing "+state.branch, out, err)
	}
	args := []string{"pr", "create", "--fill", "--head", state.branch}
	if state.slug != "" {
		args = append(args, "--repo", state.slug)
	}
	if out, err := runIn(ctx, state.root, "gh", args...); err != nil {
		return pullRequest{}, wrapCLI("gh pr create", out, err)
	}
	// gh prints the address it created; the rest is read back rather than
	// parsed out of that output, so the badge carries the same fields a
	// listing would have given it.
	out, err := runIn(ctx, state.root, "gh", "pr", "view", state.branch, "--repo", state.slug, "--json", prFields)
	if err != nil {
		return pullRequest{}, wrapCLI("reading the new pull request", out, err)
	}
	var pr pullRequest
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return pullRequest{}, err
	}
	return pr, nil
}

func runIn(ctx context.Context, dir, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// wrapCLI keeps what the command said. A push refused for want of a
// credential and one refused for want of permission are different problems,
// and only the tool's own words tell them apart.
func wrapCLI(what, out string, err error) error {
	if out == "" {
		return errCLI{what: what, detail: err.Error()}
	}
	return errCLI{what: what, detail: lastLine(out)}
}

type errCLI struct{ what, detail string }

func (e errCLI) Error() string { return e.what + ": " + e.detail }

// lastLine is the end of a tool's output, which is where the reason a command
// failed is written; the lines above it are progress.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return out
}

func (m *Model) viewPRCreate() string {
	var b strings.Builder
	b.WriteString(mutedStyle.Render("No pull request for this session yet."))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("branch  ") + valueStyle.Render(m.prCreate.branch) + "\n")
	b.WriteString(labelStyle.Render("into    ") + valueStyle.Render(m.prCreate.slug) +
		subtleStyle.Render("  on its default branch") + "\n\n")
	b.WriteString(subtleStyle.Render("Pushes the branch, then opens a pull request titled from its commits."))
	b.WriteString("\n")
	b.WriteString(subtleStyle.Render("The address is recorded against this session, so the badge is a fact."))
	hint := [][2]string{{"↵", "create it"}, {"r", "open the repository"}, {"esc", "cancel"}}
	return m.card("⇅ New pull request", b.String(), hint)
}
