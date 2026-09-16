//go:build oracle

package diff

import (
	"strings"
	"testing"
)

func pathspecTrees() (treeFiles, treeFiles) {
	names := []string{
		"README.md", "main.go", "Main.GO", "cmd/app/main.go", "cmd/app/util_test.go",
		"dir/sub/deep/x.go", "dir/sub/y.txt", "dir/a.txt", "dir-x/b.txt", "dir.go",
		"docs/guide.md", "Docs/Upper.md", "we*ird/f.txt", "we?ird.txt", "a[1].txt", "src/lib.c",
	}
	old, updated := treeFiles{}, treeFiles{}
	for _, name := range names {
		old[name] = blobSpec("old " + name + "\n")
		updated[name] = blobSpec("new " + name + "\n")
	}
	old["gone/removed.go"] = blobSpec("removed\n")
	updated["fresh/added.go"] = blobSpec("added\n")
	return old, updated
}

func pathspecCases() [][]string {
	return [][]string{
		{"*.go"}, {"dir/**"}, {":(glob)dir/**"}, {":(glob)*.go"}, {":(glob)**/*.go"},
		{":!*.go"}, {":^dir"}, {":(exclude)cmd", "dir"}, {":(icase)docs"}, {":(icase)*.MD"},
		{":(literal)we*ird"}, {"we*ird"}, {"a[1].txt"}, {":(literal)a[1].txt"}, {"dir/"},
		{"dir"}, {"di"}, {"./dir/../cmd"}, {":/cmd/app"}, {":(top)dir"}, {"cmd/*/main.go"},
		{":(glob)cmd/*/main.go"}, {"dir*"}, {":(glob)dir*"}, {"*"}, {"."}, {":"}, {"*/main.go"},
		{"dir/sub", ":!dir/sub/deep"}, {":(icase,glob)CMD/**/*.GO"}, {"we?ird.txt"},
		{"cmd/app/main.go", "src"}, {"dir/sub/deep/"}, {"::dir"}, {":!:dir"}, {":(exclude,icase)DIR", "*.txt"},
		{"*.GO"}, {"d?r"}, {"dir/*/x.go"}, {":(glob)dir/*/x.go"}, {"fresh", "gone"}, {":(icase)dir/SUB"},
		{"dir.go/x/.."}, {"dir/sub/.."}, {"src/./lib.c"}, {"dir//a.txt"},
	}
}

func TestPathspecTreeDiffMatchesGit(t *testing.T) {
	o := newOracle(t)
	db := o.objects()
	oldFiles, newFiles := pathspecTrees()
	oldTree, newTree := buildTree(o, oldFiles), buildTree(o, newFiles)
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	for _, specs := range pathspecCases() {
		t.Run(strings.Join(specs, " "), func(t *testing.T) {
			args := append([]string{"diff-tree", "-r", "--name-only", "--no-renames", oldTree.String(), newTree.String(), "--"}, specs...)
			want := strings.Fields(o.run(o.repo, nil, args...))
			opts := Defaults()
			opts.DetectRenames = false
			opts.Paths = specs
			files, err := TreeChanges(t.Context(), db, oldTree, newTree, opts)
			if err != nil {
				t.Fatalf("TreeChanges returned error %v", err)
			}
			got := pathsOf(files)
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("paths %q, git reports %q", got, want)
			}
		})
	}
}
