package diff

import (
	"bytes"
	"fmt"
	"strings"
)

func Blobs(oldData, newData []byte, opts Options) []Hunk {
	opts = opts.normalized()
	oldData, newData = trimForContext(oldData, newData, opts)
	return diffLines(splitLines(oldData), splitLines(newData), opts)
}

const tailBlock = 1024

func trimForContext(oldData, newData []byte, opts Options) ([]byte, []byte) {
	if opts.Context > 0 {
		return oldData, newData
	}
	return trimCommonTail(oldData, newData)
}

func trimCommonTail(a, b []byte) ([]byte, []byte) {
	smaller := min(len(a), len(b))
	trimmed := 0
	for tailBlock+trimmed <= smaller && bytes.Equal(a[len(a)-trimmed-tailBlock:len(a)-trimmed], b[len(b)-trimmed-tailBlock:len(b)-trimmed]) {
		trimmed += tailBlock
	}
	tail := a[len(a)-trimmed:]
	recovered := 0
	for recovered < trimmed {
		recovered++
		if tail[recovered-1] == '\n' {
			break
		}
	}
	cut := trimmed - recovered
	return a[:len(a)-cut], b[:len(b)-cut]
}

type Change struct {
	OldIndex int
	OldCount int
	NewIndex int
	NewCount int
}

func Changes(oldData, newData []byte, opts Options) []Change {
	opts = opts.normalized()
	opts.Context = 0
	oldData, newData = trimCommonTail(oldData, newData)
	_, changes := computeChanges(splitLines(oldData), splitLines(newData), opts)
	out := make([]Change, len(changes))
	for at, c := range changes {
		out[at] = Change{OldIndex: c.i1, OldCount: c.chg1, NewIndex: c.i2, NewCount: c.chg2}
	}
	return out
}

func computeChanges(oldLines, newLines []string, opts Options) (*env, []change) {
	e := prepareEnv(oldLines, newLines, opts)
	if opts.Algorithm == AlgorithmHistogram {
		e.histogram(e.a.dstart+1, e.a.dend-e.a.dstart+1, e.b.dstart+1, e.b.dend-e.b.dstart+1)
	} else {
		e.myers()
	}
	compact(e.a, e.b, opts.IndentHeuristic)
	compact(e.b, e.a, opts.IndentHeuristic)
	return e, e.script()
}

func diffLines(oldLines, newLines []string, opts Options) []Hunk {
	e, changes := computeChanges(oldLines, newLines, opts)
	if opts.IgnoreWhitespace&IgnoreBlankLines != 0 {
		e.markIgnorable(changes)
	}
	return e.emit(changes)
}

func isBinary(data []byte) bool {
	if len(data) > binarySniffLimit {
		data = data[:binarySniffLimit]
	}
	return bytes.IndexByte(data, 0) >= 0
}

func binaryFor(path string, data []byte, opts Options) bool {
	if opts.BinaryHint != nil {
		if binary, known := opts.BinaryHint(path); known {
			return binary
		}
	}
	return isBinary(data)
}

func lineRecord(line Line) string {
	if line.NoNewline {
		return line.Text
	}
	return line.Text + "\n"
}

func Apply(old []byte, hunks []Hunk) ([]byte, error) {
	lines := splitLines(old)
	var out strings.Builder
	out.Grow(len(old))
	pos := 0
	for index, hunk := range hunks {
		start := hunk.OldStart - 1
		if start < pos || start > len(lines) {
			return nil, fmt.Errorf("%w: hunk %d starts at line %d", ErrApply, index+1, hunk.OldStart)
		}
		for ; pos < start; pos++ {
			out.WriteString(lines[pos])
		}
		for _, line := range hunk.Lines {
			record := lineRecord(line)
			if line.Kind == KindAdd {
				out.WriteString(record)
				continue
			}
			if pos >= len(lines) || lines[pos] != record {
				return nil, fmt.Errorf("%w: hunk %d does not match line %d", ErrApply, index+1, pos+1)
			}
			pos++
			if line.Kind == KindContext {
				out.WriteString(record)
			}
		}
		if pos != start+hunk.OldLines {
			return nil, fmt.Errorf("%w: hunk %d covers %d lines instead of %d", ErrApply, index+1, pos-start, hunk.OldLines)
		}
	}
	for ; pos < len(lines); pos++ {
		out.WriteString(lines[pos])
	}
	return []byte(out.String()), nil
}
