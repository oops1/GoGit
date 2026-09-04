package watch

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	headFileName         = "HEAD"
	indexFileName        = "index"
	packedRefsFileName   = "packed-refs"
	refsDirName          = "refs"
	dotGitDirName        = ".git"
	defaultWorkTreeDepth = 2
	defaultMinInterval   = time.Second
	defaultMaxInterval   = 8 * time.Second
	defaultMaxEntries    = 5000
)

var stateFileNames = []string{"MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD"}

var stateDirNames = []string{"rebase-merge", "rebase-apply", "sequencer"}

type Kind int

const (
	Head Kind = iota
	Index
	Refs
	State
	WorkTree
)

type Change struct {
	Kind Kind
}

type ChangeSet map[Change]struct{}

func (c ChangeSet) Has(kind Kind) bool {
	_, ok := c[Change{Kind: kind}]
	return ok
}

func (c ChangeSet) add(kind Kind) ChangeSet {
	if c == nil {
		c = make(ChangeSet)
	}
	c[Change{Kind: kind}] = struct{}{}
	return c
}

type Entry struct {
	Kind    Kind
	Size    int64
	ModTime time.Time
	Exists  bool
}

func (e Entry) equal(o Entry) bool {
	return e.Kind == o.Kind && e.Exists == o.Exists && e.Size == o.Size && e.ModTime.Equal(o.ModTime)
}

type Snapshot map[string]Entry

func Diff(prev, next Snapshot) ChangeSet {
	var changes ChangeSet
	for path, before := range prev {
		after, ok := next[path]
		if !ok || !before.equal(after) {
			changes = changes.add(before.Kind)
		}
	}
	for path, after := range next {
		if _, ok := prev[path]; !ok {
			changes = changes.add(after.Kind)
		}
	}
	return changes
}

type Options struct {
	WorkTreeDepth int
	MinInterval   time.Duration
	MaxInterval   time.Duration
	MaxEntries    int
	Snapshot      func() Snapshot
}

func (o Options) normalize() Options {
	if o.WorkTreeDepth <= 0 {
		o.WorkTreeDepth = defaultWorkTreeDepth
	}
	if o.MinInterval <= 0 {
		o.MinInterval = defaultMinInterval
	}
	if o.MaxInterval <= 0 {
		o.MaxInterval = defaultMaxInterval
	}
	if o.MaxEntries <= 0 {
		o.MaxEntries = defaultMaxEntries
	}
	return o
}

type Watcher struct {
	layout     repo.Layout
	opts       Options
	snapshotFn func() Snapshot

	mu       sync.Mutex
	paused   bool
	resumeCh chan struct{}
	pokeCh   chan struct{}

	baseline     Snapshot
	baselineOnce sync.Once
}

func New(layout repo.Layout, opts Options) *Watcher {
	opts = opts.normalize()
	w := &Watcher{layout: layout, opts: opts, pokeCh: make(chan struct{}, 1)}
	if opts.Snapshot != nil {
		w.snapshotFn = opts.Snapshot
	} else {
		w.snapshotFn = func() Snapshot { return take(w.layout, w.opts) }
	}
	w.baseline = w.snapshotFn()
	return w
}

func (w *Watcher) initialSnapshot() Snapshot {
	first := false
	w.baselineOnce.Do(func() { first = true })
	if first {
		return w.baseline
	}
	return w.snapshotFn()
}

func (w *Watcher) Poke() {
	select {
	case w.pokeCh <- struct{}{}:
	default:
	}
}

func (w *Watcher) Pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.paused {
		w.paused = true
		w.resumeCh = make(chan struct{})
	}
}

func (w *Watcher) Resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.paused {
		w.paused = false
		close(w.resumeCh)
	}
}

func (w *Watcher) resumeSignal() chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.paused {
		return nil
	}
	return w.resumeCh
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func (w *Watcher) Run(ctx context.Context) iter.Seq[ChangeSet] {
	return func(yield func(ChangeSet) bool) {
		prev := w.initialSnapshot()
		interval := w.opts.MinInterval
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			if resumeCh := w.resumeSignal(); resumeCh != nil {
				select {
				case <-ctx.Done():
					return
				case <-resumeCh:
				}
				prev = w.snapshotFn()
				interval = w.opts.MinInterval
				timer.Reset(interval)
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-w.pokeCh:
			case <-timer.C:
			}
			if w.resumeSignal() != nil {
				timer.Reset(w.opts.MinInterval)
				continue
			}
			next := w.snapshotFn()
			changes := Diff(prev, next)
			prev = next
			if len(changes) == 0 {
				interval = minDuration(interval*2, w.opts.MaxInterval)
			} else {
				interval = w.opts.MinInterval
				if !yield(changes) {
					return
				}
			}
			timer.Reset(interval)
		}
	}
}

func gitPath(layout repo.Layout, rel string) string {
	return filepath.Join(layout.GitDir, filepath.FromSlash(rel))
}

func commonPath(layout repo.Layout, rel string) string {
	return filepath.Join(layout.CommonDir, filepath.FromSlash(rel))
}

func addPath(snap Snapshot, kind Kind, path string) {
	entry := Entry{Kind: kind}
	if info, err := os.Stat(path); err == nil {
		entry.Size = info.Size()
		entry.ModTime = info.ModTime()
		entry.Exists = true
	}
	snap[path] = entry
}

func addRefsTree(snap Snapshot, root string) {
	addPath(snap, Refs, root)
	walkRefsTree(snap, root)
}

func walkRefsTree(snap Snapshot, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		addPath(snap, Refs, path)
		if e.IsDir() {
			walkRefsTree(snap, path)
		}
	}
}

func collectWorkTreeDirs(root string, depth, budget int) ([]string, bool) {
	if budget <= 0 {
		return nil, false
	}
	var dirs []string
	var walk func(dir string, level int) bool
	walk = func(dir string, level int) bool {
		dirs = append(dirs, dir)
		if len(dirs) > budget {
			return false
		}
		if level >= depth {
			return true
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return true
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == dotGitDirName {
				continue
			}
			if !walk(filepath.Join(dir, e.Name()), level+1) {
				return false
			}
		}
		return true
	}
	if !walk(root, 0) {
		return nil, false
	}
	return dirs, true
}

func addWorkTree(snap Snapshot, layout repo.Layout, opts Options) {
	root := layout.WorkTree
	if root == "" {
		return
	}
	budget := opts.MaxEntries - len(snap)
	dirs, ok := collectWorkTreeDirs(root, opts.WorkTreeDepth, budget)
	if !ok {
		addPath(snap, WorkTree, root)
		return
	}
	for _, dir := range dirs {
		addPath(snap, WorkTree, dir)
	}
}

func take(layout repo.Layout, opts Options) Snapshot {
	snap := make(Snapshot)
	addPath(snap, Head, gitPath(layout, headFileName))
	addPath(snap, Index, gitPath(layout, indexFileName))
	addPath(snap, Refs, commonPath(layout, packedRefsFileName))
	addRefsTree(snap, commonPath(layout, refsDirName))
	for _, name := range stateFileNames {
		addPath(snap, State, gitPath(layout, name))
	}
	for _, name := range stateDirNames {
		addPath(snap, State, gitPath(layout, name))
	}
	addWorkTree(snap, layout, opts)
	return snap
}
