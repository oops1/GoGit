package ops

import (
	"math/bits"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const bisectRandomModulo = 32768

type bisectNode struct {
	id      hash.ObjectID
	subject string
	parents []hash.ObjectID
	kin     []*bisectNode
	weight  int
}

func linkBisectNodes(list []*bisectNode) {
	known := make(map[hash.ObjectID]*bisectNode, len(list))
	for _, n := range list {
		known[n.id] = n
	}
	for _, n := range list {
		n.kin = n.kin[:0]
		for _, parent := range n.parents {
			if inside, ok := known[parent]; ok {
				n.kin = append(n.kin, inside)
			}
		}
	}
}

func bisectDistance(n *bisectNode, nr int) int { return min(n.weight, nr-n.weight) }

func bisectHalfway(n *bisectNode, nr int) bool {
	gap := 2*n.weight - nr
	return gap >= -1 && gap <= 1
}

func countBisectDistance(start *bisectNode) int {
	seen := make(map[hash.ObjectID]struct{})
	stack := []*bisectNode{start}
	count := 0
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, done := seen[n.id]; done {
			continue
		}
		seen[n.id] = struct{}{}
		count++
		stack = append(stack, n.kin...)
	}
	return count
}

func weighedBisectParent(n *bisectNode) *bisectNode {
	for _, parent := range n.kin {
		if parent.weight >= 0 {
			return parent
		}
	}
	return nil
}

func seedBisectWeights(list []*bisectNode) int {
	counted := 0
	for _, n := range list {
		switch len(n.kin) {
		case 0:
			n.weight = 1
			counted++
		case 1:
			n.weight = -1
		default:
			n.weight = -2
		}
	}
	return counted
}

func findBisection(list []*bisectNode, sorted bool) ([]*bisectNode, int) {
	linkBisectNodes(list)
	nr := len(list)
	counted := seedBisectWeights(list)
	for _, n := range list {
		if n.weight != -2 {
			continue
		}
		n.weight = countBisectDistance(n)
		counted++
		if !sorted && bisectHalfway(n, nr) {
			return []*bisectNode{n}, n.weight
		}
	}
	for counted < nr {
		for _, n := range list {
			if n.weight >= 0 {
				continue
			}
			parent := weighedBisectParent(n)
			if parent == nil {
				continue
			}
			n.weight = parent.weight + 1
			counted++
			if !sorted && bisectHalfway(n, nr) {
				return []*bisectNode{n}, n.weight
			}
		}
	}
	if sorted {
		ordered := sortedBisection(list, nr)
		return ordered, ordered[0].weight
	}
	best := bestBisection(list, nr)
	return []*bisectNode{best}, best.weight
}

func bestBisection(list []*bisectNode, nr int) *bisectNode {
	best, reach := list[0], -1
	for _, n := range list {
		if d := bisectDistance(n, nr); d > reach {
			best, reach = n, d
		}
	}
	return best
}

func sortedBisection(list []*bisectNode, nr int) []*bisectNode {
	ordered := slices.Clone(list)
	slices.SortFunc(ordered, func(a, b *bisectNode) int {
		if gap := bisectDistance(b, nr) - bisectDistance(a, nr); gap != 0 {
			return gap
		}
		return a.id.Compare(b.id)
	})
	return ordered
}

func bisectRandom(count int) int {
	mixed := uint32(count)*1103515245 + 12345
	return int(mixed / 65536 % bisectRandomModulo)
}

func integerRoot(value int) int {
	if value <= 0 {
		return 0
	}
	root, next := value, (value+1)/2
	for next < root {
		root, next = next, (next+value/next)/2
	}
	return root
}

func bisectSkipIndex(count int) int {
	prn := bisectRandom(count)
	return count * prn / bisectRandomModulo * integerRoot(prn) / integerRoot(bisectRandomModulo)
}

func skipBisectionAway(list []*bisectNode, bad hash.ObjectID) []*bisectNode {
	index := bisectSkipIndex(len(list))
	if list[index].id != bad {
		return list[index:]
	}
	if index > 0 {
		return list[index-1:]
	}
	return list
}

func estimateBisectSteps(all int) int {
	if all < 3 {
		return 0
	}
	n := bits.Len(uint(all)) - 1
	e := 1 << n
	if e < 3*(all-e) {
		return n
	}
	return n - 1
}
