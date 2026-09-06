package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func (s *session) Push(ctx context.Context, req transport.PushRequest) (*transport.PushResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOpen(ctx); err != nil {
		return nil, err
	}

	result := &transport.PushResult{}
	if req.Pack != nil {
		if _, err := pack.IndexPack(ctx, req.Pack, s.repo.PackDir(), pack.IndexOptions{
			Bases:    s.db,
			FixThin:  true,
			Progress: req.Progress,
		}); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return nil, fmt.Errorf("%w: %s: %w", ErrReadOnly, s.repo.PackDir(), err)
			}
			result.UnpackError = err.Error()
			result.Refs = make([]transport.RefStatus, 0, len(req.Updates))
			for _, u := range req.Updates {
				result.Refs = append(result.Refs, transport.RefStatus{Name: u.Name, Message: "unpack failed"})
			}
			return result, nil
		}
		if _, err := s.db.Reload(); err != nil {
			return nil, err
		}
	}
	result.UnpackOK = true

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	plan, statuses := s.planUpdates(ctx, req.Updates, req.Options)
	if req.Atomic && len(statuses) > 0 {
		for _, u := range plan {
			statuses[u.Name] = transport.RefStatus{Name: u.Name, Message: "transaction aborted"}
		}
		plan = nil
	}
	if req.Atomic {
		s.applyAtomic(ctx, plan, statuses)
	} else {
		s.applyIndividually(ctx, plan, statuses)
	}

	result.Refs = make([]transport.RefStatus, 0, len(req.Updates))
	for _, u := range req.Updates {
		if st, ok := statuses[u.Name]; ok {
			result.Refs = append(result.Refs, st)
			continue
		}
		result.Refs = append(result.Refs, transport.RefStatus{Name: u.Name, OK: true})
	}
	return result, nil
}

func (s *session) planUpdates(ctx context.Context, updates []transport.Update, options []string) ([]transport.Update, map[string]transport.RefStatus) {
	plan := make([]transport.Update, 0, len(updates))
	statuses := make(map[string]transport.RefStatus, len(updates))
	rctx := revision.Context{Objects: s.db}
	for _, u := range updates {
		if err := ctx.Err(); err != nil {
			statuses[u.Name] = transport.RefStatus{Name: u.Name, Message: err.Error()}
			continue
		}
		if u.New.IsZero() || u.Old.IsZero() || isForced(options, u.Name) {
			plan = append(plan, u)
			continue
		}
		ff, err := revision.IsAncestor(rctx, u.Old, u.New)
		if err != nil {
			statuses[u.Name] = transport.RefStatus{Name: u.Name, Message: err.Error()}
			continue
		}
		if !ff {
			statuses[u.Name] = transport.RefStatus{Name: u.Name, Message: "non-fast-forward"}
			continue
		}
		plan = append(plan, u)
	}
	return plan, statuses
}

func (s *session) applyAtomic(ctx context.Context, plan []transport.Update, statuses map[string]transport.RefStatus) {
	if len(plan) == 0 {
		return
	}
	tx := s.refs.Begin()
	for _, u := range plan {
		if err := applyUpdate(tx, u); err != nil {
			tx.Rollback()
			for _, p := range plan {
				statuses[p.Name] = transport.RefStatus{Name: p.Name, Message: err.Error()}
			}
			return
		}
	}
	if err := ctx.Err(); err != nil {
		tx.Rollback()
		for _, p := range plan {
			statuses[p.Name] = transport.RefStatus{Name: p.Name, Message: err.Error()}
		}
		return
	}
	if err := tx.Commit(); err != nil {
		for _, p := range plan {
			statuses[p.Name] = transport.RefStatus{Name: p.Name, Message: err.Error()}
		}
	}
}

func (s *session) applyIndividually(ctx context.Context, plan []transport.Update, statuses map[string]transport.RefStatus) {
	for _, u := range plan {
		if err := ctx.Err(); err != nil {
			statuses[u.Name] = transport.RefStatus{Name: u.Name, Message: err.Error()}
			continue
		}
		tx := s.refs.Begin()
		if err := applyUpdate(tx, u); err != nil {
			tx.Rollback()
			statuses[u.Name] = transport.RefStatus{Name: u.Name, Message: err.Error()}
			continue
		}
		if err := tx.Commit(); err != nil {
			statuses[u.Name] = transport.RefStatus{Name: u.Name, Message: err.Error()}
		}
	}
}

func applyUpdate(tx *refs.Transaction, u transport.Update) error {
	name := refs.Name(u.Name)
	if u.New.IsZero() {
		return tx.Delete(name, u.Old)
	}
	return tx.Update(name, u.New, u.Old)
}

func isForced(options []string, name string) bool {
	for _, opt := range options {
		if opt == "force" {
			return true
		}
		if rest, ok := strings.CutPrefix(opt, "force:"); ok && rest == name {
			return true
		}
	}
	return false
}
