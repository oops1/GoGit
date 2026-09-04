package diff

import "github.com/oops1/gogit/internal/gitcore/hash"

type Algorithm uint8

const (
	AlgorithmMyers Algorithm = iota
	AlgorithmHistogram
)

type Whitespace uint8

const (
	IgnoreAllSpace Whitespace = 1 << iota
	IgnoreSpaceChange
	IgnoreSpaceAtEOL
	IgnoreBlankLines
)

const (
	DefaultContext         = 3
	DefaultRenameThreshold = 50
	DefaultRenameLimit     = 1000
	DefaultAbbrev          = 7
	DefaultStatWidth       = 80
)

const (
	maxScore           = 60000.0
	numCandidatePerDst = 4
	binarySniffLimit   = 8000
)

type BinaryHint func(path string) (binary bool, known bool)

type Options struct {
	Algorithm        Algorithm
	IgnoreWhitespace Whitespace
	Context          int
	InterHunkContext int
	IndentHeuristic  bool
	DetectRenames    bool
	DetectCopies     bool
	RenameThreshold  int
	RenameLimit      int
	Paths            []string
	Abbrev           int
	StatWidth        int
	BinaryHint       BinaryHint
}

func Defaults() Options {
	return Options{
		Context:         DefaultContext,
		IndentHeuristic: true,
		DetectRenames:   true,
		RenameThreshold: DefaultRenameThreshold,
		RenameLimit:     DefaultRenameLimit,
		Abbrev:          DefaultAbbrev,
		StatWidth:       DefaultStatWidth,
	}
}

func (o Options) normalized() Options {
	if o.Context < 0 {
		o.Context = 0
	}
	if o.InterHunkContext < 0 {
		o.InterHunkContext = 0
	}
	if o.RenameThreshold <= 0 {
		o.RenameThreshold = DefaultRenameThreshold
	}
	if o.RenameThreshold > 100 {
		o.RenameThreshold = 100
	}
	if o.RenameLimit < 0 {
		o.RenameLimit = 0
	}
	if o.Abbrev <= 0 {
		o.Abbrev = DefaultAbbrev
	}
	if o.Abbrev > hash.HexSize {
		o.Abbrev = hash.HexSize
	}
	if o.StatWidth <= 0 {
		o.StatWidth = DefaultStatWidth
	}
	return o
}

func (w Whitespace) ignoresSpace() bool {
	return w&(IgnoreAllSpace|IgnoreSpaceChange|IgnoreSpaceAtEOL) != 0
}

func (o Options) minimumScore() int {
	return int(float64(o.RenameThreshold) * maxScore / 100)
}
