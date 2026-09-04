package diff

import (
	"errors"
	"strings"
	"testing"
)

func TestApplyRebuildsTheNewSideForEveryCorpusPair(t *testing.T) {
	for _, pair := range corpus() {
		for _, v := range variants() {
			if v.opts.IgnoreWhitespace != 0 {
				continue
			}
			t.Run(pair.name+"/"+v.name, func(t *testing.T) {
				hunks := Blobs([]byte(pair.old), []byte(pair.new), v.opts)
				got, err := Apply([]byte(pair.old), hunks)
				if err != nil {
					t.Fatalf("Apply returned error %v", err)
				}
				if string(got) != pair.new {
					t.Errorf("Apply produced %q instead of %q", got, pair.new)
				}
			})
		}
	}
}

func TestApplyKeepsTheOldSideWhenThereAreNoHunks(t *testing.T) {
	got, err := Apply([]byte("a\nb\n"), nil)
	if err != nil {
		t.Fatalf("Apply returned error %v", err)
	}
	if string(got) != "a\nb\n" {
		t.Errorf("Apply produced %q instead of the original text", got)
	}
}

func TestApplyFailsOnUnusableHunks(t *testing.T) {
	cases := []struct {
		name  string
		old   string
		hunks []Hunk
	}{
		{
			name:  "start before the current position",
			old:   "a\nb\n",
			hunks: []Hunk{{OldStart: 0, OldLines: 1, Lines: []Line{{Kind: KindDel, Text: "a"}}}},
		},
		{
			name:  "start past the end of the file",
			old:   "a\n",
			hunks: []Hunk{{OldStart: 9, OldLines: 1, Lines: []Line{{Kind: KindDel, Text: "a"}}}},
		},
		{
			name:  "context line does not match",
			old:   "a\nb\n",
			hunks: []Hunk{{OldStart: 1, OldLines: 1, Lines: []Line{{Kind: KindContext, Text: "z"}}}},
		},
		{
			name:  "deleted line runs past the end of the file",
			old:   "a\n",
			hunks: []Hunk{{OldStart: 2, OldLines: 1, Lines: []Line{{Kind: KindDel, Text: "b"}}}},
		},
		{
			name:  "line count disagrees with the header",
			old:   "a\nb\n",
			hunks: []Hunk{{OldStart: 1, OldLines: 2, Lines: []Line{{Kind: KindContext, Text: "a"}}}},
		},
		{
			name: "second hunk overlaps the first",
			old:  "a\nb\nc\n",
			hunks: []Hunk{
				{OldStart: 1, OldLines: 1, Lines: []Line{{Kind: KindDel, Text: "a"}}},
				{OldStart: 1, OldLines: 1, Lines: []Line{{Kind: KindDel, Text: "a"}}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Apply([]byte(c.old), c.hunks)
			if !errors.Is(err, ErrApply) {
				t.Fatalf("Apply returned (%q, %v) instead of ErrApply", got, err)
			}
		})
	}
}

func TestApplyKeepsALineWithoutATrailingNewline(t *testing.T) {
	hunks := Blobs([]byte("a\nb"), []byte("a\nc"), Defaults())
	got, err := Apply([]byte("a\nb"), hunks)
	if err != nil {
		t.Fatalf("Apply returned error %v", err)
	}
	if string(got) != "a\nc" {
		t.Errorf("Apply produced %q instead of %q", got, "a\nc")
	}
}

func TestIsBinaryLooksOnlyAtTheHeadOfTheData(t *testing.T) {
	head := append([]byte("text\n"), 0)
	if !isBinary(head) {
		t.Error("a NUL byte near the start should mark the data as binary")
	}
	tail := append([]byte(strings.Repeat("x", binarySniffLimit+10)), 0)
	if isBinary(tail) {
		t.Error("a NUL byte past the sniff limit should be ignored")
	}
}

func TestBinaryForPrefersTheHint(t *testing.T) {
	opts := Defaults()
	opts.BinaryHint = func(path string) (bool, bool) {
		return path == "forced.txt", path != "unknown.txt"
	}
	if !binaryFor("forced.txt", []byte("plain text\n"), opts) {
		t.Error("the hint should mark forced.txt as binary")
	}
	if binaryFor("other.txt", []byte{0}, opts) {
		t.Error("the hint should mark other.txt as text")
	}
	if !binaryFor("unknown.txt", []byte{0}, opts) {
		t.Error("an unknown path should fall back to sniffing the data")
	}
	if binaryFor("plain.txt", []byte("plain\n"), Defaults()) {
		t.Error("text without a hint should not be binary")
	}
}

func TestBlobsOnEqualInputProducesNoHunks(t *testing.T) {
	for _, algorithm := range []Algorithm{AlgorithmMyers, AlgorithmHistogram} {
		opts := Defaults()
		opts.Algorithm = algorithm
		if hunks := Blobs([]byte("a\nb\n"), []byte("a\nb\n"), opts); len(hunks) != 0 {
			t.Errorf("algorithm %d produced %d hunks for equal input", algorithm, len(hunks))
		}
	}
}
