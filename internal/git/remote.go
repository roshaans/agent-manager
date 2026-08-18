package git

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ErrNoRemote is a repository with nowhere to open: no remote at all, or one
// that names a directory rather than a host.
var ErrNoRemote = errors.New("no remote to open")

// RemoteURL is the address a repository is published at. origin wins when it
// exists, because that is where a branch gets pushed and so where its pull
// request lives; a repository that named its remote something else usually
// has only the one, and that one answers.
func (d *Driver) RemoteURL(root string) (string, error) {
	if out, err := d.run(root, "remote", "get-url", "origin"); err == nil && out != "" {
		return out, nil
	}
	out, err := d.run(root, "remote")
	if err != nil {
		return "", err
	}
	for _, name := range strings.Split(out, "\n") {
		if name = strings.TrimSpace(name); name != "" {
			return d.run(root, "remote", "get-url", name)
		}
	}
	return "", ErrNoRemote
}

// HeadSHA is the commit a checkout is on, in full. A pull request is found
// through it rather than through the branch's name: a name is a label two
// forks can both use and a rename can detach, where a commit either is in a
// pull request or is not.
func (d *Driver) HeadSHA(root string) (string, error) {
	return d.run(root, "rev-parse", "HEAD")
}

// scpRemote is git's scp-style address: an optional user, a host, a colon,
// and the path. It is only tried once a URL scheme has been ruled out, since
// the colon in https:// would otherwise read as that separator.
var scpRemote = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// WebURL is the https page a remote address belongs to, for handing to a
// browser: the same host and path, over https, without the .git suffix.
//
// Userinfo is dropped rather than carried across. A repository cloned with a
// token in its remote — https://x-access-token:<token>@host/owner/repo — would
// otherwise put that token in the address bar and in browser history.
func WebURL(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil || u.Host == "" {
			return "", ErrNoRemote
		}
		host := u.Host
		if u.Scheme != "http" && u.Scheme != "https" {
			// ssh://host:22/… names the ssh port, which the web server does
			// not answer on; an http remote's port is already the web port.
			host = u.Hostname()
		}
		return "https://" + host + webPath(u.Path), nil
	}
	if m := scpRemote.FindStringSubmatch(remote); m != nil {
		return "https://" + m[1] + webPath(m[2]), nil
	}
	return "", ErrNoRemote
}

func webPath(path string) string {
	path = strings.TrimSuffix(strings.TrimRight(path, "/"), ".git")
	if path == "" || strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

// AheadBehind counts what a checkout has that its remote branch does not, and
// the other way round: the two halves of "do I need to push, do I need to
// pull". A branch nobody has pushed has no upstream to compare against and
// answers with an error rather than zeroes, since "in step" and "no idea" are
// different things to show.
//
// Behind is only as fresh as the last fetch, which is what Fetch is for.
func (d *Driver) AheadBehind(root string) (ahead, behind int, err error) {
	out, err := d.run(root, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("rev-list counted %q", out)
	}
	// Left of the range is the upstream's own commits, which is how far this
	// checkout is behind; right is its own, which is how far ahead.
	if behind, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, err
	}
	if ahead, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

// Fetch updates what the repository knows about its remote. Only the
// remote-tracking refs move: no local branch, no index, and nothing in the
// working tree, so it is safe to run under an agent that is mid-edit.
//
// FETCH_HEAD is deliberately left alone. It is a file an agent may be reading
// for its own purposes, and a background pass has no business rewriting it
// every minute.
func (d *Driver) Fetch(root string) error {
	_, err := d.run(root, "fetch", "--quiet", "--no-write-fetch-head")
	return err
}
