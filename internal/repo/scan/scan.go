package scan

import (
	"bytes"
	"context"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Found struct {
	Path     string
	Bare     bool
	Worktree bool
}

type Options struct {
	Exclude     []string
	SkipHidden  bool
	MaxDepth    int
	IncludeBare bool
	Nested      bool
	Progress    func(dir string)
}

var DefaultExclude = []string{"node_modules", ".cache", "$RECYCLE.BIN", "System Volume Information"}

func Scan(ctx context.Context, root string, opts Options) iter.Seq2[Found, error] {
	if opts.Exclude == nil {
		opts.Exclude = DefaultExclude
	}
	return func(yield func(Found, error) bool) {
		s := scanner{ctx: ctx, opts: opts}
		s.visit(filepath.Clean(root), 0, yield)
	}
}

type scanner struct {
	ctx  context.Context
	opts Options
}

func (s *scanner) visit(dir string, depth int, yield func(Found, error) bool) bool {
	if err := s.ctx.Err(); err != nil {
		yield(Found{}, err)
		return false
	}
	if s.opts.Progress != nil {
		s.opts.Progress(dir)
	}
	if found, ok := classify(dir, s.opts.IncludeBare); ok {
		if !yield(found, nil) {
			return false
		}
		if !s.opts.Nested {
			return true
		}
	}
	if s.opts.MaxDepth > 0 && depth >= s.opts.MaxDepth {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !yield(Found{}, err) {
		return false
	}
	for _, entry := range entries {
		if !s.descends(entry) {
			continue
		}
		if !s.visit(filepath.Join(dir, entry.Name()), depth+1, yield) {
			return false
		}
	}
	return true
}

func (s *scanner) descends(entry os.DirEntry) bool {
	name := entry.Name()
	if !entry.IsDir() || name == ".git" || slices.Contains(s.opts.Exclude, name) {
		return false
	}
	return !s.opts.SkipHidden || !strings.HasPrefix(name, ".")
}

func classify(dir string, includeBare bool) (Found, bool) {
	gitPath := filepath.Join(dir, ".git")
	if st, err := os.Stat(gitPath); err == nil {
		if st.IsDir() {
			if isFile(filepath.Join(gitPath, "HEAD")) {
				return Found{Path: dir}, true
			}
		} else if data, err := os.ReadFile(gitPath); err == nil && bytes.HasPrefix(bytes.TrimSpace(data), []byte("gitdir:")) {
			target := filepath.ToSlash(string(bytes.TrimSpace(data)))
			return Found{Path: dir, Worktree: strings.Contains(target, "/worktrees/")}, true
		}
	}
	if includeBare && isFile(filepath.Join(dir, "HEAD")) && isDir(filepath.Join(dir, "objects")) && isDir(filepath.Join(dir, "refs")) {
		return Found{Path: dir, Bare: true}, true
	}
	return Found{}, false
}

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
