package spawn

import "strings"

// renameDirective asks the agent, as the first line of its first prompt, to
// name its own session via the rename subcommand. Injected only for
// auto-named sessions that launch with a prompt, so it fires exactly once.
const renameDirective = `First, run this exact shell command once, replacing <name> with a short 2-4 word kebab-case name for the broad feature or theme of this whole session (not one subtask of a larger feature): agent-manager rename "<name>". Run rename only this once. Do not rename again later in the conversation unless the user explicitly asks you to rename; if they do, pick a broad name from context, not a narrow step. Then do the task:`

// deferredRenameDirective is the standalone message sent into sessions whose
// first prompt could not carry the directive: slash-command prompts (the
// command must open the message) and promptless launches.
const deferredRenameDirective = `Run this exact shell command once, replacing <name> with a short 2-4 word kebab-case name for the broad feature or theme of this whole session (not one subtask of a larger feature): agent-manager rename "<name>". Run rename only this once. Do not rename again later in the conversation unless the user explicitly asks you to rename; if they do, pick a broad name from context, not a narrow step. Then continue.`

// renameAvailableNote tells a custom-named session that rename exists for
// later use without asking it to rename now.
const renameAvailableNote = `This session is already named. You can rename it later with agent-manager rename "<name>" only if the user asks. Do not rename it now. Then do the task:`

// directiveEmbeddable reports whether a launch note can ride the session's
// first prompt; otherwise auto-named sessions get the rename directive later
// as its own message.
func directiveEmbeddable(prompt string) bool {
	return prompt != "" && !strings.HasPrefix(prompt, "/")
}

// launchPrompt prepends a short agent note when the first prompt can carry
// one: auto-named sessions must rename once; custom-named sessions only learn
// that rename is available later.
func launchPrompt(prompt string, autoNamed bool) string {
	if !directiveEmbeddable(prompt) {
		return prompt
	}
	if autoNamed {
		return renameDirective + "\n\n" + prompt
	}
	return renameAvailableNote + "\n\n" + prompt
}
