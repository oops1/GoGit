package revision

import "github.com/oops1/gogit/internal/gitcore/hash"

func (w *walker) simplify(n *node) error {
	if len(w.paths) == 0 {
		return nil
	}
	if len(n.parents) == 0 {
		empty, err := w.sameAsEmpty(n.commit.Tree)
		if err != nil {
			return err
		}
		if empty {
			n.flags |= flagTreeSame
		}
		return nil
	}
	changed := false
	kept := make([]hash.ObjectID, 0, len(n.parents))
	for index, id := range n.parents {
		if w.opts.FirstParent && index > 0 {
			break
		}
		parent, err := w.commit(id)
		if err != nil {
			return err
		}
		same, err := w.sameTree(parent.commit.Tree, n.commit.Tree)
		if err != nil {
			return err
		}
		switch {
		case !same:
			changed = true
		case parent.flags&flagUninteresting == 0:
			n.parents = []hash.ObjectID{id}
			n.flags |= flagTreeSame
			return nil
		}
		kept = append(kept, id)
	}
	n.parents = kept
	if !changed {
		n.flags |= flagTreeSame
	}
	return nil
}

func (w *walker) sameTree(before, after hash.ObjectID) (bool, error) {
	if before == after {
		return true, nil
	}
	for _, path := range w.paths {
		first, err := w.store.lookup(before, path)
		if err != nil {
			return false, err
		}
		second, err := w.store.lookup(after, path)
		if err != nil {
			return false, err
		}
		if first != second {
			return false, nil
		}
	}
	return true, nil
}

func (w *walker) sameAsEmpty(tree hash.ObjectID) (bool, error) {
	for _, path := range w.paths {
		found, err := w.store.lookup(tree, path)
		if err != nil {
			return false, err
		}
		if found.found {
			return false, nil
		}
	}
	return true, nil
}
