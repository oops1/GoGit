package ops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func swapRootMkdirFailForPath(t testing.TB, failPath string) {
	t.Helper()
	original := fsRootMkdir
	fsRootMkdir = func(root *os.Root, name string, perm fs.FileMode) error {
		if filepath.ToSlash(name) == failPath {
			return errInjected
		}
		return original(root, name, perm)
	}
	t.Cleanup(func() { fsRootMkdir = original })
}

func swapRootRemoveFailForPath(t testing.TB, failPath string) {
	t.Helper()
	original := fsRootRemove
	fsRootRemove = func(root *os.Root, name string) error {
		if filepath.ToSlash(name) == failPath {
			return errInjected
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootRemove = original })
}

func treeAt(t testing.TB, r *testRepo, rel string, mode object.Mode, id hash.ObjectID) hash.ObjectID {
	t.Helper()
	first, rest, nested := strings.Cut(rel, "/")
	if !nested {
		return putTree(t, r, object.TreeEntry{Mode: mode, Name: first, ID: id})
	}
	return putTree(t, r, object.TreeEntry{Mode: object.ModeTree, Name: first, ID: treeAt(t, r, rest, mode, id)})
}

func storedBlob(t testing.TB, r *testRepo, text string) hash.ObjectID {
	t.Helper()
	id, err := r.db().Put(object.TypeBlob, []byte(text))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	return id
}

func (r *testRepo) unsafeBranch(rel string) hash.ObjectID {
	r.t.Helper()
	blob, err := r.db().Put(object.TypeBlob, []byte("[core]\n\thooksPath = /tmp/evil\n"))
	if err != nil {
		r.t.Fatalf("Put returned error %v", err)
	}
	commit := putCommit(r.t, r, treeAt(r.t, r, rel, object.ModeBlob, blob), r.branchTarget("main"))
	r.createBranch("unsafe", commit)
	return commit
}

func (r *testRepo) initialCommit() hash.ObjectID {
	r.t.Helper()
	r.writeFile("a.txt", "hello\n")
	mustStage(r.t, r, "a.txt")
	return r.commitAll("initial")
}

func (r *testRepo) gitConfig() string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.repo.CommonDir(), "config"))
	if err != nil {
		r.t.Fatalf("ReadFile returned error %v", err)
	}
	return string(data)
}

func TestPathRulesOfFollowsTheProtectSettings(t *testing.T) {
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tprotectNTFS = false\n\tprotectHFS = true\n")
	rules, err := pathRulesOf(r.reopen())
	if err != nil {
		t.Fatalf("pathRulesOf returned error %v", err)
	}
	if rules.ProtectNTFS || !rules.ProtectHFS {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestPathRulesOfRejectsInvalidProtectSettings(t *testing.T) {
	for _, key := range []string{"protectNTFS", "protectHFS"} {
		r := newTestRepo(t)
		r.appendConfig("[core]\n\t" + key + " = maybe\n")
		if _, err := pathRulesOf(r.reopen()); err == nil {
			t.Fatalf("%s = maybe was accepted", key)
		}
	}
}

func TestSwitchRefusesTreesThatWriteIntoTheGitDirectory(t *testing.T) {
	for _, rel := range []string{".git/config", ".GIT/config", "git~1/config", ".git. /hooks/post-checkout", "sub/.Git/config", "a/b/../../.git/config"} {
		t.Run(rel, func(t *testing.T) {
			r := newTestRepo(t)
			r.initialCommit()
			before := r.gitConfig()
			r.unsafeBranch(rel)
			err := Switch(t.Context(), r.repo, "unsafe", SwitchOptions{})
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("Switch = %v, want ErrUnsafePath", err)
			}
			if r.gitConfig() != before {
				t.Fatal("the git config was rewritten")
			}
			if target, _ := r.headSymbolicTarget(); target.Short() != "main" {
				t.Fatalf("HEAD moved to %s", target)
			}
		})
	}
}

func TestSwitchRefusesTreeEntryNamesWithASlash(t *testing.T) {
	r := newTestRepo(t)
	r.initialCommit()
	blob := storedBlob(t, r, "x\n")
	r.createBranch("unsafe", putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a/b", ID: blob})))
	if err := Switch(t.Context(), r.repo, "unsafe", SwitchOptions{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Switch = %v, want ErrUnsafePath", err)
	}
}

func TestSwitchRefusesAPathThatIsBothAFileAndADirectory(t *testing.T) {
	r := newTestRepo(t)
	r.initialCommit()
	blob := storedBlob(t, r, "x\n")
	inner := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "hooks", ID: blob})
	tree := putTree(t, r,
		object.TreeEntry{Mode: object.ModeSymlink, Name: "a", ID: storedBlob(t, r, ".git")},
		object.TreeEntry{Mode: object.ModeTree, Name: "a", ID: inner},
	)
	r.createBranch("unsafe", putCommit(t, r, tree))
	if err := Switch(t.Context(), r.repo, "unsafe", SwitchOptions{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Switch = %v, want ErrUnsafePath", err)
	}
	if r.exists(".git/hooks/hooks") {
		t.Fatal("a hook was written")
	}
}

func TestCheckoutTreeRefusesUnsafeTrees(t *testing.T) {
	r := newTestRepo(t)
	r.initialCommit()
	commit := r.unsafeBranch(".git/config")
	if err := CheckoutTree(t.Context(), r.repo, commit, CheckoutOptions{Force: true}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("CheckoutTree = %v, want ErrUnsafePath", err)
	}
}

func TestHistoryOperationsRefuseUnsafeTrees(t *testing.T) {
	operations := map[string]func(r *testRepo, commit hash.ObjectID) error{
		"hard reset": func(r *testRepo, commit hash.ObjectID) error {
			_, err := r.reset(commit.String(), resetOptions(ResetHard))
			return err
		},
		"mixed reset": func(r *testRepo, commit hash.ObjectID) error {
			_, err := r.reset(commit.String(), resetOptions(ResetMixed))
			return err
		},
		"reset of paths": func(r *testRepo, commit hash.ObjectID) error {
			_, err := r.reset(commit.String(), resetOptions(ResetMixed, "a.txt"))
			return err
		},
		"fast-forward merge": func(r *testRepo, _ hash.ObjectID) error {
			_, err := r.merge("unsafe", MergeOptions{})
			return err
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			r := newTestRepo(t)
			r.initialCommit()
			before := r.gitConfig()
			commit := r.unsafeBranch(".GIT/config")
			if err := operation(r, commit); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("err = %v, want ErrUnsafePath", err)
			}
			if r.gitConfig() != before {
				t.Fatal("the git config was rewritten")
			}
		})
	}
}

func TestOperationsFailWhenTheProtectSettingIsInvalid(t *testing.T) {
	operations := map[string]func(r *testRepo, commit hash.ObjectID) error{
		"switch": func(r *testRepo, _ hash.ObjectID) error {
			return Switch(t.Context(), r.repo, "main", SwitchOptions{})
		},
		"checkout": func(r *testRepo, commit hash.ObjectID) error {
			return CheckoutTree(t.Context(), r.repo, commit, CheckoutOptions{})
		},
		"stage": func(r *testRepo, _ hash.ObjectID) error {
			return Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{})
		},
		"hard reset": func(r *testRepo, commit hash.ObjectID) error {
			_, err := r.reset(commit.String(), resetOptions(ResetHard))
			return err
		},
		"mixed reset": func(r *testRepo, commit hash.ObjectID) error {
			_, err := r.reset(commit.String(), resetOptions(ResetMixed))
			return err
		},
		"reset of paths": func(r *testRepo, commit hash.ObjectID) error {
			_, err := r.reset(commit.String(), resetOptions(ResetMixed, "a.txt"))
			return err
		},
		"fast-forward merge": func(r *testRepo, _ hash.ObjectID) error {
			_, err := r.merge("feature", MergeOptions{})
			return err
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			r := newTestRepo(t)
			commit := r.initialCommit()
			r.createBranch("feature", putCommit(t, r, treeAt(t, r, "b.txt", object.ModeBlob, storedBlob(t, r, "b\n")), commit))
			r.appendConfig("[core]\n\tprotectNTFS = maybe\n")
			r.repo = r.reopen()
			err := operation(r, commit)
			if err == nil || errors.Is(err, ErrUnsafePath) {
				t.Fatalf("err = %v, want a configuration error", err)
			}
		})
	}
}

func TestStageRefusesUnsafePaths(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("sub/git~1", "x\n")
	if err := Stage(t.Context(), r.repo, []string{"sub/git~1"}, StageOptions{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Stage = %v, want ErrUnsafePath", err)
	}
	if _, ok := entryOf(t, r.index(), "sub/git~1"); ok {
		t.Fatal("the unsafe path was staged")
	}
}

func (r *testRepo) indexBlob(rel, content string) {
	r.t.Helper()
	idx := r.index()
	idx.Add(index.Entry{Path: rel, Mode: object.ModeBlob, ID: storedBlob(r.t.(*testing.T), r, content), Stage: index.StageMerged})
	r.saveIndex(idx)
}

func TestDiscardRefusesToWriteIntoTheGitDirectory(t *testing.T) {
	r := newTestRepo(t)
	r.indexBlob(".git/hooks/post-checkout", "evil\n")
	err := Discard(t.Context(), r.repo, []string{".git/hooks/post-checkout"}, DiscardOptions{})
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Discard = %v, want ErrUnsafePath", err)
	}
	if r.exists(".git/hooks/post-checkout") {
		t.Fatal("the hook was written")
	}
}

func TestCheckoutReplacesAFileInTheLeadingPath(t *testing.T) {
	r := newTestRepo(t)
	r.indexBlob("d/x", "inside\n")
	r.writeFile("d", "file\n")
	if err := Discard(t.Context(), r.repo, []string{"d/x"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if got := r.readFile("d/x"); got != "inside\n" {
		t.Fatalf("d/x = %q", got)
	}
}

func TestCheckoutRemovesASymlinkInTheLeadingPath(t *testing.T) {
	r := newTestRepo(t)
	r.indexBlob("a/hooks/post-checkout", "inside\n")
	if !r.symlink(".git", "a") {
		t.Skip("symbolic links are not supported")
	}
	if err := Discard(t.Context(), r.repo, []string{"a/hooks/post-checkout"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if r.exists(".git/hooks/post-checkout") {
		t.Fatal("the checkout followed the symlink into .git")
	}
	info, err := os.Lstat(r.path("a"))
	if err != nil || info.Mode().Type() != fs.ModeDir {
		t.Fatalf("a = %v, %v, want a directory", info, err)
	}
}

func TestCheckoutFailsWhenTheLeadingPathCannotBePrepared(t *testing.T) {
	faults := map[string]func(t *testing.T){
		"lstat":  func(t *testing.T) { swapRootLstatFailForPath(t, "d") },
		"remove": func(t *testing.T) { swapRootRemoveFailForPath(t, "d") },
		"mkdir":  func(t *testing.T) { swapRootMkdirFailForPath(t, "d") },
	}
	for name, fault := range faults {
		t.Run(name, func(t *testing.T) {
			r := newTestRepo(t)
			r.indexBlob("d/x", "inside\n")
			if name == "remove" {
				r.writeFile("d", "file\n")
			}
			fault(t)
			if err := Discard(t.Context(), r.repo, []string{"d/x"}, DiscardOptions{}); !errors.Is(err, errInjected) {
				t.Fatalf("Discard = %v, want errInjected", err)
			}
		})
	}
}
