package repo

import (
	gitrefs "github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

const branchShortHashLength = 7

var openBranchRepository = gitrepo.Open

var openBranchRefsStore = gitrefs.Open

func CurrentBranch(path string) string {
	r, err := openBranchRepository(path, gitrepo.OpenOptions{})
	if err != nil {
		return ""
	}
	defer func() { _ = r.Close() }()

	store, err := openBranchRefsStore(gitrefs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir()})
	if err != nil {
		return ""
	}
	defer func() { _ = store.Close() }()

	head, err := store.Lookup(gitrefs.HEAD)
	if err != nil {
		return ""
	}
	if head.IsSymbolic() {
		return head.SymbolicTarget.Short()
	}
	return head.Target.String()[:branchShortHashLength]
}
