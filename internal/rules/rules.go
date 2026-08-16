// Package rules resolves the instruction files an agent CLI reads before its
// first turn: CLAUDE.md for Claude Code, AGENTS.md for Codex and OpenCode,
// GEMINI.md for Gemini CLI, and the global copy each of them keeps under the
// home directory.
//
// Which file a session actually obeys is a property of the tool it runs, not
// of the manager, so the names come from the tool's config block rather than
// from anything hardcoded here. What this package owns is the resolution: for
// a directory and the repository around it, which of those files exist, where
// they are, and where a missing one would go.
package rules

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Scope separates a file committed with the project from the one the tool
// keeps per machine. The two are edited for different reasons — the project's
// rules are everyone's, the global ones are yours — and a picker that did not
// say which is which would invite writing a personal note into a repository.
type Scope int

const (
	Project Scope = iota
	Global
)

func (s Scope) String() string {
	if s == Global {
		return "global"
	}
	return "project"
}

// Fallback is what a tool with no rules_files of its own is looked up under.
// AGENTS.md is the name the CLIs converged on, so a hand-written tool block
// gets a useful answer rather than an empty picker.
var Fallback = []string{"AGENTS.md"}

// File is one instruction file, whether or not it has been written yet.
// Missing files are listed rather than hidden: "this project tells Claude
// nothing" is exactly what a reader opening the list wants to learn, and the
// path is where the answer to that goes.
type File struct {
	// Path is absolute.
	Path string
	// Label is how the file is named on screen: relative to the repository
	// for a project file, ~-shortened for a global one.
	Label   string
	Scope   Scope
	Exists  bool
	Size    int64
	ModTime time.Time
}

// Find lists the instruction files specs names, for a session working in dir
// inside the repository root.
//
// A bare or relative spec is a project file, looked for in dir and every
// directory up to root, nearest first: agents read the whole chain, so a
// session started a level down must show the rules above it as well as its
// own. When a spec matches nothing in the chain, the repository root is
// listed as where it would go — the file the reader is about to want.
//
// The walk stops at root for the same reason project settings' does: it
// bounds discovery to the user's own checkout instead of climbing into a
// shared parent directory. An empty root reads dir alone.
//
// A spec that is absolute or begins with ~ is a global file and is listed as
// itself, existing or not.
func Find(specs []string, dir, root, home string) []File {
	dir = absOr(dir)
	stop := resolve(dir)
	if root != "" {
		stop = resolve(absOr(root))
	}

	var out []File
	seen := map[string]bool{}
	add := func(file File) {
		if seen[file.Path] {
			return
		}
		seen[file.Path] = true
		out = append(out, file)
	}

	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		if path, ok := globalPath(spec, home); ok {
			add(describe(path, shorten(path, home), Global))
			continue
		}
		clean := filepath.Clean(spec)
		// A spec climbing out of the directory it is resolved against would
		// reach exactly the shared parent the walk refuses to read.
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			continue
		}
		found := false
		for cur := resolve(dir); ; {
			path := filepath.Join(cur, clean)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				add(describe(path, projectLabel(path, stop, home), Project))
				found = true
			}
			if cur == stop {
				break
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				// Above the root without meeting it: dir was not inside the
				// repository it was said to be in.
				break
			}
			cur = parent
		}
		if !found {
			add(File{Path: filepath.Join(stop, clean), Label: clean, Scope: Project})
		}
	}
	return out
}

func describe(path, label string, scope Scope) File {
	file := File{Path: path, Label: label, Scope: scope}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		file.Exists = true
		file.Size = info.Size()
		file.ModTime = info.ModTime()
	}
	return file
}

// globalPath expands a spec that names a file outside the project, reporting
// false for one that does not. A ~ with no home to expand it against is not a
// path we can offer to create, so it is dropped rather than taken literally.
func globalPath(spec, home string) (string, bool) {
	switch {
	case spec == "~" || strings.HasPrefix(spec, "~/"):
		if home == "" {
			return "", false
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(spec, "~"), "/")), true
	case filepath.IsAbs(spec):
		return filepath.Clean(spec), true
	}
	return "", false
}

// shorten is the path as a reader recognizes it: under the home directory it
// wears a ~, and anywhere else it keeps only its last two segments, which is
// the filename plus enough of a parent to tell two CLAUDE.md files apart.
func shorten(path, home string) string {
	if home != "" {
		if rel, err := filepath.Rel(home, path); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join("~", rel)
		}
	}
	dir, base := filepath.Split(path)
	parent := filepath.Base(filepath.Clean(dir))
	if parent == "." || parent == string(filepath.Separator) {
		return base
	}
	return filepath.Join(parent, base)
}

// projectLabel names a project file by its position under the repository —
// "AGENTS.md", "packages/api/AGENTS.md" — so the list reads the way the
// repository is laid out rather than as a column of absolute paths. A file
// that turns out to sit outside the root falls back to the shortened path,
// which at least says where it is.
func projectLabel(path, root, home string) string {
	rel, err := filepath.Rel(root, resolve(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return shorten(path, home)
	}
	return rel
}

// MaxRead caps what the viewer loads. An instruction file is prose meant for
// a context window, so anything past this is not one, and reading it into the
// manager would cost more than showing it is worth.
const MaxRead = 512 << 10

// Read returns a file's text, truncated at MaxRead with the cut reported so
// the viewer can say the end is missing rather than implying the file stops
// there.
func Read(path string) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxRead+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > MaxRead {
		return string(data[:MaxRead]), true, nil
	}
	return string(data), false, nil
}

// Create makes an empty instruction file and the directories above it,
// leaving one that already exists exactly as it is. Empty rather than
// templated: whatever the manager wrote into it would become a rule the agent
// then obeys, and nobody asked for that.
func Create(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return file.Close()
}

func absOr(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// resolve follows symlinks where it can, leaving the path alone where it
// cannot: /tmp on macOS is a symlink, and a root that never compares equal to
// its own children would stop the walk before it started.
func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}
