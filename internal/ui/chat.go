package ui

import (
	"fmt"
	"time"

	"github.com/YoanWai/agent-manager/internal/spawn"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	tea "github.com/charmbracelet/bubbletea"
)

// A chat is a fork that starts fresh. `f` continues a conversation in a new
// session on the same checkout; `c` opens a new session on the same checkout
// with no conversation behind it. Both carry the tool, the directory, the
// group and the worktree of the session they were opened from, and both
// record it in ParentID — the link a terminal has always recorded, meaning
// the same thing it has always meant: the row I was opened from, and so the
// checkout I belong to.
//
// That link is all the grouping there is. The chats sharing a checkout are
// the ones whose ParentID chain reaches the same session, resolved when the
// rows are built rather than stored, so nothing has to be written when a
// family gains or loses a member.

// chatKeyWindow is how long after handling a c another one reads as
// autorepeat rather than a second request, matching the window T uses for
// shells. A chat costs a whole agent process, so a held key that spawned four
// would be expensive to undo.
const chatKeyWindow = 250 * time.Millisecond

// chatKey opens a chat unless c arrived inside the burst a held key sends.
// The window runs from the end of the previous spawn, for the reason
// terminalKey documents.
func (m *Model) chatKey() (tea.Model, tea.Cmd) {
	if time.Since(m.chatKeyAt) < chatKeyWindow {
		m.chatKeyAt = time.Now()
		return m, nil
	}
	model, cmd := m.openChat()
	m.chatKeyAt = time.Now()
	return model, cmd
}

// chatOrigin is the chat a new one opened here would join: the agent under
// the cursor, or the agent a shell was opened from when the cursor is on a
// shell, so a chat opened from a terminal lands in the worktree that
// terminal belongs to. A group row names no checkout and answers false.
func (m *Model) chatOrigin() (store.Session, bool) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup {
		return store.Session{}, false
	}
	if !m.isShell(entry.sess.Tool) {
		return entry.sess, true
	}
	if parent, linked := m.sessionByID(entry.sess.ParentID); linked && !m.isShell(parent.Tool) {
		return parent, true
	}
	// A shell whose link this view cannot resolve still carries the
	// checkout it was opened in, which is the only question that matters
	// here: the chats on that checkout are all equally good to join.
	chats := m.chatFamily(m.chatRoot(entry.sess))
	if len(chats) == 0 {
		return store.Session{}, false
	}
	return chats[0], true
}

// openChat starts another agent on the checkout under the cursor. It runs
// the same CLI in the same directory on the same branch, and deliberately
// does none of the work a first session does there: no worktree is created,
// no setup script re-runs, and no auto-run scripts start a second time. The
// checkout is already dressed; what is missing is a conversation.
func (m *Model) openChat() (tea.Model, tea.Cmd) {
	source, ok := m.chatOrigin()
	if !ok {
		m.errBar.text = "select a session to open another chat on it"
		return m, nil
	}
	if m.isShell(source.Tool) {
		m.errBar.text = source.Name + " is a shell, not an agent - press T for another terminal"
		return m, nil
	}
	// An archived checkout is one nobody is working in; a live chat opened
	// into it would leave the archived view the moment it was created.
	if source.Archived {
		m.errBar.text = "restore " + source.Name + " (u) before opening another chat on it"
		return m, nil
	}
	before := len(m.sessions)
	// Everything a fork inherits, inherited the same way, and Worktree left
	// off: the branch travels with the checkout rather than being cut again,
	// so the setup script does not re-run and the auto-run scripts do not
	// start a second time.
	if err := m.spawnSession(spawn.Options{
		Tool:      source.Tool,
		Name:      m.nextChatName(m.chatRoot(source)),
		Group:     source.Group,
		Directory: source.Cwd,
		// A checkout's conversations sit one level deep, the way a terminal
		// opened from a terminal joins the agent rather than the terminal:
		// the tree allows a parent no parent of its own, and a family of
		// peers is what this is.
		ParentID:       chatAnchor(source),
		WorktreeRepo:   source.WorktreeRepo,
		WorktreeBranch: source.WorktreeBranch,
	}); err != nil {
		m.reportLaunchError(err)
		return m, nil
	}
	// A chat starts as starting, which attention excludes; clear so the row
	// the key just made is on screen.
	m.statusFilter = statusFilterAll
	m.errBar.text = ""
	m.rebuildRows()
	if len(m.sessions) > before {
		m.focusSession(m.sessions[before].ID)
	}
	return m, m.refreshCmd()
}

// nextChatName names a chat after the family it joins and its place in
// it, so a rail row says which conversation of the branch it is rather than
// repeating the branch four times. The count is what the row is numbered by,
// so the name and the jump key agree.
func (m *Model) nextChatName(root string) string {
	chats := m.chatFamily(root)
	base := root
	if len(chats) > 0 {
		base = chats[0].Name
	}
	taken := make(map[string]bool, len(m.sessions))
	for _, sess := range m.sessions {
		taken[sess.Name] = true
	}
	// One candidate per session already named plus this one is always more
	// than the names in the way, so the search cannot run past the end.
	for n := len(chats) + 1; n <= len(taken)+len(chats)+1; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !taken[candidate] {
			return candidate
		}
	}
	return base + "-" + spawn.NewID()[:4]
}

// chatAnchor is what a new chat opened from this session hangs off: the
// session itself when it heads its checkout, or whatever it hangs off when it
// does not. Nesting is one level deep by construction, so this is also the
// family's first conversation.
func chatAnchor(sess store.Session) string {
	if sess.ParentID != "" {
		return sess.ParentID
	}
	return sess.ID
}

// chatRoot is the conversation a chat's family hangs off, which with one
// level of nesting is its parent, or itself when it is the parent. A parent
// this view cannot see leaves the chat heading its own family rather than
// pointing at a row nobody can reach.
func (m *Model) chatRoot(sess store.Session) string {
	if parent, ok := m.sessionByID(sess.ParentID); ok && !m.isShell(parent.Tool) && parent.Group == sess.Group {
		return parent.ID
	}
	return sess.ID
}

// adoptChildrenOf keeps a family together across the deletion of one of its
// members. A row with a parent hands its children up to it; one that was the
// first conversation on its checkout has no parent to hand them to, so the
// eldest chat opened from it takes its place.
//
// Terminals are never promoted: a shell heading a family would be a row the
// chat keys skip and the strip cannot name.
func (m *Model) adoptChildrenOf(sess store.Session) error {
	kids, err := m.store.Children(sess.ID)
	if err != nil {
		return err
	}
	var eldest store.Session
	for _, kid := range kids {
		if m.isShell(kid.Tool) {
			continue
		}
		if eldest.ID == "" || kid.CreatedAt.Before(eldest.CreatedAt) {
			eldest = kid
		}
	}
	if eldest.ID == "" {
		return nil
	}
	// The tree allows the heir no parent of its own, so it takes the deleted
	// row's place at the head and the rest of the family hangs off it.
	if err := m.store.SetParent(eldest.ID, ""); err != nil {
		return err
	}
	return m.store.AdoptChildren(sess.ID, eldest.ID)
}

// chatFamily is the conversations open on one checkout, in the order the rail
// draws them, which is the order the keys count them in. Shells are left out:
// they belong to the checkout but they are not conversations to move between.
//
// It reads the order the rows were built from rather than the whole store, so
// the number printed beside a row and the number a key takes are the same
// number under a search or a status filter, and a jump can never land on a
// row the view is holding back.
func (m *Model) chatFamily(root string) []store.Session {
	if root == "" {
		return nil
	}
	ids := m.chatOrder[root]
	chats := make([]store.Session, 0, len(ids))
	for _, id := range ids {
		if sess, ok := m.sessionByID(id); ok {
			chats = append(chats, sess)
		}
	}
	return chats
}

// selectedChat is the conversation the chat keys act on: the agent under
// the cursor, or the one a shell was opened from.
func (m *Model) selectedChat() (store.Session, bool) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup {
		return store.Session{}, false
	}
	if !m.isShell(entry.sess.Tool) {
		return entry.sess, true
	}
	return m.sessionByID(entry.sess.ParentID)
}

// cycleChat moves to the next or previous chat of the checkout the cursor
// is in, wrapping at both ends. A lone conversation has nowhere to go and
// says so rather than appearing to do nothing.
func (m *Model) cycleChat(delta int) (tea.Model, tea.Cmd) {
	current, ok := m.selectedChat()
	if !ok {
		return m, nil
	}
	chats := m.chatFamily(m.chatRoot(current))
	if len(chats) < 2 {
		m.errBar.text = "only one chat here - press c to open another"
		return m, nil
	}
	at := 0
	for i, chat := range chats {
		if chat.ID == current.ID {
			at = i
			break
		}
	}
	next := ((at+delta)%len(chats) + len(chats)) % len(chats)
	return m.selectChat(chats[next].ID)
}

// jumpChat moves to the nth chat of the checkout the cursor is in, counting
// from one, which is the number the rail prints beside each row.
func (m *Model) jumpChat(n int) (tea.Model, tea.Cmd) {
	current, ok := m.selectedChat()
	if !ok {
		return m, nil
	}
	chats := m.chatFamily(m.chatRoot(current))
	if len(chats) < 2 {
		m.errBar.text = "only one chat here - press c to open another"
		return m, nil
	}
	if n < 1 || n > len(chats) {
		m.errBar.text = fmt.Sprintf("no chat %d here - this checkout has %d", n, len(chats))
		return m, nil
	}
	return m.selectChat(chats[n-1].ID)
}

// selectChat puts the cursor on a chat, and puts the keyboard back in its
// pane when the move was made from inside one: switching conversations
// should not cost the focus that made switching worth a key.
func (m *Model) selectChat(id string) (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if ok && sess.ID == id {
		m.errBar.text = ""
		return m, nil
	}
	focused := m.mode == modeFocus
	m.focusSession(id)
	if selected, ok := m.selected(); !ok || selected.ID != id {
		// The chat is one the view knows about but is not drawing: its group
		// is folded, or it was moved into one that is. Moving the cursor
		// somewhere else would be worse than saying so.
		m.errBar.text = "that chat is in a folded group - press F or unfold it to reach it"
		return m, nil
	}
	m.preview = ""
	m.proc = sysstat.ProcStat{}
	m.procFor = ""
	m.previewGen++
	m.errBar.text = ""
	if focused {
		return m.focusSelected()
	}
	return m, m.schedulePreview()
}

// chatSwitchTarget reads a focus-mode key as a request to move to another
// chat on the same checkout. The agent in the pane owns the plain keys and
// the manager has already spent the ctrl block it reserves, so what is left
// has to clear three bars at once: no Meta, a distinct escape sequence every
// terminal sends, and nothing an agent wants.
//
// Shift+arrow clears all three. It is a CSI sequence (ESC [ 1;2C) rather
// than a modified character, so it survives a terminal that treats Option as
// a compose key instead of as Meta — which is the default on macOS, where
// Option+. is "≥" and reaches an application as a plain rune with no
// modifier on it at all. The manager already claims bare Left at the head of
// a prompt, so the arrow row is half its territory before this.
//
// The Alt forms stay for terminals that do send Meta, and because a reader
// who has learned them should not have them taken away. Alt+[ and alt+] are
// deliberately not among them: a terminal sends Alt as an Escape prefix, so
// alt+[ is indistinguishable on the wire from the CSI introducer that opens
// every other escape sequence.
//
// The target is a chat number, or one of the two sentinels meaning next and
// previous.
const (
	chatNext = -1
	chatPrev = -2
)

func (m *Model) chatSwitchTarget(msg tea.KeyMsg) (int, bool) {
	switch msg.Type {
	case tea.KeyShiftRight:
		return chatNext, true
	case tea.KeyShiftLeft:
		return chatPrev, true
	}
	if !msg.Alt || msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return 0, false
	}
	switch key := msg.Runes[0]; {
	case key >= '1' && key <= '9':
		return int(key - '0'), true
	case key == '.':
		return chatNext, true
	case key == ',':
		return chatPrev, true
	}
	return 0, false
}

// chatSwitch performs what chatSwitchTarget read.
func (m *Model) chatSwitch(target int) (tea.Model, tea.Cmd) {
	switch target {
	case chatNext:
		return m.cycleChat(1)
	case chatPrev:
		return m.cycleChat(-1)
	}
	return m.jumpChat(target)
}

// chatIndex resolves how the tree draws each family of chats: the members in
// order, which of them the block hangs off, and the number printed beside
// each. All three come out of one walk over the same filtered set the tree is
// built from, so the number a row shows is the number its key takes, and a
// search that hides the first chat promotes the first one still on screen
// rather than leaving its siblings hanging off a row nobody can reach.
//
// A conversation with no siblings carries neither a head nor a number: there
// is no block to draw and nothing to tell apart.
func (m *Model) chatIndex(sessions []store.Session) (order map[string][]string, numbers map[string]int) {
	kept := map[string]bool{}
	for _, sess := range sessions {
		kept[sess.ID] = true
	}
	// Roots are resolved against the rows on screen: a parent the view is
	// holding back is not one this tree can count from.
	rootOf := func(sess store.Session) string {
		parent, ok := m.sessionByID(sess.ParentID)
		if !ok || !kept[parent.ID] || m.isShell(parent.Tool) || parent.Group != sess.Group {
			return sess.ID
		}
		return parent.ID
	}

	seen := []string{}
	members := map[string][]store.Session{}
	for _, sess := range sessions {
		if m.isShell(sess.Tool) {
			continue
		}
		root := rootOf(sess)
		if _, known := members[root]; !known {
			seen = append(seen, root)
		}
		members[root] = append(members[root], sess)
	}
	order, numbers = map[string][]string{}, map[string]int{}
	for _, root := range seen {
		chats := members[root]
		ids := make([]string, 0, len(chats))
		for _, chat := range chats {
			ids = append(ids, chat.ID)
		}
		order[root] = ids
		if len(chats) < 2 {
			continue
		}
		for i, chat := range chats {
			numbers[chat.ID] = i + 1
		}
	}
	return order, numbers
}

// chatNumber is the position the rail prints beside a chat, or zero for one
// that has no siblings to be told apart from.
func (m *Model) chatNumber(sess store.Session) int { return m.chatNumbers[sess.ID] }
