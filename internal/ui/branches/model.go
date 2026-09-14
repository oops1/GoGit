package branches

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

const stashSelectorPrefix = "stash@{"

type Branch struct {
	Name           refs.Name
	Target         hash.ObjectID
	SymbolicTarget refs.Name
}

type Remote struct {
	Name     string
	Head     refs.Name
	Branches []Branch
}

type Tag struct {
	Name   refs.Name
	Target hash.ObjectID
	Peeled hash.ObjectID
}

type Stash struct {
	Index   int
	Commit  hash.ObjectID
	Message string
}

type Snapshot struct {
	Current  string
	Detached bool
	HeadID   hash.ObjectID
	Local    []Branch
	Remotes  []Remote
	Tags     []Tag
	Stashes  []Stash
}

func Load(store *refs.Store) (Snapshot, error) {
	snap := Snapshot{}
	if err := loadHead(store, &snap); err != nil {
		return Snapshot{}, err
	}
	local, err := loadRefs(store, refs.HeadsPrefix)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Local = local
	remotes, err := loadRemotes(store)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Remotes = remotes
	tags, err := loadTags(store)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Tags = tags
	stashes, err := loadStashes(store)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Stashes = stashes
	return snap, nil
}

func loadHead(store *refs.Store, snap *Snapshot) error {
	raw, err := store.Lookup(refs.HEAD)
	if err != nil {
		return err
	}
	if !raw.IsSymbolic() {
		snap.Detached = true
		snap.HeadID = raw.Target
		snap.Current = raw.Target.String()
		return nil
	}
	snap.Current = raw.SymbolicTarget.Short()
	resolved, err := store.Resolve(refs.HEAD)
	if err != nil {
		if errors.Is(err, refs.ErrNotFound) {
			return nil
		}
		return err
	}
	snap.HeadID = resolved.Target
	return nil
}

func loadRefs(store *refs.Store, prefix string) ([]Branch, error) {
	var out []Branch
	for ref, err := range store.Prefix(prefix) {
		if err != nil {
			return nil, err
		}
		out = append(out, Branch{Name: ref.Name, Target: ref.Target, SymbolicTarget: ref.SymbolicTarget})
	}
	return out, nil
}

func loadRemotes(store *refs.Store) ([]Remote, error) {
	var remotes []Remote
	index := map[string]int{}
	for ref, err := range store.Prefix(refs.RemotesPrefix) {
		if err != nil {
			return nil, err
		}
		rest := strings.TrimPrefix(string(ref.Name), refs.RemotesPrefix)
		remoteName, _, _ := strings.Cut(rest, "/")
		position, ok := index[remoteName]
		if !ok {
			position = len(remotes)
			index[remoteName] = position
			remotes = append(remotes, Remote{Name: remoteName})
		}
		if isRemoteHead(ref.Name) {
			remotes[position].Head = ref.SymbolicTarget
			continue
		}
		remotes[position].Branches = append(remotes[position].Branches, Branch{
			Name:           ref.Name,
			Target:         ref.Target,
			SymbolicTarget: ref.SymbolicTarget,
		})
	}
	return remotes, nil
}

func isRemoteHead(name refs.Name) bool {
	return strings.HasSuffix(string(name), "/"+string(refs.HEAD))
}

func loadTags(store *refs.Store) ([]Tag, error) {
	var out []Tag
	for ref, err := range store.Prefix(refs.TagsPrefix) {
		if err != nil {
			return nil, err
		}
		out = append(out, Tag{Name: ref.Name, Target: ref.Target, Peeled: ref.Peeled})
	}
	return out, nil
}

func loadStashes(store *refs.Store) ([]Stash, error) {
	var out []Stash
	for entry, err := range store.Reflog(refs.StashName) {
		if err != nil {
			return nil, err
		}
		out = append(out, Stash{Commit: entry.New, Message: entry.Message})
	}
	slices.Reverse(out)
	for i := range out {
		out[i].Index = i
	}
	return out, nil
}

func StashRef(index int) refs.Name {
	return refs.Name(stashSelectorPrefix + strconv.Itoa(index) + "}")
}

func StashIndex(ref refs.Name) (int, bool) {
	inner, ok := strings.CutPrefix(string(ref), stashSelectorPrefix)
	if !ok {
		return 0, false
	}
	digits, ok := strings.CutSuffix(inner, "}")
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(digits)
	return index, err == nil && index >= 0
}
