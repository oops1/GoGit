package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

const lfsTestAttributes = "*.bin filter=lfs -text\n*.ext filter=ext\n"

func lfsOIDOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func lfsObjectFileOf(storage, oid string) string {
	return filepath.Join(storage, "objects", oid[:2], oid[2:4], oid)
}

func (r *testRepo) lfsStorage() string {
	return filepath.Join(r.repo.CommonDir(), "lfs")
}

func (r *testRepo) storeLFSObjectAt(storage, oid, content string) {
	r.t.Helper()
	path := lfsObjectFileOf(storage, oid)
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		r.t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) storeLFSObject(content string) {
	r.t.Helper()
	r.storeLFSObjectAt(r.lfsStorage(), lfsOIDOf(content), content)
}

func lfsTopicRepo(t *testing.T, config string, files map[string]string) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	r.appendConfig(config)
	r.repo = r.reopen()
	base := r.commitFiles("base", map[string]string{".gitattributes": lfsTestAttributes})
	r.createBranch("topic", base)
	r.switchTo("topic")
	r.commitFiles("topic", files)
	r.switchTo("main")
	return r
}

func switchReporting(t *testing.T, r *testRepo, target string) CheckoutReport {
	t.Helper()
	var report CheckoutReport
	if err := Switch(t.Context(), r.repo, target, SwitchOptions{Report: &report}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	return report
}

func requireFiles(t *testing.T, r *testRepo, want map[string]string) {
	t.Helper()
	for rel, content := range want {
		if got := r.readFile(rel); got != content {
			t.Errorf("%s = %q, want %q", rel, got, content)
		}
	}
}

func TestSwitchSmudgesLFSObjectsFromTheLocalStore(t *testing.T) {
	r := lfsTopicRepo(t, lfsFilterConfig, map[string]string{
		"a.bin":     lfsPointerOf("changed"),
		"d.bin":     lfsPointerOf("fourth"),
		"e.bin":     lfsPointerOf("fifth"),
		"sub/c.bin": lfsPointerOf("third"),
	})
	r.storeLFSObject("changed")
	r.storeLFSObject("third")

	report := switchReporting(t, r, "topic")
	requireFiles(t, r, map[string]string{
		"a.bin":     "changed",
		"sub/c.bin": "third",
		"d.bin":     lfsPointerOf("fourth"),
		"e.bin":     lfsPointerOf("fifth"),
	})
	want := []LFSPointerFile{
		{Path: "d.bin", OID: lfsOIDOf("fourth"), Size: 6, Reason: LFSObjectMissing},
		{Path: "e.bin", OID: lfsOIDOf("fifth"), Size: 5, Reason: LFSObjectMissing},
	}
	if !slices.Equal(report.LFSPointers, want) || len(report.Unfiltered) != 0 {
		t.Fatalf("report = %+v, want pointers %+v", report, want)
	}

	before := r.index()
	for _, rel := range []string{"a.bin", "sub", "d.bin", "e.bin"} {
		mustStage(t, r, rel)
	}
	after := r.index()
	for _, rel := range []string{"a.bin", "sub/c.bin", "d.bin", "e.bin"} {
		old, _ := entryOf(t, before, rel)
		now, ok := entryOf(t, after, rel)
		if !ok || now.ID != old.ID {
			t.Errorf("staging %s changed the blob from %s to %s", rel, old.ID, now.ID)
		}
	}
	if back := switchReporting(t, r, "main"); len(back.LFSPointers) != 0 || r.exists("a.bin") {
		t.Fatalf("switching back reported %+v and left a.bin: %v", back, r.exists("a.bin"))
	}
}

func TestSwitchWritesLFSTrackedContentThatIsNotAPointerAsIs(t *testing.T) {
	r := lfsTopicRepo(t, lfsFilterConfig, map[string]string{"a.bin": lfsPointerOf("first")})
	tree := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: ".gitattributes", ID: storedBlob(t, r, lfsTestAttributes)},
		object.TreeEntry{Mode: object.ModeBlob, Name: "raw.bin", ID: storedBlob(t, r, "plain")},
		object.TreeEntry{Mode: object.ModeBlob, Name: "zero.bin", ID: storedBlob(t, r, lfsPointerOf(""))},
	)
	r.createBranch("raw", putCommit(t, r, tree, r.branchTarget("main")))
	report := switchReporting(t, r, "raw")
	requireFiles(t, r, map[string]string{"raw.bin": "plain", "zero.bin": ""})
	if len(report.LFSPointers) != 0 || len(report.Unfiltered) != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestSwitchWritesPointersForLFSObjectsItCannotUse(t *testing.T) {
	oid := lfsOIDOf("first")
	upper := strings.Replace(lfsPointerOf("first"), oid, strings.ToUpper(oid), 1)
	tests := []struct {
		name    string
		pointer string
		oid     string
		store   func(t *testing.T, r *testRepo)
	}{
		{"missing", lfsPointerOf("first"), oid, func(*testing.T, *testRepo) {}},
		{"wrongSize", lfsPointerOf("first"), oid, func(_ *testing.T, r *testRepo) { r.storeLFSObjectAt(r.lfsStorage(), oid, "firstly") }},
		{"wrongContent", lfsPointerOf("first"), oid, func(_ *testing.T, r *testRepo) { r.storeLFSObjectAt(r.lfsStorage(), oid, "FIRST") }},
		{"directory", lfsPointerOf("first"), oid, func(t *testing.T, r *testRepo) {
			if err := os.MkdirAll(lfsObjectFileOf(r.lfsStorage(), oid), 0o777); err != nil {
				t.Fatalf("MkdirAll returned error %v", err)
			}
		}},
		{"upperCaseID", upper, strings.ToUpper(oid), func(_ *testing.T, r *testRepo) { r.storeLFSObjectAt(r.lfsStorage(), strings.ToUpper(oid), "first") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := lfsTopicRepo(t, lfsFilterConfig, map[string]string{"a.bin": tc.pointer})
			tc.store(t, r)
			report := switchReporting(t, r, "topic")
			requireFiles(t, r, map[string]string{"a.bin": tc.pointer})
			want := []LFSPointerFile{{Path: "a.bin", OID: tc.oid, Size: 5, Reason: LFSObjectMissing}}
			if !slices.Equal(report.LFSPointers, want) {
				t.Fatalf("report = %+v, want %+v", report.LFSPointers, want)
			}
		})
	}
}

func TestSwitchLeavesPointersWhereGitLFSSkipsTheSmudge(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{"skipProcess", "[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n\tprocess = git-lfs filter-process --skip\n"},
		{"skipSmudge", "[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n\tsmudge = git-lfs smudge --skip -- %f\n"},
		{"fetchExclude", lfsFilterConfig + "[lfs]\n\tfetchexclude = sub\n"},
		{"fetchInclude", lfsFilterConfig + "[lfs]\n\tfetchinclude = other\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := lfsTopicRepo(t, tc.config, map[string]string{"sub/c.bin": lfsPointerOf("third")})
			r.storeLFSObject("third")
			report := switchReporting(t, r, "topic")
			requireFiles(t, r, map[string]string{"sub/c.bin": lfsPointerOf("third")})
			want := []LFSPointerFile{{Path: "sub/c.bin", OID: lfsOIDOf("third"), Size: 5, Reason: LFSSmudgeSkipped}}
			if !slices.Equal(report.LFSPointers, want) {
				t.Fatalf("report = %+v, want %+v", report.LFSPointers, want)
			}
		})
	}
}

func TestSwitchReportsFiltersItCannotRun(t *testing.T) {
	extended := "version https://git-lfs.github.com/spec/v1\next-0-foo sha256:" + lfsOIDOf("x") + "\noid sha256:" + lfsOIDOf("first") + "\nsize 5\n"
	r := lfsTopicRepo(t, lfsFilterConfig+"[filter \"ext\"]\n\tsmudge = cat\n", map[string]string{
		"b.ext":   "raw b\n",
		"a.ext":   "raw a\n",
		"ext.bin": extended,
	})
	r.storeLFSObject("first")

	report := switchReporting(t, r, "topic")
	requireFiles(t, r, map[string]string{"a.ext": "raw a\n", "b.ext": "raw b\n", "ext.bin": extended})
	if !slices.Equal(report.Unfiltered, []string{"a.ext", "b.ext", "ext.bin"}) || len(report.LFSPointers) != 0 {
		t.Fatalf("report = %+v", report)
	}

	r.writeFile("a.ext", "edited\n")
	if err := Discard(t.Context(), r.repo, []string{"a.ext"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	requireFiles(t, r, map[string]string{"a.ext": "raw a\n"})
}

func TestLFSStorageFollowsTheConfiguredDirectory(t *testing.T) {
	absolute := t.TempDir()
	tests := []struct {
		name    string
		storage string
		dir     func(r *testRepo) string
	}{
		{"relative", "media", func(r *testRepo) string { return filepath.Join(r.repo.CommonDir(), "media") }},
		{"absolute", filepath.ToSlash(absolute), func(*testRepo) string { return absolute }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := lfsTopicRepo(t, lfsFilterConfig+"[lfs]\n\tstorage = "+tc.storage+"\n", map[string]string{"a.bin": lfsPointerOf("first")})
			r.storeLFSObjectAt(tc.dir(r), lfsOIDOf("first"), "first")
			if report := switchReporting(t, r, "topic"); len(report.LFSPointers) != 0 {
				t.Fatalf("report = %+v", report)
			}
			requireFiles(t, r, map[string]string{"a.bin": "first"})
		})
	}
}

type fakeLFSObject struct {
	info    fs.FileInfo
	statErr error
	readErr error
}

func (f fakeLFSObject) Read([]byte) (int, error)   { return 0, f.readErr }
func (fakeLFSObject) Close() error                 { return nil }
func (f fakeLFSObject) Stat() (fs.FileInfo, error) { return f.info, f.statErr }

func swapLFSOpenObject(t *testing.T, object fakeLFSObject) {
	t.Helper()
	original := lfsOpenObject
	lfsOpenObject = func(string) (lfsObjectFile, error) { return object, nil }
	t.Cleanup(func() { lfsOpenObject = original })
}

func TestSwitchStopsWhenAnLFSObjectCannotBeCopied(t *testing.T) {
	r := lfsTopicRepo(t, lfsFilterConfig, map[string]string{"a.bin": lfsPointerOf("first")})
	r.storeLFSObject("first")
	info, err := os.Stat(lfsObjectFileOf(r.lfsStorage(), lfsOIDOf("first")))
	if err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
	swapLFSOpenObject(t, fakeLFSObject{info: info, readErr: errInjected})
	if err := Switch(t.Context(), r.repo, "topic", SwitchOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("Switch returned %v, want %v", err, errInjected)
	}
}

func TestSwitchStopsWhenTheSmudgedFileCannotBeWritten(t *testing.T) {
	r := lfsTopicRepo(t, lfsFilterConfig, map[string]string{"a.bin": lfsPointerOf("first")})
	r.storeLFSObject("first")
	swapRootOpenFileFailForPath(t, "a.bin")
	if err := Switch(t.Context(), r.repo, "topic", SwitchOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("Switch returned %v, want %v", err, errInjected)
	}
}

func TestSwitchStopsWhenTheDirectoryOfASmudgedFileCannotBeCreated(t *testing.T) {
	r := lfsTopicRepo(t, lfsFilterConfig, map[string]string{"sub/c.bin": lfsPointerOf("third")})
	r.storeLFSObject("third")
	original := fsRootMkdir
	fsRootMkdir = func(root *os.Root, name string, perm os.FileMode) error {
		if filepath.ToSlash(name) == "sub" {
			return errInjected
		}
		return original(root, name, perm)
	}
	t.Cleanup(func() { fsRootMkdir = original })
	if err := Switch(t.Context(), r.repo, "topic", SwitchOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("Switch returned %v, want %v", err, errInjected)
	}
}

func TestSwitchWritesAPointerWhenTheLFSObjectCannotBeInspected(t *testing.T) {
	r := lfsTopicRepo(t, lfsFilterConfig, map[string]string{"a.bin": lfsPointerOf("first")})
	r.storeLFSObject("first")
	swapLFSOpenObject(t, fakeLFSObject{statErr: errInjected})
	report := switchReporting(t, r, "topic")
	requireFiles(t, r, map[string]string{"a.bin": lfsPointerOf("first")})
	if len(report.LFSPointers) != 1 || report.LFSPointers[0].Reason != LFSObjectMissing {
		t.Fatalf("report = %+v", report)
	}
}

func TestDiscardRestoresLFSContentFromTheLocalStore(t *testing.T) {
	r := lfsTopicRepo(t, lfsFilterConfig, map[string]string{"a.bin": lfsPointerOf("first")})
	r.storeLFSObject("first")
	switchReporting(t, r, "topic")
	r.writeFile("a.bin", "edited")
	if err := Discard(t.Context(), r.repo, []string{"a.bin"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	requireFiles(t, r, map[string]string{"a.bin": "first"})
}
