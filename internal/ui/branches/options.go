package branches

type SortMode int

const (
	SortByName SortMode = iota
	SortByNameReverseNumbers
	SortByCommitTime
)

type Grouping struct {
	ByPath         bool
	ExceptSingles  bool
	GroupsFirst    bool
	AfterLastSlash bool
}

type Options struct {
	Sort         SortMode
	Grouping     Grouping
	FlowSections bool
}

func DefaultOptions() Options {
	return Options{Grouping: Grouping{ByPath: true}}
}

func (v *View) SetOptions(o Options) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.options = o
	if v.tree != nil {
		v.render(v.last)
	}
}

func (v *View) Options() Options {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.options
}
