package ops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func mustRefuseOverwriting(t *testing.T, err error, want ...string) {
	t.Helper()
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) || !slices.Equal(overwrite.Paths, want) {
		t.Fatalf("err = %v, want an OverwriteError for %v", err, want)
	}
}

func TestMergeRefusesAnUntrackedFileInALeadingPath(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"d/x": "theirs\n"})
	tr.writeFile("d", "untracked\n")

	_, err := tr.merge("feature", MergeOptions{})

	mustRefuseOverwriting(t, err, "d")
	if got := tr.readFile("d"); got != "untracked\n" {
		t.Fatalf("d = %q after the refused merge", got)
	}
}

func TestFastForwardRefusesAnUntrackedFileInALeadingPath(t *testing.T) {
	tr := newTestRepo(t)
	base := tr.commitFiles("base", map[string]string{"f": "f\n"})
	tr.createBranch("feature", base)
	tr.switchTo("feature")
	tr.commitFiles("next", map[string]string{"d/x": "theirs\n"})
	tr.switchTo("main")
	tr.writeFile("d", "untracked\n")

	result, err := tr.merge("feature", MergeOptions{})

	mustRefuseOverwriting(t, err, "d")
	if result.FastForward || tr.readFile("d") != "untracked\n" {
		t.Fatalf("result = %+v, d = %q after the refused fast-forward", result, tr.readFile("d"))
	}
}

func TestMergeReportsALeadingPathItCannotInspect(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"d/x": "theirs\n"})
	original := fsRootLstat
	fsRootLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if filepath.ToSlash(name) == "d" {
			return nil, errInjected
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootLstat = original })

	if _, err := tr.merge("feature", MergeOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the injected failure", err)
	}
}

func TestMergeWithIgnoreCaseJudgesANewNameByItsTrackedCaseTwin(t *testing.T) {
	tests := []struct {
		name    string
		local   string
		refused bool
	}{
		{"clean twin", "", false},
		{"edited twin", "local edit\n", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := newIgnoreCaseRepo(t)
			f := tenLines("f")
			base := tr.commitFiles("base", map[string]string{"f": f, "readme": "hello\n"})
			renamed := putTree(t, tr,
				object.TreeEntry{Mode: object.ModeBlob, Name: "README", ID: storedBlob(t, tr, "hello\n")},
				object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: storedBlob(t, tr, f)},
			)
			tr.createBranch("feature", putCommit(t, tr, renamed, base))
			tr.commitFiles("ours", map[string]string{"f": changeLine(f, 0, "OURS")})
			if tc.local != "" {
				tr.writeFile("readme", tc.local)
			}

			_, err := tr.merge("feature", MergeOptions{})

			if refused := err != nil; refused != tc.refused {
				t.Fatalf("err = %v, want refused = %v", err, tc.refused)
			}
			if tc.refused {
				return
			}
			idx := tr.index()
			if _, ok := entryOf(t, idx, "README"); !ok {
				t.Fatal("README is not staged after the merge")
			}
			if _, ok := entryOf(t, idx, "readme"); ok {
				t.Fatal("readme stays staged after the merge")
			}
		})
	}
}
