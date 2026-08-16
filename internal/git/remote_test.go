package git

import (
	"errors"
	"testing"
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
