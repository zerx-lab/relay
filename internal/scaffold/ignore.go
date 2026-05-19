// Package scaffold implements the project generator behind `zerx new`.
//
// The flow:
//
//  1. fetch.go    resolves a release tag against the GitHub API and streams
//     the source tarball into a temp directory.
//  2. ignore.go   filters out CLI-only paths declared in .scaffoldignore.
//  3. rewrite.go  rewrites the Go module path (and package.json name) so the
//     generated project compiles under the user-supplied identity.
//
// Everything is stdlib-only on purpose: the CLI is small enough that pulling
// in cobra/survey/go-git is more cost than benefit.
package scaffold

import (
	"bufio"
	"io"
	"strings"
)

// Ignorer decides whether an extracted tarball entry should be dropped on its
// way to the user's project directory. Patterns are anchored, forward-slash,
// and either match a file exactly or, with a trailing "/", a directory prefix.
type Ignorer struct {
	files []string // exact paths
	dirs  []string // directory prefixes, each ending with "/"
}

// ParseIgnore reads a .scaffoldignore-style list. Blank lines and lines
// starting with "#" are skipped. Leading "/" is stripped (patterns are always
// anchored to the tree root).
func ParseIgnore(r io.Reader) (*Ignorer, error) {
	ig := &Ignorer{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "/")
		if strings.HasSuffix(line, "/") {
			ig.dirs = append(ig.dirs, line)
		} else {
			ig.files = append(ig.files, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ig, nil
}

// Match reports whether the given path (forward-slash, relative to the tree
// root, no leading slash) should be ignored.
func (ig *Ignorer) Match(path string) bool {
	if ig == nil {
		return false
	}
	for _, f := range ig.files {
		if path == f {
			return true
		}
	}
	for _, d := range ig.dirs {
		if strings.HasPrefix(path, d) {
			return true
		}
	}
	return false
}
