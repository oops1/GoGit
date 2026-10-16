package app

import (
	"sync"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/repo"
)

type divergenceKey struct {
	local  hash.ObjectID
	remote hash.ObjectID
}

func keyOf(pair repo.BranchPair) divergenceKey {
	return divergenceKey{local: pair.Local, remote: pair.Remote}
}

type divergenceMemo struct {
	mu    sync.Mutex
	known map[divergenceKey]repo.Divergence
}

func (m *divergenceMemo) count(pairs []repo.BranchPair, compute repo.Counter) (map[refs.Name]repo.Divergence, error) {
	result := make(map[refs.Name]repo.Divergence, len(pairs))
	var missing []repo.BranchPair
	m.mu.Lock()
	for _, pair := range pairs {
		counted, ok := m.known[keyOf(pair)]
		if !ok {
			missing = append(missing, pair)
			continue
		}
		if counted != (repo.Divergence{}) {
			result[pair.Name] = counted
		}
	}
	m.mu.Unlock()
	if len(missing) == 0 {
		return result, nil
	}
	computed, err := compute(missing)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.known == nil {
		m.known = make(map[divergenceKey]repo.Divergence, len(missing))
	}
	for _, pair := range missing {
		counted := computed[pair.Name]
		m.known[keyOf(pair)] = counted
		if counted != (repo.Divergence{}) {
			result[pair.Name] = counted
		}
	}
	return result, nil
}

func (m *divergenceMemo) keepOnly(pairs []repo.BranchPair) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := make(map[divergenceKey]repo.Divergence, len(pairs))
	for _, pair := range pairs {
		if counted, ok := m.known[keyOf(pair)]; ok {
			kept[keyOf(pair)] = counted
		}
	}
	m.known = kept
}
