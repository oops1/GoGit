package diff

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type RenameSearch struct {
	Limit    int
	Relevant func(path string) bool
}

type RenameReport struct {
	Files       []File
	NeededLimit int
}

func TreeRenames(ctx context.Context, source Objects, oldTree, newTree hash.ObjectID, search RenameSearch) (RenameReport, error) {
	opts := Options{DetectRenames: true, NoRenameEmpty: true, RenameLimit: search.Limit}.normalized()
	w := &walker{ctx: ctx, source: source, opts: opts}
	if err := w.walk("", oldTree, newTree); err != nil {
		return RenameReport{}, err
	}
	pairs, needed, err := detectRelevantRenames(w.pairs, source, opts, search.Relevant)
	if err != nil {
		return RenameReport{}, err
	}
	report := RenameReport{Files: make([]File, 0, len(pairs)), NeededLimit: needed}
	for _, p := range pairs {
		report.Files = append(report.Files, p.file)
	}
	return report, nil
}
