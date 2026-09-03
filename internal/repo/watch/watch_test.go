package watch

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

func newTestLayout(t *testing.T, bare bool) repo.Layout {
	t.Helper()
	dir := t.TempDir()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", global, err)
	}
	r, err := repo.Init(dir, repo.InitOptions{
		Bare:       bare,
		Env:        func(string) string { return "" },
		NoSystem:   true,
		GlobalFile: global,
	})
	if err != nil {
		t.Fatalf("Init(%q) returned error %v", dir, err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close returned error %v", err)
		}
	})
	return r.Layout()
}

func TestChangeSetHasReportsMembership(t *testing.T) {
	var changes ChangeSet
	changes = changes.add(Head)
	changes = changes.add(Refs)
	if !changes.Has(Head) || !changes.Has(Refs) {
		t.Fatalf("changes = %v, want Head and Refs", changes)
	}
	if changes.Has(Index) {
		t.Fatalf("changes = %v, must not contain Index", changes)
	}
}

func TestChangeSetHasOnNilSetReturnsFalse(t *testing.T) {
	var changes ChangeSet
	if changes.Has(Head) {
		t.Fatal("nil ChangeSet must report no members")
	}
}

func TestEntryEqualComparesAllFields(t *testing.T) {
	base := Entry{Kind: Head, Size: 10, ModTime: time.Unix(100, 0), Exists: true}
	cases := []struct {
		name  string
		other Entry
		want  bool
	}{
		{"identical", base, true},
		{"different kind", Entry{Kind: Index, Size: 10, ModTime: time.Unix(100, 0), Exists: true}, false},
		{"different size", Entry{Kind: Head, Size: 11, ModTime: time.Unix(100, 0), Exists: true}, false},
		{"different modtime", Entry{Kind: Head, Size: 10, ModTime: time.Unix(200, 0), Exists: true}, false},
		{"different exists", Entry{Kind: Head, Size: 10, ModTime: time.Unix(100, 0), Exists: false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := base.equal(tc.other); got != tc.want {
				t.Fatalf("equal = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDiffOfIdenticalSnapshotsIsEmpty(t *testing.T) {
	snap := Snapshot{"a": {Kind: Refs, Size: 1, ModTime: time.Unix(1, 0), Exists: true}}
	if changes := Diff(snap, snap); len(changes) != 0 {
		t.Fatalf("changes = %v, want none", changes)
	}
}

func TestDiffDetectsChangedAddedAndRemovedEntries(t *testing.T) {
	base := time.Unix(1000, 0)
	prev := Snapshot{
		"head":    {Kind: Head, Size: 5, ModTime: base, Exists: true},
		"index":   {Kind: Index, Size: 5, ModTime: base, Exists: true},
		"removed": {Kind: State, Size: 1, ModTime: base, Exists: true},
	}
	next := Snapshot{
		"head":  {Kind: Head, Size: 6, ModTime: base, Exists: true},
		"index": {Kind: Index, Size: 5, ModTime: base, Exists: true},
		"added": {Kind: WorkTree, Size: 1, ModTime: base, Exists: true},
	}
	changes := Diff(prev, next)
	for _, k := range []Kind{Head, State, WorkTree} {
		if !changes.Has(k) {
			t.Errorf("changes = %v, missing %v", changes, k)
		}
	}
	if changes.Has(Index) {
		t.Errorf("changes = %v, must not report unchanged Index", changes)
	}
	if len(changes) != 3 {
		t.Fatalf("len(changes) = %d, want 3", len(changes))
	}
}

func TestOptionsNormalizeFillsDefaults(t *testing.T) {
	got := Options{}.normalize()
	want := Options{WorkTreeDepth: defaultWorkTreeDepth, MinInterval: defaultMinInterval, MaxInterval: defaultMaxInterval, MaxEntries: defaultMaxEntries}
	if got != want {
		t.Fatalf("normalize() = %+v, want %+v", got, want)
	}
}

func TestOptionsNormalizeKeepsProvidedValues(t *testing.T) {
	want := Options{WorkTreeDepth: 5, MinInterval: 2 * time.Second, MaxInterval: 30 * time.Second, MaxEntries: 10}
	if got := want.normalize(); got != want {
		t.Fatalf("normalize() = %+v, want %+v", got, want)
	}
}

func TestMinDurationReturnsTheSmallerValue(t *testing.T) {
	if got := minDuration(2*time.Second, 3*time.Second); got != 2*time.Second {
		t.Fatalf("minDuration = %v, want 2s", got)
	}
	if got := minDuration(5*time.Second, 3*time.Second); got != 3*time.Second {
		t.Fatalf("minDuration = %v, want 3s", got)
	}
}

func TestGitPathJoinsRelativeToGitDir(t *testing.T) {
	layout := repo.Layout{GitDir: filepath.Join("a", "b"), CommonDir: filepath.Join("a", "b", "common")}
	if got, want := gitPath(layout, "HEAD"), filepath.Join(layout.GitDir, "HEAD"); got != want {
		t.Fatalf("gitPath = %q, want %q", got, want)
	}
}

func TestCommonPathJoinsRelativeToCommonDir(t *testing.T) {
	layout := repo.Layout{GitDir: filepath.Join("a", "b"), CommonDir: filepath.Join("a", "common")}
	if got, want := commonPath(layout, "refs"), filepath.Join(layout.CommonDir, "refs"); got != want {
		t.Fatalf("commonPath = %q, want %q", got, want)
	}
}

func TestAddPathMarksMissingPathAsAbsent(t *testing.T) {
	snap := make(Snapshot)
	missing := filepath.Join(t.TempDir(), "nope")
	addPath(snap, Head, missing)
	e := snap[missing]
	if e.Exists || e.Kind != Head {
		t.Fatalf("entry = %+v, want an absent Head entry", e)
	}
}

func TestAddPathReportsSizeAndModTimeForAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	snap := make(Snapshot)
	addPath(snap, Index, path)
	e := snap[path]
	if !e.Exists || e.Kind != Index || e.Size != 5 {
		t.Fatalf("entry = %+v, want existing Index entry with size 5", e)
	}
}

func TestWalkRefsTreeIgnoresAnUnreadableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	snap := make(Snapshot)
	walkRefsTree(snap, file)
	if len(snap) != 0 {
		t.Fatalf("snap = %v, want empty", snap)
	}
}

func TestCollectWorkTreeDirsWalksUpToDepth(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(a, "b")
	c := filepath.Join(b, "c")
	for _, dir := range []string{a, b, c} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) returned error %v", dir, err)
		}
	}
	dirs, ok := collectWorkTreeDirs(root, 2, 100)
	if !ok {
		t.Fatal("collectWorkTreeDirs reported an exceeded budget")
	}
	for _, want := range []string{root, a, b} {
		if !slices.Contains(dirs, want) {
			t.Fatalf("dirs = %v, missing %q", dirs, want)
		}
	}
	if slices.Contains(dirs, c) {
		t.Fatalf("dirs = %v, must not contain %q beyond depth", dirs, c)
	}
	if len(dirs) != 3 {
		t.Fatalf("len(dirs) = %d, want 3", len(dirs))
	}
}

func TestCollectWorkTreeDirsSkipsDotGitAndPlainFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dotGit := filepath.Join(root, ".git")
	for _, dir := range []string{src, dotGit} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) returned error %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	dirs, ok := collectWorkTreeDirs(root, 1, 100)
	if !ok {
		t.Fatal("collectWorkTreeDirs reported an exceeded budget")
	}
	if !slices.Contains(dirs, root) || !slices.Contains(dirs, src) {
		t.Fatalf("dirs = %v, want root and src", dirs)
	}
	if slices.Contains(dirs, dotGit) {
		t.Fatalf("dirs = %v, must skip .git", dirs)
	}
	if len(dirs) != 2 {
		t.Fatalf("len(dirs) = %d, want 2", len(dirs))
	}
}

func TestCollectWorkTreeDirsReturnsFalseWithNoBudget(t *testing.T) {
	if dirs, ok := collectWorkTreeDirs(t.TempDir(), 2, 0); ok || dirs != nil {
		t.Fatalf("collectWorkTreeDirs(budget=0) = (%v, %v), want (nil, false)", dirs, ok)
	}
}

func TestCollectWorkTreeDirsReturnsFalseWhenBudgetExceededMidWalk(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 10; i++ {
		dir := filepath.Join(root, "sub"+strconv.Itoa(i))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("Mkdir(%q) returned error %v", dir, err)
		}
	}
	if dirs, ok := collectWorkTreeDirs(root, 1, 3); ok || dirs != nil {
		t.Fatalf("collectWorkTreeDirs = (%v, %v), want (nil, false)", dirs, ok)
	}
}

func TestCollectWorkTreeDirsTreatsAnUnreadableRootAsASingleEntry(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	dirs, ok := collectWorkTreeDirs(file, 2, 10)
	if !ok {
		t.Fatal("collectWorkTreeDirs reported an exceeded budget for an unreadable root")
	}
	if len(dirs) != 1 || dirs[0] != file {
		t.Fatalf("dirs = %v, want [%q]", dirs, file)
	}
}

func TestAddWorkTreeAddsDirectoriesUpToDepth(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir returned error %v", err)
	}
	snap := make(Snapshot)
	addWorkTree(snap, repo.Layout{WorkTree: root}, Options{}.normalize())
	if e, ok := snap[root]; !ok || e.Kind != WorkTree {
		t.Fatalf("root entry = %+v, ok=%v", e, ok)
	}
	if e, ok := snap[sub]; !ok || e.Kind != WorkTree {
		t.Fatalf("sub entry = %+v, ok=%v", e, ok)
	}
}

func TestAddWorkTreeSkippedWhenLayoutHasNoWorkTree(t *testing.T) {
	snap := make(Snapshot)
	addWorkTree(snap, repo.Layout{}, Options{}.normalize())
	if len(snap) != 0 {
		t.Fatalf("snap = %v, want empty for a bare repository layout", snap)
	}
}

func TestAddWorkTreeFallsBackToRootWhenBudgetExhausted(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := os.Mkdir(filepath.Join(root, "sub"+strconv.Itoa(i)), 0o755); err != nil {
			t.Fatalf("Mkdir returned error %v", err)
		}
	}
	snap := make(Snapshot)
	opts := Options{WorkTreeDepth: 1, MaxEntries: 3, MinInterval: time.Second, MaxInterval: time.Second}
	addWorkTree(snap, repo.Layout{WorkTree: root}, opts)
	if len(snap) != 1 {
		t.Fatalf("snap = %v, want a single fallback entry", snap)
	}
	e, ok := snap[root]
	if !ok || e.Kind != WorkTree {
		t.Fatalf("fallback entry = %+v, ok=%v", e, ok)
	}
}

func TestTakeReportsHeadIndexPackedRefsAndRefsTree(t *testing.T) {
	layout := newTestLayout(t, false)
	snap := take(layout, Options{}.normalize())

	head := snap[gitPath(layout, "HEAD")]
	if !head.Exists || head.Kind != Head {
		t.Fatalf("HEAD entry = %+v", head)
	}
	index := snap[gitPath(layout, "index")]
	if index.Exists || index.Kind != Index {
		t.Fatalf("index entry = %+v, want absent", index)
	}
	packed := snap[commonPath(layout, "packed-refs")]
	if packed.Exists || packed.Kind != Refs {
		t.Fatalf("packed-refs entry = %+v, want absent", packed)
	}
	refsDir := snap[commonPath(layout, "refs")]
	if !refsDir.Exists || refsDir.Kind != Refs {
		t.Fatalf("refs entry = %+v, want present", refsDir)
	}
	heads := snap[commonPath(layout, "refs/heads")]
	if !heads.Exists || heads.Kind != Refs {
		t.Fatalf("refs/heads entry = %+v, want present", heads)
	}
}

func TestTakeDetectsALooseRefFile(t *testing.T) {
	layout := newTestLayout(t, false)
	ref := filepath.Join(layout.CommonDir, "refs", "heads", "topic")
	if err := os.WriteFile(ref, []byte("0000000000000000000000000000000000000000\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	snap := take(layout, Options{}.normalize())
	entry, ok := snap[ref]
	if !ok || !entry.Exists || entry.Kind != Refs {
		t.Fatalf("loose ref entry = %+v, ok=%v", entry, ok)
	}
}

func TestTakeDetectsStateFilesAndDirectories(t *testing.T) {
	layout := newTestLayout(t, false)
	mergeHead := gitPath(layout, "MERGE_HEAD")
	if err := os.WriteFile(mergeHead, []byte("abc\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	rebaseMerge := gitPath(layout, "rebase-merge")
	if err := os.Mkdir(rebaseMerge, 0o755); err != nil {
		t.Fatalf("Mkdir returned error %v", err)
	}
	snap := take(layout, Options{}.normalize())
	if e := snap[mergeHead]; !e.Exists || e.Kind != State {
		t.Fatalf("MERGE_HEAD entry = %+v", e)
	}
	if e := snap[rebaseMerge]; !e.Exists || e.Kind != State {
		t.Fatalf("rebase-merge entry = %+v", e)
	}
	if e := snap[gitPath(layout, "REBASE_HEAD")]; e.Exists {
		t.Fatalf("REBASE_HEAD entry = %+v, want absent", e)
	}
	if e := snap[gitPath(layout, "sequencer")]; e.Exists {
		t.Fatalf("sequencer entry = %+v, want absent", e)
	}
}

func TestTakeReportsWorkTreeDirectoriesAndSkipsDotGit(t *testing.T) {
	layout := newTestLayout(t, false)
	sub := filepath.Join(layout.WorkTree, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir returned error %v", err)
	}
	snap := take(layout, Options{}.normalize())
	if e := snap[layout.WorkTree]; !e.Exists || e.Kind != WorkTree {
		t.Fatalf("work tree root entry = %+v", e)
	}
	if e := snap[sub]; !e.Exists || e.Kind != WorkTree {
		t.Fatalf("work tree subdirectory entry = %+v", e)
	}
	if _, ok := snap[filepath.Join(layout.WorkTree, ".git")]; ok {
		t.Fatal("the .git directory must not be tracked as a work tree entry")
	}
}

func TestTakeOmitsWorkTreeEntriesForABareRepository(t *testing.T) {
	layout := newTestLayout(t, true)
	snap := take(layout, Options{}.normalize())
	for path, e := range snap {
		if e.Kind == WorkTree {
			t.Fatalf("unexpected work tree entry for a bare repository: %s", path)
		}
	}
}

func TestNewNormalizesOptions(t *testing.T) {
	w := New(repo.Layout{}, Options{})
	want := Options{WorkTreeDepth: defaultWorkTreeDepth, MinInterval: defaultMinInterval, MaxInterval: defaultMaxInterval, MaxEntries: defaultMaxEntries}
	if w.opts != want {
		t.Fatalf("opts = %+v, want %+v", w.opts, want)
	}
}

func TestNewSnapshotFnReflectsTheGivenLayout(t *testing.T) {
	layout := newTestLayout(t, false)
	w := New(layout, Options{})
	snap := w.snapshotFn()
	if e, ok := snap[gitPath(layout, "HEAD")]; !ok || !e.Exists {
		t.Fatalf("snapshot missing HEAD entry: %+v", snap)
	}
}

func TestPokeDropsExtraSignalsWhenAlreadyQueued(t *testing.T) {
	w := New(repo.Layout{}, Options{})
	w.Poke()
	w.Poke()
	if len(w.pokeCh) != 1 {
		t.Fatalf("len(pokeCh) = %d, want 1", len(w.pokeCh))
	}
}

func TestPauseIsIdempotentWhileAlreadyPaused(t *testing.T) {
	w := New(repo.Layout{}, Options{})
	w.Pause()
	first := w.resumeCh
	w.Pause()
	if w.resumeCh != first {
		t.Fatal("a second Pause call must not replace the resume channel")
	}
}

func TestResumeIsANoOpWhenNotPaused(t *testing.T) {
	w := New(repo.Layout{}, Options{})
	w.Resume()
	if w.paused {
		t.Fatal("paused must remain false")
	}
}

func TestRunReportsHeadChangeWithinMinInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		layout := newTestLayout(t, false)
		w := New(layout, Options{MinInterval: time.Second, MaxInterval: 4 * time.Second})
		headFile := gitPath(layout, "HEAD")
		ctx, cancel := context.WithCancel(t.Context())
		results := make(chan ChangeSet, 4)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for cs := range w.Run(ctx) {
				results <- cs
			}
		}()
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(headFile, []byte("ref: refs/heads/a-rather-longer-branch-name\n"), 0o644); err != nil {
			t.Fatalf("WriteFile returned error %v", err)
		}
		select {
		case cs := <-results:
			if !cs.Has(Head) {
				t.Fatalf("changes = %v, want Head", cs)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no change reported within MinInterval")
		}
		cancel()
		<-done
	})
}

func TestRunIntervalGrowsDuringSilenceUpToMax(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := New(repo.Layout{}, Options{MinInterval: time.Second, MaxInterval: 4 * time.Second})
		var calls atomic.Int32
		w.snapshotFn = func() Snapshot {
			calls.Add(1)
			return Snapshot{}
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range w.Run(ctx) {
				t.Error("no changes expected")
			}
		}()
		time.Sleep(9500 * time.Millisecond)
		cancel()
		<-done
		if got := calls.Load(); got != 4 {
			t.Fatalf("snapshotFn called %d times, want 4", got)
		}
	})
}

func TestRunPokeTriggersAnImmediatePoll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := New(repo.Layout{}, Options{MinInterval: time.Hour, MaxInterval: time.Hour})
		polled := make(chan struct{}, 8)
		w.snapshotFn = func() Snapshot {
			polled <- struct{}{}
			return Snapshot{}
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range w.Run(ctx) {
			}
		}()
		<-polled
		w.Poke()
		select {
		case <-polled:
		case <-time.After(time.Second):
			t.Fatal("Poke did not trigger an immediate poll")
		}
		cancel()
		<-done
	})
}

func TestRunPausedBeforeStartBlocksUntilResumeOrCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := New(repo.Layout{}, Options{MinInterval: time.Millisecond, MaxInterval: time.Millisecond})
		w.snapshotFn = func() Snapshot { return Snapshot{} }
		w.Pause()
		ctx, cancel := context.WithCancel(t.Context())
		results := make(chan ChangeSet, 4)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for cs := range w.Run(ctx) {
				results <- cs
			}
		}()
		time.Sleep(time.Second)
		select {
		case cs := <-results:
			t.Fatalf("unexpected change while paused before start: %v", cs)
		default:
		}
		cancel()
		<-done
	})
}

func TestRunContextCancelWhilePausedEndsIterator(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := New(repo.Layout{}, Options{})
		w.snapshotFn = func() Snapshot { return Snapshot{} }
		w.Pause()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range w.Run(ctx) {
			}
		}()
		time.Sleep(time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Run did not stop after context cancellation while paused")
		}
	})
}

func TestRunPauseSuppressesPollingUntilResume(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		layout := newTestLayout(t, false)
		w := New(layout, Options{MinInterval: time.Second, MaxInterval: 2 * time.Second})
		headFile := gitPath(layout, "HEAD")
		ctx, cancel := context.WithCancel(t.Context())
		results := make(chan ChangeSet, 8)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for cs := range w.Run(ctx) {
				results <- cs
			}
		}()
		time.Sleep(10 * time.Millisecond)

		w.Pause()
		if err := os.WriteFile(headFile, []byte("ref: refs/heads/some-other-branch-name\n"), 0o644); err != nil {
			t.Fatalf("WriteFile returned error %v", err)
		}
		time.Sleep(5 * time.Second)
		select {
		case cs := <-results:
			t.Fatalf("unexpected change reported while paused: %v", cs)
		default:
		}

		w.Resume()
		w.Poke()
		time.Sleep(10 * time.Millisecond)
		select {
		case cs := <-results:
			t.Fatalf("unexpected change reported for a modification made during pause: %v", cs)
		default:
		}

		if err := os.WriteFile(headFile, []byte("ref: refs/heads/master\n"), 0o644); err != nil {
			t.Fatalf("WriteFile returned error %v", err)
		}
		w.Poke()
		select {
		case cs := <-results:
			if !cs.Has(Head) {
				t.Fatalf("changes = %v, want Head", cs)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("expected a Head change after resume")
		}
		cancel()
		<-done
	})
}

func TestRunStopsWhenConsumerBreaksEarly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		layout := newTestLayout(t, false)
		w := New(layout, Options{MinInterval: time.Second, MaxInterval: time.Second})
		headFile := gitPath(layout, "HEAD")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range w.Run(ctx) {
				break
			}
		}()
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(headFile, []byte("ref: refs/heads/other-branch-name\n"), 0o644); err != nil {
			t.Fatalf("WriteFile returned error %v", err)
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not stop after the consumer broke out of the loop")
		}
	})
}

func TestConcurrentPokePauseResumeIsRaceFree(t *testing.T) {
	layout := newTestLayout(t, false)
	w := New(layout, Options{MinInterval: time.Millisecond, MaxInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range w.Run(ctx) {
		}
	}()
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			w.Poke()
			w.Pause()
			w.Resume()
		})
	}
	wg.Wait()
	cancel()
	<-done
}
