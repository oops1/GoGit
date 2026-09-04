package diff

import (
	"bytes"
	"fmt"
	"strings"
)

func Blobs(oldData, newData []byte, opts Options) []Hunk {
	return diffLines(splitLines(oldData), splitLines(newData), opts.normalized())
}

func diffLines(oldLines, newLines []string, opts Options) []Hunk {
	e := prepareEnv(oldLines, newLines, opts)
	if opts.Algorithm == AlgorithmHistogram {
		e.histogram(e.a.dstart+1, e.a.dend-e.a.dstart+1, e.b.dstart+1, e.b.dend-e.b.dstart+1)
	} else {
		e.myers()
	}
	compact(e.a, e.b, opts.IndentHeuristic)
	compact(e.b, e.a, opts.IndentHeuristic)
	changes := e.script()
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
