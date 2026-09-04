package changes

import (
	"path"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/i18n"
)

const MaxFiles = 500

type Row struct {
	Status string
	Name   string
	Path   string
}

func Files(files []diff.File) []Row {
	limit := len(files)
	truncated := limit > MaxFiles
	if truncated {
		limit = MaxFiles
	}
	rows := make([]Row, 0, limit+1)
	for _, f := range files[:limit] {
		rows = append(rows, fileRow(f))
	}
	if truncated {
		rows = append(rows, Row{Name: i18n.T("Diff.Truncated")})
	}
	return rows
}

func fileRow(f diff.File) Row {
	oldPath, newPath := f.OldPath, f.NewPath
	namePath := newPath
	if namePath == "" {
		namePath = oldPath
	}
	displayPath := namePath
	if (f.Status == diff.StatusRenamed || f.Status == diff.StatusCopied) && oldPath != newPath {
		displayPath = oldPath + " → " + newPath
	}
	return Row{Status: f.Status.String(), Name: path.Base(namePath), Path: displayPath}
}

func SortEntries(entries []worktree.Entry) []worktree.Entry {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, compareWorkingEntries)
	return sorted
}

func compareWorkingEntries(a, b worktree.Entry) int {
	if order := workingRank(a) - workingRank(b); order != 0 {
		return order
	}
	return strings.Compare(a.Path, b.Path)
}

func workingRank(e worktree.Entry) int {
	switch {
	case e.Conflict != worktree.ConflictNone:
		return 0
	case e.Staged != worktree.StatusUnmodified:
		return 1
	case e.Unstaged != worktree.StatusUnmodified && e.Unstaged != worktree.StatusUntracked:
		return 2
	default:
		return 3
	}
}

func WorkingRows(s worktree.Status) []Row {
	sorted := SortEntries(s.Entries)
	limit := len(sorted)
	truncated := limit > MaxFiles
	if truncated {
		limit = MaxFiles
	}
	rows := make([]Row, 0, limit+1)
	for _, e := range sorted[:limit] {
		rows = append(rows, workingRow(e))
	}
	if truncated {
		rows = append(rows, Row{Name: i18n.T("Diff.Truncated")})
	}
	return rows
}

func workingRow(e worktree.Entry) Row {
	displayPath := e.Path
	if e.OrigPath != "" {
		displayPath = e.OrigPath + " → " + e.Path
	}
	return Row{Status: workingStatus(e), Name: path.Base(e.Path), Path: displayPath}
}

func workingStatus(e worktree.Entry) string {
	if e.Unstaged == worktree.StatusUntracked {
		return "??"
	}
	return string(rune(e.Staged)) + string(rune(e.Unstaged))
}
