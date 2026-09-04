package diff

import (
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type Status uint8

const (
	StatusModified Status = iota
	StatusAdded
	StatusDeleted
	StatusRenamed
	StatusCopied
	StatusTypeChanged
)

type Kind uint8

const (
	KindContext Kind = iota
	KindAdd
	KindDel
)

type Line struct {
	Kind      Kind
	Text      string
	NoNewline bool
}

type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Header   string
	Lines    []Line
}

type File struct {
	OldPath    string
	NewPath    string
	OldMode    object.Mode
	NewMode    object.Mode
	OldID      hash.ObjectID
	NewID      hash.ObjectID
	Status     Status
	Similarity int
	Binary     bool
	OldSize    int
	NewSize    int
	Hunks      []Hunk
	Parts      []File
}

type Span struct {
	Kind Kind
	Text string
}

func (s Status) String() string {
	switch s {
	case StatusModified:
		return "M"
	case StatusAdded:
		return "A"
	case StatusDeleted:
		return "D"
	case StatusRenamed:
		return "R"
	case StatusCopied:
		return "C"
	case StatusTypeChanged:
		return "T"
	default:
		return "?"
	}
}

func (k Kind) prefix() string {
	switch k {
	case KindAdd:
		return "+"
	case KindDel:
		return "-"
	default:
		return " "
	}
}

func (f File) Added() int {
	added, _ := f.counts()
	return added
}

func (f File) Deleted() int {
	_, deleted := f.counts()
	return deleted
}

func (f File) counts() (added, deleted int) {
	for _, hunk := range f.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case KindAdd:
				added++
			case KindDel:
				deleted++
			case KindContext:
			}
		}
	}
	return added, deleted
}
