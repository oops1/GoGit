package diffview

import "slices"

type Kind int

const (
	Context Kind = iota
	Added
	Removed
	HunkHeader
	NoNewline
)

var kindNames = map[Kind]string{
	Context:    "context",
	Added:      "added",
	Removed:    "removed",
	HunkHeader: "hunk",
	NoNewline:  "nonewline",
}

func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return "unknown"
}

type Span struct {
	Start int
	End   int
}

type Line struct {
	Kind  Kind
	OldNo int
	NewNo int
	Text  string
	Spans []Span
}

type Hunk struct {
	Header string
	Lines  []Line
}

type Document struct {
	OldName string
	NewName string
	Binary  bool
	Hunks   []Hunk
}

func (d Document) IsEmpty() bool {
	if d.Binary {
		return false
	}
	for _, h := range d.Hunks {
		if h.Header != "" || len(h.Lines) > 0 {
			return false
		}
	}
	return true
}

func (d Document) Clone() Document {
	out := d
	out.Hunks = slices.Clone(d.Hunks)
	for i := range out.Hunks {
		out.Hunks[i].Lines = slices.Clone(out.Hunks[i].Lines)
		for j := range out.Hunks[i].Lines {
			out.Hunks[i].Lines[j].Spans = slices.Clone(out.Hunks[i].Lines[j].Spans)
		}
	}
	return out
}
