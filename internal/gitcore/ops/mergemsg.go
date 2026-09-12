package ops

import (
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/gitcore/wildmatch"
)

const (
	suppressDestKey  = "merge.suppressdest"
	squashHeader     = "Squashed commit of the following:\n"
	squashDateLayout = "Mon Jan 2 15:04:05 2006 -0700"
	squashIndent     = "    "
)

var defaultSuppressDest = []string{"main", "master"}

func defaultMergeMessage(r *repo.Repository, store *refs.Store, name string, ref refs.Name, head headTarget) string {
	var subject string
	branch, early, ok := branchAncestry(store, name)
	switch {
	case ok && early:
		subject = "Merge branch '" + branch + "' (early part)"
	case ok:
		subject = "Merge branch '" + branch + "'"
	case ref.IsBranch():
		subject = "Merge branch '" + ref.Short() + "'"
	case ref.IsRemote():
		subject = "Merge remote-tracking branch '" + ref.Short() + "'"
	case ref.IsTag():
		subject = "Merge tag '" + ref.Short() + "'"
	default:
		subject = "Merge commit '" + name + "'"
	}
	if dest := head.ref.Short(); !suppressesDest(r, dest) {
		subject += " into " + dest
	}
	return subject + "\n"
}

func branchAncestry(store *refs.Store, name string) (string, bool, bool) {
	branch, early := ancestrySuffix(name)
	if branch == name {
		return "", false, false
	}
	if _, err := store.Lookup(refs.BranchName(branch)); err != nil {
		return "", false, false
	}
	return branch, early, true
}

func ancestrySuffix(name string) (string, bool) {
	if trimmed := strings.TrimRight(name, "^"); trimmed != name {
		return trimmed, true
	}
	at := strings.LastIndexByte(name, '~')
	if at < 0 {
		return name, false
	}
	digits := name[at+1:]
	if strings.Trim(digits, "0123456789") != "" {
		return name, false
	}
	return name[:at], strings.Trim(digits, "0") != "" || digits == ""
}

func suppressesDest(r *repo.Repository, branch string) bool {
	patterns := defaultSuppressDest
	if r.Config().Has(suppressDestKey) {
		patterns = nil
		for _, value := range r.Config().GetAll(suppressDestKey) {
			if value == "" {
				patterns = nil
				continue
			}
			patterns = append(patterns, value)
		}
	}
	for _, pattern := range patterns {
		if wildmatch.Match(pattern, branch, 0) {
			return true
		}
	}
	return false
}

func withConflictList(message string, conflicts []string) string {
	if len(conflicts) == 0 {
		return message
	}
	var b strings.Builder
	b.WriteString(message)
	b.WriteString("\n# Conflicts:\n")
	for _, path := range conflicts {
		b.WriteString("#\t" + path + "\n")
	}
	return b.String()
}

func (m *merger) squashMessage(ours, theirs hash.ObjectID) (string, error) {
	var b strings.Builder
	b.WriteString(squashHeader)
	walk := revision.Walk(m.ctx, revision.Options{
		Context: revision.Context{Objects: m.store()},
		Include: []hash.ObjectID{theirs},
		Exclude: []hash.ObjectID{ours},
	})
	for commit, err := range walk {
		if err != nil {
			return "", err
		}
		b.WriteString("\ncommit " + commit.ID.String() + "\n")
		if len(commit.Parents) > 1 {
			short := make([]string, len(commit.Parents))
			for i, parent := range commit.Parents {
				short[i] = abbreviate(parent)
			}
			b.WriteString("Merge: " + strings.Join(short, " ") + "\n")
		}
		b.WriteString("Author: " + commit.Author.Name + " <" + commit.Author.Email + ">\n")
		b.WriteString("Date:   " + commit.Author.When.Format(squashDateLayout) + "\n\n")
		for line := range strings.SplitSeq(strings.TrimRight(commit.Message, "\n"), "\n") {
			b.WriteString(squashIndent + line + "\n")
		}
	}
	return b.String(), nil
}
