package ui

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/git"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// prLookupTimeout bounds the lookup a cold badge falls back on. Past it the
// repository's own page is the answer: the key is pressed for a quick look,
// and a look that keeps the list waiting is worth less than one that lands.
const prLookupTimeout = 5 * time.Second

// prTitleWidth keeps a long pull request title from pushing the base branch
// off the card.
const prTitleWidth = 48

// prLinkMsg carries what P resolved to once the lookup that needed the
// network has come back: the branch's pull requests, and the repository page
// that answers when it has none.
type prLinkMsg struct {
	prs  []pullRequest
	repo string
}

// prPickState is the chooser P opens when a branch carries more than one
// open pull request — the same branch merged into a release line as well as
// into main, or one reopened beside another still in flight.
type prPickState struct {
	prs    []pullRequest
	repo   string
	cursor int
}

// prKey opens what a session produced, on the host its repository is
// published to: the branch's pull request when it has one, and the
// repository's own page when it does not.
//
// One key rather than two, because "show me this branch on the remote" is the
// same wish either side of the moment a pull request gets opened, and nobody
// wants to remember which side of it they are on before pressing.
//
// The row's live directory decides the repository, as it does for G, so a
// session in a worktree answers with its own branch rather than with the
// branch of the repository it was cut from.
func (m *Model) prKey() (tea.Model, tea.Cmd) {
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to open a remote for: " + dir
		return m, nil
	}
	root, err := m.gitRoot(dir)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	remote, err := m.gitDrv.RemoteURL(root)
	if err != nil || remote == "" {
		m.errBar.text = "no remote to open: this repository is not published anywhere"
		return m, nil
	}
	web, err := git.WebURL(remote)
	if err != nil {
		m.errBar.text = "no web page for the remote " + remote
		return m, nil
	}
	m.errBar.text = ""
	// The badge scan has usually already answered for this row, and a key
	// pressed for a quick look should not wait on the network to be told
	// again what is already on screen.
	if prs := m.rowPullRequests(); len(prs) > 0 {
		return m.resolvePRLink(prLinkMsg{prs: prs, repo: web})
	}

	repo, err := m.gitDrv.OpenRepo(root)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	branch := repo.Branch
	if repo.Detached || repo.Unborn {
		branch = ""
	}
	// A row the scan has not covered yet — one spawned seconds ago, or a
	// group — asks for itself, which is a network round trip and so waits
	// for a command instead of holding the keystroke.
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), prLookupTimeout)
		defer cancel()
		return prLinkMsg{prs: pullRequestsOn(ctx, root, web, branch), repo: web}
	}
}

// rowPullRequests is what the last scan found for the row under the cursor.
// A group row has no branch of its own, so it has none.
//
// A session's recorded address answers when the scan has nothing: in the
// seconds before the first pass of a run, and for a pull request already
// merged, which stops being a badge — that badge is for work in flight — but
// does not stop being the pull request this session produced.
func (m *Model) rowPullRequests() []pullRequest {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup {
		return nil
	}
	if prs := m.prs[entry.sess.ID]; len(prs) > 0 {
		return prs
	}
	if entry.sess.PRURL != "" {
		return []pullRequest{{URL: entry.sess.PRURL, Number: prNumberOf(entry.sess.PRURL)}}
	}
	return nil
}

// resolvePRLink turns what was found into what happens: nothing opens the
// repository, one opens itself, and several open the chooser rather than
// picking for the reader.
func (m *Model) resolvePRLink(msg prLinkMsg) (tea.Model, tea.Cmd) {
	switch len(msg.prs) {
	case 0:
		return m.openPRTarget(msg.repo)
	case 1:
		return m.openPRTarget(msg.prs[0].URL)
	}
	m.prPick = prPickState{prs: msg.prs, repo: msg.repo}
	m.mode = modePRPick
	m.errBar.text = ""
	return m, nil
}

// openPRTarget hands a URL to the browser and names it, since the URL is what
// says which of the two answers came back, and for which repository.
func (m *Model) openPRTarget(target string) (tea.Model, tea.Cmd) {
	m.reportDone("opening " + target)
	return m, openLink(target)
}

func (m *Model) handlePRPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		m.mode = modeList
		return m, nil
	case "up", "k":
		if m.prPick.cursor > 0 {
			m.prPick.cursor--
		}
	case "down", "j":
		if m.prPick.cursor < len(m.prPick.prs)-1 {
			m.prPick.cursor++
		}
	case "r":
		// The chooser stands in for the fallback P would otherwise have
		// taken, so the repository it replaced stays reachable from it.
		m.mode = modeList
		return m.openPRTarget(m.prPick.repo)
	case "enter":
		if m.prPick.cursor < len(m.prPick.prs) {
			m.mode = modeList
			return m.openPRTarget(m.prPick.prs[m.prPick.cursor].URL)
		}
	}
	return m, nil
}

func (m *Model) viewPRPick() string {
	var b strings.Builder
	for i, pr := range m.prPick.prs {
		marker := "  "
		numberStyle := valueStyle
		if m.prPick.cursor == i {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			numberStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		b.WriteString(marker)
		b.WriteString(numberStyle.Render("#" + strconv.Itoa(pr.Number)))
		if pr.IsDraft {
			b.WriteString(subtleStyle.Render(" draft"))
		}
		b.WriteString("  " + valueStyle.Render(ansi.Truncate(firstLine(pr.Title), prTitleWidth, "…")))
		// The base branch is what tells two pull requests off the same branch
		// apart, which is the only reason this card is on screen.
		b.WriteString(mutedStyle.Render("  → " + pr.Base))
		b.WriteByte('\n')
	}
	hint := [][2]string{{"↑↓", "move"}, {"↵", "open"}, {"r", "repository"}, {"esc", "back"}}
	return m.card("⇅ Pull requests", strings.TrimRight(b.String(), "\n"), hint)
}
