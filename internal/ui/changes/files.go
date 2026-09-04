package changes

import (
	"path"

	"github.com/oops1/gogit/internal/gitcore/diff"
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
