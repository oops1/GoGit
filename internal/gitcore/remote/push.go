package remote

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

const (
	packWindow = 10
	packDepth  = 50

	pushReflogMessage = "update by push"
)

type PushOptions struct {
	Refspecs       []refspec.RefSpec
	Force          bool
	ForceWithLease map[string]hash.ObjectID
	Atomic         bool
	Options        []string
	Progress       progress.Func
	Transport      transport.Options
}

type PushResult struct {
	Changes  []Change
	Rejected []transport.RefStatus
}

type pendingUpdate struct {
	name    refs.Name
	old     hash.ObjectID
	new     hash.ObjectID
	created bool
	deleted bool
	forced  bool
}

func pushRefspecs(rem Remote, opts PushOptions) []refspec.RefSpec {
	if len(opts.Refspecs) > 0 {
		return opts.Refspecs
	}
	return rem.Push
}

func Push(ctx context.Context, r *repo.Repository, rem Remote, opts PushOptions) (PushResult, error) {
	url := rem.PushURL()
	if url == "" {
		return PushResult{}, ErrNoURL
	}
	specs := pushRefspecs(rem, opts)
	prog := opts.Progress
	if len(specs) == 0 {
		return PushResult{}, nil
	}

	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return PushResult{}, err
	}
	defer func() { _ = db.Close() }()

	store, err := refsOpen(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Bare:      r.IsBare(),
		Peeler:    db,
		Committer: refs.CommitterFor(r.Config()),
	})
	if err != nil {
		return PushResult{}, err
	}
	defer func() { _ = store.Close() }()

	transportOpts := opts.Transport
	transportOpts.Progress = prog
	prog.Phase("connecting")
	session, err := dial(ctx, url, transport.ReceivePack, transportOpts)
	if err != nil {
		return PushResult{}, err
	}
	defer func() { _ = session.Close() }()

	adv, err := session.Advertise(ctx)
	if err != nil {
		return PushResult{}, err
	}

	planner := newPushPlanner(store, db, rem, opts, adv)
	if err := planner.plan(specs); err != nil {
		return PushResult{}, err
	}
	result := PushResult{Rejected: planner.rejected}
	planErr := errors.Join(planner.errs...)
	if len(planner.pending) == 0 {
		return result, planErr
	}

	updates := make([]transport.Update, 0, len(planner.pending))
	newIDs := make([]hash.ObjectID, 0, len(planner.pending))
	for _, p := range planner.pending {
		updates = append(updates, transport.Update{Name: p.name.String(), Old: p.old, New: p.new})
		if !p.deleted {
			newIDs = append(newIDs, p.new)
		}
	}

	haveIDs, err := gatherHaveIDs(adv, store, rem)
	if err != nil {
		return PushResult{}, err
	}

	prog.Phase(progress.PhaseCounting)
	ids, thin, err := collectPushObjects(ctx, db, haveIDs, newIDs, prog)
	if err != nil {
		return PushResult{}, err
	}

	pr, pw := io.Pipe()
	go func() {
		_, writeErr := pack.WritePack(ctx, pw, db, ids, pack.WriteOptions{
			Window:   packWindow,
			Depth:    packDepth,
			Thin:     thin,
			Progress: prog,
		})
		_ = pw.CloseWithError(writeErr)
	}()

	pushReq := transport.PushRequest{
		Updates:  updates,
		Pack:     pr,
		Atomic:   opts.Atomic,
		Options:  opts.Options,
		Progress: prog,
	}
	presp, err := session.Push(ctx, pushReq)
	if err != nil {
		return result, err
	}
	if !presp.UnpackOK {
		return result, errors.Join(planErr, fmt.Errorf("%w: unpack error: %s", ErrRejected, presp.UnpackError))
	}

	prog.Phase("updating-refs")
	changes, rejected, applyErrs, err := applyReportStatus(store, rem, planner.pending, presp)
	if err != nil {
		return result, err
	}
	result.Changes = changes
	result.Rejected = append(result.Rejected, rejected...)
	return result, errors.Join(planErr, errors.Join(applyErrs...))
}

type pushPlanner struct {
	store    *refs.Store
	db       *odb.DB
	rem      Remote
	opts     PushOptions
	advMap   map[string]hash.ObjectID
	seen     map[string]bool
	pending  []pendingUpdate
	rejected []transport.RefStatus
	errs     []error
}

func newPushPlanner(store *refs.Store, db *odb.DB, rem Remote, opts PushOptions, adv transport.Advertisement) *pushPlanner {
	advMap := make(map[string]hash.ObjectID, len(adv.Refs))
	for _, ref := range adv.Refs {
		advMap[ref.Name] = ref.ID
	}
	return &pushPlanner{store: store, db: db, rem: rem, opts: opts, advMap: advMap, seen: make(map[string]bool)}
}

func (p *pushPlanner) plan(specs []refspec.RefSpec) error {
	for _, spec := range specs {
		if spec.IsDelete() {
			if err := p.add(spec.Dst, hash.Zero, spec.Force, true); err != nil {
				return err
			}
			continue
		}
		if !spec.IsWildcard() {
			name := refs.Name(spec.Src)
			current, existed, err := lookupCurrent(p.store, name)
			if err != nil {
				return err
			}
			if !existed {
				p.rejected = append(p.rejected, transport.RefStatus{Name: spec.Dst, Message: "src refspec does not match any"})
				p.errs = append(p.errs, fmt.Errorf("remote: src refspec %s does not match any local ref", spec.Src))
				continue
			}
			if err := p.add(spec.Dst, current, spec.Force, false); err != nil {
				return err
			}
			continue
		}
		prefix := wildcardPrefix(spec.Src)
		for ref, err := range p.store.Prefix(prefix) {
			if err != nil {
				return err
			}
			dst, ok := spec.MatchSrc(ref.Name.String())
			if !ok {
				continue
			}
			if err := p.add(dst, ref.Target, spec.Force, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *pushPlanner) add(dst string, newID hash.ObjectID, forceSpec, deleted bool) error {
	if p.seen[dst] {
		return nil
	}
	p.seen[dst] = true
	old := p.advMap[dst]
	if deleted {
		if old.IsZero() {
			return nil
		}
	} else if old == newID {
		return nil
	}
	forced := forceSpec || p.opts.Force
	if lease, ok := p.opts.ForceWithLease[dst]; ok {
		tracking := refs.RemoteBranchName(p.rem.Name, refs.Name(dst).Short())
		current, _, err := lookupCurrent(p.store, tracking)
		if err != nil {
			return err
		}
		if current != lease {
			p.rejected = append(p.rejected, transport.RefStatus{Name: dst, Message: "stale info"})
			p.errs = append(p.errs, fmt.Errorf("%w: %s: stale info", ErrRejected, dst))
			return nil
		}
		forced = true
	}
	forcedApplied := false
	if !deleted && !old.IsZero() {
		ff, err := isFastForward(p.db, nil, old, newID)
		if err != nil {
			if !forced {
				return err
			}
			ff = false
		}
		if !ff && !forced {
			p.rejected = append(p.rejected, transport.RefStatus{Name: dst, Message: ErrNonFastForward.Error()})
			p.errs = append(p.errs, fmt.Errorf("%w: %s", ErrNonFastForward, dst))
			return nil
		}
		forcedApplied = !ff && forced
	}
	p.pending = append(p.pending, pendingUpdate{
		name:    refs.Name(dst),
		old:     old,
		new:     newID,
		created: old.IsZero() && !deleted,
		deleted: deleted,
		forced:  forcedApplied,
	})
	return nil
}

func applyReportStatus(store *refs.Store, rem Remote, pending []pendingUpdate, resp *transport.PushResult) ([]Change, []transport.RefStatus, []error, error) {
	byName := make(map[string]pendingUpdate, len(pending))
	for _, p := range pending {
		byName[p.name.String()] = p
	}
	tx := store.Begin()
	tx.SetMessage(pushReflogMessage)
	var changes []Change
	var rejected []transport.RefStatus
	var errs []error
	for _, status := range resp.Refs {
		p, ok := byName[status.Name]
		if !ok {
			continue
		}
		if !status.OK {
			rejected = append(rejected, status)
			errs = append(errs, fmt.Errorf("%w: %s: %s", ErrRejected, status.Name, status.Message))
			continue
		}
		tracking := refs.RemoteBranchName(rem.Name, refs.Name(status.Name).Short())
		current, existed, err := lookupCurrent(store, tracking)
		if err != nil {
			return nil, nil, nil, err
		}
		if p.deleted {
			if !existed {
				continue
			}
			if err := tx.Delete(tracking, current); err != nil {
				return nil, nil, nil, err
			}
			changes = append(changes, Change{Name: tracking, Old: current, Deleted: true})
			continue
		}
		if existed && current == p.new {
			continue
		}
		if err := tx.Update(tracking, p.new, current); err != nil {
			return nil, nil, nil, err
		}
		changes = append(changes, Change{Name: tracking, Old: current, New: p.new, Created: !existed, Forced: p.forced})
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, nil, err
	}
	return changes, rejected, errs, nil
}
