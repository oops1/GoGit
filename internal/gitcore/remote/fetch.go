package remote

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

const fetchHeadFile = "FETCH_HEAD"

var (
	dial     = dialAny
	odbOpen  = odb.Open
	refsOpen = refs.Open
)

type FetchOptions struct {
	Refspecs    []refspec.RefSpec
	Depth       int
	Deepen      int
	DeepenSince time.Time
	DeepenNot   []string
	Unshallow   bool
	Tags        TagMode
	Prune       bool
	Force       bool
	Progress    progress.Func
	Transport   transport.Options
}

type FetchResult struct {
	Changes   []Change
	Refs      []transport.Ref
	Head      string
	Objects   int
	Bytes     int64
	Shallow   []hash.ObjectID
	Unshallow []hash.ObjectID
}

func fetchRefspecs(rem Remote, opts FetchOptions) []refspec.RefSpec {
	if len(opts.Refspecs) > 0 {
		return opts.Refspecs
	}
	if len(rem.Fetch) > 0 {
		return rem.Fetch
	}
	return []refspec.RefSpec{refspec.DefaultFetch(rem.Name)}
}

func Fetch(ctx context.Context, r *repo.Repository, rem Remote, opts FetchOptions) (FetchResult, error) {
	url := rem.FetchURL()
	if url == "" {
		return FetchResult{}, ErrNoURL
	}
	specs := fetchRefspecs(rem, opts)
	prog := opts.Progress

	transportOpts := opts.Transport
	transportOpts.Progress = prog
	prog.Phase("connecting")
	session, err := dial(ctx, url, transport.UploadPack, transportOpts)
	if err != nil {
		return FetchResult{}, err
	}
	defer func() { _ = session.Close() }()

	adv, err := session.Advertise(ctx)
	if err != nil {
		return FetchResult{}, err
	}

	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return FetchResult{}, err
	}
	defer func() { _ = db.Close() }()

	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare(), Peeler: db})
	if err != nil {
		return FetchResult{}, err
	}
	defer func() { _ = store.Close() }()

	shallow, err := r.Shallow()
	if err != nil {
		return FetchResult{}, err
	}

	matched, err := selectMatches(adv, specs, opts.Tags, db)
	if err != nil {
		return FetchResult{}, err
	}

	result := FetchResult{Refs: adv.Refs, Head: adv.Head}
	if len(matched) == 0 {
		return result, nil
	}

	neg, err := newNegotiator(ctx, store, db, shallow)
	if err != nil {
		return FetchResult{}, err
	}

	req := buildFetchRequest(matched, opts, shallow, prog)
	prog.Phase("negotiating")
	resp, err := session.Fetch(ctx, req, neg)
	if err != nil {
		return FetchResult{}, err
	}
	defer resp.Pack.Close()

	prog.Phase("receiving")
	indexed, err := pack.IndexPack(ctx, resp.Pack, r.PackDir(), pack.IndexOptions{Bases: db, FixThin: true, Progress: prog})
	if err != nil {
		return FetchResult{}, err
	}
	result.Objects, result.Bytes = indexed.Objects, indexed.Bytes
	result.Shallow, result.Unshallow = resp.Shallow, resp.Unshallow
	if _, err := db.Reload(); err != nil {
		return FetchResult{}, err
	}

	var stale []refs.Ref
	if opts.Prune {
		stale, err = pruneStale(store, specs, matched)
		if err != nil {
			return FetchResult{}, err
		}
	}

	prog.Phase("updating-refs")
	applied, changes, updateErr := commitRefUpdates(store, db, shallow, matched, stale, opts)
	result.Changes = changes

	var errs []error
	if updateErr != nil {
		errs = append(errs, updateErr)
	}
	if err := writeFetchHead(r, url, applied, adv.Head); err != nil {
		errs = append(errs, err)
	}
	if err := applyShallowResponse(r, shallow, resp); err != nil {
		errs = append(errs, err)
	}
	return result, errors.Join(errs...)
}

func buildFetchRequest(matched []matchedRef, opts FetchOptions, shallow map[hash.ObjectID]struct{}, prog progress.Func) transport.FetchRequest {
	wants := make([]hash.ObjectID, 0, len(matched))
	seen := make(map[hash.ObjectID]bool, len(matched))
	for _, m := range matched {
		if seen[m.ref.ID] {
			continue
		}
		seen[m.ref.ID] = true
		wants = append(wants, m.ref.ID)
	}
	req := transport.FetchRequest{
		Wants:       wants,
		DeepenSince: opts.DeepenSince,
		DeepenNot:   opts.DeepenNot,
		IncludeTags: opts.Tags == TagsFollow,
		ThinPack:    true,
		Progress:    prog,
	}
	switch {
	case opts.Unshallow:
		req.Depth = math.MaxInt32
	case opts.Deepen > 0:
		req.Depth = opts.Deepen
	case opts.Depth > 0:
		req.Depth = opts.Depth
	}
	if len(shallow) > 0 {
		req.Shallow = make([]hash.ObjectID, 0, len(shallow))
		for id := range shallow {
			req.Shallow = append(req.Shallow, id)
		}
	}
	return req
}

func commitRefUpdates(store *refs.Store, db objectStore, shallow map[hash.ObjectID]struct{}, matched []matchedRef, stale []refs.Ref, opts FetchOptions) ([]matchedRef, []Change, error) {
	tx := store.Begin()
	var applied []matchedRef
	var changes []Change
	var rejected []error

	for _, m := range matched {
		has, err := db.Has(m.ref.ID)
		if err != nil {
			return nil, nil, err
		}
		if !has {
			if m.optional {
				continue
			}
			rejected = append(rejected, fmt.Errorf("remote: object %s for %s was not received", m.ref.ID, m.dst))
			continue
		}
		current, existed, err := lookupCurrent(store, m.dst)
		if err != nil {
			return nil, nil, err
		}
		if existed && current == m.ref.ID {
			applied = append(applied, m)
			continue
		}
		forced := m.force || opts.Force
		ff, err := isFastForward(db, shallow, current, m.ref.ID)
		if err != nil {
			if !forced {
				return nil, nil, err
			}
			ff = false
		}
		if !ff && !forced {
			rejected = append(rejected, fmt.Errorf("%w: %s", ErrNonFastForward, m.dst))
			continue
		}
		if err := tx.Update(m.dst, m.ref.ID, current); err != nil {
			return nil, nil, err
		}
		applied = append(applied, m)
		changes = append(changes, Change{Name: m.dst, Old: current, New: m.ref.ID, Created: !existed, Forced: !ff && forced})
	}

	for _, ref := range stale {
		if err := tx.Delete(ref.Name, ref.Target); err != nil {
			return nil, nil, err
		}
		changes = append(changes, Change{Name: ref.Name, Old: ref.Target, Deleted: true})
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return applied, changes, errors.Join(rejected...)
}

func lookupCurrent(store *refs.Store, name refs.Name) (hash.ObjectID, bool, error) {
	ref, err := store.Lookup(name)
	if errors.Is(err, refs.ErrNotFound) {
		return hash.Zero, false, nil
	}
	if err != nil {
		return hash.Zero, false, err
	}
	if ref.IsSymbolic() {
		resolved, err := store.Resolve(name)
		if err != nil {
			return hash.Zero, false, err
		}
		return resolved.Target, true, nil
	}
	return ref.Target, true, nil
}

func isFastForward(db objectStore, shallow map[hash.ObjectID]struct{}, oldID, newID hash.ObjectID) (bool, error) {
	if oldID.IsZero() || oldID == newID {
		return true, nil
	}
	oldType, oldPeeled, err := db.Peel(oldID)
	if err != nil {
		return false, err
	}
	newType, newPeeled, err := db.Peel(newID)
	if err != nil {
		return false, err
	}
	if oldType != object.TypeCommit || newType != object.TypeCommit {
		return false, nil
	}
	return revision.IsAncestor(revision.Context{Objects: db, Shallow: shallow}, oldPeeled, newPeeled)
}

func applyShallowResponse(r *repo.Repository, shallow map[hash.ObjectID]struct{}, resp *transport.FetchResponse) error {
	if len(resp.Shallow) == 0 && len(resp.Unshallow) == 0 {
		return nil
	}
	merged := make(map[hash.ObjectID]struct{}, len(shallow)+len(resp.Shallow))
	for id := range shallow {
		merged[id] = struct{}{}
	}
	for _, id := range resp.Shallow {
		merged[id] = struct{}{}
	}
	for _, id := range resp.Unshallow {
		delete(merged, id)
	}
	ids := make([]hash.ObjectID, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	return r.WriteShallow(ids)
}

func forMergeIndexes(matched []matchedRef, head string) map[int]bool {
	primary := make(map[int]bool)
	for i, m := range matched {
		if !m.wildcard {
			primary[i] = true
		}
	}
	if len(primary) > 0 {
		return primary
	}
	if head != "" {
		for i, m := range matched {
			if m.ref.Name == head {
				primary[i] = true
			}
		}
	}
	if len(primary) == 0 {
		primary[0] = true
	}
	return primary
}

func describeRef(name refs.Name, url string) string {
	switch {
	case name.IsBranch():
		return "branch '" + name.Short() + "' of " + url
	case name.IsTag():
		return "tag '" + name.Short() + "' of " + url
	default:
		return "'" + string(name) + "' of " + url
	}
}

func fetchHeadLine(id hash.ObjectID, forMerge bool, description string) string {
	mark := "not-for-merge"
	if forMerge {
		mark = ""
	}
	return id.String() + "\t" + mark + "\t" + description + "\n"
}

func writeFetchHead(r *repo.Repository, url string, applied []matchedRef, head string) error {
	if len(applied) == 0 {
		return nil
	}
	primary := forMergeIndexes(applied, head)
	var b strings.Builder
	for i, m := range applied {
		if !primary[i] {
			continue
		}
		b.WriteString(fetchHeadLine(m.ref.ID, true, describeRef(refs.Name(m.ref.Name), url)))
	}
	for i, m := range applied {
		if primary[i] {
			continue
		}
		b.WriteString(fetchHeadLine(m.ref.ID, false, describeRef(refs.Name(m.ref.Name), url)))
	}
	return r.Root().WriteFile(fetchHeadFile, []byte(b.String()), 0o666)
}

func LsRemote(ctx context.Context, url string, opts transport.Options) ([]transport.Ref, error) {
	session, err := dial(ctx, url, transport.UploadPack, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	adv, err := session.Advertise(ctx)
	if err != nil {
		return nil, err
	}
	return adv.Refs, nil
}
