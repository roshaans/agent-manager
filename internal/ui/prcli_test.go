package ui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// fakeCLI puts stand-in gh and git executables on PATH, the way the hermes
// and editor tests stand in for the programs they launch. Canned output for
// a subcommand is a file named for it; naming a subcommand in failing makes
// it exit non-zero instead.
//
// This covers what a swapped Go seam cannot: the arguments actually handed to
// the tool, and what the code does when the tool refuses.
type fakeCLI struct{ dir string }

func newFakeCLI(t *testing.T, failing ...string) *fakeCLI {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in executables are shell scripts")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// One script serves both names: it records the call, then either fails or
	// prints whatever the test left for that subcommand.
	script := `#!/bin/sh
printf '%s %s\n' "$(basename "$0")" "$*" >> "$FAKE_CLI_DIR/args"
case "$1" in
  api) key="api" ;;
  pr)  key="pr-$2" ;;
  *)   key="$(basename "$0")-$1" ;;
esac
case " $FAKE_CLI_FAIL " in
  *" $key "*) echo "working"; echo "$key refused" >&2; exit 1 ;;
esac
if [ -f "$FAKE_CLI_DIR/$key" ]; then cat "$FAKE_CLI_DIR/$key"; fi
exit 0
`
	for _, name := range []string{"gh", "git"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_CLI_DIR", dir)
	t.Setenv("FAKE_CLI_FAIL", strings.Join(failing, " "))
	return &fakeCLI{dir: dir}
}

// answers leaves canned output for a subcommand, keyed the way the script
// keys it: "pr-list", "pr-view", "pr-create", "api", "git-push".
func (f *fakeCLI) answers(t *testing.T, key, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, key), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// calls is every invocation, one per line, as "<binary> <args>".
func (f *fakeCLI) calls(t *testing.T) []string {
	t.Helper()
	out, err := os.ReadFile(filepath.Join(f.dir, "args"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func (f *fakeCLI) called(t *testing.T, want string) bool {
	t.Helper()
	for _, call := range f.calls(t) {
		if strings.Contains(call, want) {
			return true
		}
	}
	return false
}

const onePR = `[{"number":7,"url":"https://github.com/me/fork/pull/7","title":"a fix",` +
	`"isDraft":false,"state":"OPEN","headRefName":"am/x","baseRefName":"main"}]`

func TestGHListOnceNamesTheRepositoryOnlyWhenGiven(t *testing.T) {
	cli := newFakeCLI(t)
	cli.answers(t, "pr-list", onePR)

	got, err := ghListOnce(context.Background(), t.TempDir(), "https://github.com/me/fork")
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(got) != 1 || got[0].Number != 7 || got[0].Head != "am/x" || got[0].Base != "main" {
		t.Fatalf("decoded %+v, want the listing's own fields", got)
	}
	if !cli.called(t, "--repo https://github.com/me/fork") {
		t.Fatalf("calls = %v, want the repository named", cli.calls(t))
	}

	ghListOnce(context.Background(), t.TempDir(), "")
	if last := cli.calls(t); strings.Contains(last[len(last)-1], "--repo") {
		t.Fatalf("an unnamed repository still passed --repo: %q", last[len(last)-1])
	}
}

// A gh that refuses is a row with no badge, not a crash and not a stack of
// zero values.
func TestGHCallsThatRefuseReturnNothing(t *testing.T) {
	newFakeCLI(t, "pr-list", "pr-view", "api")
	ctx, dir := context.Background(), t.TempDir()

	// A refusal has to be distinguishable from an empty repository, or the
	// pass cannot tell "no pull requests" from "GitHub would not say".
	if got, err := ghListOnce(ctx, dir, ""); got != nil || err == nil {
		t.Fatalf("listing = %v, %v; want nothing and an error", got, err)
	}
	if got := ghViewPullRequest(ctx, dir, "https://github.com/me/fork/pull/7"); got.URL != "" {
		t.Fatalf("view = %+v, want the zero pull request", got)
	}
	if got := ghCommitPullRequests(ctx, dir, "me/fork", "abc123"); got != nil {
		t.Fatalf("commit lookup = %v, want nothing", got)
	}
}

func TestGHViewAndCommitLookupBuildTheirCalls(t *testing.T) {
	cli := newFakeCLI(t)
	cli.answers(t, "pr-view", strings.Trim(onePR, "[]"))
	cli.answers(t, "api", onePR)
	ctx, dir := context.Background(), t.TempDir()

	if got := ghViewPullRequest(ctx, dir, "https://github.com/me/fork/pull/7"); got.Number != 7 {
		t.Fatalf("view = %+v", got)
	}
	if !cli.called(t, "pr view https://github.com/me/fork/pull/7") {
		t.Fatalf("calls = %v, want the address passed through", cli.calls(t))
	}

	if got := ghCommitPullRequests(ctx, dir, "me/fork", "abc123"); len(got) != 1 {
		t.Fatalf("commit lookup = %v", got)
	}
	if !cli.called(t, "api repos/me/fork/commits/abc123/pulls") {
		t.Fatalf("calls = %v, want the commit's own path", cli.calls(t))
	}
}

// A commit nobody has pushed, and a repository nothing resolved, are both
// asked about by nobody.
func TestGHCommitLookupSkipsWhatCannotBeAsked(t *testing.T) {
	cli := newFakeCLI(t)
	ctx := context.Background()

	if got := ghCommitPullRequests(ctx, t.TempDir(), "", "abc123"); got != nil {
		t.Fatalf("= %v, want nothing without a repository", got)
	}
	if got := ghCommitPullRequests(ctx, t.TempDir(), "me/fork", ""); got != nil {
		t.Fatalf("= %v, want nothing without a commit", got)
	}
	if calls := cli.calls(t); len(calls) != 0 {
		t.Fatalf("asked anyway: %v", calls)
	}
}

func TestCreatePullRequestPushesThenOpensThenReadsBack(t *testing.T) {
	cli := newFakeCLI(t)
	cli.answers(t, "pr-view", strings.Trim(onePR, "[]"))
	state := prCreateState{root: t.TempDir(), slug: "me/fork", branch: "am/x"}

	pr, err := ghCreatePullRequest(state)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if pr.Number != 7 || pr.Title != "a fix" {
		t.Fatalf("created %+v, want the pull request read back in full", pr)
	}
	for _, want := range []string{
		"git push --set-upstream origin am/x",
		"gh pr create --fill --head am/x --repo me/fork",
		"gh pr view am/x --repo me/fork --json " + prFields,
	} {
		if !cli.called(t, want) {
			t.Fatalf("calls = %v\nwant %q", cli.calls(t), want)
		}
	}
}

// The push comes first because gh cannot open a pull request for a branch the
// host has never seen, so a refused push must stop there and say why.
func TestCreatePullRequestStopsWhenThePushRefuses(t *testing.T) {
	cli := newFakeCLI(t, "git-push")
	state := prCreateState{root: t.TempDir(), slug: "me/fork", branch: "am/x"}

	_, err := ghCreatePullRequest(state)
	if err == nil {
		t.Fatal("a refused push reported success")
	}
	if !strings.Contains(err.Error(), "pushing am/x") || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error = %q, want it to name the step and carry the tool's words", err)
	}
	if cli.called(t, "pr create") {
		t.Fatal("opened a pull request for a branch that was never pushed")
	}
}

func TestCreatePullRequestReportsAGHThatRefuses(t *testing.T) {
	newFakeCLI(t, "pr-create")
	state := prCreateState{root: t.TempDir(), slug: "me/fork", branch: "am/x"}

	if _, err := ghCreatePullRequest(state); err == nil ||
		!strings.Contains(err.Error(), "gh pr create") {
		t.Fatalf("error = %v, want it to name the step that failed", err)
	}
}

// The three ways a pull request arrives have to agree on its shape. The
// listing and the single view ask gh for prFields; the commit lookup goes
// through the REST API, whose own names differ, and reshapes them with jq.
// Nothing else checks that reshape against the struct it decodes into, and a
// field added to one and not the other reads as an empty value rather than an
// error.
func TestEveryQueryAgreesWithTheStruct(t *testing.T) {
	tags := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeOf(pullRequest{})) {
		tags[strings.Split(field.Tag.Get("json"), ",")[0]] = true
	}

	for _, name := range strings.Split(prFields, ",") {
		if !tags[name] {
			t.Errorf("prFields asks gh for %q, which pullRequest cannot hold", name)
		}
	}

	// The jq object body, split on the commas between its entries; each is
	// either "name" or "name: expression".
	body := commitPullsShape[strings.Index(commitPullsShape, "{")+1 : strings.LastIndex(commitPullsShape, "}")]
	produced := map[string]bool{}
	for _, entry := range strings.Split(body, ",") {
		name := strings.TrimSpace(strings.SplitN(entry, ":", 2)[0])
		produced[name] = true
		if !tags[name] {
			t.Errorf("the commit lookup produces %q, which pullRequest cannot hold", name)
		}
	}
	// Fields the commit lookup genuinely cannot answer for. The REST endpoint
	// it uses returns pull requests containing a commit, and every one of
	// these reads back null from it, so a pull request found only that way
	// goes without until a listing covers it too. Named here so adding a field by accident still
	// fails, and adding one deliberately has to say so.
	listingOnly := map[string]bool{
		"statusCheckRollup": true,
		"mergeable":         true,
		"additions":         true,
		"deletions":         true,
		"changedFiles":      true,
	}
	for _, name := range strings.Split(prFields, ",") {
		if !produced[name] && !listingOnly[name] {
			t.Errorf("gh is asked for %q but the commit lookup never produces it", name)
		}
	}
}
