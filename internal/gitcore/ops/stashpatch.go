package ops

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

const binarySniffLength = 8000

func looksBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), binarySniffLength)], 0) >= 0
}

func (m *merger) binaryContent(path string, blobs ...[]byte) (bool, error) {
	opts, err := repoDiffOptions(m.r, diff.Options{})
	if err != nil {
		return false, err
	}
	if binary, known := opts.BinaryHint(path); known {
		return binary, nil
	}
	return slices.ContainsFunc(blobs, looksBinary), nil
}

func splitRecords(data []byte) []string {
	records := strings.SplitAfter(string(data), "\n")
	return slices.DeleteFunc(records, func(record string) bool { return record == "" })
}

func hunkRecord(line diff.Line) string {
	if line.NoNewline {
		return line.Text
	}
	return line.Text + "\n"
}

type hunkImage struct {
	pre, post []string
	trailing  int
}

func imageOf(hunk diff.Hunk) hunkImage {
	var image hunkImage
	for _, line := range hunk.Lines {
		record := hunkRecord(line)
		if line.Kind != diff.KindAdd {
			image.pre = append(image.pre, record)
		}
		if line.Kind != diff.KindDel {
			image.post = append(image.post, record)
		}
	}
	for _, line := range slices.Backward(hunk.Lines) {
		if line.Kind != diff.KindContext {
			break
		}
		image.trailing++
	}
	return image
}

func applyHunks(data []byte, hunks []diff.Hunk) ([]byte, error) {
	lines := splitRecords(data)
	delta := 0
	for number, hunk := range hunks {
		image := imageOf(hunk)
		start := max(hunk.OldStart-1, 0)
		at, found := findImage(lines, image, start+delta, hunk.OldStart <= 1, image.trailing == 0)
		if !found {
			return nil, fmt.Errorf("%w: hunk %d does not apply", diff.ErrApply, number+1)
		}
		lines = slices.Concat(lines[:at], image.post, lines[at+len(image.pre):])
		delta = at - start + len(image.post) - len(image.pre)
	}
	return []byte(strings.Join(lines, "")), nil
}

func findImage(lines []string, image hunkImage, expected int, atStart, atEnd bool) (int, bool) {
	fits := func(at int) bool {
		switch {
		case at < 0 || at+len(image.pre) > len(lines):
			return false
		case atStart && at != 0:
			return false
		case atEnd && at+len(image.pre) != len(lines):
			return false
		}
		return slices.Equal(lines[at:at+len(image.pre)], image.pre)
	}
	for distance := 0; distance <= len(lines); distance++ {
		if fits(expected + distance) {
			return expected + distance, true
		}
		if distance > 0 && fits(expected-distance) {
			return expected - distance, true
		}
	}
	return 0, false
}
