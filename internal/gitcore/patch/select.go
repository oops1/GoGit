package patch

import (
	"errors"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

var ErrUnsplittable = errors.New("patch: the change cannot be split at the chosen lines")

type Picked func(hunk, line int) bool

func All(int, int) bool { return true }

func Hunk(index int) Picked {
	return func(hunk, _ int) bool { return hunk == index }
}

func Lines(hunk int, lines ...int) Picked {
	chosen := make(map[int]bool, len(lines))
	for _, line := range lines {
		chosen[line] = true
	}
	return func(h, line int) bool { return h == hunk && chosen[line] }
}

func Select(hunks []diff.Hunk, picked Picked) ([]diff.Hunk, error) {
	var out []diff.Hunk
	delta := 0
	for index, hunk := range hunks {
		lines, changed := selectLines(hunk.Lines, func(line int) bool { return picked(index, line) })
		if !changed {
			continue
		}
		if !endsCleanly(lines) {
			return nil, fmt.Errorf("%w: hunk %d", ErrUnsplittable, index+1)
		}
		oldLines, newLines := counts(lines)
		out = append(out, diff.Hunk{
			OldStart: hunk.OldStart,
			OldLines: oldLines,
			NewStart: hunk.OldStart + delta,
			NewLines: newLines,
			Header:   hunk.Header,
			Lines:    lines,
		})
		delta += newLines - oldLines
	}
	return out, nil
}

func selectLines(lines []diff.Line, picked func(int) bool) ([]diff.Line, bool) {
	out := make([]diff.Line, 0, len(lines))
	changed := false
	for index, line := range lines {
		switch {
		case line.Kind == diff.KindContext:
			out = append(out, line)
		case picked(index):
			out = append(out, line)
			changed = true
		case line.Kind == diff.KindDel:
			line.Kind = diff.KindContext
			out = append(out, line)
		}
	}
	return out, changed
}

func endsCleanly(lines []diff.Line) bool {
	lastOld, lastNew := -1, -1
	for index, line := range lines {
		if line.Kind != diff.KindAdd {
			lastOld = index
		}
		if line.Kind != diff.KindDel {
			lastNew = index
		}
	}
	for index, line := range lines {
		if !line.NoNewline {
			continue
		}
		if line.Kind != diff.KindAdd && index != lastOld {
			return false
		}
		if line.Kind != diff.KindDel && index != lastNew {
			return false
		}
	}
	return true
}

func counts(lines []diff.Line) (oldLines, newLines int) {
	for _, line := range lines {
		if line.Kind != diff.KindAdd {
			oldLines++
		}
		if line.Kind != diff.KindDel {
			newLines++
		}
	}
	return oldLines, newLines
}

func Reverse(hunks []diff.Hunk) []diff.Hunk {
	out := make([]diff.Hunk, 0, len(hunks))
	for _, hunk := range hunks {
		lines := make([]diff.Line, len(hunk.Lines))
		for index, line := range hunk.Lines {
			switch line.Kind {
			case diff.KindAdd:
				line.Kind = diff.KindDel
			case diff.KindDel:
				line.Kind = diff.KindAdd
			}
			lines[index] = line
		}
		out = append(out, diff.Hunk{
			OldStart: hunk.NewStart,
			OldLines: hunk.NewLines,
			NewStart: hunk.OldStart,
			NewLines: hunk.OldLines,
			Header:   hunk.Header,
			Lines:    lines,
		})
	}
	return out
}
