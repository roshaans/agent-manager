package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/YoanWai/agent-manager/internal/rules"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The rules surface answers a question the list otherwise cannot: what has
// this agent already been told? Every CLI reads a file before its first turn
// — CLAUDE.md, AGENTS.md, GEMINI.md — and which one is a property of the tool
// the session runs, so i shows the files that session obeys rather than a
// fixed name. The manager already knows the tool and the directory, which is
// exactly what turning "somewhere in the repo" into a path takes.
//
// Both halves are here because inspecting and editing are one errand: you
// open the file to find out what it says, and half the time what you find is
// a line you want to change.

// rulesState is the i surface: the resolved file list, and the file opened
// out of it. body caches the wrapped render against the width it was made
// for, so scrolling a long file does not re-wrap it every frame while a
// resize still does.
type rulesState struct {
	// tool is the CLI the list was resolved for, empty when the cursor sat
	// on a group or a shell and the list is whatever exists.
	tool   string
	dir    string
	files  []rules.File
	cursor int

	label     string
	text      string
	truncated bool
	scroll    int
	body      []string
	bodyWidth int
}

// rulesKey opens the instruction files the row under the cursor is governed
// by. A file that does not exist yet is listed too: "this project tells the
// agent nothing" is an answer, and the row is where creating one starts.
func (m *Model) rulesKey() (tea.Model, tea.Cmd) {
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to read rules for: " + dir
		return m, nil
	}
	root := m.repoRootOf(dir)
	tool, files := m.rulesFiles(dir, root)
	if len(files) == 0 {
		m.errBar.text = "no rules files configured: add rules_files to the tool block in config.toml"
		return m, nil
	}
	m.rules = rulesState{tool: tool, dir: dir, files: files}
	m.mode = modeRulesPick
	m.errBar.text = ""
	return m, nil
}

// rulesFiles resolves what the row under the cursor reads, and the tool that
// reads it.
//
// A session names its own tool, so its list is that tool's files whether or
// not they have been written — an agent with no CLAUDE.md is the case worth
// showing. A group or a shell names no tool, so there is nothing to offer
// creating; its list is every instruction file that actually exists for any
// configured CLI, with AGENTS.md as the one candidate when none does.
func (m *Model) rulesFiles(dir, root string) (string, []rules.File) {
	home := homeDir()
	if entry, ok := m.selectedRow(); ok && !entry.isGroup && !m.isShell(entry.sess.Tool) {
		tool := entry.sess.Tool
		return tool, rules.Find(m.rulesSpecs(tool), dir, root, home)
	}

	var specs []string
	for _, name := range sortedToolNames(m.cfg) {
		specs = append(specs, m.rulesSpecs(name)...)
	}
	var found []rules.File
	for _, file := range rules.Find(specs, dir, root, home) {
		if file.Exists {
			found = append(found, file)
		}
	}
	if len(found) == 0 {
		return "", rules.Find(rules.Fallback, dir, root, home)
	}
	return "", found
}

// rulesSpecs is a tool's declared instruction files, falling back to the name
// the CLIs converged on so a hand-written tool block still answers to i.
func (m *Model) rulesSpecs(tool string) []string {
	if specs := m.cfg.Tools[tool].RulesFiles; len(specs) > 0 {
		return specs
	}
	return rules.Fallback
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func (m *Model) handleRulesPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.mode = modeList
		return m, nil
	case "up", "k":
		if m.rules.cursor > 0 {
			m.rules.cursor--
		}
	case "down", "j":
		if m.rules.cursor < len(m.rules.files)-1 {
			m.rules.cursor++
		}
	case "enter":
		file, ok := m.selectedRulesFile()
		if !ok {
			return m, nil
		}
		// A file that is not there has nothing to show, so ↵ on it means the
		// only other thing it could mean: write one and open it.
		if !file.Exists {
			return m.editRulesFile(file)
		}
		return m.openRulesFile(file)
	case "e":
		file, ok := m.selectedRulesFile()
		if !ok {
			return m, nil
		}
		return m.editRulesFile(file)
	}
	return m, nil
}

func (m *Model) selectedRulesFile() (rules.File, bool) {
	if m.rules.cursor < 0 || m.rules.cursor >= len(m.rules.files) {
		return rules.File{}, false
	}
	return m.rules.files[m.rules.cursor], true
}

// openRulesFile reads a file into the viewer. A read that fails leaves the
// picker up with the reason on its status bar rather than opening an empty
// pane that reads as an empty file.
func (m *Model) openRulesFile(file rules.File) (tea.Model, tea.Cmd) {
	text, truncated, err := rules.Read(file.Path)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.rules.label = file.Label
	m.rules.text, m.rules.truncated = text, truncated
	m.rules.scroll, m.rules.body, m.rules.bodyWidth = 0, nil, 0
	m.mode = modeRulesView
	m.errBar.text = ""
	return m, nil
}

// editRulesFile hands the file to the editor, creating an empty one first
// when it does not exist: an editor pointed at a path inside a directory that
// is not there fails on save, and the directory a global rules file lives in
// is routinely the one part missing.
func (m *Model) editRulesFile(file rules.File) (tea.Model, tea.Cmd) {
	m.mode = modeList
	if !file.Exists {
		if err := rules.Create(file.Path); err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.reportDone("created " + file.Path)
	}
	return m.openInEditor(file.Path)
}

func (m *Model) viewRulesPick() string {
	labelWidth := 0
	for _, file := range m.rules.files {
		if w := ansi.StringWidth(file.Label); w > labelWidth {
			labelWidth = w
		}
	}

	var b strings.Builder
	for i, file := range m.rules.files {
		marker, style := "  ", valueStyle
		if m.rules.cursor == i {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			style = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		if !file.Exists {
			// A file that has not been written recedes: the list is read for
			// what the agent was told, and this row is the absence of it.
			style = mutedStyle
		}
		b.WriteString(marker)
		b.WriteString(padRight(style.Render(file.Label), labelWidth+2))
		b.WriteString(subtleStyle.Render(rulesMeta(file)))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	// The directory the chain was resolved from, kept inside the default card
	// so a deep worktree path cannot stretch the dialog to the terminal edge;
	// its tail is the half that says which worktree this is.
	room := cardInnerWidth(m.cardWidth()) - len("  read from ")
	b.WriteString(subtleStyle.Render("  read from " + tailOf(shortPath(m.rules.dir, homeDir()), room)))
	return m.cardFlex(rulesTitle(m.rules.tool), strings.TrimRight(b.String(), "\n"), m.rulesPickHint())
}

// rulesMeta is the row's right half: whether the file is there, and enough
// about it to tell a rule set someone maintains from one written once and
// forgotten.
func rulesMeta(file rules.File) string {
	if !file.Exists {
		return file.Scope.String() + " · not created"
	}
	return fmt.Sprintf("%s · %s · %s",
		file.Scope, humanBytes(uint64(file.Size)), relSince(file.ModTime))
}

func rulesTitle(tool string) string {
	if tool == "" {
		return "▤ Rules"
	}
	return "▤ Rules · " + tool
}

func (m *Model) rulesPickHint() [][2]string {
	open := "view"
	if file, ok := m.selectedRulesFile(); ok && !file.Exists {
		open = "create and edit"
	}
	return [][2]string{{"↑↓", "move"}, {"↵", open}, {"e", "edit"}, {"esc", "back"}}
}

func (m *Model) handleRulesViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		// Back to the list the file was picked from, not out of the surface:
		// reading one rules file and then its neighbour is the normal way
		// round when a project keeps more than one.
		m.mode = modeRulesPick
		return m, nil
	case "e":
		if file, ok := m.selectedRulesFile(); ok {
			return m.editRulesFile(file)
		}
	case "up", "k":
		m.scrollRules(-1)
	case "down", "j":
		m.scrollRules(1)
	case "pgup", "ctrl+u":
		m.scrollRules(-m.rulesPage())
	case "pgdown", "ctrl+d":
		m.scrollRules(m.rulesPage())
	case "g", "home":
		m.rules.scroll = 0
	case "G", "end":
		m.rules.scroll = m.rulesScrollLimit()
	}
	return m, nil
}

func (m *Model) scrollRules(delta int) {
	m.rules.scroll = min(max(m.rules.scroll+delta, 0), m.rulesScrollLimit())
}

func (m *Model) rulesPage() int { return max(m.rulesBodyRoom()-1, 1) }

func (m *Model) rulesScrollLimit() int {
	return max(0, len(m.rulesBody())-m.rulesBodyRoom())
}

// rulesBodyRoom is how many rows of the file the card can show once its own
// chrome, the hint and an error row are taken off the terminal's height.
func (m *Model) rulesBodyRoom() int {
	inner := cardInnerWidth(helpCardWidth(m.width))
	room := m.height - 5 - lipgloss.Height(legendInline(m.rulesViewHint(), inner))
	if m.errBar.text != "" {
		room -= 2
	}
	return max(room, 1)
}

// rulesBody is the file laid out for the card, cached against the width it
// was wrapped at so scrolling costs nothing and a resize still re-wraps.
func (m *Model) rulesBody() []string {
	width := cardInnerWidth(helpCardWidth(m.width))
	if m.rules.body != nil && m.rules.bodyWidth == width {
		return m.rules.body
	}
	m.rules.body = renderRules(m.rules.text, m.rules.truncated, width)
	m.rules.bodyWidth = width
	return m.rules.body
}

// renderRules lays a rules file out as prose: headings picked out so a long
// file can be skimmed for the section that matters, everything else wrapped
// to the card. Markdown is left otherwise as written — this is a viewer for a
// file the reader is about to edit, and a rendered version they cannot map
// back onto its source is the wrong thing to show them.
func renderRules(text string, truncated bool, width int) []string {
	if strings.TrimSpace(text) == "" {
		return []string{subtleStyle.Render("this file is empty")}
	}
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.ReplaceAll(line, "\t", "    ")
		style := valueStyle
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			style = sectionStyle
		}
		lines = appendStyledWrap(lines, line, width, style)
	}
	if truncated {
		lines = append(lines, "", subtleStyle.Render(
			fmt.Sprintf("… truncated at %s; open it in your editor for the rest",
				humanBytes(rules.MaxRead))))
	}
	return lines
}

func (m *Model) viewRulesView() string {
	width := helpCardWidth(m.width)
	body := fitBody(m.rulesBody(), m.rulesBodyRoom(), m.rules.scroll)
	return m.cardSized(width, "▤ "+m.rules.label, strings.Join(body, "\n"), m.rulesViewHint())
}

func (m *Model) rulesViewHint() [][2]string {
	return [][2]string{
		{"↑↓/jk", "scroll"}, {"pgup/pgdn", "page"}, {"g/G", "top/bottom"},
		{"e", "edit"}, {"esc", "back"},
	}
}

// tailOf keeps the end of a path that will not fit, which is the end a reader
// recognizes: the directories nearest the file are what tell two checkouts of
// the same repository apart.
func tailOf(path string, width int) string {
	if width < 2 || ansi.StringWidth(path) <= width {
		return path
	}
	runes := []rune(path)
	return "…" + string(runes[len(runes)-(width-1):])
}

// shortPath is a directory as the reader knows it, with the home directory
// worn as a ~ so a card is not spent on the part of the path they typed
// themselves.
func shortPath(path, home string) string {
	if home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	rest := strings.TrimPrefix(path, home)
	if rest == "" {
		return "~"
	}
	if strings.HasPrefix(rest, string(os.PathSeparator)) {
		return "~" + rest
	}
	return path
}
