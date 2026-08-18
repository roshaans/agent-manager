package spawn

import (
	"strings"
	"testing"
)

func TestLaunchPromptInjectsDirectiveOnlyForAutoNamedWithPrompt(t *testing.T) {
	withDirective := launchPrompt("build the api", true)
	if !strings.HasPrefix(withDirective, renameDirective+"\n\n") || !strings.HasSuffix(withDirective, "build the api") {
		t.Fatalf("auto-named prompt should carry the directive, got %q", withDirective)
	}
	named := launchPrompt("build the api", false)
	if !strings.HasPrefix(named, renameAvailableNote+"\n\n") || !strings.HasSuffix(named, "build the api") {
		t.Fatalf("custom-named prompt should note rename is optional later, got %q", named)
	}
	if strings.Contains(named, "Run rename only this once") || strings.HasPrefix(named, renameDirective) {
		t.Fatalf("custom-named prompt must not force a rename, got %q", named)
	}
	if got := launchPrompt("", true); got != "" {
		t.Fatalf("promptless session should stay clean, got %q", got)
	}
	if got := launchPrompt("/compact keep the api notes", true); got != "/compact keep the api notes" {
		t.Fatalf("slash-command prompt should stay clean, got %q", got)
	}
	if got := launchPrompt("/compact keep the api notes", false); got != "/compact keep the api notes" {
		t.Fatalf("named slash-command prompt should stay clean, got %q", got)
	}
}

// A prompt the directive cannot open has to carry it separately: a slash
// command must be the first thing the tool reads, and a promptless launch has
// nothing to prepend to.
func TestDirectiveEmbeddable(t *testing.T) {
	for prompt, want := range map[string]bool{
		"build the api": true,
		"":              false,
		"/compact":      false,
	} {
		if got := directiveEmbeddable(prompt); got != want {
			t.Errorf("directiveEmbeddable(%q) = %v want %v", prompt, got, want)
		}
	}
}
