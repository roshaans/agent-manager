package store

import (
	"path/filepath"
	"testing"
)

func TestParseFormatHiddenTools(t *testing.T) {
	if got := ParseHiddenTools(""); got != nil {
		t.Fatalf("empty parse = %v", got)
	}
	if got := ParseHiddenTools(" , "); got != nil {
		t.Fatalf("blank entries parse = %v", got)
	}
	got := ParseHiddenTools("codex, grok")
	if !got["codex"] || !got["grok"] || len(got) != 2 {
		t.Fatalf("parse = %v", got)
	}
	if FormatHiddenTools(map[string]bool{"grok": true, "codex": true}) != "codex,grok" {
		t.Fatalf("format should sort: %q", FormatHiddenTools(map[string]bool{"grok": true, "codex": true}))
	}
	if FormatHiddenTools(map[string]bool{"codex": false}) != "" {
		t.Fatalf("a set holding only false entries encodes as nothing hidden")
	}
}

func TestWorktreeDefaultWalksToNearestChoice(t *testing.T) {
	choices := map[string]string{
		"backend":            "on",
		"backend/legacy":     "off",
		"backend/legacy/api": "",
	}
	for _, tc := range []struct {
		path     string
		fallback bool
		want     bool
	}{
		{"backend", false, true},
		{"backend/api", false, true},
		{"backend/legacy", true, false},
		// The nearest ancestor with a choice wins over both the group's own
		// blank and the global setting.
		{"backend/legacy/api", true, false},
		{"frontend", true, true},
		{"frontend/web", false, false},
		{"", true, true},
	} {
		if got := WorktreeDefault(choices, tc.path, tc.fallback); got != tc.want {
			t.Errorf("WorktreeDefault(%q, fallback=%v) = %v want %v", tc.path, tc.fallback, got, tc.want)
		}
	}
}

// The database is opened by more than one process, so a writer that meets
// another mid-write has to wait rather than fail on the spot.
func TestOpenSetsABusyTimeout(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	var timeout int
	if err := st.db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if timeout <= 0 {
		t.Fatalf("busy_timeout = %d, want a wait", timeout)
	}
}
