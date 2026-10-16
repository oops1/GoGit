package worktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const batchedStatMinimum = 16

var (
	fsLstatFile    = (*os.Root).Lstat
	fsReadlinkFile = (*os.Root).Readlink
	fsReadFileFile = (*os.Root).ReadFile
)

type workingStat struct {
	size    int64
	modTime time.Time
}

type workingScan struct {
	changes map[string]unstagedChange
	stats   map[string]workingStat
}

type directoryListing map[string]fs.DirEntry

func (l directoryListing) info(name string) (fs.FileInfo, bool) {
	item, ok := l[name]
	if !ok {
		return nil, false
	}
	info, err := item.Info()
	return info, err == nil
}

func (w *Worktree) listDirectory(rel string) directoryListing {
	items, err := w.readDir(rel)
	if err != nil {
		return nil
	}
	listing := make(directoryListing, len(items))
	for _, item := range items {
		listing[item.Name()] = item
	}
	return listing
}

func groupByDirectory(entries []*index.Entry) [][]int {
	byDirectory := make(map[string][]int)
	for at, entry := range entries {
		dir := path.Dir(entry.Path)
		byDirectory[dir] = append(byDirectory[dir], at)
	}
	groups := make([][]int, 0, len(byDirectory))
	for _, group := range byDirectory {
		groups = append(groups, group)
	}
	return groups
}

func (w *Worktree) unstagedStatuses(ctx context.Context, entries []*index.Entry, rules gitlinkRules) (workingScan, error) {
	groups := groupByDirectory(entries)
	queue := make(chan []int, len(groups))
	for _, group := range groups {
		queue <- group
	}
	close(queue)
	scanned := make([]unstagedChange, len(entries))
	var (
		wg      sync.WaitGroup
		once    sync.Once
		failure error
	)
	fail := func(err error) { once.Do(func() { failure = err }) }
	for range min(w.workers, len(groups)) {
		wg.Go(func() {
			for group := range queue {
				if err := w.scanGroup(ctx, entries, group, rules, scanned); err != nil {
					fail(err)
					return
				}
			}
		})
	}
	wg.Wait()
	if failure != nil {
		return workingScan{}, failure
	}
	if err := ctx.Err(); err != nil {
		return workingScan{}, err
	}
	scan := workingScan{
		changes: make(map[string]unstagedChange),
		stats:   make(map[string]workingStat, len(entries)),
	}
	for at, change := range scanned {
		if change.code != StatusUnmodified {
			scan.changes[entries[at].Path] = change
		}
		if change.statted {
			scan.stats[entries[at].Path] = change.stat
		}
	}
	return scan, nil
}

func (w *Worktree) scanGroup(ctx context.Context, entries []*index.Entry, group []int, rules gitlinkRules, scanned []unstagedChange) error {
	var listing directoryListing
	if len(group) >= batchedStatMinimum {
		listing = w.listDirectory(path.Dir(entries[group[0]].Path))
	}
	for _, at := range group {
		if err := ctx.Err(); err != nil {
			return err
		}
		change, err := w.unstagedChangeOf(ctx, entries[at], rules, listing)
		if err != nil {
			return err
		}
		scanned[at] = change
	}
	return nil
}

func (w *Worktree) statEntry(rel, name string, listing directoryListing) (fs.FileInfo, error) {
	if info, ok := listing.info(path.Base(rel)); ok {
		return info, nil
	}
	return fsLstatFile(w.root, name)
}

func (w *Worktree) compareToWorktree(entry *index.Entry, listing directoryListing) (StatusCode, fs.FileInfo, error) {
	if entry.SkipWorktree {
		return StatusUnmodified, nil, nil
	}
	name := filepath.FromSlash(entry.Path)
	fi, err := w.statEntry(entry.Path, name, listing)
	if errors.Is(err, fs.ErrNotExist) {
		return StatusDeleted, nil, nil
	}
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, entry.Path, err)
	}
	if fi.IsDir() && !entry.Mode.IsSubmodule() {
		return StatusDeleted, fi, nil
	}
	wantKind, actualKind := kindOfMode(entry.Mode), kindOfInfo(fi)
	if wantKind != actualKind && !w.holdsPlainSymlink(wantKind, fi) {
		return StatusTypeChanged, fi, nil
	}
	if w.currentIndex().MatchesFile(entry, fi, w.symlinks) {
		if w.modeChanged(entry, fi) {
			return StatusModified, fi, nil
		}
		return StatusUnmodified, fi, nil
	}
	if sizeChangedWithoutReading(entry, fi) {
		return StatusModified, fi, nil
	}
	code, err := w.compareContent(entry, name, fi, actualKind)
	return code, fi, err
}

func (w *Worktree) compareContent(entry *index.Entry, name string, fi fs.FileInfo, actualKind entryKind) (StatusCode, error) {
	switch actualKind {
	case kindSymlink:
		target, err := fsReadlinkFile(w.root, name)
		if err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, entry.Path, err)
		}
		id, err := hash.Sum(w.format, "blob", []byte(target))
		if err != nil {
			return 0, err
		}
		if id == entry.ID {
			return StatusUnmodified, nil
		}
		return StatusModified, nil
	default:
		data, err := fsReadFileFile(w.root, name)
		if err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrReadWorkingTree, entry.Path, err)
		}
		id, err := hash.Sum(w.format, "blob", w.convertForCheckin(entry.Path, data))
		if err != nil {
			return 0, err
		}
		if id != entry.ID {
			return StatusModified, nil
		}
		if w.modeChanged(entry, fi) {
			return StatusModified, nil
		}
		return StatusUnmodified, nil
	}
}

func sizeChangedWithoutReading(entry *index.Entry, fi os.FileInfo) bool {
	if entry.Stat.Size == 0 || uint32(fi.Size()) == entry.Stat.Size {
		return false
	}
	return fi.Mode()&os.ModeSymlink == 0 || fi.Size() != 0
}

func (w *Worktree) holdsPlainSymlink(wantKind entryKind, fi os.FileInfo) bool {
	return !w.symlinks && wantKind == kindSymlink && fi.Mode().IsRegular()
}

func (w *Worktree) fillWorkingInfo(e *Entry, known map[string]workingStat) {
	if e.IsDir || !e.ModTime.IsZero() {
		return
	}
	if stat, ok := known[e.Path]; ok {
		e.Size, e.ModTime = stat.size, stat.modTime
		return
	}
	fi, err := fsLstatFile(w.root, filepath.FromSlash(e.Path))
	if err != nil {
		return
	}
	e.Size = fi.Size()
	e.ModTime = fi.ModTime()
}

func (w *Worktree) modeChanged(entry *index.Entry, fi os.FileInfo) bool {
	if !w.fileMode || entry.Mode.IsSymlink() || entry.Mode.IsSubmodule() {
		return false
	}
	wantExec := entry.Mode == object.ModeExecutable
	haveExec := fi.Mode().Perm()&0o111 != 0
	return wantExec != haveExec
}

func (w *Worktree) convertForCheckin(path string, data []byte) []byte {
	return w.policy(path).CompareToGit(data, func() ([]byte, bool) {
		return w.currentIndex().ContentBlob(w.db, path)
	})
}
