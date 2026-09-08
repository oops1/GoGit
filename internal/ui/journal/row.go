package journal

import (
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/ui/journal/graph"
)

const (
	shortHashSize = 7
	dateLayout    = "2006-01-02 15:04"
)

type RefKind int

const (
	RefBranch RefKind = iota
	RefRemote
	RefTag
)

type Ref struct {
	Name string
	Kind RefKind
	Head bool
}

type Row struct {
	Message   string
	Author    string
	Date      string
	ShortHash string
	ID        hash.ObjectID
	Parents   []hash.ObjectID
	Refs      []Ref
	Unpushed  bool
	Graph     graph.Row
}

func newRow(commit *revision.Commit, decorations map[hash.ObjectID][]Ref, unpushed map[hash.ObjectID]struct{}) Row {
	return Row{
		Message:   firstLine(commit.Message),
		Author:    commit.Author.Name,
		Date:      commit.Author.When.Local().Format(dateLayout),
		ShortHash: commit.ID.String()[:shortHashSize],
		ID:        commit.ID,
		Parents:   commit.Parents,
		Refs:      decorations[commit.ID],
		Unpushed:  contains(unpushed, commit.ID),
	}
}

func contains(set map[hash.ObjectID]struct{}, id hash.ObjectID) bool {
	_, ok := set[id]
	return ok
}

func firstLine(message string) string {
	if index := strings.IndexByte(message, '\n'); index >= 0 {
		return message[:index]
	}
	return message
}
