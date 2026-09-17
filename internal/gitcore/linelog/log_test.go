package linelog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var errMissing = errors.New("object is missing")

type memory struct {
	kinds  map[hash.ObjectID]object.Type
	data   map[hash.ObjectID][]byte
	failAt map[hash.ObjectID]int
	reads  map[hash.ObjectID]int
	clock  int64
}

func newMemory() *memory {
	return &memory{
		kinds:  map[hash.ObjectID]object.Type{},
		data:   map[hash.ObjectID][]byte{},
		failAt: map[hash.ObjectID]int{},
		reads:  map[hash.ObjectID]int{},
		clock:  1700000000,
	}
}

func (m *memory) Get(id hash.ObjectID) (object.Type, []byte, error) {
	m.reads[id]++
	if at, ok := m.failAt[id]; ok && m.reads[id] >= at {
		return 0, nil, fmt.Errorf("%w: %s", errMissing, id)
	}
	kind, ok := m.kinds[id]
	if !ok {
		return 0, nil, fmt.Errorf("%w: %s", errMissing, id)
	}
	return kind, m.data[id], nil
}

func (m *memory) put(kind object.Type, data []byte) hash.ObjectID {
	id := hash.SumSHA1(kind.String(), data)
	m.kinds[id], m.data[id] = kind, data
	return id
}

func (m *memory) blob(content string) hash.ObjectID {
	return m.put(object.TypeBlob, []byte(content))
}

type node struct {
	mode object.Mode
	id   hash.ObjectID
}

func (m *memory) tree(files map[string]any) hash.ObjectID {
	dirs := map[string]map[string]any{}
	var entries []object.TreeEntry
	for path, value := range files {
		if dir, rest, nested := strings.Cut(path, "/"); nested {
			if dirs[dir] == nil {
				dirs[dir] = map[string]any{}
			}
			dirs[dir][rest] = value
			continue
		}
		switch typed := value.(type) {
		case string:
			entries = append(entries, object.TreeEntry{Mode: object.ModeBlob, Name: path, ID: m.blob(typed)})
		case node:
			entries = append(entries, object.TreeEntry{Mode: typed.mode, Name: path, ID: typed.id})
		}
	}
	for dir, nested := range dirs {
		entries = append(entries, object.TreeEntry{Mode: object.ModeTree, Name: dir, ID: m.tree(nested)})
	}
	slices.SortFunc(entries, object.CompareEntries)
	return m.put(object.TypeTree, (&object.Tree{Entries: entries}).Encode())
}

func (m *memory) commit(message string, files map[string]any, parents ...hash.ObjectID) hash.ObjectID {
	m.clock += 60
	sig := object.Signature{Name: "tester", Email: "tester@example.com", When: time.Unix(m.clock, 0).UTC()}
	c := &object.Commit{Tree: m.tree(files), Parents: parents, Author: sig, Committer: sig, Message: message + "\n"}
	return m.put(object.TypeCommit, c.Encode())
}

func lined(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

type result struct {
	subjects []string
	patch    string
}

func collect(t *testing.T, m *memory, start hash.ObjectID, specs []Spec, opts Options) (result, error) {
	t.Helper()
	var out result
	var patch bytes.Buffer
	for entry, err := range Log(t.Context(), m, start, specs, opts) {
		if err != nil {
			return out, err
		}
		out.subjects = append(out.subjects, strings.TrimSpace(entry.Commit.Message))
		if err := entry.WritePatch(&patch); err != nil {
			return out, err
		}
	}
	out.patch = patch.String()
	return out, nil
}

func TestLogFollowsTheRangeUntilItsLinesAppear(t *testing.T) {
	m := newMemory()
	root := m.commit("unrelated", map[string]any{"other": "x\n"})
	create := m.commit("create", map[string]any{"other": "x\n", "f": lined("a", "b", "c")}, root)
	edit := m.commit("edit", map[string]any{"other": "x\n", "f": lined("a", "B", "c")}, create)
	head := m.commit("outside", map[string]any{"other": "x\n", "f": lined("a", "B", "c", "d")}, edit)
	var progress []Progress

	got, err := collect(t, m, head, []Spec{{Range: "2,2", Path: "f"}}, Options{Progress: func(p Progress) { progress = append(progress, p) }})

	if err != nil || !slices.Equal(got.subjects, []string{"edit", "create"}) {
		t.Fatalf("subjects = %v, %v", got.subjects, err)
	}
	want := "\ndiff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -2,1 +2,1 @@\n-b\n+B\n" +
		"\ndiff --git a/f b/f\n--- /dev/null\n+++ b/f\n@@ -0,0 +2,1 @@\n+b\n"
	if got.patch != want {
		t.Fatalf("patch = %q", got.patch)
	}
	if len(progress) != 3 || progress[2] != (Progress{Done: 2, Total: 4}) {
		t.Fatalf("progress = %v", progress)
	}
}

func TestLogStopsWhenTheCallerStops(t *testing.T) {
	m := newMemory()
	first := m.commit("first", map[string]any{"f": lined("a")})
	head := m.commit("second", map[string]any{"f": lined("b")}, first)

	seen := 0
	for _, err := range Log(t.Context(), m, head, []Spec{{Range: "1", Path: "f"}}, Options{}) {
		if err != nil {
			t.Fatal(err)
		}
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("seen = %d", seen)
	}
}

func TestLogShowsAMergeOnlyWhenEveryParentChangedTheRange(t *testing.T) {
	m := newMemory()
	base := m.commit("base", map[string]any{"f": lined("1", "2", "3", "4", "5")})
	ours := m.commit("ours", map[string]any{"f": lined("1", "OURS", "3", "4", "5")}, base)
	theirs := m.commit("theirs", map[string]any{"f": lined("1", "2", "3", "THEIRS", "5")}, base)
	both := m.commit("both", map[string]any{"f": lined("1", "OURS", "3", "THEIRS", "5")}, ours, theirs)
	side := m.commit("side", map[string]any{"f": lined("1", "OURS", "3", "THEIRS", "5"), "g": "g\n"}, base)
	oneSide := m.commit("one side", map[string]any{"f": lined("1", "OURS", "3", "THEIRS", "5"), "g": "g\n"}, both, side)

	got, err := collect(t, m, oneSide, []Spec{{Range: "1,5", Path: "f"}}, Options{})

	if err != nil || !slices.Equal(got.subjects, []string{"both", "theirs", "ours", "base"}) {
		t.Fatalf("subjects = %v, %v", got.subjects, err)
	}
	if !strings.HasPrefix(got.patch, "\n\ndiff --git") {
		t.Fatalf("the merge printed a patch: %q", got.patch)
	}
}

func TestLogFollowsRenamesUnlessTheyAreSwitchedOff(t *testing.T) {
	m := newMemory()
	body := lined("one", "two", "three", "four", "five", "six", "seven", "eight")
	old := m.commit("old", map[string]any{"a": body})
	head := m.commit("move", map[string]any{"b": strings.Replace(body, "two", "TWO", 1)}, old)
	spec := []Spec{{Range: "1,3", Path: "b"}}

	followed, err := collect(t, m, head, spec, Options{})
	if err != nil || !slices.Equal(followed.subjects, []string{"move", "old"}) || !strings.Contains(followed.patch, "diff --git a/a b/b\n--- a/a\n") {
		t.Fatalf("followed = %+v, %v", followed, err)
	}
	strict, err := collect(t, m, head, spec, Options{Diff: diff.Options{RenameThreshold: 100}})
	if err != nil || !slices.Equal(strict.subjects, []string{"move"}) {
		t.Fatalf("a strict threshold gave %+v, %v", strict, err)
	}
	plain, err := collect(t, m, head, spec, Options{NoRenames: true})
	if err != nil || !slices.Equal(plain.subjects, []string{"move"}) || !strings.Contains(plain.patch, "--- /dev/null") {
		t.Fatalf("without renames = %+v, %v", plain, err)
	}
}

func TestLogReadsSubmodulesSymlinksAndNestedPaths(t *testing.T) {
	m := newMemory()
	first := m.commit("first", map[string]any{"dir/sub": node{mode: object.ModeSubmodule, id: hash.ObjectID{1}}, "dir/link": node{mode: object.ModeSymlink, id: m.blob("target")}})
	head := m.commit("second", map[string]any{"dir/sub": node{mode: object.ModeSubmodule, id: hash.ObjectID{2}}, "dir/link": node{mode: object.ModeSymlink, id: m.blob("target")}}, first)

	got, err := collect(t, m, head, []Spec{{Range: "1", Path: "dir/sub"}, {Range: "1", Path: "dir/link"}}, Options{})

	if err != nil || !slices.Equal(got.subjects, []string{"second", "first"}) {
		t.Fatalf("subjects = %v, %v", got.subjects, err)
	}
	if !strings.Contains(got.patch, "-Subproject commit 01") || !strings.Contains(got.patch, "+target\n\\ No newline at end of file\n") {
		t.Fatalf("patch = %q", got.patch)
	}
}

func TestLogTreatsAShallowBoundaryAsARoot(t *testing.T) {
	m := newMemory()
	first := m.commit("first", map[string]any{"f": lined("a")})
	head := m.commit("second", map[string]any{"f": lined("b")}, first)

	got, err := collect(t, m, head, []Spec{{Range: "1", Path: "f"}}, Options{Shallow: map[hash.ObjectID]struct{}{head: {}}})

	if err != nil || !slices.Equal(got.subjects, []string{"second"}) || !strings.Contains(got.patch, "--- /dev/null") {
		t.Fatalf("got = %+v, %v", got, err)
	}
}

func TestLogKeepsFollowingOtherFilesWhenOneRangeRunsOut(t *testing.T) {
	m := newMemory()
	grand := m.commit("grand", map[string]any{"a": lined("a1", "a2"), "b": lined("b1", "b2")})
	parent := m.commit("parent", map[string]any{"a": lined("A1", "a2"), "b": lined("B1", "b2")}, grand)
	head := m.commit("head", map[string]any{"a": lined("A1", "a2"), "b": lined("new", "B1", "b2")}, parent)

	got, err := collect(t, m, head, []Spec{{Range: "1,1", Path: "b"}, {Range: "1,1", Path: "a"}}, Options{})

	if err != nil || !slices.Equal(got.subjects, []string{"head", "parent", "grand"}) {
		t.Fatalf("subjects = %v, %v", got.subjects, err)
	}
	if strings.Count(got.patch, "diff --git a/b") != 1 {
		t.Fatalf("patch = %q", got.patch)
	}
}

func TestLogSkipsPathsMissingFromOlderCommits(t *testing.T) {
	m := newMemory()
	parent := m.commit("parent", map[string]any{"a": lined("a1")})
	head := m.commit("head", map[string]any{"a": lined("A1"), "b": lined("b1")}, parent)

	got, err := collect(t, m, head, []Spec{{Range: "1,1", Path: "a"}, {Range: "1,1", Path: "b"}}, Options{})

	if err != nil || !slices.Equal(got.subjects, []string{"head", "parent"}) {
		t.Fatalf("subjects = %v, %v", got.subjects, err)
	}
}

func TestLogPrintsOnlyTheRangesAChangeTouched(t *testing.T) {
	m := newMemory()
	lines := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	first := m.commit("first", map[string]any{"f": lined(lines...)})
	edited := slices.Clone(lines)
	edited[1], edited[2] = "TWO", "THREE"
	head := m.commit("second", map[string]any{"f": lined(edited...)}, first)

	got, err := collect(t, m, head, []Spec{{Range: "1,4", Path: "f"}, {Range: "8,9", Path: "f"}}, Options{})

	if err != nil || len(got.subjects) != 2 {
		t.Fatalf("subjects = %v, %v", got.subjects, err)
	}
	want := "\ndiff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,4 +1,4 @@\n 1\n-2\n-3\n+TWO\n+THREE\n 4\n"
	if !strings.HasPrefix(got.patch, want+"\n") {
		t.Fatalf("patch = %q", got.patch)
	}
}

func TestLogReportsItsFailures(t *testing.T) {
	m := newMemory()
	good := m.commit("good", map[string]any{"f": lined("a", "b"), "dir/x": "x\n", "empty": ""})
	emptyFile := m.put(object.TypeBlob, nil)
	withEmpty := m.commit("empty", map[string]any{"f": lined("a"), "e": node{mode: object.ModeBlob, id: emptyFile}})
	missingBlob := m.commit("missing blob", map[string]any{"f": node{mode: object.ModeBlob, id: hash.ObjectID{9}}})
	treeAsBlob := m.commit("tree as blob", map[string]any{"f": node{mode: object.ModeBlob, id: m.tree(map[string]any{"x": "x\n"})}})
	missingTree := m.put(object.TypeCommit, (&object.Commit{Tree: hash.ObjectID{7}, Message: "m\n"}).Encode())
	blobTree := m.put(object.TypeCommit, (&object.Commit{Tree: m.blob("not a tree"), Message: "m\n"}).Encode())
	badTree := m.put(object.TypeCommit, (&object.Commit{Tree: m.put(object.TypeTree, []byte("garbage")), Message: "m\n"}).Encode())
	orphan := m.commit("orphan", map[string]any{"f": lined("a")}, hash.ObjectID{8})

	cases := []struct {
		name  string
		start hash.ObjectID
		specs []Spec
		want  error
	}{
		{name: "no ranges", start: good, want: ErrNoRanges},
		{name: "missing start", start: hash.ObjectID{3}, specs: []Spec{{Range: "1", Path: "f"}}, want: errMissing},
		{name: "start is a blob", start: m.blob("x"), specs: []Spec{{Range: "1", Path: "f"}}, want: ErrNotCommit},
		{name: "missing path", start: good, specs: []Spec{{Range: "1", Path: "nothing"}}, want: ErrPathNotFound},
		{name: "directory", start: good, specs: []Spec{{Range: "1", Path: "dir"}}, want: ErrPathNotFound},
		{name: "below a file", start: good, specs: []Spec{{Range: "1", Path: "f/x"}}, want: ErrPathNotFound},
		{name: "missing blob", start: missingBlob, specs: []Spec{{Range: "1", Path: "f"}}, want: errMissing},
		{name: "tree as blob", start: treeAsBlob, specs: []Spec{{Range: "1", Path: "f"}}, want: ErrPathNotFound},
		{name: "missing tree", start: missingTree, specs: []Spec{{Range: "1", Path: "f"}}, want: errMissing},
		{name: "blob as tree", start: blobTree, specs: []Spec{{Range: "1", Path: "f"}}, want: ErrPathNotFound},
		{name: "bad range", start: good, specs: []Spec{{Range: "0", Path: "f"}}, want: ErrInvalidLine},
		{name: "past the end", start: good, specs: []Spec{{Range: "3,4", Path: "f"}}, want: ErrTooFewLines},
		{name: "empty file", start: withEmpty, specs: []Spec{{Range: "1,1", Path: "e"}}, want: ErrTooFewLines},
		{name: "missing parent", start: orphan, specs: []Spec{{Range: "1", Path: "f"}}, want: errMissing},
	}
	for _, c := range cases {
		if _, err := collect(t, m, c.start, c.specs, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := collect(t, m, badTree, []Spec{{Range: "1", Path: "f"}}, Options{}); err == nil {
		t.Error("a corrupt tree was read")
	}
	if got, err := collect(t, m, withEmpty, []Spec{{Range: ",", Path: "e"}}, Options{}); err != nil || len(got.subjects) != 0 {
		t.Errorf("an empty range gave %+v, %v", got, err)
	}
	corrupt := m.put(object.TypeCommit, []byte("not a commit"))
	if _, err := collect(t, m, corrupt, []Spec{{Range: "1", Path: "f"}}, Options{}); err == nil {
		t.Error("a corrupt commit was read")
	}
}

func TestLogReportsFailuresInOlderCommits(t *testing.T) {
	cases := []struct {
		name  string
		build func(m *memory) hash.ObjectID
	}{
		{name: "parent blob unreadable", build: func(m *memory) hash.ObjectID {
			first := m.commit("first", map[string]any{"f": node{mode: object.ModeBlob, id: hash.ObjectID{4}}})
			return m.commit("second", map[string]any{"f": lined("b")}, first)
		}},
		{name: "commit blob unreadable on the second read", build: func(m *memory) hash.ObjectID {
			m.failAt[m.blob(lined("b"))] = 2
			first := m.commit("first", map[string]any{"f": lined("a")})
			return m.commit("second", map[string]any{"f": lined("b")}, first)
		}},
		{name: "parent tree unreadable", build: func(m *memory) hash.ObjectID {
			first := m.put(object.TypeCommit, (&object.Commit{Tree: hash.ObjectID{5}, Message: "first\n"}).Encode())
			return m.commit("second", map[string]any{"f": lined("b")}, first)
		}},
		{name: "merge parent tree unreadable", build: func(m *memory) hash.ObjectID {
			first := m.put(object.TypeCommit, (&object.Commit{Tree: hash.ObjectID{5}, Message: "first\n"}).Encode())
			other := m.commit("other", map[string]any{"f": lined("c")})
			return m.commit("merge", map[string]any{"f": lined("b")}, first, other)
		}},
		{name: "rename search unreadable", build: func(m *memory) hash.ObjectID {
			first := m.commit("first", map[string]any{"gone/x": node{mode: object.ModeBlob, id: hash.ObjectID{6}}})
			m.failAt[m.tree(map[string]any{"x": node{mode: object.ModeBlob, id: hash.ObjectID{6}}})] = 2
			return m.commit("second", map[string]any{"f": lined("b")}, first)
		}},
		{name: "rename source unreadable", build: func(m *memory) hash.ObjectID {
			body := lined("same", "content", "here")
			m.failAt[m.blob(body)] = 3
			first := m.commit("first", map[string]any{"old": body})
			return m.commit("second", map[string]any{"new": body}, first)
		}},
	}
	for _, c := range cases {
		m := newMemory()
		head := c.build(m)
		spec := []Spec{{Range: "1", Path: "f"}}
		if c.name == "rename source unreadable" {
			spec[0].Path = "new"
		}
		if _, err := collect(t, m, head, spec, Options{}); !errors.Is(err, errMissing) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}

func TestLogStopsOnCancellation(t *testing.T) {
	m := newMemory()
	first := m.commit("first", map[string]any{"f": lined("a")})
	head := m.commit("second", map[string]any{"f": lined("b")}, first)
	ctx, cancel := context.WithCancel(t.Context())

	var err error
	for _, err = range Log(ctx, m, head, []Spec{{Range: "1", Path: "f"}}, Options{Progress: func(Progress) { cancel() }}) {
		if err != nil {
			break
		}
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}

	for _, err = range Log(ctx, m, head, []Spec{{Range: "1", Path: "f"}}, Options{}) {
		break
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled walk returned %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errMissing }

func TestWritePatchReportsWriteFailures(t *testing.T) {
	if err := (&Entry{}).WritePatch(failingWriter{}); !errors.Is(err, errMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestRangeListsKeepPathOrderAndMergeByPath(t *testing.T) {
	var list rangeList
	list = list.insert("m", 0, 1)
	list = list.insert("z", 2, 3)
	list = list.insert("a", 4, 5)
	list = list.insert("m", 6, 7)
	var paths []string
	for _, entry := range list {
		paths = append(paths, entry.path)
	}
	if !slices.Equal(paths, []string{"a", "m", "z"}) || len(list.find("m").ranges) != 2 || list.find("q") != nil {
		t.Fatalf("paths = %v", paths)
	}

	merged := mergeLists(
		rangeList{{path: "a", ranges: sp(0, 2)}, {path: "c", ranges: sp(1, 2)}},
		rangeList{{path: "a", ranges: sp(1, 4)}, {path: "b", ranges: sp(5, 6)}, {path: "d", ranges: sp(0, 1)}},
	)
	var summary []string
	for _, entry := range merged {
		summary = append(summary, fmt.Sprint(entry.path, entry.ranges))
	}
	if !slices.Equal(summary, []string{"a[{0 4}]", "b[{5 6}]", "c[{1 2}]", "d[{0 1}]"}) {
		t.Fatalf("merged = %v", summary)
	}
	if (rangeList{{path: "x"}}).live() {
		t.Fatal("a list of empty ranges is live")
	}
}

func TestQueueDiffsLooksAtEachPathOnce(t *testing.T) {
	m := newMemory()
	parent := m.tree(map[string]any{"f": lined("a")})
	child := m.tree(map[string]any{"f": lined("b")})
	l := &logger{ctx: t.Context(), src: m, opts: Options{Diff: diff.Defaults()}, entries: map[entryKey]entryResult{}}

	pairs, err := l.queueDiffs(rangeList{{path: "f", ranges: sp(0, 1)}, {path: "f", ranges: sp(0, 1)}}, child, parent)

	if err != nil || len(pairs) != 1 {
		t.Fatalf("pairs = %d, %v", len(pairs), err)
	}
}
