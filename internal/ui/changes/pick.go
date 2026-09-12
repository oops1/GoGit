package changes

import (
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/patch"
	"github.com/oops1/gogit/internal/ui/diffview"
)

func Picked(f diff.File, refs []diffview.LineRef) patch.Picked {
	chosen := make(map[[2]int]bool, len(refs))
	for _, ref := range refs {
		if line, ok := diffLine(f, ref); ok {
			chosen[[2]int{ref.Hunk, line}] = true
		}
	}
	return func(hunk, line int) bool { return chosen[[2]int{hunk, line}] }
}

func PickedHunks(hunks []int) patch.Picked {
	chosen := make(map[int]bool, len(hunks))
	for _, hunk := range hunks {
		chosen[hunk] = true
	}
	return func(hunk, _ int) bool { return chosen[hunk] }
}

func diffLine(f diff.File, ref diffview.LineRef) (int, bool) {
	if ref.Hunk < 0 || ref.Hunk >= len(f.Hunks) {
		return 0, false
	}
	row := 0
	for index, line := range f.Hunks[ref.Hunk].Lines {
		if row == ref.Line {
			return index, true
		}
		row++
		if line.NoNewline {
			row++
		}
	}
	return 0, false
}
