package journal

import (
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

const (
	graphMark     = "*"
	shortHashSize = 7
	dateLayout    = "2006-01-02 15:04"
)

type Row struct {
	Graph     string
	Message   string
	Author    string
	Date      string
	ShortHash string
	ID        hash.ObjectID
	Refs      []string
	Unpushed  bool
}

func newRow(commit *revision.Commit, decorations map[hash.ObjectID][]string) Row {
	return Row{
		Graph:     graphMark,
		Message:   firstLine(commit.Message),
		Author:    commit.Author.Name,
		Date:      commit.Author.When.Local().Format(dateLayout),
		ShortHash: commit.ID.String()[:shortHashSize],
		ID:        commit.ID,
		Refs:      decorations[commit.ID],
		Unpushed:  false,
	}
}

func firstLine(message string) string {
	if index := strings.IndexByte(message, '\n'); index >= 0 {
		return message[:index]
	}
	return message
}
