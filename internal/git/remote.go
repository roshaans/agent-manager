package git

import (
	"errors"
	"net/url"
	"regexp"
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
