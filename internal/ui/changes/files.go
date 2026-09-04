package changes

import (
	"cmp"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/i18n"
)

const MaxFiles = 500

const modifiedLayout = "2006-01-02 15:04:05"

type Row struct {
	Name         string
	State        string
	IndexState   string
	WorkingState string
	RelDir       string
	Extension    string
	Modified     string
	RelPath      string
	RenamedFrom  string
	Size         string
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
	namePath := cmp.Or(f.NewPath, f.OldPath)
	dir, name := splitRelDir(namePath)
	row := Row{
		Name:      name,
		State:     diffStatusWord(f.Status),
		RelDir:    dir,
		RelPath:   namePath,
		Size:      sizeString(diffFileSize(f)),
		Extension: extensionOf(name),
	}
	if (f.Status == diff.StatusRenamed || f.Status == diff.StatusCopied) && f.OldPath != f.NewPath {
		row.RenamedFrom = f.OldPath
	}
	return row
}

func diffFileSize(f diff.File) int64 {
	if f.NewSize > 0 {
		return int64(f.NewSize)
	}
	return int64(f.OldSize)
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
	dir, name := splitRelDir(e.Path)
	row := Row{
		Name:         name,
		State:        combinedState(e),
		IndexState:   optionalStatusWord(e.Staged),
		WorkingState: optionalStatusWord(e.Unstaged),
		RelDir:       dir,
		Extension:    extensionOf(name),
		RelPath:      e.Path,
		RenamedFrom:  e.OrigPath,
		Size:         sizeString(e.Size),
	}
	if !e.ModTime.IsZero() {
		row.Modified = e.ModTime.Format(modifiedLayout)
	}
	return row
}

func splitRelDir(p string) (dir, name string) {
	trimmed := strings.TrimSuffix(p, "/")
	d := path.Dir(trimmed)
	if d == "." {
		d = ""
	}
	return d, path.Base(trimmed)
}

func extensionOf(name string) string {
	idx := strings.LastIndexByte(name, '.')
	if idx <= 0 {
		return ""
	}
	return name[idx+1:]
}

func sizeString(size int64) string {
	if size <= 0 {
		return ""
	}
	return strconv.FormatInt(size, 10)
}

func combinedState(e worktree.Entry) string {
	switch {
	case e.Conflict != worktree.ConflictNone:
		return i18n.T("Files.State.Conflict")
	case e.Unstaged == worktree.StatusUntracked:
		return i18n.T("Files.State.Untracked")
	case e.Staged != worktree.StatusUnmodified && e.Unstaged != worktree.StatusUnmodified:
		return i18n.T("Files.State.Modified")
	case e.Staged != worktree.StatusUnmodified:
		return statusCodeWord(e.Staged)
	case e.Unstaged != worktree.StatusUnmodified:
		return statusCodeWord(e.Unstaged)
	default:
		return i18n.T("Files.State.Unmodified")
	}
}

func optionalStatusWord(c worktree.StatusCode) string {
	if c == worktree.StatusUnmodified {
		return ""
	}
	return statusCodeWord(c)
}

func statusCodeWord(c worktree.StatusCode) string {
	switch c {
	case worktree.StatusModified:
		return i18n.T("Files.State.Modified")
	case worktree.StatusAdded:
		return i18n.T("Files.State.Added")
	case worktree.StatusDeleted:
		return i18n.T("Files.State.Deleted")
	case worktree.StatusRenamed:
		return i18n.T("Files.State.Renamed")
	case worktree.StatusCopied:
		return i18n.T("Files.State.Copied")
	case worktree.StatusTypeChanged:
		return i18n.T("Files.State.TypeChanged")
	case worktree.StatusUnmerged:
		return i18n.T("Files.State.Conflict")
	case worktree.StatusUntracked:
		return i18n.T("Files.State.Untracked")
	case worktree.StatusIgnored:
		return i18n.T("Files.State.Ignored")
	default:
		return i18n.T("Files.State.Unmodified")
	}
}

func diffStatusWord(s diff.Status) string {
	switch s {
	case diff.StatusModified:
		return i18n.T("Files.State.Modified")
	case diff.StatusAdded:
		return i18n.T("Files.State.Added")
	case diff.StatusDeleted:
		return i18n.T("Files.State.Deleted")
	case diff.StatusRenamed:
		return i18n.T("Files.State.Renamed")
	case diff.StatusCopied:
		return i18n.T("Files.State.Copied")
	case diff.StatusTypeChanged:
		return i18n.T("Files.State.TypeChanged")
	default:
		return i18n.T("Files.State.Unmodified")
	}
}
