package merge

import (
	"bytes"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

type Style int

const (
	StyleMerge Style = iota
	StyleDiff3
	StyleZDiff3
)

const (
	oursMarker   = "<<<<<<<"
	baseMarker   = "|||||||"
	middleMarker = "======="
	theirsMarker = ">>>>>>>"
	joinGap      = 3
)

type Labels struct {
	Ours   string
	Base   string
	Theirs string
}

type Options struct {
	Style  Style
	Labels Labels
	Diff   diff.Options
}

type Result struct {
	Content   []byte
	Conflicts int
}

type region struct {
	start int
	end   int
	lines []string
}

type chunk struct {
	conflict bool
	ours     []string
	theirs   []string
	base     []string
}

func File(base, ours, theirs []byte, opts Options) Result {
	chunks := chunksOf(base, ours, theirs, opts)
	if opts.Style == StyleMerge {
		chunks = join(chunks)
	}
	return render(chunks, opts)
}

func chunksOf(base, ours, theirs []byte, opts Options) []chunk {
	baseLines := splitLines(base)
	ourChanges := changesOf(base, ours, opts.Diff)
	theirChanges := changesOf(base, theirs, opts.Diff)

	var out []chunk
	at, i, j := 0, 0, 0
	for i < len(ourChanges) || j < len(theirChanges) {
		start := nextStart(ourChanges, theirChanges, i, j)
		out = appendClean(out, baseLines[at:start])

		end, takenOurs, takenTheirs := span(ourChanges, theirChanges, i, j)
		ourSide := applyRegion(baseLines, ourChanges[i:takenOurs], start, end)
		theirSide := applyRegion(baseLines, theirChanges[j:takenTheirs], start, end)
		baseSide := baseLines[start:end]
		i, j, at = takenOurs, takenTheirs, end

		switch {
		case slices.Equal(ourSide, theirSide), slices.Equal(theirSide, baseSide):
			out = appendClean(out, ourSide)
		case slices.Equal(ourSide, baseSide):
			out = appendClean(out, theirSide)
		default:
			out = append(out, chunk{conflict: true, ours: ourSide, theirs: theirSide, base: baseSide})
		}
	}
	return appendClean(out, baseLines[at:])
}

func appendClean(out []chunk, lines []string) []chunk {
	if len(lines) == 0 {
		return out
	}
	if last := len(out) - 1; last >= 0 && !out[last].conflict {
		out[last].ours = append(out[last].ours, lines...)
		return out
	}
	return append(out, chunk{ours: lines})
}

func join(chunks []chunk) []chunk {
	for at := 1; at+1 < len(chunks); {
		if !joinable(chunks, at) {
			at++
			continue
		}
		chunks[at-1] = joined(chunks[at-1], chunks[at], chunks[at+1])
		chunks = slices.Delete(chunks, at, at+2)
	}
	return chunks
}

func joinable(chunks []chunk, at int) bool {
	return chunks[at-1].conflict && !chunks[at].conflict &&
		chunks[at+1].conflict && len(chunks[at].ours) <= joinGap
}

func joined(first, between, second chunk) chunk {
	return chunk{
		conflict: true,
		ours:     concat(first.ours, between.ours, second.ours),
		theirs:   concat(first.theirs, between.ours, second.theirs),
		base:     concat(first.base, between.ours, second.base),
	}
}

func concat(parts ...[]string) []string {
	var out []string
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func render(chunks []chunk, opts Options) Result {
	var out []string
	conflicts := 0
	for _, current := range chunks {
		if !current.conflict {
			out = append(out, current.ours...)
			continue
		}
		conflicts++
		out = appendConflict(out, current, opts)
	}
	return Result{Content: []byte(strings.Join(out, "")), Conflicts: conflicts}
}

func appendConflict(out []string, current chunk, opts Options) []string {
	if opts.Style == StyleDiff3 {
		return conflictBody(out, current.ours, current.theirs, current.base, opts)
	}
	head, ours, theirs, tail := trimCommon(current.ours, current.theirs)
	out = append(out, head...)
	return append(conflictBody(out, ours, theirs, current.base, opts), tail...)
}

func conflictBody(out, ourSide, theirSide, baseSide []string, opts Options) []string {
	out = append(out, marker(oursMarker, opts.Labels.Ours))
	out = appendLines(out, ourSide)
	if opts.Style != StyleMerge {
		out = append(out, marker(baseMarker, opts.Labels.Base))
		out = appendLines(out, baseSide)
	}
	out = append(out, marker(middleMarker, ""))
	out = appendLines(out, theirSide)
	return append(out, marker(theirsMarker, opts.Labels.Theirs))
}

func trimCommon(ourSide, theirSide []string) (head, ours, theirs, tail []string) {
	at := 0
	for at < len(ourSide) && at < len(theirSide) && ourSide[at] == theirSide[at] {
		at++
	}
	head = ourSide[:at]
	ours, theirs = ourSide[at:], theirSide[at:]
	back := 0
	for back < len(ours) && back < len(theirs) && ours[len(ours)-1-back] == theirs[len(theirs)-1-back] {
		back++
	}
	tail = ours[len(ours)-back:]
	return head, ours[:len(ours)-back], theirs[:len(theirs)-back], tail
}

func appendLines(out, lines []string) []string {
	for _, line := range lines {
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		out = append(out, line)
	}
	return out
}

func marker(mark, label string) string {
	if label == "" {
		return mark + "\n"
	}
	return mark + " " + label + "\n"
}

func nextStart(ours, theirs []region, i, j int) int {
	switch {
	case i >= len(ours):
		return theirs[j].start
	case j >= len(theirs):
		return ours[i].start
	default:
		return min(ours[i].start, theirs[j].start)
	}
}

func span(ours, theirs []region, i, j int) (int, int, int) {
	start := nextStart(ours, theirs, i, j)
	end := start
	for i < len(ours) && ours[i].start == start {
		end = max(end, ours[i].end)
		i++
	}
	for j < len(theirs) && theirs[j].start == start {
		end = max(end, theirs[j].end)
		j++
	}
	for {
		grown := false
		for i < len(ours) && ours[i].start <= end {
			end = max(end, ours[i].end)
			i++
			grown = true
		}
		for j < len(theirs) && theirs[j].start <= end {
			end = max(end, theirs[j].end)
			j++
			grown = true
		}
		if !grown {
			return end, i, j
		}
	}
}

func applyRegion(baseLines []string, changes []region, from, to int) []string {
	var out []string
	at := from
	for _, change := range changes {
		out = append(out, baseLines[at:change.start]...)
		out = append(out, change.lines...)
		at = change.end
	}
	if at < to {
		out = append(out, baseLines[at:to]...)
	}
	return out
}

func changesOf(base, side []byte, opts diff.Options) []region {
	if bytes.Equal(base, side) {
		return nil
	}
	opts.Context = 0
	opts.InterHunkContext = 0
	var out []region
	for _, hunk := range diff.Blobs(base, side, opts) {
		out = append(out, regionOf(hunk))
	}
	return out
}

func regionOf(hunk diff.Hunk) region {
	current := region{start: hunk.OldStart - 1}
	current.end = current.start + hunk.OldLines
	for _, line := range hunk.Lines {
		if line.Kind == diff.KindDel {
			continue
		}
		text := line.Text
		if !line.NoNewline {
			text += "\n"
		}
		current.lines = append(current.lines, text)
	}
	return current
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	text := string(data)
	lines := make([]string, 0, strings.Count(text, "\n")+1)
	for len(text) > 0 {
		at := strings.IndexByte(text, '\n')
		if at < 0 {
			return append(lines, text)
		}
		lines = append(lines, text[:at+1])
		text = text[at+1:]
	}
	return lines
}
