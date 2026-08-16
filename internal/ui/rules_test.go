package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// claudeReads points the test model's first CLI at the given files, which is
// what every i press resolves against.
func claudeReads(m *Model, specs ...string) {
	tool := m.cfg.Tools["claude"]
	tool.RulesFiles = specs
	m.cfg.Tools["claude"] = tool
}

func writeRules(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func pressRulesKey(t *testing.T, m *Model) {
	t.Helper()
	_, cmd := m.Update(runeKey("i"))
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
}

func pressInRules(t *testing.T, m *Model, key tea.KeyMsg) {
	t.Helper()
	updated, _ := m.Update(key)
	*m = *updated.(*Model)
}

func rulesLabels(m *Model) []string {
	labels := make([]string, len(m.rules.files))
	for i, file := range m.rules.files {
		labels[i] = file.Label
	}
	return labels
}

func TestRulesKeyListsTheFilesTheSessionsToolReads(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	writeRules(t, dir, "CLAUDE.md", "always run the tests\n")
	claudeReads(m, "CLAUDE.md")
	onSessionIn(t, m, dir)

	pressRulesKey(t, m)

	if m.mode != modeRulesPick {
		t.Fatalf("mode = %v, want the rules list; err = %q", m.mode, m.errBar.text)
	}
	if labels := rulesLabels(m); len(labels) != 1 || labels[0] != "CLAUDE.md" {
		t.Fatalf("files = %v, want the tool's own CLAUDE.md", labels)
	}
	if !m.rules.files[0].Exists {
		t.Fatal("an existing file was listed as missing")
	}
	// The card names the tool, because which file governs a session is a
	// property of the CLI it runs rather than of the directory.
	if frame := ansi.Strip(m.View()); !strings.Contains(frame, "Rules · claude") {
		t.Fatalf("card does not name the tool:\n%s", frame)
	}
}

// A project that tells the agent nothing is the answer a reader came for, so
// the file it does not have is a row rather than an empty list.
func TestRulesKeyListsAFileThatIsNotThereYet(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	claudeReads(m, "CLAUDE.md")
	onSessionIn(t, m, dir)

	pressRulesKey(t, m)

	if len(m.rules.files) != 1 || m.rules.files[0].Exists {
		t.Fatalf("files = %+v, want one missing candidate", m.rules.files)
	}
	frame := ansi.Strip(m.View())
	if !strings.Contains(frame, "not created") {
		t.Fatalf("card does not say the file is missing:\n%s", frame)
	}
	if !strings.Contains(frame, "create and edit") {
		t.Fatalf("hint does not offer to create it:\n%s", frame)
	}
}

func TestRulesEnterReadsTheFile(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	writeRules(t, dir, "CLAUDE.md", "# House style\n\nnever git stash\n")
	claudeReads(m, "CLAUDE.md")
	onSessionIn(t, m, dir)
	pressRulesKey(t, m)

	pressInRules(t, m, namedKey(tea.KeyEnter))

	if m.mode != modeRulesView {
		t.Fatalf("mode = %v, want the viewer; err = %q", m.mode, m.errBar.text)
	}
	frame := ansi.Strip(m.View())
	for _, want := range []string{"CLAUDE.md", "House style", "never git stash"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("viewer missing %q:\n%s", want, frame)
		}
	}
}

// Reading one rules file and then its neighbour is the normal way round, so
// esc goes back to the list rather than out of the surface.
func TestRulesEscLeavesTheViewerForTheList(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	writeRules(t, dir, "CLAUDE.md", "rules\n")
	claudeReads(m, "CLAUDE.md")
	onSessionIn(t, m, dir)
	pressRulesKey(t, m)
	pressInRules(t, m, namedKey(tea.KeyEnter))

	pressInRules(t, m, namedKey(tea.KeyEsc))
	if m.mode != modeRulesPick {
		t.Fatalf("mode = %v, want back on the list", m.mode)
	}

	pressInRules(t, m, namedKey(tea.KeyEsc))
	if m.mode != modeList {
		t.Fatalf("mode = %v, want back on the sessions", m.mode)
	}
}

// The file is created empty: anything the manager wrote into it would become
// a rule the agent then obeys, and nobody asked for that.
func TestRulesEnterOnAMissingFileCreatesItEmpty(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	claudeReads(m, "CLAUDE.md")
	onSessionIn(t, m, dir)
	pressRulesKey(t, m)

	pressInRules(t, m, namedKey(tea.KeyEnter))

	body, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("file was not created: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("created file holds %q, want it empty", body)
	}
	if !strings.Contains(m.errBar.text, "created") {
		t.Fatalf("status = %q, want it to report the file it wrote", m.errBar.text)
	}
}

// A row with no tool behind it — a group, or a shell — has nothing to offer
// creating, so it shows what the directory actually carries.
func TestRulesOnAShellListsOnlyWhatExists(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	writeRules(t, dir, "AGENTS.md", "shared rules\n")
	claudeReads(m, "CLAUDE.md")
	onSessionIn(t, m, dir)

	m.openTerminal()
	m.applyCmd(t, nil)
	m.selectSessionRow(t, m.sessions[len(m.sessions)-1].Name)
	pressRulesKey(t, m)

	if m.mode != modeRulesPick {
		t.Fatalf("mode = %v, want the rules list; err = %q", m.mode, m.errBar.text)
	}
	if labels := rulesLabels(m); len(labels) != 1 || labels[0] != "AGENTS.md" {
		t.Fatalf("files = %v, want only the file that is there", labels)
	}
}

// A tool block written by hand carries no rules_files, and answering i with
// an empty list would read as "this tool has none" rather than "nothing was
// declared".
func TestRulesFallsBackToAgentsMd(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	writeRules(t, dir, "AGENTS.md", "shared rules\n")
	m.cfg.Tools["claude"] = config.Tool{Command: "cat"}
	onSessionIn(t, m, dir)

	pressRulesKey(t, m)

	if labels := rulesLabels(m); len(labels) != 1 || labels[0] != "AGENTS.md" {
		t.Fatalf("files = %v, want the AGENTS.md fallback", labels)
	}
}

func TestRulesViewerScrollsALongFile(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	writeRules(t, dir, "CLAUDE.md", strings.Repeat("a rule\n", 400))
	claudeReads(m, "CLAUDE.md")
	onSessionIn(t, m, dir)
	pressRulesKey(t, m)
	pressInRules(t, m, namedKey(tea.KeyEnter))

	pressInRules(t, m, namedKey(tea.KeyPgDown))
	if m.rules.scroll == 0 {
		t.Fatal("pgdn did not scroll the file")
	}
	pressInRules(t, m, runeKey("G"))
	if m.rules.scroll != m.rulesScrollLimit() {
		t.Fatalf("scroll = %d, want the bottom at %d", m.rules.scroll, m.rulesScrollLimit())
	}
	pressInRules(t, m, runeKey("g"))
	if m.rules.scroll != 0 {
		t.Fatalf("scroll = %d, want the top", m.rules.scroll)
	}
}

// A rules file the reader is about to edit is shown as written; the frame
// still has to fit the terminal it is painted into.
func TestRulesFramePaintsInsideTheTerminal(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	writeRules(t, dir, "CLAUDE.md", strings.Repeat("a fairly long rule about style\n", 200))
	claudeReads(m, "CLAUDE.md")
	onSessionIn(t, m, dir)
	pressRulesKey(t, m)
	pressInRules(t, m, namedKey(tea.KeyEnter))

	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("frame is %d rows, want %d", len(lines), m.height)
	}
	for i, line := range lines {
		if width := ansi.StringWidth(line); width > m.width {
			t.Fatalf("row %d is %d columns wide, want at most %d", i, width, m.width)
		}
	}
}
