package ops

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var ErrNotARegularFile = errors.New("ops: only a regular file can be edited in the index")

type IndexSides struct {
	Path       string
	Head       []byte
	Index      []byte
	Working    []byte
	HasHead    bool
	HasIndex   bool
	HasWorking bool
	Binary     bool
}

func ReadIndexSides(ctx context.Context, r *repo.Repository, path string) (IndexSides, error) {
	if err := ctx.Err(); err != nil {
		return IndexSides{}, err
	}
	rel, err := cleanRepoPath(path)
	if err != nil {
		return IndexSides{}, err
	}
	rc, err := openRepoContext(r)
	if err != nil {
		return IndexSides{}, err
	}
	defer func() { _ = rc.close() }()
	idx, err := readIndex(r)
	if err != nil {
		return IndexSides{}, err
	}
	if len(idx.Conflicts(rel)) > 0 {
		return IndexSides{}, fmt.Errorf("%w: %s", ErrUnmergedPaths, rel)
	}
	sides := IndexSides{Path: rel}
	if sides.Index, sides.HasIndex, err = indexSideBlob(rc.db, idx, rel); err != nil {
		return IndexSides{}, err
	}
	if sides.Head, sides.HasHead, err = headSideBlob(rc, rel); err != nil {
		return IndexSides{}, err
	}
	if sides.Working, sides.HasWorking, err = workingSideBlob(r, rel); err != nil {
		return IndexSides{}, err
	}
	sides.Binary = attributes.IsBinaryContent(sides.Head) ||
		attributes.IsBinaryContent(sides.Index) ||
		attributes.IsBinaryContent(sides.Working)
	return sides, nil
}

func indexSideBlob(db *odb.DB, idx *index.Index, rel string) ([]byte, bool, error) {
	entry, ok := idx.Get(rel, index.StageMerged)
	if !ok {
		return nil, false, nil
	}
	return blobOf(db, entry.Mode, entry.ID, rel)
}

func headSideBlob(rc *repoContext, rel string) ([]byte, bool, error) {
	commitID, err := resolveHeadCommit(rc.refs)
	if err != nil {
		return nil, false, err
	}
	if commitID.IsZero() {
		return nil, false, nil
	}
	commit, err := dbCommit(rc.db, commitID)
	if err != nil {
		return nil, false, err
	}
	entry, found, err := treeEntryAt(rc.db, commit.Tree, rel)
	if err != nil || !found {
		return nil, false, err
	}
	return blobOf(rc.db, entry.mode, entry.id, rel)
}

func treeEntryAt(db *odb.DB, root hash.ObjectID, rel string) (treeEntry, bool, error) {
	current := treeEntry{mode: object.ModeTree, id: root}
	for _, name := range strings.Split(rel, "/") {
		if !current.mode.IsTree() {
			return treeEntry{}, false, nil
		}
		tree, err := dbTree(db, current.id)
		if err != nil {
			return treeEntry{}, false, err
		}
		found, ok := tree.Find(name)
		if !ok {
			return treeEntry{}, false, nil
		}
		current = treeEntry{mode: found.Mode, id: found.ID}
	}
	return current, true, nil
}

func blobOf(db *odb.DB, mode object.Mode, id hash.ObjectID, rel string) ([]byte, bool, error) {
	if !mode.IsRegular() {
		return nil, false, fmt.Errorf("%w: %s", ErrNotARegularFile, rel)
	}
	kind, data, err := dbGet(db, id)
	if err != nil {
		return nil, false, err
	}
	if kind != object.TypeBlob {
		return nil, false, fmt.Errorf("%w: %s", ErrNotARegularFile, rel)
	}
	return data, true, nil
}

func workingSideBlob(r *repo.Repository, rel string) ([]byte, bool, error) {
	wt, err := openWorkingTree(r)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = wt.close() }()
	info, err := fsRootLstat(wt.root, filepath.FromSlash(rel))
	if missingPath(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%w: %s", ErrNotARegularFile, rel)
	}
	data, err := fsRootReadFile(wt.root, filepath.FromSlash(rel))
	if err != nil {
		return nil, false, err
	}
	data, err = wt.stageConvert(rel, data)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func SaveIndexContent(ctx context.Context, r *repo.Repository, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rel, err := cleanRepoPath(path)
	if err != nil {
		return err
	}
	wt, err := openWorkingTree(r)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	rules, err := pathRulesOf(r)
	if err != nil {
		return err
	}
	lock, err := lockIndex(r)
	if err != nil {
		return err
	}
	if err := writeIndexContent(wt, db, lock.idx, rules, rel, content); err != nil {
		lock.abort()
		return err
	}
	return lock.commit()
}

func writeIndexContent(wt *workingTree, db *odb.DB, idx *index.Index, rules index.PathRules, rel string, content []byte) error {
	if len(idx.Conflicts(rel)) > 0 {
		return fmt.Errorf("%w: %s", ErrUnmergedPaths, rel)
	}
	mode, err := indexContentMode(wt, idx, rel)
	if err != nil {
		return err
	}
	id, err := dbPut(db, object.TypeBlob, content)
	if err != nil {
		return err
	}
	return idx.AddVerified(index.Entry{
		Path:  rel,
		Mode:  mode,
		ID:    id,
		Stage: index.StageMerged,
		Stat:  index.Stat{Size: uint32(len(content))},
	}, rules)
}

func indexContentMode(wt *workingTree, idx *index.Index, rel string) (object.Mode, error) {
	if entry, ok := idx.Get(rel, index.StageMerged); ok {
		if !entry.Mode.IsRegular() {
			return 0, fmt.Errorf("%w: %s", ErrNotARegularFile, rel)
		}
		return entry.Mode, nil
	}
	info, err := fsRootLstat(wt.root, filepath.FromSlash(rel))
	if missingPath(err) {
		return object.ModeBlob, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%w: %s", ErrNotARegularFile, rel)
	}
	if wt.fileMode && info.Mode().Perm()&0o111 != 0 {
		return object.ModeExecutable, nil
	}
	return object.ModeBlob, nil
}
