package revision

import (
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func comparatorFor(order Order) func(a, b entry) bool {
	switch order {
	case DateOrder:
		return byCommitDate
	case AuthorDate:
		return byAuthorDate
	default:
		return byInsertion
	}
}

func sortNodes(list []*node, order Order) []*node {
	if order == Default || len(list) == 0 {
		return list
	}
	indegree := make(map[hash.ObjectID]int, len(list))
	byID := make(map[hash.ObjectID]*node, len(list))
	for _, n := range list {
		indegree[n.id] = 1
		byID[n.id] = n
	}
	for _, n := range list {
		for _, id := range n.parents {
			if count, ok := indegree[id]; ok && count > 0 {
				indegree[id] = count + 1
			}
		}
	}
	tips := make([]*node, 0, len(list))
	for _, n := range list {
		if indegree[n.id] == 1 {
			tips = append(tips, n)
		}
	}
	if order == Topo {
		slices.Reverse(tips)
	}
	pending := newQueue(comparatorFor(order))
	for _, n := range tips {
		pending.push(n)
	}
	sorted := make([]*node, 0, len(list))
	for pending.Len() > 0 {
		current := pending.pop()
		for _, id := range current.parents {
			count, ok := indegree[id]
			if !ok || count == 0 {
				continue
			}
			indegree[id] = count - 1
			if count-1 == 1 {
				pending.push(byID[id])
			}
		}
		indegree[current.id] = 0
		sorted = append(sorted, current)
	}
	return sorted
}
