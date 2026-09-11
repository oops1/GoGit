package merge

import "strings"

type Mode int

const (
	ModeFastForward Mode = iota
	ModeMergeCommit
	ModeFastForwardOnly
	ModeSquash
)

func (m Mode) AllowsNoCommit() bool {
	return m == ModeFastForward || m == ModeMergeCommit
}

type Request struct {
	Source   string
	Mode     Mode
	NoCommit bool
}

type Known struct {
	Current    string
	Candidates []string
}

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

const (
	hintSourceRequired  = "Dialog.Merge.Hint.SourceRequired"
	hintSourceIsCurrent = "Dialog.Merge.Hint.SourceIsCurrent"
	hintFastForward     = "Dialog.Merge.Hint.FastForward"
	hintMergeCommit     = "Dialog.Merge.Hint.MergeCommit"
	hintFastForwardOnly = "Dialog.Merge.Hint.FastForwardOnly"
	hintSquash          = "Dialog.Merge.Hint.Squash"
	hintNoCommit        = "Dialog.Merge.Hint.NoCommit"
)

func Validate(req Request, known Known) Hint {
	source := strings.TrimSpace(req.Source)
	switch {
	case source == "":
		return Hint{Key: hintSourceRequired}
	case source == known.Current:
		return Hint{Key: hintSourceIsCurrent, Args: []any{source}}
	case req.Mode == ModeSquash:
		return Hint{Key: hintSquash, Args: []any{source}, OK: true}
	case req.Mode == ModeFastForwardOnly:
		return Hint{Key: hintFastForwardOnly, Args: []any{known.Current, source}, OK: true}
	case req.NoCommit:
		return Hint{Key: hintNoCommit, Args: []any{source}, OK: true}
	case req.Mode == ModeMergeCommit:
		return Hint{Key: hintMergeCommit, Args: []any{source, known.Current}, OK: true}
	}
	return Hint{Key: hintFastForward, Args: []any{source, known.Current}, OK: true}
}
