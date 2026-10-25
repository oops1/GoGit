package worktree

import (
	"context"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
)

type untrackedScan struct {
	w            *Worktree
	ctx          context.Context
	trackedDirs  map[string]bool
	trackedFiles map[string]bool
	listings     map[string][]fs.DirEntry

	mu      sync.Mutex
	out     []Entry
	wg      sync.WaitGroup
	once    sync.Once
	failure error
	count   atomic.Int64
	slots   chan struct{}
}

func (w *Worktree) untrackedEntries(ctx context.Context, trackedDirs, trackedFiles map[string]bool, listings map[string][]fs.DirEntry) ([]Entry, error) {
	scan := &untrackedScan{
		w:            w,
		ctx:          ctx,
		trackedDirs:  trackedDirs,
		trackedFiles: trackedFiles,
		listings:     listings,
		slots:        make(chan struct{}, w.workers),
	}
	items, err := scan.readTrackedDir("")
	if err != nil {
		return nil, err
	}
	if err := scan.budget(len(items)); err != nil {
		return nil, err
	}
	scan.visit("", withoutGitDir(items))
	scan.wg.Wait()
	if scan.failure != nil {
		return nil, scan.failure
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return scan.out, nil
}

func withoutGitDir(items []dirEntry) []dirEntry {
	kept := make([]dirEntry, 0, len(items))
	for _, item := range items {
		if item.name != gitDirName {
			kept = append(kept, item)
		}
	}
	return kept
}

func (s *untrackedScan) fail(err error) {
	s.once.Do(func() { s.failure = err })
}

func (s *untrackedScan) budget(added int) error {
	if s.w.maxFiles <= 0 {
		return nil
	}
	if s.count.Add(int64(added)) > int64(s.w.maxFiles) {
		return fmt.Errorf("%w: %d", ErrTooManyFiles, s.w.maxFiles)
	}
	return nil
}

func (s *untrackedScan) record(entry Entry) {
	s.mu.Lock()
	s.out = append(s.out, entry)
	s.mu.Unlock()
}

func (s *untrackedScan) descend(rel string) {
	select {
	case s.slots <- struct{}{}:
		s.wg.Go(func() {
			defer func() { <-s.slots }()
			s.enter(rel)
		})
	default:
		s.enter(rel)
	}
}

func (s *untrackedScan) readTrackedDir(rel string) ([]dirEntry, error) {
	if items, ok := s.listings[rel]; ok {
		return typedEntries(items), nil
	}
	return s.w.readDirTyped(rel)
}

func (s *untrackedScan) enter(rel string) {
	children, err := s.readTrackedDir(rel)
	if err != nil {
		s.fail(err)
		return
	}
	if err := s.budget(len(children)); err != nil {
		s.fail(err)
		return
	}
	s.visit(rel, children)
}

func (s *untrackedScan) visit(dir string, items []dirEntry) {
	for _, item := range items {
		if s.ctx.Err() != nil {
			return
		}
		rel := joinRel(dir, item.name)
		if !item.isDir {
			s.visitFile(rel, item)
			continue
		}
		s.visitDirectory(rel)
	}
}

func (s *untrackedScan) visitFile(rel string, item dirEntry) {
	if s.trackedFiles[rel] {
		return
	}
	code := StatusUntracked
	if s.w.isIgnored(rel, false) {
		code = StatusIgnored
	}
	entry := Entry{Path: rel, Staged: StatusUnmodified, Unstaged: code}
	if info, ok := item.describe(); ok {
		entry.Size, entry.ModTime = info.Size(), info.ModTime()
	}
	s.record(entry)
}

func (s *untrackedScan) visitDirectory(rel string) {
	if s.trackedFiles[rel] {
		return
	}
	if s.trackedDirs[rel] {
		s.descend(rel)
		return
	}
	visible, anyFile, err := s.w.directoryContents(rel)
	if err != nil {
		s.fail(err)
		return
	}
	switch {
	case visible:
		s.record(Entry{Path: rel + "/", IsDir: true, Staged: StatusUnmodified, Unstaged: StatusUntracked})
	case anyFile:
		s.record(Entry{Path: rel + "/", IsDir: true, Staged: StatusUnmodified, Unstaged: StatusIgnored})
	}
}

func (w *Worktree) directoryContents(dir string) (visible, anyFile bool, err error) {
	items, err := w.readDirTyped(dir)
	if err != nil {
		return false, false, err
	}
	for _, item := range items {
		rel := joinRel(dir, item.name)
		ignored := w.isIgnored(rel, item.isDir)
		switch {
		case !item.isDir:
			anyFile = true
			if !ignored {
				return true, true, nil
			}
		case ignored:
			if anyFile {
				continue
			}
			if anyFile, err = w.hasAnyFile(rel); err != nil {
				return false, false, err
			}
		default:
			nestedVisible, nestedAny, err := w.directoryContents(rel)
			if err != nil {
				return false, false, err
			}
			if nestedVisible {
				return true, true, nil
			}
			anyFile = anyFile || nestedAny
		}
	}
	return false, anyFile, nil
}

func (w *Worktree) hasAnyFile(dir string) (bool, error) {
	items, err := w.readDirTyped(dir)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if !item.isDir {
			return true, nil
		}
		has, err := w.hasAnyFile(joinRel(dir, item.name))
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
	entry fs.DirEntry
}

func (e dirEntry) describe() (fs.FileInfo, bool) {
	if e.entry == nil {
		return nil, false
	}
	info, err := e.entry.Info()
	return info, err == nil
}

func (w *Worktree) readDirTyped(rel string) ([]dirEntry, error) {
	entries, err := w.readDir(rel)
	if err != nil {
		return nil, err
	}
	return typedEntries(entries), nil
}

func typedEntries(entries []fs.DirEntry) []dirEntry {
	out := make([]dirEntry, len(entries))
	for i, entry := range entries {
		out[i] = dirEntry{name: entry.Name(), isDir: entry.IsDir(), entry: entry}
	}
	return out
}
