package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWebURLFromEveryRemoteForm(t *testing.T) {
	for _, tc := range []struct{ name, remote, want string }{
		{"scp", "git@github.com:owner/repo.git", "https://github.com/owner/repo"},
		{"scp without user", "github.com:owner/repo", "https://github.com/owner/repo"},
		{"ssh url", "ssh://git@github.com/owner/repo.git", "https://github.com/owner/repo"},
		{"https", "https://github.com/owner/repo.git", "https://github.com/owner/repo"},
		{"https without suffix", "https://github.com/owner/repo", "https://github.com/owner/repo"},
		{"trailing slash", "https://github.com/owner/repo/", "https://github.com/owner/repo"},
		{"git protocol", "git://github.com/owner/repo.git", "https://github.com/owner/repo"},
		{"nested group", "git@gitlab.com:team/sub/repo.git", "https://gitlab.com/team/sub/repo"},
		{"self hosted port", "https://git.example.com:8443/owner/repo.git", "https://git.example.com:8443/owner/repo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := WebURL(tc.remote)
			if err != nil {
				t.Fatalf("WebURL(%q): %v", tc.remote, err)
			}
			if got != tc.want {
				t.Fatalf("WebURL(%q) = %q, want %q", tc.remote, got, tc.want)
			}
		})
	}
}

// The ssh port is not the web port, so it is dropped rather than carried into
// an https address nothing answers on.
func TestWebURLDropsTheSSHPort(t *testing.T) {
	got, err := WebURL("ssh://git@github.com:22/owner/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://github.com/owner/repo"; got != want {
		t.Fatalf("WebURL = %q, want %q", got, want)
	}
}

// A token in the remote must not reach the browser's address bar or history.
func TestWebURLStripsCredentials(t *testing.T) {
	got, err := WebURL("https://x-access-token:ghs_secret@github.com/owner/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://github.com/owner/repo"; got != want {
		t.Fatalf("WebURL = %q, want %q", got, want)
	}
}

// A remote that names a directory has no page to open, and saying so beats
// handing a browser something it cannot fetch.
func TestWebURLRejectsRemotesWithNoHost(t *testing.T) {
	for _, remote := range []string{"/srv/git/repo.git", "../sibling", "file:///srv/git/repo.git", ""} {
		if got, err := WebURL(remote); !errors.Is(err, ErrNoRemote) {
			t.Fatalf("WebURL(%q) = %q, %v; want ErrNoRemote", remote, got, err)
		}
	}
}

func TestRemoteURLPrefersOrigin(t *testing.T) {
	driver, dir := testRepo(t)
	gitIn(t, dir, "remote", "add", "upstream", "git@github.com:upstream/repo.git")
	gitIn(t, dir, "remote", "add", "origin", "git@github.com:owner/repo.git")

	got, err := driver.RemoteURL(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "git@github.com:owner/repo.git"; got != want {
		t.Fatalf("RemoteURL = %q, want origin's %q", got, want)
	}
}

// A fork cloned under another name still has somewhere to open.
func TestRemoteURLFallsBackToTheOnlyRemote(t *testing.T) {
	driver, dir := testRepo(t)
	gitIn(t, dir, "remote", "add", "upstream", "git@github.com:upstream/repo.git")

	got, err := driver.RemoteURL(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "git@github.com:upstream/repo.git"; got != want {
		t.Fatalf("RemoteURL = %q, want %q", got, want)
	}
}

func TestRemoteURLWithoutAnyRemote(t *testing.T) {
	driver, dir := testRepo(t)

	if got, err := driver.RemoteURL(dir); err == nil {
		t.Fatalf("RemoteURL = %q, want an error for a repository with no remote", got)
	}
}

// fakeGit puts a stand-in git at a path and hands back a driver pointed at
// it, so a fetch can be observed without a remote to talk to.
func fakeGit(t *testing.T, script string) (*Driver, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "git")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Driver{bin: bin}, dir
}

// A background fetch has no terminal to prompt at and nobody watching for
// one. Every way git could stop and ask has to be turned off, or a remote
// wanting a password sits there until the manager quits.
func TestFetchCannotStopToAskAnyone(t *testing.T) {
	drv, dir := fakeGit(t, `#!/bin/sh
printf '%s\n' "$*" > "$(dirname "$0")/args"
printf 'GIT_TERMINAL_PROMPT=[%s] GIT_ASKPASS=[%s] SSH_ASKPASS=[%s] GIT_SSH_COMMAND=[%s]\n' \
  "$GIT_TERMINAL_PROMPT" "$GIT_ASKPASS" "$SSH_ASKPASS" "$GIT_SSH_COMMAND" > "$(dirname "$0")/env"
exit 0
`)
	if err := drv.Fetch(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	env, err := os.ReadFile(filepath.Join(dir, "env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=[0]", "GIT_ASKPASS=[]", "SSH_ASKPASS=[]", "BatchMode=yes",
	} {
		if !strings.Contains(string(env), want) {
			t.Errorf("fetch ran without %s:\n  %s", want, env)
		}
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	// FETCH_HEAD is a file an agent may be reading for its own purposes.
	if !strings.Contains(string(args), "--no-write-fetch-head") {
		t.Errorf("fetch rewrote FETCH_HEAD: %s", args)
	}
}

// A fetch that hangs must end with the pass rather than outlive it.
func TestFetchStopsWithItsContext(t *testing.T) {
	drv, _ := fakeGit(t, "#!/bin/sh\nsleep 30\n")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := drv.Fetch(ctx, t.TempDir())

	if err == nil {
		t.Fatal("a fetch that outran its context reported success")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("fetch ran %s past its deadline", elapsed)
	}
}

// A branch nobody pushed has no upstream, and zero would report that as
// being in step with one.
func TestAheadBehindWithoutAnUpstream(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "one")
	commit(t, dir, "first")

	if _, _, err := driver.AheadBehind(dir); err == nil {
		t.Fatal("a branch with no upstream reported a count")
	}
}
