package diff

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestDefaultsMatchTheGitDefaults(t *testing.T) {
	opts := Defaults()
	if opts.Algorithm != AlgorithmMyers {
		t.Errorf("Algorithm is %d instead of AlgorithmMyers", opts.Algorithm)
	}
	if opts.Context != DefaultContext {
		t.Errorf("Context is %d instead of %d", opts.Context, DefaultContext)
	}
	if !opts.IndentHeuristic {
		t.Error("IndentHeuristic should be on by default")
	}
	if !opts.DetectRenames || opts.DetectCopies {
		t.Error("renames should be detected by default and copies should not")
	}
	if opts.RenameThreshold != DefaultRenameThreshold || opts.RenameLimit != DefaultRenameLimit {
		t.Errorf("rename settings are %d/%d instead of %d/%d",
			opts.RenameThreshold, opts.RenameLimit, DefaultRenameThreshold, DefaultRenameLimit)
	}
	if opts.Abbrev != DefaultAbbrev || opts.StatWidth != DefaultStatWidth {
		t.Errorf("Abbrev/StatWidth are %d/%d instead of %d/%d",
			opts.Abbrev, opts.StatWidth, DefaultAbbrev, DefaultStatWidth)
	}
}

type numericOptions struct {
	context         int
	interHunk       int
	renameThreshold int
	renameLimit     int
	abbrev          int
	statWidth       int
}

func numbersOf(opts Options) numericOptions {
	return numericOptions{
		context:         opts.Context,
		interHunk:       opts.InterHunkContext,
		renameThreshold: opts.RenameThreshold,
		renameLimit:     opts.RenameLimit,
		abbrev:          opts.Abbrev,
		statWidth:       opts.StatWidth,
	}
}

func TestNormalizedClampsOutOfRangeSettings(t *testing.T) {
	cases := []struct {
		name string
		in   Options
		want numericOptions
	}{
		{
			name: "negative counters fall back to zero",
			in:   Options{Context: -1, InterHunkContext: -2, RenameLimit: -3},
			want: numericOptions{
				renameThreshold: DefaultRenameThreshold, abbrev: DefaultAbbrev, statWidth: DefaultStatWidth,
			},
		},
		{
			name: "a zero rename threshold falls back to the default",
			in:   Options{RenameThreshold: 0},
			want: numericOptions{renameThreshold: DefaultRenameThreshold, abbrev: DefaultAbbrev, statWidth: DefaultStatWidth},
		},
		{
			name: "a rename threshold above a hundred is capped",
			in:   Options{RenameThreshold: 250},
			want: numericOptions{renameThreshold: 100, abbrev: DefaultAbbrev, statWidth: DefaultStatWidth},
		},
		{
			name: "a long abbreviation is capped at the hex size",
			in:   Options{Abbrev: hash.HexSize + 5},
			want: numericOptions{renameThreshold: DefaultRenameThreshold, abbrev: hash.HexSize, statWidth: DefaultStatWidth},
		},
		{
			name: "a zero stat width falls back to the default",
			in:   Options{StatWidth: 0, Abbrev: 4},
			want: numericOptions{renameThreshold: DefaultRenameThreshold, abbrev: 4, statWidth: DefaultStatWidth},
		},
		{
			name: "settings inside the range are kept",
			in:   Options{Context: 5, InterHunkContext: 1, RenameThreshold: 30, RenameLimit: 7, Abbrev: 12, StatWidth: 100},
			want: numericOptions{context: 5, interHunk: 1, renameThreshold: 30, renameLimit: 7, abbrev: 12, statWidth: 100},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := numbersOf(c.in.normalized()); got != c.want {
				t.Errorf("normalized returned %+v instead of %+v", got, c.want)
			}
		})
	}
}

func TestIgnoresSpaceCoversOnlyTheSpaceFlags(t *testing.T) {
	cases := []struct {
		value Whitespace
		want  bool
	}{
		{0, false},
		{IgnoreAllSpace, true},
		{IgnoreSpaceChange, true},
		{IgnoreSpaceAtEOL, true},
		{IgnoreBlankLines, false},
		{IgnoreBlankLines | IgnoreAllSpace, true},
	}
	for _, c := range cases {
		if got := c.value.ignoresSpace(); got != c.want {
			t.Errorf("ignoresSpace of %d returned %v instead of %v", c.value, got, c.want)
		}
	}
}

func TestMinimumScoreScalesThePercentThreshold(t *testing.T) {
	cases := []struct {
		threshold int
		want      int
	}{
		{100, int(maxScore)},
		{50, int(maxScore) / 2},
		{0, int(maxScore) / 2},
	}
	for _, c := range cases {
		opts := Options{RenameThreshold: c.threshold}.normalized()
		if got := opts.minimumScore(); got != c.want {
			t.Errorf("threshold %d gave a minimum score of %d instead of %d", c.threshold, got, c.want)
		}
	}
}
