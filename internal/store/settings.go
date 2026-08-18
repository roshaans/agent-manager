package store

import (
	"sort"
	"strings"
)

// The settings keys below are the ones read outside the TUI. A session-scoped
// command creating a session has to reach the same answers the New Session
// form would, so the key and the encoding of its value live here with the
// table that holds them rather than in the screen that happens to write them.
// Keys only the TUI ever reads stay in internal/ui.
const (
	// SettingDefaultTool names the agent CLI a session launches with when
	// nobody picked one.
	SettingDefaultTool = "default_tool"
	// SettingHiddenTools lists the CLIs left out of the new-session pickers,
	// comma-separated. Empty shows every configured tool.
	SettingHiddenTools = "hidden_tools"
	// SettingWorktreeDefault is the global spawn-in-worktree choice, "on" or
	// unset, used by groups that declare none.
	SettingWorktreeDefault = "worktree_default"
)

// ParseHiddenTools reads the stored hidden-CLI list. A list naming nothing
// is no list at all, so it answers nil rather than an empty set and callers
// can treat "nothing hidden" as one case.
func ParseHiddenTools(raw string) map[string]bool {
	if raw == "" {
		return nil
	}
	hidden := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			hidden[name] = true
		}
	}
	if len(hidden) == 0 {
		return nil
	}
	return hidden
}

// FormatHiddenTools encodes the set back, sorted so a rewrite that changed
// nothing produces the same bytes.
func FormatHiddenTools(hidden map[string]bool) string {
	if len(hidden) == 0 {
		return ""
	}
	names := make([]string, 0, len(hidden))
	for name, on := range hidden {
		if on {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// WorktreeDefault resolves whether a session spawned into a group gets its
// own worktree: the nearest ancestor with an explicit choice wins, and a
// path whose whole line inherits falls back to the global setting.
//
// Shaped like EffectivelyArchived, and for the same reason: the walk is the
// meaning of the stored value, so every reader has to do it identically.
func WorktreeDefault(choices map[string]string, path string, fallback bool) bool {
	answer := fallback
	eachAncestor(path, func(ancestor string) bool {
		switch choices[ancestor] {
		case "on":
			answer = true
			return false
		case "off":
			answer = false
			return false
		}
		return true
	})
	return answer
}
