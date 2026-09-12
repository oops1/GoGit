package ops

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/rerere"
)

type rerereRun struct {
	ctx context.Context
	r   *repo.Repository
	wt  *workingTree
	db  *odb.DB
}

func (m *merger) rerere() *rerereRun {
	return &rerereRun{ctx: m.ctx, r: m.r, wt: m.wt, db: m.rc.db}
}

func recordRerereResolutions(ctx context.Context, r *repo.Repository, db *odb.DB) error {
	if !rerereActive(r) || r.IsBare() {
		return nil
	}
	wt, err := openWorkingTree(r)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()
	return (&rerereRun{ctx: ctx, r: r, wt: wt, db: db}).recordResolutions()
}

const rerereEnabledKey = "rerere.enabled"

const rerereAutoUpdateKey = "rerere.autoupdate"

func rerereActive(r *repo.Repository) bool {
	cfg := r.Config()
	if cfg.Has(rerereEnabledKey) {
		enabled, err := cfg.GetBool(rerereEnabledKey)
		return err == nil && enabled
	}
	return rerere.CacheExists(r.GitDir())
}

func rerereAutoUpdate(r *repo.Repository) bool {
	update, err := r.Config().GetBool(rerereAutoUpdateKey)
	return err == nil && update
}

func (w *workingTree) markerSizeOf(path string) int {
	value := w.attrs.Get(path, "conflict-marker-size")["conflict-marker-size"]
	size, err := strconv.Atoi(value.Text())
	if err != nil || size <= 0 {
		return merge.DefaultMarkerSize
	}
	return size
}

func (p *rerereRun) conflicts(conflicted []string) error {
	if !rerereActive(p.r) {
		return nil
	}
	cache, err := rerere.OpenCache(p.r.GitDir())
	if err != nil {
		return err
	}

	known, err := readMergeRR(p.r)
	if err != nil {
		return err
	}
	replayed, err := p.rememberEach(cache, &known, conflicted)
	if err != nil {
		return err
	}
	if err := writeMergeRR(p.r, known); err != nil {
		return err
	}
	if len(replayed) == 0 || !rerereAutoUpdate(p.r) {
		return nil
	}
	return p.stageReplayed(replayed)
}

func (p *rerereRun) rememberEach(cache *rerere.Cache, known *[]rerere.Entry, conflicted []string) ([]string, error) {
	var replayed []string
	for _, path := range slices.Sorted(slices.Values(conflicted)) {
		data, err := p.readWorkFile(path)
		if err != nil {
			return nil, err
		}
		conflict, ok := rerere.Normalize(data, p.wt.markerSizeOf(path))
		if !ok {
			dropEntry(known, path)
			continue
		}
		done, err := p.replayOrRemember(cache, known, path, conflict)
		if err != nil {
			return nil, err
		}
		if done {
			replayed = append(replayed, path)
		}
	}
	return replayed, nil
}

func (p *rerereRun) replayOrRemember(cache *rerere.Cache, known *[]rerere.Entry, path string, conflict rerere.Conflict) (bool, error) {
	for _, variant := range cache.Variants(conflict.ID) {
		if !variant.Complete() {
			continue
		}
		resolved, err := p.replay(cache, conflict, variant.Index)
		if err != nil {
			return false, err
		}
		if resolved == nil {
			continue
		}
		if err := p.writeWorkFile(path, resolved); err != nil {
			return false, err
		}
		dropEntry(known, path)
		return true, nil
	}
	index := cache.VariantForAPreimage(conflict.ID)
	if err := cache.WritePreimage(conflict.ID, index, conflict.Preimage); err != nil {
		return false, err
	}
	rememberEntry(known, rerere.Entry{ID: conflict.ID, Variant: index, Path: path})
	return false, nil
}

func (p *rerereRun) replay(cache *rerere.Cache, conflict rerere.Conflict, variant int) ([]byte, error) {
	preimage, err := cache.Preimage(conflict.ID, variant)
	if err != nil {
		return nil, err
	}
	postimage, err := cache.Postimage(conflict.ID, variant)
	if err != nil {
		return nil, err
	}
	if err := cache.WriteThisimage(conflict.ID, variant, conflict.Preimage); err != nil {
		return nil, err
	}
	result := merge.File(preimage, conflict.Preimage, postimage, merge.Options{})
	if result.Conflicts > 0 {
		return nil, nil
	}
	return result.Content, nil
}

func (p *rerereRun) recordResolutions() error {
	known, err := readMergeRR(p.r)
	if err != nil || len(known) == 0 {
		return err
	}
	cache, err := rerere.OpenCache(p.r.GitDir())
	if err != nil {
		return err
	}

	left := make([]rerere.Entry, 0, len(known))
	for _, entry := range known {
		data, err := p.readWorkFile(entry.Path)
		if err != nil {
			return err
		}
		if _, conflicted := rerere.Normalize(data, p.wt.markerSizeOf(entry.Path)); conflicted {
			left = append(left, entry)
			continue
		}
		if err := cache.WritePostimage(entry.ID, entry.Variant, data); err != nil {
			return err
		}
	}
	return writeMergeRR(p.r, left)
}

func dropEntry(known *[]rerere.Entry, path string) {
	*known = slices.DeleteFunc(*known, func(entry rerere.Entry) bool { return entry.Path == path })
}

func rememberEntry(known *[]rerere.Entry, entry rerere.Entry) {
	dropEntry(known, entry.Path)
	*known = append(*known, entry)
	slices.SortFunc(*known, func(a, b rerere.Entry) int { return strings.Compare(a.Path, b.Path) })
}

func readMergeRR(r *repo.Repository) ([]rerere.Entry, error) {
	data, err := readStateFile(r, rerere.MergeRRFile)
	if err != nil || data == "" {
		return nil, err
	}
	return rerere.ParseMergeRR([]byte(data))
}

func writeMergeRR(r *repo.Repository, entries []rerere.Entry) error {
	return writeStateFile(r, rerere.MergeRRFile, string(rerere.FormatMergeRR(entries)))
}

func forgetMergeRR(r *repo.Repository) error {
	return removeStateFiles(r, rerere.MergeRRFile)
}

func (p *rerereRun) readWorkFile(path string) ([]byte, error) {
	data, err := fsRootReadFile(p.wt.root, filepath.FromSlash(path))
	if errors.Is(err, fs.ErrNotExist) || missingPath(err) {
		return nil, nil
	}
	return data, err
}

func (p *rerereRun) writeWorkFile(path string, data []byte) error {
	return fsRootWriteFile(p.wt.root, filepath.FromSlash(path), data, stateFileMode)
}

func (p *rerereRun) stageReplayed(paths []string) error {
	lock, err := lockIndex(p.r)
	if err != nil {
		return err
	}
	staging := &stager{ctx: p.ctx, wt: p.wt, db: p.db, idx: lock.idx}
	for _, path := range paths {
		if err := staging.stage(path); err != nil {
			lock.abort()
			return err
		}
	}
	return lock.commit()
}
