package ops

import (
	"context"
	"crypto/sha1"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func patchID(ctx context.Context, source diff.Objects, oldTree, newTree hash.ObjectID) (hash.ObjectID, error) {
	files, err := diff.Trees(ctx, source, oldTree, newTree, diff.Options{Context: diff.DefaultContext})
	if err != nil {
		return hash.Zero, err
	}
	var id hash.ObjectID
	for _, file := range files {
		if file.OldID == file.NewID {
			continue
		}
		addPatchIDPart(&id, sha1.Sum(patchIDText(file)))
	}
	return id, nil
}

func patchIDText(file diff.File) []byte {
	path := withoutSpace(file.NewPath)
	oldLabel, newLabel := "---a/"+path, "+++b/"+path
	text := []byte("diff--gita/" + path + "b/" + path)
	switch {
	case file.Status == diff.StatusAdded:
		text = fmt.Appendf(text, "newfilemode%06o", file.NewMode)
		oldLabel = "---/dev/null"
	case file.Status == diff.StatusDeleted:
		text = fmt.Appendf(text, "deletedfilemode%06o", file.OldMode)
		newLabel = "+++/dev/null"
	case file.OldMode != file.NewMode:
		text = fmt.Appendf(text, "oldmode%06onewmode%06o", file.OldMode, file.NewMode)
	}
	if file.Binary {
		return append(text, file.OldID.String()+file.NewID.String()...)
	}
	text = append(text, oldLabel+newLabel...)
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			text = append(text, patchIDPrefix[line.Kind]...)
			text = append(text, withoutSpace(line.Text)...)
		}
	}
	return text
}

var patchIDPrefix = map[diff.Kind]string{diff.KindContext: "", diff.KindAdd: "+", diff.KindDel: "-"}

func withoutSpace(text string) string {
	out := make([]byte, 0, len(text))
	for i := range len(text) {
		switch text[i] {
		case ' ', '\t', '\n', '\r':
		default:
			out = append(out, text[i])
		}
	}
	return string(out)
}

func addPatchIDPart(id *hash.ObjectID, part [sha1.Size]byte) {
	carry := 0
	for i := range id {
		carry += int(id[i]) + int(part[i])
		id[i] = byte(carry)
		carry >>= 8
	}
}

func (m *merger) commitPatchID(commit *revision.Commit) (hash.ObjectID, bool, error) {
	var parent hash.ObjectID
	if len(commit.Parents) == 1 {
		tree, err := m.treeOf(commit.Parents[0])
		if err != nil {
			return hash.Zero, false, err
		}
		parent = tree
	}
	id, err := patchID(m.ctx, m.store(), parent, commit.Tree)
	return id, parent == commit.Tree, err
}

func (m *merger) upstreamPatchIDs(base, head hash.ObjectID) (map[hash.ObjectID]bool, error) {
	ids := map[hash.ObjectID]bool{}
	walk := revision.Walk(m.ctx, revision.Options{
		Context: revision.Context{Objects: m.store()},
		Include: []hash.ObjectID{base},
		Exclude: []hash.ObjectID{head},
	})
	for commit, err := range walk {
		if err != nil {
			return nil, err
		}
		if len(commit.Parents) > 1 {
			continue
		}
		id, _, err := m.commitPatchID(commit)
		if err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, nil
}

func (m *merger) appliedUpstream(commit *revision.Commit, upstream map[hash.ObjectID]bool) (bool, error) {
	if len(upstream) == 0 {
		return false, nil
	}
	id, empty, err := m.commitPatchID(commit)
	return err == nil && !empty && upstream[id], err
}
