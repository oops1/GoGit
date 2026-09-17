package diff

import (
	"context"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TreeRenamesInto(ctx context.Context, source Objects, oldTree, newTree hash.ObjectID, opts Options, wanted func(path string) bool) ([]File, error) {
	opts = opts.normalized()
	opts.DetectRenames, opts.DetectCopies, opts.Paths = true, false, nil
	w := &walker{ctx: ctx, source: source, opts: opts}
	if err := w.walk("", oldTree, newTree); err != nil {
		return nil, err
	}
	candidates := slices.DeleteFunc(w.pairs, func(p pair) bool {
		return p.file.Status != StatusDeleted && !wanted(p.file.NewPath)
	})
	pairs, err := detectRenames(candidates, source, opts)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(pairs))
	for _, p := range pairs {
		if p.file.Status != StatusDeleted {
			files = append(files, p.file)
		}
	}
	return files, nil
}
