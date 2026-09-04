package worktree

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

func (w *Worktree) untrackedEntries(ctx context.Context, trackedDirs, trackedFiles map[string]bool) ([]Entry, error) {
	entries, err := w.readDir("")
	if err != nil {
		return nil, err
	}
	var (
		mu    sync.Mutex
		out   []Entry
		wg    sync.WaitGroup
		once  sync.Once
		fail  error
		count atomic.Int64
	)
	sem := make(chan struct{}, w.workers)
	reportFail := func(err error) { once.Do(func() { fail = err }) }
	if err := w.countBudget(&count, len(entries)); err != nil {
		return nil, err
	}

	var walk func(dir string, items []dirEntry)
	walk = func(dir string, items []dirEntry) {
		for _, item := range items {
			if ctx.Err() != nil {
				return
			}
			rel := joinRel(dir, item.name)
			ignored := w.isIgnored(rel, item.isDir)
			if ignored {
				continue
			}
			if !item.isDir {
				if !trackedFiles[rel] {
					mu.Lock()
					out = append(out, Entry{Path: rel, Staged: StatusUnmodified, Unstaged: StatusUntracked})
					mu.Unlock()
				}
				continue
			}
			if trackedDirs[rel] {
				children, err := w.readDirTyped(rel)
				if err != nil {
					reportFail(err)
					continue
				}
				if err := w.countBudget(&count, len(children)); err != nil {
					reportFail(err)
					continue
				}
				walk(rel, children)
				continue
			}
			has, err := w.hasVisibleFile(rel)
			if err != nil {
				reportFail(err)
				continue
			}
			if has {
				mu.Lock()
				out = append(out, Entry{Path: rel + "/", IsDir: true, Staged: StatusUnmodified, Unstaged: StatusUntracked})
				mu.Unlock()
			}
		}
	}

	for _, entry := range entries {
		if entry.Name() == gitDirName && entry.IsDir() {
			continue
		}
		item := dirEntry{name: entry.Name(), isDir: entry.IsDir()}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			walk("", []dirEntry{item})
		})
	}
	wg.Wait()

	if fail != nil {
		return nil, fail
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (w *Worktree) countBudget(count *atomic.Int64, added int) error {
	if w.maxFiles <= 0 {
		return nil
	}
	if count.Add(int64(added)) > int64(w.maxFiles) {
		return fmt.Errorf("%w: %d", ErrTooManyFiles, w.maxFiles)
	}
	return nil
}

func (w *Worktree) hasVisibleFile(dir string) (bool, error) {
	items, err := w.readDirTyped(dir)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		rel := joinRel(dir, item.name)
		ignored := w.isIgnored(rel, item.isDir)
		if ignored {
			continue
		}
		if !item.isDir {
			return true, nil
		}
		has, err := w.hasVisibleFile(rel)
		if err != nil {
			return false, err
		}
		if has {
			return true, nil
		}
	}
	return false, nil
}

type dirEntry struct {
	name  string
	isDir bool
}

func (w *Worktree) readDirTyped(rel string) ([]dirEntry, error) {
	entries, err := w.readDir(rel)
	if err != nil {
		return nil, err
	}
	out := make([]dirEntry, len(entries))
	for i, entry := range entries {
		out[i] = dirEntry{name: entry.Name(), isDir: entry.IsDir()}
	}
	return out, nil
}
