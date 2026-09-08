package worktree

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

type Mode int

const (
	ModeNewBranch Mode = iota
	ModeExistingBranch
	ModeDetached
)

type Request struct {
	Path       string
	Mode       Mode
	Branch     string
	StartPoint string
	NoCheckout bool
}

type Known struct {
	Branches   []string
	CheckedOut []string
	Head       string
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

const (
	hintPathRequired     = "Dialog.Worktree.Hint.PathRequired"
	hintPathNotDirectory = "Dialog.Worktree.Hint.PathNotDirectory"
	hintPathBusy         = "Dialog.Worktree.Hint.PathBusy"
	hintBranchRequired   = "Dialog.Worktree.Hint.BranchRequired"
	hintBranchInvalid    = "Dialog.Worktree.Hint.BranchInvalid"
	hintBranchExists     = "Dialog.Worktree.Hint.BranchExists"
	hintBranchCheckedOut = "Dialog.Worktree.Hint.BranchCheckedOut"
	hintStartRequired    = "Dialog.Worktree.Hint.StartRequired"
	hintWillCreate       = "Dialog.Worktree.Hint.WillCreate"
	hintWillCheckOut     = "Dialog.Worktree.Hint.WillCheckOut"
	hintWillDetach       = "Dialog.Worktree.Hint.WillDetach"
)

func Validate(req Request, known Known) Hint {
	path := strings.TrimSpace(req.Path)
	if hint, ok := validatePath(path); !ok {
		return hint
	}
	switch req.Mode {
	case ModeExistingBranch:
		return validateExisting(path, strings.TrimSpace(req.Branch), known)
	case ModeDetached:
		return validateDetached(path, strings.TrimSpace(req.StartPoint), known)
	default:
		return validateNew(path, strings.TrimSpace(req.Branch), known)
	}
}

func validatePath(path string) (Hint, bool) {
	if path == "" {
		return Hint{Key: hintPathRequired}, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return Hint{}, true
	}
	if !info.IsDir() {
		return Hint{Key: hintPathNotDirectory, Args: []any{path}}, false
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) > 0 {
		return Hint{Key: hintPathBusy, Args: []any{path}}, false
	}
	return Hint{}, true
}

func validateNew(path, branch string, known Known) Hint {
	if branch == "" {
		return Hint{Key: hintBranchRequired}
	}
	if err := refs.BranchName(branch).Validate(); err != nil {
		return Hint{Key: hintBranchInvalid, Args: []any{branch}}
	}
	if slices.Contains(known.Branches, branch) {
		return Hint{Key: hintBranchExists, Args: []any{branch}}
	}
	return Hint{Key: hintWillCreate, Args: []any{branch, startOrHead(known)}, OK: true}
}

func validateExisting(path, branch string, known Known) Hint {
	if branch == "" {
		return Hint{Key: hintBranchRequired}
	}
	if slices.Contains(known.CheckedOut, branch) {
		return Hint{Key: hintBranchCheckedOut, Args: []any{branch}}
	}
	return Hint{Key: hintWillCheckOut, Args: []any{branch, path}, OK: true}
}

func validateDetached(path, start string, known Known) Hint {
	if start == "" {
		return Hint{Key: hintStartRequired}
	}
	return Hint{Key: hintWillDetach, Args: []any{start, path}, OK: true}
}

func startOrHead(known Known) string {
	if known.Head != "" {
		return known.Head
	}
	return string(refs.HEAD)
}

func DirectoryFor(parent, branch string) string {
	name := SanitiseName(branch)
	if parent == "" || name == "" {
		return ""
	}
	return filepath.Join(parent, name)
}

func SanitiseName(branch string) string {
	name := strings.TrimSpace(branch)
	name = strings.Trim(name, "/")
	name = strings.ReplaceAll(name, "/", "-")
	return strings.Map(keepPathRune, name)
}

func keepPathRune(r rune) rune {
	if strings.ContainsRune(`\:*?"<>|`, r) {
		return '-'
	}
	return r
}
