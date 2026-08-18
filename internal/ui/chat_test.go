package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// spawnChat opens another chat on the session under the cursor and returns
// it, leaving the cursor on it the way the key does.
func spawnChat(t *testing.T, m *Model) string {
	t.Helper()
	_, cmd := m.openChat()
	if m.errBar.text != "" {
		t.Fatalf("chat spawn reported %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	sess, ok := m.selected()
	if !ok {
		t.Fatal("spawn should leave the cursor on the new chat")
	}
	return sess.ID
}

func TestOpenChatJoinsTheWorkspaceItWasOpenedOn(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", dir, "")
	m.selectSessionRow(t, "build")

	source, ok := m.selected()
	if !ok {
		t.Fatal("no session after create")
	}
	chatID := spawnChat(t, m)

	chat, ok := m.sessionByID(chatID)
	if !ok {
		t.Fatal("the chat is not in the model")
	}
	if chat.ParentID != source.ID {
		t.Fatalf("chat parent = %q, want the session it was opened on (%q)", chat.ParentID, source.ID)
	}
	if chat.Cwd != source.Cwd {
		t.Fatalf("chat cwd = %q, want the checkout it joined (%q)", chat.Cwd, source.Cwd)
	}
	if chat.Tool != source.Tool {
		t.Fatalf("chat tool = %q, want the CLI already running there (%q)", chat.Tool, source.Tool)
	}
	if chat.ID == source.ID {
		t.Fatal("the chat reused the source's id")
	}

	// Opening a chat writes nothing to the session it was opened from: the
	// link lives on the new row alone, which is the whole point of using the
	// one a terminal already records.
	stored, err := m.store.Get(source.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if stored.ParentID != "" {
		t.Fatalf("opening a chat wrote %q onto its source", stored.ParentID)
	}
	if got := len(m.chatFamily(source.ID)); got != 2 {
		t.Fatalf("the checkout holds %d chats, want 2", got)
	}
}

func TestChatNestsUnderTheChatThatOpenedIt(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", dir, "")
	m.selectSessionRow(t, "build")
	source, _ := m.selected()
	chatID := spawnChat(t, m)

	head := rowFor(t, m, source.ID)
	chat := rowFor(t, m, chatID)
	if chat.depth != head.depth+1 {
		t.Fatalf("chat depth = %d, want one past its workspace's head (%d)", chat.depth, head.depth)
	}
	if m.chatNumber(source) != 1 {
		t.Fatalf("the first chat is numbered %d, want 1", m.chatNumber(source))
	}
	if got, _ := m.sessionByID(chatID); m.chatNumber(got) != 2 {
		t.Fatalf("the second chat is numbered %d, want 2", m.chatNumber(got))
	}
	rail := m.rail()
	if !strings.Contains(rail, "1 build") || !strings.Contains(rail, "2 build-2") {
		t.Fatalf("the rail should number both chats:\n%s", rail)
	}
}

// A session with nothing to be told apart from carries no number: the rail
// is too narrow to spend a column on a key that would answer with a refusal.
func TestLoneSessionIsNotNumbered(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")

	sess, _ := m.selected()
	if n := m.chatNumber(sess); n != 0 {
		t.Fatalf("a lone session is numbered %d, want no number at all", n)
	}
	if strip := m.chatStrip(80); strip != "" {
		t.Fatalf("a lone session draws a chat strip: %q", strip)
	}
}

func TestChatStripNamesEveryChatAndTheKeys(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")
	spawnChat(t, m)

	strip := ansi.Strip(m.chatStrip(100))
	for _, want := range []string{"1 build", "2 build-2", "alt+1…2"} {
		if !strings.Contains(strip, want) {
			t.Fatalf("chat strip is missing %q:\n%s", want, strip)
		}
	}
}

func TestCycleChatWrapsThroughTheWorkspace(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")
	first, _ := m.selected()
	second := spawnChat(t, m)

	// The cursor is on the second chat; forward wraps to the first.
	if _, cmd := m.cycleChat(1); cmd != nil {
		_ = cmd()
	}
	if sess, _ := m.selected(); sess.ID != first.ID {
		t.Fatalf("next from the last chat = %q, want the first to come round", sess.Name)
	}
	if _, cmd := m.cycleChat(-1); cmd != nil {
		_ = cmd()
	}
	if sess, _ := m.selected(); sess.ID != second {
		t.Fatalf("previous from the first chat should wrap to the last, got %q", sess.Name)
	}
}

func TestJumpChatTakesTheNumberTheRailPrints(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")
	first, _ := m.selected()
	spawnChat(t, m)

	if _, cmd := m.jumpChat(1); cmd != nil {
		_ = cmd()
	}
	if sess, _ := m.selected(); sess.ID != first.ID {
		t.Fatalf("alt+1 landed on %q, want the first chat", sess.Name)
	}
	m.jumpChat(9)
	if !strings.Contains(m.errBar.text, "no chat 9") {
		t.Fatalf("a number past the end should say so, got %q", m.errBar.text)
	}
}

// A workspace of one has nowhere to go, and saying so beats a key that
// looks broken.
func TestChatKeysOnALoneSessionExplainThemselves(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")

	m.cycleChat(1)
	if !strings.Contains(m.errBar.text, "only one chat") {
		t.Fatalf("cycling a lone session reported %q", m.errBar.text)
	}
}

// The point of the alt keys is that switching costs nothing: the pane keeps
// the keyboard across the move.
func TestAltNumberSwitchesChatWithoutLeavingFocus(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")
	first, _ := m.selected()
	spawnChat(t, m)

	if _, cmd := m.focusSelected(); cmd != nil {
		_ = cmd()
	}
	if m.mode != modeFocus {
		t.Fatalf("mode = %v, want focus before the switch", m.mode)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	m = updated.(*Model)
	if sess, _ := m.selected(); sess.ID != first.ID {
		t.Fatalf("alt+1 landed on %q, want the first chat", sess.Name)
	}
	if m.mode != modeFocus {
		t.Fatalf("mode = %v after alt+1, want the pane to keep the keyboard", m.mode)
	}
}

// Every other Alt key still belongs to the agent: intercepting the whole
// family would cost it bindings the manager never asked for.
func TestUnclaimedAltKeysStillReachTheAgent(t *testing.T) {
	m := buildModel(t)
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'0'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'1'}},
		{Type: tea.KeyEnter, Alt: true},
	} {
		if _, claimed := m.chatSwitchTarget(msg); claimed {
			t.Fatalf("%s should reach the agent, not switch chats", msg.String())
		}
	}
	for key, want := range map[rune]int{'1': 1, '9': 9, '.': chatNext, ',': chatPrev} {
		target, claimed := m.chatSwitchTarget(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}, Alt: true})
		if !claimed || target != want {
			t.Fatalf("alt+%c read as (%d, %v), want (%d, true)", key, target, claimed, want)
		}
	}
	// Shift+arrow carries no Meta, so it is the pair that survives a
	// terminal treating Option as a compose key rather than as Meta.
	for _, c := range []struct {
		key  tea.KeyType
		want int
	}{{tea.KeyShiftRight, chatNext}, {tea.KeyShiftLeft, chatPrev}} {
		target, claimed := m.chatSwitchTarget(tea.KeyMsg{Type: c.key})
		if !claimed || target != c.want {
			t.Fatalf("%v read as (%d, %v), want (%d, true)", c.key, target, claimed, c.want)
		}
	}
	// The unshifted arrows still belong to the agent: Left is already spoken
	// for at the head of a prompt and nowhere else.
	for _, key := range []tea.KeyType{tea.KeyLeft, tea.KeyRight, tea.KeyShiftUp, tea.KeyShiftDown} {
		if _, claimed := m.chatSwitchTarget(tea.KeyMsg{Type: key}); claimed {
			t.Fatalf("%v was read as a chat switch", key)
		}
	}
}

// A shell knows the checkout it was opened in, so the key that opens another
// conversation there works from its row too.
func TestChatOpenedFromAShellJoinsTheSameWorkspace(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")
	source, _ := m.selected()
	shell := spawnTerminal(t, m)

	if shell.ParentID != source.ID {
		t.Fatalf("shell parent = %q, want the session it was opened for (%q)", shell.ParentID, source.ID)
	}
	chatID := spawnChat(t, m)
	chat, _ := m.sessionByID(chatID)
	if chat.ParentID != source.ID {
		t.Fatalf("a chat opened from a shell joined %q, want %q", chat.ParentID, source.ID)
	}
	if m.isShell(chat.Tool) {
		t.Fatal("c on a shell row opened another shell rather than a conversation")
	}
}

// Deleting one conversation is not deleting the checkout: the sibling and
// the worktree both stay.
func TestDeletingAChatLeavesItsSiblingsAlone(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")
	first, _ := m.selected()
	second := spawnChat(t, m)

	m.prepareDelete()
	for _, target := range m.confirm.sessions {
		if target.ID == first.ID {
			t.Fatal("deleting the second chat would take the first one with it")
		}
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if _, alive := m.sessionByID(second); alive {
		t.Fatal("the deleted chat is still listed")
	}
	if _, alive := m.sessionByID(first.ID); !alive {
		t.Fatal("deleting one chat took its sibling with it")
	}
}

// A chat opened on an existing worktree runs in it rather than cutting a
// second one: the checkout is already dressed, and a fresh one would be the
// slow half of a spawn done for nothing.
func TestChatSharesTheWorktreeItJoined(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	m.applyCmd(t, m.refreshCmd())
	source := createWorktreeSession(t, m, "mover", repo)
	m.selectSessionRow(t, "mover")

	chatID := spawnChat(t, m)
	chat, _ := m.sessionByID(chatID)
	if chat.Cwd != source.Cwd {
		t.Fatalf("chat cwd = %q, want the worktree it joined (%q)", chat.Cwd, source.Cwd)
	}
	if chat.WorktreeBranch != source.WorktreeBranch {
		t.Fatalf("chat branch = %q, want %q", chat.WorktreeBranch, source.WorktreeBranch)
	}
	if chat.WorktreeRepo != source.WorktreeRepo {
		t.Fatalf("chat repo = %q, want %q", chat.WorktreeRepo, source.WorktreeRepo)
	}
}

// A checkout with more than one conversation on it keeps its directory where
// it is, and the rename lands anyway.
func TestRenamingAChatKeepsTheWorktreeWhereItIs(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	m.applyCmd(t, m.refreshCmd())
	source := createWorktreeSession(t, m, "mover", repo)
	m.selectSessionRow(t, "mover")
	chatID := spawnChat(t, m)

	m.openRename()
	m.rename.input.SetValue("side-quest")
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeList {
		t.Fatalf("mode = %v, want the rename to go through", m.mode)
	}

	stored, err := m.store.Get(chatID)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if stored.Name != "side-quest" {
		t.Fatalf("chat name = %q, want the rename to have landed", stored.Name)
	}
	if stored.Cwd != source.Cwd || stored.WorktreeBranch != source.WorktreeBranch {
		t.Fatalf("renaming a chat moved the checkout: %+v", stored)
	}
	if !strings.Contains(m.errBar.text, "worktree is shared") {
		t.Fatalf("the reason the branch stayed should be said once, got %q", m.errBar.text)
	}
}

// Deleting the conversation a family was opened from hands its siblings to
// that session's own parent, so the block does not scatter across the group.
func TestDeletingTheFirstChatKeepsTheFamilyTogether(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")
	first, _ := m.selected()
	second := spawnChat(t, m)
	m.selectSessionRow(t, "build")
	third := spawnChat(t, m)

	m.selectSessionRow(t, "build")
	m.prepareDelete()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	if _, alive := m.sessionByID(first.ID); alive {
		t.Fatal("the deleted chat is still listed")
	}
	held, ok := m.sessionByID(second)
	if !ok {
		t.Fatal("deleting the first chat took its sibling with it")
	}
	if held.ParentID != "" {
		t.Fatalf("sibling still points at the deleted row: %q", held.ParentID)
	}
	// The two survivors are one family still, headed by the elder of them.
	family := m.chatFamily(m.chatRoot(held))
	if len(family) != 2 || family[0].ID != second || family[1].ID != third {
		t.Fatalf("family after the delete = %+v, want the two survivors in order", family)
	}
}

// The pane is sized from the same budget the frame is laid out from, so a
// strip that takes a row has to take it from tmux too: an agent drawing one
// row taller than the panel can paint loses its top line to the crop.
func TestChatStripCostsThePaneItsRow(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")

	alone := m.previewPaneHeight()
	spawnChat(t, m)
	m.selectSessionRow(t, "build")
	if strip := m.chatStrip(m.previewPaneWidth() - 2*contentGutter); strip == "" {
		t.Fatal("two chats on one checkout should draw a strip")
	}
	withStrip := m.previewPaneHeight()
	if withStrip != alone-1 {
		t.Fatalf("pane height = %d with a strip and %d without, want exactly one row less", withStrip, alone)
	}

	// And the frame agrees: the rows the panel paints below the strip are
	// the rows the pane was sized to.
	lines := m.contentLines(m.previewPaneWidth(), m.listBodyHeight())
	painted := len(lines) - m.previewBodyOffset
	if painted != withStrip {
		t.Fatalf("panel paints %d pane rows, tmux is sized for %d", painted, withStrip)
	}
}

// The pair works from the list as well as from a pane, so the gesture that
// moves between conversations is one gesture wherever the reader is.
func TestShiftArrowsCycleFromTheListAndTheFocusedPane(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "build", t.TempDir(), "")
	m.selectSessionRow(t, "build")
	first, _ := m.selected()
	second := spawnChat(t, m)

	m.selectSessionRow(t, "build")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
	m = updated.(*Model)
	if sess, _ := m.selected(); sess.ID != second {
		t.Fatalf("shift+right from the list landed on %q, want the second chat", sess.Name)
	}

	if _, cmd := m.focusSelected(); cmd != nil {
		_ = cmd()
	}
	if m.mode != modeFocus {
		t.Fatalf("mode = %v, want focus", m.mode)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftLeft})
	m = updated.(*Model)
	if sess, _ := m.selected(); sess.ID != first.ID {
		t.Fatalf("shift+left from the pane landed on %q, want the first chat", sess.Name)
	}
	if m.mode != modeFocus {
		t.Fatalf("mode = %v after the switch, want the pane to keep the keyboard", m.mode)
	}
}

// The tree allows a parent no parent of its own, so a chat opened from a
// chat joins the family rather than nesting under its sibling.
func TestAChatOpenedFromAChatJoinsTheFamily(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "hi", t.TempDir(), "")
	m.selectSessionRow(t, "hi")
	first, _ := m.selected()

	second := spawnChat(t, m) // opened from hi
	third := spawnChat(t, m)  // opened from the chat above

	for _, id := range []string{second, third} {
		held, _ := m.sessionByID(id)
		if held.ParentID != first.ID {
			t.Fatalf("%s hangs off %q, want the checkout's first conversation (%q)", held.Name, held.ParentID, first.ID)
		}
	}

	root := rowFor(t, m, first.ID)
	for i, id := range []string{second, third} {
		row := rowFor(t, m, id)
		if row.depth != root.depth+1 {
			t.Fatalf("chat %d sits at depth %d, want one level under the first (%d)", i+2, row.depth, root.depth)
		}
		sess, _ := m.sessionByID(id)
		if got := m.chatNumber(sess); got != i+2 {
			t.Fatalf("chat %q is numbered %d, want %d", sess.Name, got, i+2)
		}
	}
	if family := m.chatFamily(m.chatRoot(first)); len(family) != 3 {
		t.Fatalf("family holds %d chats, want 3", len(family))
	}
}

// Forking a chat that has never taken a turn resumes an id with nothing
// behind it, and the failure lands inside the new pane where it is hardest to
// read. It is refused where it was asked for instead.
func TestForkRefusesAConversationThatNeverStarted(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "hi", t.TempDir(), "")
	m.selectSessionRow(t, "hi")
	spawnChat(t, m)

	sess, _ := m.selected()
	if err := m.store.SetAgentSessionID(sess.ID, "90b0c5ed-16e7-4de9-b355-9749f676d0cf"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, sess.Name)

	tool := m.cfg.Tools[sess.Tool]
	tool.ForkCommand = "claude --resume {id}"
	tool.SessionStore = "claude"
	m.cfg.Tools[sess.Tool] = tool

	restore := conversationExists
	t.Cleanup(func() { conversationExists = restore })

	conversationExists = func(string, string) (bool, bool) { return false, true }
	m.openFork()
	if m.mode == modeFork {
		t.Fatal("a chat with no conversation behind it opened the fork card")
	}
	if !strings.Contains(m.errBar.text, "not started a conversation") {
		t.Fatalf("refusal = %q, want it to say the conversation is missing", m.errBar.text)
	}

	// A store the check has no opinion about must never block the fork.
	conversationExists = func(string, string) (bool, bool) { return false, false }
	m.openFork()
	if m.mode != modeFork {
		t.Fatalf("mode = %v, want the fork card: an unknown store is not evidence of anything", m.mode)
	}
}
