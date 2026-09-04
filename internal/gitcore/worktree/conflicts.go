package worktree

import "github.com/oops1/gogit/internal/gitcore/index"

func (w *Worktree) conflictEntries() []Entry {
	seen := map[string]bool{}
	var out []Entry
	for entry := range w.index.Entries() {
		if entry.Stage == index.StageMerged || seen[entry.Path] {
			continue
		}
		seen[entry.Path] = true
		kind := classifyConflict(w.index.Conflicts(entry.Path))
		out = append(out, Entry{Path: entry.Path, Staged: StatusUnmerged, Unstaged: StatusUnmerged, Conflict: kind})
	}
	return out
}

func classifyConflict(stages []index.Entry) ConflictKind {
	var has [4]bool
	for _, stage := range stages {
		if stage.Stage.Valid() {
			has[stage.Stage] = true
		}
	}
	switch {
	case !has[index.StageAncestor] && has[index.StageOurs] && has[index.StageTheirs]:
		return ConflictBothAdded
	case has[index.StageAncestor] && has[index.StageOurs] && has[index.StageTheirs]:
		return ConflictBothModified
	case has[index.StageAncestor] && has[index.StageOurs] && !has[index.StageTheirs]:
		return ConflictDeletedByThem
	case has[index.StageAncestor] && !has[index.StageOurs] && has[index.StageTheirs]:
		return ConflictDeletedByUs
	case !has[index.StageAncestor] && has[index.StageOurs] && !has[index.StageTheirs]:
		return ConflictAddedByUs
	case !has[index.StageAncestor] && !has[index.StageOurs] && has[index.StageTheirs]:
		return ConflictAddedByThem
	default:
		return ConflictBothDeleted
	}
}
