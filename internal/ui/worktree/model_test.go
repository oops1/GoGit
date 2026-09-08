package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func emptyDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "wt")
}

func TestAPathIsRequired(t *testing.T) {
	if hint := Validate(Request{}, Known{}); hint.Key != hintPathRequired || hint.OK {
		t.Fatalf("hint = %+v, want the path request", hint)
	}
}

func TestAFileInPlaceOfADirectoryIsRefused(t *testing.T) {
	file := filepath.Join(t.TempDir(), "busy")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if hint := Validate(Request{Path: file, Branch: "feature"}, Known{}); hint.Key != hintPathNotDirectory {
		t.Fatalf("hint = %+v, want the not-a-directory refusal", hint)
	}
}

func TestADirectoryWithFilesInItIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if hint := Validate(Request{Path: dir, Branch: "feature"}, Known{}); hint.Key != hintPathBusy {
		t.Fatalf("hint = %+v, want the busy-directory refusal", hint)
	}
}

func TestAnEmptyDirectoryThatExistsIsAccepted(t *testing.T) {
	if hint := Validate(Request{Path: t.TempDir(), Branch: "feature"}, Known{}); !hint.OK {
		t.Fatalf("hint = %+v, want an empty directory to be accepted", hint)
	}
}

func TestANewBranchNeedsAName(t *testing.T) {
	if hint := Validate(Request{Path: emptyDir(t)}, Known{}); hint.Key != hintBranchRequired {
		t.Fatalf("hint = %+v, want the branch request", hint)
	}
}

func TestANewBranchNameIsCheckedAgainstTheRefFormat(t *testing.T) {
	if hint := Validate(Request{Path: emptyDir(t), Branch: "bad..name"}, Known{}); hint.Key != hintBranchInvalid {
		t.Fatalf("hint = %+v, want the invalid-name refusal", hint)
	}
}

func TestANewBranchThatAlreadyExistsIsRefused(t *testing.T) {
	known := Known{Branches: []string{"feature"}}

	if hint := Validate(Request{Path: emptyDir(t), Branch: "feature"}, known); hint.Key != hintBranchExists {
		t.Fatalf("hint = %+v, want the existing-branch refusal", hint)
	}
}

func TestANewBranchStartsFromTheCurrentOne(t *testing.T) {
	hint := Validate(Request{Path: emptyDir(t), Branch: "feature"}, Known{Head: "main"})

	if !hint.OK || hint.Key != hintWillCreate || hint.Args[1] != "main" {
		t.Fatalf("hint = %+v, want the branch to start from main", hint)
	}
}

func TestWithoutACurrentBranchTheStartPointIsHead(t *testing.T) {
	hint := Validate(Request{Path: emptyDir(t), Branch: "feature"}, Known{})

	if hint.Args[1] != "HEAD" {
		t.Fatalf("start point = %v, want HEAD", hint.Args[1])
	}
}

func TestAnExistingBranchMustBeChosen(t *testing.T) {
	req := Request{Path: emptyDir(t), Mode: ModeExistingBranch}

	if hint := Validate(req, Known{}); hint.Key != hintBranchRequired {
		t.Fatalf("hint = %+v, want the branch request", hint)
	}
}

func TestABranchTakenByAnotherWorktreeIsRefused(t *testing.T) {
	req := Request{Path: emptyDir(t), Mode: ModeExistingBranch, Branch: "feature"}
	known := Known{Branches: []string{"feature"}, CheckedOut: []string{"feature"}}

	if hint := Validate(req, known); hint.Key != hintBranchCheckedOut {
		t.Fatalf("hint = %+v, want the checked-out refusal", hint)
	}
}

func TestAFreeExistingBranchIsAccepted(t *testing.T) {
	req := Request{Path: emptyDir(t), Mode: ModeExistingBranch, Branch: "feature"}

	if hint := Validate(req, Known{Branches: []string{"feature"}}); !hint.OK || hint.Key != hintWillCheckOut {
		t.Fatalf("hint = %+v, want the branch to be checked out", hint)
	}
}

func TestADetachedWorktreeNeedsAStartPoint(t *testing.T) {
	req := Request{Path: emptyDir(t), Mode: ModeDetached}

	if hint := Validate(req, Known{}); hint.Key != hintStartRequired {
		t.Fatalf("hint = %+v, want the start point request", hint)
	}
}

func TestADetachedWorktreeIsAcceptedWithAStartPoint(t *testing.T) {
	req := Request{Path: emptyDir(t), Mode: ModeDetached, StartPoint: "HEAD~2"}

	if hint := Validate(req, Known{}); !hint.OK || hint.Key != hintWillDetach {
		t.Fatalf("hint = %+v, want the detached worktree to be accepted", hint)
	}
}

func TestTheDirectoryIsNamedAfterTheBranch(t *testing.T) {
	for _, c := range []struct{ parent, branch, want string }{
		{"/repos", "feature/login", filepath.Join("/repos", "feature-login")},
		{"/repos", "bug:1", filepath.Join("/repos", "bug-1")},
		{"", "feature", ""},
		{"/repos", "  ", ""},
	} {
		if got := DirectoryFor(c.parent, c.branch); got != c.want {
			t.Fatalf("DirectoryFor(%q, %q) = %q, want %q", c.parent, c.branch, got, c.want)
		}
	}
}

func TestADirectoryThatCannotBeReadIsTreatedAsBusy(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub")
	if err := os.Mkdir(nested, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(nested, 0o700) })
	if err := os.WriteFile(filepath.Join(nested, "file"), []byte("x"), 0o600); err != nil {
		t.Skip("the directory is readable on this platform")
	}

	if hint := Validate(Request{Path: nested, Branch: "feature"}, Known{}); hint.Key != hintPathBusy {
		t.Fatalf("hint = %+v, want the busy-directory refusal", hint)
	}
}
