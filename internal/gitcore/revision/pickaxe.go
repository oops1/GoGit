package revision

import (
	"bytes"
	"context"
	"regexp"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (w *walker) pickaxe(ctx context.Context, n *node) (bool, error) {
	if w.opts.Pickaxe == "" && w.opts.PickaxeRegexp == nil {
		return true, nil
	}
	if len(n.commit.Parents) > 1 {
		return false, nil
	}
	base := hash.Zero
	if len(n.commit.Parents) == 1 && n.flags&flagShallow == 0 {
		base = w.node(n.commit.Parents[0]).commit.Tree
	}
	opts := diff.Defaults()
	opts.Paths = w.paths
	files, err := diff.Trees(ctx, w.store.objects, base, n.commit.Tree, opts)
	if err != nil {
		return false, err
	}
	for _, file := range files {
		found, err := w.fileMatches(file)
		if err != nil || found {
			return found, err
		}
	}
	return false, nil
}

func (w *walker) fileMatches(file diff.File) (bool, error) {
	if w.opts.PickaxeRegexp != nil {
		return !file.Binary && changedLineMatches(w.opts.PickaxeRegexp, file), nil
	}
	before, err := w.blobData(file.OldMode, file.OldID)
	if err != nil {
		return false, err
	}
	after, err := w.blobData(file.NewMode, file.NewID)
	if err != nil {
		return false, err
	}
	needle := []byte(w.opts.Pickaxe)
	return bytes.Count(before, needle) != bytes.Count(after, needle), nil
}

func changedLineMatches(pattern *regexp.Regexp, file diff.File) bool {
	for _, part := range append([]diff.File{file}, file.Parts...) {
		for _, hunk := range part.Hunks {
			for _, line := range hunk.Lines {
				if line.Kind != diff.KindContext && pattern.MatchString(line.Text) {
					return true
				}
			}
		}
	}
	return false
}

func (w *walker) blobData(mode object.Mode, id hash.ObjectID) ([]byte, error) {
	if id.IsZero() {
		return nil, nil
	}
	if mode.IsSubmodule() {
		return []byte("Subproject commit " + id.String() + "\n"), nil
	}
	_, data, err := w.store.objects.Get(id)
	return data, err
}
