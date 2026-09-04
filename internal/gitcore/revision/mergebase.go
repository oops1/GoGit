package revision

import "github.com/oops1/gogit/internal/gitcore/hash"

func MergeBase(ctx Context, ids ...hash.ObjectID) ([]hash.ObjectID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	objects := newStore(ctx.Objects)
	result := []hash.ObjectID{ids[0]}
	for _, id := range ids[1:] {
		var merged []hash.ObjectID
		for _, base := range result {
			found, err := mergeBasesPair(objects, id, base)
			if err != nil {
				return nil, err
			}
			merged = append(merged, found...)
		}
		if len(merged) == 0 {
			return nil, nil
		}
		result = merged
	}
	return removeRedundant(objects, result)
}

func IsAncestor(ctx Context, a, b hash.ObjectID) (bool, error) {
	return isAncestor(newStore(ctx.Objects), a, b)
}

func mergeBasesPair(objects *store, a, b hash.ObjectID) ([]hash.ObjectID, error) {
	if a == b {
		return []hash.ObjectID{a}, nil
	}
	common, err := paintDownToCommon(newGraph(objects), a, []hash.ObjectID{b})
	if err != nil {
		return nil, err
	}
	ids := make([]hash.ObjectID, 0, len(common))
	for _, n := range common {
		ids = append(ids, n.id)
	}
	return removeRedundant(objects, ids)
}

func isAncestor(objects *store, a, b hash.ObjectID) (bool, error) {
	if a == b {
		return true, nil
	}
	painted := newGraph(objects)
	if _, err := paintDownToCommon(painted, a, []hash.ObjectID{b}); err != nil {
		return false, err
	}
	return painted.node(a).flags&flagParent2 != 0, nil
}

func removeRedundant(objects *store, ids []hash.ObjectID) ([]hash.ObjectID, error) {
	unique := make([]hash.ObjectID, 0, len(ids))
	seen := make(map[hash.ObjectID]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	kept := make([]hash.ObjectID, 0, len(unique))
	for index, id := range unique {
		redundant := false
		for other := range unique {
			if other == index {
				continue
			}
			ancestor, err := isAncestor(objects, id, unique[other])
			if err != nil {
				return nil, err
			}
			if ancestor {
				redundant = true
				break
			}
		}
		if !redundant {
			kept = append(kept, id)
		}
	}
	return kept, nil
}

func paintDownToCommon(painted *graph, one hash.ObjectID, twos []hash.ObjectID) ([]*node, error) {
	pending := newQueue(byCommitDate)
	first, err := painted.commit(one)
	if err != nil {
		return nil, err
	}
	first.flags |= flagParent1
	pending.push(first)
	for _, id := range twos {
		other, err := painted.commit(id)
		if err != nil {
			return nil, err
		}
		other.flags |= flagParent2
		pending.push(other)
	}
	var common []*node
	for hasNonStale(pending) {
		current := pending.pop()
		flags := current.flags & (flagParent1 | flagParent2 | flagStale)
		if flags == flagParent1|flagParent2 {
			if current.flags&flagResult == 0 {
				current.flags |= flagResult
				common = append(common, current)
			}
			flags |= flagStale
		}
		for _, id := range current.parents {
			parent := painted.node(id)
			if parent.flags&flags == flags {
				continue
			}
			if err := painted.load(parent); err != nil {
				return nil, err
			}
			parent.flags |= flags
			pending.push(parent)
		}
	}
	return common, nil
}

func hasNonStale(pending *queue) bool {
	for _, item := range pending.items {
		if item.node.flags&flagStale == 0 {
			return true
		}
	}
	return false
}
